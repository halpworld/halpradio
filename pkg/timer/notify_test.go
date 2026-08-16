package timer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRingTerminalBell(t *testing.T) {
	// RingTerminalBell should execute without panic
	RingTerminalBell()
}

func TestDispatchEvent(t *testing.T) {
	// Dispatch with notifications disabled
	ev := Event{
		Type:        EventFocusStart,
		Title:       "Test Event",
		Message:     "Testing dispatch",
		Phase:       PhaseFocus,
		Cycle:       1,
		TotalCycles: 4,
	}

	DispatchEvent(ev, false, false, "")
	DispatchEvent(ev, true, true, "")

	// Give background goroutine small time to complete
	time.Sleep(10 * time.Millisecond)
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
