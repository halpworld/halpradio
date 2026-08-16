package components

import (
	"strings"
	"testing"
	"time"

	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
)

func TestRenderHistoryList(t *testing.T) {
	th := theme.GetTheme("tokyonight")

	// 1. Empty history
	emptyOut := RenderHistoryList([]radio.HistoryEntry{}, 0, 80, 20, th)
	if !strings.Contains(emptyOut, "No track history yet") {
		t.Errorf("Expected empty history notice, got: %s", emptyOut)
	}

	// 2. Populated history
	entries := []radio.HistoryEntry{
		{
			Artist:      "Tycho",
			Title:       "Awake",
			StationName: "SomaFM Groove Salad",
			PlayedAt:    time.Now(),
		},
		{
			Artist:      "Boards of Canada",
			Title:       "Dayvan Cowboy",
			StationName: "Chillout Lounge",
			PlayedAt:    time.Now().Add(-5 * time.Minute),
		},
	}

	out := RenderHistoryList(entries, 0, 90, 20, th)
	if !strings.Contains(out, "Tycho") || !strings.Contains(out, "Awake") || !strings.Contains(out, "Boards of Canada") {
		t.Errorf("Expected track details in history list render, got: %s", out)
	}

	// 3. Narrow width
	narrowOut := RenderHistoryList(entries, 1, 45, 10, th)
	if !strings.Contains(narrowOut, "Tycho") && !strings.Contains(narrowOut, "Boards of Canada") {
		t.Errorf("Expected rendered track history in narrow view, got: %s", narrowOut)
	}
}
