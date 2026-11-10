package app

import (
	"os"
	"strings"

	"github.com/halpworld/halpradio/pkg/util"
)

// safe makes a dynamic string from station, plugin, theme, party, registry or
// IPC data safe to print to a terminal: control, bidi and zero-width
// characters are dropped and the length is capped. Use it for every non-constant
// %s argument in CLI output.
func safe(s string) string {
	return util.SanitizeDisplay(s, 300)
}

// stdinIsTerminal reports whether stdin is an interactive terminal. It is a
// variable so tests can simulate both cases.
var stdinIsTerminal = func() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// safeJoin sanitizes each element and joins them with sep.
func safeJoin(items []string, sep string) string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, safe(it))
	}
	return strings.Join(out, sep)
}
