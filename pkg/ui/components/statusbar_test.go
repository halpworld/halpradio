package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/theme"
)

func TestRenderStatusBar(t *testing.T) {
	th := theme.GetTheme("nord")

	// 1. Search Query active
	searchOut := RenderStatusBar("lofi", "", 0, 80, th)
	if !strings.Contains(searchOut, "SEARCH: lofi") {
		t.Errorf("Expected search bar render, got: %s", searchOut)
	}

	// 2. Notification Message active
	msgOut := RenderStatusBar("", "Volume set to 80%", 0, 80, th)
	if !strings.Contains(msgOut, "Volume set to 80%") {
		t.Errorf("Expected message in status bar, got: %s", msgOut)
	}

	// 3. Standard tabs at multiple widths
	for _, w := range []int{100, 70, 45} {
		stdOut := RenderStatusBar("", "", 0, w, th)
		if !strings.Contains(stdOut, "Nav") {
			t.Errorf("Expected Nav key in standard statusbar at width %d, got: %s", w, stdOut)
		}
	}

	// 4. History tab at multiple widths
	for _, w := range []int{100, 70, 45} {
		histOut := RenderStatusBar("", "", 7, w, th)
		if !strings.Contains(histOut, "Yank") {
			t.Errorf("Expected Yank key in history statusbar at width %d, got: %s", w, histOut)
		}
	}

	// 5. Globe tab status bar
	globeOut := RenderStatusBar("", "", 8, 100, th, false)
	if !strings.Contains(globeOut, "Spin/Tilt") && !strings.Contains(globeOut, "Zoom") {
		t.Errorf("Expected Globe controls in Globe statusbar, got: %s", globeOut)
	}

	// 6. Tuner tab status bar
	tunerOut := RenderStatusBar("", "", 8, 100, th, true)
	if !strings.Contains(tunerOut, "Sweep") || !strings.Contains(tunerOut, "Band") {
		t.Errorf("Expected Sweep/Band keys in Tuner statusbar, got: %s", tunerOut)
	}
}

func TestRenderStatusBar_SurfacesLyricsAndArtKeys(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	for _, width := range []int{65, 80, 95, 118, 140, 200} {
		out := RenderStatusBar("", "", 1, width, th)
		if !strings.Contains(out, "[L]") {
			t.Errorf("width %d: expected the lyrics key in the legend, got:\n%s", width, out)
		}
		if !strings.Contains(out, "[A]") {
			t.Errorf("width %d: expected the album art key in the legend, got:\n%s", width, out)
		}
		if got := lipgloss.Width(out); got > width {
			t.Errorf("width %d: legend rendered %d columns", width, got)
		}
	}

	// The narrowest tier keeps lyrics but drops art, and must still fit.
	narrow := RenderStatusBar("", "", 1, 50, th)
	if !strings.Contains(narrow, "[L]") {
		t.Errorf("expected the lyrics key to survive a 50 column legend, got:\n%s", narrow)
	}
	if got := lipgloss.Width(narrow); got > 50 {
		t.Errorf("narrow legend rendered %d columns, want at most 50", got)
	}
}
