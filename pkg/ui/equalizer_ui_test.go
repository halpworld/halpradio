package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/player/dsp"
	"github.com/halpworld/halpradio/pkg/util"
)

func eqKey(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "shift+tab":
			msg = tea.KeyMsg{Type: tea.KeyShiftTab}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		next, _ := m.Update(msg)
		m = next.(Model)
	}
	return m
}

func newEQTestModel(t *testing.T) (Model, *player.MockPlayer) {
	t.Helper()
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	m := createTestModel()
	mock := m.Player.(*player.MockPlayer)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return next.(Model), mock
}

func TestEQModalOpensOnShiftE(t *testing.T) {
	m, _ := newEQTestModel(t)
	m = eqKey(t, m, "E")
	if !m.ShowEQModal {
		t.Fatal("E should open the equalizer modal")
	}
	view := m.View()
	for _, want := range []string{"GRAPHIC EQUALIZER", "16k", "EBU R128 Normalizer"} {
		if !strings.Contains(view, want) {
			t.Errorf("view missing %q", want)
		}
	}
}

func TestEQModalBandNavigationWraps(t *testing.T) {
	m, _ := newEQTestModel(t)
	m = eqKey(t, m, "E", "shift+tab")
	if m.EQBand != dsp.NumBands-1 {
		t.Fatalf("shift+tab from band 0 should wrap to %d, got %d", dsp.NumBands-1, m.EQBand)
	}
	m = eqKey(t, m, "tab", "l", "l")
	if m.EQBand != 2 {
		t.Fatalf("expected band 2, got %d", m.EQBand)
	}
	m = eqKey(t, m, "h")
	if m.EQBand != 1 {
		t.Fatalf("expected band 1, got %d", m.EQBand)
	}
}

func TestEQModalGainChangesReachPlayerLive(t *testing.T) {
	m, mock := newEQTestModel(t)
	before := mock.DSPUpdates()
	m = eqKey(t, m, "E", "l", "k", "k", "K")
	if got := m.DSP.Bands[1]; got != 5 {
		t.Fatalf("band 64 Hz should be +5 dB, got %v", got)
	}
	if m.DSP.Preset != dsp.PresetCustom {
		t.Errorf("hand-tuned curve should be Custom, got %q", m.DSP.Preset)
	}
	if mock.DSPUpdates()-before != 3 {
		t.Errorf("each adjustment should reach the player, got %d updates", mock.DSPUpdates()-before)
	}
	if got := mock.DSPSettings().Bands[1]; got != 5 {
		t.Errorf("player has %v dB on band 1, want 5", got)
	}

	m = eqKey(t, m, "j", "J", "J", "J", "J", "J", "J")
	if got := m.DSP.Bands[1]; got != dsp.MinBandGain {
		t.Errorf("gain should clamp at %v, got %v", dsp.MinBandGain, got)
	}
	m = eqKey(t, m, "0")
	if got := m.DSP.Bands[1]; got != 0 {
		t.Errorf("0 should reset the band, got %v", got)
	}
	if m.DSP.Preset != dsp.PresetFlat {
		t.Errorf("an all-zero curve should match Flat, got %q", m.DSP.Preset)
	}
}

func TestEQModalPresetCycling(t *testing.T) {
	m, mock := newEQTestModel(t)
	m = eqKey(t, m, "E", "p")
	if m.DSP.Preset != dsp.Presets[1].Name {
		t.Fatalf("p from Flat should select %q, got %q", dsp.Presets[1].Name, m.DSP.Preset)
	}
	if got := mock.DSPSettings().Preset; got != dsp.Presets[1].Name {
		t.Errorf("player preset = %q", got)
	}
	m = eqKey(t, m, "P", "P")
	if m.DSP.Preset != dsp.Presets[len(dsp.Presets)-1].Name {
		t.Errorf("P should cycle backwards and wrap, got %q", m.DSP.Preset)
	}

	// From a Custom curve p starts at the first preset.
	m = eqKey(t, m, "r", "k")
	if m.DSP.Preset != dsp.PresetCustom {
		t.Fatalf("expected Custom, got %q", m.DSP.Preset)
	}
	m = eqKey(t, m, "p")
	if m.DSP.Preset != dsp.Presets[0].Name {
		t.Errorf("p from Custom should select %q, got %q", dsp.Presets[0].Name, m.DSP.Preset)
	}
}

func TestEQModalToggles(t *testing.T) {
	m, mock := newEQTestModel(t)
	m = eqKey(t, m, "E")
	start := m.DSP
	m = eqKey(t, m, "n", "c", "t")
	if m.DSP.Normalizer == start.Normalizer || m.DSP.Crossfeed == start.Crossfeed || m.DSP.LoFi == start.LoFi {
		t.Fatalf("n/c/t should toggle normalizer, crossfeed and lo-fi: %+v", m.DSP)
	}
	got := mock.DSPSettings()
	if got.Normalizer != m.DSP.Normalizer || got.Crossfeed != m.DSP.Crossfeed || got.LoFi != m.DSP.LoFi {
		t.Errorf("player out of sync: %+v vs %+v", got, m.DSP)
	}
	// Inside the modal these keys must not leak to the global bindings
	// (c = genre filter, t = theme picker).
	if m.ShowThemePicker {
		t.Error("t leaked to the theme picker")
	}
}

func TestEQModalEscSavesAndCloses(t *testing.T) {
	m, _ := newEQTestModel(t)
	m = eqKey(t, m, "E", "p", "c", "esc")
	if m.ShowEQModal {
		t.Fatal("esc should close the modal")
	}
	if !strings.Contains(m.StatusMessage, "DSP rack saved") {
		t.Errorf("status = %q", m.StatusMessage)
	}
	saved, err := dsp.Load(util.GetDSPFile())
	if err != nil {
		t.Fatalf("dsp.yaml not readable: %v", err)
	}
	if saved.Preset != dsp.Presets[1].Name || saved.Crossfeed != m.DSP.Crossfeed {
		t.Errorf("saved %+v, want preset %q crossfeed %v", saved, dsp.Presets[1].Name, m.DSP.Crossfeed)
	}
}

func TestEQModalWarnsOnUnsupportedBackend(t *testing.T) {
	m, mock := newEQTestModel(t)
	mock.SetDSPSupport(player.DSPUnavailable)
	m = eqKey(t, m, "E")
	if !strings.Contains(m.View(), "cannot run the DSP rack") {
		t.Error("expected a warning for a backend without DSP support")
	}
}
