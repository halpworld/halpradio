package ui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/ai"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
)

func TestLinerNotesDrawerToggleScrollAndClose(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("XDG_CACHE_HOME", tempDir)

	m := createTestModel()
	m.Width = 120
	m.Height = 36

	st := radio.Station{
		ID:      "somafm_groovesalad",
		Name:    "SomaFM Groove Salad",
		URL:     "https://ice1.somafm.com/groovesalad-256-mp3",
		Genre:   "Ambient",
		Country: "US",
		Bitrate: 256,
		Codec:   "MP3",
	}
	_ = m.Player.Play(st)
	m.PlayingID = st.ID
	if mp, ok := m.Player.(*player.MockPlayer); ok {
		mp.SetTrack("Tycho - Awake")
	}

	// 1. Press 'i' to open Liner Notes drawer
	mModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	m = mModel.(Model)
	if !m.ShowLinerNotes {
		t.Fatalf("expected ShowLinerNotes=true after pressing 'i'")
	}
	if m.ActiveFocus != FocusLinerNotes {
		t.Errorf("expected ActiveFocus=FocusLinerNotes, got %v", m.ActiveFocus)
	}
	if cmd == nil {
		t.Errorf("expected async fetchLinerNotesCmd when opening Liner Notes drawer")
	}

	// Deliver rich LinerNotesFetchedMsg
	richNotes := &ai.LinerNotes{
		Artist:      "Tycho",
		Title:       "Awake",
		Album:       "Awake",
		ReleaseYear: "2014",
		Country:     "US",
		OriginCity:  "San Francisco",
		Genre:       "Ambient / Post-Rock",
		Label:       "Ghostly International",
		Biography:   "Scott Hansen, known professionally as Tycho, is an American ambient music producer and composer.",
		AlbumNotes:  "Recorded in San Francisco with a full live band arrangement.",
		Discography: []ai.DiscographyItem{
			{Title: "Dive", Year: "2011", Type: "Album"},
			{Title: "Awake", Year: "2014", Type: "Album"},
			{Title: "Epoch", Year: "2016", Type: "Album"},
		},
		TrackTrivia: []string{
			"Broadcast on SomaFM Groove Salad in 256 kbps MP3.",
			"Marked Tycho's transition into a live three-piece touring ensemble.",
		},
		Source: "MusicBrainz + Wikipedia",
	}
	mModel, _ = m.Update(LinerNotesFetchedMsg{
		StationID: st.ID,
		TrackKey:  m.LinerNotesTrackKey,
		Notes:     richNotes,
	})
	m = mModel.(Model)

	view := m.View()
	for _, want := range []string{"LINER NOTES & ARTIST BIO", "Tycho", "Awake", "San Francisco"} {
		if !strings.Contains(view, want) {
			t.Errorf("expected view with open Liner Notes drawer to contain %q, got:\n%s", want, view)
		}
	}

	// 2. Scroll down with 'j' and up with 'k'
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = mModel.(Model)
	if m.LinerNotesScroll != 1 {
		t.Errorf("expected LinerNotesScroll=1 after 'j', got %d", m.LinerNotesScroll)
	}

	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = mModel.(Model)
	if m.LinerNotesScroll != 0 {
		t.Errorf("expected LinerNotesScroll=0 after 'k', got %d", m.LinerNotesScroll)
	}

	// 3. Close with 'Esc'
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mModel.(Model)
	if m.ShowLinerNotes {
		t.Errorf("expected ShowLinerNotes=false after Esc")
	}
	if m.ActiveFocus != FocusMainList {
		t.Errorf("expected ActiveFocus=FocusMainList after closing drawer, got %v", m.ActiveFocus)
	}
}

func TestOnDemandAIDJCommentaryDuckingAndUnducking(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("XDG_CACHE_HOME", tempDir)

	m := createTestModel()
	m.Width = 110
	m.Height = 32
	m.Config.AIDJPersona = "late-night-chill"
	m.Config.AIDJTTSEngine = "silent"
	m.Config.AIDJDuckLevel = 20

	st := radio.Station{
		ID:      "somafm_groovesalad",
		Name:    "SomaFM Groove Salad",
		URL:     "https://ice1.somafm.com/groovesalad-256-mp3",
		Genre:   "Ambient",
		Country: "US",
	}
	_ = m.Player.Play(st)
	m.PlayingID = st.ID
	if mp, ok := m.Player.(*player.MockPlayer); ok {
		mp.SetTrack("Boards of Canada - Roygbiv")
	}

	if m.Player.DuckFactor() != 1.0 {
		t.Fatalf("expected initial DuckFactor=1.0, got %.2f", m.Player.DuckFactor())
	}

	// Press 'd' to trigger on-demand AI DJ commentary
	mModel, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = mModel.(Model)

	if !m.DJBannerActive {
		t.Fatalf("expected DJBannerActive=true immediately after pressing 'd'")
	}
	if m.Player.DuckFactor() >= 1.0 {
		t.Errorf("expected audio to be ducked (< 1.0) during DJ announcement, got %.2f", m.Player.DuckFactor())
	}
	if m.DJBannerScript == "" {
		t.Errorf("expected non-empty DJBannerScript")
	}
	if cmd == nil {
		t.Errorf("expected non-nil tea.Cmd from DJ announcement")
	}

	// Verify DJ Banner is rendered in View()
	view := m.View()
	for _, want := range []string{"LOCAL AI RADIO DJ", "Late Night Chill", "Music Ducked: 20%"} {
		if !strings.Contains(view, want) {
			t.Errorf("expected View() to contain DJ banner text %q, got:\n%s", want, view)
		}
	}

	// Complete speech and verify audio unducks back to 1.0
	mModel, _ = m.Update(DJSpeechFinishedMsg{
		TrackKey: "Boards of Canada - Roygbiv",
		Script:   m.DJBannerScript,
	})
	m = mModel.(Model)
	if m.Player.DuckFactor() != 1.0 {
		t.Errorf("expected DuckFactor to return to 1.0 after DJSpeechFinishedMsg, got %.2f", m.Player.DuckFactor())
	}
	if !m.DJBannerFading {
		t.Errorf("expected DJBannerFading=true right after speech finishes")
	}

	// Complete unduck delay and verify banner closes
	mModel, _ = m.Update(DJUnduckFinishedMsg{Script: m.DJBannerScript})
	m = mModel.(Model)
	if m.DJBannerActive {
		t.Errorf("expected DJBannerActive=false after DJUnduckFinishedMsg")
	}
}

func TestRapidDJPressesIgnoreStaleMessages(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("XDG_CACHE_HOME", tempDir)

	m := createTestModel()
	m.Width = 110
	m.Height = 32
	m.Config.AIDJPersona = "late-night-chill"
	m.Config.AIDJTTSEngine = "silent"
	m.Config.AIDJDuckLevel = 20

	st := radio.Station{
		ID:      "somafm_groovesalad",
		Name:    "SomaFM Groove Salad",
		URL:     "https://ice1.somafm.com/groovesalad-256-mp3",
		Genre:   "Ambient",
		Country: "US",
	}
	_ = m.Player.Play(st)
	m.PlayingID = st.ID
	if mp, ok := m.Player.(*player.MockPlayer); ok {
		mp.SetTrack("Tycho - Awake")
	}

	// First 'd' press
	mModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = mModel.(Model)
	firstSeq := m.DJAnnounceSeq
	firstScript := m.DJBannerScript

	// Second 'd' press immediately while first is still active
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = mModel.(Model)
	secondSeq := m.DJAnnounceSeq
	if secondSeq <= firstSeq {
		t.Fatalf("expected DJAnnounceSeq to increment on second 'd' press (%d -> %d)", firstSeq, secondSeq)
	}

	// Stale DJSpeechFinishedMsg from first press must NOT unduck or set fading on second announcement
	mModel, _ = m.Update(DJSpeechFinishedMsg{
		Seq:      firstSeq,
		TrackKey: "Tycho - Awake",
		Script:   firstScript,
	})
	m = mModel.(Model)
	if m.Player.DuckFactor() >= 1.0 {
		t.Errorf("expected stale DJSpeechFinishedMsg to be ignored and audio to remain ducked, got %.2f", m.Player.DuckFactor())
	}
	if m.DJBannerFading {
		t.Errorf("expected stale DJSpeechFinishedMsg not to mark active banner as fading")
	}

	// Stale DJUnduckFinishedMsg from first press must NOT close the active banner
	mModel, _ = m.Update(DJUnduckFinishedMsg{
		Seq:    firstSeq,
		Script: firstScript,
	})
	m = mModel.(Model)
	if !m.DJBannerActive {
		t.Errorf("expected stale DJUnduckFinishedMsg not to close active DJ banner")
	}

	// Current sequence DJSpeechFinishedMsg and DJUnduckFinishedMsg should succeed
	mModel, _ = m.Update(DJSpeechFinishedMsg{
		Seq:      secondSeq,
		TrackKey: "Tycho - Awake",
		Script:   m.DJBannerScript,
	})
	m = mModel.(Model)
	if m.Player.DuckFactor() != 1.0 {
		t.Errorf("expected current sequence DJSpeechFinishedMsg to unduck audio to 1.0, got %.2f", m.Player.DuckFactor())
	}

	mModel, _ = m.Update(DJUnduckFinishedMsg{
		Seq:    secondSeq,
		Script: m.DJBannerScript,
	})
	m = mModel.(Model)
	if m.DJBannerActive {
		t.Errorf("expected current sequence DJUnduckFinishedMsg to close banner")
	}
}

func TestStopPauseAndStationSwitchCancelActiveDJ(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("XDG_CACHE_HOME", tempDir)

	m := createTestModel()
	m.Width = 110
	m.Height = 32
	m.ActiveFocus = FocusMainList
	m.Config.AIDJDuckLevel = 20

	st := m.Stations[0]
	_ = m.Player.Play(st)
	m.PlayingID = st.ID

	// Trigger DJ announcement, then Pause with Space
	mModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = mModel.(Model)
	if !m.DJBannerActive || m.Player.DuckFactor() >= 1.0 {
		t.Fatalf("expected DJ banner active and audio ducked")
	}

	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mModel.(Model)
	if m.DJBannerActive {
		t.Errorf("expected DJBannerActive=false after pausing playback")
	}
	if m.Player.DuckFactor() != 1.0 {
		t.Errorf("expected DuckFactor=1.0 after pausing playback, got %.2f", m.Player.DuckFactor())
	}

	// Resume, trigger DJ announcement, then switch to next station ('n')
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mModel.(Model)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = mModel.(Model)
	if !m.DJBannerActive {
		t.Fatalf("expected DJBannerActive=true before station switch")
	}

	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = mModel.(Model)
	if m.DJBannerActive {
		t.Errorf("expected DJBannerActive=false after switching stations")
	}
	if m.Player.DuckFactor() != 1.0 {
		t.Errorf("expected DuckFactor=1.0 after switching stations, got %.2f", m.Player.DuckFactor())
	}

	// Trigger DJ announcement, then Stop ('s')
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = mModel.(Model)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = mModel.(Model)
	if m.DJBannerActive {
		t.Errorf("expected DJBannerActive=false after stopping playback")
	}
	if m.Player.DuckFactor() != 1.0 {
		t.Errorf("expected DuckFactor=1.0 after stopping playback, got %.2f", m.Player.DuckFactor())
	}
}

func TestAIDJStudioModalInteraction(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("XDG_CACHE_HOME", tempDir)

	m := createTestModel()
	m.Width = 100
	m.Height = 30

	// 1. Press 'D' to open AI DJ Studio modal
	mModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'D'}})
	m = mModel.(Model)
	if !m.ShowDJModal {
		t.Fatalf("expected ShowDJModal=true after pressing 'D'")
	}

	view := m.View()
	if !strings.Contains(view, "LOCAL AI RADIO DJ STUDIO & ONNX VOICES") {
		t.Errorf("expected DJ Studio modal title in View(), got:\n%s", view)
	}

	// 2. Cursor 0: Toggle Auto AI Radio DJ on
	if m.Config.AIDJEnabled {
		t.Fatalf("expected AIDJEnabled=false by default")
	}
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mModel.(Model)
	if !m.Config.AIDJEnabled {
		t.Errorf("expected AIDJEnabled=true after pressing Enter on row 0")
	}

	// 3. Move down to Cursor 1 (DJ Persona) and cycle personas
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = mModel.(Model)
	if m.DJModalCursor != 1 {
		t.Fatalf("expected DJModalCursor=1, got %d", m.DJModalCursor)
	}
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mModel.(Model)
	if m.Config.AIDJPersona != "retro-80s" {
		t.Errorf("expected AIDJPersona='retro-80s' after cycling from late-night-chill, got %q", m.Config.AIDJPersona)
	}

	// 4. Move down to Cursor 2 (TTS Engine Mode) and cycle engine
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = mModel.(Model)
	if m.DJModalCursor != 2 {
		t.Fatalf("expected DJModalCursor=2, got %d", m.DJModalCursor)
	}
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mModel.(Model)
	if m.Config.AIDJTTSEngine != "piper" {
		t.Errorf("expected AIDJTTSEngine='piper' after cycling from auto, got %q", m.Config.AIDJTTSEngine)
	}

	// 5. Move down to Cursor 3 (first ONNX voice model) and select it
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = mModel.(Model)
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mModel.(Model)
	if m.Config.AIDJVoiceModel == "" {
		t.Errorf("expected AIDJVoiceModel to be set")
	}

	// 6. Close modal with Esc
	mModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mModel.(Model)
	if m.ShowDJModal {
		t.Errorf("expected ShowDJModal=false after Esc")
	}
}

func TestAutoAIDJTriggersOnTrackChange(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("XDG_CACHE_HOME", tempDir)

	m := createTestModel()
	m.Width = 100
	m.Height = 30
	m.Config.AIDJEnabled = true
	m.Config.AIDJPersona = "cyberpunk"
	m.Config.AIDJTTSEngine = "silent"

	st := radio.Station{
		ID:      "nightride_fm",
		Name:    "Nightride FM",
		URL:     "https://stream.nightride.fm/nightride.m4a",
		Genre:   "Synthwave",
		Country: "US",
	}
	_ = m.Player.Play(st)
	m.PlayingID = st.ID
	if mp, ok := m.Player.(*player.MockPlayer); ok {
		mp.SetTrack("Kavinsky - Nightcall")
	}

	mModel, cmd := m.Update(TrackUpdatedMsg{
		StationID:   st.ID,
		StationName: st.Name,
		TrackTitle:  "Kavinsky - Nightcall",
	})
	m = mModel.(Model)

	if !m.DJBannerActive {
		t.Fatalf("expected auto AI DJ to activate banner on track update when AIDJEnabled=true")
	}
	if m.Player.DuckFactor() >= 1.0 {
		t.Errorf("expected audio ducking (< 1.0) on auto AI DJ intro, got %.2f", m.Player.DuckFactor())
	}
	if !strings.Contains(m.DJBannerPersona, "Cyberpunk") {
		t.Errorf("expected Cyberpunk persona in banner, got %q", m.DJBannerPersona)
	}
	if cmd == nil {
		t.Errorf("expected non-nil batch command on auto AI DJ track change")
	}
}

func TestLayoutBoundsWithAIDJAndLinerNotes(t *testing.T) {
	m := createTestModel()
	m.ShowLinerNotes = true
	m.DJBannerActive = true
	m.DJBannerPersona = "BBC Presenter"
	m.DJBannerEngine = "Piper (Local ONNX, 0ms Cloud Lag)"
	m.DJBannerScript = "You're listening to BBC Radio 6 on halpradio. Up next is Four Tet with 'Two Thousand and Seventeen'."
	m.LinerNotes = m.initialLinerNotes()

	sizes := []struct{ w, h int }{
		{60, 20},
		{80, 24},
		{100, 30},
		{120, 40},
	}

	for _, sz := range sizes {
		m.Width = sz.w
		m.Height = sz.h
		view := m.View()
		if h := lipgloss.Height(view); h >= sz.h {
			t.Errorf("size %dx%d: View() height %d >= terminal height %d", sz.w, sz.h, h, sz.h)
		}
		for idx, line := range strings.Split(view, "\n") {
			if w := lipgloss.Width(line); w > sz.w {
				t.Errorf("size %dx%d line %d width %d > terminal width %d", sz.w, sz.h, idx, w, sz.w)
			}
		}
	}
}

func TestZeroHardcodedHexColorsInLinerNotesAndUI(t *testing.T) {
	hexRe := regexp.MustCompile(`#[0-9a-fA-F]{6}`)
	files := []string{
		filepath.Join("modals", "linernotes.go"),
		"nowplaying.go",
		"view.go",
	}
	for _, rel := range files {
		raw, err := os.ReadFile(rel)
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if match := hexRe.FindString(string(raw)); match != "" {
			t.Errorf("found hardcoded hex color %q in %s; must use theme.Theme tokens", match, rel)
		}
	}
}
