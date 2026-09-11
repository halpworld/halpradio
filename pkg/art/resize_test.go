package art

import (
	"image"
	"image/color"
	"testing"
)

func TestDecodeImageErrors(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"text", []byte("not an image at all")},
		{"png magic only", []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := decodeImage(tc.data); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestDecodeImageFormats(t *testing.T) {
	pngData := testPNG(t, 16, 9)
	jpegData := testJPEG(t, 16, 9)
	for name, data := range map[string][]byte{"png": pngData, "jpeg": jpegData} {
		t.Run(name, func(t *testing.T) {
			img, err := decodeImage(data)
			if err != nil {
				t.Fatalf("decodeImage: %v", err)
			}
			if got := img.Bounds().Dx(); got != 16 {
				t.Fatalf("width = %d, want 16", got)
			}
		})
	}
}

func TestSubCellGrid(t *testing.T) {
	tests := []struct {
		proto        Protocol
		aspect       float64
		wantW, wantH int
	}{
		{ProtocolHalfBlock, 2.0, 1, 2},
		{ProtocolHalfBlock, 1.7, 1, 2},
		{ProtocolBraille, 2.0, 2, 4},
		{ProtocolBraille, 3.0, 2, 4},
		{ProtocolKitty, 2.0, 10, 20},
		{ProtocolITerm2, 2.4, 10, 24},
		{ProtocolSixel, 0.05, 10, 1},
	}
	for _, tc := range tests {
		t.Run(string(tc.proto), func(t *testing.T) {
			w, h := subCellGrid(tc.proto, tc.aspect)
			if w != tc.wantW || h != tc.wantH {
				t.Fatalf("subCellGrid = %dx%d, want %dx%d", w, h, tc.wantW, tc.wantH)
			}
		})
	}
}

func TestFitDimensions(t *testing.T) {
	tests := []struct {
		name         string
		srcW, srcH   int
		gridW, gridH int
		cellW, cellH int
		aspect       float64
		wantW, wantH int
	}{
		// A square source stays square: 20 cells wide is 20 units, so it
		// occupies 20 of the 40 vertical units available.
		{"square into square", 100, 100, 20, 40, 1, 2, 2.0, 20, 20},
		// Wide source: full width, letterboxed vertically.
		{"wide into square", 200, 100, 20, 40, 1, 2, 2.0, 20, 10},
		// Tall source: full height, letterboxed horizontally.
		{"tall into square", 100, 200, 20, 40, 1, 2, 2.0, 20, 40},
		// Graphical cells.
		{"square graphical", 600, 600, 200, 400, 10, 20, 2.0, 200, 200},
		{"wide graphical", 1200, 600, 200, 400, 10, 20, 2.0, 200, 100},
		// Degenerate inputs never divide by zero and never return 0.
		{"zero source", 0, 0, 10, 10, 1, 2, 2.0, 10, 10},
		{"zero grid", 10, 10, 0, 0, 1, 2, 2.0, 1, 1},
		{"single cell", 64, 64, 1, 2, 1, 2, 2.0, 1, 1},
		{"single column", 64, 64, 1, 16, 1, 2, 2.0, 1, 1},
		{"single row", 64, 64, 16, 2, 1, 2, 2.0, 2, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, h := fitDimensions(tc.srcW, tc.srcH, tc.gridW, tc.gridH, tc.cellW, tc.cellH, tc.aspect)
			if w != tc.wantW || h != tc.wantH {
				t.Fatalf("fitDimensions = %dx%d, want %dx%d", w, h, tc.wantW, tc.wantH)
			}
			if tc.gridW > 0 && tc.gridH > 0 {
				if w < 1 || h < 1 || w > tc.gridW || h > tc.gridH {
					t.Fatalf("result %dx%d escapes grid %dx%d", w, h, tc.gridW, tc.gridH)
				}
			}
		})
	}
}

func TestResampleBoxAverages(t *testing.T) {
	// A 2x2 image with known colours must average to a single pixel.
	src := image.NewRGBA(image.Rect(0, 0, 2, 2))
	src.Set(0, 0, color.RGBA{R: 0xff, A: 0xff})
	src.Set(1, 0, color.RGBA{G: 0xff, A: 0xff})
	src.Set(0, 1, color.RGBA{B: 0xff, A: 0xff})
	src.Set(1, 1, color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})

	out := resampleBox(src, 1, 1)
	r, g, b := pixelAt(out, 0, 0)
	// Each channel is 0xff in two of the four pixels.
	for name, got := range map[string]uint8{"r": r, "g": g, "b": b} {
		if got < 125 || got > 130 {
			t.Fatalf("channel %s = %d, want ~127", name, got)
		}
	}
}

func TestResampleBoxSizes(t *testing.T) {
	src := testImage(9, 7)
	tests := []struct{ w, h int }{
		{1, 1}, {3, 3}, {9, 7}, {20, 20}, {0, 5}, {5, 0}, {-4, -4},
	}
	for _, tc := range tests {
		t.Run(sizeName(tc.w, tc.h), func(t *testing.T) {
			out := resampleBox(src, tc.w, tc.h)
			wantW, wantH := maxInt(tc.w, 1), maxInt(tc.h, 1)
			if out.Bounds().Dx() != wantW || out.Bounds().Dy() != wantH {
				t.Fatalf("bounds = %v, want %dx%d", out.Bounds(), wantW, wantH)
			}
		})
	}
}

func TestPrepareImageGridSize(t *testing.T) {
	tests := []struct {
		proto        Protocol
		cols, rows   int
		wantW, wantH int
	}{
		{ProtocolHalfBlock, 10, 5, 10, 10},
		{ProtocolBraille, 10, 5, 20, 20},
		{ProtocolKitty, 10, 5, 100, 100},
		{ProtocolHalfBlock, 1, 1, 1, 2},
		{ProtocolBraille, 1, 1, 2, 4},
	}
	for _, tc := range tests {
		t.Run(string(tc.proto)+"_"+sizeName(tc.cols, tc.rows), func(t *testing.T) {
			canvas := prepareImage(testImage(40, 40), tc.proto, tc.cols, tc.rows, 0)
			if canvas.Bounds().Dx() != tc.wantW || canvas.Bounds().Dy() != tc.wantH {
				t.Fatalf("canvas = %v, want %dx%d", canvas.Bounds(), tc.wantW, tc.wantH)
			}
			// Letterbox padding must be fully opaque so terminal output has
			// concrete colours everywhere.
			for y := 0; y < tc.wantH; y++ {
				for x := 0; x < tc.wantW; x++ {
					if a := canvas.Pix[canvas.PixOffset(x, y)+3]; a != 0xff {
						t.Fatalf("pixel (%d,%d) alpha = %d, want 255", x, y, a)
					}
				}
			}
		})
	}
}

func TestPixelAtClamps(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 0xff})
	img.Set(1, 1, color.RGBA{R: 40, G: 50, B: 60, A: 0xff})

	tests := []struct {
		x, y    int
		r, g, b uint8
	}{
		{0, 0, 10, 20, 30},
		{-5, -5, 10, 20, 30},
		{99, 99, 40, 50, 60},
		{1, 1, 40, 50, 60},
	}
	for _, tc := range tests {
		r, g, b := pixelAt(img, tc.x, tc.y)
		if r != tc.r || g != tc.g || b != tc.b {
			t.Fatalf("pixelAt(%d,%d) = %d,%d,%d want %d,%d,%d", tc.x, tc.y, r, g, b, tc.r, tc.g, tc.b)
		}
	}
}

func TestLuminanceAndClamp(t *testing.T) {
	if got := luminance(0, 0, 0); got != 0 {
		t.Fatalf("luminance(black) = %d", got)
	}
	if got := luminance(255, 255, 255); got < 254 {
		t.Fatalf("luminance(white) = %d", got)
	}
	tests := []struct{ v, lo, hi, want int }{
		{5, 0, 10, 5},
		{-1, 0, 10, 0},
		{11, 0, 10, 10},
		{5, 10, 0, 10}, // inverted bounds collapse to lo
	}
	for _, tc := range tests {
		if got := clampInt(tc.v, tc.lo, tc.hi); got != tc.want {
			t.Fatalf("clampInt(%d,%d,%d) = %d, want %d", tc.v, tc.lo, tc.hi, got, tc.want)
		}
	}
}
