package plugin

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/halpworld/halpradio/pkg/util"
)

// Limits for manifest and registry text. Every string a plugin author or the
// registry controls is shown in the plugin manager and in the permission
// prompt, so it is rejected (never silently cleaned) when it contains control
// or format characters or exceeds these lengths.
const (
	MaxNameRunes        = 100
	MaxVersionLen       = 32
	MaxAuthorRunes      = 100
	MaxDescriptionRunes = 500
	MaxHomepageRunes    = 200
	MaxNetworkRules     = 32
	MaxNetworkRuleLen   = 253
	MaxEventEntries     = 8
	MaxStorageEntries   = 4
	MaxTags             = 16
	MaxTagRunes         = 32
)

// KnownEvents lists the lifecycle events a plugin may subscribe to.
var KnownEvents = []string{"on_track_change", "on_playback_change", "on_timer_tick"}

var (
	versionRegex  = regexp.MustCompile(`^v?[0-9]{1,9}(\.[0-9]{1,9}){0,3}([-+][0-9A-Za-z.+-]{1,24})?$`)
	hostRuleRegex = regexp.MustCompile(`^(?i)(\*\.)?[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)*$`)
	hexRegex      = regexp.MustCompile(`^[0-9a-fA-F]+$`)
)

func validateDisplayField(field, value string, maxRunes int, required bool) error {
	if required && strings.TrimSpace(value) == "" {
		return fmt.Errorf("plugin %s is required", field)
	}
	if n := utf8.RuneCountInString(value); n > maxRunes {
		return fmt.Errorf("plugin %s too long (%d chars, max %d)", field, n, maxRunes)
	}
	if !utf8.ValidString(value) || !util.IsDisplaySafe(value) {
		return fmt.Errorf("plugin %s contains control, format or extra whitespace characters", field)
	}
	return nil
}

func validateVersion(v string) error {
	if strings.TrimSpace(v) == "" {
		return errors.New("plugin version is required")
	}
	if len(v) > MaxVersionLen {
		return fmt.Errorf("plugin version too long (%d chars, max %d)", len(v), MaxVersionLen)
	}
	if !versionRegex.MatchString(v) {
		return fmt.Errorf("plugin version %q is invalid (expected a dotted numeric version such as 1.2.3)", v)
	}
	return nil
}

func validateHomepage(h string) error {
	if h == "" {
		return nil
	}
	if err := validateDisplayField("homepage", h, MaxHomepageRunes, false); err != nil {
		return err
	}
	u, err := url.Parse(h)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil {
		return fmt.Errorf("plugin homepage %q must be an http(s) URL", h)
	}
	return nil
}

// ValidNetworkRule reports whether rule is a syntactically valid network
// permission: exactly "*", an IP address, a CIDR block, or a host name with
// an optional "*." prefix. Ports, paths, schemes and whitespace are refused.
func ValidNetworkRule(rule string) bool {
	if rule == "" || len(rule) > MaxNetworkRuleLen {
		return false
	}
	if rule == "*" {
		return true
	}
	if n, ok := parseIPRule(rule); ok {
		// 0.0.0.0/8, :: and ::/96 are refused at dial time under every
		// rule, so a rule naming only them is not shown in a prompt.
		return !ruleOnlyUnspecified(n)
	}
	if strings.Contains(rule, "/") {
		return false // malformed CIDR
	}
	return hostRuleRegex.MatchString(rule)
}

// Validate checks every permission entry for syntax, known values, display
// safety and count limits.
func (p PermissionsConfig) Validate() error {
	if len(p.Network) > MaxNetworkRules {
		return fmt.Errorf("too many network rules (%d, max %d)", len(p.Network), MaxNetworkRules)
	}
	for _, r := range p.Network {
		if !util.IsDisplaySafe(r) || !ValidNetworkRule(r) {
			return fmt.Errorf("invalid network rule %q (expected \"*\", an IP, a CIDR or a host name)", r)
		}
	}
	if len(p.Storage) > MaxStorageEntries {
		return fmt.Errorf("too many storage entries (%d, max %d)", len(p.Storage), MaxStorageEntries)
	}
	for _, s := range p.Storage {
		if s != "local" && s != "*" {
			return fmt.Errorf("invalid storage permission %q (only \"local\" is supported)", s)
		}
	}
	if len(p.Events) > MaxEventEntries {
		return fmt.Errorf("too many event entries (%d, max %d)", len(p.Events), MaxEventEntries)
	}
	for _, e := range p.Events {
		if e == "*" {
			continue
		}
		known := false
		for _, k := range KnownEvents {
			if e == k {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("unknown event %q (allowed: %s)", e, strings.Join(KnownEvents, ", "))
		}
	}
	return nil
}

func normalizeList(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

// Normalized returns a copy with every list trimmed, lower-cased, sorted and
// de-duplicated, so two permission sets can be compared.
func (p PermissionsConfig) Normalized() PermissionsConfig {
	return PermissionsConfig{
		Network: normalizeList(p.Network),
		Storage: normalizeList(p.Storage),
		Events:  normalizeList(p.Events),
	}
}

// Equal reports whether p and o grant exactly the same permissions.
func (p PermissionsConfig) Equal(o PermissionsConfig) bool {
	a, b := p.Normalized(), o.Normalized()
	eq := func(x, y []string) bool {
		if len(x) != len(y) {
			return false
		}
		for i := range x {
			if x[i] != y[i] {
				return false
			}
		}
		return true
	}
	return eq(a.Network, b.Network) && eq(a.Storage, b.Storage) && eq(a.Events, b.Events)
}

// PermissionLine is one line of a permission prompt. Each network rule gets
// its own line so no entry can hide another.
type PermissionLine struct {
	Category string // "network", "storage" or "events"
	Rule     string // the raw rule ("" for summary lines)
	Text     string // plain-language description
	// Warning is true when the line grants access to this computer, the
	// local network, link-local / cloud-metadata or other non-public
	// addresses. UIs must render these lines prominently.
	Warning bool
}

// RuleReachesLocal reports whether a network rule grants access to loopback,
// private, link-local, metadata or other non-public addresses (or to host
// names such as localhost and *.local).
func RuleReachesLocal(rule string) bool {
	rule = strings.TrimSpace(rule)
	if n, ok := parseIPRule(rule); ok {
		return ruleIsLocal(n)
	}
	if rule == "*" {
		return false
	}
	h := strings.ToLower(strings.TrimPrefix(rule, "*."))
	return isLocalHostName(h) || h == "local" || h == "internal"
}

// LocalNetworkRules returns the network rules that reach non-public
// addresses (see RuleReachesLocal).
func (p PermissionsConfig) LocalNetworkRules() []string {
	var out []string
	for _, r := range p.Network {
		if RuleReachesLocal(r) {
			out = append(out, r)
		}
	}
	return out
}

func describeLocalRule(n *net.IPNet, rule string) string {
	ip := n.IP
	switch {
	case ip.IsLoopback():
		return fmt.Sprintf("WARNING: this computer (%s)", rule)
	case ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast():
		return fmt.Sprintf("WARNING: link-local / cloud metadata addresses (%s)", rule)
	case ip.IsPrivate():
		return fmt.Sprintf("WARNING: your local network (%s)", rule)
	default:
		return fmt.Sprintf("WARNING: non-public addresses (%s)", rule)
	}
}

// DescribeNetworkRule turns one network rule into a prompt line.
func DescribeNetworkRule(rule string) PermissionLine {
	line := PermissionLine{Category: "network", Rule: rule}
	switch n, ok := parseIPRule(rule); {
	case rule == "*":
		line.Text = "Any public internet host (this computer and local network blocked)"
	case ok && ruleIsLocal(n):
		line.Text = describeLocalRule(n, rule)
		line.Warning = true
	case ok:
		line.Text = fmt.Sprintf("Public addresses in %s (this computer and local network blocked)", rule)
	case RuleReachesLocal(rule):
		line.Text = fmt.Sprintf("WARNING: local host name %s", rule)
		line.Warning = true
	case strings.HasPrefix(rule, "*."):
		line.Text = fmt.Sprintf("%s and all its subdomains", strings.TrimPrefix(rule, "*."))
	default:
		line.Text = rule
	}
	return line
}

// Summary returns the permission prompt lines for p, one per network rule,
// followed by storage and events. Use it in every place that asks the user
// to approve a plugin (TUI modal and CLI) so both show the same thing.
func (p PermissionsConfig) Summary() []PermissionLine {
	var lines []PermissionLine
	if len(p.Network) == 0 {
		lines = append(lines, PermissionLine{Category: "network", Text: "No network access"})
	}
	for _, r := range p.Network {
		lines = append(lines, DescribeNetworkRule(r))
	}
	if p.HasStorage() {
		lines = append(lines, PermissionLine{Category: "storage", Text: "Private key-value storage for this plugin"})
	} else {
		lines = append(lines, PermissionLine{Category: "storage", Text: "No storage"})
	}
	if len(p.Events) == 0 {
		lines = append(lines, PermissionLine{Category: "events", Text: "All playback events: " + strings.Join(KnownEvents, ", ")})
	} else {
		lines = append(lines, PermissionLine{Category: "events", Text: "Events: " + strings.Join(p.Events, ", ")})
	}
	return lines
}

// PermissionChanges lists the network, storage and event entries in current
// that are not in approved, for showing "new since you approved" in a
// re-approval prompt. A nil approved means nothing was approved before.
func PermissionChanges(approved *PermissionsConfig, current PermissionsConfig) []string {
	cur := current.Normalized()
	var old PermissionsConfig
	if approved != nil {
		old = approved.Normalized()
	}
	diff := func(kind string, newL, oldL []string) []string {
		have := make(map[string]bool, len(oldL))
		for _, v := range oldL {
			have[v] = true
		}
		var out []string
		for _, v := range newL {
			if !have[v] {
				out = append(out, kind+": "+v)
			}
		}
		return out
	}
	var out []string
	out = append(out, diff("network", cur.Network, old.Network)...)
	out = append(out, diff("storage", cur.Storage, old.Storage)...)
	out = append(out, diff("events", cur.Events, old.Events)...)
	return out
}

// CompareVersions compares two plugin versions (dotted numeric with an
// optional pre-release suffix). It returns -1, 0 or 1, or an error when
// either version cannot be parsed.
func CompareVersions(a, b string) (int, error) {
	pa, prea, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	pb, preb, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	for i := 0; i < 4; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1, nil
			}
			return 1, nil
		}
	}
	switch {
	case prea == preb:
		return 0, nil
	case prea == "":
		return 1, nil
	case preb == "":
		return -1, nil
	case prea < preb:
		return -1, nil
	default:
		return 1, nil
	}
}

func parseVersion(v string) ([4]int, string, error) {
	var out [4]int
	if !versionRegex.MatchString(v) {
		return out, "", fmt.Errorf("invalid version %q", v)
	}
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexByte(v, '+'); i >= 0 {
		v = v[:i] // build metadata does not affect ordering
	}
	pre := ""
	if i := strings.IndexByte(v, '-'); i >= 0 {
		v, pre = v[:i], v[i+1:]
	}
	for i, part := range strings.Split(v, ".") {
		n, err := strconv.Atoi(part)
		if err != nil {
			return out, "", fmt.Errorf("invalid version %q", v)
		}
		out[i] = n
	}
	return out, pre, nil
}

// ValidateRegistryPlugin checks a registry entry before it is displayed or
// installed: the same rules as a manifest, plus checksum and signature
// format, an https download URL, and safe tags.
func ValidateRegistryPlugin(p RegistryPlugin) error {
	m := p.toManifest()
	if err := m.Validate(); err != nil {
		return err
	}
	cs := strings.TrimSpace(p.Checksum)
	if len(cs) != 64 || !hexRegex.MatchString(cs) {
		return fmt.Errorf("plugin %s: checksum must be 64 hex characters", p.ID)
	}
	if p.Signature != "" && (len(p.Signature) != 128 || !hexRegex.MatchString(p.Signature)) {
		return fmt.Errorf("plugin %s: signature must be 128 hex characters", p.ID)
	}
	if len(p.DownloadURL) > 2048 || !util.IsDisplaySafe(p.DownloadURL) {
		return fmt.Errorf("plugin %s: invalid download URL", p.ID)
	}
	if len(p.Tags) > MaxTags {
		return fmt.Errorf("plugin %s: too many tags", p.ID)
	}
	for _, t := range p.Tags {
		if err := validateDisplayField("tag", t, MaxTagRunes, true); err != nil {
			return fmt.Errorf("plugin %s: %w", p.ID, err)
		}
	}
	return nil
}

func (p RegistryPlugin) toManifest() Manifest {
	return Manifest{
		ID:          p.ID,
		Name:        p.Name,
		Version:     p.Version,
		Author:      p.Author,
		Description: p.Description,
		Homepage:    p.Homepage,
		WasmFile:    "plugin.wasm",
		Permissions: p.Permissions,
	}
}
