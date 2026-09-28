package dsp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

const testRate = 44100

// sine fills an interleaved stereo buffer with a sine of the given peak
// amplitude on each channel (0 disables that channel).
func sine(seconds, freq, ampL, ampR float64) []float32 {
	n := int(seconds * testRate)
	buf := make([]float32, 2*n)
	for i := 0; i < n; i++ {
		v := math.Sin(2 * math.Pi * freq * float64(i) / testRate)
		buf[2*i] = float32(ampL * v)
		buf[2*i+1] = float32(ampR * v)
	}
	return buf
}

func rms(buf []float32, ch int) float64 {
	var sum float64
	n := 0
	for i := ch; i < len(buf); i += 2 {
		sum += float64(buf[i]) * float64(buf[i])
		n++
	}
	return math.Sqrt(sum / float64(n))
}

func db(x float64) float64 { return 20 * math.Log10(x) }

// measureLUFS runs buf through a fresh normalizer (whose gain is ignored) and
// returns the sliding integrated loudness of its input.
func measureLUFS(buf []float32) float64 {
	n := newNormalizer(testRate, DefaultTargetLUFS)
	for i := 0; i+1 < len(buf); i += 2 {
		n.process(float64(buf[i]), float64(buf[i+1]))
	}
	return n.measured
}

func TestDefaultSettingsBypassed(t *testing.T) {
	s := DefaultSettings()
	if s.Active() {
		t.Fatal("default settings should bypass every stage")
	}
	if len(s.Bands) != NumBands || s.Preset != PresetFlat || s.TargetLUFS != -14 {
		t.Fatalf("unexpected defaults: %+v", s)
	}
	if s.Summary() != "Bypassed" {
		t.Errorf("Summary() = %q", s.Summary())
	}
}

func TestSettingsNormalize(t *testing.T) {
	s := Settings{Bands: []float64{99, -99, math.NaN()}, TargetLUFS: -80}.Normalize()
	if len(s.Bands) != NumBands {
		t.Fatalf("expected %d bands, got %d", NumBands, len(s.Bands))
	}
	if s.Bands[0] != MaxBandGain || s.Bands[1] != MinBandGain || s.Bands[2] != 0 {
		t.Errorf("bands not clamped: %v", s.Bands)
	}
	if s.TargetLUFS != MinTargetLUFS {
		t.Errorf("target not clamped: %v", s.TargetLUFS)
	}
	if s.Preset != PresetCustom {
		t.Errorf("preset = %q, want Custom", s.Preset)
	}
	if got := (Settings{TargetLUFS: 0}).Normalize().TargetLUFS; got != DefaultTargetLUFS {
		t.Errorf("unset target should default, got %v", got)
	}
}

func TestPresetsAndBands(t *testing.T) {
	if len(Presets) < 6 {
		t.Fatalf("expected the six required presets, got %d", len(Presets))
	}
	for _, want := range []string{"Flat", "Bass Boost", "Vocal Clarity", "Electronic", "Acoustic", "Deep Focus"} {
		if PresetIndex(want) < 0 {
			t.Errorf("missing preset %q", want)
		}
	}

	s := DefaultSettings().ApplyPreset("bass boost")
	if s.Preset != "Bass Boost" || s.Bands[0] != 7 {
		t.Fatalf("ApplyPreset: %+v", s)
	}
	if !s.EQActive() || !strings.Contains(s.Summary(), "Bass Boost") {
		t.Errorf("summary = %q", s.Summary())
	}

	edited := s.SetBand(9, 3)
	if edited.Preset != PresetCustom || edited.Bands[9] != 3 {
		t.Errorf("SetBand should produce a Custom curve: %+v", edited)
	}
	if s.Bands[9] != 0 {
		t.Error("SetBand must not alias the original band slice")
	}
	if back := edited.SetBand(9, 0); back.Preset != "Bass Boost" {
		t.Errorf("restoring the curve should re-match the preset, got %q", back.Preset)
	}
	if got := s.SetBand(9, 40).Bands[9]; got != MaxBandGain {
		t.Errorf("SetBand should clamp, got %v", got)
	}
	if unchanged := s.ApplyPreset("nope"); unchanged.Preset != s.Preset {
		t.Error("unknown preset should be ignored")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dsp.yaml")

	got, err := Load(path)
	if err != nil || got.Active() {
		t.Fatalf("missing file should load defaults without error: %+v, %v", got, err)
	}

	want := DefaultSettings().ApplyPreset("Deep Focus")
	want.Normalizer = true
	want.Crossfeed = true
	want.LoFi = true
	want.TargetLUFS = -18
	if err := Save(path, want); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "target") {
		t.Errorf("target loudness belongs in config.yaml, not dsp.yaml:\n%s", data)
	}

	got, err = Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Preset != "Deep Focus" || !got.Normalizer || !got.Crossfeed || !got.LoFi {
		t.Errorf("round trip lost settings: %+v", got)
	}
	for i := range want.Bands {
		if got.Bands[i] != want.Bands[i] {
			t.Errorf("band %d = %v, want %v", i, got.Bands[i], want.Bands[i])
		}
	}
	if got.TargetLUFS != DefaultTargetLUFS {
		t.Errorf("loaded target = %v, want default", got.TargetLUFS)
	}

	if err := os.WriteFile(path, []byte("bands: [oops"), 0644); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(path); err == nil || got.Active() {
		t.Errorf("corrupt file should error and fall back to defaults: %+v, %v", got, err)
	}
}

func TestBypassLeavesAudioUntouched(t *testing.T) {
	c := NewChain(testRate, DefaultSettings())
	buf := sine(0.1, 440, 0.5, 0.25)
	orig := append([]float32(nil), buf...)
	c.Process(buf)
	for i := range buf {
		if buf[i] != orig[i] {
			t.Fatalf("sample %d changed while bypassed", i)
		}
	}
}

func TestEqualizerBandResponse(t *testing.T) {
	for i, f := range BandFrequencies {
		var bq biquad
		bq.peaking(testRate, f, eqBandQ, 6)
		if got := db(bq.magnitudeAt(testRate, f)); math.Abs(got-6) > 0.01 {
			t.Errorf("band %s: gain at centre = %.3f dB, want 6", BandLabels[i], got)
		}
	}

	// A +9 dB boost at 1 kHz lifts a 1 kHz tone and leaves 32 Hz almost alone.
	s := DefaultSettings().SetBand(5, 9)
	for _, tc := range []struct {
		freq    float64
		wantDB  float64
		tolerDB float64
	}{{1000, 9, 0.3}, {32, 0, 0.3}} {
		c := NewChain(testRate, s)
		buf := sine(1, tc.freq, 0.05, 0.05)
		in := rms(buf[len(buf)/2:], 0)
		c.Process(buf)
		out := rms(buf[len(buf)/2:], 0)
		if got := db(out / in); math.Abs(got-tc.wantDB) > tc.tolerDB {
			t.Errorf("%v Hz: gain %.2f dB, want %.1f", tc.freq, got, tc.wantDB)
		}
	}
}

func TestEqualizerSkipsBandsAboveNyquist(t *testing.T) {
	e := newEqualizer(22050)
	bands := make([]float64, NumBands)
	for i := range bands {
		bands[i] = 6
	}
	e.setGains(bands)
	if e.active[9] {
		t.Error("16 kHz band must be skipped at a 22.05 kHz sample rate")
	}
	if !e.active[8] {
		t.Error("8 kHz band should still run at 22.05 kHz")
	}
}

func TestCrossfeedImaging(t *testing.T) {
	s := DefaultSettings()
	s.Crossfeed = true

	// A hard-left low tone should bleed into the right ear, attenuated.
	c := NewChain(testRate, s)
	buf := sine(1, 200, 0.3, 0)
	c.Process(buf)
	tail := buf[len(buf)/2:]
	bleed := db(rms(tail, 1) / rms(tail, 0))
	if bleed < -12 || bleed > -3 {
		t.Errorf("200 Hz crossfeed bleed = %.1f dB, want between -12 and -3", bleed)
	}

	// High frequencies are shadowed by the head: much less bleed.
	c = NewChain(testRate, s)
	buf = sine(1, 8000, 0.3, 0)
	c.Process(buf)
	tail = buf[len(buf)/2:]
	if hi := db(rms(tail, 1) / rms(tail, 0)); hi > bleed-8 {
		t.Errorf("8 kHz bleed %.1f dB should be well below 200 Hz bleed %.1f dB", hi, bleed)
	}

	// A centred (mono) source stays centred and roughly level.
	c = NewChain(testRate, s)
	buf = sine(1, 440, 0.2, 0.2)
	c.Process(buf)
	tail = buf[len(buf)/2:]
	if d := math.Abs(rms(tail, 0) - rms(tail, 1)); d > 1e-6 {
		t.Errorf("mono source drifted off centre by %v", d)
	}
	if g := db(rms(tail, 0) / (0.2 / math.Sqrt2)); math.Abs(g) > 1.5 {
		t.Errorf("mono level changed by %.2f dB", g)
	}
}

func TestLoFiColoursAudio(t *testing.T) {
	s := DefaultSettings()
	s.LoFi = true

	// High frequencies are rolled off like a worn tape head…
	c := NewChain(testRate, s)
	buf := sine(1, 12000, 0.3, 0.3)
	c.Process(buf)
	if g := db(rms(buf[len(buf)/2:], 0) / (0.3 / math.Sqrt2)); g > -9 {
		t.Errorf("12 kHz only attenuated by %.1f dB", g)
	}
	// …and silence gains a faint surface hiss, far below the music.
	c = NewChain(testRate, s)
	silent := make([]float32, 2*testRate)
	c.Process(silent)
	hiss := db(rms(silent[len(silent)/2:], 0))
	if hiss > -60 || math.IsInf(hiss, -1) {
		t.Errorf("hiss level %.1f dBFS, want present but below -60", hiss)
	}
}

// BS.1770-4 conformance: a stereo 997 Hz sine at -20 dBFS measures -20 LUFS,
// and a full-scale one-channel sine measures -3.01 LUFS.
func TestLoudnessMeasurementConformance(t *testing.T) {
	amp := math.Pow(10, -20.0/20)
	if got := measureLUFS(sine(5, 997, amp, amp)); math.Abs(got-(-20)) > 0.1 {
		t.Errorf("stereo -20 dBFS sine measured %.3f LUFS, want -20", got)
	}
	if got := measureLUFS(sine(5, 997, 1, 0)); math.Abs(got-(-3.01)) > 0.1 {
		t.Errorf("0 dBFS mono sine measured %.3f LUFS, want -3.01", got)
	}
	if got := measureLUFS(make([]float32, 2*testRate)); !math.IsInf(got, -1) {
		t.Errorf("digital silence should be gated out, got %v", got)
	}
}

// ampForLUFS returns the per-channel sine peak giving a stereo 997 Hz tone the
// requested loudness.
func ampForLUFS(lufs float64) float64 {
	return math.Sqrt(math.Pow(10, lufs/10))
}

func TestNormalizerEqualizesStations(t *testing.T) {
	s := DefaultSettings()
	s.Normalizer = true

	for _, station := range []float64{-24, -8} { // quiet classical, crushed pop
		c := NewChain(testRate, s)
		amp := ampForLUFS(station)
		buf := sine(12, 997, amp, amp)
		c.Process(buf)
		out := measureLUFS(buf[len(buf)-2*4*testRate:])
		if math.Abs(out-DefaultTargetLUFS) > 0.5 {
			t.Errorf("%v LUFS station came out at %.2f LUFS, want -14 ±0.5", station, out)
		}
		lufs, gain, ok := c.Loudness()
		if !ok || math.Abs(lufs-station) > 0.2 || math.Abs(gain-(DefaultTargetLUFS-station)) > 0.2 {
			t.Errorf("%v LUFS station: meter %.2f LUFS, gain %.2f dB, ok %v", station, lufs, gain, ok)
		}
	}
}

func TestNormalizerRespectsTargetAndClamp(t *testing.T) {
	s := DefaultSettings()
	s.Normalizer = true
	s.TargetLUFS = -20
	c := NewChain(testRate, s)
	amp := ampForLUFS(-10)
	c.Process(sine(8, 997, amp, amp))
	if _, gain, _ := c.Loudness(); math.Abs(gain-(-10)) > 0.2 {
		t.Errorf("gain %.2f dB, want -10 for a -20 LUFS target", gain)
	}

	// A near-silent station is lifted by at most maxBoostDB.
	c = NewChain(testRate, DefaultSettings())
	c.Update(Settings{Normalizer: true})
	amp = ampForLUFS(-50)
	c.Process(sine(8, 997, amp, amp))
	if _, gain, _ := c.Loudness(); gain > maxBoostDB+1e-9 {
		t.Errorf("gain %.2f dB exceeds the %v dB boost cap", gain, maxBoostDB)
	}
}

// Music is bursty; a slow sliding-window AGC must not ride individual beats.
func TestNormalizerDoesNotPump(t *testing.T) {
	s := DefaultSettings()
	s.Normalizer = true
	c := NewChain(testRate, s)

	amp := ampForLUFS(-12)
	pos := 0 // sample position, so the gate keeps its phase across calls
	beat := func(seconds float64) []float32 {
		n := int(seconds * testRate)
		buf := make([]float32, 2*n)
		for i := 0; i < n; i, pos = i+1, pos+1 {
			// 2 Hz on/off gating, like a kick-heavy loop.
			if (pos*4/testRate)%2 == 0 {
				v := float32(amp * math.Sin(2*math.Pi*220*float64(pos)/testRate))
				buf[2*i], buf[2*i+1] = v, v
			}
		}
		return buf
	}
	c.Process(beat(12))

	minG, maxG := math.Inf(1), math.Inf(-1)
	for i := 0; i < 50; i++ {
		c.Process(beat(0.1))
		_, g, _ := c.Loudness()
		minG, maxG = math.Min(minG, g), math.Max(maxG, g)
	}
	if maxG-minG > 0.5 {
		t.Errorf("gain moved %.2f dB across beats (%.2f..%.2f), expected a steady gain", maxG-minG, minG, maxG)
	}

	// A pause in the programme holds the gain instead of boosting the silence.
	c.Process(make([]float32, 2*5*testRate))
	if _, g, _ := c.Loudness(); math.Abs(g-maxG) > 0.5 {
		t.Errorf("gain drifted to %.2f dB during silence (was %.2f)", g, maxG)
	}
}

func TestLimiterHoldsCeiling(t *testing.T) {
	s := DefaultSettings()
	for i := range s.Bands {
		s.Bands[i] = MaxBandGain
	}
	s.Normalizer = true
	c := NewChain(testRate, s)
	buf := sine(3, 125, 0.9, 0.9)
	c.Process(buf)
	var peak float64
	for _, v := range buf[len(buf)/3:] {
		peak = math.Max(peak, math.Abs(float64(v)))
	}
	if peak > limiterCeiling*1.03 {
		t.Errorf("peak %.3f exceeds the %.3f ceiling", peak, limiterCeiling)
	}

	// Instantaneous transients never exceed full scale.
	c = NewChain(testRate, s)
	spikes := make([]float32, 2*testRate)
	for i := 0; i < len(spikes); i += 2000 {
		spikes[i] = 1
	}
	c.Process(spikes)
	for i, v := range spikes {
		if v > 1 || v < -1 {
			t.Fatalf("sample %d = %v escaped [-1, 1]", i, v)
		}
	}
}

func TestLiveUpdateKeepsStreaming(t *testing.T) {
	c := NewChain(testRate, DefaultSettings())
	buf := sine(0.2, 440, 0.2, 0.2)
	c.Process(buf)

	s := DefaultSettings().ApplyPreset("Electronic")
	s.Crossfeed, s.Normalizer, s.LoFi = true, true, true
	c.Update(s)
	if got := c.Settings(); got.Preset != "Electronic" || !got.Crossfeed {
		t.Fatalf("Update not applied: %+v", got)
	}
	c.Process(sine(0.2, 440, 0.2, 0.2))

	c.Update(DefaultSettings())
	if _, _, ok := c.Loudness(); ok {
		t.Error("loudness meter should report off once the normalizer is disabled")
	}
	for _, v := range buf {
		if math.IsNaN(float64(v)) {
			t.Fatal("NaN in output")
		}
	}
}

func pcmBytes(buf []float32) []byte {
	out := make([]byte, 2*len(buf))
	for i, v := range buf {
		binary.LittleEndian.PutUint16(out[2*i:], uint16(toInt16(float64(v))))
	}
	return out
}

func TestPCMReaderChunking(t *testing.T) {
	s := DefaultSettings().ApplyPreset("Bass Boost")
	s.Crossfeed = true
	src := pcmBytes(sine(0.5, 300, 0.4, 0.1))

	whole, err := io.ReadAll(NewPCMReader(bytes.NewReader(src), NewChain(testRate, s)))
	if err != nil {
		t.Fatal(err)
	}
	if len(whole) != len(src) {
		t.Fatalf("read %d bytes, want %d", len(whole), len(src))
	}
	if bytes.Equal(whole, src) {
		t.Fatal("active chain should have changed the audio")
	}

	// Frames split across one-byte source reads, and a caller reading three
	// bytes at a time, must see exactly the same processed stream.
	r := NewPCMReader(iotest.OneByteReader(bytes.NewReader(src)), NewChain(testRate, s))
	var got []byte
	p := make([]byte, 3)
	for {
		n, err := r.Read(p)
		got = append(got, p[:n]...)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(got, whole) {
		t.Fatal("chunked read produced different audio")
	}

	// A stream ending mid-frame drops the dangling bytes.
	short, err := io.ReadAll(NewPCMReader(bytes.NewReader(src[:10]), NewChain(testRate, DefaultSettings())))
	if err != nil || len(short) != 8 {
		t.Errorf("truncated stream: %d bytes, err %v; want 8", len(short), err)
	}
}

func TestLavfiGraph(t *testing.T) {
	if g := LavfiGraph(DefaultSettings()); g != "" {
		t.Errorf("bypassed graph = %q", g)
	}
	if af := MPVAudioFilter(DefaultSettings()); af != "" {
		t.Errorf("bypassed mpv af = %q", af)
	}

	s := DefaultSettings().SetBand(0, 7).SetBand(5, -3.5)
	s.LoFi, s.Crossfeed, s.Normalizer = true, true, true
	s.TargetLUFS = -16
	want := "equalizer=f=32:t=o:w=1:g=7,equalizer=f=1000:t=o:w=1:g=-3.5," +
		"highpass=f=90,lowpass=f=6500,asoftclip=type=tanh," +
		"crossfeed=strength=0.3:range=0.5,loudnorm=I=-16:TP=-1:LRA=11"
	if g := LavfiGraph(s); g != want {
		t.Errorf("graph =\n%s\nwant\n%s", g, want)
	}
	if af := MPVAudioFilter(s); af != "@halpradio:lavfi=["+want+"]" {
		t.Errorf("mpv af = %q", af)
	}
}

// The whole rack must run far faster than real time so it can never starve
// the audio device.
func TestChainRealTimeFactor(t *testing.T) {
	if raceEnabled {
		t.Skip("timing is meaningless under the race detector")
	}
	s := DefaultSettings().ApplyPreset("Electronic")
	s.Normalizer, s.Crossfeed, s.LoFi = true, true, true
	c := NewChain(testRate, s)
	buf := sine(5, 440, 0.3, 0.3)

	start := time.Now()
	c.Process(buf)
	elapsed := time.Since(start)
	if factor := 5 * time.Second.Seconds() / elapsed.Seconds(); factor < 20 {
		t.Errorf("full rack runs at only %.0fx real time (%v for 5 s of audio)", factor, elapsed)
	}
}

func BenchmarkChainFullRack(b *testing.B) {
	s := DefaultSettings().ApplyPreset("Electronic")
	s.Normalizer, s.Crossfeed, s.LoFi = true, true, true
	c := NewChain(testRate, s)
	buf := sine(1, 440, 0.3, 0.3) // one second of audio per iteration
	b.SetBytes(int64(len(buf) * 2))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Process(buf)
	}
}
