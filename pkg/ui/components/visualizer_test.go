package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/theme"
)

func TestVisualizerModesAndCycle(t *testing.T) {
	viz := NewVisualizer("dj-cat")
	if viz.Mode != "dj-cat" {
		t.Errorf("Expected initial mode 'dj-cat', got '%s'", viz.Mode)
	}

	modeOrder := []string{
		"dj-dog",
		"dj-bear",
		"dj-frog",
		"dj-bunny",
		"bars",
		"wave",
		"spectrum",
		"minimal",
		"off",
		"dj-cat",
	}

	for _, expected := range modeOrder {
		next := viz.CycleMode()
		if next != expected {
			t.Errorf("Expected cycle mode '%s', got '%s'", expected, next)
		}
	}
}

func TestVisualizerAliases(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "dj-cat"},
		{"default", "dj-cat"},
		{"dj", "dj-cat"},
		{"cat", "dj-cat"},
		{"dj-cat", "dj-cat"},
		{"dj_cat", "dj-cat"},
		{"dog", "dj-dog"},
		{"dj-dog", "dj-dog"},
		{"bear", "dj-bear"},
		{"dj-bear", "dj-bear"},
		{"frog", "dj-frog"},
		{"dj-frog", "dj-frog"},
		{"bunny", "dj-bunny"},
		{"dj-bunny", "dj-bunny"},
		{"waveform", "wave"},
		{"bars", "bars"},
		{"wave", "wave"},
		{"spectrum", "spectrum"},
		{"minimal", "minimal"},
		{"off", "off"},
	}

	for _, tc := range tests {
		viz := NewVisualizer(tc.input)
		if viz.Mode != tc.expected {
			t.Errorf("Input '%s': expected mode '%s', got '%s'", tc.input, tc.expected, viz.Mode)
		}
	}
}

func TestVisualizerPosesExactWidth(t *testing.T) {
	animals := []string{"dj-cat", "dj-dog", "dj-bear", "dj-frog", "dj-bunny"}

	for _, animal := range animals {
		poses := getAnimalPoses(animal)
		if len(poses) == 0 {
			t.Fatalf("Animal '%s' has no poses", animal)
		}
		for i, pose := range poses {
			headWidth := lipgloss.Width(pose.head)
			armWidth := lipgloss.Width(pose.arm)
			if headWidth != 9 {
				t.Errorf("Animal '%s' pose %d head '%s' visual width is %d, expected 9", animal, i, pose.head, headWidth)
			}
			if armWidth != 2 {
				t.Errorf("Animal '%s' pose %d arm '%s' visual width is %d, expected 2", animal, i, pose.arm, armWidth)
			}
		}
	}
}

func TestVisualizerFrameWidthStability(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	animals := []string{"dj-cat", "dj-dog", "dj-bear", "dj-frog", "dj-bunny"}
	testWidths := []int{24, 31, 45, 80}

	for _, animal := range animals {
		viz := NewVisualizer(animal)
		for _, w := range testWidths {
			var expectedWidth int
			for f := 0; f < 20; f++ {
				viz.Tick()
				out := viz.Render(true, w, th)
				actWidth := lipgloss.Width(out)
				if f == 0 {
					expectedWidth = actWidth
				} else if actWidth != expectedWidth {
					t.Errorf("Animal '%s' at target width %d changed visual width between frames (frame 0: %d, frame %d: %d)",
						animal, w, expectedWidth, f, actWidth)
				}
				if actWidth > w {
					t.Errorf("Animal '%s' frame %d width %d exceeded requested width %d", animal, f, actWidth, w)
				}
			}
		}
	}
}

func TestVisualizerAnimalRender(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	animals := []string{"dj-cat", "dj-dog", "dj-bear", "dj-frog", "dj-bunny"}

	for _, animal := range animals {
		viz := NewVisualizer(animal)

		// Test stopped state
		stopped := viz.Render(false, 40, th)
		if stopped == "" {
			t.Errorf("Expected non-empty stopped rendering for '%s'", animal)
		}
		if !strings.Contains(stopped, "STOPPED") {
			t.Errorf("Expected stopped rendering for '%s' to contain 'STOPPED', got: %s", animal, stopped)
		}

		// Test multiple animation ticks and playing state
		for f := 0; f < 10; f++ {
			viz.Tick()
			playing := viz.Render(true, 40, th)
			if playing == "" {
				t.Errorf("Expected non-empty playing rendering for '%s' at frame %d", animal, f)
			}
			if lipgloss.Width(playing) > 40 {
				t.Errorf("Rendered width %d exceeded requested width 40 for '%s'", lipgloss.Width(playing), animal)
			}
		}
	}
}

func TestVisualizerRenderPlayingVsStopped(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	viz := NewVisualizer("bars")

	// Render when audio stopped
	stoppedOutput := viz.Render(false, 40, th)
	if !strings.Contains(stoppedOutput, "STOPPED") {
		t.Errorf("Expected stopped output to contain 'STOPPED', got: %s", stoppedOutput)
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

func TestVisualizerWidthConstraints(t *testing.T) {
	th := theme.GetTheme("synthwave")
	modes := []string{"dj-cat", "dj-dog", "dj-bear", "dj-frog", "dj-bunny", "bars", "wave", "spectrum", "minimal"}
	widths := []int{10, 15, 20, 25, 35, 50, 80}

	for _, mode := range modes {
		viz := NewVisualizer(mode)
		for _, w := range widths {
			viz.Tick()
			out := viz.Render(true, w, th)
			actualWidth := lipgloss.Width(out)
			if actualWidth > w {
				t.Errorf("Mode '%s' at width %d produced width %d", mode, w, actualWidth)
			}
		}
	}
}
