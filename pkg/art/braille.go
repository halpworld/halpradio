package art

import (
	"image"
	"strings"
)

// brailleBase is U+2800, the blank braille pattern. Dots are added as a
// bitmask on top of it.
const brailleBase = rune(0x2800)

// brailleDotBits maps a (dx, dy) position inside a 2x4 braille cell onto its
// Unicode dot bit.
var brailleDotBits = [2][4]byte{
	{0x01, 0x02, 0x04, 0x40}, // left column: dots 1, 2, 3, 7
	{0x08, 0x10, 0x20, 0x80}, // right column: dots 4, 5, 6, 8
}

// renderBraille converts a prepared (cols*2) x (rows*4) pixel grid into rows
// lines of braille cells. A dot is set where the pixel is strictly brighter
// than a global Otsu threshold and each cell is tinted with the average colour
// of the pixels it lit up.
func renderBraille(img *image.RGBA, cols, rows int) []string {
	threshold := otsuThreshold(img, cols*2, rows*4)

	lines := make([]string, 0, rows)
	var sb strings.Builder

	for row := 0; row < rows; row++ {
		sb.Reset()
		sb.Grow(cols * 24)
		lastFg := -1

		for col := 0; col < cols; col++ {
			var mask byte
			var onR, onG, onB, onN int
			var allR, allG, allB int

			for dx := 0; dx < 2; dx++ {
				for dy := 0; dy < 4; dy++ {
					r, g, b := pixelAt(img, col*2+dx, row*4+dy)
					allR += int(r)
					allG += int(g)
					allB += int(b)
					if luminance(r, g, b) > threshold {
						mask |= brailleDotBits[dx][dy]
						onR += int(r)
						onG += int(g)
						onB += int(b)
						onN++
					}
				}
			}

			var cr, cg, cb uint8
			if onN > 0 {
				cr, cg, cb = uint8(onR/onN), uint8(onG/onN), uint8(onB/onN)
			} else {
				cr, cg, cb = uint8(allR/8), uint8(allG/8), uint8(allB/8)
			}

			if key := rgbKey(cr, cg, cb); key != lastFg {
				writeSGRColor(&sb, 38, cr, cg, cb)
				lastFg = key
			}
			sb.WriteRune(brailleBase + rune(mask))
		}
		sb.WriteString(sgrReset)
		lines = append(lines, sb.String())
	}
	return lines
}

// otsuThreshold picks a luminance cut-off that maximises between-class
// variance over the sampled region. Callers treat pixels strictly brighter
// than the result as foreground, so a flat non-black image lights every dot
// and a flat black image lights none.
func otsuThreshold(img *image.RGBA, w, h int) int {
	if w <= 0 || h <= 0 {
		return 128
	}

	var hist [256]int
	total := 0
	lo, hi := 255, 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b := pixelAt(img, x, y)
			l := luminance(r, g, b)
			hist[l]++
			total++
			if l < lo {
				lo = l
			}
			if l > hi {
				hi = l
			}
		}
	}
	if total == 0 {
		return 128
	}
	if hi <= lo {
		// Completely flat: light every dot unless the image is pure black.
		if hi == 0 {
			return 0
		}
		return lo - 1
	}

	sum := 0.0
	for i := 0; i < 256; i++ {
		sum += float64(i) * float64(hist[i])
	}

	var (
		sumB, wB   float64
		best       = -1.0
		bestThresh = (lo + hi) / 2
	)
	for i := 0; i < 256; i++ {
		wB += float64(hist[i])
		if wB == 0 {
			continue
		}
		wF := float64(total) - wB
		if wF == 0 {
			break
		}
		sumB += float64(i) * float64(hist[i])
		mB := sumB / wB
		mF := (sum - sumB) / wF
		variance := wB * wF * (mB - mF) * (mB - mF)
		if variance > best {
			best = variance
			bestThresh = i
		}
	}
	return clampInt(bestThresh, lo, hi-1)
}
