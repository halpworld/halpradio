package app

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/util"
)

func TestRunVolumeHelp(t *testing.T) {
	var buf bytes.Buffer
	done, err := RunVolume([]string{"help"}, &buf)
	if err != nil || !done {
		t.Fatalf("RunVolume help failed: %v", err)
	}
	if !strings.Contains(buf.String(), "halpradio volume") || !strings.Contains(buf.String(), "Usage:") {
		t.Errorf("Expected volume help text, got: %s", buf.String())
	}
}

func TestRunVolumeOffline(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	// Ensure IPC socket does not exist
	_ = os.Remove(desktop.GetDefaultSocketPath())

	var buf bytes.Buffer

	// 1. Query offline volume
	done, err := RunVolume([]string{}, &buf)
	if err != nil || !done {
		t.Fatalf("RunVolume query failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Volume: 80%") {
		t.Errorf("Expected default volume 80%%, got: %s", buf.String())
	}

	// 2. Set absolute volume offline
	buf.Reset()
	done, err = RunVolume([]string{"65"}, &buf)
	if err != nil || !done {
		t.Fatalf("RunVolume set 65 failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Default volume set to 65%") {
		t.Errorf("Expected volume set to 65%%, got: %s", buf.String())
	}

	// Verify updated config
	cfg, _ := util.LoadConfig()
	if cfg.Volume != 65 {
		t.Errorf("Expected config volume 65, got %d", cfg.Volume)
	}

	// 3. Adjust relative volume offline
	buf.Reset()
	done, err = RunVolume([]string{"+10"}, &buf)
	if err != nil || !done {
		t.Fatalf("RunVolume +10 failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Default volume set to 75%") {
		t.Errorf("Expected volume adjusted to 75%%, got: %s", buf.String())
	}

	// 4. Invalid volume value
	buf.Reset()
	done, err = RunVolume([]string{"invalid-vol"}, &buf)
	if err == nil || done {
		t.Errorf("Expected error for invalid volume string")
	}

	// 5. Volume with --json flag
	buf.Reset()
	done, err = RunVolume([]string{"--json"}, &buf)
	if err != nil || !done {
		t.Fatalf("RunVolume --json failed: %v", err)
	}
	if !strings.Contains(buf.String(), `"volume": 75`) {
		t.Errorf("Expected volume in JSON output, got: %s", buf.String())
	}
}

func TestRunVolumeWithLiveIPC(t *testing.T) {
	sockPath := desktop.GetDefaultSocketPath()

	currentVol := 80
	isMuted := false

	server, err := desktop.StartIPCServer(sockPath, func(action desktop.MediaAction) (*desktop.PlaybackInfo, error) {
		switch action {
		case desktop.ActionVolumeUp:
			currentVol += 5
			if currentVol > 100 {
				currentVol = 100
			}
		case desktop.ActionVolumeDown:
			currentVol -= 5
			if currentVol < 0 {
				currentVol = 0
			}
		case desktop.ActionMute:
			isMuted = !isMuted
		}
		return &desktop.PlaybackInfo{
			Status:  "playing",
			Station: "Test Station",
			Volume:  currentVol,
			Muted:   isMuted,
		}, nil
	})
	if err != nil {
		t.Fatalf("Failed to start IPC test server: %v", err)
	}
	defer server.Close()

	var buf bytes.Buffer

	// 1. Query live volume
	done, err := RunVolume([]string{}, &buf)
	if err != nil || !done {
		t.Fatalf("RunVolume live query failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Volume: 80%") {
		t.Errorf("Expected live volume 80%%, got: %s", buf.String())
	}

	// 2. Adjust volume up (+5)
	buf.Reset()
	done, err = RunVolume([]string{"+5"}, &buf)
	if err != nil || !done {
		t.Fatalf("RunVolume live +5 failed: %v", err)
	}
	if currentVol != 85 {
		t.Errorf("Expected currentVol 85, got %d", currentVol)
	}

	// 3. Set volume directly (60)
	buf.Reset()
	done, err = RunVolume([]string{"60"}, &buf)
	if err != nil || !done {
		t.Fatalf("RunVolume live set 60 failed: %v", err)
	}
	if currentVol != 60 {
		t.Errorf("Expected currentVol 60, got %d", currentVol)
	}

	// 4. Toggle mute
	buf.Reset()
	done, err = RunVolume([]string{"mute"}, &buf)
	if err != nil || !done {
		t.Fatalf("RunVolume live mute failed: %v", err)
	}
	if !isMuted {
		t.Errorf("Expected isMuted true")
	}

	// 5. Adjust volume down (-10)
	buf.Reset()
	done, err = RunVolume([]string{"-10"}, &buf)
	if err != nil || !done {
		t.Fatalf("RunVolume live -10 failed: %v", err)
	}
	if currentVol != 50 {
		t.Errorf("Expected currentVol 50 after -10, got %d", currentVol)
	}

	// 6. Volume with JSON on live server
	buf.Reset()
	done, err = RunVolume([]string{"--json"}, &buf)
	if err != nil || !done {
		t.Fatalf("RunVolume live --json failed: %v", err)
	}
	if !strings.Contains(buf.String(), `"volume": 50`) {
		t.Errorf("Expected volume in live JSON output, got: %s", buf.String())
	}
}

func TestRunVolumeBoundsAndErrors(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	_ = os.Remove(desktop.GetDefaultSocketPath())
	var buf bytes.Buffer

	// 1. Mute when offline returns error
	done, err := RunVolume([]string{"mute"}, &buf)
	if err == nil || done {
		t.Errorf("Expected error when attempting mute while halpradio offline")
	}

	// 2. Volume > 100 returns error
	buf.Reset()
	done, err = RunVolume([]string{"150"}, &buf)
	if err == nil || done {
		t.Errorf("Expected error for volume > 100")
	}

	// 3. Volume < 0 returns error
	buf.Reset()
	done, err = RunVolume([]string{"-50"}, &buf)
	// Note: "-50" is treated as relative stepping -50%, which clamps offline volume
	if err != nil || !done {
		t.Fatalf("RunVolume relative -50 failed: %v", err)
	}
	cfg, _ := util.LoadConfig()
	if cfg.Volume != 30 {
		t.Errorf("Expected volume 30 after 80 - 50, got %d", cfg.Volume)
	}
}

func TestSetupAppVolumeRouting(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	var buf bytes.Buffer
	_, isDone, err := SetupApp([]string{"volume", "50"}, nil, &buf)
	if err != nil || !isDone {
		t.Fatalf("SetupApp volume routing failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Default volume set to 50%") {
		t.Errorf("Expected volume set confirmation, got: %s", buf.String())
	}
}
