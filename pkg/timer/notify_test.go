package timer

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestRingTerminalBell(t *testing.T) {
	var buf bytes.Buffer
	cleanup := SetBellWriterForTesting(&buf)
	defer cleanup()

	// RingTerminalBell should write ASCII bell character to the configured writer
	RingTerminalBell()

	if buf.String() != "\a" {
		t.Errorf("Expected bell character '\\a', got %q", buf.String())
	}
}

func TestDispatchEvent(t *testing.T) {
	var buf bytes.Buffer
	cleanupBell := SetBellWriterForTesting(&buf)
	defer cleanupBell()

	var mu sync.Mutex
	var notifiedTitle, notifiedMessage string
	cleanupNotif := SetNotificationRunnerForTesting(func(title, msg string) {
		mu.Lock()
		defer mu.Unlock()
		notifiedTitle = title
		notifiedMessage = msg
	})
	defer cleanupNotif()

	ev := Event{
		Type:        EventFocusStart,
		Title:       "Test Event",
		Message:     "Testing dispatch",
		Phase:       PhaseFocus,
		Cycle:       1,
		TotalCycles: 4,
	}

	// 1. Dispatch with notifications disabled
	DispatchEvent(ev, false, false, "")
	time.Sleep(10 * time.Millisecond)

	mu.Lock()
	if notifiedTitle != "" {
		t.Errorf("Expected no notification when disabled, got %q", notifiedTitle)
	}
	mu.Unlock()
	if buf.Len() != 0 {
		t.Errorf("Expected no bell when disabled, got %q", buf.String())
	}

	// 2. Dispatch with notifications enabled
	DispatchEvent(ev, true, true, "")
	time.Sleep(30 * time.Millisecond)

	mu.Lock()
	if notifiedTitle != "Test Event" || notifiedMessage != "Testing dispatch" {
		t.Errorf("Expected notification 'Test Event' / 'Testing dispatch', got %q / %q", notifiedTitle, notifiedMessage)
	}
	mu.Unlock()

	if buf.String() != "\a" {
		t.Errorf("Expected bell character '\\a', got %q", buf.String())
	}
}

func TestRunCommandHook(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping unix command hook test on windows")
	}

	tempDir := t.TempDir()
	outFile := filepath.Join(tempDir, "hook_out.txt")

	script := "echo $HALPRADIO_EVENT:$HALPRADIO_PHASE:$HALPRADIO_CYCLE > " + outFile

	ev := Event{
		Type:        EventShortBreakStart,
		Phase:       PhaseShortBreak,
		Cycle:       2,
		TotalCycles: 4,
		Title:       "Short Break",
		Message:     "Take a break",
		StationID:   "break-station",
		StationName: "Break Radio",
	}

	runCommandHook(script, ev)

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("Failed reading hook output file: %v", err)
	}

	expected := "short_break_start:Short Break:2\n"
	if string(data) != expected {
		t.Errorf("Hook output mismatch: got %q, want %q", string(data), expected)
	}
}

func TestRunCommandHookEmpty(t *testing.T) {
	ev := Event{Type: EventFocusStart}
	// Should return early and not panic
	runCommandHook("", ev)
	runCommandHook("   ", ev)
}
