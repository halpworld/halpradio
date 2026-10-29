package modals

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/ai"
	"github.com/halpworld/halpradio/pkg/theme"
)

func TestRenderLinerNotesDrawer_RichContentAndScrolling(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	notes := &ai.LinerNotes{
		Artist:      "Tycho",
		Title:       "A Walk",
		Album:       "Dive",
		ReleaseYear: "2011",
		Country:     "US",
		OriginCity:  "San Francisco",
		Genre:       "Ambient / Chillwave",
		Label:       "Ghostly International",
		Biography:   "Tycho is an American ambient music project led by Scott Hansen, blending vintage synthesizers with organic instrumentation.",
		AlbumNotes:  "'A Walk' opens the 2011 album Dive with warm analog synth layers.",
		Discography: []ai.DiscographyItem{
			{Title: "Past Is Prologue", Year: "2006", Type: "Album"},
			{Title: "Dive", Year: "2011", Type: "Album"},
			{Title: "Awake", Year: "2014", Type: "Album"},
		},
		TrackTrivia: []string{
			"Originated in San Francisco, California.",
			"Sonic profile: ambient, downtempo, chillwave.",
		},
		Source: "MusicBrainz + Wikipedia",
	}

	out := RenderLinerNotesDrawer(LinerNotesDrawerInput{
		Notes:       notes,
		TrackLabel:  "Tycho - A Walk",
		StationName: "SomaFM Groove Salad",
		Focused:     true,
		Width:       48,
		Height:      36,
	}, th)

	for _, want := range []string{
		"LINER NOTES & ARTIST BIO",
		"MusicBrainz + Wikipedia",
		"Tycho",
		"A Walk",
		"Dive (2011)",
		"ARTIST BIOGRAPHY",
		"DISCOGRAPHY HIGHLIGHTS",
		"LOCAL AI DJ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected drawer output to contain %q, got:\n%s", want, out)
		}
	}

	// Verify scrolling down reveals lower sections on smaller heights
	outScrolled := RenderLinerNotesDrawer(LinerNotesDrawerInput{
		Notes:       notes,
		TrackLabel:  "Tycho - A Walk",
		StationName: "SomaFM Groove Salad",
		Scroll:      8,
		Focused:     true,
		Width:       48,
		Height:      22,
	}, th)
	if !strings.Contains(outScrolled, "DISCOGRAPHY HIGHLIGHTS") && !strings.Contains(outScrolled, "TRACK TRIVIA") {
		t.Errorf("expected scrolled drawer to reveal lower sections, got:\n%s", outScrolled)
	}
}

func TestRenderDJBanner_ExactBroadcastLayout(t *testing.T) {
	th := theme.GetTheme("synthwave")
	banner := RenderDJBanner(DJBannerInput{
		Persona:     "Late Night Chill",
		Engine:      "Piper (Local ONNX, 0ms Cloud Lag)",
		Script:      "Up next is Tycho with 'A Walk' from the 2011 album Dive...",
		DuckPercent: 20,
		Fading:      true,
		Width:       80,
	}, th)

	for _, want := range []string{
		"LOCAL AI RADIO DJ",
		"Late Night Chill",
		"Piper (Local ONNX, 0ms Cloud Lag)",
		"Up next is Tycho with 'A Walk'",
		"[Music Ducked: 20% ──► Fading to 100%]",
	} {
		if !strings.Contains(banner, want) {
			t.Errorf("expected DJ banner to contain %q, got:\n%s", want, banner)
		}
	}

	for i, line := range strings.Split(banner, "\n") {
		if w := lipgloss.Width(line); w > 80 {
			t.Errorf("banner line %d width %d exceeds 80: %q", i, w, line)
		}
	}
}

func TestRenderDJSettingsModal_FitsAllTerminalSizes(t *testing.T) {
	th := theme.GetTheme("catppuccin")
	sizes := [][2]int{
		{70, 22},
		{80, 24},
		{100, 30},
		{120, 40},
	}

	for _, sz := range sizes {
		out := RenderDJSettingsModal(DJSettingsModalInput{
			Enabled:           true,
			PersonaID:         "cyberpunk",
			TTSEngine:         "auto",
			ActiveEngineLabel: "Piper (Local ONNX, 0ms Cloud Lag)",
			SelectedVoiceID:   "en_US-amy-medium",
			DuckLevel:         20,
			VoiceModels:       ai.DefaultVoiceModels(),
			InstalledModels:   map[string]bool{"en_US-amy-medium": true},
			Cursor:            3,
			Status:            "✓ Voice model ready",
			Width:             sz[0],
			Height:            sz[1],
		}, th)

		if h := lipgloss.Height(out); h > sz[1] {
			t.Errorf("%dx%d: modal height %d exceeds terminal height %d", sz[0], sz[1], h, sz[1])
		}
		for i, line := range strings.Split(out, "\n") {
			if w := lipgloss.Width(line); w > sz[0] {
				t.Errorf("%dx%d line %d width %d exceeds %d", sz[0], sz[1], i, w, sz[0])
			}
		}
		if !strings.Contains(out, "LOCAL AI RADIO DJ STUDIO") {
			t.Errorf("%dx%d: expected modal title in output", sz[0], sz[1])
		}
	}
}

func TestModalsNoHardcodedHexColors(t *testing.T) {
	src, err := os.ReadFile("linernotes.go")
	if err != nil {
		t.Fatalf("read linernotes.go: %v", err)
	}
	hexRe := regexp.MustCompile(`#[0-9a-fA-F]{3,8}`)
	if matches := hexRe.FindAllString(string(src), -1); len(matches) > 0 {
		t.Errorf("found hardcoded hex colors in linernotes.go: %v", matches)
	}
}
