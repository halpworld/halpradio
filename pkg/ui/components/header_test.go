package components

import (
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/theme"
)

func TestRenderHeader(t *testing.T) {
	th := theme.GetTheme("tokyonight")

	statuses := []player.PlayStatus{
		player.StatusPlaying,
		player.StatusConnecting,
		player.StatusPaused,
		player.StatusError,
		player.StatusStopped,
	}

	widths := []int{140, 100, 75, 48}

	for _, w := range widths {
		for tab := 0; tab < 9; tab++ {
			for _, st := range statuses {
				out := RenderHeader(w, tab, st, "mpv", th)
				if out == "" {
					t.Errorf("RenderHeader returned empty string for width %d, tab %d, status %s", w, tab, st)
				}
				if !strings.Contains(out, "mpv") {
					t.Errorf("RenderHeader output missing backend 'mpv' for width %d", w)
				}
			}
		}
	}
}

func TestRenderHeaderTunerTab(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	// Test wide header contains 0: Tuner
	wideOut := RenderHeader(130, 8, player.StatusPlaying, "mpv", th, true)
	if !strings.Contains(wideOut, "0: Tuner") {
		t.Errorf("Expected wide header to contain '0: Tuner', got:\n%s", wideOut)
	}

	// Test medium header contains 0:Tuner
	medOut := RenderHeader(100, 8, player.StatusPlaying, "mpv", th, true)
	if !strings.Contains(medOut, "0:Tuner") {
		t.Errorf("Expected medium header to contain '0:Tuner', got:\n%s", medOut)
	}

	// Test compact header contains 0:Tune
	compactOut := RenderHeader(70, 8, player.StatusPlaying, "mpv", th, true)
	if !strings.Contains(compactOut, "0:Tune") {
		t.Errorf("Expected compact header to contain '0:Tune', got:\n%s", compactOut)
	}
}
