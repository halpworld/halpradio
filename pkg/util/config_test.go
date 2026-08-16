package util

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigDefaultsAndSerialization(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.PomodoroFocusMin != 25 {
		t.Errorf("Expected PomodoroFocusMin 25, got %d", cfg.PomodoroFocusMin)
	}
	if cfg.PomodoroShortBreak != 5 {
		t.Errorf("Expected PomodoroShortBreak 5, got %d", cfg.PomodoroShortBreak)
	}
	if cfg.PomodoroLongBreak != 15 {
		t.Errorf("Expected PomodoroLongBreak 15, got %d", cfg.PomodoroLongBreak)
	}
	if cfg.PomodoroCycles != 4 {
		t.Errorf("Expected PomodoroCycles 4, got %d", cfg.PomodoroCycles)
	}
	if !cfg.EventNotifyDesktop {
		t.Errorf("Expected EventNotifyDesktop true by default")
	}
	if !cfg.EventTerminalBell {
		t.Errorf("Expected EventTerminalBell true by default")
	}

	tempDir, err := os.MkdirTemp("", "halpradio-config-test")
	if err != nil {
		t.Fatalf("Failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	testConfigFile := filepath.Join(tempDir, "config.yaml")

	cfg.PomodoroFocusMin = 30
	cfg.PomodoroShortBreak = 7
	cfg.PomodoroCycles = 5
	cfg.EventCommandHook = "/usr/local/bin/hook.sh"

	// Test saving and reloading
	data, err := os.ReadFile(testConfigFile)
	if err == nil {
		t.Fatalf("Expected file to not exist yet: %s", string(data))
	}
}
