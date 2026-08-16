package ui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/radio"
)

func TestUpdateMessages(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	m := createTestModel()

	// 1. WindowSizeMsg
	mModel, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = mModel.(Model)
	if m.Width != 100 || m.Height != 30 {
		t.Errorf("Window size not updated: %dx%d", m.Width, m.Height)
	}

	// 2. FlashMessageMsg
	mModel, _ = m.Update(FlashMessageMsg("Testing status message"))
	m = mModel.(Model)
	if m.StatusMessage != "Testing status message" {
		t.Errorf("FlashMessageMsg not set: %s", m.StatusMessage)
	}

	// 3. RadioBrowserResultMsg with Error
	mModel, _ = m.Update(RadioBrowserResultMsg{Err: errors.New("timeout")})
	m = mModel.(Model)
	if m.StatusMessage != "RadioBrowser error: timeout" {
		t.Errorf("Expected RB error message, got: %s", m.StatusMessage)
	}

	// 4. RadioBrowserResultMsg with Stations
	rbStations := []radio.Station{
		{ID: "rb-1", Name: "RadioBrowser One", URL: "http://rb.stream/1", Genre: "Jazz"},
	}
	mModel, _ = m.Update(RadioBrowserResultMsg{Stations: rbStations})
	m = mModel.(Model)
	if len(m.RBStations) != 1 {
		t.Errorf("Expected 1 RB station loaded, got %d", len(m.RBStations))
	}

	// 5. TrackUpdatedMsg
	mModel, _ = m.Update(TrackUpdatedMsg{
		StationID:   "ambient-1",
		StationName: "Ambient One",
		TrackTitle:  "Solar Fields - Sol",
	})
	m = mModel.(Model)
	if len(m.Store.History) == 0 || m.Store.History[0].Title != "Sol" {
		t.Errorf("TrackUpdatedMsg did not record history properly")
	}

	// 6. TickMsg
	mModel, cmd := m.Update(TickMsg(time.Now()))
	m = mModel.(Model)
	if cmd == nil {
		t.Errorf("Expected non-nil tickCmd from TickMsg")
	}
}

func TestUpdateKeyboardNavigationAndShortcuts(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	m := createTestModel()

	// 1. Help modal toggle (?)
	mModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = mModel.(Model)
	if !m.ShowWhichKey {
		t.Errorf("Expected ShowWhichKey true after '?'")
	}
	// Dismiss help with esc
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mModel.(Model)
	if m.ShowWhichKey {
		t.Errorf("Expected ShowWhichKey false after Esc")
	}

	// 2. Theme picker toggle (t)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = mModel.(Model)
	if !m.ShowThemePicker {
		t.Errorf("Expected ShowThemePicker true after 't'")
	}
	// Select theme 2 (catppuccin)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = mModel.(Model)
	if m.Theme.Name != "Catppuccin Mocha" {
		t.Errorf("Expected Catppuccin theme, got %s", m.Theme.Name)
	}
	// Dismiss theme picker
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mModel.(Model)
	if m.ShowThemePicker {
		t.Errorf("Expected ShowThemePicker false after Esc")
	}

	// 3. Search query typing and backspace
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = mModel.(Model)
	if !m.IsSearching {
		t.Errorf("Expected IsSearching true after '/'")
	}
	// Type 'r' then 'o'
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = mModel.(Model)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = mModel.(Model)
	if m.SearchQuery != "ro" {
		t.Errorf("Expected SearchQuery 'ro', got %s", m.SearchQuery)
	}
	// Backspace
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = mModel.(Model)
	if m.SearchQuery != "r" {
		t.Errorf("Expected SearchQuery 'r' after backspace, got %s", m.SearchQuery)
	}
	// Exit search with esc
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mModel.(Model)
	if m.IsSearching {
		t.Errorf("Expected IsSearching false after Esc")
	}

	// 4. Volume controls (+, -) and Mute (m)
	initialVol := m.Player.Volume()
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	m = mModel.(Model)
	if m.Player.Volume() != initialVol+5 {
		t.Errorf("Volume did not increase by 5: was %d, now %d", initialVol, m.Player.Volume())
	}
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	m = mModel.(Model)
	if m.Player.Volume() != initialVol {
		t.Errorf("Volume did not decrease back to initial: %d", m.Player.Volume())
	}
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m = mModel.(Model)
	if !m.Player.IsMuted() {
		t.Errorf("Expected muted player after 'm'")
	}
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m = mModel.(Model)
	if m.Player.IsMuted() {
		t.Errorf("Expected unmuted player after second 'm'")
	}

	// 5. Visualizer mode cycle (v)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = mModel.(Model)
	if m.Visualizer == nil {
		t.Errorf("Expected visualizer to remain valid after cycle")
	}

	// 6. Favorite toggle (f)
	if len(m.Stations) > 0 {
		targetID := m.Stations[m.SelectedIndex].ID
		mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
		m = mModel.(Model)
		if !m.Store.Favorites[targetID] {
			t.Errorf("Expected station %s to be favorited", targetID)
		}
	}

	// 7. PR Export modal (p)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = mModel.(Model)
	if !m.ShowPRExport {
		t.Errorf("Expected ShowPRExport true after 'p'")
	}
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mModel.(Model)
	if m.ShowPRExport {
		t.Errorf("Expected ShowPRExport false after Esc")
	}

	// 8. Add Station modal (a)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = mModel.(Model)
	if !m.ShowAddModal {
		t.Errorf("Expected ShowAddModal true after 'a'")
	}
	// Dismiss add modal
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mModel.(Model)
	if m.ShowAddModal {
		t.Errorf("Expected ShowAddModal false after Esc")
	}

	// 9. Quit key (q)
	_, quitCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if quitCmd == nil {
		t.Errorf("Expected tea.Quit command after 'q'")
	}
}

func TestModelLifecycleAndInit(t *testing.T) {
	m := createTestModel()

	initCmd := m.Init()
	if initCmd == nil {
		t.Errorf("Expected non-nil initCmd from m.Init()")
	}

	title := m.WindowTitle()
	if title == "" {
		t.Errorf("Expected non-empty window title")
	}
}
