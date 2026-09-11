package art

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"

	// Registered for their image.Decode side effects only.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// Nominal pixel size of a single terminal cell for the graphical transports.
// The height is derived from Renderer.CellAspect so unusual fonts stay square.
const (
	graphicalCellWidth = 10
	defaultCellAspect  = 2.0
)

// decodeImage decodes PNG, JPEG or GIF bytes into an image.Image.
func decodeImage(data []byte) (image.Image, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("art: empty image data")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("art: decode image: %w", err)
	}
	if img.Bounds().Dx() <= 0 || img.Bounds().Dy() <= 0 {
		return nil, fmt.Errorf("art: image has no pixels")
	}
	return img, nil
}

// subCellGrid reports how many pixels one terminal cell contributes to the
// sampling grid for a protocol, together with the cell aspect the fit should
// assume. Character-cell protocols have a fixed sub-cell layout (half-blocks
// are 1x2, braille cells are 2x4) so the aspect correction is applied when
// fitting rather than by stretching the grid.
func subCellGrid(p Protocol, aspect float64) (cellW, cellH int) {
	switch p {
	case ProtocolHalfBlock:
		return 1, 2
	case ProtocolBraille:
		return 2, 4
	default:
		h := int(float64(graphicalCellWidth)*aspect + 0.5)
		if h < 1 {
			h = 1
		}
		return graphicalCellWidth, h
	}
}

// fitDimensions computes the pixel size the artwork should occupy inside a
// gridW x gridH sampling grid while keeping the source aspect ratio visually
// intact. cellW/cellH describe the sub-cell pixel layout and aspect is the
// pixel height/width ratio of one terminal cell.
func fitDimensions(srcW, srcH, gridW, gridH, cellW, cellH int, aspect float64) (int, int) {
	if srcW <= 0 || srcH <= 0 || gridW <= 0 || gridH <= 0 || cellW <= 0 || cellH <= 0 {
		return maxInt(gridW, 1), maxInt(gridH, 1)
	}
	if aspect <= 0 {
		aspect = defaultCellAspect
	}

	// Convert the grid into "cell units" so the two axes are comparable.
	maxW := float64(gridW) / float64(cellW)
	maxH := float64(gridH) * aspect / float64(cellH)
	srcRatio := float64(srcW) / float64(srcH)

	w := maxW
	h := w / srcRatio
	if h > maxH {
		h = maxH
		w = h * srcRatio
	}

	dw := int(w*float64(cellW) + 0.5)
	dh := int(h*float64(cellH)/aspect + 0.5)
	return clampInt(dw, 1, gridW), clampInt(dh, 1, gridH)
}

// resampleBox downscales (or upscales) src into a dw x dh image using an
// area-average box filter over alpha-premultiplied samples.
func resampleBox(src image.Image, dw, dh int) *image.RGBA {
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))

	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 {
		return dst
	}

	for y := 0; y < dh; y++ {
		y0 := b.Min.Y + y*sh/dh
		y1 := b.Min.Y + (y+1)*sh/dh
		if y1 <= y0 {
			y1 = y0 + 1
		}
		for x := 0; x < dw; x++ {
			x0 := b.Min.X + x*sw/dw
			x1 := b.Min.X + (x+1)*sw/dw
			if x1 <= x0 {
				x1 = x0 + 1
			}

			var sr, sg, sb, sa, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					r, g, bb, a := src.At(xx, yy).RGBA()
					sr += uint64(r)
					sg += uint64(g)
					sb += uint64(bb)
					sa += uint64(a)
					n++
				}
			}
			if n == 0 {
				continue
			}
			off := dst.PixOffset(x, y)
			dst.Pix[off+0] = uint8(sr / n / 257)
			dst.Pix[off+1] = uint8(sg / n / 257)
			dst.Pix[off+2] = uint8(sb / n / 257)
			dst.Pix[off+3] = uint8(sa / n / 257)
		}
	}
	return dst
}

// prepare decodes data and lays it out on an opaque sampling grid sized for
// cols x rows terminal cells under the given protocol. The artwork is centred
// and letterboxed against black so it stays square instead of stretching.
func prepare(data []byte, p Protocol, cols, rows int, aspect float64) (*image.RGBA, error) {
	src, err := decodeImage(data)
	if err != nil {
		return nil, err
	}
	return prepareImage(src, p, cols, rows, aspect), nil
}

// prepareImage performs the resize/letterbox step for an already decoded image.
func prepareImage(src image.Image, p Protocol, cols, rows int, aspect float64) *image.RGBA {
	if aspect <= 0 {
		aspect = defaultCellAspect
	}
	cellW, cellH := subCellGrid(p, aspect)

	gridW := maxInt(cols*cellW, 1)
	gridH := maxInt(rows*cellH, 1)

	canvas := image.NewRGBA(image.Rect(0, 0, gridW, gridH))
	draw.Draw(canvas, canvas.Bounds(), image.NewUniform(color.RGBA{A: 0xff}), image.Point{}, draw.Src)

	b := src.Bounds()
	dw, dh := fitDimensions(b.Dx(), b.Dy(), gridW, gridH, cellW, cellH, aspect)
	scaled := resampleBox(src, dw, dh)

	offX := (gridW - dw) / 2
	offY := (gridH - dh) / 2
	draw.Draw(canvas, image.Rect(offX, offY, offX+dw, offY+dh), scaled, image.Point{}, draw.Over)
	return canvas
}

// pixelAt returns the opaque RGB triple at (x, y), clamped to the image.
func pixelAt(img *image.RGBA, x, y int) (uint8, uint8, uint8) {
	b := img.Bounds()
	x = clampInt(x, b.Min.X, b.Max.X-1)
	y = clampInt(y, b.Min.Y, b.Max.Y-1)
	off := img.PixOffset(x, y)
	return img.Pix[off], img.Pix[off+1], img.Pix[off+2]
}

// luminance returns the perceptual brightness of an RGB triple in 0..255.
func luminance(r, g, b uint8) int {
	return int((299*uint32(r) + 587*uint32(g) + 114*uint32(b)) / 1000)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
