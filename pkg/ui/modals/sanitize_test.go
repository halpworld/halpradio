package modals

import (
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/ai"
	"github.com/halpworld/halpradio/pkg/theme"
)

const evil = "A\x1b]52;c;cm0gLXJmIH4K\x07‮B\x1b[2J"

func clean(s string) bool {
	return !strings.ContainsAny(s, "\x07‮") && !strings.Contains(s, "\x1b]") && !strings.Contains(s, "\x1b[2J")
}

// Sink-side guard: the drawer and banner never print escape sequences, even
// when handed raw text.
func TestLinerNotesAndDJBannerSanitizeAtSink(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	notes := &ai.LinerNotes{Artist: evil, Title: evil, Biography: evil, AlbumNotes: evil, TrackTrivia: []string{evil}, Discography: []ai.DiscographyItem{{Title: evil}}, Source: evil}
	out := RenderLinerNotesDrawer(LinerNotesDrawerInput{Notes: notes, Status: evil, TrackLabel: evil, StationName: evil, Genre: evil, Country: evil, Width: 80, Height: 40}, th)
	if !clean(out) {
		t.Fatalf("liner notes drawer printed raw escapes: %q", out)
	}
	if notes.Artist != evil {
		t.Error("renderer modified its input")
	}
	out = RenderDJBanner(DJBannerInput{Persona: evil, Engine: evil, Script: evil, Width: 80}, th)
	if !clean(out) {
		t.Fatalf("DJ banner printed raw escapes: %q", out)
	}
}
