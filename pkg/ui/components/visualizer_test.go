package components

import (
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/theme"
)

func TestVisualizerModesAndCycle(t *testing.T) {
	viz := NewVisualizer("bars")
	if viz.Mode != "bars" {
		t.Errorf("Expected initial mode 'bars', got '%s'", viz.Mode)
	}

	modeOrder := []string{"wave", "spectrum", "minimal", "off", "bars"}
	for _, expected := range modeOrder {
		next := viz.CycleMode()
		if next != expected {
			t.Errorf("Expected cycle mode '%s', got '%s'", expected, next)
		}
	}
}

func TestVisualizerWaveformAlias(t *testing.T) {
	viz := NewVisualizer("waveform")
	if viz.Mode != "wave" {
		t.Errorf("Expected mode 'wave' for 'waveform' alias, got '%s'", viz.Mode)
	}
}

func TestVisualizerRenderPlayingVsStopped(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	viz := NewVisualizer("bars")

	// Render when audio stopped
	stoppedOutput := viz.Render(false, 40, th)
	if !strings.Contains(stoppedOutput, "AUDIO STOPPED") {
		t.Errorf("Expected stopped output to contain 'AUDIO STOPPED', got: %s", stoppedOutput)
	}

	// Tick physics and render when playing
	viz.Tick()
	playingBars := viz.Render(true, 40, th)
	if playingBars == "" {
		t.Errorf("Expected non-empty output when playing in 'bars' mode")
	}

	// Test wave mode
	viz.Mode = "wave"
	viz.Tick()
	playingWave := viz.Render(true, 40, th)
	if playingWave == "" {
		t.Errorf("Expected non-empty output when playing in 'wave' mode")
	}

	// Test spectrum mode
	viz.Mode = "spectrum"
	viz.Tick()
	playingSpectrum := viz.Render(true, 40, th)
	if playingSpectrum == "" {
		t.Errorf("Expected non-empty output when playing in 'spectrum' mode")
	}

	// Test minimal mode
	viz.Mode = "minimal"
	viz.Tick()
	playingMinimal := viz.Render(true, 40, th)
	if playingMinimal == "" {
		t.Errorf("Expected non-empty output when playing in 'minimal' mode")
	}

	// Test off mode
	viz.Mode = "off"
	if viz.Render(true, 40, th) != "" {
		t.Errorf("Expected empty string when mode is 'off'")
	}
}
