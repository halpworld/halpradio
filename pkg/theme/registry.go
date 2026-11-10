package theme

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"

	"github.com/halpworld/halpradio/pkg/util"
)

const (
	// DefaultThemeRegistryURL points to the official themes repository index.
	DefaultThemeRegistryURL = "https://raw.githubusercontent.com/halpworld/halpradio-themes/main/themes.json"
)

var validIDRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// RegistryTheme represents a downloadable theme from the remote themes repository.
type RegistryTheme struct {
	ID          string         `json:"id" yaml:"id"`
	Name        string         `json:"name" yaml:"name"`
	Author      string         `json:"author" yaml:"author"`
	Description string         `json:"description" yaml:"description"`
	Category    string         `json:"category,omitempty" yaml:"category,omitempty"`
	DownloadURL string         `json:"download_url,omitempty" yaml:"download_url,omitempty"`
	Primary     lipgloss.Color `json:"primary" yaml:"primary"`
	Secondary   lipgloss.Color `json:"secondary" yaml:"secondary"`
	Background  lipgloss.Color `json:"background" yaml:"background"`
	Foreground  lipgloss.Color `json:"foreground" yaml:"foreground"`
	Muted       lipgloss.Color `json:"muted" yaml:"muted"`
	Playing     lipgloss.Color `json:"playing" yaml:"playing"`
	Favorite    lipgloss.Color `json:"favorite" yaml:"favorite"`
	Border      lipgloss.Color `json:"border" yaml:"border"`
	Highlight   lipgloss.Color `json:"highlight" yaml:"highlight"`
	Badge       lipgloss.Color `json:"badge" yaml:"badge"`
	BadgeText   lipgloss.Color `json:"badge_text" yaml:"badge_text"`
	HeaderAscii lipgloss.Color `json:"header_ascii" yaml:"header_ascii"`
}

// ToTheme converts a RegistryTheme to an in-memory runtime Theme struct.
func (r RegistryTheme) ToTheme() Theme {
	th := Theme{
		ID:          r.ID,
		Name:        r.Name,
		Author:      r.Author,
		Description: r.Description,
		Primary:     r.Primary,
		Secondary:   r.Secondary,
		Background:  r.Background,
		Foreground:  r.Foreground,
		Muted:       r.Muted,
		Playing:     r.Playing,
		Favorite:    r.Favorite,
		Border:      r.Border,
		Highlight:   r.Highlight,
		Badge:       r.Badge,
		BadgeText:   r.BadgeText,
		HeaderAscii: r.HeaderAscii,
		IsCustom:    true,
	}
	return sanitizeThemeTokens(th)
}

// RegistryIndex represents the parsed themes.json format from the remote themes repo.
type RegistryIndex struct {
	Version   string          `json:"version"`
	UpdatedAt string          `json:"updated_at"`
	Themes    []RegistryTheme `json:"themes"`
}

// RegistryClient fetches remote themes, previews them, and downloads them to disk.
type RegistryClient struct {
	httpClient  *http.Client
	registryURL string

	// allowPrivateHosts disables the loopback/private/link-local address
	// guard on theme downloads. Only tests set it (httptest listens on
	// 127.0.0.1).
	allowPrivateHosts bool
}

const (
	// maxRegistryThemes caps how many registry entries are kept, matching
	// MaxCustomThemes.
	maxRegistryThemes = MaxCustomThemes
	// maxThemeDownloadRedirects bounds redirect chains on download_url.
	maxThemeDownloadRedirects = 3
)

// NewRegistryClient creates a new RegistryClient instance.
func NewRegistryClient(registryURL string) *RegistryClient {
	if strings.TrimSpace(registryURL) == "" {
		registryURL = DefaultThemeRegistryURL
	}
	return &RegistryClient{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		registryURL: registryURL,
	}
}

// FetchRegistry downloads and parses the latest themes from the repository index.
// If the network request fails, it falls back to the curated built-in offline registry.
func (c *RegistryClient) FetchRegistry(ctx context.Context) (*RegistryIndex, error) {
	if err := checkRegistryIndexURL(c.registryURL); err != nil {
		return c.fallbackRegistry(), err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.registryURL, nil)
	if err != nil {
		return c.fallbackRegistry(), err
	}
	req.Header.Set("User-Agent", "halpradio/theme-manager")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return c.fallbackRegistry(), fmt.Errorf("failed to fetch theme registry: %w", err)
	}
	defer resp.Body.Close()

	if resp.Request != nil && resp.Request.URL != nil {
		// A redirect must not downgrade the index to plain http either.
		if err := checkRegistryIndexURL(resp.Request.URL.String()); err != nil {
			return c.fallbackRegistry(), err
		}
	}
	if resp.StatusCode != http.StatusOK {
		return c.fallbackRegistry(), fmt.Errorf("theme registry returned HTTP %d", resp.StatusCode)
	}

	const maxRegistryBytes = 5 * 1024 * 1024 // 5 MB limit
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxRegistryBytes))
	if err != nil {
		return c.fallbackRegistry(), fmt.Errorf("failed to read theme registry response: %w", err)
	}

	// Try parsing standard RegistryIndex wrapper
	var index RegistryIndex
	if err := json.Unmarshal(data, &index); err == nil && len(index.Themes) > 0 {
		c.sanitizeRegistryThemes(&index)
		return &index, nil
	}

	// Try parsing direct array of RegistryTheme
	var themeList []RegistryTheme
	if err := json.Unmarshal(data, &themeList); err == nil && len(themeList) > 0 {
		index = RegistryIndex{
			Version:   "1.0.0",
			UpdatedAt: time.Now().UTC().Format(time.RFC3339),
			Themes:    themeList,
		}
		c.sanitizeRegistryThemes(&index)
		return &index, nil
	}

	return c.fallbackRegistry(), fmt.Errorf("failed to parse theme registry JSON")
}

// checkRegistryIndexURL requires the theme index (theme_registry_url) to be
// https, as download_url already is: over plain http anyone on the network
// path could replace the list of themes. Plain http is accepted only for a
// loopback host (a registry served from this machine).
func checkRegistryIndexURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return fmt.Errorf("theme registry URL is not a valid URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if strings.EqualFold(host, "localhost") {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
	}
	return fmt.Errorf("theme registry URL must use https (got %s://%s)", u.Scheme, util.SanitizeDisplay(u.Host, 100))
}

// sanitizeRegistryThemes makes every registry entry safe to display and
// install: colours are validated, text fields go through the shared terminal
// sanitizer, and the ID must be a valid slug that does not shadow a built-in
// theme. Entries that fail are dropped, and at most maxRegistryThemes are kept.
func (c *RegistryClient) sanitizeRegistryThemes(index *RegistryIndex) {
	kept := make([]RegistryTheme, 0, min(len(index.Themes), maxRegistryThemes))
	seen := make(map[string]bool)
	for i := range index.Themes {
		if len(kept) >= maxRegistryThemes {
			break
		}
		t := index.Themes[i]
		// The ID must already be a slug (case-insensitive); it is not
		// rewritten into one, so "evil\x1b[2J" is dropped rather than
		// silently becoming "evil-2j". Only an absent ID is derived from
		// the name.
		var id string
		if raw := strings.ToLower(strings.TrimSpace(t.ID)); raw != "" {
			id = raw
		} else {
			id = normalizeID(util.SanitizeDisplay(t.Name, 128))
		}
		if !validIDRegex.MatchString(id) || isBuiltinThemeID(id) || seen[id] {
			continue
		}
		seen[id] = true
		t.ID = id

		th := t.ToTheme()
		t.Primary = th.Primary
		t.Secondary = th.Secondary
		t.Background = th.Background
		t.Foreground = th.Foreground
		t.Muted = th.Muted
		t.Playing = th.Playing
		t.Favorite = th.Favorite
		t.Border = th.Border
		t.Highlight = th.Highlight
		t.Badge = th.Badge
		t.BadgeText = th.BadgeText
		t.HeaderAscii = th.HeaderAscii

		t.Name = sanitizeString(t.Name, 64)
		if t.Name == "" {
			t.Name = prettifyName(id)
		}
		t.Author = sanitizeString(t.Author, 64)
		t.Category = sanitizeString(t.Category, 32)
		t.Description = sanitizeString(t.Description, 256)
		t.DownloadURL = strings.TrimSpace(t.DownloadURL)
		if !util.IsDisplaySafe(t.DownloadURL) || len(t.DownloadURL) > 2048 {
			t.DownloadURL = ""
		}
		kept = append(kept, t)
	}
	index.Themes = kept
	index.Version = sanitizeString(index.Version, 32)
	index.UpdatedAt = sanitizeString(index.UpdatedAt, 64)
}

// DownloadAndInstall downloads or writes a theme into the themesDir and registers it.
//
// When the registry entry has a download_url, it must be https (relative
// references resolve against the registry URL), redirects may not leave https
// or reach loopback/private/link-local addresses, and the body is parsed and
// validated as a theme; the file written is the re-serialised, sanitized
// theme, never the downloaded bytes. Built-in theme IDs cannot be replaced.
// Files are written atomically with mode 0600.
func (c *RegistryClient) DownloadAndInstall(ctx context.Context, th RegistryTheme, themesDir string) error {
	rawID := strings.TrimSpace(th.ID)
	if rawID == "" {
		rawID = th.Name
	}
	if strings.Contains(th.ID, "..") || strings.Contains(th.ID, "/") || strings.Contains(th.ID, "\\") {
		return fmt.Errorf("invalid theme ID %q (path traversal characters not allowed)", util.SanitizeDisplay(th.ID, 64))
	}
	normID := normalizeID(rawID)
	if normID == "" || !validIDRegex.MatchString(normID) {
		return fmt.Errorf("invalid theme ID %q", util.SanitizeDisplay(th.ID, 64))
	}
	if isBuiltinThemeID(normID) {
		return fmt.Errorf("theme ID %q is a built-in theme and cannot be replaced", normID)
	}

	runtimeTheme := th.ToTheme()
	if strings.TrimSpace(th.DownloadURL) != "" {
		downloaded, err := c.downloadThemeFile(ctx, th.DownloadURL)
		if err != nil {
			return fmt.Errorf("download theme %q: %w", normID, err)
		}
		if downloaded.Name == "" {
			downloaded.Name = runtimeTheme.Name
		}
		if downloaded.Author == "" {
			downloaded.Author = runtimeTheme.Author
		}
		if downloaded.Description == "" {
			downloaded.Description = runtimeTheme.Description
		}
		runtimeTheme = downloaded
	}
	runtimeTheme.ID = normID
	runtimeTheme.IsCustom = true
	runtimeTheme = sanitizeThemeTokens(runtimeTheme)

	if err := os.MkdirAll(themesDir, 0700); err != nil {
		return fmt.Errorf("failed to create themes directory: %w", err)
	}
	targetPath := filepath.Join(themesDir, fmt.Sprintf("%s.yaml", normID))
	if err := util.WriteFileAtomic(targetPath, []byte(FormatThemeYAML(runtimeTheme)), 0o600); err != nil {
		return fmt.Errorf("failed to save theme file: %w", err)
	}

	RegisterTheme(normID, runtimeTheme)
	return nil
}

// downloadThemeFile fetches a theme YAML from a registry download_url and
// returns it parsed and validated.
func (c *RegistryClient) downloadThemeFile(ctx context.Context, rawURL string) (Theme, error) {
	base, err := url.Parse(c.registryURL)
	if err != nil {
		return Theme{}, fmt.Errorf("invalid registry URL")
	}
	ref, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return Theme{}, fmt.Errorf("invalid download_url")
	}
	u := base.ResolveReference(ref)
	if err := c.checkThemeDownloadURL(u); err != nil {
		return Theme{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Theme{}, err
	}
	req.Header.Set("User-Agent", "halpradio/theme-installer")
	resp, err := c.themeDownloadClient().Do(req)
	if err != nil {
		return Theme{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Theme{}, fmt.Errorf("theme download returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxThemeFileSize+1))
	if err != nil {
		return Theme{}, err
	}
	if len(data) > MaxThemeFileSize {
		return Theme{}, fmt.Errorf("theme file exceeds %d bytes", MaxThemeFileSize)
	}
	return parseThemeYAML(data)
}

// parseThemeYAML parses a downloaded theme and requires it to look like one:
// valid YAML into the Theme schema with valid primary, background and
// foreground colours.
func parseThemeYAML(data []byte) (Theme, error) {
	if len(data) == 0 {
		return Theme{}, errors.New("empty theme file")
	}
	var th Theme
	if err := yaml.Unmarshal(data, &th); err != nil {
		return Theme{}, fmt.Errorf("theme file is not valid YAML")
	}
	for _, c := range []lipgloss.Color{th.Primary, th.Background, th.Foreground} {
		if !isValidColor(string(c)) {
			return Theme{}, errors.New("theme file is missing valid primary/background/foreground colours")
		}
	}
	return sanitizeThemeTokens(th), nil
}

func (c *RegistryClient) checkThemeDownloadURL(u *url.URL) error {
	if u == nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("theme download_url must be https")
	}
	if u.User != nil {
		return errors.New("theme download_url must not contain credentials")
	}
	if c.allowPrivateHosts {
		return nil
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errors.New("theme download_url may not point at a local address")
	}
	if ip := net.ParseIP(host); ip != nil && isNonPublicIP(ip) {
		return errors.New("theme download_url may not point at a local or private address")
	}
	return nil
}

// themeDownloadClient returns a client for download_url fetches: redirects are
// re-checked with checkThemeDownloadURL (https only, no local/private hosts),
// and unless a proxy is configured the dialer refuses non-public addresses
// so a public hostname that resolves to 127.0.0.1 or 10.x is refused too.
func (c *RegistryClient) themeDownloadClient() *http.Client {
	base := c.httpClient
	if base == nil {
		base = &http.Client{Timeout: 10 * time.Second}
	}
	cl := *base
	if !c.allowPrivateHosts {
		dialer := &net.Dialer{Timeout: 10 * time.Second, Control: refuseNonPublicDial}
		cl.Transport = &http.Transport{
			Proxy:               http.ProxyFromEnvironment,
			DialContext:         dialer.DialContext,
			TLSHandshakeTimeout: 10 * time.Second,
			ForceAttemptHTTP2:   true,
		}
	}
	cl.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxThemeDownloadRedirects {
			return errors.New("too many redirects")
		}
		return c.checkThemeDownloadURL(req.URL)
	}
	return &cl
}

// refuseNonPublicDial is a net.Dialer Control hook that blocks connections to
// loopback, private, link-local, unspecified and multicast addresses. With an
// HTTP(S) proxy configured the dial goes to the proxy, which is allowed.
func refuseNonPublicDial(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("refusing to dial unresolved address %q", address)
	}
	if isNonPublicIP(ip) && !isConfiguredProxy(address) {
		return fmt.Errorf("refusing to connect to non-public address %s", ip)
	}
	return nil
}

func isNonPublicIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

// isConfiguredProxy reports whether address is the host:port of a proxy from
// the environment, so a local corporate proxy keeps working.
func isConfiguredProxy(address string) bool {
	for _, env := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy", "ALL_PROXY", "all_proxy"} {
		v := strings.TrimSpace(os.Getenv(env))
		if v == "" {
			continue
		}
		pu, err := url.Parse(v)
		if err != nil || pu.Host == "" {
			continue
		}
		port := pu.Port()
		if port == "" {
			port = "80"
			if pu.Scheme == "https" {
				port = "443"
			}
		}
		if addrs, err := net.LookupHost(pu.Hostname()); err == nil {
			for _, a := range addrs {
				if net.JoinHostPort(a, port) == address {
					return true
				}
			}
		}
	}
	return false
}

// Uninstall removes a custom theme YAML file from disk and unregisters it.
// Built-in themes cannot be uninstalled.
func (c *RegistryClient) Uninstall(id string, themesDir string) error {
	if strings.Contains(id, "..") || strings.Contains(id, "/") || strings.Contains(id, "\\") {
		return fmt.Errorf("invalid theme ID %q (path traversal characters not allowed)", util.SanitizeDisplay(id, 64))
	}
	normID := normalizeID(id)
	if normID == "" || !validIDRegex.MatchString(normID) {
		return fmt.Errorf("invalid theme ID %q", util.SanitizeDisplay(id, 64))
	}
	if isBuiltinThemeID(normID) {
		return fmt.Errorf("theme %q is built in and cannot be uninstalled", normID)
	}

	yamlPath := filepath.Join(themesDir, fmt.Sprintf("%s.yaml", normID))
	ymlPath := filepath.Join(themesDir, fmt.Sprintf("%s.yml", normID))

	_ = os.Remove(yamlPath)
	_ = os.Remove(ymlPath)

	UnregisterTheme(normID)
	return nil
}

// fallbackRegistry provides offline / bundled known community themes from halpradio-themes.
func (c *RegistryClient) fallbackRegistry() *RegistryIndex {
	return &RegistryIndex{
		Version:   "1.0.0",
		UpdatedAt: "2026-08-20T00:00:00Z",
		Themes: []RegistryTheme{
			{
				ID:          "rose-pine",
				Name:        "Rosé Pine",
				Author:      "mvllow",
				Description: "All natural pine, faux fur and delicate warmth with soft muted pastels",
				Category:    "Pastel / Warm",
				Primary:     lipgloss.Color("#eb6f92"),
				Secondary:   lipgloss.Color("#f6c177"),
				Background:  lipgloss.Color("#191724"),
				Foreground:  lipgloss.Color("#e0def4"),
				Muted:       lipgloss.Color("#6e6a86"),
				Playing:     lipgloss.Color("#9ccfd8"),
				Favorite:    lipgloss.Color("#eb6f92"),
				Border:      lipgloss.Color("#26233a"),
				Highlight:   lipgloss.Color("#31748f"),
				Badge:       lipgloss.Color("#eb6f92"),
				BadgeText:   lipgloss.Color("#191724"),
				HeaderAscii: lipgloss.Color("#c4a7e7"),
			},
			{
				ID:          "everforest",
				Name:        "Everforest Dark",
				Author:      "sainnhe",
				Description: "Comfortable green-based natural theme designed to be easy on the eyes",
				Category:    "Nature / Green",
				Primary:     lipgloss.Color("#a7c080"),
				Secondary:   lipgloss.Color("#dbbc7f"),
				Background:  lipgloss.Color("#2d353b"),
				Foreground:  lipgloss.Color("#d3c6aa"),
				Muted:       lipgloss.Color("#859289"),
				Playing:     lipgloss.Color("#a7c080"),
				Favorite:    lipgloss.Color("#e67e80"),
				Border:      lipgloss.Color("#475258"),
				Highlight:   lipgloss.Color("#7fbbb3"),
				Badge:       lipgloss.Color("#a7c080"),
				BadgeText:   lipgloss.Color("#232a2e"),
				HeaderAscii: lipgloss.Color("#7fbbb3"),
			},
			{
				ID:          "kanagawa",
				Name:        "Kanagawa",
				Author:      "rebelot",
				Description: "Elegantly balanced palette inspired by traditional Japanese paintings",
				Category:    "Dark / Elegant",
				Primary:     lipgloss.Color("#7e9cd8"),
				Secondary:   lipgloss.Color("#957fb8"),
				Background:  lipgloss.Color("#1f1f28"),
				Foreground:  lipgloss.Color("#dcd7ba"),
				Muted:       lipgloss.Color("#727169"),
				Playing:     lipgloss.Color("#98bb6c"),
				Favorite:    lipgloss.Color("#e46876"),
				Border:      lipgloss.Color("#2a2a37"),
				Highlight:   lipgloss.Color("#7aa89f"),
				Badge:       lipgloss.Color("#7e9cd8"),
				BadgeText:   lipgloss.Color("#16161d"),
				HeaderAscii: lipgloss.Color("#957fb8"),
			},
			{
				ID:          "monokai-pro",
				Name:        "Monokai Pro",
				Author:      "Monokai",
				Description: "Refined professional palette with vibrant yellow, orange, and magenta accents",
				Category:    "Vibrant / Dark",
				Primary:     lipgloss.Color("#ffd866"),
				Secondary:   lipgloss.Color("#ab9df2"),
				Background:  lipgloss.Color("#2d2a2e"),
				Foreground:  lipgloss.Color("#fcfcfa"),
				Muted:       lipgloss.Color("#727072"),
				Playing:     lipgloss.Color("#a9dc76"),
				Favorite:    lipgloss.Color("#ff6188"),
				Border:      lipgloss.Color("#403e41"),
				Highlight:   lipgloss.Color("#78dce8"),
				Badge:       lipgloss.Color("#ffd866"),
				BadgeText:   lipgloss.Color("#221f22"),
				HeaderAscii: lipgloss.Color("#fc9867"),
			},
			{
				ID:          "one-dark",
				Name:        "One Dark Pro",
				Author:      "Atom / Binaryify",
				Description: "Iconic deep grey aesthetic with crisp blue, purple, and green tones",
				Category:    "Modern / Cool",
				Primary:     lipgloss.Color("#61afef"),
				Secondary:   lipgloss.Color("#c678dd"),
				Background:  lipgloss.Color("#282c34"),
				Foreground:  lipgloss.Color("#abb2bf"),
				Muted:       lipgloss.Color("#5c6370"),
				Playing:     lipgloss.Color("#98c379"),
				Favorite:    lipgloss.Color("#e06c75"),
				Border:      lipgloss.Color("#3e4451"),
				Highlight:   lipgloss.Color("#56b6c2"),
				Badge:       lipgloss.Color("#61afef"),
				BadgeText:   lipgloss.Color("#1e2227"),
				HeaderAscii: lipgloss.Color("#61afef"),
			},
			{
				ID:          "solarized-dark",
				Name:        "Solarized Dark",
				Author:      "Ethan Schoonover",
				Description: "Precision-engineered color palette with cyan, blue, and warm yellow hues",
				Category:    "Classic / High-Contrast",
				Primary:     lipgloss.Color("#268bd2"),
				Secondary:   lipgloss.Color("#6c71c4"),
				Background:  lipgloss.Color("#002b36"),
				Foreground:  lipgloss.Color("#839496"),
				Muted:       lipgloss.Color("#586e75"),
				Playing:     lipgloss.Color("#859900"),
				Favorite:    lipgloss.Color("#dc322f"),
				Border:      lipgloss.Color("#073642"),
				Highlight:   lipgloss.Color("#2aa198"),
				Badge:       lipgloss.Color("#268bd2"),
				BadgeText:   lipgloss.Color("#002b36"),
				HeaderAscii: lipgloss.Color("#b58900"),
			},
			{
				ID:          "cyberpunk",
				Name:        "Cyberpunk 2077",
				Author:      "halpworld",
				Description: "High-octane neon yellow, electric cyan, and laser red Night City vibes",
				Category:    "Cyberpunk / Neon",
				Primary:     lipgloss.Color("#fee801"),
				Secondary:   lipgloss.Color("#00ff9f"),
				Background:  lipgloss.Color("#050505"),
				Foreground:  lipgloss.Color("#fdfdfd"),
				Muted:       lipgloss.Color("#606060"),
				Playing:     lipgloss.Color("#00ff9f"),
				Favorite:    lipgloss.Color("#ff003c"),
				Border:      lipgloss.Color("#fee801"),
				Highlight:   lipgloss.Color("#00b8ff"),
				Badge:       lipgloss.Color("#fee801"),
				BadgeText:   lipgloss.Color("#000000"),
				HeaderAscii: lipgloss.Color("#ff003c"),
			},
			{
				ID:          "cyberdream",
				Name:        "Cyberdream",
				Author:      "scottmckendry",
				Description: "High-contrast futuristic synthwave palette with neon green and vibrant pink",
				Category:    "Synthwave / Neon",
				Primary:     lipgloss.Color("#5eff6c"),
				Secondary:   lipgloss.Color("#ff5ea0"),
				Background:  lipgloss.Color("#16181a"),
				Foreground:  lipgloss.Color("#ffffff"),
				Muted:       lipgloss.Color("#7b8496"),
				Playing:     lipgloss.Color("#5eff6c"),
				Favorite:    lipgloss.Color("#ff6e5e"),
				Border:      lipgloss.Color("#3c4048"),
				Highlight:   lipgloss.Color("#5ef1ff"),
				Badge:       lipgloss.Color("#5eff6c"),
				BadgeText:   lipgloss.Color("#16181a"),
				HeaderAscii: lipgloss.Color("#bd5eff"),
			},
			{
				ID:          "oxocarbon",
				Name:        "Oxocarbon",
				Author:      "shaunsingh",
				Description: "Dark, minimal IBM-inspired design with pink, cyan, and vibrant accents",
				Category:    "Minimal / Dark",
				Primary:     lipgloss.Color("#ee5396"),
				Secondary:   lipgloss.Color("#33b1ff"),
				Background:  lipgloss.Color("#161616"),
				Foreground:  lipgloss.Color("#dde1e6"),
				Muted:       lipgloss.Color("#525252"),
				Playing:     lipgloss.Color("#42be65"),
				Favorite:    lipgloss.Color("#ff7eb6"),
				Border:      lipgloss.Color("#262626"),
				Highlight:   lipgloss.Color("#3ddbd9"),
				Badge:       lipgloss.Color("#ee5396"),
				BadgeText:   lipgloss.Color("#161616"),
				HeaderAscii: lipgloss.Color("#be95ff"),
			},
			{
				ID:          "gruvbox-material",
				Name:        "Gruvbox Material",
				Author:      "sainnhe",
				Description: "Warm, textured earthy palette with soft orange, golden yellow, and moss",
				Category:    "Retro / Warm",
				Primary:     lipgloss.Color("#e78a4e"),
				Secondary:   lipgloss.Color("#d8a657"),
				Background:  lipgloss.Color("#282828"),
				Foreground:  lipgloss.Color("#dfbf8e"),
				Muted:       lipgloss.Color("#928374"),
				Playing:     lipgloss.Color("#a9b665"),
				Favorite:    lipgloss.Color("#ea6962"),
				Border:      lipgloss.Color("#3c3836"),
				Highlight:   lipgloss.Color("#7daea3"),
				Badge:       lipgloss.Color("#e78a4e"),
				BadgeText:   lipgloss.Color("#1d2021"),
				HeaderAscii: lipgloss.Color("#d3869b"),
			},
			{
				ID:          "moonlight",
				Name:        "Moonlight",
				Author:      "atomiks",
				Description: "Deep nocturnal midnight blues with soft pastel lavender and neon cyan",
				Category:    "Night / Blue",
				Primary:     lipgloss.Color("#82aaff"),
				Secondary:   lipgloss.Color("#c792ea"),
				Background:  lipgloss.Color("#212337"),
				Foreground:  lipgloss.Color("#c8d3f5"),
				Muted:       lipgloss.Color("#637777"),
				Playing:     lipgloss.Color("#c3e88d"),
				Favorite:    lipgloss.Color("#ff757f"),
				Border:      lipgloss.Color("#2f334d"),
				Highlight:   lipgloss.Color("#86e1fc"),
				Badge:       lipgloss.Color("#82aaff"),
				BadgeText:   lipgloss.Color("#191a2a"),
				HeaderAscii: lipgloss.Color("#ff98a4"),
			},
			{
				ID:          "poimandres",
				Name:        "Poimandres",
				Author:      "drcmda",
				Description: "Minimal dark theme with storm blues, soft pink, and teal highlights",
				Category:    "Minimal / Cool",
				Primary:     lipgloss.Color("#add7ff"),
				Secondary:   lipgloss.Color("#d0679d"),
				Background:  lipgloss.Color("#1b1e28"),
				Foreground:  lipgloss.Color("#a6accd"),
				Muted:       lipgloss.Color("#506477"),
				Playing:     lipgloss.Color("#5de4c7"),
				Favorite:    lipgloss.Color("#f087bd"),
				Border:      lipgloss.Color("#303340"),
				Highlight:   lipgloss.Color("#89ddff"),
				Badge:       lipgloss.Color("#add7ff"),
				BadgeText:   lipgloss.Color("#171922"),
				HeaderAscii: lipgloss.Color("#e4f0fb"),
			},
			{
				ID:          "catppuccin-frappe",
				Name:        "Catppuccin Frappé",
				Author:      "Catppuccin Community",
				Description: "Medium-contrast soothing pastel palette with mauve and lavender",
				Category:    "Pastel / Medium Dark",
				Primary:     lipgloss.Color("#ca9ee6"),
				Secondary:   lipgloss.Color("#f4b8e4"),
				Background:  lipgloss.Color("#303446"),
				Foreground:  lipgloss.Color("#c6d0f5"),
				Muted:       lipgloss.Color("#737994"),
				Playing:     lipgloss.Color("#a6d189"),
				Favorite:    lipgloss.Color("#e78284"),
				Border:      lipgloss.Color("#51576d"),
				Highlight:   lipgloss.Color("#85c1dc"),
				Badge:       lipgloss.Color("#ca9ee6"),
				BadgeText:   lipgloss.Color("#232634"),
				HeaderAscii: lipgloss.Color("#babbf1"),
			},
			{
				ID:          "catppuccin-latte",
				Name:        "Catppuccin Latte (Light)",
				Author:      "Catppuccin Community",
				Description: "Crisp, clean light theme with deep lavender, pink, and vibrant blue accents",
				Category:    "Light / Daylight",
				Primary:     lipgloss.Color("#8839ef"),
				Secondary:   lipgloss.Color("#ea76cb"),
				Background:  lipgloss.Color("#eff1f5"),
				Foreground:  lipgloss.Color("#4c4f69"),
				Muted:       lipgloss.Color("#9ca0b0"),
				Playing:     lipgloss.Color("#40a02b"),
				Favorite:    lipgloss.Color("#d20f39"),
				Border:      lipgloss.Color("#ccd0da"),
				Highlight:   lipgloss.Color("#1e66f5"),
				Badge:       lipgloss.Color("#8839ef"),
				BadgeText:   lipgloss.Color("#ffffff"),
				HeaderAscii: lipgloss.Color("#7287fd"),
			},
			{
				ID:          "tokyonight-storm",
				Name:        "Tokyo Night Storm",
				Author:      "folke",
				Description: "Deeper blue stormy aesthetic with vibrant lavender and cyan highlights",
				Category:    "Dark / Night",
				Primary:     lipgloss.Color("#7aa2f7"),
				Secondary:   lipgloss.Color("#bb9af7"),
				Background:  lipgloss.Color("#24283b"),
				Foreground:  lipgloss.Color("#c0caf5"),
				Muted:       lipgloss.Color("#565f89"),
				Playing:     lipgloss.Color("#9ece6a"),
				Favorite:    lipgloss.Color("#f7768e"),
				Border:      lipgloss.Color("#414868"),
				Highlight:   lipgloss.Color("#2ac3de"),
				Badge:       lipgloss.Color("#7aa2f7"),
				BadgeText:   lipgloss.Color("#1f2335"),
				HeaderAscii: lipgloss.Color("#7dcfff"),
			},
			{
				ID:          "palenight",
				Name:        "Palenight",
				Author:      "Material Theme",
				Description: "Material Design night aesthetic with violet, soft pink, and soft teal",
				Category:    "Modern / Purple",
				Primary:     lipgloss.Color("#82aaff"),
				Secondary:   lipgloss.Color("#c792ea"),
				Background:  lipgloss.Color("#292d3e"),
				Foreground:  lipgloss.Color("#a6accd"),
				Muted:       lipgloss.Color("#676e95"),
				Playing:     lipgloss.Color("#c3e88d"),
				Favorite:    lipgloss.Color("#ff5370"),
				Border:      lipgloss.Color("#3b3f51"),
				Highlight:   lipgloss.Color("#89ddff"),
				Badge:       lipgloss.Color("#82aaff"),
				BadgeText:   lipgloss.Color("#1b1e2b"),
				HeaderAscii: lipgloss.Color("#ffcb6b"),
			},
			{
				ID:          "horizon",
				Name:        "Horizon",
				Author:      "Jonathan Olaleye",
				Description: "Warm sunset aesthetics with glowing corals, apricot orange, and turquoise",
				Category:    "Warm / Sunset",
				Primary:     lipgloss.Color("#e95678"),
				Secondary:   lipgloss.Color("#fab795"),
				Background:  lipgloss.Color("#1c1e26"),
				Foreground:  lipgloss.Color("#d5d8da"),
				Muted:       lipgloss.Color("#6c6f93"),
				Playing:     lipgloss.Color("#29d398"),
				Favorite:    lipgloss.Color("#ee64ac"),
				Border:      lipgloss.Color("#2e303e"),
				Highlight:   lipgloss.Color("#26bbd9"),
				Badge:       lipgloss.Color("#e95678"),
				BadgeText:   lipgloss.Color("#16161c"),
				HeaderAscii: lipgloss.Color("#f09483"),
			},
			{
				ID:          "ayu-dark",
				Name:        "Ayu Dark",
				Author:      "dempfi",
				Description: "Simple, bright, and elegant dark theme with golden amber and cyan tones",
				Category:    "Dark / Gold",
				Primary:     lipgloss.Color("#ffb454"),
				Secondary:   lipgloss.Color("#ff7733"),
				Background:  lipgloss.Color("#0a0e14"),
				Foreground:  lipgloss.Color("#b3b1ad"),
				Muted:       lipgloss.Color("#4d5566"),
				Playing:     lipgloss.Color("#c2d94c"),
				Favorite:    lipgloss.Color("#f07178"),
				Border:      lipgloss.Color("#273747"),
				Highlight:   lipgloss.Color("#36a3d9"),
				Badge:       lipgloss.Color("#ffb454"),
				BadgeText:   lipgloss.Color("#0a0e14"),
				HeaderAscii: lipgloss.Color("#59c2ff"),
			},
		},
	}
}
