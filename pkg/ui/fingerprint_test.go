package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/player/fingerprint"
)

func TestIdentifyKeyBinding_InitiatesFingerprinting(t *testing.T) {
	m := createTestModel()

	// 1. When audio is stopped, pressing 'I' should display a friendly warning
	updatedModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	m = updatedModel.(Model)
	if cmd != nil {
		t.Errorf("Expected nil command when pressing 'I' while stopped, got: %v", cmd)
	}
	if !strings.Contains(m.StatusMessage, "audio is not playing") {
		t.Errorf("Expected status message about audio not playing, got: %q", m.StatusMessage)
	}

	// 2. When audio is playing, pressing 'I' should initiate fingerprinting
	st := m.Stations[0]
	_ = m.Player.Play(st)
	m.PlayingID = st.ID

	updatedModel, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}})
	m = updatedModel.(Model)

	if !m.IsIdentifying {
		t.Errorf("Expected m.IsIdentifying to be true after pressing 'I'")
	}
	if cmd == nil {
		t.Errorf("Expected non-nil cmd after pressing 'I'")
	}
	if !strings.Contains(m.StatusMessage, "identifying with AcoustID") {
		t.Errorf("Expected status message mentioning AcoustID, got: %q", m.StatusMessage)
	}
}

func TestTrackIdentifiedMsg_UpdatesModelAndHistory(t *testing.T) {
	m := createTestModel()
	st := m.Stations[0]
	_ = m.Player.Play(st)
	m.PlayingID = st.ID
	m.IsIdentifying = true

	res := &fingerprint.Result{
		Artist:      "Daft Punk",
		Title:       "Voyager",
		Album:       "Discovery",
		Year:        2001,
		Confidence:  0.94,
		Source:      "AcoustID",
		StationID:   st.ID,
		StationName: st.Name,
	}

	msg := TrackIdentifiedMsg{
		StationID:   st.ID,
		StationName: st.Name,
		Result:      res,
	}

	updatedModel, _ := m.Update(msg)
	m = updatedModel.(Model)

	if m.IsIdentifying {
		t.Errorf("Expected m.IsIdentifying to be false after TrackIdentifiedMsg")
	}
	if m.IdentifiedResult == nil || m.IdentifiedResult.Artist != "Daft Punk" {
		t.Fatalf("Expected IdentifiedResult to be set to Daft Punk, got: %+v", m.IdentifiedResult)
	}

	// Verify History contains enriched entry
	hist := m.Store.GetHistory()
	if len(hist) == 0 {
		t.Fatalf("Expected history entry to be added")
	}
	top := hist[0]
	if top.Artist != "Daft Punk" || top.Title != "Voyager" || top.Album != "Discovery" || top.Year != 2001 {
		t.Errorf("Unexpected history entry metadata: %+v", top)
	}

	// Verify WindowTitle reflects identified track
	title := m.WindowTitle()
	if !strings.Contains(title, "Daft Punk - Voyager") {
		t.Errorf("Expected WindowTitle to include identified track, got: %q", title)
	}

	// Verify StatusMessage reflects match
	if !strings.Contains(m.StatusMessage, "✨ Identified") || !strings.Contains(m.StatusMessage, "94%") {
		t.Errorf("Expected StatusMessage with match, got: %q", m.StatusMessage)
	}
}

func TestTrackIdentifiedMsg_FailureGracefulFallback(t *testing.T) {
	m := createTestModel()
	st := m.Stations[0]
	_ = m.Player.Play(st)
	m.PlayingID = st.ID
	m.IsIdentifying = true

	msg := TrackIdentifiedMsg{
		StationID:   st.ID,
		StationName: st.Name,
		Err:         errors.New("connection timed out"),
	}

	updatedModel, _ := m.Update(msg)
	m = updatedModel.(Model)

	if m.IsIdentifying {
		t.Errorf("Expected m.IsIdentifying to be false after failed TrackIdentifiedMsg")
	}
	if m.IdentifiedResult != nil {
		t.Errorf("Expected IdentifiedResult to remain nil on failure")
	}
	if !strings.Contains(m.StatusMessage, "No acoustic match found") {
		t.Errorf("Expected friendly fallback status message, got: %q", m.StatusMessage)
	}
	if m.Player.Status() != player.StatusPlaying {
		t.Errorf("Expected player to remain playing, got: %s", m.Player.Status())
	}
}

func TestYankIdentifiedTrack(t *testing.T) {
	m := createTestModel()
	st := m.Stations[0]
	_ = m.Player.Play(st)
	m.PlayingID = st.ID
	m.IdentifiedResult = &fingerprint.Result{
		Artist:     "Daft Punk",
		Title:      "Voyager",
		Confidence: 0.94,
		Source:     "AcoustID",
	}

	// Press 'y' to yank
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updatedModel.(Model)

	if !strings.Contains(m.StatusMessage, "Daft Punk - Voyager") {
		t.Errorf("Expected status message copying Daft Punk - Voyager, got: %q", m.StatusMessage)
	}
}

func TestAutoIdentifyInactivityTrigger(t *testing.T) {
	m := createTestModel()
	st := m.Stations[0]
	_ = m.Player.Play(st)
	m.PlayingID = st.ID

	// Stream playing with generic/empty metadata for > 20s
	m.PlaybackStartTime = time.Now().Add(-25 * time.Second)
	m.LastFingerprintTime = time.Time{}
	m.IsIdentifying = false
	m.IdentifiedResult = nil

	// TickMsg triggers auto-identification
	updatedModel, cmd := m.Update(TickMsg(time.Now()))
	m = updatedModel.(Model)

	if !m.IsIdentifying {
		t.Errorf("Expected m.IsIdentifying to be true after 20s of missing metadata")
	}
	if cmd == nil {
		t.Errorf("Expected auto-identify cmd to be dispatched")
	}
	if !strings.Contains(m.StatusMessage, "Auto-fingerprinting") {
		t.Errorf("Expected status message mentioning Auto-fingerprinting, got: %q", m.StatusMessage)
	}
}
