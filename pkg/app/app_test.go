package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/debuglog"
	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/util"
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
	if !strings.Contains(buf.String(), "halpradio remote") || !strings.Contains(buf.String(), "Usage:") {
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
	if err != nil || !done || !strings.Contains(buf.String(), "halpradio current") || !strings.Contains(buf.String(), "Usage:") {
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

func TestSetupAppCustomTheme(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	// Create custom theme in themes dir
	themesDir := util.GetThemesDir()
	_ = os.MkdirAll(themesDir, 0700)
	customYAML := `
name: "Everforest Dark"
primary: "#a7c080"
background: "#2d353b"
`
	if err := os.WriteFile(filepath.Join(themesDir, "everforest.yaml"), []byte(customYAML), 0644); err != nil {
		t.Fatalf("Failed to write custom theme: %v", err)
	}

	var buf bytes.Buffer
	appInst, isVersion, err := SetupApp([]string{
		"-theme", "everforest",
	}, []byte{}, &buf)
	if err != nil {
		t.Fatalf("Unexpected error for -theme everforest: %v", err)
	}
	if isVersion {
		t.Errorf("Expected isVersion false")
	}
	if appInst == nil {
		t.Fatalf("Expected non-nil AppInstance")
	}
	defer func() {
		if appInst.Desktop != nil {
			_ = appInst.Desktop.Close()
		}
	}()

	if appInst.Config.Theme != "everforest" {
		t.Errorf("Expected theme 'everforest', got %s", appInst.Config.Theme)
	}

	// Verify sample_theme.yaml.example was automatically created
	sampleFile := filepath.Join(themesDir, "sample_theme.yaml.example")
	if _, err := os.Stat(sampleFile); os.IsNotExist(err) {
		t.Errorf("Expected sample_theme.yaml.example to be created")
	}
}

func TestRunHelp(t *testing.T) {
	var buf bytes.Buffer
	RunHelp(&buf)
	out := buf.String()
	if !strings.Contains(out, "halpradio - Terminal Internet Radio Player") {
		t.Errorf("Expected app help header, got: %s", out)
	}
	if !strings.Contains(out, "play <target>") || !strings.Contains(out, "stations [list|search|fav]") {
		t.Errorf("Expected core commands in help output, got: %s", out)
	}

	// Test help routing via SetupApp
	buf.Reset()
	_, isDone, err := SetupApp([]string{"help"}, nil, &buf)
	if err != nil || !isDone {
		t.Fatalf("SetupApp help routing failed: %v", err)
	}
	if !strings.Contains(buf.String(), "halpradio - Terminal Internet Radio Player") {
		t.Errorf("Expected help output, got: %s", buf.String())
	}
}

func TestFormatPlaybackInfo(t *testing.T) {
	st := &desktop.PlaybackInfo{
		Status:      "playing",
		StationID:   "somafm_groovesalad",
		StationName: "SomaFM Groove Salad",
		Artist:      "Tycho",
		Title:       "A Walk",
		Track:       "Tycho - A Walk",
		Bitrate:     128,
		Volume:      80,
		Backend:     "native",
	}

	// 1. Full template
	formatted := formatPlaybackInfo(st, "[%p] %s | %a - %T (vol: %v%%, %r kbps)")
	expected := "[PLAYING] SomaFM Groove Salad | Tycho - A Walk (vol: 80%, 128 kbps)"
	if formatted != expected {
		t.Errorf("Format mismatch: got %q, want %q", formatted, expected)
	}

	// 2. Track only
	formatted = formatPlaybackInfo(st, "%s: %t")
	expected = "SomaFM Groove Salad: Tycho - A Walk"
	if formatted != expected {
		t.Errorf("Format mismatch: got %q, want %q", formatted, expected)
	}

	// 3. Nil state fallback
	formatted = formatPlaybackInfo(nil, "[%p]")
	if formatted != "[STOPPED]" {
		t.Errorf("Expected [STOPPED] on nil state, got %q", formatted)
	}
}

func TestRunCurrentAndStatusWithFormat(t *testing.T) {
	sockPath := desktop.GetDefaultSocketPath()

	server, err := desktop.StartIPCServer(sockPath, func(action desktop.MediaAction) (*desktop.PlaybackInfo, error) {
		return &desktop.PlaybackInfo{
			Status:      "playing",
			StationName: "Chill Beats",
			Track:       "Artist - Song Title",
			Volume:      70,
		}, nil
	})
	if err != nil {
		t.Fatalf("Failed to start IPC server: %v", err)
	}
	defer server.Close()

	var buf bytes.Buffer
	done, err := RunCurrent([]string{"--format", "%s: %t"}, &buf)
	if err != nil || !done {
		t.Fatalf("RunCurrent --format failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Chill Beats: Artist - Song Title") {
		t.Errorf("Expected formatted current output, got: %s", buf.String())
	}

	buf.Reset()
	done, err = RunStatus([]string{"-f", "[%p] %s (%v%%)"}, &buf)
	if err != nil || !done {
		t.Fatalf("RunStatus -f failed: %v", err)
	}
	if !strings.Contains(buf.String(), "[PLAYING] Chill Beats (70%)") {
		t.Errorf("Expected formatted status output, got: %s", buf.String())
	}
}

func TestSetupAppVersionSubcommand(t *testing.T) {
	var buf bytes.Buffer
	_, isDone, err := SetupApp([]string{"version"}, nil, &buf)
	if err != nil || !isDone {
		t.Fatalf("SetupApp version subcommand failed: %v", err)
	}
	if !strings.Contains(buf.String(), Version) {
		t.Errorf("Expected version in output, got: %s", buf.String())
	}
}

func TestSetupAppDebugFlagWritesLog(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("HALPRADIO_DEBUG", "")

	var buf bytes.Buffer
	appInst, _, err := SetupApp([]string{"-debug", "-mpris=false", "-ipc=false", "-discord=false"}, []byte{}, &buf)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if appInst == nil {
		t.Fatal("Expected non-nil AppInstance")
	}
	defer func() {
		if appInst.Desktop != nil {
			_ = appInst.Desktop.Close()
		}
		debuglog.Close()
	}()

	if appInst.DebugLogPath == "" {
		t.Fatal("Expected DebugLogPath to be set with -debug")
	}
	if appInst.DebugLogPath != util.GetDebugLogFile() {
		t.Errorf("Expected the default log path %q, got %q", util.GetDebugLogFile(), appInst.DebugLogPath)
	}

	data, err := os.ReadFile(appInst.DebugLogPath)
	if err != nil {
		t.Fatalf("Reading debug log: %v", err)
	}
	contents := string(data)
	for _, want := range []string{"halpradio v" + Version + " starting", "TERM=", "config: backend="} {
		if !strings.Contains(contents, want) {
			t.Errorf("Expected %q in the debug log, got:\n%s", want, contents)
		}
	}
}

func TestSetupAppDebugLogCustomPath(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("HALPRADIO_DEBUG", "")
	custom := filepath.Join(tempDir, "logs", "halp.log")

	var buf bytes.Buffer
	appInst, _, err := SetupApp([]string{"-debug-log", custom, "-mpris=false", "-ipc=false", "-discord=false"}, []byte{}, &buf)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer func() {
		if appInst != nil && appInst.Desktop != nil {
			_ = appInst.Desktop.Close()
		}
		debuglog.Close()
	}()

	if appInst.DebugLogPath != custom {
		t.Errorf("Expected -debug-log to select %q, got %q", custom, appInst.DebugLogPath)
	}
	if _, err := os.Stat(custom); err != nil {
		t.Errorf("Expected the custom log file to exist: %v", err)
	}
}

func TestSetupAppNoDebugLogByDefault(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)
	t.Setenv("HALPRADIO_DEBUG", "")

	var buf bytes.Buffer
	appInst, _, err := SetupApp([]string{"-mpris=false", "-ipc=false", "-discord=false"}, []byte{}, &buf)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	defer func() {
		if appInst != nil && appInst.Desktop != nil {
			_ = appInst.Desktop.Close()
		}
	}()

	if appInst.DebugLogPath != "" {
		t.Errorf("Expected no debug log without -debug, got %q", appInst.DebugLogPath)
	}
	if debuglog.Enabled() {
		t.Error("Expected diagnostic logging to stay off by default")
	}
	if _, err := os.Stat(util.GetDebugLogFile()); !os.IsNotExist(err) {
		t.Errorf("Expected no debug.log to be created, stat err = %v", err)
	}
}
