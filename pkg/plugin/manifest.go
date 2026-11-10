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
		"192.88.99.0/24",  // 6to4 relay anycast (deprecated)
		"::/96",           // IPv4-compatible IPv6 (deprecated; also covers :: and ::1)
		"64:ff9b:1::/48",  // Local-use NAT64 (RFC 8215): translator reaches internal IPv4
		"fec0::/10",       // IPv6 site-local (deprecated, still routed on some LANs)
		"2001::/32",       // Teredo: tunnels to an embedded IPv4 server/client
		"100::/64",        // IPv6 discard-only
		"2001:db8::/32",   // IPv6 documentation
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
	// IPv6 forms that carry an IPv4 address the network may translate or
	// tunnel to: judge them by the embedded IPv4 address. (IPv4-mapped
	// ::ffff:a.b.c.d is already handled by net.IP's To4 above, and
	// IPv4-compatible ::a.b.c.d is refused outright by ::/96.)
	if v4 := embeddedIPv4(ip); v4 != nil {
		return IsPrivateOrLoopbackIP(v4)
	}
	return false
}

var (
	nat64WellKnown = mustCIDR("64:ff9b::/96") // RFC 6052: last 32 bits
	sixToFour      = mustCIDR("2002::/16")    // RFC 3056: bits 16-47
)

// mustCIDR parses a constant prefix. It cannot fail for the literals
// above; a nil result would only make embeddedIPv4 skip that form.
func mustCIDR(s string) *net.IPNet {
	_, n, _ := net.ParseCIDR(s)
	return n
}

// embeddedIPv4 returns the IPv4 address embedded in a NAT64 well-known
// prefix or 6to4 address, or nil. A public NAT64 target (what DNS64 returns
// for a public host on an IPv6-only network) stays reachable; one that wraps
// a loopback, private or metadata address is refused.
func embeddedIPv4(ip net.IP) net.IP {
	if ip.To4() != nil {
		return nil
	}
	ip16 := ip.To16()
	if ip16 == nil {
		return nil
	}
	switch {
	case nat64WellKnown != nil && nat64WellKnown.Contains(ip16):
		return net.IPv4(ip16[12], ip16[13], ip16[14], ip16[15]).To4()
	case sixToFour != nil && sixToFour.Contains(ip16):
		return net.IPv4(ip16[2], ip16[3], ip16[4], ip16[5]).To4()
	}
	return nil
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

// ruleIsLocal reports whether a CIDR rule lies entirely inside one of the
// private, loopback, link-local or otherwise non-public blocks. Only such a
// rule names local addresses "explicitly and narrowly"; a broad rule that
// merely contains them (0.0.0.0/0, ::/0, 0.0.0.0/1) does not.
func ruleIsLocal(n *net.IPNet) bool {
	if n == nil {
		return false
	}
	ones, bits := n.Mask.Size()
	for _, block := range privateCIDRs {
		bOnes, bBits := block.Mask.Size()
		if bits != bBits || ones < bOnes {
			continue
		}
		if block.Contains(n.IP) {
			return true
		}
	}
	return false
}

// parseIPRule parses an IP or CIDR network rule. ok is false for "*" and
// host-name rules.
func parseIPRule(rule string) (n *net.IPNet, ok bool) {
	if strings.Contains(rule, "/") {
		_, ipNet, err := net.ParseCIDR(rule)
		if err != nil {
			return nil, false
		}
		return ipNet, true
	}
	ip := net.ParseIP(rule)
	if ip == nil {
		return nil, false
	}
	if v4 := ip.To4(); v4 != nil {
		return &net.IPNet{IP: v4, Mask: net.CIDRMask(32, 32)}, true
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(128, 128)}, true
}

// IsUnspecifiedRangeIP reports whether ip is in 0.0.0.0/8, is the IPv6
// unspecified address ::, or is an IPv4-compatible address (::/96). A
// connection to 0.0.0.0 or :: reaches this computer on Linux and macOS, so
// these are refused at dial time under every rule, and a rule that names
// only such addresses is rejected when the manifest is validated (FR-C-6).
func IsUnspecifiedRangeIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if v4 := ip.To4(); v4 != nil {
		return v4[0] == 0
	}
	ip16 := ip.To16()
	if ip16 == nil {
		return true
	}
	if ip16.Equal(net.IPv6loopback) {
		return false // ::1 is loopback, allowed by an explicit narrow rule
	}
	for _, b := range ip16[:12] {
		if b != 0 {
			return false
		}
	}
	return true // :: and IPv4-compatible ::a.b.c.d
}

// ruleOnlyUnspecified reports whether a CIDR rule lies inside 0.0.0.0/8 or
// ::/96 (other than the exact rule ::1), so it could never grant anything.
func ruleOnlyUnspecified(n *net.IPNet) bool {
	ones, bits := n.Mask.Size()
	if bits == 32 {
		return ones >= 8 && n.IP.To4() != nil && n.IP.To4()[0] == 0
	}
	if v4 := n.IP.To4(); v4 != nil { // IPv4-mapped prefix ::ffff:a.b.c.d/k
		return ones >= 104 && v4[0] == 0
	}
	if ones < 96 {
		return false
	}
	ip16 := n.IP.To16()
	for _, b := range ip16[:12] {
		if b != 0 {
			return false
		}
	}
	// Inside ::/96 only the exact loopback rule ::1 names something
	// reachable; wider rules such as ::/96 are refused rather than shown
	// as a roundabout loopback grant.
	return !(ones == 128 && ip16.Equal(net.IPv6loopback))
}

// ipRuleAllows reports whether an IP or CIDR rule grants access to ip. A
// private, loopback, link-local or metadata target is granted only by a rule
// that itself lies inside such a block, so 0.0.0.0/0 grants public
// addresses only, exactly like "*".
func ipRuleAllows(rule string, ip net.IP) bool {
	if IsUnspecifiedRangeIP(ip) {
		return false // never reachable, whatever the rule says
	}
	n, ok := parseIPRule(rule)
	if !ok || !n.Contains(ip) {
		return false
	}
	if IsPrivateOrLoopbackIP(ip) {
		return ruleIsLocal(n)
	}
	return true
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
		if ipRuleAllows(rule, ip) {
			return true
		}
	}

	// Private, loopback, link-local, and cloud metadata IPs require an explicit, narrow IP or CIDR rule.
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
		for _, rule := range p.Network {
			rule = strings.TrimSpace(rule)
			if rule == "" {
				continue
			}
			if ipRuleAllows(rule, targetIP) {
				return true
			}
		}

		// Private/loopback IPs are ONLY allowed if explicitly and narrowly matched above.
		if IsPrivateOrLoopbackIP(targetIP) {
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
	isLocalHost := isLocalHostName(hostLower)

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

func isLocalHostName(hostLower string) bool {
	hostLower = strings.TrimSuffix(hostLower, ".")
	return hostLower == "localhost" ||
		strings.HasSuffix(hostLower, ".localhost") ||
		strings.HasSuffix(hostLower, ".local") ||
		strings.HasSuffix(hostLower, ".internal")
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
	if err := validateDisplayField("name", m.Name, MaxNameRunes, true); err != nil {
		return err
	}
	if err := validateVersion(m.Version); err != nil {
		return err
	}
	if err := validateDisplayField("author", m.Author, MaxAuthorRunes, false); err != nil {
		return err
	}
	if err := validateDisplayField("description", m.Description, MaxDescriptionRunes, false); err != nil {
		return err
	}
	if err := validateHomepage(m.Homepage); err != nil {
		return err
	}
	if err := m.Permissions.Validate(); err != nil {
		return err
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
//
// An approval is bound to the exact plugin binary (ApprovedSHA256) and the
// exact permission set (ApprovedPermissions) the user was shown. If either
// differs from what is on disk, the approval is void and the plugin does not
// start until it is approved again.
type PluginState struct {
	Enabled             bool               `json:"enabled"`
	PermissionsApproved bool               `json:"permissions_approved"`
	ApprovedSHA256      string             `json:"approved_sha256,omitempty"`
	ApprovedPermissions *PermissionsConfig `json:"approved_permissions,omitempty"`
	Config              map[string]string  `json:"config,omitempty"`
	InstalledAt         string             `json:"installed_at,omitempty"`
	UpdatedAt           string             `json:"updated_at,omitempty"`
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
	// WasmSHA256 is the SHA-256 of plugin.wasm as last read from disk. Pass
	// the PluginInfo you displayed to ApprovePermissionsFor so the approval
	// only applies to this exact binary and permission set.
	WasmSHA256 string `json:"wasm_sha256,omitempty"`
	// ApprovalNote explains why a plugin needs (re-)approval, for example
	// "the plugin binary changed since you approved it". Empty when approved.
	ApprovalNote string `json:"approval_note,omitempty"`
}

// RegistryIndex represents the official plugin catalog retrieved from the registry repo.
type RegistryIndex struct {
	Version   string           `json:"version"`
	UpdatedAt string           `json:"updated_at"`
	Plugins   []RegistryPlugin `json:"plugins"`
	// Skipped counts entries FetchRegistry dropped because they failed
	// validation or repeated an ID.
	Skipped int `json:"-"`
}

// RegistryPlugin is one entry of the plugin registry. Entries are validated
// by FetchRegistry (display strings, permissions, checksum, https download
// URL); the checksum comes from the same document, so it proves integrity of
// the download, not who published it. See Signature.
type RegistryPlugin struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Author      string            `json:"author"`
	Description string            `json:"description"`
	Homepage    string            `json:"homepage,omitempty"`
	DownloadURL string            `json:"download_url"`
	Checksum    string            `json:"checksum"`            // SHA256
	Signature   string            `json:"signature,omitempty"` // hex Ed25519 over SigningPayload()
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
