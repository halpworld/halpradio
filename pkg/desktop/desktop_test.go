package desktop

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDesktopManagerLifecycle(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "halpradio-desktop-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	sockPath := filepath.Join(tempDir, "desktop.sock")

	var lastAction MediaAction
	var notifyCalls []string
	mockRunner := func(ctx context.Context, name string, args ...string) error {
		notifyCalls = append(notifyCalls, name)
		return nil
	}

	cfg := DesktopConfig{
		NotificationsEnabled: true,
		MPRISEnabled:         false, // Disabled in unit test unless Linux D-Bus is present
		IPCEnabled:           true,
		SocketPath:           sockPath,
		Runner:               mockRunner,
	}

	mgr := NewManager(cfg, func(a MediaAction) {
		lastAction = a
	})
	defer mgr.Close()

	// Verify initial playback info
	info := mgr.GetPlaybackInfo()
	if info.Status != "STOPPED" || info.Volume != 80 {
		t.Errorf("unexpected initial info: %+v", info)
	}

	// Update playback state
	mgr.UpdatePlayback("PLAYING", "Synthwave City", "Synthwave", "Kavinsky - Nightcall", "http://stream.url", 90, false, "native")

	info = mgr.GetPlaybackInfo()
	if info.Status != "PLAYING" || info.Station != "Synthwave City" || info.Track != "Kavinsky - Nightcall" || info.Volume != 90 {
		t.Errorf("unexpected updated info: %+v", info)
	}

	// Update playback state with muted
	mgr.UpdatePlayback("PLAYING", "Synthwave City", "Synthwave", "Kavinsky - Nightcall", "http://stream.url", 90, true, "native")
	info = mgr.GetPlaybackInfo()
	if !info.Muted {
		t.Errorf("expected muted true in PlaybackInfo")
	}

	// Test IPC interaction with manager
	resp, err := SendIPCCommand(sockPath, "play-pause")
	if err != nil {
		t.Fatalf("SendIPCCommand failed: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success true, got %v", resp.Success)
	}
	if lastAction != ActionPlayPause {
		t.Errorf("expected lastAction = %v, got %v", ActionPlayPause, lastAction)
	}

	// Test status query
	resp, err = SendIPCCommand(sockPath, "status")
	if err != nil {
		t.Fatalf("SendIPCCommand status failed: %v", err)
	}
	if !resp.Success || resp.Status == nil || resp.Status.Station != "Synthwave City" {
		t.Errorf("unexpected status query response: %+v", resp)
	}

	// Test notification toggle
	mgr.SetNotificationsEnabled(false)
	mgr.NotifySong("Station", "Track")
	mgr.SetNotificationsEnabled(true)
	mgr.NotifySong("Station", "Track")

	// Test close
	if err := mgr.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}

	// Second close should be safe
	if err := mgr.Close(); err != nil {
		t.Errorf("Second close failed: %v", err)
	}
}

func TestDesktopManagerNilSafe(t *testing.T) {
	var mgr *Manager
	mgr.UpdatePlayback("PLAYING", "Station", "Genre", "Track", "URL", 80, false, "mpv")
	mgr.NotifySong("Station", "Track")
	mgr.SetNotificationsEnabled(false)
	info := mgr.GetPlaybackInfo()
	if info == nil {
		t.Errorf("expected non-nil PlaybackInfo")
	}
	_ = mgr.Close()
}
