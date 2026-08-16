package theme

import (
	"testing"
)

func TestGetTheme(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectedName string
	}{
		{"tokyonight theme", "tokyonight", "Tokyo Night"},
		{"catppuccin theme", "catppuccin", "Catppuccin Mocha"},
		{"synthwave theme", "synthwave", "Synthwave '84"},
		{"nord theme", "nord", "Nord"},
		{"gruvbox theme", "gruvbox", "Gruvbox Dark"},
		{"dracula theme", "dracula", "Dracula"},
		{"fallback on unknown", "nonexistent-theme", "Tokyo Night"},
		{"fallback on empty", "", "Tokyo Night"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			th := GetTheme(tt.input)
			if th.Name != tt.expectedName {
				t.Errorf("GetTheme(%q).Name = %q, expected %q", tt.input, th.Name, tt.expectedName)
			}
		})
	}
}

func TestThemesIntegrity(t *testing.T) {
	for key, th := range Themes {
		t.Run(key, func(t *testing.T) {
			if th.Name == "" {
				t.Errorf("Theme %q has empty Name", key)
			}
			if th.Primary == "" {
				t.Errorf("Theme %q has empty Primary color", key)
			}
			if th.Secondary == "" {
				t.Errorf("Theme %q has empty Secondary color", key)
			}
			if th.Background == "" {
				t.Errorf("Theme %q has empty Background color", key)
			}
			if th.Foreground == "" {
				t.Errorf("Theme %q has empty Foreground color", key)
			}
			if th.Muted == "" {
				t.Errorf("Theme %q has empty Muted color", key)
			}
			if th.Playing == "" {
				t.Errorf("Theme %q has empty Playing color", key)
			}
			if th.Favorite == "" {
				t.Errorf("Theme %q has empty Favorite color", key)
			}
			if th.Border == "" {
				t.Errorf("Theme %q has empty Border color", key)
			}
			if th.Highlight == "" {
				t.Errorf("Theme %q has empty Highlight color", key)
			}
			if th.Badge == "" {
				t.Errorf("Theme %q has empty Badge color", key)
			}
			if th.BadgeText == "" {
				t.Errorf("Theme %q has empty BadgeText color", key)
			}
			if th.HeaderAscii == "" {
				t.Errorf("Theme %q has empty HeaderAscii color", key)
			}
		})
	}
}
