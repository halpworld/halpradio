package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
)

func TestPadRightDisplayWidth(t *testing.T) {
	testCases := []struct {
		input    string
		width    int
		expected int
	}{
		{"Smooth Radio UK", 35, 35},
		{"CNR-1 Voice of China (中国之声)", 35, 35},
		{"Guangdong Music FM (广东音乐之声)", 35, 35},
		{"AsiaFM HD (亚洲音乐台)", 35, 35},
		{"Shonan Beach FM 78.9 (湘南ビーチFM)", 35, 35},
		{"🇨🇳", 8, 8},
		{"🇯🇵", 8, 8},
		{"🇬🇧", 8, 8},
		{"🌐", 8, 8},
	}

	for _, tc := range testCases {
		res := padRight(tc.input, tc.width)
		actualWidth := lipgloss.Width(res)
		if actualWidth != tc.expected {
			t.Errorf("padRight(%q, %d) produced visual width %d, expected %d", tc.input, tc.width, actualWidth, tc.expected)
		}
	}
}

func TestTruncateDisplayWidth(t *testing.T) {
	testCases := []struct {
		input    string
		maxWidth int
	}{
		{"Smooth Radio UK", 20},
		{"CNR-1 Voice of China (中国之声)", 25},
		{"Guangdong Music FM (广东音乐之声)", 20},
		{"AsiaFM HD (亚洲音乐台)", 15},
		{"Shonan Beach FM 78.9 (湘南ビーチFM)", 20},
	}

	for _, tc := range testCases {
		res := truncate(tc.input, tc.maxWidth)
		w := lipgloss.Width(res)
		if w > tc.maxWidth {
			t.Errorf("truncate(%q, %d) produced visual width %d > %d (result: %q)", tc.input, tc.maxWidth, w, tc.maxWidth, res)
		}
	}
}

func TestRenderStationListColumnAlignment(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	stations := []radio.Station{
		{
			ID:      "smooth-uk",
			Name:    "Smooth Radio UK",
			Genre:   "Soft Rock / Easy Listening / Soul",
			Country: "GB",
			Bitrate: 128,
			Codec:   "MP3",
		},
		{
			ID:      "cnr-voice-of-china",
			Name:    "CNR-1 Voice of China (中国之声)",
			Genre:   "News / Talk / National",
			Country: "CN",
			Bitrate: 64,
			Codec:   "MP3",
		},
		{
			ID:      "guangdong-music-fm",
			Name:    "Guangdong Music FM (广东音乐之声)",
			Genre:   "Mandopop / Cantopop / Hits",
			Country: "CN",
			Bitrate: 64,
			Codec:   "MP3",
		},
		{
			ID:      "shonan-beach-fm",
			Name:    "Shonan Beach FM 78.9 (湘南ビーチFM)",
			Genre:   "Jazz / Smooth Jazz / Pop",
			Country: "JP",
			Bitrate: 128,
			Codec:   "MP3",
		},
	}

	rendered := RenderStationList(stations, 1, "", 100, 20, true, th)
	lines := strings.Split(rendered, "\n")

	if len(lines) < 4 {
		t.Fatalf("Expected at least 4 rendered lines, got %d", len(lines))
	}

	// Verify all row lines have identical display width
	var rowWidths []int
	for i := 1; i < len(lines)-1; i++ { // Skip header and footer
		w := lipgloss.Width(lines[i])
		rowWidths = append(rowWidths, w)
	}

	for i := 1; i < len(rowWidths); i++ {
		if rowWidths[i] != rowWidths[0] {
			t.Errorf("Row %d width (%d) does not match Row 0 width (%d)", i, rowWidths[i], rowWidths[0])
		}
	}
}
