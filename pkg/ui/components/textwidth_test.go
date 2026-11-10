package components

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
)

func TestWidthHelpersKeepWidth(t *testing.T) {
	cases := []string{"", "abc", "日本語のラジオ局", "Café del Mar", "🎙️ DJ ❤️ Live 🇸🇪 radio", strings.Repeat("x", 50)}
	for _, s := range cases {
		for _, w := range []int{0, 1, 3, 4, 10, 30} {
			if got := lipgloss.Width(truncate(s, w)); got > w && w > 0 {
				t.Errorf("truncate(%q,%d) width %d", s, w, got)
			}
			if got := lipgloss.Width(padRight(s, w)); got > w {
				t.Errorf("padRight(%q,%d) width %d", s, w, got)
			}
			if lipgloss.Width(s) <= w && truncate(s, w) != s {
				t.Errorf("truncate changed a string that fits: %q", s)
			}
		}
		for _, line := range wrapPlain(s, 5) {
			if lipgloss.Width(line) > 5 {
				t.Errorf("wrapPlain line %q wider than 5", line)
			}
		}
	}
	if got := truncate("Hello World", 8); got != "Hello..." {
		t.Errorf("truncate = %q", got)
	}
}

// TTY-7: one huge station name or lyric line used to take seconds per frame
// (50,000 runes measured 7.4 s in RenderStationList). The helpers must be
// linear so a frame stays fast.
func TestHugeStringsRenderQuickly(t *testing.T) {
	huge := strings.Repeat("界", 200000)
	th := theme.GetTheme("tokyonight")

	start := time.Now()
	_ = RenderStationList([]radio.Station{{ID: "x", Name: huge, Genre: huge, URL: "http://example.com/"}}, 0, "", 100, 20, true, th)
	lines := wrapPlain(huge, 60)
	// Linear code takes ~0.1 s (~1.6 s under -race); the quadratic version
	// took minutes, so a generous race budget still catches a regression.
	budget := 3 * time.Second
	if raceEnabled {
		budget = 20 * time.Second
	}
	if d := time.Since(start); d > budget {
		t.Fatalf("rendering a 200k-rune name and wrapping it took %v", d)
	}
	if len(lines) != 200000*2/60+1 {
		t.Errorf("wrapPlain produced %d lines", len(lines))
	}
}
