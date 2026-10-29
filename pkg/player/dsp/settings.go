// Package dsp is halpradio's pure-Go audio effects rack: a real-time
// EBU R128 / ITU-R BS.1770 loudness normalizer with a lookahead limiter, a
// Bauer (bs2b) binaural crossfeed, a 10-band graphic equalizer and a lo-fi
// cassette effect. The same Settings also map onto libavfilter filter graphs
// so external backends such as mpv and ffplay can apply an equivalent chain.
package dsp

import (
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// NumBands is the number of graphic equalizer bands.
const NumBands = 10

// BandFrequencies are the equalizer centre frequencies in Hz (ISO octave bands).
var BandFrequencies = [NumBands]float64{32, 64, 125, 250, 500, 1000, 2000, 4000, 8000, 16000}

// BandLabels are the short display labels for BandFrequencies.
var BandLabels = [NumBands]string{"32", "64", "125", "250", "500", "1k", "2k", "4k", "8k", "16k"}

const (
	// MinBandGain and MaxBandGain bound every equalizer band, in dB.
	MinBandGain = -12.0
	MaxBandGain = 12.0

	// DefaultTargetLUFS matches the -14 LUFS reference used by Spotify and YouTube.
	DefaultTargetLUFS = -14.0
	// MinTargetLUFS and MaxTargetLUFS bound the configurable loudness target.
	MinTargetLUFS = -31.0
	MaxTargetLUFS = -5.0

	// PresetCustom names a band curve that no longer matches a built-in preset.
	PresetCustom = "Custom"
	// PresetFlat is the neutral curve every band starts from.
	PresetFlat = "Flat"
)

// Preset is a named equalizer curve.
type Preset struct {
	Name  string
	Bands [NumBands]float64
}

// Presets are the built-in equalizer curves, in the order the UI cycles them.
var Presets = []Preset{
	{Name: PresetFlat},
	{Name: "Bass Boost", Bands: [NumBands]float64{7, 6, 5, 3, 1, 0, 0, 0, 0, 0}},
	{Name: "Vocal Clarity", Bands: [NumBands]float64{-3, -2, -1, 0, 2, 4, 4, 3, 1, 0}},
	{Name: "Electronic", Bands: [NumBands]float64{5, 4, 1, 0, -2, 1, 0, 1, 4, 5}},
	{Name: "Acoustic", Bands: [NumBands]float64{3, 3, 2, 1, 1, 1, 2, 3, 3, 2}},
	{Name: "Deep Focus", Bands: [NumBands]float64{2, 3, 2, 1, 0, -1, -2, -3, -4, -5}},
	{Name: "Lo-Fi Tape", Bands: [NumBands]float64{-4, 0, 2, 3, 2, 0, -1, -3, -6, -9}},
}

// Settings is the user-facing configuration of the DSP rack. It is persisted
// to dsp.yaml, except TargetLUFS which comes from config.yaml.
type Settings struct {
	Preset     string    `yaml:"preset"`
	Bands      []float64 `yaml:"bands"`
	Normalizer bool      `yaml:"normalizer"`
	Crossfeed  bool      `yaml:"crossfeed"`
	LoFi       bool      `yaml:"lofi"`

	// TargetLUFS is the normalizer's integrated loudness target. It lives in
	// config.yaml (loudness_target_lufs), so it is not written to dsp.yaml.
	TargetLUFS float64 `yaml:"-"`
}

// DefaultSettings returns a flat, fully bypassed rack.
func DefaultSettings() Settings {
	return Settings{
		Preset:     PresetFlat,
		Bands:      make([]float64, NumBands),
		TargetLUFS: DefaultTargetLUFS,
	}
}

// Normalize returns a copy with exactly NumBands bands, every gain clamped to
// its legal range and a known preset name.
func (s Settings) Normalize() Settings {
	bands := make([]float64, NumBands)
	copy(bands, s.Bands)
	for i, g := range bands {
		bands[i] = ClampBandGain(g)
	}
	s.Bands = bands
	s.TargetLUFS = ClampTargetLUFS(s.TargetLUFS)
	if s.Preset == "" || (s.Preset != PresetCustom && PresetIndex(s.Preset) < 0) {
		s.Preset = MatchPreset(s.Bands)
	}
	return s
}

// Clone returns a deep copy, so the band slice can be edited independently.
func (s Settings) Clone() Settings {
	s.Bands = append([]float64(nil), s.Bands...)
	return s
}

// EQActive reports whether any equalizer band is boosted or cut.
func (s Settings) EQActive() bool {
	for _, g := range s.Bands {
		if g != 0 {
			return true
		}
	}
	return false
}

// Active reports whether any stage of the rack changes the audio.
func (s Settings) Active() bool {
	return s.Normalizer || s.Crossfeed || s.LoFi || s.EQActive()
}

// Summary is a compact human description such as "EQ Bass Boost · R128 · Crossfeed".
func (s Settings) Summary() string {
	var parts []string
	if s.EQActive() {
		parts = append(parts, "EQ "+s.Preset)
	}
	if s.Normalizer {
		parts = append(parts, "R128")
	}
	if s.Crossfeed {
		parts = append(parts, "Crossfeed")
	}
	if s.LoFi {
		parts = append(parts, "Lo-Fi")
	}
	if len(parts) == 0 {
		return "Bypassed"
	}
	return strings.Join(parts, " · ")
}

// ApplyPreset replaces the band curve with the named preset.
func (s Settings) ApplyPreset(name string) Settings {
	idx := PresetIndex(name)
	if idx < 0 {
		return s
	}
	s.Bands = append([]float64(nil), Presets[idx].Bands[:]...)
	s.Preset = Presets[idx].Name
	return s
}

// SetBand sets one band's gain, clamped, and re-derives the preset name.
func (s Settings) SetBand(band int, gain float64) Settings {
	if band < 0 || band >= NumBands {
		return s
	}
	s = s.Normalize()
	s.Bands[band] = ClampBandGain(gain)
	s.Preset = MatchPreset(s.Bands)
	return s
}

// PresetIndex returns the index of the named preset in Presets, or -1.
func PresetIndex(name string) int {
	for i, p := range Presets {
		if strings.EqualFold(p.Name, name) {
			return i
		}
	}
	return -1
}

// MatchPreset returns the name of the built-in preset whose curve equals
// bands, or PresetCustom when none does.
func MatchPreset(bands []float64) string {
	for _, p := range Presets {
		match := true
		for i := 0; i < NumBands; i++ {
			var g float64
			if i < len(bands) {
				g = bands[i]
			}
			if math.Abs(g-p.Bands[i]) > 1e-9 {
				match = false
				break
			}
		}
		if match {
			return p.Name
		}
	}
	return PresetCustom
}

// ClampBandGain limits a band gain to [MinBandGain, MaxBandGain].
func ClampBandGain(g float64) float64 {
	if math.IsNaN(g) {
		return 0
	}
	return math.Max(MinBandGain, math.Min(MaxBandGain, g))
}

// ClampTargetLUFS limits a loudness target to a sane broadcast range; an unset
// (zero) or invalid target falls back to DefaultTargetLUFS.
func ClampTargetLUFS(t float64) float64 {
	if t == 0 || math.IsNaN(t) || math.IsInf(t, 0) {
		return DefaultTargetLUFS
	}
	return math.Max(MinTargetLUFS, math.Min(MaxTargetLUFS, t))
}

// Load reads dsp.yaml at path. A missing file yields DefaultSettings and no
// error; a corrupt file yields DefaultSettings and the parse error.
func Load(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultSettings(), nil
		}
		return DefaultSettings(), err
	}
	s := DefaultSettings()
	if err := yaml.Unmarshal(data, &s); err != nil {
		return DefaultSettings(), err
	}
	return s.Normalize(), nil
}

// Save writes s to path as YAML, creating the parent directory if needed.
func Save(path string, s Settings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := yaml.Marshal(s.Normalize())
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
