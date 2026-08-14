package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/theme"
)

func TestRenderWhichKeyOverlayNoLineWrapping(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	testSizes := []struct {
		w, h int
	}{
		{60, 26},
		{70, 26},
		{80, 24},
		{100, 30},
		{120, 40},
	}

	for _, ts := range testSizes {
		rendered := RenderWhichKeyOverlay(ts.w, ts.h, th)
		lines := strings.Split(rendered, "\n")
		if len(lines) == 0 {
			t.Fatalf("RenderWhichKeyOverlay returned empty for size %dx%d", ts.w, ts.h)
		}

		for lineIdx, line := range lines {
			w := lipgloss.Width(line)
			if w > ts.w {
				t.Errorf("Line %d width (%d) exceeds terminal width (%d) in size %dx%d:\n%s", lineIdx, w, ts.w, ts.w, ts.h, line)
			}
		}
	}
}
