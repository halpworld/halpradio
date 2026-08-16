package components

import (
	"strings"
	"testing"

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
		histOut := RenderStatusBar("", "", 6, w, th)
		if !strings.Contains(histOut, "Yank") {
			t.Errorf("Expected Yank key in history statusbar at width %d, got: %s", w, histOut)
		}
	}
}
