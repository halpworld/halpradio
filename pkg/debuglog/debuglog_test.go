package debuglog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func readLog(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading log: %v", err)
	}
	return string(data)
}

func TestDisabledByDefault(t *testing.T) {
	Close()
	if Enabled() {
		t.Fatal("expected logging to be disabled before Init")
	}
	// Must not panic or write anywhere.
	Logf("test", "dropped %d", 1)
	DumpStacks("ignored")
	if Path() != "" {
		t.Errorf("expected empty path when disabled, got %q", Path())
	}
}

func TestInitAndLog(t *testing.T) {
	Close()
	path := filepath.Join(t.TempDir(), "nested", "debug.log")
	got, err := Init(path)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Close()

	if got != path || Path() != path {
		t.Errorf("expected path %q, got %q / %q", path, got, Path())
	}
	if !Enabled() {
		t.Fatal("expected logging to be enabled after Init")
	}

	Logf("player", "backend %q selected", "mpv")
	contents := readLog(t, path)
	if !strings.Contains(contents, `[player] backend "mpv" selected`) {
		t.Errorf("log line missing, got:\n%s", contents)
	}
}

func TestInitIsIdempotent(t *testing.T) {
	Close()
	dir := t.TempDir()
	first := filepath.Join(dir, "one.log")
	if _, err := Init(first); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Close()

	got, err := Init(filepath.Join(dir, "two.log"))
	if err != nil {
		t.Fatalf("second Init: %v", err)
	}
	if got != first {
		t.Errorf("expected the original path %q to be kept, got %q", first, got)
	}
}

func TestInitRequiresPath(t *testing.T) {
	Close()
	if _, err := Init(""); err == nil {
		t.Fatal("expected an error for an empty path")
	}
}

func TestFilePermissionsAreOwnerOnly(t *testing.T) {
	Close()
	path := filepath.Join(t.TempDir(), "debug.log")
	if _, err := Init(path); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Close()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Errorf("expected 0600 on a log holding stream URLs, got %04o", perm)
	}
}

func TestRotatesOversizedLog(t *testing.T) {
	Close()
	dir := t.TempDir()
	path := filepath.Join(dir, "debug.log")
	if err := os.WriteFile(path, make([]byte, MaxSize+1), 0600); err != nil {
		t.Fatalf("seeding log: %v", err)
	}

	if _, err := Init(path); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Close()

	if _, err := os.Stat(path + ".old"); err != nil {
		t.Errorf("expected the oversized log to be rotated aside: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Size() != 0 {
		t.Errorf("expected a fresh log, got %d bytes", info.Size())
	}
}

func TestEnvEnabled(t *testing.T) {
	tests := map[string]bool{
		"":      false,
		"0":     false,
		"false": false,
		"no":    false,
		"off":   false,
		"1":     true,
		"true":  true,
		"yes":   true,
	}
	for value, want := range tests {
		t.Setenv("HALPRADIO_DEBUG", value)
		if got := EnvEnabled(); got != want {
			t.Errorf("HALPRADIO_DEBUG=%q: expected %t, got %t", value, want, got)
		}
	}
}

func TestHeaderRecordsEnvironment(t *testing.T) {
	Close()
	path := filepath.Join(t.TempDir(), "debug.log")
	if _, err := Init(path); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Close()

	Header("9.9.9", map[string]string{"zeta": "last", "alpha": "first"})
	contents := readLog(t, path)
	for _, want := range []string{"halpradio v9.9.9 starting", "TERM=", "alpha=first", "zeta=last"} {
		if !strings.Contains(contents, want) {
			t.Errorf("expected %q in the header, got:\n%s", want, contents)
		}
	}
	if strings.Index(contents, "alpha=first") > strings.Index(contents, "zeta=last") {
		t.Error("expected extra fields to be logged in sorted order")
	}
}

func TestWatchLogsSlowOperations(t *testing.T) {
	Close()
	path := filepath.Join(t.TempDir(), "debug.log")
	if _, err := Init(path); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Close()

	oldSlow := SlowThreshold
	SlowThreshold = time.Millisecond
	defer func() { SlowThreshold = oldSlow }()

	done := Watch("Update TickMsg", true)
	time.Sleep(5 * time.Millisecond)
	done()

	contents := readLog(t, path)
	if !strings.Contains(contents, "Update TickMsg SLOW") {
		t.Errorf("expected a slow-operation line even for a quiet watch, got:\n%s", contents)
	}
	if strings.Contains(contents, "→ Update TickMsg") {
		t.Errorf("quiet watches should not log on entry, got:\n%s", contents)
	}
}

func TestWatchLogsLoudOperations(t *testing.T) {
	Close()
	path := filepath.Join(t.TempDir(), "debug.log")
	if _, err := Init(path); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Close()

	Watch(`Update KeyMsg "enter"`, false)()

	contents := readLog(t, path)
	if !strings.Contains(contents, `→ Update KeyMsg "enter"`) {
		t.Errorf("expected an entry line, got:\n%s", contents)
	}
	if !strings.Contains(contents, `← Update KeyMsg "enter"`) {
		t.Errorf("expected a completion line, got:\n%s", contents)
	}
}

func TestWatchIsNoOpWhenDisabled(t *testing.T) {
	Close()
	// Must be safe to call, and must not leave a watch behind.
	Watch("Update TickMsg", false)()
	if Enabled() {
		t.Fatal("Watch must not enable logging")
	}
}

func TestDumpStacksIncludesGoroutines(t *testing.T) {
	Close()
	path := filepath.Join(t.TempDir(), "debug.log")
	if _, err := Init(path); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Close()

	DumpStacks("test")
	contents := readLog(t, path)
	if !strings.Contains(contents, "goroutine dump (test)") || !strings.Contains(contents, "goroutine ") {
		t.Errorf("expected goroutine stacks, got:\n%s", contents)
	}
}

func TestStalledWatchDumpsStacks(t *testing.T) {
	Close()
	path := filepath.Join(t.TempDir(), "debug.log")
	if _, err := Init(path); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer Close()

	oldStall := StallThreshold
	StallThreshold = 10 * time.Millisecond
	defer func() { StallThreshold = oldStall }()

	done := Watch("Update SyncDesktop", false)
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(readLog(t, path), "STALLED: Update SyncDesktop") {
		if time.Now().After(deadline) {
			done()
			t.Fatalf("watchdog never reported the stall, log:\n%s", readLog(t, path))
		}
		time.Sleep(20 * time.Millisecond)
	}
	done()

	contents := readLog(t, path)
	if !strings.Contains(contents, "goroutine dump (stalled operation)") {
		t.Errorf("expected a goroutine dump alongside the stall, got:\n%s", contents)
	}
	if !strings.Contains(contents, "recovered after") {
		t.Errorf("expected the recovery to be recorded, got:\n%s", contents)
	}
}
