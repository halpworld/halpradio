package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/player/dsp"
	"github.com/halpworld/halpradio/pkg/theme"
)

func eqInput(w, h int) EqualizerModalInput {
	s := dsp.DefaultSettings().ApplyPreset("Deep Focus")
	s.Normalizer = true
	s.Crossfeed = true
	return EqualizerModalInput{
		Settings:     s,
		SelectedBand: 2,
		Backend:      "native",
		Support:      player.DSPLive,
		MeterOK:      true,
		MeterLUFS:    -21.3,
		MeterGainDB:  7.3,
		Width:        w,
		Height:       h,
	}
}

func TestRenderEqualizerModalContent(t *testing.T) {
	out := RenderEqualizerModal(eqInput(100, 40), theme.GetTheme("tokyonight"))
	for _, want := range []string{
		"GRAPHIC EQUALIZER & DSP RACK",
		"[Deep Focus]", "Bass Boost", "Vocal Clarity", "Electronic", "Acoustic", "Flat",
		"32", "64", "125", "250", "500", "1k", "2k", "4k", "8k", "16k",
		"+12dB", "0dB", "-12dB",
		"[x] n EBU R128 Normalizer (-14 LUFS)",
		"[x] c Binaural Crossfeed",
		"[ ] t Lo-Fi Cassette Tape",
		"measuring -21.3 LUFS → applying +7.3 dB",
		"live on the native backend",
		"Save & Close",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("modal missing %q", want)
		}
	}
}

func TestRenderEqualizerModalFitsTerminal(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	for _, size := range [][2]int{{120, 40}, {80, 24}, {60, 30}, {52, 26}} {
		out := RenderEqualizerModal(eqInput(size[0], size[1]), th)
		lines := strings.Split(out, "\n")
		if len(lines) > size[1] {
			t.Errorf("%dx%d: %d lines overflow the height", size[0], size[1], len(lines))
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w > size[0] {
				t.Errorf("%dx%d: line %d is %d wide", size[0], size[1], i, w)
			}
		}
	}
}

func TestRenderEqualizerModalSupportNotes(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	in := eqInput(100, 40)

	in.Backend, in.Support = "ffplay", player.DSPNextStation
	if out := RenderEqualizerModal(in, th); !strings.Contains(out, "next station") {
		t.Error("ffplay note missing")
	}
	in.Backend, in.Support, in.MeterOK = "vlc", player.DSPUnavailable, false
	out := RenderEqualizerModal(in, th)
	if !strings.Contains(out, "vlc backend cannot run the DSP rack") {
		t.Error("unsupported-backend warning missing")
	}
	if strings.Contains(out, "measuring") {
		t.Error("meter line should be hidden without a measurement")
	}
}

func TestSliderFilled(t *testing.T) {
	cases := []struct {
		gain, level float64
		want        bool
	}{
		{0, 0, true}, {0, 3, false}, {0, -3, false},
		{1, 3, false}, {2, 3, true}, {12, 12, true}, {6, 9, false},
		{-2, -3, true}, {-1, -3, false}, {-12, -12, true}, {-6, 3, false},
	}
	for _, c := range cases {
		if got := sliderFilled(c.gain, c.level); got != c.want {
			t.Errorf("sliderFilled(%v, %v) = %v, want %v", c.gain, c.level, got, c.want)
		}
	}
}
