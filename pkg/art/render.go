package art

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"strings"
)

// Renderer converts encoded images into terminal-ready output.
type Renderer struct {
	Protocol Protocol
	// CellAspect is the pixel height/width ratio of one terminal cell,
	// used to keep artwork square. Defaults to 2.0 when zero.
	CellAspect float64
}

// NewRenderer returns a Renderer for the given protocol.
func NewRenderer(p Protocol) *Renderer {
	return &Renderer{Protocol: p, CellAspect: defaultCellAspect}
}

// Render decodes img and returns exactly rows lines of terminal output, each
// padded so that lipgloss.Width reports cols. Escape-sequence protocols place
// the image at the cursor and pad the remaining rows with spaces so the
// surrounding Bubble Tea layout stays intact.
//
// It returns an error for a non-positive size, for ProtocolNone, for an
// unknown protocol and for image bytes it cannot decode. It never panics.
func (r *Renderer) Render(img []byte, cols, rows int) ([]string, error) {
	if cols <= 0 || rows <= 0 {
		return nil, fmt.Errorf("art: invalid render size %dx%d cells", cols, rows)
	}

	aspect := defaultCellAspect
	if r != nil && r.CellAspect > 0 {
		aspect = r.CellAspect
	}
	proto := ProtocolNone
	if r != nil {
		proto = r.Protocol
	}

	switch proto {
	case ProtocolNone:
		return nil, fmt.Errorf("art: artwork rendering is disabled")
	case ProtocolHalfBlock, ProtocolBraille, ProtocolKitty, ProtocolITerm2, ProtocolSixel:
	default:
		return nil, fmt.Errorf("art: unsupported protocol %q", string(proto))
	}

	canvas, err := prepare(img, proto, cols, rows, aspect)
	if err != nil {
		return nil, err
	}

	switch proto {
	case ProtocolHalfBlock:
		return renderHalfBlock(canvas, cols, rows), nil
	case ProtocolBraille:
		return renderBraille(canvas, cols, rows), nil
	case ProtocolKitty:
		data, err := encodePNG(canvas)
		if err != nil {
			return nil, err
		}
		return escapeLines(renderKitty(data, cols, rows), cols, rows), nil
	case ProtocolITerm2:
		data, err := encodePNG(canvas)
		if err != nil {
			return nil, err
		}
		return escapeLines(renderITerm2(data, cols, rows), cols, rows), nil
	case ProtocolSixel:
		b := canvas.Bounds()
		return escapeLines(renderSixel(canvas, b.Dx(), b.Dy()), cols, rows), nil
	}

	// Unreachable: the switch above validates every accepted protocol.
	return nil, fmt.Errorf("art: unsupported protocol %q", string(proto))
}

// Clear returns the escape sequence that removes any previously placed images
// for this protocol, or "" when the protocol leaves no residue.
func (r *Renderer) Clear() string {
	if r == nil {
		return ""
	}
	if r.Protocol == ProtocolKitty {
		return kittyClear
	}
	return ""
}

// escapeLines wraps a single escape sequence into the rows x cols block shape
// every protocol must honour: the sequence is emitted at the cursor on the
// first line and the whole block is padded with spaces so the caller's layout
// arithmetic holds.
func escapeLines(escape string, cols, rows int) []string {
	pad := strings.Repeat(" ", cols)
	lines := make([]string, rows)
	lines[0] = escape + pad
	for i := 1; i < rows; i++ {
		lines[i] = pad
	}
	return lines
}

// encodePNG re-encodes a prepared canvas as PNG for the escape-based
// transports.
func encodePNG(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("art: encode png: %w", err)
	}
	return buf.Bytes(), nil
}
