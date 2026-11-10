package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/halpworld/halpradio/pkg/ai"
	"github.com/halpworld/halpradio/pkg/art"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/player/fingerprint"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/util"
)

const evilText = "Good\x1b]52;c;cm0gLXJmIH4K\x07‮Text\x1b[2J"

func hasControl(s string) bool {
	return strings.ContainsAny(s, "\x1b\x07‮")
}

// TTY-4: third-party metadata is sanitized when Update stores it.
func TestLinerNotesSanitizedWhenStored(t *testing.T) {
	m := NewModel(radio.NewStore(), player.NewMockPlayer(80, nil), util.DefaultConfig())
	notes := &ai.LinerNotes{
		Artist: evilText, Title: evilText, Biography: evilText + "\nsecond line",
		AlbumNotes:  evilText,
		Discography: []ai.DiscographyItem{{Title: evilText, Year: "19\x1b[2J99"}},
		TrackTrivia: []string{evilText},
		Source:      evilText,
	}
	nm, _ := m.Update(LinerNotesFetchedMsg{Notes: notes})
	m = nm.(Model)
	n := m.LinerNotes
	if n == nil {
		t.Fatal("notes not stored")
	}
	for _, s := range []string{n.Artist, n.Title, n.Biography, n.AlbumNotes, n.Discography[0].Title, n.Discography[0].Year, n.TrackTrivia[0], n.Source} {
		if hasControl(s) {
			t.Fatalf("control characters stored: %q", s)
		}
	}
	if !strings.Contains(n.Biography, "\n") {
		t.Errorf("biography lost its line break: %q", n.Biography)
	}
	if hasControl(notes.Artist) == false {
		t.Error("the message's own value should not be mutated in place")
	}
}

func TestCoverAndDJScriptSanitizedWhenStored(t *testing.T) {
	m := NewModel(radio.NewStore(), player.NewMockPlayer(80, nil), util.DefaultConfig())
	m.ArtTrackKey = "k"
	nm, _ := m.Update(CoverArtFetchedMsg{TrackKey: "k", Cover: &art.Cover{Album: evilText, Artist: evilText, Title: evilText, Source: evilText}})
	m = nm.(Model)
	if m.Cover == nil || hasControl(m.Cover.Album+m.Cover.Artist+m.Cover.Title+m.Cover.Source) {
		t.Fatalf("cover metadata not sanitized: %+v", m.Cover)
	}

	nm, _ = m.Update(DJIntroGeneratedMsg{Persona: evilText, Engine: evilText, Script: evilText})
	m = nm.(Model)
	if hasControl(m.DJBannerScript + m.DJBannerPersona + m.DJBannerEngine) {
		t.Fatalf("DJ banner not sanitized: %q %q %q", m.DJBannerScript, m.DJBannerPersona, m.DJBannerEngine)
	}
}

func TestIdentifiedResultSanitizedWhenStored(t *testing.T) {
	mp := player.NewMockPlayer(80, nil)
	m := NewModel(radio.NewStore(), mp, util.DefaultConfig())
	_ = mp.Play(radio.Station{ID: "s1", Name: "S", URL: "http://example.com/"})
	mp.SetStatus(player.StatusPlaying)
	m.PlayingID = "s1"
	nm, _ := m.Update(TrackIdentifiedMsg{StationID: "s1", StationName: "S", Result: &fingerprint.Result{Artist: evilText, Title: evilText, Album: evilText, Source: evilText}})
	m = nm.(Model)
	r := m.IdentifiedResult
	if r == nil {
		t.Fatal("result not stored")
	}
	if hasControl(r.Artist + r.Title + r.Album + r.Source) {
		t.Fatalf("identified result not sanitized: %+v", r)
	}
	if hasControl(m.StatusMessage) || hasControl(m.WindowTitle()) {
		t.Fatalf("control characters reached status or title")
	}
	for _, h := range m.Store.GetHistory() {
		if hasControl(h.TrackTitle) || hasControl(h.FullDisplay()) {
			t.Fatalf("control characters in history: %q", h.FullDisplay())
		}
	}
}

// B11: when the store refuses a station the add form stays open with the
// reason instead of reporting "Saved".
func TestAddStationShowsStoreError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home+"/.config")
	t.Setenv("APPDATA", home+"/AppData")
	m := NewModel(radio.NewStore(), player.NewMockPlayer(80, nil), util.DefaultConfig())
	m.ShowAddModal = true
	// A name made only of control characters is empty once sanitized.
	m.AddInputs = []string{"\x1b\x07", "https://stream.example.com/live", "", "", "", "", ""}
	nm, _ := m.handleAddModalKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !nm.ShowAddModal {
		t.Fatalf("form closed although the store refused the station; status %q", nm.StatusMessage)
	}
	if !strings.Contains(nm.AddErrMsg, "Could not save station") {
		t.Errorf("AddErrMsg %q", nm.AddErrMsg)
	}
	if strings.Contains(nm.StatusMessage, "Saved") {
		t.Errorf("status claims success: %q", nm.StatusMessage)
	}
}
