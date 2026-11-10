package components

import (
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/theme"
)

// TTY-9: the status bar is the one sink for status text from stations,
// plugins and party peers; control sequences must not reach the terminal.
func TestRenderStatusBarSanitizes(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	for _, in := range []struct{ query, msg string }{
		{"", "Now: \x1b]52;c;ZXZpbA==\x07 ‮evil"},
		{"q\x1b[2J", ""},
	} {
		out := RenderStatusBar(in.query, in.msg, 0, 120, th)
		if strings.Contains(out, "\x07") || strings.Contains(out, "\x1b]") || strings.Contains(out, "\x1b[2J") || strings.Contains(out, "‮") {
			t.Fatalf("control sequence reached status bar: %q", out)
		}
	}
}

// LOC-2: theme names, authors, categories and descriptions from the
// registry or a theme file never reach the terminal with escape sequences.
func TestRenderThemePickerSanitizes(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	evil := "N\x1b]0;pwn\x07‮X"
	installed := []theme.Theme{th}
	installed[0].Name = evil
	installed[0].Author = evil
	installed[0].Description = evil
	reg := []theme.RegistryTheme{{ID: "evil", Name: evil, Author: evil, Category: evil, Description: evil}}
	for tab := 0; tab < 2; tab++ {
		out := RenderThemePickerModal(installed, reg, tab, 0, "", false, evil, "", 120, 40, th)
		if strings.ContainsAny(out, "\x07‮") || strings.Contains(out, "\x1b]") {
			t.Fatalf("tab %d: unsanitized theme text: %q", tab, out)
		}
	}
}
