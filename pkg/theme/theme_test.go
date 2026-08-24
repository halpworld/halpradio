package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetTheme(t *testing.T) {
	ResetCustomThemes()

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
		{"case-insensitive id", "TOKYONIGHT", "Tokyo Night"},
		{"lookup by name", "Synthwave '84", "Synthwave '84"},
		{"lookup by name case-insensitive", "dracula", "Dracula"},
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
	ResetCustomThemes()

	for key, th := range BuiltinThemes {
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
			if th.IsCustom {
				t.Errorf("Builtin theme %q marked as custom", key)
			}
		})
	}
}

func TestGetAllThemesAndIDs(t *testing.T) {
	ResetCustomThemes()

	all := GetAllThemes()
	if len(all) != len(BuiltinThemeIDs) {
		t.Fatalf("Expected %d built-in themes, got %d", len(BuiltinThemeIDs), len(all))
	}

	ids := GetThemeIDs()
	if len(ids) != len(BuiltinThemeIDs) {
		t.Fatalf("Expected %d theme IDs, got %d", len(BuiltinThemeIDs), len(ids))
	}

	for i, id := range BuiltinThemeIDs {
		if ids[i] != id {
			t.Errorf("Expected theme index %d to be %q, got %q", i, id, ids[i])
		}
	}
}

func TestLoadCustomThemes(t *testing.T) {
	ResetCustomThemes()
	tempDir := t.TempDir()

	// 1. Write a valid custom theme YAML
	rosePineYAML := `
name: "Rosé Pine"
author: "Community"
description: "All natural pine, faux fur and delicate warmth"
primary: "#eb6f92"
secondary: "#f6c177"
background: "#191724"
foreground: "#e0def4"
muted: "#6e6a86"
playing: "#9ccfd8"
favorite: "#eb6f92"
border: "#26233a"
highlight: "#31748f"
badge: "#eb6f92"
badge_text: "#191724"
header_ascii: "#c4a7e7"
`
	if err := os.WriteFile(filepath.Join(tempDir, "rose-pine.yaml"), []byte(rosePineYAML), 0644); err != nil {
		t.Fatalf("Failed to write rose-pine.yaml: %v", err)
	}

	// 2. Write a theme with missing tokens (to verify fallback handling)
	minimalYAML := `
name: "Minimalist Red"
primary: "#ff0000"
background: "#000000"
`
	if err := os.WriteFile(filepath.Join(tempDir, "minimal-red.yml"), []byte(minimalYAML), 0644); err != nil {
		t.Fatalf("Failed to write minimal-red.yml: %v", err)
	}

	// 3. Write a malformed YAML file (should be skipped gracefully without panic)
	malformedYAML := `
name: "Broken Theme
primary: [invalid, yaml
`
	if err := os.WriteFile(filepath.Join(tempDir, "broken.yaml"), []byte(malformedYAML), 0644); err != nil {
		t.Fatalf("Failed to write broken.yaml: %v", err)
	}

	// 4. Write an example file (should be ignored by loader)
	if err := os.WriteFile(filepath.Join(tempDir, "sample_theme.yaml.example"), []byte(SampleThemeYAML()), 0644); err != nil {
		t.Fatalf("Failed to write sample_theme.yaml.example: %v", err)
	}

	// 5. Write a non-YAML file (should be ignored)
	if err := os.WriteFile(filepath.Join(tempDir, "notes.txt"), []byte("not a theme"), 0644); err != nil {
		t.Fatalf("Failed to write notes.txt: %v", err)
	}

	loaded, err := LoadCustomThemes(tempDir)
	if err != nil {
		t.Fatalf("LoadCustomThemes returned unexpected error: %v", err)
	}

	if len(loaded) != 2 {
		t.Fatalf("Expected 2 custom themes loaded, got %d (%+v)", len(loaded), loaded)
	}

	// Verify rose-pine
	rp, ok := loaded["rose-pine"]
	if !ok {
		t.Fatalf("Expected 'rose-pine' in loaded map")
	}
	if rp.Name != "Rosé Pine" || rp.Author != "Community" {
		t.Errorf("Unexpected metadata for rose-pine: %+v", rp)
	}
	if string(rp.Primary) != "#eb6f92" {
		t.Errorf("Expected primary #eb6f92, got %s", rp.Primary)
	}
	if !rp.IsCustom {
		t.Errorf("Expected IsCustom to be true")
	}

	// Verify minimal-red has fallback tokens
	minRed, ok := loaded["minimal-red"]
	if !ok {
		t.Fatalf("Expected 'minimal-red' in loaded map")
	}
	if minRed.Name != "Minimalist Red" {
		t.Errorf("Expected 'Minimalist Red', got %s", minRed.Name)
	}
	if string(minRed.Primary) != "#ff0000" {
		t.Errorf("Expected primary #ff0000, got %s", minRed.Primary)
	}
	if string(minRed.Foreground) == "" || string(minRed.Border) == "" {
		t.Errorf("Expected non-empty fallback tokens for minimal-red: %+v", minRed)
	}

	// Verify GetTheme finds the custom theme
	foundTheme := GetTheme("rose-pine")
	if foundTheme.Name != "Rosé Pine" {
		t.Errorf("GetTheme('rose-pine') failed, got %s", foundTheme.Name)
	}

	foundByName := GetTheme("Rosé Pine")
	if foundByName.ID != "rose-pine" {
		t.Errorf("GetTheme('Rosé Pine') failed, got %s", foundByName.ID)
	}

	// Verify GetAllThemes includes custom themes after built-ins
	all := GetAllThemes()
	if len(all) != len(BuiltinThemeIDs)+2 {
		t.Errorf("Expected %d total themes, got %d", len(BuiltinThemeIDs)+2, len(all))
	}
}

func TestLoadCustomThemesNonExistentDir(t *testing.T) {
	ResetCustomThemes()
	loaded, err := LoadCustomThemes("/non/existent/path/that/should/not/fail")
	if err != nil {
		t.Errorf("Expected nil error for non-existent dir, got %v", err)
	}
	if len(loaded) != 0 {
		t.Errorf("Expected empty map, got %d items", len(loaded))
	}
}

func TestExportActiveThemeAndEnsureExample(t *testing.T) {
	tempDir := t.TempDir()

	// Test EnsureExampleTheme
	if err := EnsureExampleTheme(tempDir); err != nil {
		t.Fatalf("EnsureExampleTheme failed: %v", err)
	}

	exampleFile := filepath.Join(tempDir, "sample_theme.yaml.example")
	data, err := os.ReadFile(exampleFile)
	if err != nil {
		t.Fatalf("Expected sample_theme.yaml.example to exist: %v", err)
	}
	if !strings.Contains(string(data), "Rosé Pine") {
		t.Errorf("Expected example theme content, got: %s", string(data))
	}

	// Ensure calling again is idempotent
	if err := EnsureExampleTheme(tempDir); err != nil {
		t.Errorf("Second EnsureExampleTheme failed: %v", err)
	}

	// Test ExportActiveTheme
	th := BuiltinThemes["nord"]
	exportedPath, err := ExportActiveTheme(tempDir, th)
	if err != nil {
		t.Fatalf("ExportActiveTheme failed: %v", err)
	}

	if !strings.HasSuffix(exportedPath, "nord.yaml") {
		t.Errorf("Expected exported path to end with nord.yaml, got %s", exportedPath)
	}

	exportedData, err := os.ReadFile(exportedPath)
	if err != nil {
		t.Fatalf("Failed to read exported theme: %v", err)
	}
	if !strings.Contains(string(exportedData), "name: \"Nord\"") || !strings.Contains(string(exportedData), "primary: \"#88c0d0\"") {
		t.Errorf("Exported YAML missing expected tokens: %s", string(exportedData))
	}
}

func TestColorValidation(t *testing.T) {
	tests := []struct {
		input string
		valid bool
	}{
		{"#ffffff", true},
		{"#fff", true},
		{"#1a1b26", true},
		{"#1a1b26ff", true},
		{"red", true},
		{"blue", true},
		{"cyan", true},
		{"brightgreen", true},
		{"208", true},
		{"0", true},
		{"255", true},
		{"", false},
		{"invalid", false},
		{"#12", false},
		{"#12345", false},
		{"#gggggg", false},
		{"9999", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isValidColor(tt.input)
			if got != tt.valid {
				t.Errorf("isValidColor(%q) = %v, expected %v", tt.input, got, tt.valid)
			}
		})
	}
}

func TestThemeSecuritySanitization(t *testing.T) {
	// 1. Control character / ANSI injection stripping in metadata
	malicious := Theme{
		Name:        "Evil\x1b[2J\x1b]50;hack\x07Theme\x00\r\n",
		Author:      "Attacker\x1b[31m",
		Description: "Payload\x08\x0b",
		Primary:     "#ff0000",
	}

	sanitized := sanitizeThemeTokens(malicious)
	if strings.Contains(sanitized.Name, "\x1b") || strings.Contains(sanitized.Name, "\x00") || strings.Contains(sanitized.Name, "\r") {
		t.Errorf("Expected control chars stripped from Name, got: %q", sanitized.Name)
	}
	if strings.Contains(sanitized.Author, "\x1b") {
		t.Errorf("Expected ANSI escape stripped from Author, got: %q", sanitized.Author)
	}
	if strings.Contains(sanitized.Description, "\x08") {
		t.Errorf("Expected control chars stripped from Description, got: %q", sanitized.Description)
	}
}

func TestThemeFileSizeLimit(t *testing.T) {
	ResetCustomThemes()
	tempDir := t.TempDir()

	// Write oversized theme file (> 64KB)
	largeData := strings.Repeat("# padding\n", 10000) + `
name: "Oversized Theme"
primary: "#123456"
`
	oversizedPath := filepath.Join(tempDir, "oversized.yaml")
	if err := os.WriteFile(oversizedPath, []byte(largeData), 0644); err != nil {
		t.Fatalf("Failed to write oversized file: %v", err)
	}

	loaded, err := LoadCustomThemes(tempDir)
	if err != nil {
		t.Fatalf("Unexpected error loading custom themes: %v", err)
	}
	if _, ok := loaded["oversized"]; ok {
		t.Errorf("Expected oversized theme file (>64KB) to be skipped")
	}
}

func TestExportThemePathTraversal(t *testing.T) {
	tempDir := t.TempDir()

	traversalTheme := Theme{
		ID:      "../../../../etc/evil",
		Name:    "../../../evil",
		Primary: "#ff0000",
	}

	exportedPath, err := ExportActiveTheme(tempDir, traversalTheme)
	if err != nil {
		t.Fatalf("ExportActiveTheme failed: %v", err)
	}

	// Verify exportedPath is strictly inside tempDir
	rel, err := filepath.Rel(tempDir, exportedPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("Exported path escaped destination directory: %s (rel: %s)", exportedPath, rel)
	}
}

