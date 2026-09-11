package art

import (
	"image"
	"strconv"
	"strings"
)

// upperHalfBlock paints the top half of a cell, letting the cell background
// show through as the bottom half. One cell therefore carries two pixels.
const upperHalfBlock = "▀"

// sgrReset returns the terminal to its default colours.
const sgrReset = "\x1b[0m"

// renderHalfBlock converts a prepared cols x (rows*2) pixel grid into rows
// lines of truecolor half-block cells. Each line is exactly cols cells wide
// and ends with an SGR reset so no colour state escapes the line.
func renderHalfBlock(img *image.RGBA, cols, rows int) []string {
	lines := make([]string, 0, rows)
	var sb strings.Builder

	for row := 0; row < rows; row++ {
		sb.Reset()
		sb.Grow(cols * 24)

		// -1 forces the first cell of every line to emit both colours, so a
		// line never depends on the SGR state left by the previous one.
		lastFg, lastBg := -1, -1
		for col := 0; col < cols; col++ {
			tr, tg, tb := pixelAt(img, col, row*2)
			br, bg, bb := pixelAt(img, col, row*2+1)

			fg := rgbKey(tr, tg, tb)
			bgk := rgbKey(br, bg, bb)
			if fg != lastFg {
				writeSGRColor(&sb, 38, tr, tg, tb)
				lastFg = fg
			}
			if bgk != lastBg {
				writeSGRColor(&sb, 48, br, bg, bb)
				lastBg = bgk
			}
			sb.WriteString(upperHalfBlock)
		}
		sb.WriteString(sgrReset)
		lines = append(lines, sb.String())
	}
	return lines
}

// writeSGRColor appends a truecolor SGR sequence for layer 38 (foreground) or
// 48 (background).
func writeSGRColor(sb *strings.Builder, layer int, r, g, b uint8) {
	sb.WriteString("\x1b[")
	sb.WriteString(strconv.Itoa(layer))
	sb.WriteString(";2;")
	sb.WriteString(strconv.Itoa(int(r)))
	sb.WriteByte(';')
	sb.WriteString(strconv.Itoa(int(g)))
	sb.WriteByte(';')
	sb.WriteString(strconv.Itoa(int(b)))
	sb.WriteByte('m')
}

// rgbKey packs an RGB triple into a comparable integer.
func rgbKey(r, g, b uint8) int {
	return int(r)<<16 | int(g)<<8 | int(b)
}
