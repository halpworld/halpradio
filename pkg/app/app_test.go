package app

import (
	"bytes"
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/desktop"
)

func TestSetupAppVersion(t *testing.T) {
	var buf bytes.Buffer
	appInst, isVersion, err := SetupApp([]string{"-version"}, nil, &buf)
	if err != nil {
		t.Fatalf("Unexpected error for -version: %v", err)
	}
	if !isVersion {
		t.Errorf("Expected isVersion to be true")
	}
	if appInst != nil {
		t.Errorf("Expected appInst to be nil when version flag is passed")
	}
	if !strings.Contains(buf.String(), Version) {
		t.Errorf("Expected version output to contain %s, got: %s", Version, buf.String())
	}
}

func TestSetupAppFlagsAndDefaults(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	var buf bytes.Buffer
	appInst, isVersion, err := SetupApp([]string{
		"-backend", "native",
		"-theme", "dracula",
		"-notifications=false",
		"-mpris=false",
		"-ipc=false",
	}, []byte{}, &buf)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if isVersion {
		t.Errorf("Expected isVersion to be false")
	}
	if appInst == nil {
		t.Fatalf("Expected non-nil AppInstance")
	}
	defer func() {
		if appInst.Desktop != nil {
			_ = appInst.Desktop.Close()
		}
	}()

	if appInst.Config.PlayerBackend != "native" {
		t.Errorf("Expected backend 'native', got %s", appInst.Config.PlayerBackend)
	}
	if appInst.Config.Theme != "dracula" {
		t.Errorf("Expected theme 'dracula', got %s", appInst.Config.Theme)
	}
	if appInst.Config.SongNotifications {
		t.Errorf("Expected SongNotifications to be false")
	}
	if appInst.Config.MPRISEnabled {
		t.Errorf("Expected MPRISEnabled to be false")
	}
	if appInst.Config.IPCEnabled {
		t.Errorf("Expected IPCEnabled to be false")
	}
	if appInst.Player == nil || appInst.Program == nil || appInst.Store == nil || appInst.Desktop == nil {
		t.Errorf("AppInstance has uninitialized components: %+v", appInst)
	}
}

func TestSetupAppInvalidFlag(t *testing.T) {
	var buf bytes.Buffer
	_, _, err := SetupApp([]string{"-invalid-flag"}, nil, &buf)
	if err == nil {
		t.Errorf("Expected error for invalid CLI flag, got nil")
	}
}

func TestRunRemoteHelp(t *testing.T) {
	var buf bytes.Buffer
	done, err := RunRemote([]string{}, &buf)
	if err != nil {
		t.Fatalf("unexpected error on empty remote args: %v", err)
	}
	if !done {
		t.Errorf("expected done to be true")
	}
	if !strings.Contains(buf.String(), "Usage: halpradio remote") {
		t.Errorf("expected usage help message, got %s", buf.String())
	}
}

func TestSetupAppRemoteSubcommand(t *testing.T) {
	var buf bytes.Buffer
	// Test when socket is not present
	_, isDone, err := SetupApp([]string{"remote", "status"}, nil, &buf)
	if !isDone {
		t.Errorf("expected isDone to be true for remote command")
	}
	if err == nil {
		t.Errorf("expected error when remote instance is not running")
	}
}

func TestRunRemoteWithActiveIPC(t *testing.T) {
	sockPath := desktop.GetDefaultSocketPath()

	server, err := desktop.StartIPCServer(sockPath, func(action desktop.MediaAction) (*desktop.PlaybackInfo, error) {
		return &desktop.PlaybackInfo{
			Status:  "PLAYING",
			Station: "Ibiza Global Radio",
			Track:   "Deep House Mix",
			Volume:  80,
			Backend: "native",
		}, nil
	})
	if err != nil {
		t.Fatalf("failed to start test IPC server: %v", err)
	}
	defer server.Close()

	var buf bytes.Buffer
	done, err := RunRemote([]string{"status"}, &buf)
	if err != nil {
		t.Fatalf("RunRemote status failed: %v", err)
	}
	if !done {
		t.Errorf("expected done true")
	}
	if !strings.Contains(buf.String(), "Ibiza Global Radio") {
		t.Errorf("expected output to contain station name, got %s", buf.String())
	}
}
