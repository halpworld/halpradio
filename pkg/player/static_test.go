package player

import (
	"bytes"
	"sync"
	"testing"
)

func TestStaticSynthesizerLifecycleAndAudioGeneration(t *testing.T) {
	synth := NewStaticSynthesizer(44100)
	if synth.IsEnabled() {
		t.Errorf("expected synth disabled initially")
	}

	// While disabled, Read should output silence (all zeros)
	buf := make([]byte, 1024)
	n, err := synth.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error on Read: %v", err)
	}
	if n != 1024 {
		t.Errorf("expected 1024 bytes, got %d", n)
	}
	zeroBuf := make([]byte, 1024)
	if !bytes.Equal(buf, zeroBuf) {
		t.Errorf("expected silence when disabled")
	}

	// Enable static
	synth.SetEnabled(true)
	synth.SetParams(0.0, 90.3, "FM", 0.8, false)
	if !synth.IsEnabled() {
		t.Errorf("expected synth enabled")
	}

	// Read should now produce non-zero PCM audio samples
	n, err = synth.Read(buf)
	if err != nil {
		t.Fatalf("unexpected error on Read: %v", err)
	}
	if bytes.Equal(buf, zeroBuf) {
		t.Errorf("expected non-zero audio static when enabled")
	}

	// Test volume scaling and mute
	synth.SetVolume(0.0, false)
	n, _ = synth.Read(buf)
	if !bytes.Equal(buf, zeroBuf) {
		t.Errorf("expected silence when volume is 0")
	}

	synth.SetVolume(0.8, true)
	n, _ = synth.Read(buf)
	if !bytes.Equal(buf, zeroBuf) {
		t.Errorf("expected silence when muted")
	}
}

func TestStaticSynthesizerRSSIAttenuation(t *testing.T) {
	synth := NewStaticSynthesizer(44100)
	synth.SetEnabled(true)

	calcRMS := func(signal float64, band string) float64 {
		synth.SetParams(signal, 100.0, band, 1.0, false)
		buf := make([]byte, 4096)
		// Burn first chunk to settle filters
		_, _ = synth.Read(buf)
		_, _ = synth.Read(buf)

		var sumSq float64
		sampleCount := len(buf) / 2
		for i := 0; i < len(buf); i += 2 {
			val := int16(uint16(buf[i]) | (uint16(buf[i+1]) << 8))
			norm := float64(val) / 32768.0
			sumSq += norm * norm
		}
		return sumSq / float64(sampleCount)
	}

	rmsPureStatic := calcRMS(0.0, "FM")
	rmsWeakSignal := calcRMS(0.5, "FM")
	rmsLockedSignal := calcRMS(0.98, "FM")

	if rmsPureStatic <= 0 {
		t.Errorf("expected pure static RMS > 0, got %f", rmsPureStatic)
	}
	if rmsWeakSignal >= rmsPureStatic {
		t.Errorf("expected weak signal static RMS (%f) < pure static RMS (%f)", rmsWeakSignal, rmsPureStatic)
	}
	if rmsLockedSignal >= rmsWeakSignal {
		t.Errorf("expected locked signal static RMS (%f) < weak signal RMS (%f)", rmsLockedSignal, rmsWeakSignal)
	}
}

func TestStaticSynthesizerBandsAndHeterodyne(t *testing.T) {
	bands := []string{"FM", "AM", "SW"}
	synth := NewStaticSynthesizer(44100)
	synth.SetEnabled(true)

	for _, b := range bands {
		synth.SetParams(0.2, 90.0, b, 0.8, false)
		synth.SetNearestStationCarrier(90.1)
		buf := make([]byte, 2048)
		n, err := synth.Read(buf)
		if err != nil || n != 2048 {
			t.Errorf("band %s read failed: n=%d, err=%v", b, n, err)
		}
	}
}

func TestStaticSynthesizerConcurrency(t *testing.T) {
	synth := NewStaticSynthesizer(44100)
	synth.SetEnabled(true)

	var wg sync.WaitGroup
	// Reader goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 1024)
		for i := 0; i < 200; i++ {
			_, _ = synth.Read(buf)
		}
	}()

	// Parameter updater goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			synth.SetParams(float64(i%10)/10.0, 90.0+float64(i)*0.1, "FM", 0.7, i%2 == 0)
			synth.SetNearestStationCarrier(92.0)
		}
	}()

	wg.Wait()
}

func TestManagerTunerModeMethods(t *testing.T) {
	pm := NewManager("mock", 80, nil)
	if pm.IsTunerActive() {
		t.Errorf("expected TunerActive=false initially")
	}

	pm.SetTunerMode(true, 0.5, 93.9, "FM")
	if !pm.IsTunerActive() {
		t.Errorf("expected TunerActive=true after SetTunerMode")
	}

	pm.UpdateTunerSignal(0.8, 93.9, "FM")
	if pm.lastTunerRSSI != 0.8 {
		t.Errorf("expected lastTunerRSSI=0.8, got %f", pm.lastTunerRSSI)
	}

	// Test volume adjustment in tuner mode
	vol := pm.SetVolume(90)
	if vol != 90 {
		t.Errorf("expected SetVolume to return 90, got %d", vol)
	}

	// Test mute in tuner mode
	muted := pm.ToggleMute()
	if !muted {
		t.Errorf("expected ToggleMute to return true")
	}
	muted = pm.ToggleMute()
	if muted {
		t.Errorf("expected ToggleMute to return false (unmuted)")
	}

	pm.SetTunerMode(false, 1.0, 93.9, "FM")
	if pm.IsTunerActive() {
		t.Errorf("expected TunerActive=false after disabling")
	}

	_ = pm.Close()
}
