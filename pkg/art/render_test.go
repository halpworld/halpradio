package art

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// testPNG builds a deterministic w x h gradient PNG in memory.
func testPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, testImage(w, h)); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return buf.Bytes()
}

// testJPEG builds a deterministic w x h gradient JPEG in memory.
func testJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, testImage(w, h), nil); err != nil {
		t.Fatalf("encode test jpeg: %v", err)
	}
	return buf.Bytes()
}

func testImage(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	dw, dh := maxInt(w-1, 1), maxInt(h-1, 1)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{
				R: uint8(x * 255 / dw),
				G: uint8(y * 255 / dh),
				B: uint8((x + y) * 127 / (dw + dh)),
				A: 0xff,
			})
		}
	}
	return img
}

// TestRenderBlockGeometry is the load-bearing invariant of this package: every
// protocol must return exactly rows lines, each measuring cols columns as far
// as lipgloss is concerned, so the caller's Bubble Tea layout arithmetic holds.
func TestRenderBlockGeometry(t *testing.T) {
	protocols := []Protocol{
		ProtocolHalfBlock,
		ProtocolBraille,
		ProtocolKitty,
		ProtocolITerm2,
		ProtocolSixel,
	}
	sizes := []struct{ cols, rows int }{
		{1, 1},
		{1, 8},
		{8, 1},
		{20, 10},
		{24, 12},
		{40, 7},
	}
	images := map[string][]byte{
		"png_square":    testPNG(t, 64, 64),
		"png_wide":      testPNG(t, 120, 40),
		"png_tall":      testPNG(t, 40, 120),
		"png_tiny":      testPNG(t, 1, 1),
		"jpeg_square":   testJPEG(t, 64, 64),
		"png_oversized": testPNG(t, 600, 600),
	}

	for _, proto := range protocols {
		for name, data := range images {
			for _, size := range sizes {
				t.Run(string(proto)+"/"+name+"/"+sizeName(size.cols, size.rows), func(t *testing.T) {
					r := NewRenderer(proto)
					lines, err := r.Render(data, size.cols, size.rows)
					if err != nil {
						t.Fatalf("Render: %v", err)
					}
					if len(lines) != size.rows {
						t.Fatalf("got %d lines, want %d", len(lines), size.rows)
					}
					for i, line := range lines {
						if w := lipgloss.Width(line); w != size.cols {
							t.Fatalf("line %d: lipgloss.Width = %d, want %d", i, w, size.cols)
						}
						if strings.ContainsAny(line, "\n\r") {
							t.Fatalf("line %d contains an embedded newline", i)
						}
					}
				})
			}
		}
	}
}

func sizeName(cols, rows int) string {
	return fmt.Sprintf("%dx%d", cols, rows)
}

// TestRenderCellProtocolsResetSGR guards against colour state leaking past a
// line boundary in the character-cell renderers.
func TestRenderCellProtocolsResetSGR(t *testing.T) {
	for _, proto := range []Protocol{ProtocolHalfBlock, ProtocolBraille} {
		t.Run(string(proto), func(t *testing.T) {
			lines, err := NewRenderer(proto).Render(testPNG(t, 64, 64), 12, 6)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			for i, line := range lines {
				if !strings.HasSuffix(line, sgrReset) {
					t.Fatalf("line %d does not end with an SGR reset: %q", i, line)
				}
				if !strings.HasPrefix(line, "\x1b[38;2;") {
					t.Fatalf("line %d does not open with its own colour: %q", i, line)
				}
			}
		})
	}
}

// TestRenderEscapeProtocolsShape checks the escape-based contract: the payload
// lands on the first line and the rest of the block is plain padding.
func TestRenderEscapeProtocolsShape(t *testing.T) {
	tests := []struct {
		proto  Protocol
		prefix string
	}{
		{ProtocolKitty, "\x1b_G"},
		{ProtocolITerm2, "\x1b]1337;File=inline=1;"},
		{ProtocolSixel, "\x1bPq\"1;1;"},
	}

	const cols, rows = 16, 8
	for _, tc := range tests {
		t.Run(string(tc.proto), func(t *testing.T) {
			lines, err := NewRenderer(tc.proto).Render(testPNG(t, 64, 64), cols, rows)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if !strings.HasPrefix(lines[0], tc.prefix) {
				t.Fatalf("first line does not start with %q", tc.prefix)
			}
			if !strings.HasSuffix(lines[0], strings.Repeat(" ", cols)) {
				t.Fatal("first line is not padded to cols")
			}
			for i := 1; i < rows; i++ {
				if lines[i] != strings.Repeat(" ", cols) {
					t.Fatalf("line %d is not pure padding: %q", i, lines[i])
				}
			}
		})
	}
}

func TestRenderErrors(t *testing.T) {
	good := testPNG(t, 32, 32)

	tests := []struct {
		name  string
		proto Protocol
		data  []byte
		cols  int
		rows  int
	}{
		{"zero cols", ProtocolHalfBlock, good, 0, 4},
		{"zero rows", ProtocolHalfBlock, good, 4, 0},
		{"negative cols", ProtocolHalfBlock, good, -3, 4},
		{"negative rows", ProtocolHalfBlock, good, 4, -3},
		{"protocol none", ProtocolNone, good, 8, 4},
		{"unknown protocol", Protocol("ascii"), good, 8, 4},
		{"empty protocol", Protocol(""), good, 8, 4},
		{"nil data", ProtocolHalfBlock, nil, 8, 4},
		{"garbage data", ProtocolHalfBlock, []byte("this is not an image"), 8, 4},
		{"truncated png", ProtocolKitty, good[:12], 8, 4},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lines, err := (&Renderer{Protocol: tc.proto}).Render(tc.data, tc.cols, tc.rows)
			if err == nil {
				t.Fatalf("expected an error, got %d lines", len(lines))
			}
			if lines != nil {
				t.Fatalf("expected nil lines alongside the error, got %d", len(lines))
			}
		})
	}
}

func TestRendererClear(t *testing.T) {
	tests := []struct {
		proto Protocol
		want  string
	}{
		{ProtocolKitty, "\x1b_Ga=d,d=A\x1b\\"},
		{ProtocolITerm2, ""},
		{ProtocolSixel, ""},
		{ProtocolHalfBlock, ""},
		{ProtocolBraille, ""},
		{ProtocolNone, ""},
	}
	for _, tc := range tests {
		t.Run(string(tc.proto), func(t *testing.T) {
			if got := NewRenderer(tc.proto).Clear(); got != tc.want {
				t.Fatalf("Clear() = %q, want %q", got, tc.want)
			}
		})
	}
	if got := (*Renderer)(nil).Clear(); got != "" {
		t.Fatalf("nil Renderer Clear() = %q, want empty", got)
	}
	// The Kitty clear sequence must not consume the layout's width.
	if w := lipgloss.Width(NewRenderer(ProtocolKitty).Clear() + "abc"); w != 3 {
		t.Fatalf("Kitty clear is not zero-width: %d", w)
	}
}

func TestNewRendererDefaults(t *testing.T) {
	r := NewRenderer(ProtocolHalfBlock)
	if r.Protocol != ProtocolHalfBlock {
		t.Fatalf("Protocol = %q", r.Protocol)
	}
	if r.CellAspect != defaultCellAspect {
		t.Fatalf("CellAspect = %v, want %v", r.CellAspect, defaultCellAspect)
	}
	// A zero CellAspect must still render correctly.
	lines, err := (&Renderer{Protocol: ProtocolHalfBlock}).Render(testPNG(t, 32, 32), 10, 5)
	if err != nil {
		t.Fatalf("Render with zero CellAspect: %v", err)
	}
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5", len(lines))
	}
	for i, line := range lines {
		if w := lipgloss.Width(line); w != 10 {
			t.Fatalf("line %d width = %d, want 10", i, w)
		}
	}
}

// TestRenderKittyChunking verifies the APC chunking rules for a payload large
// enough to need several escapes.
func TestRenderKittyChunking(t *testing.T) {
	lines, err := NewRenderer(ProtocolKitty).Render(testPNG(t, 400, 400), 40, 20)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	out := lines[0]
	chunks := strings.Split(strings.TrimSuffix(out, strings.Repeat(" ", 40)), "\x1b\\")
	chunks = chunks[:len(chunks)-1] // trailing empty element after the final ST
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for i, chunk := range chunks {
		if !strings.HasPrefix(chunk, "\x1b_G") {
			t.Fatalf("chunk %d missing APC introducer", i)
		}
		keys, payload, ok := strings.Cut(strings.TrimPrefix(chunk, "\x1b_G"), ";")
		if !ok {
			t.Fatalf("chunk %d has no key/payload separator", i)
		}
		if len(payload) > kittyChunkSize {
			t.Fatalf("chunk %d payload is %d bytes, over the %d limit", i, len(payload), kittyChunkSize)
		}
		wantMore := "m=1"
		if i == len(chunks)-1 {
			wantMore = "m=0"
		}
		if !strings.Contains(keys, wantMore) {
			t.Fatalf("chunk %d keys %q missing %q", i, keys, wantMore)
		}
		if i == 0 {
			for _, want := range []string{"a=T", "f=100", "c=40", "r=20", "i=", "q=2"} {
				if !strings.Contains(keys, want) {
					t.Fatalf("first chunk keys %q missing %q", keys, want)
				}
			}
		} else if strings.Contains(keys, "a=T") {
			t.Fatalf("continuation chunk %d repeats the action key: %q", i, keys)
		}
	}
}

func TestRenderITerm2Payload(t *testing.T) {
	lines, err := NewRenderer(ProtocolITerm2).Render(testPNG(t, 64, 64), 12, 6)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	seq := strings.TrimSuffix(lines[0], strings.Repeat(" ", 12))
	if !strings.HasPrefix(seq, "\x1b]1337;File=inline=1;width=12;height=6;preserveAspectRatio=1;size=") {
		t.Fatalf("unexpected OSC header: %q", seq[:min(80, len(seq))])
	}
	if !strings.HasSuffix(seq, "\x07") {
		t.Fatal("OSC sequence is not BEL terminated")
	}
}
