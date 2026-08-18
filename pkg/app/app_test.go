package app

import (
	"bytes"
	"fmt"
	"os"
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
	// Ensure socket is not present
	_ = os.Remove(desktop.GetDefaultSocketPath())

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

func TestSetupAppDiscordFlag(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	var buf bytes.Buffer
	appInst, _, err := SetupApp([]string{"-discord=false"}, []byte{}, &buf)
	if err != nil {
		t.Fatalf("Unexpected error for -discord=false: %v", err)
	}
	defer func() {
		if appInst != nil && appInst.Desktop != nil {
			_ = appInst.Desktop.Close()
		}
	}()

	if appInst.Config.DiscordRPC {
		t.Errorf("Expected DiscordRPC to be false when -discord=false passed")
	}
}

func TestRunCurrentAndStatusWithActiveIPC(t *testing.T) {
	sockPath := desktop.GetDefaultSocketPath()

	mockState := &desktop.PlaybackInfo{
		Status:      "playing",
		StationID:   "somafm_groovesalad",
		StationName: "SomaFM Groove Salad",
		Station:     "SomaFM Groove Salad",
		Artist:      "Tycho",
		Title:       "A Walk",
		Track:       "Tycho - A Walk",
		Bitrate:     128,
		Volume:      80,
		Backend:     "mpv",
		Visualizer:  "dj-cat",
	}

	server, err := desktop.StartIPCServer(sockPath, func(action desktop.MediaAction) (*desktop.PlaybackInfo, error) {
		return mockState, nil
	})
	if err != nil {
		t.Fatalf("failed to start test IPC server: %v", err)
	}
	defer server.Close()

	// 1. Test `halpradio current` plain text
	var buf bytes.Buffer
	done, err := RunCurrent([]string{}, &buf)
	if err != nil {
		t.Fatalf("RunCurrent plain text failed: %v", err)
	}
	if !done {
		t.Errorf("expected done true")
	}
	expectedPlain := "SomaFM Groove Salad: Tycho - A Walk"
	if !strings.Contains(buf.String(), expectedPlain) {
		t.Errorf("expected plain text output %q, got %q", expectedPlain, buf.String())
	}

	// 2. Test `halpradio current --json`
	buf.Reset()
	done, err = RunCurrent([]string{"--json"}, &buf)
	if err != nil {
		t.Fatalf("RunCurrent --json failed: %v", err)
	}
	if !done {
		t.Errorf("expected done true")
	}
	if !strings.Contains(buf.String(), `"status": "playing"`) ||
		!strings.Contains(buf.String(), `"station_id": "somafm_groovesalad"`) ||
		!strings.Contains(buf.String(), `"artist": "Tycho"`) ||
		!strings.Contains(buf.String(), `"title": "A Walk"`) ||
		!strings.Contains(buf.String(), `"visualizer": "dj-cat"`) {
		t.Errorf("unexpected JSON payload: %s", buf.String())
	}

	// 3. Test `halpradio status --json`
	buf.Reset()
	done, err = RunStatus([]string{"--json"}, &buf)
	if err != nil {
		t.Fatalf("RunStatus --json failed: %v", err)
	}
	if !done {
		t.Errorf("expected done true")
	}
	if !strings.Contains(buf.String(), `"station_name": "SomaFM Groove Salad"`) {
		t.Errorf("expected status JSON to contain station_name, got %s", buf.String())
	}

	// 4. Test `halpradio status` plain text
	buf.Reset()
	done, err = RunStatus([]string{}, &buf)
	if err != nil {
		t.Fatalf("RunStatus plain text failed: %v", err)
	}
	if !done {
		t.Errorf("expected done true")
	}
	if !strings.Contains(buf.String(), "SomaFM Groove Salad") {
		t.Errorf("expected status output to contain station name, got %s", buf.String())
	}

	// 5. Test Paused and Stopped states for `current`
	mockState.Status = "paused"
	buf.Reset()
	_, _ = RunCurrent([]string{}, &buf)
	if !strings.Contains(buf.String(), "[PAUSED] SomaFM Groove Salad") {
		t.Errorf("expected paused text output, got %s", buf.String())
	}

	mockState.Status = "stopped"
	mockState.StationName = ""
	buf.Reset()
	_, _ = RunCurrent([]string{}, &buf)
	if !strings.Contains(buf.String(), "[STOPPED]") {
		t.Errorf("expected stopped text output, got %s", buf.String())
	}
}

func TestSetupAppTopLevelShortcuts(t *testing.T) {
	sockPath := desktop.GetDefaultSocketPath()

	var lastAction desktop.MediaAction
	server, err := desktop.StartIPCServer(sockPath, func(action desktop.MediaAction) (*desktop.PlaybackInfo, error) {
		lastAction = action
		return &desktop.PlaybackInfo{
			Status:  "playing",
			Station: "Test Station",
			Volume:  80,
		}, nil
	})
	if err != nil {
		t.Fatalf("failed to start test IPC server: %v", err)
	}
	defer server.Close()

	shortcuts := []struct {
		cmd        string
		wantAction desktop.MediaAction
	}{
		{"toggle", desktop.ActionPlayPause},
		{"play", desktop.ActionPlay},
		{"pause", desktop.ActionPause},
		{"stop", desktop.ActionStop},
		{"next", desktop.ActionNextStation},
		{"prev", desktop.ActionPrevStation},
		{"volup", desktop.ActionVolumeUp},
		{"voldown", desktop.ActionVolumeDown},
		{"mute", desktop.ActionMute},
		{"random", desktop.ActionRandom},
	}

	for _, sc := range shortcuts {
		t.Run(sc.cmd, func(t *testing.T) {
			var buf bytes.Buffer
			_, isDone, err := SetupApp([]string{sc.cmd}, nil, &buf)
			if err != nil {
				t.Fatalf("SetupApp(%q) failed: %v", sc.cmd, err)
			}
			if !isDone {
				t.Errorf("expected isDone = true for shortcut %q", sc.cmd)
			}
			if lastAction != sc.wantAction {
				t.Errorf("for shortcut %q expected action %v, got %v", sc.cmd, sc.wantAction, lastAction)
			}
		})
	}
}

func TestSetupAppUpdateCatalogSubcommand(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	var buf bytes.Buffer
	_, isDone, err := SetupApp([]string{"update-stations"}, nil, &buf)
	if !isDone {
		t.Errorf("expected isDone to be true for update-stations subcommand")
	}
	_ = err
}

func TestRunPluginCLIBranches(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	var buf bytes.Buffer

	// 1. Help
	done, err := RunPluginCLI([]string{"help"}, &buf)
	if err != nil || !done || !strings.Contains(buf.String(), "Sandboxed Wasm Plugin Manager") {
		t.Errorf("expected help output for plugin CLI, got err=%v buf=%s", err, buf.String())
	}

	// 2. Missing IDs for enable, disable, remove, install
	buf.Reset()
	done, err = RunPluginCLI([]string{"enable"}, &buf)
	if err == nil || done {
		t.Errorf("expected error for enable without ID")
	}

	buf.Reset()
	done, err = RunPluginCLI([]string{"disable"}, &buf)
	if err == nil || done {
		t.Errorf("expected error for disable without ID")
	}

	buf.Reset()
	done, err = RunPluginCLI([]string{"remove"}, &buf)
	if err == nil || done {
		t.Errorf("expected error for remove without ID")
	}

	buf.Reset()
	done, err = RunPluginCLI([]string{"install"}, &buf)
	if err == nil || done {
		t.Errorf("expected error for install without ID")
	}

	// 3. Unknown plugin command
	buf.Reset()
	done, err = RunPluginCLI([]string{"unknown-cmd"}, &buf)
	if err == nil || done {
		t.Errorf("expected error for unknown plugin subcommand")
	}

	// 4. List command
	buf.Reset()
	done, err = RunPluginCLI([]string{"list"}, &buf)
	if err != nil || !done || !strings.Contains(buf.String(), "INSTALLED PLUGINS") {
		t.Errorf("expected list output for plugin CLI, got err=%v buf=%s", err, buf.String())
	}
}

func TestRunCurrentAndStatusEdgeCases(t *testing.T) {
	// 1. Current help
	var buf bytes.Buffer
	done, err := RunCurrent([]string{"--help"}, &buf)
	if err != nil || !done || !strings.Contains(buf.String(), "Usage: halpradio current") {
		t.Errorf("expected help output for current, got %s", buf.String())
	}

	// 2. Current offline (plain vs JSON)
	_ = os.Remove(desktop.GetDefaultSocketPath())
	buf.Reset()
	done, err = RunCurrent([]string{}, &buf)
	if err == nil || done {
		t.Errorf("expected error when offline")
	}

	buf.Reset()
	done, err = RunCurrent([]string{"--json"}, &buf)
	if err == nil || done || !strings.Contains(buf.String(), `"status": "stopped"`) {
		t.Errorf("expected JSON stopped response when offline, got %s", buf.String())
	}

	// 3. Status offline
	buf.Reset()
	done, err = RunStatus([]string{}, &buf)
	if err == nil || done {
		t.Errorf("expected error for status when offline")
	}

	// 4. Server returning error
	sockPath := desktop.GetDefaultSocketPath()
	server, err := desktop.StartIPCServer(sockPath, func(action desktop.MediaAction) (*desktop.PlaybackInfo, error) {
		return nil, fmt.Errorf("forced server error")
	})
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Close()

	buf.Reset()
	_, err = RunCurrent([]string{}, &buf)
	if err == nil {
		t.Errorf("expected error when server fails")
	}

	buf.Reset()
	_, err = RunCurrent([]string{"--json"}, &buf)
	if err == nil || !strings.Contains(buf.String(), "forced server error") {
		t.Errorf("expected error in JSON output when server fails, got %s", buf.String())
	}
}

func TestRunRemoteEdgeCases(t *testing.T) {
	_ = os.Remove(desktop.GetDefaultSocketPath())
	var buf bytes.Buffer

	// Remote offline with --json
	done, err := RunRemote([]string{"toggle", "--json"}, &buf)
	if err == nil || done || !strings.Contains(buf.String(), `"status": "error"`) {
		t.Errorf("expected json error payload when remote offline, got %s", buf.String())
	}

	// Remote with active server returning volume-only
	sockPath := desktop.GetDefaultSocketPath()
	server, err := desktop.StartIPCServer(sockPath, func(action desktop.MediaAction) (*desktop.PlaybackInfo, error) {
		return &desktop.PlaybackInfo{
			Status: "playing",
			Volume: 75,
		}, nil
	})
	if err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer server.Close()

	buf.Reset()
	done, err = RunRemote([]string{"status"}, &buf)
	if err != nil || !done || !strings.Contains(buf.String(), "Volume: 75%") {
		t.Errorf("expected volume only status output, got err=%v buf=%s", err, buf.String())
	}

	// Remote status redirecting to current when --json is provided
	buf.Reset()
	done, err = RunRemote([]string{"status", "--json"}, &buf)
	if err != nil || !done || !strings.Contains(buf.String(), `"status": "playing"`) {
		t.Errorf("expected json output for remote status --json, got %s", buf.String())
	}
}
