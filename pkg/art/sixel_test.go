package art

import (
	"image"
	"image/color"
	"strings"
	"testing"
)

func TestQuantizeSixel(t *testing.T) {
	tests := []struct {
		name      string
		build     func() *image.RGBA
		maxColors int
		wantMax   int
	}{
		{
			name: "flat image collapses to one colour",
			build: func() *image.RGBA {
				img := image.NewRGBA(image.Rect(0, 0, 8, 8))
				for y := 0; y < 8; y++ {
					for x := 0; x < 8; x++ {
						img.Set(x, y, color.RGBA{R: 0x20, G: 0x40, B: 0x60, A: 0xff})
					}
				}
				return img
			},
			maxColors: 256,
			wantMax:   1,
		},
		{
			name:      "gradient is capped at the requested palette size",
			build:     func() *image.RGBA { return testImage(64, 64) },
			maxColors: 16,
			wantMax:   16,
		},
		{
			name:      "full palette request stays within the register bank",
			build:     func() *image.RGBA { return testImage(128, 128) },
			maxColors: 1000, // clamped down to sixelMaxColors
			wantMax:   sixelMaxColors,
		},
		{
			name:      "single pixel",
			build:     func() *image.RGBA { return testImage(1, 1) },
			maxColors: 256,
			wantMax:   1,
		},
		{
			name:      "zero palette request is raised to one",
			build:     func() *image.RGBA { return testImage(16, 16) },
			maxColors: 0,
			wantMax:   1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			img := tc.build()
			w, h := img.Bounds().Dx(), img.Bounds().Dy()
			palette, indices := quantizeSixel(img, w, h, tc.maxColors)
			if len(palette) == 0 {
				t.Fatal("empty palette")
			}
			if len(palette) > tc.wantMax {
				t.Fatalf("palette has %d colours, want at most %d", len(palette), tc.wantMax)
			}
			if len(indices) != w*h {
				t.Fatalf("index buffer has %d entries, want %d", len(indices), w*h)
			}
			for i, idx := range indices {
				if int(idx) >= len(palette) {
					t.Fatalf("index %d points outside the palette (%d >= %d)", i, idx, len(palette))
				}
			}
		})
	}
}

// TestQuantizeSixelDeterministic guards against Go map iteration order leaking
// into the encoded output.
func TestQuantizeSixelDeterministic(t *testing.T) {
	img := testImage(48, 48)
	first := renderSixel(img, 48, 48)
	for i := 0; i < 5; i++ {
		if got := renderSixel(img, 48, 48); got != first {
			t.Fatalf("run %d produced different output", i)
		}
	}
}

func TestRenderSixelStructure(t *testing.T) {
	tests := []struct{ w, h int }{
		{6, 6}, {1, 1}, {12, 7}, {20, 13}, {0, 4}, {4, 0},
	}
	for _, tc := range tests {
		t.Run(sizeName(tc.w, tc.h), func(t *testing.T) {
			img := testImage(maxInt(tc.w, 1), maxInt(tc.h, 1))
			out := renderSixel(img, tc.w, tc.h)
			if tc.w <= 0 || tc.h <= 0 {
				if out != "" {
					t.Fatalf("expected empty output for %dx%d", tc.w, tc.h)
				}
				return
			}
			if !strings.HasPrefix(out, "\x1bPq\"1;1;") {
				t.Fatalf("missing DCS header: %q", out[:min(40, len(out))])
			}
			if !strings.Contains(out, "#0;2;") {
				t.Fatal("no colour register definition emitted")
			}
			if !strings.HasSuffix(out, "\x1b\\") {
				t.Fatal("DCS sequence is not ST terminated")
			}
			// Band separators must never exceed the number of bands - 1.
			bands := (tc.h + 5) / 6
			if got := strings.Count(out, "-"); got > bands {
				t.Fatalf("%d band separators for %d bands", got, bands)
			}
		})
	}
}

func TestWriteSixelRun(t *testing.T) {
	tests := []struct {
		name string
		row  []byte
		want string
	}{
		{"empty row is elided", []byte{0, 0, 0, 0}, ""},
		{"short run written literally", []byte{1, 1, 1}, "@@@"},
		{"long run is RLE encoded", []byte{1, 1, 1, 1, 1}, "!5@"},
		{"trailing blanks trimmed", []byte{63, 0, 0, 0, 0}, "~"},
		{"mixed", []byte{1, 1, 1, 1, 2}, "!4@A"},
		{"nil", nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var sb strings.Builder
			writeSixelRun(&sb, tc.row)
			if got := sb.String(); got != tc.want {
				t.Fatalf("writeSixelRun = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestQuantKeyAndWidestAxis(t *testing.T) {
	if quantKey(0, 0, 0) != 0 {
		t.Fatal("black should map to bucket 0")
	}
	if quantKey(255, 255, 255) != 0x7fff {
		t.Fatalf("white bucket = %#x", quantKey(255, 255, 255))
	}
	// Values inside the same 5-bit bucket must collide.
	if quantKey(8, 8, 8) != quantKey(15, 15, 15) {
		t.Fatal("expected 5-bit bucket collision")
	}

	box := []*histEntry{
		{r: 0, g: 10, b: 100},
		{r: 5, g: 20, b: 0},
	}
	axis, span := widestAxis(box)
	if axis != 2 || span != 100 {
		t.Fatalf("widestAxis = (%d, %d), want (2, 100)", axis, span)
	}
}

func TestMedianCutSplits(t *testing.T) {
	entries := make([]*histEntry, 0, 32)
	for i := 0; i < 32; i++ {
		entries = append(entries, &histEntry{
			key:   uint16(i),
			r:     uint8(i * 8),
			count: 1,
			sr:    uint64(i * 8),
		})
	}
	tests := []int{1, 2, 4, 8, 32, 64}
	for _, want := range tests {
		boxes := medianCut(entries, want)
		if len(boxes) > want && want <= 32 {
			t.Fatalf("medianCut(%d) produced %d boxes", want, len(boxes))
		}
		total := 0
		for _, b := range boxes {
			total += len(b)
		}
		if total != len(entries) {
			t.Fatalf("medianCut(%d) lost entries: %d of %d", want, total, len(entries))
		}
	}
}

func TestOtsuThreshold(t *testing.T) {
	tests := []struct {
		name   string
		build  func() *image.RGBA
		w, h   int
		wantLo int
		wantHi int
	}{
		{
			name: "bimodal black and white",
			build: func() *image.RGBA {
				img := image.NewRGBA(image.Rect(0, 0, 4, 4))
				for y := 0; y < 4; y++ {
					for x := 0; x < 4; x++ {
						c := color.RGBA{A: 0xff}
						if x >= 2 {
							c = color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff}
						}
						img.Set(x, y, c)
					}
				}
				return img
			},
			w: 4, h: 4, wantLo: 0, wantHi: 254,
		},
		{
			name: "flat black",
			build: func() *image.RGBA {
				return image.NewRGBA(image.Rect(0, 0, 4, 4))
			},
			w: 4, h: 4, wantLo: 0, wantHi: 0,
		},
		{
			name:  "degenerate size",
			build: func() *image.RGBA { return image.NewRGBA(image.Rect(0, 0, 1, 1)) },
			w:     0, h: 0, wantLo: 128, wantHi: 128,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := otsuThreshold(tc.build(), tc.w, tc.h)
			if got < tc.wantLo || got > tc.wantHi {
				t.Fatalf("otsuThreshold = %d, want within [%d,%d]", got, tc.wantLo, tc.wantHi)
			}
		})
	}
}
