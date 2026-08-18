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

// CanAccessNetwork checks if a target URL/host is allowed under network permissions.
func (p PermissionsConfig) CanAccessNetwork(rawURL string) bool {
	if len(p.Network) == 0 {
		return false // Default deny
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	host := u.Hostname()
	if host == "" {
		host = rawURL
	}

	targetIP := net.ParseIP(host)

	for _, rule := range p.Network {
		rule = strings.TrimSpace(rule)
		if rule == "*" {
			return true
		}

		// Check CIDR / IP match
		if strings.Contains(rule, "/") {
			_, ipNet, err := net.ParseCIDR(rule)
			if err == nil && targetIP != nil && ipNet.Contains(targetIP) {
				return true
			}
		} else if targetIP != nil && rule == host {
			return true
		}

		// Check exact host or wildcard domain match (*.discord.com or discord.com)
		if strings.HasPrefix(rule, "*.") {
			suffix := strings.TrimPrefix(rule, "*.")
			if host == suffix || strings.HasSuffix(host, "."+suffix) {
				return true
			}
		} else if strings.EqualFold(host, rule) {
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

// Validate verifies that the manifest contains required valid fields.
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
	if strings.TrimSpace(m.Version) == "" {
		return errors.New("plugin version is required")
	}
	if m.WasmFile == "" {
		m.WasmFile = "plugin.wasm"
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
