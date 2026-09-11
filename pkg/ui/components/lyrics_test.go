package components

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/lyrics"
	"github.com/halpworld/halpradio/pkg/theme"
)

func syncedTestSheet() *lyrics.Sheet {
	return &lyrics.Sheet{
		Artist: "Tycho",
		Title:  "A Walk",
		Synced: true,
		Source: "LRCLIB",
		Lines: []lyrics.Line{
			{At: 0, Text: "I've been wandering through the neon lights"},
			{At: 4 * time.Second, Text: "Searching for a signal in the dead of night"},
			{At: 9 * time.Second, Text: "Everything is quiet when the music starts"},
			{At: 14 * time.Second, Text: "And the city holds its breath"},
		},
	}
}

func plainTestSheet(n int) *lyrics.Sheet {
	sheet := &lyrics.Sheet{Artist: "Boards", Title: "Olson", Source: "NetEase"}
	for i := 0; i < n; i++ {
		sheet.Lines = append(sheet.Lines, lyrics.Line{Text: strings.Repeat("word ", 6)})
	}
	return sheet
}

func TestRenderLyricsDrawer_HighlightsActiveLine(t *testing.T) {
	in := LyricsDrawerInput{
		Sheet:      syncedTestSheet(),
		TrackLabel: "Tycho - A Walk",
		Elapsed:    5 * time.Second,
		Width:      44,
		Height:     20,
	}
	out := RenderLyricsDrawer(in, theme.GetTheme("tokyonight"))

	if !strings.Contains(out, "►") || !strings.Contains(out, "◄") {
		t.Errorf("expected the active line markers in the drawer, got:\n%s", out)
	}
	if !strings.Contains(out, "Searching for a signal") {
		t.Errorf("expected the line active at 5s to be visible, got:\n%s", out)
	}
	if !strings.Contains(out, "Synced via LRCLIB") {
		t.Errorf("expected LRCLIB provenance in the footer, got:\n%s", out)
	}
}

func TestRenderLyricsDrawer_RespectsHeight(t *testing.T) {
	sheet := plainTestSheet(60)
	for _, h := range []int{8, 14, 20, 40} {
		in := LyricsDrawerInput{Sheet: sheet, Width: 40, Height: h}
		out := RenderLyricsDrawer(in, theme.GetTheme("nord"))
		if got := lipgloss.Height(out); got != h {
			t.Errorf("height %d: drawer rendered %d rows, want %d", h, got, h)
		}
		if got := lipgloss.Width(out); got != 40 {
			t.Errorf("height %d: drawer rendered %d columns, want 40", h, got)
		}
	}
}

func TestRenderLyricsDrawer_UnsyncedScrolls(t *testing.T) {
	sheet := &lyrics.Sheet{Source: "NetEase"}
	for i := 0; i < 40; i++ {
		sheet.Lines = append(sheet.Lines, lyrics.Line{Text: lineMarker(i)})
	}

	top := RenderLyricsDrawer(LyricsDrawerInput{Sheet: sheet, Width: 40, Height: 16}, theme.GetTheme("dracula"))
	if !strings.Contains(top, lineMarker(0)) {
		t.Errorf("expected the first line at scroll 0, got:\n%s", top)
	}

	scrolled := RenderLyricsDrawer(LyricsDrawerInput{Sheet: sheet, Width: 40, Height: 16, Scroll: 20}, theme.GetTheme("dracula"))
	if strings.Contains(scrolled, lineMarker(0)) {
		t.Errorf("expected the first line to be scrolled away, got:\n%s", scrolled)
	}
	if !strings.Contains(scrolled, lineMarker(20)) {
		t.Errorf("expected line 20 after scrolling, got:\n%s", scrolled)
	}
	if !strings.Contains(scrolled, "Unsynced via NetEase") {
		t.Errorf("expected unsynced provenance, got:\n%s", scrolled)
	}
}

func TestRenderLyricsDrawer_EmptyStates(t *testing.T) {
	th := theme.GetTheme("gruvbox")

	fetching := RenderLyricsDrawer(LyricsDrawerInput{Fetching: true, Width: 40, Height: 14}, th)
	if !strings.Contains(fetching, "Querying LRCLIB") {
		t.Errorf("expected a fetching hint, got:\n%s", fetching)
	}

	missing := RenderLyricsDrawer(LyricsDrawerInput{Status: "No lyrics found for \"Foo\"", Width: 40, Height: 14}, th)
	if !strings.Contains(missing, "No lyrics found") {
		t.Errorf("expected the status message, got:\n%s", missing)
	}
}

func TestRenderLyricsDrawer_InstrumentalFooter(t *testing.T) {
	sheet := &lyrics.Sheet{
		Synced:       false,
		Instrumental: true,
		Source:       "LRCLIB",
		Lines:        []lyrics.Line{{Text: "♪ Instrumental ♪"}},
	}
	out := RenderLyricsDrawer(LyricsDrawerInput{Sheet: sheet, Width: 40, Height: 14}, theme.GetTheme("synthwave"))
	if !strings.Contains(out, "Instrumental") {
		t.Errorf("expected the instrumental badge, got:\n%s", out)
	}
}

func TestRenderLyricsDrawer_ShowsSyncOffset(t *testing.T) {
	in := LyricsDrawerInput{
		Sheet:   syncedTestSheet(),
		Elapsed: 5 * time.Second,
		Offset:  -1500 * time.Millisecond,
		Width:   46,
		Height:  20,
	}
	out := RenderLyricsDrawer(in, theme.GetTheme("tokyonight"))
	if !strings.Contains(out, "-1.5s") {
		t.Errorf("expected the sync offset in the footer, got:\n%s", out)
	}
}

func TestRenderLyricsDrawer_KeepsArtworkPadding(t *testing.T) {
	artLines := []string{strings.Repeat("#", 12), strings.Repeat("#", 12)}
	in := LyricsDrawerInput{
		Sheet:     syncedTestSheet(),
		ArtLines:  artLines,
		ArtCols:   12,
		ArtSource: "iTunes",
		Width:     44,
		Height:    24,
	}
	out := RenderLyricsDrawer(in, theme.GetTheme("tokyonight"))
	if got := lipgloss.Width(out); got != 44 {
		t.Errorf("artwork changed the drawer width to %d, want 44", got)
	}
	if !strings.Contains(out, "iTunes") {
		t.Errorf("expected the artwork provider label, got:\n%s", out)
	}
}

func TestWrapPlain(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  []string
	}{
		{"empty", "", 10, []string{""}},
		{"fits", "hello world", 20, []string{"hello world"}},
		{"wraps on words", "hello world again", 11, []string{"hello world", "again"}},
		{"breaks long word", "abcdefghij", 4, []string{"abcd", "efgh", "ij"}},
		{"collapses whitespace", "  a   b  ", 10, []string{"a b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := wrapPlain(tc.text, tc.width)
			if len(got) != len(tc.want) {
				t.Fatalf("wrapPlain(%q, %d) = %q, want %q", tc.text, tc.width, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("row %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestWrapSheetTracksSourceLines(t *testing.T) {
	sheet := &lyrics.Sheet{Lines: []lyrics.Line{
		{Text: "short"},
		{Text: "a much longer line that certainly wraps"},
	}}
	rows := wrapSheet(sheet, 12)
	if len(rows) < 3 {
		t.Fatalf("expected the long line to wrap, got %d rows", len(rows))
	}
	if rows[0].srcIdx != 0 || rows[0].cont {
		t.Errorf("first row should be source line 0 and not a continuation, got %+v", rows[0])
	}
	var conts int
	for _, r := range rows {
		if r.srcIdx == 1 && r.cont {
			conts++
		}
	}
	if conts == 0 {
		t.Error("expected at least one continuation row for the wrapped line")
	}
	if idx := firstRowFor(rows, 1); rows[idx].srcIdx != 1 || rows[idx].cont {
		t.Errorf("firstRowFor(1) returned %+v, want the first row of source line 1", rows[idx])
	}
}

func lineMarker(i int) string {
	return "LYRICLINE" + string(rune('A'+i%26)) + string(rune('a'+i/26))
}
