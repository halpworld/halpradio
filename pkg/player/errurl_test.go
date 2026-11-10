package player

import (
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/radio"
)

// The "Invalid stream URL" status text reaches the TUI status bar: it must
// not show credentials, query tokens or control characters from the URL.
func TestInvalidStreamURLErrorIsRedacted(t *testing.T) {
	m := NewManager("mpv", 50, nil)
	_ = m.Play(radio.Station{ID: "x", URL: "ftp://user:hunter2@example.com/live?token=s3cret\x1b]0;x\x07"})
	got := m.Error()
	if m.Status() != StatusError || got == "" {
		t.Fatalf("status %v err %q", m.Status(), got)
	}
	for _, bad := range []string{"hunter2", "s3cret", "\x1b", "\x07"} {
		if strings.Contains(got, bad) {
			t.Errorf("error message leaks %q: %q", bad, got)
		}
	}
}
