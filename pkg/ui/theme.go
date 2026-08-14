package ui

import "github.com/charmbracelet/lipgloss"

type Theme struct {
	Name        string
	Primary     lipgloss.Color
	Secondary   lipgloss.Color
	Background  lipgloss.Color
	Foreground  lipgloss.Color
	Muted       lipgloss.Color
	Playing     lipgloss.Color
	Favorite    lipgloss.Color
	Border      lipgloss.Color
	Highlight   lipgloss.Color
	Badge       lipgloss.Color
	BadgeText   lipgloss.Color
	HeaderAscii lipgloss.Color
}

var Themes = map[string]Theme{
	"tokyonight": {
		Name:        "Tokyo Night",
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
	},
	"catppuccin": {
		Name:        "Catppuccin Mocha",
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
	},
	"synthwave": {
		Name:        "Synthwave '84",
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
	},
	"nord": {
		Name:        "Nord",
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
	},
	"gruvbox": {
		Name:        "Gruvbox Dark",
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
	},
	"dracula": {
		Name:        "Dracula",
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
	},
}

func GetTheme(name string) Theme {
	if t, ok := Themes[name]; ok {
		return t
	}
	return Themes["tokyonight"]
}
