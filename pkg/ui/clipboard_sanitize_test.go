package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/util"
)

// TTY-10: a track title from stream metadata is copied to the clipboard
// without control characters, so pasting it into a terminal cannot run
// escape sequences.
func TestCopyTrackSanitizesClipboard(t *testing.T) {
	m := createTestModel()
	m.Store.AddHistory("ambient-1", "Ambient One", "Evil\x1b]52;c;cm0gLXJmIH4K\x07Song‮")
	m.SwitchTab(7)

	var copied string
	cleanup := util.SetClipboardWriterForTesting(func(text string) error {
		copied = text
		return nil
	})
	defer cleanup()

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = nm.(Model)

	if copied == "" {
		t.Fatalf("nothing copied; status %q", m.StatusMessage)
	}
	if strings.ContainsAny(copied, "\x1b\x07‮") {
		t.Fatalf("control characters copied: %q", copied)
	}
	if strings.ContainsAny(m.StatusMessage, "\x1b\x07‮") {
		t.Fatalf("control characters in status: %q", m.StatusMessage)
	}
	if !strings.Contains(copied, "Song") {
		t.Errorf("track text lost: %q", copied)
	}
}
