package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/theme"
)

func TestThemeModalTabsAndLivePreview(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	m := createTestModel()
	initialTheme := m.Theme.Name

	// 1. Open theme picker with 't'
	mModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = mModel.(Model)
	if !m.ShowThemePicker {
		t.Fatalf("Expected ShowThemePicker to be true")
	}
	if m.ThemeModalTab != 0 {
		t.Errorf("Expected ThemeModalTab to be 0 (Installed), got %d", m.ThemeModalTab)
	}
	if cmd == nil {
		t.Errorf("Expected fetchThemeRegistryCmd returned on opening theme picker")
	}

	// 2. Switch tab to Community Hub (Tab 1) with 'tab'
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mModel.(Model)
	if m.ThemeModalTab != 1 {
		t.Fatalf("Expected ThemeModalTab 1 after Tab, got %d", m.ThemeModalTab)
	}

	// 3. Load sample registry themes
	mModel, _ = m.Update(ThemeRegistryLoadedMsg{
		Themes: []theme.RegistryTheme{
			{
				ID:          "rose-pine",
				Name:        "Rosé Pine",
				Author:      "mvllow",
				Description: "All natural pine",
				Category:    "Pastel",
				Primary:     "#eb6f92",
				Background:  "#191724",
			},
			{
				ID:          "everforest",
				Name:        "Everforest Dark",
				Author:      "sainnhe",
				Description: "Natural green palette",
				Category:    "Green",
				Primary:     "#a7c080",
				Background:  "#2d353b",
			},
		},
	})
	m = mModel.(Model)
	if len(m.ThemeRegistryList) != 2 {
		t.Fatalf("Expected 2 registry themes loaded, got %d", len(m.ThemeRegistryList))
	}

	// 4. Test Live Preview Toggle ('p') in Community Hub
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = mModel.(Model)
	if !m.IsPreviewingTheme {
		t.Errorf("Expected IsPreviewingTheme true after pressing 'p'")
	}
	if m.Theme.Name != "Rosé Pine" {
		t.Errorf("Expected live preview theme Rosé Pine, got %s", m.Theme.Name)
	}

	// 5. Navigate down ('j') while live preview is active -> should update live preview theme!
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = mModel.(Model)
	if m.ThemeRegistryCursor != 1 {
		t.Errorf("Expected ThemeRegistryCursor 1, got %d", m.ThemeRegistryCursor)
	}
	if m.Theme.Name != "Everforest Dark" {
		t.Errorf("Expected live preview updated to Everforest Dark, got %s", m.Theme.Name)
	}

	// 6. Dismiss/Revert with 'esc'
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mModel.(Model)
	if m.ShowThemePicker {
		t.Errorf("Expected ShowThemePicker false after Esc")
	}
	if m.IsPreviewingTheme {
		t.Errorf("Expected IsPreviewingTheme false after Esc")
	}
	if m.Theme.Name != initialTheme {
		t.Errorf("Expected theme reverted to %s, got %s", initialTheme, m.Theme.Name)
	}
}

func TestThemeModalSearchFilter(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	m := createTestModel()
	m.ShowThemePicker = true
	m.ThemeModalTab = 1
	m.ThemeRegistryList = []theme.RegistryTheme{
		{ID: "rose-pine", Name: "Rosé Pine", Category: "Pastel"},
		{ID: "everforest", Name: "Everforest Dark", Category: "Green"},
		{ID: "kanagawa", Name: "Kanagawa", Category: "Dark"},
	}

	// 1. Activate search mode with '/'
	mModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = mModel.(Model)
	if !m.ThemeIsSearching {
		t.Fatalf("Expected ThemeIsSearching to be true")
	}

	// 2. Type "green"
	for _, r := range "green" {
		mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mModel.(Model)
	}
	if m.ThemeSearchQuery != "green" {
		t.Errorf("Expected ThemeSearchQuery 'green', got %q", m.ThemeSearchQuery)
	}
	filtered := m.getFilteredRegistryThemes()
	if len(filtered) != 1 || filtered[0].ID != "everforest" {
		t.Errorf("Expected filtered list with everforest, got %+v", filtered)
	}

	// 3. Backspace
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = mModel.(Model)
	if m.ThemeSearchQuery != "gree" {
		t.Errorf("Expected ThemeSearchQuery 'gree', got %q", m.ThemeSearchQuery)
	}

	// 4. Exit search with Enter
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mModel.(Model)
	if m.ThemeIsSearching {
		t.Errorf("Expected ThemeIsSearching false after Enter")
	}
}

func TestThemeModalDownloadAndApply(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	m := createTestModel()
	m.ShowThemePicker = true
	m.ThemeModalTab = 1
	m.ThemeRegistryList = []theme.RegistryTheme{
		{
			ID:          "nordic-night",
			Name:        "Nordic Night",
			Author:      "Community",
			Description: "Cool arctic palette",
			Primary:     "#88c0d0",
		},
	}
	m.ThemeRegistryCursor = 0

	// Press 'i' to install
	mModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = mModel.(Model)
	if cmd == nil {
		t.Fatalf("Expected installThemeCmd to be returned")
	}

	// Simulate ThemeInstalledMsg
	mModel, _ = m.Update(ThemeInstalledMsg{ThemeID: "nordic-night"})
	m = mModel.(Model)
	if m.ShowThemePicker {
		t.Errorf("Expected ShowThemePicker to close after install")
	}
	if m.Config.Theme != "nordic-night" {
		t.Errorf("Expected Config.Theme to be nordic-night, got %s", m.Config.Theme)
	}
}
