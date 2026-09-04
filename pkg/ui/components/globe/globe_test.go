package globe

import (
	"testing"

	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
)

func TestBrailleCanvasDots(t *testing.T) {
	c := NewBrailleCanvas(10, 10)
	if c.WidthDots != 20 || c.HeightDots != 40 {
		t.Fatalf("expected 20x40 dots, got %dx%d", c.WidthDots, c.HeightDots)
	}

	// Set dot (0, 0)
	c.SetDot(0, 0)
	if c.grid[0] != 0x01 {
		t.Errorf("expected dot (0,0) mask 0x01, got 0x%02x", c.grid[0])
	}

	// Set dot (1, 3) -> should set 0x80 (dot 8)
	c.SetDot(1, 3)
	if c.grid[0] != (0x01 | 0x80) {
		t.Errorf("expected combined mask 0x81, got 0x%02x", c.grid[0])
	}

	// Out of bounds safety
	c.SetDot(-1, 0)
	c.SetDot(100, 100)
}

func TestRenderGlobeViewDoesNotPanic(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	clusters := []radio.StationCluster{
		{
			ID:          "tokyo",
			City:        "Tokyo",
			CountryCode: "JP",
			CountryName: "Japan",
			Flag:        "🇯🇵",
			Lat:         35.6762,
			Lon:         139.6503,
			Stations: []radio.Station{
				{ID: "tokyo-1", Name: "Tokyo FM", Frequency: "80.0 FM"},
			},
		},
		{
			ID:          "london",
			City:        "London",
			CountryCode: "GB",
			CountryName: "United Kingdom",
			Flag:        "🇬🇧",
			Lat:         51.5074,
			Lon:         -0.1278,
			Stations: []radio.Station{
				{ID: "bbc-1", Name: "BBC Radio 1", Frequency: "98.8 FM"},
			},
		},
	}

	testSizes := []struct {
		w, h int
	}{
		{20, 8}, // Very small
		{60, 18},
		{80, 24},
		{100, 30},
		{120, 40},
		{160, 50},
	}

	for _, s := range testSizes {
		view := RenderGlobeView(clusters, 35.6, 139.6, 1.0, 0, "tokyo-1", s.w, s.h, th)
		if view == "" {
			t.Errorf("expected non-empty rendered globe view for size %dx%d", s.w, s.h)
		}

		// Southern & Western hemisphere view with empty clusters
		swView := RenderGlobeView(nil, -33.86, -70.66, 1.5, 0, "", s.w, s.h, th)
		if swView == "" {
			t.Errorf("expected non-empty southern/western globe view")
		}
	}
}

func TestGlobeDirections(t *testing.T) {
	if latDirection(10.0) != "N" || latDirection(-10.0) != "S" {
		t.Errorf("incorrect lat direction")
	}
	if lonDirection(10.0) != "E" || lonDirection(-10.0) != "W" {
		t.Errorf("incorrect lon direction")
	}
}

func TestIsLandCoverage(t *testing.T) {
	// Tokyo: Land
	if !IsLand(35.6762, 139.6503) {
		t.Errorf("expected Tokyo to be land")
	}

	// London: Land
	if !IsLand(51.5074, -0.1278) {
		t.Errorf("expected London to be land")
	}

	// Mid Atlantic Ocean: Ocean (false)
	if IsLand(30.0, -40.0) {
		t.Errorf("expected Mid Atlantic (30, -40) to be ocean")
	}

	// Mid Pacific Ocean: Ocean (false)
	if IsLand(0.0, -140.0) {
		t.Errorf("expected Mid Pacific (0, -140) to be ocean")
	}
}
