package plugin

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Minimal valid WebAssembly binary
var minimalWasmBytes = []byte{
	0x00, 0x61, 0x73, 0x6d, // \0asm (magic)
	0x01, 0x00, 0x00, 0x00, // version 1
}

func TestManagerLifecycle(t *testing.T) {
	tempHome, err := os.MkdirTemp("", "halpradio-plugin-mgr-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempHome)

	pluginsDir := filepath.Join(tempHome, "plugins")
	dataDir := filepath.Join(tempHome, "plugins_data")
	stateFile := filepath.Join(tempHome, "plugins.json")

	_ = os.MkdirAll(pluginsDir, 0700)

	// Create test plugin directory
	pDir := filepath.Join(pluginsDir, "sample-plugin")
	_ = os.MkdirAll(pDir, 0700)

	m := Manifest{
		ID:          "sample-plugin",
		Name:        "Sample Plugin",
		Version:     "1.0.0",
		Author:      "tester",
		Description: "A sample test plugin",
		WasmFile:    "plugin.wasm",
		Permissions: PermissionsConfig{
			Storage: []string{"local"},
			Events:  []string{"on_track_change", "on_playback_change"},
		},
	}
	mData, _ := yaml.Marshal(m)
	_ = os.WriteFile(filepath.Join(pDir, "manifest.yaml"), mData, 0644)
	_ = os.WriteFile(filepath.Join(pDir, "plugin.wasm"), minimalWasmBytes, 0644)

	mgr := &Manager{
		pluginsDir: pluginsDir,
		dataDir:    dataDir,
		stateFile:  stateFile,
		entries:    make(map[string]*pluginEntry),
		registry:   NewRegistryClient(""),
	}

	var notifyMu sync.Mutex
	var notifiedTitle, notifiedMsg string
	mgr.SetNotifyHandler(func(title, msg string) {
		notifyMu.Lock()
		defer notifyMu.Unlock()
		notifiedTitle = title
		notifiedMsg = msg
	})
	_ = notifiedTitle
	_ = notifiedMsg

	if err := mgr.Init(); err != nil {
		t.Fatalf("mgr.Init() failed: %v", err)
	}

	plugins := mgr.GetPlugins()
	if len(plugins) != 1 {
		t.Fatalf("expected 1 plugin, got %d", len(plugins))
	}
	if plugins[0].Manifest.ID != "sample-plugin" {
		t.Errorf("expected plugin ID sample-plugin, got %s", plugins[0].Manifest.ID)
	}
	if plugins[0].State.PermissionsApproved {
		t.Errorf("expected permissions to not be approved by default")
	}

	// Test ApprovePermissions
	if err := mgr.ApprovePermissions("sample-plugin", true); err != nil {
		t.Fatalf("ApprovePermissions failed: %v", err)
	}

	info, found := mgr.GetPlugin("sample-plugin")
	if !found || !info.State.PermissionsApproved {
		t.Errorf("expected plugin to be approved")
	}

	// Test Event Dispatching (should not crash or deadlock)
	mgr.DispatchTrackChange(TrackChangePayload{
		Station:   "Lofi Girl",
		Artist:    "Lofi Artist",
		Title:     "Chill Beats",
		Timestamp: time.Now().Format(time.RFC3339),
	})

	mgr.DispatchPlaybackChange(PlaybackChangePayload{
		Status:  "playing",
		Volume:  80,
		Backend: "native",
		Station: "Lofi Girl",
	})

	mgr.DispatchTimerTick(TimerTickPayload{
		Mode:             "pomodoro",
		State:            "focus",
		RemainingSeconds: 1500,
		TotalSeconds:     1500,
	})

	// Test Disable / Enable
	if err := mgr.DisablePlugin("sample-plugin"); err != nil {
		t.Fatalf("DisablePlugin failed: %v", err)
	}
	info, _ = mgr.GetPlugin("sample-plugin")
	if info.State.Enabled {
		t.Errorf("expected plugin to be disabled")
	}

	if err := mgr.EnablePlugin("sample-plugin"); err != nil {
		t.Fatalf("EnablePlugin failed: %v", err)
	}
	info, _ = mgr.GetPlugin("sample-plugin")
	if !info.State.Enabled {
		t.Errorf("expected plugin to be enabled")
	}

	// Test Uninstall
	if err := mgr.UninstallPlugin("sample-plugin"); err != nil {
		t.Fatalf("UninstallPlugin failed: %v", err)
	}
	if len(mgr.GetPlugins()) != 0 {
		t.Errorf("expected 0 plugins after uninstall")
	}

	_ = mgr.Close()
}

func TestSandboxCapabilities(t *testing.T) {
	tempHome, err := os.MkdirTemp("", "halpradio-sandbox-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempHome)

	storage, err := NewStorage(tempHome, "test-mod")
	if err != nil {
		t.Fatalf("NewStorage failed: %v", err)
	}

	manifest := Manifest{
		ID:      "test-mod",
		Name:    "Test Module",
		Version: "1.0.0",
		Permissions: PermissionsConfig{
			Storage: []string{"local"},
			Network: []string{"api.example.com"},
		},
	}

	sb, err := NewSandbox(
		context.Background(),
		manifest,
		PluginState{Enabled: true, PermissionsApproved: true},
		minimalWasmBytes,
		storage,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewSandbox failed: %v", err)
	}
	defer sb.Close()

	if err := sb.Start(nil); err != nil {
		t.Fatalf("sb.Start failed: %v", err)
	}

	// Invoke hook that is not implemented (should return nil safely)
	if err := sb.InvokeHook("on_track_change", []byte("{}")); err != nil {
		t.Errorf("InvokeHook on non-existent hook returned error: %v", err)
	}
}
