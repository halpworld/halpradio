package components

import (
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/theme"
)

func TestRenderSidebar(t *testing.T) {
	th := theme.GetTheme("catppuccin")
	items := []string{"Ambient", "Chillout", "Electronic", "Jazz", "Lofi", "Rock", "Synthwave"}

	// Focused and unfocused
	outFocused := RenderSidebar("GENRES", items, "Lofi", 5, 24, 15, true, th)
	if !strings.Contains(outFocused, "GENRES") || !strings.Contains(outFocused, "Lofi") {
		t.Errorf("Expected GENRES title and Lofi item in sidebar, got: %s", outFocused)
	}

	outUnfocused := RenderSidebar("", items, "all", 0, 20, 10, false, th)
	if !strings.Contains(outUnfocused, "All Stations") {
		t.Errorf("Expected 'All Stations' in default sidebar, got: %s", outUnfocused)
	}
}
