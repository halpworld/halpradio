package art

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

// solidPNG encodes a w x h image of a single colour.
func solidPNG(t *testing.T, w, h int, c color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode solid png: %v", err)
	}
	return buf.Bytes()
}

func TestRenderHalfBlockColours(t *testing.T) {
	tests := []struct {
		name   string
		colour color.RGBA
		wantFg string
		wantBg string
	}{
		{"red", color.RGBA{R: 0xff, A: 0xff}, "\x1b[38;2;255;0;0m", "\x1b[48;2;255;0;0m"},
		{"white", color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, "\x1b[38;2;255;255;255m", "\x1b[48;2;255;255;255m"},
		{"black", color.RGBA{A: 0xff}, "\x1b[38;2;0;0;0m", "\x1b[48;2;0;0;0m"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// A square source in a square cell block fills the block, so
			// every cell carries the source colour.
			lines, err := NewRenderer(ProtocolHalfBlock).Render(solidPNG(t, 32, 32, tc.colour), 8, 4)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			for i, line := range lines {
				if !strings.Contains(line, tc.wantFg) {
					t.Fatalf("line %d missing fg %q: %q", i, tc.wantFg, line)
				}
				if !strings.Contains(line, tc.wantBg) {
					t.Fatalf("line %d missing bg %q: %q", i, tc.wantBg, line)
				}
				if got := strings.Count(line, upperHalfBlock); got != 8 {
					t.Fatalf("line %d has %d block glyphs, want 8", i, got)
				}
			}
		})
	}
}

func TestRenderBrailleDotCoverage(t *testing.T) {
	tests := []struct {
		name   string
		colour color.RGBA
		want   rune
	}{
		{"black lights no dots", color.RGBA{A: 0xff}, brailleBase},
		{"white lights every dot", color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}, brailleBase + 0xff},
		{"mid grey lights every dot", color.RGBA{R: 0x80, G: 0x80, B: 0x80, A: 0xff}, brailleBase + 0xff},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines, err := NewRenderer(ProtocolBraille).Render(solidPNG(t, 64, 64, tc.colour), 8, 4)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			for i, line := range lines {
				body := strings.TrimSuffix(line, sgrReset)
				for _, r := range body {
					if r == '\x1b' || r == '[' || r == ';' || r == 'm' || (r >= '0' && r <= '9') {
						continue
					}
					if r != tc.want {
						t.Fatalf("line %d: got rune %U, want %U", i, r, tc.want)
					}
				}
			}
		})
	}
}

func TestRGBKeyDistinguishesColours(t *testing.T) {
	tests := []struct {
		a, b [3]uint8
		same bool
	}{
		{[3]uint8{1, 2, 3}, [3]uint8{1, 2, 3}, true},
		{[3]uint8{1, 2, 3}, [3]uint8{3, 2, 1}, false},
		{[3]uint8{0, 0, 0}, [3]uint8{0, 0, 1}, false},
	}
	for _, tc := range tests {
		ka := rgbKey(tc.a[0], tc.a[1], tc.a[2])
		kb := rgbKey(tc.b[0], tc.b[1], tc.b[2])
		if (ka == kb) != tc.same {
			t.Fatalf("rgbKey(%v)==rgbKey(%v) was %v, want %v", tc.a, tc.b, ka == kb, tc.same)
		}
	}
}
