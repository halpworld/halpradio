package theme

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
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
}

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

func (c *RegistryClient) sanitizeRegistryThemes(index *RegistryIndex) {
	for i := range index.Themes {
		t := &index.Themes[i]
		if t.ID == "" {
			t.ID = normalizeID(t.Name)
		}
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
	}
}

// DownloadAndInstall downloads or writes a theme into the themesDir and registers it.
func (c *RegistryClient) DownloadAndInstall(ctx context.Context, th RegistryTheme, themesDir string) error {
	rawID := strings.TrimSpace(th.ID)
	if rawID == "" {
		rawID = th.Name
	}
	if strings.Contains(th.ID, "..") || strings.Contains(th.ID, "/") || strings.Contains(th.ID, "\\") {
		return fmt.Errorf("invalid theme ID %q (path traversal characters not allowed)", th.ID)
	}
	normID := normalizeID(rawID)
	if normID == "" || !validIDRegex.MatchString(normID) {
		return fmt.Errorf("invalid theme ID %q", th.ID)
	}

	if err := os.MkdirAll(themesDir, 0700); err != nil {
		return fmt.Errorf("failed to create themes directory: %w", err)
	}

	targetPath := filepath.Join(themesDir, fmt.Sprintf("%s.yaml", normID))

	// If a custom HTTP download URL is specified, try downloading raw YAML
	var yamlContent string
	if th.DownloadURL != "" {
		downloadURL := th.DownloadURL
		if !strings.HasPrefix(downloadURL, "http://") && !strings.HasPrefix(downloadURL, "https://") {
			base, err := url.Parse(c.registryURL)
			if err == nil {
				rel, err := url.Parse(downloadURL)
				if err == nil {
					downloadURL = base.ResolveReference(rel).String()
				}
			}
		}

		if parsedURL, err := url.Parse(downloadURL); err == nil && (parsedURL.Scheme == "http" || parsedURL.Scheme == "https") {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
			if err == nil {
				req.Header.Set("User-Agent", "halpradio/theme-installer")
				resp, err := c.httpClient.Do(req)
				if err == nil && resp.StatusCode == http.StatusOK {
					defer resp.Body.Close()
					data, err := io.ReadAll(io.LimitReader(resp.Body, MaxThemeFileSize))
					if err == nil && len(data) > 0 {
						yamlContent = string(data)
					}
				}
			}
		}
	}

	if yamlContent == "" {
		yamlContent = FormatThemeYAML(th.ToTheme())
	}

	if err := os.WriteFile(targetPath, []byte(yamlContent), 0644); err != nil {
		return fmt.Errorf("failed to save theme file: %w", err)
	}

	// Register in runtime memory
	runtimeTheme := th.ToTheme()
	runtimeTheme.ID = normID
	runtimeTheme.IsCustom = true
	RegisterTheme(normID, runtimeTheme)

	return nil
}

// Uninstall removes a custom theme YAML file from disk and unregisters it.
func (c *RegistryClient) Uninstall(id string, themesDir string) error {
	if strings.Contains(id, "..") || strings.Contains(id, "/") || strings.Contains(id, "\\") {
		return fmt.Errorf("invalid theme ID %q (path traversal characters not allowed)", id)
	}
	normID := normalizeID(id)
	if normID == "" || !validIDRegex.MatchString(normID) {
		return fmt.Errorf("invalid theme ID %q", id)
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
