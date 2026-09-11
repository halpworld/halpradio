package art

import (
	"encoding/base64"
	"fmt"
)

// renderITerm2 encodes a PNG payload as an iTerm2 inline image (OSC 1337)
// sized to cols x rows terminal cells.
func renderITerm2(png []byte, cols, rows int) string {
	if len(png) == 0 {
		return ""
	}
	payload := base64.StdEncoding.EncodeToString(png)
	return fmt.Sprintf(
		"\x1b]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=1;size=%d:%s\x07",
		cols, rows, len(png), payload,
	)
}
