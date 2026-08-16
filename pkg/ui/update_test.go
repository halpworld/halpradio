package ui

import (
	"errors"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/player"
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

func TestMediaActionMessages(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	m := createTestModel()
	initialIdx := m.SelectedIndex

	// MediaNextMsg
	mModel, _ := m.Update(MediaNextMsg{})
	m = mModel.(Model)
	if len(m.Stations) > 1 && m.SelectedIndex == initialIdx {
		t.Errorf("MediaNextMsg did not advance selected station")
	}

	// MediaPrevMsg
	mModel, _ = m.Update(MediaPrevMsg{})
	m = mModel.(Model)
	if m.SelectedIndex != initialIdx {
		t.Errorf("MediaPrevMsg did not return to previous station")
	}

	// MediaVolUpMsg
	vol := m.Player.Volume()
	mModel, _ = m.Update(MediaVolUpMsg{})
	m = mModel.(Model)
	if m.Player.Volume() != vol+5 {
		t.Errorf("MediaVolUpMsg expected volume %d, got %d", vol+5, m.Player.Volume())
	}

	// MediaVolDownMsg
	mModel, _ = m.Update(MediaVolDownMsg{})
	m = mModel.(Model)
	if m.Player.Volume() != vol {
		t.Errorf("MediaVolDownMsg expected volume %d, got %d", vol, m.Player.Volume())
	}

	// MediaMuteMsg
	mModel, _ = m.Update(MediaMuteMsg{})
	m = mModel.(Model)
	if !m.Player.IsMuted() {
		t.Errorf("MediaMuteMsg expected muted")
	}

	// MediaPlayPauseMsg
	mModel, _ = m.Update(MediaPlayPauseMsg{})
	m = mModel.(Model)

	// MediaPlayMsg & MediaPauseMsg
	mModel, _ = m.Update(MediaPlayMsg{})
	m = mModel.(Model)

	mModel, _ = m.Update(MediaPauseMsg{})
	m = mModel.(Model)

	// MediaStopMsg
	mModel, _ = m.Update(MediaStopMsg{})
	m = mModel.(Model)
	if m.Player.Status() != player.StatusStopped {
		t.Errorf("MediaStopMsg expected stopped status")
	}

	// MediaRandomMsg
	mModel, _ = m.Update(MediaRandomMsg{})
	m = mModel.(Model)

	// MediaQuitMsg
	_, quitCmd := m.Update(MediaQuitMsg{})
	if quitCmd == nil {
		t.Errorf("MediaQuitMsg expected tea.Quit command")
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

	// 4. Volume controls (+, -, =) and Mute (m, 0)
	initialVol := m.Player.Volume()
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	m = mModel.(Model)
	if m.Player.Volume() != initialVol+5 {
		t.Errorf("Volume did not increase by 5: was %d, now %d", initialVol, m.Player.Volume())
	}
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'='}})
	m = mModel.(Model)
	if m.Player.Volume() != initialVol+10 {
		t.Errorf("Volume did not increase by 5 with '=': was %d, now %d", initialVol+5, m.Player.Volume())
	}
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'-'}})
	m = mModel.(Model)
	if m.Player.Volume() != initialVol+5 {
		t.Errorf("Volume did not decrease back: %d", m.Player.Volume())
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

	// 5. Next & Prev station navigation keys (n, ], N, [)
	idxBefore := m.SelectedIndex
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = mModel.(Model)
	if len(m.Stations) > 1 && m.SelectedIndex == idxBefore {
		t.Errorf("Expected selected index to advance after 'n'")
	}
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{']'}})
	m = mModel.(Model)

	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	m = mModel.(Model)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}})
	m = mModel.(Model)

	// 6. Stop key variant (x)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = mModel.(Model)
	if m.Player.Status() != player.StatusStopped {
		t.Errorf("Expected player stopped after 'x'")
	}

	// 7. Visualizer mode cycle (v)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = mModel.(Model)
	if m.Visualizer == nil {
		t.Errorf("Expected visualizer to remain valid after cycle")
	}

	// 8. Favorite toggle (f)
	if len(m.Stations) > 0 {
		targetID := m.Stations[m.SelectedIndex].ID
		mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
		m = mModel.(Model)
		if !m.Store.Favorites[targetID] {
			t.Errorf("Expected station %s to be favorited", targetID)
		}
	}

	// 9. PR Export modal (p)
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

	// 10. Add Station modal (a)
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

	// 11. Quit key (q)
	_, quitCmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if quitCmd == nil {
		t.Errorf("Expected tea.Quit command after 'q'")
	}
}

func TestDesktopSongNotificationIntegration(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	m := createTestModel()
	m.Config.SongNotifications = true

	desktopMgr := desktop.NewManager(desktop.DesktopConfig{
		NotificationsEnabled: true,
		MPRISEnabled:         false,
		IPCEnabled:           false,
	}, nil)
	defer desktopMgr.Close()

	m.SetDesktop(desktopMgr)

	// Send track updated message
	mModel, _ := m.Update(TrackUpdatedMsg{
		StationID:   "lofi-girl",
		StationName: "Lofi Girl",
		TrackTitle:  "Kupla - Kingdom in Blue",
	})
	m = mModel.(Model)

	info := m.Desktop.GetPlaybackInfo()
	if info.Track != "Kupla - Kingdom in Blue" {
		t.Errorf("Expected desktop playback track 'Kupla - Kingdom in Blue', got %q", info.Track)
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
