package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

// Theme represents a complete color scheme and metadata used across halpradio TUI.
type Theme struct {
	ID          string         `yaml:"id,omitempty"`
	Name        string         `yaml:"name"`
	Author      string         `yaml:"author,omitempty"`
	Description string         `yaml:"description,omitempty"`
	Primary     lipgloss.Color `yaml:"primary"`
	Secondary   lipgloss.Color `yaml:"secondary"`
	Background  lipgloss.Color `yaml:"background"`
	Foreground  lipgloss.Color `yaml:"foreground"`
	Muted       lipgloss.Color `yaml:"muted"`
	Playing     lipgloss.Color `yaml:"playing"`
	Favorite    lipgloss.Color `yaml:"favorite"`
	Border      lipgloss.Color `yaml:"border"`
	Highlight   lipgloss.Color `yaml:"highlight"`
	Badge       lipgloss.Color `yaml:"badge"`
	BadgeText   lipgloss.Color `yaml:"badge_text"`
	HeaderAscii lipgloss.Color `yaml:"header_ascii"`
	IsCustom    bool           `yaml:"-"`
}

// BuiltinThemeIDs defines the canonical display order for built-in palettes.
var BuiltinThemeIDs = []string{
	"tokyonight",
	"catppuccin",
	"synthwave",
	"nord",
	"gruvbox",
	"dracula",
}

var (
	themeMutex sync.RWMutex

	// BuiltinThemes defines the core curated theme collection.
	BuiltinThemes = map[string]Theme{
		"tokyonight": {
			ID:          "tokyonight",
			Name:        "Tokyo Night",
			Author:      "folke",
			Description: "Clean dark theme inspired by Tokyo at night",
			Primary:     lipgloss.Color("#7aa2f7"),
			Secondary:   lipgloss.Color("#bb9af7"),
			Background:  lipgloss.Color("#1a1b26"),
			Foreground:  lipgloss.Color("#a9b1d6"),
			Muted:       lipgloss.Color("#565f89"),
			Playing:     lipgloss.Color("#9ece6a"),
			Favorite:    lipgloss.Color("#f7768e"),
			Border:      lipgloss.Color("#3b4261"),
			Highlight:   lipgloss.Color("#2ac3de"),
			Badge:       lipgloss.Color("#7aa2f7"),
			BadgeText:   lipgloss.Color("#15161e"),
			HeaderAscii: lipgloss.Color("#7dcfff"),
			IsCustom:    false,
		},
		"catppuccin": {
			ID:          "catppuccin",
			Name:        "Catppuccin Mocha",
			Author:      "Catppuccin Community",
			Description: "Soothing warm pastel palette for high-spirited minds",
			Primary:     lipgloss.Color("#cba6f7"),
			Secondary:   lipgloss.Color("#f5c2e7"),
			Background:  lipgloss.Color("#1e1e2e"),
			Foreground:  lipgloss.Color("#cdd6f4"),
			Muted:       lipgloss.Color("#6c7086"),
			Playing:     lipgloss.Color("#a6e3a1"),
			Favorite:    lipgloss.Color("#f38ba8"),
			Border:      lipgloss.Color("#45475a"),
			Highlight:   lipgloss.Color("#89dceb"),
			Badge:       lipgloss.Color("#cba6f7"),
			BadgeText:   lipgloss.Color("#11111b"),
			HeaderAscii: lipgloss.Color("#b4befe"),
			IsCustom:    false,
		},
		"synthwave": {
			ID:          "synthwave",
			Name:        "Synthwave '84",
			Author:      "Robb Owen",
			Description: "High-contrast neon retro 80s aesthetic",
			Primary:     lipgloss.Color("#ff007f"),
			Secondary:   lipgloss.Color("#00f0ff"),
			Background:  lipgloss.Color("#1a0933"),
			Foreground:  lipgloss.Color("#f9f9f9"),
			Muted:       lipgloss.Color("#6c558c"),
			Playing:     lipgloss.Color("#39ff14"),
			Favorite:    lipgloss.Color("#ff0055"),
			Border:      lipgloss.Color("#ff00aa"),
			Highlight:   lipgloss.Color("#ffee00"),
			Badge:       lipgloss.Color("#ff007f"),
			BadgeText:   lipgloss.Color("#ffffff"),
			HeaderAscii: lipgloss.Color("#00f0ff"),
			IsCustom:    false,
		},
		"nord": {
			ID:          "nord",
			Name:        "Nord",
			Author:      "Arctic Ice Studio",
			Description: "Arctic, north-bluish clean and elegant aesthetic",
			Primary:     lipgloss.Color("#88c0d0"),
			Secondary:   lipgloss.Color("#81a1c1"),
			Background:  lipgloss.Color("#2e3440"),
			Foreground:  lipgloss.Color("#eceff4"),
			Muted:       lipgloss.Color("#4c566a"),
			Playing:     lipgloss.Color("#a3be8c"),
			Favorite:    lipgloss.Color("#bf616a"),
			Border:      lipgloss.Color("#434c5e"),
			Highlight:   lipgloss.Color("#8fbcbb"),
			Badge:       lipgloss.Color("#88c0d0"),
			BadgeText:   lipgloss.Color("#2e3440"),
			HeaderAscii: lipgloss.Color("#81a1c1"),
			IsCustom:    false,
		},
		"gruvbox": {
			ID:          "gruvbox",
			Name:        "Gruvbox Dark",
			Author:      "Pavel Pertsev",
			Description: "Retro groove warm color scheme with earthy accents",
			Primary:     lipgloss.Color("#fe8019"),
			Secondary:   lipgloss.Color("#fabd2f"),
			Background:  lipgloss.Color("#282828"),
			Foreground:  lipgloss.Color("#ebdbb2"),
			Muted:       lipgloss.Color("#928374"),
			Playing:     lipgloss.Color("#b8bb26"),
			Favorite:    lipgloss.Color("#fb4934"),
			Border:      lipgloss.Color("#504945"),
			Highlight:   lipgloss.Color("#83a598"),
			Badge:       lipgloss.Color("#fe8019"),
			BadgeText:   lipgloss.Color("#1d2021"),
			HeaderAscii: lipgloss.Color("#fabd2f"),
			IsCustom:    false,
		},
		"dracula": {
			ID:          "dracula",
			Name:        "Dracula",
			Author:      "Zeno Rocha",
			Description: "Famous dark theme crafted for night owls and vampires",
			Primary:     lipgloss.Color("#bd93f9"),
			Secondary:   lipgloss.Color("#ff79c6"),
			Background:  lipgloss.Color("#282a36"),
			Foreground:  lipgloss.Color("#f8f8f2"),
			Muted:       lipgloss.Color("#6272a4"),
			Playing:     lipgloss.Color("#50fa7b"),
			Favorite:    lipgloss.Color("#ff5555"),
			Border:      lipgloss.Color("#44475a"),
			Highlight:   lipgloss.Color("#8be9fd"),
			Badge:       lipgloss.Color("#bd93f9"),
			BadgeText:   lipgloss.Color("#282a36"),
			HeaderAscii: lipgloss.Color("#ff79c6"),
			IsCustom:    false,
		},
	}

	// Themes is the active runtime registry (initialized with built-ins).
	Themes = copyBuiltins()

	// customThemeIDs tracks registered custom theme IDs in sorted order.
	customThemeIDs []string
)

func copyBuiltins() map[string]Theme {
	m := make(map[string]Theme, len(BuiltinThemes))
	for k, v := range BuiltinThemes {
		m[k] = v
	}
	return m
}

// ResetCustomThemes resets the active Themes map back to built-ins only.
func ResetCustomThemes() {
	themeMutex.Lock()
	defer themeMutex.Unlock()
	Themes = copyBuiltins()
	customThemeIDs = nil
}

// RegisterTheme stores a theme in the runtime registry.
func RegisterTheme(id string, th Theme) {
	themeMutex.Lock()
	defer themeMutex.Unlock()

	normID := normalizeID(id)
	if normID == "" {
		normID = normalizeID(th.Name)
	}
	th.ID = normID
	th = sanitizeThemeTokens(th)

	Themes[normID] = th

	if th.IsCustom {
		found := false
		for _, cid := range customThemeIDs {
			if cid == normID {
				found = true
				break
			}
		}
		if !found {
			customThemeIDs = append(customThemeIDs, normID)
			sort.Strings(customThemeIDs)
		}
	}
}

// UnregisterTheme removes a theme from the active runtime registry.
func UnregisterTheme(id string) {
	themeMutex.Lock()
	defer themeMutex.Unlock()

	normID := normalizeID(id)
	delete(Themes, normID)

	var newCustom []string
	for _, cid := range customThemeIDs {
		if cid != normID {
			newCustom = append(newCustom, cid)
		}
	}
	customThemeIDs = newCustom
}

// GetTheme retrieves a theme by ID or Name (case-insensitive), falling back to Tokyo Night.
func GetTheme(name string) Theme {
	themeMutex.RLock()
	defer themeMutex.RUnlock()

	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return Themes["tokyonight"]
	}

	// 1. Exact key match
	if t, ok := Themes[trimmed]; ok {
		return t
	}

	// 2. Normalized slug match (e.g. "rose-pine", "rose_pine", "Rose Pine")
	norm := normalizeID(trimmed)
	if t, ok := Themes[norm]; ok {
		return t
	}

	// 3. Case-insensitive Name or ID lookup
	for _, t := range Themes {
		if strings.EqualFold(t.Name, trimmed) || strings.EqualFold(t.ID, trimmed) || strings.EqualFold(normalizeID(t.Name), norm) {
			return t
		}
	}

	// Fallback to default
	return Themes["tokyonight"]
}

// GetAllThemes returns all themes in display order: built-ins first, then custom themes.
func GetAllThemes() []Theme {
	themeMutex.RLock()
	defer themeMutex.RUnlock()

	var result []Theme

	// Built-in themes in canonical order
	for _, id := range BuiltinThemeIDs {
		if th, ok := Themes[id]; ok {
			result = append(result, th)
		}
	}

	// Custom themes in alphabetical order
	for _, id := range customThemeIDs {
		if th, ok := Themes[id]; ok && th.IsCustom {
			result = append(result, th)
		}
	}

	return result
}

// GetThemeIDs returns all registered theme IDs in display order.
func GetThemeIDs() []string {
	all := GetAllThemes()
	ids := make([]string, len(all))
	for i, th := range all {
		ids[i] = th.ID
	}
	return ids
}

const (
	// MaxThemeFileSize caps custom theme files at 64 KB to prevent resource exhaustion.
	MaxThemeFileSize = 64 * 1024
	// MaxCustomThemes limits the maximum number of custom themes loaded into memory.
	MaxCustomThemes = 500
)

// LoadCustomThemes scans a directory for *.yaml / *.yml files and registers them.
// Skips files that end with .example, exceed size limits, or are malformed.
func LoadCustomThemes(dir string) (map[string]Theme, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	loaded := make(map[string]Theme)

	for _, entry := range entries {
		if len(loaded) >= MaxCustomThemes {
			break
		}

		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		lowerName := strings.ToLower(filename)

		// Ignore example templates and non-yaml files
		if strings.HasSuffix(lowerName, ".example") ||
			(!strings.HasSuffix(lowerName, ".yaml") && !strings.HasSuffix(lowerName, ".yml")) {
			continue
		}

		info, infoErr := entry.Info()
		if infoErr != nil || info.Size() > MaxThemeFileSize || info.Size() == 0 {
			continue
		}

		filePath := filepath.Join(dir, filename)
		data, readErr := os.ReadFile(filePath)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to read custom theme file %s: %v\n", filename, readErr)
			continue
		}

		var th Theme
		if yamlErr := yaml.Unmarshal(data, &th); yamlErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to parse YAML custom theme %s: %v\n", filename, yamlErr)
			continue
		}

		// Derive ID from filename (e.g. rose-pine.yaml -> rose-pine)
		base := strings.TrimSuffix(strings.TrimSuffix(filename, ".yaml"), ".yml")
		themeID := normalizeID(base)
		if themeID == "" {
			themeID = normalizeID(th.Name)
		}
		if th.Name == "" {
			th.Name = prettifyName(base)
		}

		th.ID = themeID
		th.IsCustom = true
		th = sanitizeThemeTokens(th)

		RegisterTheme(themeID, th)
		loaded[themeID] = th
	}

	return loaded, nil
}

// EnsureExampleTheme writes sample_theme.yaml.example to the themes directory if not already present.
func EnsureExampleTheme(themesDir string) error {
	if err := os.MkdirAll(themesDir, 0700); err != nil {
		return err
	}

	examplePath := filepath.Join(themesDir, "sample_theme.yaml.example")
	if _, err := os.Stat(examplePath); err == nil {
		return nil // already exists
	}

	return os.WriteFile(examplePath, []byte(SampleThemeYAML()), 0644)
}

// ExportActiveTheme dumps a theme to a formatted YAML file in themesDir.
func ExportActiveTheme(themesDir string, th Theme) (string, error) {
	if err := os.MkdirAll(themesDir, 0700); err != nil {
		return "", err
	}

	id := filepath.Base(normalizeID(th.ID))
	if id == "" || id == "." || id == "/" {
		id = filepath.Base(normalizeID(th.Name))
	}
	if id == "" || id == "." || id == "/" {
		id = "custom-theme"
	}

	targetPath := filepath.Join(themesDir, fmt.Sprintf("%s.yaml", id))
	if err := ExportTheme(targetPath, th); err != nil {
		return "", err
	}

	return targetPath, nil
}

// ExportTheme writes a formatted, commented YAML theme definition to filePath.
func ExportTheme(filePath string, th Theme) error {
	content := FormatThemeYAML(th)
	return os.WriteFile(filePath, []byte(content), 0644)
}

// FormatThemeYAML returns a clean, fully-commented YAML string representing a theme.
func FormatThemeYAML(th Theme) string {
	name := th.Name
	if name == "" {
		name = "Custom Palette"
	}
	author := th.Author
	if author == "" {
		author = "Community"
	}
	desc := th.Description
	if desc == "" {
		desc = "Custom color palette for halpradio"
	}

	th = sanitizeThemeTokens(th)

	return fmt.Sprintf(`# Halpradio Theme Definition
# Place in ~/.config/halpradio/themes/<name>.yaml to use in halpradio

name: %q
author: %q
description: %q

# Semantic color tokens (Hex #RRGGBB, #RGB, or ANSI color codes)
primary: %q        # Active focus borders, highlights, main accent
secondary: %q      # Subheadings, auxiliary text, accents
background: %q     # Window & modal background
foreground: %q     # Standard text, track names, unselected items
muted: %q          # Secondary metadata, shortcuts, dim borders
playing: %q        # Live playing station indicator, visualizer VU
favorite: %q       # Starred favorites icon & indicator
border: %q         # Pane borders, splitters, modal outlines
highlight: %q      # Cursor row highlight, search matches
badge: %q          # Badge background in header & statusbar
badge_text: %q     # Text color inside solid badges
header_ascii: %q   # Radio ASCII logo & decorative banners
`,
		name,
		author,
		desc,
		string(th.Primary),
		string(th.Secondary),
		string(th.Background),
		string(th.Foreground),
		string(th.Muted),
		string(th.Playing),
		string(th.Favorite),
		string(th.Border),
		string(th.Highlight),
		string(th.Badge),
		string(th.BadgeText),
		string(th.HeaderAscii),
	)
}

// SampleThemeYAML returns the default example theme template.
func SampleThemeYAML() string {
	return `# Halpradio Custom Theme Definition Example
# Place this file as <your-theme-name>.yaml in ~/.config/halpradio/themes/
# and it will immediately appear in the Theme Picker ('t') and CLI (-theme <name>).

name: "Rosé Pine"
author: "Community"
description: "All natural pine, faux fur and delicate warmth"

# Semantic color tokens (Hex #RRGGBB or ANSI color codes)
primary: "#eb6f92"        # Active focus borders, highlights, main accent
secondary: "#f6c177"      # Subheadings, auxiliary text, accents
background: "#191724"     # Window & modal background
foreground: "#e0def4"     # Standard text, track names, unselected items
muted: "#6e6a86"          # Secondary metadata, shortcuts, dim borders
playing: "#9ccfd8"        # Live playing station indicator, visualizer VU
favorite: "#eb6f92"       # Starred favorites icon & indicator
border: "#26233a"         # Pane borders, splitters, modal outlines
highlight: "#31748f"      # Cursor row highlight, search matches
badge: "#eb6f92"          # Badge background in header & statusbar
badge_text: "#191724"     # Text color inside solid badges
header_ascii: "#c4a7e7"   # Radio ASCII logo & decorative banners
`
}

var (
	hexRegex  = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6}|[0-9a-fA-F]{8})$`)
	slugRegex = regexp.MustCompile(`[^a-z0-9_-]+`)
)

func normalizeID(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = slugRegex.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func prettifyName(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		}
	}
	return strings.Join(parts, " ")
}

func isValidColor(c string) bool {
	c = strings.TrimSpace(c)
	if c == "" {
		return false
	}
	if hexRegex.MatchString(c) {
		return true
	}
	// ANSI names or numbers 0-255
	validAnsi := map[string]bool{
		"black": true, "red": true, "green": true, "yellow": true,
		"blue": true, "magenta": true, "cyan": true, "white": true,
		"brightblack": true, "brightred": true, "brightgreen": true, "brightyellow": true,
		"brightblue": true, "brightmagenta": true, "brightcyan": true, "brightwhite": true,
	}
	if validAnsi[strings.ToLower(c)] {
		return true
	}
	// Numeric ANSI color code 0-255
	for _, r := range c {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(c) > 0 && len(c) <= 3
}

func sanitizeColor(val lipgloss.Color, fallback lipgloss.Color) lipgloss.Color {
	s := strings.TrimSpace(string(val))
	if isValidColor(s) {
		return lipgloss.Color(s)
	}
	return fallback
}

func sanitizeString(s string, maxLen int) string {
	var b strings.Builder
	for _, r := range s {
		// Reject control characters (ASCII 0-31, 127, and C1 controls 0x80-0x9F)
		if r < 32 || r == 127 || (r >= 0x80 && r <= 0x9F) {
			continue
		}
		b.WriteRune(r)
		if b.Len() >= maxLen {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func sanitizeThemeTokens(th Theme) Theme {
	// Base defaults (Tokyo Night defaults)
	defPrimary := lipgloss.Color("#7aa2f7")
	defSecondary := lipgloss.Color("#bb9af7")
	defBackground := lipgloss.Color("#1a1b26")
	defForeground := lipgloss.Color("#a9b1d6")
	defMuted := lipgloss.Color("#565f89")
	defPlaying := lipgloss.Color("#9ece6a")
	defFavorite := lipgloss.Color("#f7768e")
	defBorder := lipgloss.Color("#3b4261")
	defHighlight := lipgloss.Color("#2ac3de")
	defBadgeText := lipgloss.Color("#15161e")
	defHeaderAscii := lipgloss.Color("#7dcfff")

	th.Primary = sanitizeColor(th.Primary, defPrimary)
	th.Secondary = sanitizeColor(th.Secondary, defSecondary)
	th.Background = sanitizeColor(th.Background, defBackground)
	th.Foreground = sanitizeColor(th.Foreground, defForeground)
	th.Muted = sanitizeColor(th.Muted, defMuted)
	th.Playing = sanitizeColor(th.Playing, defPlaying)
	th.Favorite = sanitizeColor(th.Favorite, defFavorite)
	th.Border = sanitizeColor(th.Border, defBorder)
	th.Highlight = sanitizeColor(th.Highlight, defHighlight)
	th.Badge = sanitizeColor(th.Badge, th.Primary)
	th.BadgeText = sanitizeColor(th.BadgeText, defBadgeText)
	th.HeaderAscii = sanitizeColor(th.HeaderAscii, defHeaderAscii)

	th.Name = sanitizeString(th.Name, 64)
	th.Author = sanitizeString(th.Author, 64)
	th.Description = sanitizeString(th.Description, 256)

	if th.Name == "" {
		if th.ID != "" {
			th.Name = prettifyName(th.ID)
		} else {
			th.Name = "Custom Theme"
		}
	}

	return th
}
