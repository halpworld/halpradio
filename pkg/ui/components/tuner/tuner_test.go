package tuner

import (
	"testing"

	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
)

func TestNextBand(t *testing.T) {
	if NextBand("FM") != "AM" {
		t.Errorf("expected FM -> AM")
	}
	if NextBand("AM") != "SW" {
		t.Errorf("expected AM -> SW")
	}
	if NextBand("SW") != "FM" {
		t.Errorf("expected SW -> FM")
	}
}

func TestCalculateSignalRSSI(t *testing.T) {
	stations := []radio.Station{
		{ID: "kexp", Name: "KEXP", Frequency: "90.3 FM"},
		{ID: "wgn", Name: "WGN 720 AM", Frequency: "720 AM"},
	}

	// Exact match on FM 90.3
	rssiExact, st := CalculateSignalRSSI(stations, "FM", 90.3)
	if st == nil || st.ID != "kexp" {
		t.Fatalf("expected to lock onto KEXP")
	}
	if rssiExact < 0.95 {
		t.Errorf("expected near 1.0 RSSI on exact frequency, got %f", rssiExact)
	}

	// Off frequency FM 95.0 (no station nearby)
	rssiOff, _ := CalculateSignalRSSI(stations, "FM", 95.0)
	if rssiOff > 0.3 {
		t.Errorf("expected low RSSI on off-frequency, got %f", rssiOff)
	}

	// Exact match on AM 720
	rssiAM, stAM := CalculateSignalRSSI(stations, "AM", 720.0)
	if stAM == nil || stAM.ID != "wgn" {
		t.Fatalf("expected to lock onto WGN on AM")
	}
	if rssiAM < 0.95 {
		t.Errorf("expected near 1.0 RSSI on AM 720, got %f", rssiAM)
	}
}

func TestRenderAnalogTunerViewDoesNotPanic(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	stations := []radio.Station{
		{ID: "kexp", Name: "KEXP", Frequency: "90.3 FM"},
		{ID: "bbc1", Name: "BBC Radio 1", Frequency: "98.8 FM"},
	}

	testSizes := []struct {
		w, h int
	}{
		{50, 20},
		{80, 24},
		{100, 30},
		{120, 40},
	}

	for _, s := range testSizes {
		// Locked view
		view := RenderAnalogTunerView(stations, 90.3, "FM", "kexp", s.w, s.h, th)
		if view == "" {
			t.Errorf("expected non-empty rendered tuner view for size %dx%d", s.w, s.h)
		}

		// Unlocked / static noise view
		staticView := RenderAnalogTunerView(stations, 107.5, "FM", "", s.w, s.h, th)
		if staticView == "" {
			t.Errorf("expected non-empty static tuner view for size %dx%d", s.w, s.h)
		}

		// AM & SW band rendering
		amView := RenderAnalogTunerView(stations, 1000.0, "AM", "", s.w, s.h, th)
		if amView == "" {
			t.Errorf("expected non-empty AM tuner view")
		}

		swView := RenderAnalogTunerView(stations, 14.2, "SW", "", s.w, s.h, th)
		if swView == "" {
			t.Errorf("expected non-empty SW tuner view")
		}
	}

	// Small size fallback
	smallView := RenderAnalogTunerView(stations, 90.3, "FM", "", 20, 5, th)
	if smallView == "" {
		t.Errorf("expected small view message")
	}
}

func TestFindNearestStation(t *testing.T) {
	stations := []radio.Station{
		{ID: "kexp", Name: "KEXP", Frequency: "90.3 FM"},
		{ID: "kuow", Name: "KUOW", Frequency: "94.9 FM"},
	}

	st, delta := FindNearestStation(stations, "FM", 91.0)
	if st == nil || st.ID != "kexp" {
		t.Errorf("expected nearest station KEXP, got %v", st)
	}
	if delta > 0 {
		t.Errorf("expected negative delta for 90.3 relative to 91.0, got %f", delta)
	}

	st2, delta2 := FindNearestStation(stations, "FM", 94.0)
	if st2 == nil || st2.ID != "kuow" {
		t.Errorf("expected nearest station KUOW, got %v", st2)
	}
	if delta2 < 0 {
		t.Errorf("expected positive delta for 94.9 relative to 94.0, got %f", delta2)
	}
}

func TestFormatSMeter(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	// Test low RSSI
	sLow := FormatSMeter(0.1, th)
	if sLow == "" {
		t.Errorf("expected non-empty S-meter")
	}

	// Test high RSSI
	sHigh := FormatSMeter(0.98, th)
	if sHigh == "" {
		t.Errorf("expected non-empty S-meter")
	}
}
