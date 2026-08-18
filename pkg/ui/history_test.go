package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/player"
)

func TestHistoryTabSwitching(t *testing.T) {
	m := createTestModel()

	// Switch via key '7'
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'7'}})
	m = updatedModel.(Model)
	if m.ActiveTab != 6 {
		t.Fatalf("Expected ActiveTab to be 6 after pressing '7', got %d", m.ActiveTab)
	}
	if !strings.Contains(m.WindowTitle(), "7: History") {
		t.Errorf("Expected WindowTitle to contain '7: History', got %q", m.WindowTitle())
	}

	// Switch to catalog
	m.SwitchTab(0)
	if m.ActiveTab != 0 {
		t.Fatalf("Expected ActiveTab 0, got %d", m.ActiveTab)
	}

	// Switch via key 'H'
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'H'}})
	m = updatedModel.(Model)
	if m.ActiveTab != 6 {
		t.Fatalf("Expected ActiveTab to be 6 after pressing 'H', got %d", m.ActiveTab)
	}
}

func TestHistoryRecordingOnTrackUpdatedMsg(t *testing.T) {
	m := createTestModel()
	st := m.Stations[0] // ambient-1
	_ = m.Player.Play(st)
	m.PlayingID = st.ID

	// Dispatch TrackUpdatedMsg
	msg := TrackUpdatedMsg(player.TrackInfo{
		StationID:   "ambient-1",
		StationName: "Ambient One",
		TrackTitle:  "Tycho - A Walk",
	})

	updatedModel, _ := m.Update(msg)
	m = updatedModel.(Model)

	hist := m.Store.GetHistory()
	if len(hist) != 1 {
		t.Fatalf("Expected 1 history item recorded, got %d", len(hist))
	}
	if hist[0].TrackTitle != "Tycho - A Walk" {
		t.Errorf("Expected track title 'Tycho - A Walk', got %q", hist[0].TrackTitle)
	}
	if hist[0].Artist != "Tycho" || hist[0].Title != "A Walk" {
		t.Errorf("Expected parsed artist and title, got %+v", hist[0])
	}
}

func TestHistoryAndNotificationsIgnoredWhenStoppedOrMismatched(t *testing.T) {
	m := createTestModel()

	// 1. When stopped: TrackUpdatedMsg should be ignored
	msg := TrackUpdatedMsg(player.TrackInfo{
		StationID:   "ambient-1",
		StationName: "Ambient One",
		TrackTitle:  "Tycho - A Walk",
	})
	updatedModel, _ := m.Update(msg)
	m = updatedModel.(Model)
	if len(m.Store.GetHistory()) != 0 {
		t.Errorf("Expected 0 history items when stopped, got %d", len(m.Store.GetHistory()))
	}

	// 2. When playing station 1, but msg arrives for station 2: should be ignored
	st := m.Stations[0] // ambient-1
	_ = m.Player.Play(st)
	m.PlayingID = st.ID

	msgOldStation := TrackUpdatedMsg(player.TrackInfo{
		StationID:   "ambient-2",
		StationName: "Ambient Two",
		TrackTitle:  "Different Artist - Old Song",
	})
	updatedModel, _ = m.Update(msgOldStation)
	m = updatedModel.(Model)
	if len(m.Store.GetHistory()) != 0 {
		t.Errorf("Expected 0 history items for mismatched station, got %d", len(m.Store.GetHistory()))
	}

	// 3. When paused: TrackUpdatedMsg should be ignored
	_ = m.Player.Pause()
	msgPaused := TrackUpdatedMsg(player.TrackInfo{
		StationID:   "ambient-1",
		StationName: "Ambient One",
		TrackTitle:  "Paused Artist - Ignored Song",
	})
	updatedModel, _ = m.Update(msgPaused)
	m = updatedModel.(Model)
	if len(m.Store.GetHistory()) != 0 {
		t.Errorf("Expected 0 history items when paused, got %d", len(m.Store.GetHistory()))
	}
}

func TestHistoryNavigationAndYank(t *testing.T) {
	m := createTestModel()
	m.Store.AddHistory("ambient-1", "Ambient One", "Tycho - A Walk")
	m.Store.AddHistory("rock-1", "Rock Heavy", "Boards of Canada - Dayvan Cowboy")

	m.SwitchTab(6)
	if m.HistoryIndex != 0 {
		t.Fatalf("Expected HistoryIndex 0, got %d", m.HistoryIndex)
	}

	// Navigate down with 'j'
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updatedModel.(Model)
	if m.HistoryIndex != 1 {
		t.Fatalf("Expected HistoryIndex 1 after 'j', got %d", m.HistoryIndex)
	}

	// Yank track with 'y'
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updatedModel.(Model)

	if !strings.Contains(m.StatusMessage, "Copied 'Tycho - A Walk' to clipboard") {
		t.Errorf("Expected status message to confirm clipboard yank, got %q", m.StatusMessage)
	}
}

func TestHistoryBookmarkAction(t *testing.T) {
	m := createTestModel()
	m.Store.AddHistory("ambient-1", "Ambient One", "Tycho - A Walk")
	m.SwitchTab(6)

	// Press 's' to bookmark track
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updatedModel.(Model)

	if !strings.Contains(m.StatusMessage, "Bookmarked 'Tycho - A Walk'") {
		t.Errorf("Expected status message to confirm track bookmark, got %q", m.StatusMessage)
	}

	bookmarks, err := m.Store.GetBookmarkedTracks()
	if err != nil {
		t.Fatalf("Failed getting bookmarks: %v", err)
	}
	if len(bookmarks) == 0 {
		t.Errorf("Expected at least 1 saved bookmark in file")
	}
}

func TestHistoryClearAction(t *testing.T) {
	m := createTestModel()
	m.Store.AddHistory("ambient-1", "Ambient One", "Tycho - A Walk")
	m.Store.AddHistory("rock-1", "Rock Heavy", "Boards of Canada - Dayvan Cowboy")
	m.SwitchTab(6)

	if len(m.Store.GetHistory()) != 2 {
		t.Fatalf("Expected 2 history items")
	}

	// Press 'c' to clear history
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updatedModel.(Model)

	if len(m.Store.GetHistory()) != 0 {
		t.Fatalf("Expected history to be empty after 'c', got %d items", len(m.Store.GetHistory()))
	}
	if m.StatusMessage != "Cleared track history log" {
		t.Errorf("Expected 'Cleared track history log' status message, got %q", m.StatusMessage)
	}
}

func TestHistoryTuneInAction(t *testing.T) {
	m := createTestModel()
	m.Store.AddHistory("ambient-1", "Ambient One", "Tycho - A Walk")
	m.SwitchTab(6)

	// Press Enter on history item
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updatedModel.(Model)

	if m.PlayingID != "ambient-1" {
		t.Errorf("Expected PlayingID to be 'ambient-1', got %q", m.PlayingID)
	}
	if !strings.Contains(m.StatusMessage, "Playing Ambient One") {
		t.Errorf("Expected status message to confirm playing Ambient One, got %q", m.StatusMessage)
	}
}

func TestActiveTrackYankAndSearchOnCatalog(t *testing.T) {
	m := createTestModel()
	m.SwitchTab(0)
	m.SelectedIndex = 2 // "Rock Heavy"

	// Press 'y' on catalog item
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updatedModel.(Model)
	if !strings.Contains(m.StatusMessage, "Copied 'Rock Heavy' to clipboard") {
		t.Errorf("Expected status message for catalog item copy, got %q", m.StatusMessage)
	}

	// Press 'o' on catalog item
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	m = updatedModel.(Model)
	if !strings.Contains(m.StatusMessage, "Opening search for 'Rock Heavy'") {
		t.Errorf("Expected status message for search open, got %q", m.StatusMessage)
	}
}
