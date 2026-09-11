package art

import (
	"image"
	"image/color"
	"sort"
	"strconv"
	"strings"
)

// sixelMaxColors is the size of the Sixel colour register bank we target.
const sixelMaxColors = 256

// sixelRLEThreshold is the run length at which "!<count><char>" becomes
// shorter than repeating the character.
const sixelRLEThreshold = 4

// histEntry is one bucket of the 5-bit-per-channel colour histogram used as
// the input to median-cut quantization.
type histEntry struct {
	key            uint16
	r, g, b        uint8  // bucket centre, 8-bit
	sr, sg, sb     uint64 // population-weighted channel sums
	count          uint64
	paletteIndex   uint8
	hasPaletteSlot bool
}

// renderSixel encodes a prepared pixel grid as a DCS Sixel image.
func renderSixel(img *image.RGBA, w, h int) string {
	if w <= 0 || h <= 0 {
		return ""
	}

	palette, indices := quantizeSixel(img, w, h, sixelMaxColors)
	if len(palette) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.Grow(w*h/4 + 1024)

	sb.WriteString("\x1bPq\"1;1;")
	sb.WriteString(strconv.Itoa(w))
	sb.WriteByte(';')
	sb.WriteString(strconv.Itoa(h))

	// Colour registers, as 0-100 percentages per the Sixel spec.
	for i, c := range palette {
		sb.WriteByte('#')
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(";2;")
		sb.WriteString(strconv.Itoa(int(c.R) * 100 / 255))
		sb.WriteByte(';')
		sb.WriteString(strconv.Itoa(int(c.G) * 100 / 255))
		sb.WriteByte(';')
		sb.WriteString(strconv.Itoa(int(c.B) * 100 / 255))
	}

	bands := (h + 5) / 6
	row := make([]byte, w)
	for band := 0; band < bands; band++ {
		if band > 0 {
			sb.WriteByte('-') // advance to the next six-pixel band
		}

		var present [sixelMaxColors]bool
		usedCount := 0
		top := band * 6
		for dy := 0; dy < 6 && top+dy < h; dy++ {
			base := (top + dy) * w
			for x := 0; x < w; x++ {
				ci := indices[base+x]
				if !present[ci] {
					present[ci] = true
					usedCount++
				}
			}
		}

		emitted := 0
		for ci := 0; ci < len(palette); ci++ {
			if !present[ci] {
				continue
			}
			if emitted > 0 {
				sb.WriteByte('$') // carriage return within the band
			}
			emitted++

			for x := range row {
				row[x] = 0
			}
			for dy := 0; dy < 6 && top+dy < h; dy++ {
				base := (top + dy) * w
				bit := byte(1) << uint(dy)
				for x := 0; x < w; x++ {
					if int(indices[base+x]) == ci {
						row[x] |= bit
					}
				}
			}

			sb.WriteByte('#')
			sb.WriteString(strconv.Itoa(ci))
			writeSixelRun(&sb, row)

			if emitted == usedCount {
				break
			}
		}
	}

	sb.WriteString("\x1b\\")
	return sb.String()
}

// writeSixelRun run-length encodes one colour pass across a band.
func writeSixelRun(sb *strings.Builder, row []byte) {
	// Trailing empty sixels are a no-op, so drop them.
	end := len(row)
	for end > 0 && row[end-1] == 0 {
		end--
	}
	for i := 0; i < end; {
		v := row[i]
		n := 1
		for i+n < end && row[i+n] == v {
			n++
		}
		ch := byte(0x3F) + v
		if n >= sixelRLEThreshold {
			sb.WriteByte('!')
			sb.WriteString(strconv.Itoa(n))
			sb.WriteByte(ch)
		} else {
			for k := 0; k < n; k++ {
				sb.WriteByte(ch)
			}
		}
		i += n
	}
}

// quantizeSixel reduces the grid to at most maxColors colours using median-cut
// over a 5-bit-per-channel histogram. It returns the palette plus a per-pixel
// palette index buffer of length w*h.
func quantizeSixel(img *image.RGBA, w, h, maxColors int) ([]color.RGBA, []uint8) {
	if maxColors < 1 {
		maxColors = 1
	}
	if maxColors > sixelMaxColors {
		maxColors = sixelMaxColors
	}

	byKey := make(map[uint16]*histEntry, 1024)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b := pixelAt(img, x, y)
			key := quantKey(r, g, b)
			e := byKey[key]
			if e == nil {
				e = &histEntry{key: key}
				byKey[key] = e
			}
			e.sr += uint64(r)
			e.sg += uint64(g)
			e.sb += uint64(b)
			e.count++
		}
	}
	if len(byKey) == 0 {
		return nil, nil
	}

	entries := make([]*histEntry, 0, len(byKey))
	for _, e := range byKey {
		e.r, e.g, e.b = uint8(e.sr/e.count), uint8(e.sg/e.count), uint8(e.sb/e.count)
		entries = append(entries, e)
	}
	// Deterministic ordering: map iteration order must not leak into output.
	sort.Slice(entries, func(i, j int) bool { return entries[i].key < entries[j].key })

	palette := make([]color.RGBA, 0, maxColors)
	if len(entries) <= maxColors {
		for i, e := range entries {
			e.paletteIndex = uint8(i)
			e.hasPaletteSlot = true
			palette = append(palette, color.RGBA{R: e.r, G: e.g, B: e.b, A: 0xff})
		}
	} else {
		for _, box := range medianCut(entries, maxColors) {
			idx := uint8(len(palette))
			var sr, sg, sb, n uint64
			for _, e := range box {
				e.paletteIndex = idx
				e.hasPaletteSlot = true
				sr += e.sr
				sg += e.sg
				sb += e.sb
				n += e.count
			}
			if n == 0 {
				n = 1
			}
			palette = append(palette, color.RGBA{
				R: uint8(sr / n), G: uint8(sg / n), B: uint8(sb / n), A: 0xff,
			})
		}
	}

	indices := make([]uint8, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b := pixelAt(img, x, y)
			if e := byKey[quantKey(r, g, b)]; e != nil && e.hasPaletteSlot {
				indices[y*w+x] = e.paletteIndex
			}
		}
	}
	return palette, indices
}

// medianCut partitions entries into at most want boxes, repeatedly splitting
// the box with the greatest population x longest-axis product.
func medianCut(entries []*histEntry, want int) [][]*histEntry {
	boxes := [][]*histEntry{entries}
	for len(boxes) < want {
		target, axis, score := -1, 0, uint64(0)
		for i, box := range boxes {
			if len(box) < 2 {
				continue
			}
			ax, span := widestAxis(box)
			if span == 0 {
				continue
			}
			var pop uint64
			for _, e := range box {
				pop += e.count
			}
			if s := pop * uint64(span); s > score {
				target, axis, score = i, ax, s
			}
		}
		if target < 0 {
			break
		}

		box := boxes[target]
		sortByAxis(box, axis)

		var pop uint64
		for _, e := range box {
			pop += e.count
		}
		half := pop / 2
		var acc uint64
		split := 1
		for i, e := range box {
			acc += e.count
			if acc >= half && i+1 < len(box) {
				split = i + 1
				break
			}
		}
		if split < 1 {
			split = 1
		}
		if split >= len(box) {
			split = len(box) - 1
		}

		boxes[target] = box[:split]
		boxes = append(boxes, box[split:])
	}
	return boxes
}

// widestAxis reports which channel (0=R, 1=G, 2=B) spans the widest range in
// the box, along with that span.
func widestAxis(box []*histEntry) (int, int) {
	lo := [3]int{255, 255, 255}
	hi := [3]int{0, 0, 0}
	for _, e := range box {
		v := [3]int{int(e.r), int(e.g), int(e.b)}
		for c := 0; c < 3; c++ {
			if v[c] < lo[c] {
				lo[c] = v[c]
			}
			if v[c] > hi[c] {
				hi[c] = v[c]
			}
		}
	}
	axis, span := 0, hi[0]-lo[0]
	if hi[1]-lo[1] > span {
		axis, span = 1, hi[1]-lo[1]
	}
	if hi[2]-lo[2] > span {
		axis, span = 2, hi[2]-lo[2]
	}
	return axis, span
}

// sortByAxis orders a box along one colour channel, tie-broken by key so the
// result is deterministic.
func sortByAxis(box []*histEntry, axis int) {
	sort.Slice(box, func(i, j int) bool {
		a, b := box[i], box[j]
		var av, bv uint8
		switch axis {
		case 0:
			av, bv = a.r, b.r
		case 1:
			av, bv = a.g, b.g
		default:
			av, bv = a.b, b.b
		}
		if av != bv {
			return av < bv
		}
		return a.key < b.key
	})
}

// quantKey packs an RGB triple into a 15-bit 5-bit-per-channel bucket key.
func quantKey(r, g, b uint8) uint16 {
	return uint16(r>>3)<<10 | uint16(g>>3)<<5 | uint16(b>>3)
}
