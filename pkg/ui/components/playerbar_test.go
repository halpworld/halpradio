package components

import (
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
)

func TestRenderPlayerBar(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	viz := NewVisualizer("bars")

	// 1. Nil station (idle)
	idleOut := RenderPlayerBar(nil, "", player.StatusStopped, 80, false, viz, "", 80, th)
	if !strings.Contains(idleOut, "No station selected") {
		t.Errorf("Expected idle message in RenderPlayerBar, got: %s", idleOut)
	}

	idleWithTimer := RenderPlayerBar(nil, "", player.StatusStopped, 80, false, viz, "🍅 25:00", 80, th)
	if !strings.Contains(idleWithTimer, "25:00") {
		t.Errorf("Expected timer badge in idle bar, got: %s", idleWithTimer)
	}

	// 2. Active station
	st := &radio.Station{
		ID:      "test-station",
		Name:    "Synthwave Chill",
		Genre:   "Synthwave",
		Country: "US",
	}

	activeOut := RenderPlayerBar(st, "Kavinsky - Nightcall", player.StatusPlaying, 75, false, viz, "🍅 20:00", 90, th)
	if !strings.Contains(activeOut, "Synthwave Chill") || !strings.Contains(activeOut, "Kavinsky - Nightcall") {
		t.Errorf("Expected station and track title in active playerbar, got: %s", activeOut)
	}

	// 3. Muted state
	mutedOut := RenderPlayerBar(st, "Track", player.StatusPlaying, 75, true, viz, "", 80, th)
	if !strings.Contains(mutedOut, "MUTED") {
		t.Errorf("Expected MUTED indicator in playerbar, got: %s", mutedOut)
	}
}
