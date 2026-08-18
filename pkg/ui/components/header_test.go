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

	widths := []int{120, 80, 50}

	for _, w := range widths {
		for tab := 0; tab < 8; tab++ {
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
