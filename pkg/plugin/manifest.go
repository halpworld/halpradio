package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var validIDRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-_]{1,63}$`)

var privateCIDRs []*net.IPNet

func init() {
	cidrs := []string{
		"127.0.0.0/8",     // IPv4 loopback
		"::1/128",         // IPv6 loopback
		"0.0.0.0/8",       // Current network / source
		"::/128",          // Unspecified
		"10.0.0.0/8",      // RFC1918 private
		"172.16.0.0/12",   // RFC1918 private
		"192.168.0.0/16",  // RFC1918 private
		"169.254.0.0/16",  // IPv4 Link-local / Cloud metadata (AWS/GCP/Azure)
		"fe80::/10",       // IPv6 Link-local
		"fc00::/7",        // IPv6 Unique local
		"100.64.0.0/10",   // CGNAT
		"198.18.0.0/15",   // Benchmark testing
		"192.0.0.0/24",    // IETF protocol assignments
		"192.0.2.0/24",    // TEST-NET-1
		"198.51.100.0/24", // TEST-NET-2
		"203.0.113.0/24",  // TEST-NET-3
		"224.0.0.0/4",     // IPv4 Multicast
		"240.0.0.0/4",     // Reserved
		"ff00::/8",        // IPv6 Multicast
	}
	for _, c := range cidrs {
		_, ipNet, err := net.ParseCIDR(c)
		if err == nil {
			privateCIDRs = append(privateCIDRs, ipNet)
		}
	}
}

// IsPrivateOrLoopbackIP checks whether an IP address belongs to loopback, private, link-local, or cloud metadata ranges.
func IsPrivateOrLoopbackIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}
	for _, block := range privateCIDRs {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}

// PermissionsConfig defines granular host capabilities requested by a plugin.
type PermissionsConfig struct {
	Network []string `yaml:"network,omitempty" json:"network,omitempty"` // Whitelisted domains, CIDRs, or "*"
	Storage []string `yaml:"storage,omitempty" json:"storage,omitempty"` // "local", "shared"
	Events  []string `yaml:"events,omitempty" json:"events,omitempty"`   // "on_track_change", "on_playback_change", "on_timer_tick"
}

// HasStorage returns true if the plugin requested local storage permission.
func (p PermissionsConfig) HasStorage() bool {
	for _, s := range p.Storage {
		if strings.ToLower(s) == "local" || s == "*" {
			return true
		}
	}
	return false
}

// HasEvent returns true if the plugin requested listening to a specific lifecycle event.
func (p PermissionsConfig) HasEvent(eventName string) bool {
	if len(p.Events) == 0 {
		return true // If not explicitly restricted, allow default events
	}
	for _, ev := range p.Events {
		if ev == "*" || strings.EqualFold(ev, eventName) {
			return true
		}
	}
	return false
}

// IsIPAllowed checks whether a resolved IP address is permitted under network capabilities.
func (p PermissionsConfig) IsIPAllowed(ip net.IP) bool {
	if ip == nil {
		return false
	}

	isPrivate := IsPrivateOrLoopbackIP(ip)

	// Check explicit CIDR and exact IP rules
	for _, rule := range p.Network {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}

		if strings.Contains(rule, "/") {
			_, ipNet, err := net.ParseCIDR(rule)
			if err == nil && ipNet.Contains(ip) {
				return true
			}
		} else if ruleIP := net.ParseIP(rule); ruleIP != nil && ruleIP.Equal(ip) {
			return true
		}
	}

	// Private, loopback, link-local, and cloud metadata IPs require an explicit IP or CIDR rule.
	if isPrivate {
		return false
	}

	// Public internet IPs are allowed if wildcard "*" is present or if domain rules are configured.
	for _, rule := range p.Network {
		rule = strings.TrimSpace(rule)
		if rule == "*" {
			return true
		}
		if !strings.Contains(rule, "/") && net.ParseIP(rule) == nil && rule != "" {
			return true
		}
	}

	return false
}

// CanAccessNetwork checks if a target URL/host is allowed under network permissions.
func (p PermissionsConfig) CanAccessNetwork(rawURL string) bool {
	if len(p.Network) == 0 {
		return false // Default deny
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	// Strictly allow only http and https protocols (no file://, gopher://, ftp://, unix://, etc.)
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}

	// Reject user info in URL (e.g. http://user:pass@host)
	if u.User != nil {
		return false
	}

	host := u.Hostname()
	if host == "" {
		return false
	}

	targetIP := net.ParseIP(host)

	// If host is an IP literal
	if targetIP != nil {
		isPrivate := IsPrivateOrLoopbackIP(targetIP)

		for _, rule := range p.Network {
			rule = strings.TrimSpace(rule)
			if rule == "" {
				continue
			}

			// Check CIDR match
			if strings.Contains(rule, "/") {
				_, ipNet, err := net.ParseCIDR(rule)
				if err == nil && ipNet.Contains(targetIP) {
					return true
				}
			} else if ruleIP := net.ParseIP(rule); ruleIP != nil && ruleIP.Equal(targetIP) {
				return true
			}
		}

		// Private/loopback IPs are ONLY allowed if explicitly matched above.
		if isPrivate {
			return false
		}

		// Public IP literal: allowed if wildcard "*" is present.
		for _, rule := range p.Network {
			if strings.TrimSpace(rule) == "*" {
				return true
			}
		}

		return false
	}

	// Host is a domain name
	hostLower := strings.ToLower(host)

	// Disallow localhost or local mDNS domains under wildcard unless explicitly granted
	isLocalHost := hostLower == "localhost" ||
		strings.HasSuffix(hostLower, ".localhost") ||
		strings.HasSuffix(hostLower, ".local") ||
		strings.HasSuffix(hostLower, ".internal")

	for _, rule := range p.Network {
		rule = strings.TrimSpace(rule)
		if rule == "" {
			continue
		}

		if rule == "*" {
			if !isLocalHost {
				return true
			}
			continue
		}

		// Check explicit localhost rule
		if isLocalHost && strings.EqualFold(hostLower, strings.ToLower(rule)) {
			return true
		}

		// Check exact host or wildcard domain match (*.discord.com or discord.com)
		if strings.HasPrefix(rule, "*.") {
			suffix := strings.ToLower(strings.TrimPrefix(rule, "*."))
			if hostLower == suffix || strings.HasSuffix(hostLower, "."+suffix) {
				return true
			}
		} else if strings.EqualFold(hostLower, strings.ToLower(rule)) {
			return true
		}
	}

	return false
}

// Manifest defines the plugin metadata and declared permissions.
type Manifest struct {
	ID          string            `yaml:"id" json:"id"`
	Name        string            `yaml:"name" json:"name"`
	Version     string            `yaml:"version" json:"version"`
	Author      string            `yaml:"author" json:"author"`
	Description string            `yaml:"description" json:"description"`
	Homepage    string            `yaml:"homepage,omitempty" json:"homepage,omitempty"`
	WasmFile    string            `yaml:"wasm_file,omitempty" json:"wasm_file,omitempty"`
	Permissions PermissionsConfig `yaml:"permissions" json:"permissions"`
}

// Validate verifies that the manifest contains required valid fields and safe paths.
func (m *Manifest) Validate() error {
	if m.ID == "" {
		return errors.New("plugin manifest ID is required")
	}
	if !validIDRegex.MatchString(m.ID) {
		return fmt.Errorf("plugin ID %q is invalid (must be lowercase alphanumeric, dashes, underscores)", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("plugin name is required")
	}
	if len(m.Name) > 100 {
		return fmt.Errorf("plugin name too long (%d chars, max 100)", len(m.Name))
	}
	if strings.TrimSpace(m.Version) == "" {
		return errors.New("plugin version is required")
	}
	if len(m.Version) > 32 {
		return fmt.Errorf("plugin version too long (%d chars, max 32)", len(m.Version))
	}
	if m.WasmFile == "" {
		m.WasmFile = "plugin.wasm"
	} else {
		clean := filepath.Clean(m.WasmFile)
		if filepath.IsAbs(m.WasmFile) ||
			strings.Contains(m.WasmFile, "..") ||
			strings.ContainsAny(m.WasmFile, "/\\") ||
			clean != m.WasmFile ||
			!strings.HasSuffix(strings.ToLower(m.WasmFile), ".wasm") {
			return fmt.Errorf("invalid wasm_file %q: must be a relative filename with .wasm extension without path traversal", m.WasmFile)
		}
	}
	return nil
}

// LoadManifest reads and parses a manifest.yaml or manifest.json file.
func LoadManifest(filePath string) (*Manifest, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	return ParseManifest(data, filepath.Ext(filePath))
}

// ParseManifest parses manifest bytes from YAML or JSON.
func ParseManifest(data []byte, ext string) (*Manifest, error) {
	var m Manifest
	var err error

	if strings.EqualFold(ext, ".json") {
		err = json.Unmarshal(data, &m)
	} else {
		err = yaml.Unmarshal(data, &m)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}

	if err := m.Validate(); err != nil {
		return nil, err
	}

	return &m, nil
}

// PluginState tracks the user configuration and permissions status for a plugin.
type PluginState struct {
	Enabled             bool              `json:"enabled"`
	PermissionsApproved bool              `json:"permissions_approved"`
	Config              map[string]string `json:"config,omitempty"`
	InstalledAt         string            `json:"installed_at,omitempty"`
	UpdatedAt           string            `json:"updated_at,omitempty"`
}

// PluginsConfigFile maps plugin ID to user plugin state.
type PluginsConfigFile struct {
	Plugins map[string]PluginState `json:"plugins"`
}

// PluginInfo provides a unified view of an installed plugin.
type PluginInfo struct {
	Manifest        Manifest    `json:"manifest"`
	State           PluginState `json:"state"`
	Dir             string      `json:"dir"`
	WasmPath        string      `json:"wasm_path"`
	HasValidBinary  bool        `json:"has_valid_binary"`
	IsLoaded        bool        `json:"is_loaded"`
	LastError       string      `json:"last_error,omitempty"`
	UpdateAvailable bool        `json:"update_available,omitempty"`
	LatestVersion   string      `json:"latest_version,omitempty"`
}

// RegistryIndex represents the official plugin catalog retrieved from the registry repo.
type RegistryIndex struct {
	Version   string           `json:"version"`
	UpdatedAt string           `json:"updated_at"`
	Plugins   []RegistryPlugin `json:"plugins"`
}

// RegistryPlugin represents a verified plugin available in the official registry.
type RegistryPlugin struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Author      string            `json:"author"`
	Description string            `json:"description"`
	Homepage    string            `json:"homepage,omitempty"`
	DownloadURL string            `json:"download_url"`
	Checksum    string            `json:"checksum"` // SHA256
	Signature   string            `json:"signature,omitempty"`
	Permissions PermissionsConfig `json:"permissions"`
	Tags        []string          `json:"tags,omitempty"`
}

// Event payloads passed to Wasm guest hooks

type TrackChangePayload struct {
	Station   string `json:"station"`
	Artist    string `json:"artist"`
	Title     string `json:"title"`
	Bitrate   int    `json:"bitrate"`
	Codec     string `json:"codec"`
	Timestamp string `json:"timestamp"`
}

type PlaybackChangePayload struct {
	Status  string `json:"status"` // "playing", "paused", "stopped", "buffering", "error"
	Volume  int    `json:"volume"`
	Backend string `json:"backend"`
	Station string `json:"station"`
}

type TimerTickPayload struct {
	Mode             string `json:"mode"`
	State            string `json:"state"`
	RemainingSeconds int    `json:"remaining_seconds"`
	TotalSeconds     int    `json:"total_seconds"`
}
