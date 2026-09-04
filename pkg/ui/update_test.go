package ui

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
	"github.com/halpworld/halpradio/pkg/util"
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

	// 4b. CatalogUpdatedMsg
	mModel, _ = m.Update(CatalogUpdatedMsg{Updated: true, StationsCount: 42})
	m = mModel.(Model)
	if m.StatusMessage != "Station catalog updated (42 stations)" {
		t.Errorf("Expected catalog update message, got: %s", m.StatusMessage)
	}

	// 5. TrackUpdatedMsg (when playing)
	_ = m.Player.Play(m.Stations[0])
	m.PlayingID = "ambient-1"
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

func TestAutoPauseMessage(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	m := createTestModel()
	m.PlayingID = "some-station"
	mModel, _ := m.Update(AutoPauseMsg{})
	m = mModel.(Model)
	if m.PlayingID != "" {
		t.Errorf("Expected PlayingID cleared on AutoPauseMsg")
	}
	if m.StatusMessage != "Paused - headphones disconnected" {
		t.Errorf("Unexpected status message: %s", m.StatusMessage)
	}
}

func TestUpdateKeyboardNavigationAndShortcuts(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	cleanupClip := util.SetClipboardWriterForTesting(func(text string) error {
		return nil
	})
	defer cleanupClip()

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

	var mu sync.Mutex
	var notifiedArgs []string
	mockRunner := func(ctx context.Context, name string, args ...string) error {
		mu.Lock()
		defer mu.Unlock()
		notifiedArgs = args
		return nil
	}

	desktopMgr := desktop.NewManager(desktop.DesktopConfig{
		NotificationsEnabled: true,
		MPRISEnabled:         false,
		IPCEnabled:           false,
		Runner:               mockRunner,
	}, nil)
	defer desktopMgr.Close()

	m.SetDesktop(desktopMgr)
	st := radio.Station{ID: "lofi-girl", Name: "Lofi Girl", URL: "http://stream.lofigirl.com"}
	_ = m.Player.Play(st)
	m.PlayingID = "lofi-girl"
	m.SyncDesktop()

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

	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(notifiedArgs) == 0 {
		t.Errorf("Expected desktop notification runner to be invoked")
	}
	mu.Unlock()
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

func TestModalsKeyboardHandling(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	m := createTestModel()

	// 1. Add Station Modal
	m.ShowAddModal = true
	m.AddFocusIdx = 0
	m.AddInputs = []string{"My Custom Station", "https://stream.example.com", "Electronic", "US", "New York", "FM", "128"}

	// Test Tab to cycle inputs
	mModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mModel.(Model)
	if m.AddFocusIdx != 1 {
		t.Errorf("expected AddFocusIdx = 1 after Tab, got %d", m.AddFocusIdx)
	}

	// Test Enter to submit
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mModel.(Model)
	if m.ShowAddModal {
		t.Errorf("expected ShowAddModal = false after successful submission")
	}

	// Test Escape on Add Modal
	m.ShowAddModal = true
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mModel.(Model)
	if m.ShowAddModal {
		t.Errorf("expected ShowAddModal = false after Escape")
	}

	// 2. Permission Approval Modal
	m.ShowPermissionApproval = true
	m.ApprovalPlugin = plugin.PluginInfo{
		Manifest: plugin.Manifest{ID: "test-plugin"},
	}

	// Press 'y' to approve
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = mModel.(Model)
	if m.ShowPermissionApproval {
		t.Errorf("expected ShowPermissionApproval = false after 'y'")
	}

	// 3. Plugin Modal
	m.ShowPluginModal = true
	m.PluginModalTab = 0 // Installed tab

	// Test Tab to switch plugin tab
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mModel.(Model)
	if m.PluginModalTab != 1 {
		t.Errorf("expected PluginModalTab = 1 after Tab, got %d", m.PluginModalTab)
	}

	// Test Esc to close plugin modal
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mModel.(Model)
	if m.ShowPluginModal {
		t.Errorf("expected ShowPluginModal = false after Esc")
	}
}

func TestCatalogUpdateHandling(t *testing.T) {
	m := createTestModel()

	// 1. CatalogUpdatedMsg updated
	mModel, _ := m.Update(CatalogUpdatedMsg{Updated: true, StationsCount: 250})
	m = mModel.(Model)
	if !strings.Contains(m.StatusMessage, "Station catalog updated") {
		t.Errorf("expected update status message, got %s", m.StatusMessage)
	}

	// 2. Plugin flash and notification messages
	mModel, _ = m.Update(PluginFlashMsg("Plugin flash message"))
	m = mModel.(Model)
	if m.StatusMessage != "Plugin flash message" {
		t.Errorf("expected flash message, got %s", m.StatusMessage)
	}

	mModel, _ = m.Update(PluginNotificationMsg{Title: "PTitle", Message: "PBody"})
	m = mModel.(Model)
	if !strings.Contains(m.StatusMessage, "PTitle") {
		t.Errorf("expected notification title in status, got %s", m.StatusMessage)
	}
}

func TestThemePickerNavigationAndExport(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	m := createTestModel()

	// Open theme picker
	mModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = mModel.(Model)
	if !m.ShowThemePicker {
		t.Fatalf("Expected ShowThemePicker true")
	}

	// Move cursor down (j)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = mModel.(Model)
	if m.ThemeCursor != 1 {
		t.Errorf("Expected ThemeCursor 1 after 'j', got %d", m.ThemeCursor)
	}

	// Move cursor down (down arrow)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = mModel.(Model)
	if m.ThemeCursor != 2 {
		t.Errorf("Expected ThemeCursor 2 after down arrow, got %d", m.ThemeCursor)
	}

	// Move cursor up (k)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = mModel.(Model)
	if m.ThemeCursor != 1 {
		t.Errorf("Expected ThemeCursor 1 after 'k', got %d", m.ThemeCursor)
	}

	// Jump to end (G)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	m = mModel.(Model)
	allThemes := theme.GetAllThemes()
	if m.ThemeCursor != len(allThemes)-1 {
		t.Errorf("Expected ThemeCursor at end (%d), got %d", len(allThemes)-1, m.ThemeCursor)
	}

	// Jump to top (g)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	m = mModel.(Model)
	if m.ThemeCursor != 0 {
		t.Errorf("Expected ThemeCursor 0 after 'g', got %d", m.ThemeCursor)
	}

	// Export active theme (E)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'E'}})
	m = mModel.(Model)
	if !strings.Contains(m.StatusMessage, "Exported theme") {
		t.Errorf("Expected export status message, got: %s", m.StatusMessage)
	}

	// Press Enter on cursor 0 (tokyonight)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mModel.(Model)
	if m.ShowThemePicker {
		t.Errorf("Expected ShowThemePicker false after Enter")
	}
	if m.Theme.Name != "Tokyo Night" {
		t.Errorf("Expected Tokyo Night theme, got %s", m.Theme.Name)
	}

	// Reopen and press 4 (Nord)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}})
	m = mModel.(Model)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'4'}})
	m = mModel.(Model)
	if m.ShowThemePicker {
		t.Errorf("Expected ShowThemePicker false after pressing 4")
	}
	if m.Theme.Name != "Nord" {
		t.Errorf("Expected Nord theme, got %s", m.Theme.Name)
	}
}

func TestGlobeAndTunerPlaybackAndInteraction(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	m := createTestModel()

	// Switch to Tab 8 (Globe)
	mModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}})
	m = mModel.(Model)

	// Set globe coordinates near US cluster (Washington DC / US fallback)
	m.GlobeLat = 38.8951
	m.GlobeLon = -77.0364
	m.GlobeStationIndex = 0

	// Play station at target via Enter
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mModel.(Model)
	if m.PlayingID == "" {
		t.Errorf("expected station to play after pressing Enter on Globe")
	}

	// Toggle pause on same station
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mModel.(Model)
	if m.PlayingID != "" {
		t.Errorf("expected station to pause on repeat Enter")
	}

	// Cycle stations in cluster
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = mModel.(Model)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	m = mModel.(Model)

	// By default (ExperimentalTuner == false), 'F' should NOT activate tuner
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	m = mModel.(Model)
	if m.ActiveTuner {
		t.Errorf("expected ActiveTuner=false when ExperimentalTuner is disabled")
	}

	// Enable ExperimentalTuner to test experimental tuner playback and interaction
	m.Config.ExperimentalTuner = true

	// Switch to Tuner mode (F)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	m = mModel.(Model)
	if !m.ActiveTuner {
		t.Fatalf("expected ActiveTuner=true")
	}

	// Sweep to exact frequency and tune in
	m.TunerFreq = 90.3
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mModel.(Model)

	// Test sweeping dial with 'l' and 'h'
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = mModel.(Model)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	m = mModel.(Model)

	// Seek next station on dial (n)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = mModel.(Model)

	// Seek prev station on dial (N)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	m = mModel.(Model)

	// Switch band (b)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = mModel.(Model)
	if m.TunerBand != "AM" {
		t.Errorf("expected AM band")
	}

	// Verify auto-play on AM band frequency (720 AM has NPR News)
	if m.PlayingID == "" {
		t.Errorf("expected automatic analog playback of station at 720 AM")
	}

	// Verify WindowTitle reflects Tuner mode
	title := m.WindowTitle()
	if !strings.Contains(title, "Tuner") {
		t.Errorf("expected window title to contain 'Tuner', got %q", title)
	}
}

func TestSearchInputAndClearFlow(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	m := createTestModel()

	// Open search bar (/)
	mModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = mModel.(Model)
	if !m.IsSearching {
		t.Fatalf("expected IsSearching=true")
	}

	// Type query "ambient"
	for _, r := range "ambient" {
		mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mModel.(Model)
	}
	if m.SearchQuery != "ambient" {
		t.Errorf("expected query 'ambient', got %q", m.SearchQuery)
	}

	// Backspace
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = mModel.(Model)
	if m.SearchQuery != "ambien" {
		t.Errorf("expected 'ambien' after backspace, got %q", m.SearchQuery)
	}

	// First Escape exits search input mode
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mModel.(Model)
	if m.IsSearching {
		t.Errorf("expected IsSearching=false after first Esc")
	}

	// Second Escape clears search query
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mModel.(Model)
	if m.SearchQuery != "" {
		t.Errorf("expected SearchQuery cleared after second Esc")
	}
}

func TestAddStationModalFlow(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	m := createTestModel()

	// Open add station modal (a)
	mModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = mModel.(Model)
	if !m.ShowAddModal {
		t.Fatalf("expected ShowAddModal=true")
	}

	// Tab through fields
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mModel.(Model)
	if m.AddFocusIdx != 1 {
		t.Errorf("expected AddFocusIdx=1 after Tab, got %d", m.AddFocusIdx)
	}

	// Shift+Tab back
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = mModel.(Model)
	if m.AddFocusIdx != 0 {
		t.Errorf("expected AddFocusIdx=0 after Shift+Tab, got %d", m.AddFocusIdx)
	}

	// Close modal with Esc
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mModel.(Model)
	if m.ShowAddModal {
		t.Errorf("expected ShowAddModal=false after Esc")
	}
}
