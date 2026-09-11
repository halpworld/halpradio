package ui

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/art"
	"github.com/halpworld/halpradio/pkg/lyrics"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/util"
)

func keyRune(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func sizedTestModel(w, h int) Model {
	m := createTestModel()
	m.Width, m.Height = w, h
	return m
}

func TestLyricsKey_TogglesDrawerAndFocus(t *testing.T) {
	m := sizedTestModel(120, 40)

	updated, _ := m.Update(keyRune('L'))
	m = updated.(Model)
	if !m.ShowLyrics {
		t.Fatal("expected L to open the lyrics drawer")
	}
	if m.ActiveFocus != FocusLyrics {
		t.Errorf("expected focus to move into the drawer, got %v", m.ActiveFocus)
	}

	updated, _ = m.Update(keyRune('L'))
	m = updated.(Model)
	if m.ShowLyrics {
		t.Error("expected a second L to close the drawer")
	}
	if m.ActiveFocus == FocusLyrics {
		t.Error("expected focus to leave the drawer when it closes")
	}
}

func TestLyricsKey_RefusedOnNarrowTerminal(t *testing.T) {
	m := sizedTestModel(70, 20)

	updated, _ := m.Update(keyRune('L'))
	m = updated.(Model)
	if m.ShowLyrics {
		t.Error("expected the drawer to stay closed on a 60 column terminal")
	}
	if !strings.Contains(m.StatusMessage, "too narrow") {
		t.Errorf("expected a width warning, got %q", m.StatusMessage)
	}
}

func TestLyricsKey_DisabledByConfig(t *testing.T) {
	m := sizedTestModel(120, 40)
	m.Config.LyricsEnabled = false

	updated, _ := m.Update(keyRune('L'))
	m = updated.(Model)
	if m.ShowLyrics {
		t.Error("expected the drawer to stay closed when lyrics are disabled")
	}
	if !strings.Contains(m.StatusMessage, "lyrics_enabled") {
		t.Errorf("expected the config hint, got %q", m.StatusMessage)
	}
}

func TestEscapeClosesLyricsDrawerBeforeClearingSearch(t *testing.T) {
	m := sizedTestModel(120, 40)
	m.SearchQuery = "jazz"
	m.ShowLyrics = true
	m.ActiveFocus = FocusLyrics

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.ShowLyrics {
		t.Error("expected Esc to close the drawer")
	}
	if m.SearchQuery != "jazz" {
		t.Errorf("expected the search query to survive the first Esc, got %q", m.SearchQuery)
	}

	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(Model)
	if m.SearchQuery != "" {
		t.Errorf("expected the second Esc to clear the search, got %q", m.SearchQuery)
	}
}

func TestLyricsFocus_ScrollsWithJK(t *testing.T) {
	m := sizedTestModel(120, 40)
	m.ShowLyrics = true
	m.ActiveFocus = FocusLyrics
	m.SelectedIndex = 0
	m.LyricsSheet = &lyrics.Sheet{Source: "NetEase"}
	for i := 0; i < 20; i++ {
		m.LyricsSheet.Lines = append(m.LyricsSheet.Lines, lyrics.Line{Text: "line"})
	}

	updated, _ := m.Update(keyRune('j'))
	m = updated.(Model)
	if m.LyricsScroll != 1 {
		t.Errorf("expected j to scroll the sheet, got offset %d", m.LyricsScroll)
	}
	if m.SelectedIndex != 0 {
		t.Errorf("expected the station selection to stay put, got %d", m.SelectedIndex)
	}

	updated, _ = m.Update(keyRune('k'))
	m = updated.(Model)
	if m.LyricsScroll != 0 {
		t.Errorf("expected k to scroll back, got offset %d", m.LyricsScroll)
	}

	// Scrolling up at the top must not go negative.
	updated, _ = m.Update(keyRune('k'))
	m = updated.(Model)
	if m.LyricsScroll != 0 {
		t.Errorf("expected the scroll offset to clamp at 0, got %d", m.LyricsScroll)
	}
}

func TestLyricsSyncNudgeKeys(t *testing.T) {
	// The nudge persists the offset, so keep it out of the real config dir.
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	m := sizedTestModel(120, 40)

	// Closed drawer: the nudge keys do nothing.
	updated, _ := m.Update(keyRune('.'))
	m = updated.(Model)
	if m.LyricsOffset != 0 {
		t.Errorf("expected no offset change while the drawer is closed, got %v", m.LyricsOffset)
	}

	m.ShowLyrics = true
	updated, _ = m.Update(keyRune('.'))
	m = updated.(Model)
	if m.LyricsOffset != lyricsSyncStep {
		t.Errorf("expected . to add one sync step, got %v", m.LyricsOffset)
	}

	updated, _ = m.Update(keyRune(','))
	updated, _ = updated.(Model).Update(keyRune(','))
	m = updated.(Model)
	if m.LyricsOffset != -lyricsSyncStep {
		t.Errorf("expected two , presses to land at -1 step, got %v", m.LyricsOffset)
	}
	if m.Config.LyricsOffsetMs != int(-lyricsSyncStep/time.Millisecond) {
		t.Errorf("expected the offset to be mirrored into config, got %d", m.Config.LyricsOffsetMs)
	}

	saved, err := util.LoadConfig()
	if err != nil {
		t.Fatalf("expected the nudge to have written config.yaml: %v", err)
	}
	if saved.LyricsOffsetMs != int(-lyricsSyncStep/time.Millisecond) {
		t.Errorf("expected the offset to survive a reload, got %d", saved.LyricsOffsetMs)
	}
}

func TestLyricsFetchedMsg_PopulatesSheet(t *testing.T) {
	m := sizedTestModel(120, 40)
	m.LyricsTrackKey = "Tycho - A Walk"
	m.IsFetchingLyrics = true
	m.ShowLyrics = true

	sheet := &lyrics.Sheet{
		Artist: "Tycho",
		Title:  "A Walk",
		Synced: true,
		Source: "LRCLIB",
		Lines:  []lyrics.Line{{At: 0, Text: "first"}},
	}
	updated, _ := m.Update(LyricsFetchedMsg{TrackKey: "Tycho - A Walk", Sheet: sheet})
	m = updated.(Model)

	if m.IsFetchingLyrics {
		t.Error("expected the in-flight flag to clear")
	}
	if m.LyricsSheet == nil || m.LyricsSheet.Source != "LRCLIB" {
		t.Fatalf("expected the sheet to be stored, got %+v", m.LyricsSheet)
	}
	if !strings.Contains(m.StatusMessage, "synced lyrics from LRCLIB") {
		t.Errorf("expected a provenance flash, got %q", m.StatusMessage)
	}
}

func TestLyricsFetchedMsg_IgnoresStaleResult(t *testing.T) {
	m := sizedTestModel(120, 40)
	m.LyricsTrackKey = "Tycho - A Walk"
	m.IsFetchingLyrics = true

	stale := &lyrics.Sheet{Source: "LRCLIB", Lines: []lyrics.Line{{Text: "old"}}}
	updated, _ := m.Update(LyricsFetchedMsg{TrackKey: "Someone Else - Old Song", Sheet: stale})
	m = updated.(Model)

	if m.LyricsSheet != nil {
		t.Error("expected a superseded lookup to be dropped")
	}
	if !m.IsFetchingLyrics {
		t.Error("expected the in-flight flag for the current track to survive")
	}
}

func TestLyricsFetchedMsg_NoMatch(t *testing.T) {
	m := sizedTestModel(120, 40)
	m.LyricsTrackKey = "Unknown - Track"
	m.IsFetchingLyrics = true

	updated, _ := m.Update(LyricsFetchedMsg{TrackKey: "Unknown - Track", Err: lyrics.ErrNotFound})
	m = updated.(Model)

	if m.LyricsSheet != nil {
		t.Error("expected no sheet after a failed lookup")
	}
	if !strings.Contains(m.LyricsStatus, "No lyrics found") {
		t.Errorf("expected a not-found status, got %q", m.LyricsStatus)
	}
}

func TestAlbumArtKey_TogglesModal(t *testing.T) {
	m := sizedTestModel(120, 40)
	m.ArtProtocol = art.ProtocolHalfBlock
	m.ArtRenderer = art.NewRenderer(art.ProtocolHalfBlock)

	updated, _ := m.Update(keyRune('A'))
	m = updated.(Model)
	if !m.ShowArtModal {
		t.Fatal("expected A to open the art modal")
	}

	updated, _ = m.Update(keyRune('A'))
	m = updated.(Model)
	if m.ShowArtModal {
		t.Error("expected a second A to close the art modal")
	}
}

func TestAlbumArtModal_HandsOffToLyrics(t *testing.T) {
	m := sizedTestModel(120, 40)
	m.ArtProtocol = art.ProtocolHalfBlock
	m.ArtRenderer = art.NewRenderer(art.ProtocolHalfBlock)
	m.ShowArtModal = true

	updated, _ := m.Update(keyRune('L'))
	m = updated.(Model)
	if m.ShowArtModal {
		t.Error("expected L to close the art modal")
	}
	if !m.ShowLyrics {
		t.Error("expected L to open the lyrics drawer from the art modal")
	}
}

func TestAlbumArtKey_DisabledByConfig(t *testing.T) {
	m := sizedTestModel(120, 40)
	m.Config.AlbumArtEnabled = false

	updated, _ := m.Update(keyRune('A'))
	m = updated.(Model)
	if m.ShowArtModal {
		t.Error("expected the modal to stay closed when album art is disabled")
	}
	if !strings.Contains(m.StatusMessage, "album_art_enabled") {
		t.Errorf("expected the config hint, got %q", m.StatusMessage)
	}
}

func TestCoverArtFetchedMsg_StoresAndRejects(t *testing.T) {
	m := sizedTestModel(120, 40)
	m.ArtProtocol = art.ProtocolHalfBlock
	m.ArtRenderer = art.NewRenderer(art.ProtocolHalfBlock)
	m.ArtTrackKey = "Tycho - A Walk"
	m.IsFetchingArt = true

	cover := &art.Cover{Data: testPNG(t), Source: "iTunes"}
	updated, _ := m.Update(CoverArtFetchedMsg{TrackKey: "Tycho - A Walk", Cover: cover})
	m = updated.(Model)
	if m.Cover == nil || m.Cover.Source != "iTunes" {
		t.Fatalf("expected the cover to be stored, got %+v", m.Cover)
	}
	if m.IsFetchingArt {
		t.Error("expected the in-flight flag to clear")
	}

	m.ArtTrackKey = "Newer - Track"
	updated, _ = m.Update(CoverArtFetchedMsg{TrackKey: "Tycho - A Walk", Cover: cover, Err: art.ErrNotFound})
	m = updated.(Model)
	if m.Cover == nil {
		t.Error("expected a superseded artwork result to leave the held cover alone")
	}
}

func TestLyricElapsed_TracksOffsetAndPlayback(t *testing.T) {
	m := sizedTestModel(120, 40)
	if got := m.lyricElapsed(); got != 0 {
		t.Errorf("expected zero elapsed with no track start, got %v", got)
	}

	st := m.Stations[0]
	_ = m.Player.Play(st)
	m.TrackStartTime = time.Now().Add(-10 * time.Second)

	if got := m.lyricElapsed(); got < 9*time.Second || got > 11*time.Second {
		t.Errorf("expected roughly 10s elapsed, got %v", got)
	}

	m.LyricsOffset = -30 * time.Second
	if got := m.lyricElapsed(); got != 0 {
		t.Errorf("expected a large negative offset to clamp at zero, got %v", got)
	}

	m.LyricsOffset = 0
	_ = m.Player.Stop()
	if got := m.lyricElapsed(); got != 0 {
		t.Errorf("expected zero elapsed while stopped, got %v", got)
	}
}

func TestNowPlayingSignature_ChangesWithTrack(t *testing.T) {
	m := sizedTestModel(120, 40)
	if sig := m.nowPlayingSignature(); sig != "" {
		t.Errorf("expected an empty signature while stopped, got %q", sig)
	}

	mock := m.Player.(*player.MockPlayer)
	_ = mock.Play(m.Stations[0])
	first := m.nowPlayingSignature()
	if first == "" {
		t.Fatal("expected a signature once a station is playing")
	}

	mock.SetTrack("Tycho - A Walk")
	if second := m.nowPlayingSignature(); second == first {
		t.Error("expected the signature to change when the announced track changes")
	}
}

func TestView_WithLyricsDrawerKeepsTerminalBounds(t *testing.T) {
	for _, size := range [][2]int{{200, 50}, {120, 40}, {100, 30}, {80, 24}} {
		m := sizedTestModel(size[0], size[1])
		m.ShowLyrics = true
		m.ActiveFocus = FocusLyrics
		m.LyricsSheet = &lyrics.Sheet{
			Synced: true,
			Source: "LRCLIB",
			Lines: []lyrics.Line{
				{At: 0, Text: "neon lights over the overpass"},
				{At: 3 * time.Second, Text: "searching for a signal"},
			},
		}
		m.TrackStartTime = time.Now().Add(-4 * time.Second)
		_ = m.Player.Play(m.Stations[0])

		out := m.View()
		if got := lipgloss.Width(out); got > size[0] {
			t.Errorf("%dx%d: view is %d columns wide, exceeds the terminal", size[0], size[1], got)
		}
		if got := lipgloss.Height(out); got > size[1] {
			t.Errorf("%dx%d: view is %d rows tall, exceeds the terminal", size[0], size[1], got)
		}
		if !strings.Contains(out, "LIVE LYRICS") {
			t.Errorf("%dx%d: expected the drawer to be visible in the view", size[0], size[1])
		}
	}
}

func TestDrawerArtBudget_DropsThumbnailOnShortTerminals(t *testing.T) {
	m := sizedTestModel(120, 24)
	m.ShowLyrics = true
	if cols, rows := m.artTarget(); cols != 0 || rows != 0 {
		t.Errorf("expected no drawer thumbnail on a 24 row terminal, got %dx%d", cols, rows)
	}

	m = sizedTestModel(120, 50)
	m.ShowLyrics = true
	cols, rows := m.artTarget()
	if rows < drawerArtMinRows || cols < 8 {
		t.Errorf("expected a thumbnail on a 50 row terminal, got %dx%d", cols, rows)
	}
}

func TestLyricsDrawerWidth(t *testing.T) {
	tests := []struct {
		width int
		want  int
	}{
		{40, 0},
		{72, 0},
		{79, 0},
		{80, 32},
		{100, 40},
		{200, 56},
	}
	for _, tc := range tests {
		if got := LyricsDrawerWidth(tc.width); got != tc.want {
			t.Errorf("LyricsDrawerWidth(%d) = %d, want %d", tc.width, got, tc.want)
		}
	}
}

func TestTunerKeepsLKeyForDialSweep(t *testing.T) {
	m := sizedTestModel(120, 40)
	m.Config.ExperimentalTuner = true
	m.SwitchTab(8)
	m.ActiveTuner = true
	m.TunerBand = "FM"
	m.TunerFreq = 93.9

	updated, _ := m.Update(keyRune('L'))
	m = updated.(Model)
	if m.ShowLyrics {
		t.Error("expected L to sweep the dial rather than open the drawer in tuner mode")
	}
	if m.TunerFreq <= 93.9 {
		t.Errorf("expected a fast sweep upward, got %.1f", m.TunerFreq)
	}
}

// testPNG builds a small in-memory PNG so artwork handling can be exercised
// without touching the network or the disk cache.
func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encoding the test PNG: %v", err)
	}
	return buf.Bytes()
}

func TestArtTarget_SizesForSurface(t *testing.T) {
	m := sizedTestModel(120, 40)
	if cols, rows := m.artTarget(); cols != 0 || rows != 0 {
		t.Errorf("expected no artwork target with both surfaces closed, got %dx%d", cols, rows)
	}

	m.ShowLyrics = true
	cols, rows := m.artTarget()
	if cols <= 0 || rows <= 0 {
		t.Fatalf("expected a drawer artwork target, got %dx%d", cols, rows)
	}
	if rows < drawerArtMinRows {
		t.Errorf("drawer artwork of %d rows is below the minimum worth drawing", rows)
	}
	if cols > LyricsDrawerWidth(120)-6 {
		t.Errorf("drawer artwork of %d columns does not fit the drawer", cols)
	}

	m.ShowLyrics = false
	m.ShowArtModal = true
	modalCols, modalRows := m.artTarget()
	if modalCols <= cols || modalRows <= rows {
		t.Errorf("expected the modal to render larger artwork than the drawer, got %dx%d vs %dx%d",
			modalCols, modalRows, cols, rows)
	}
	if modalCols > 120-16 {
		t.Errorf("modal artwork of %d columns overflows the terminal", modalCols)
	}
}

func TestLyricsFetchedMsg_TransientFailureAllowsRetry(t *testing.T) {
	m := sizedTestModel(120, 40)
	m.LyricsTrackKey = "Tycho - A Walk"
	m.IsFetchingLyrics = true

	updated, _ := m.Update(LyricsFetchedMsg{
		TrackKey: "Tycho - A Walk",
		Err:      fmt.Errorf("lrclib: %w", errors.New("dial tcp: connection refused")),
	})
	m = updated.(Model)

	if m.LyricsTrackKey != "" {
		t.Errorf("expected the track key to be cleared so a retry can happen, got %q", m.LyricsTrackKey)
	}
	if !strings.Contains(m.LyricsStatus, "retry") {
		t.Errorf("expected a retry hint, got %q", m.LyricsStatus)
	}
}

func TestCoverArtFetchedMsg_TransientFailureAllowsRetry(t *testing.T) {
	m := sizedTestModel(120, 40)
	m.ArtTrackKey = "Tycho - A Walk"
	m.IsFetchingArt = true

	updated, _ := m.Update(CoverArtFetchedMsg{
		TrackKey: "Tycho - A Walk",
		Err:      fmt.Errorf("itunes: %w", errors.New("i/o timeout")),
	})
	m = updated.(Model)

	if m.ArtTrackKey != "" {
		t.Errorf("expected the track key to be cleared so a retry can happen, got %q", m.ArtTrackKey)
	}
	if !strings.Contains(m.ArtStatus, "retry") {
		t.Errorf("expected a retry hint, got %q", m.ArtStatus)
	}
}

func TestArtClearSequence_EvictsLingeringKittyImage(t *testing.T) {
	m := sizedTestModel(120, 50)
	m.ArtProtocol = art.ProtocolKitty
	m.ArtRenderer = art.NewRenderer(art.ProtocolKitty)
	m.Cover = &art.Cover{Data: testPNG(t), Source: "iTunes"}
	m.ShowArtModal = true
	m.renderArt()

	if len(m.ArtLines) == 0 {
		t.Fatal("expected the modal to rasterise artwork")
	}
	if m.ArtClearSequence() != "" {
		t.Error("expected no delete escape while artwork is on screen")
	}

	m.ShowArtModal = false
	m.renderArt()
	if m.ArtClearFrames <= 0 {
		t.Fatal("expected closing the modal to schedule an image delete")
	}
	if seq := m.ArtClearSequence(); seq == "" {
		t.Error("expected a Kitty delete escape after the artwork disappeared")
	}
	if !strings.Contains(m.View(), m.ArtRenderer.Clear()) {
		t.Error("expected the view to carry the delete escape")
	}

	// The escape is emitted for a few frames and then stops.
	for i := 0; i < artClearFrameCount; i++ {
		updated, _ := m.Update(TickMsg(time.Now()))
		m = updated.(Model)
	}
	if m.ArtClearSequence() != "" {
		t.Errorf("expected the delete escape to stop after %d frames", artClearFrameCount)
	}
}

func TestArtClearSequence_QuietForCellRenderers(t *testing.T) {
	m := sizedTestModel(120, 50)
	m.ArtProtocol = art.ProtocolHalfBlock
	m.ArtRenderer = art.NewRenderer(art.ProtocolHalfBlock)
	m.Cover = &art.Cover{Data: testPNG(t), Source: "iTunes"}
	m.ShowArtModal = true
	m.renderArt()
	m.ShowArtModal = false
	m.renderArt()

	// Half-blocks are ordinary text, so a repaint is enough.
	if m.ArtClearFrames != 0 {
		t.Errorf("expected no delete schedule for a cell renderer, got %d frames", m.ArtClearFrames)
	}
	if m.ArtClearSequence() != "" {
		t.Error("expected no delete escape for a cell renderer")
	}
}
