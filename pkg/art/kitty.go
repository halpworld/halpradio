package art

import (
	"encoding/base64"
	"fmt"
	"strings"
)

const (
	// kittyImageID is the placement id every halpradio cover uses. Keeping it
	// stable means a redraw replaces the previous cover instead of stacking.
	kittyImageID = 4242
	// kittyChunkSize is the maximum base64 payload per APC escape, as
	// mandated by the Kitty graphics protocol.
	kittyChunkSize = 4096
)

// renderKitty encodes a PNG payload as a Kitty graphics protocol placement
// scaled to cols x rows terminal cells. The payload is base64 encoded and
// split into 4096 byte chunks wrapped in APC escapes.
func renderKitty(png []byte, cols, rows int) string {
	payload := base64.StdEncoding.EncodeToString(png)
	if payload == "" {
		return ""
	}

	var sb strings.Builder
	first := true
	for len(payload) > 0 {
		size := kittyChunkSize
		if len(payload) < size {
			size = len(payload)
		}
		chunk := payload[:size]
		payload = payload[size:]

		more := 0
		if len(payload) > 0 {
			more = 1
		}

		sb.WriteString("\x1b_G")
		if first {
			// q=2 suppresses the terminal's success and error replies. Without
			// it kitty and ghostty answer on stdin, which a TUI input reader
			// surfaces as junk keypresses.
			fmt.Fprintf(&sb, "a=T,f=100,i=%d,c=%d,r=%d,q=2,m=%d", kittyImageID, cols, rows, more)
			first = false
		} else {
			fmt.Fprintf(&sb, "m=%d", more)
		}
		sb.WriteByte(';')
		sb.WriteString(chunk)
		// APC sequences must be terminated with ST; BEL is not recognised.
		sb.WriteString("\x1b\\")
	}
	return sb.String()
}

// kittyClear is the Kitty escape that deletes every placed image.
const kittyClear = "\x1b_Ga=d,d=A\x1b\\"
