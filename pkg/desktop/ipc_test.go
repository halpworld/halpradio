package desktop

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIPCServerAndClient(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "halpradio-ipc-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	sockPath := filepath.Join(tempDir, "test.sock")

	var lastAction MediaAction
	fakeState := &PlaybackInfo{
		Status:  "PLAYING",
		Station: "Lofi Hip Hop Radio",
		Track:   "Chill Beats - Relax",
		Volume:  85,
		Muted:   false,
		Backend: "native",
	}

	handler := func(action MediaAction) (*PlaybackInfo, error) {
		lastAction = action
		if action == ActionQuit {
			return nil, fmt.Errorf("simulated handler failure")
		}
		return fakeState, nil
	}

	server, err := StartIPCServer(sockPath, handler)
	if err != nil {
		t.Fatalf("StartIPCServer failed: %v", err)
	}
	defer server.Close()

	// Wait for server to start
	time.Sleep(20 * time.Millisecond)

	// Test successful command
	resp, err := SendIPCCommand(sockPath, "play-pause")
	if err != nil {
		t.Fatalf("SendIPCCommand failed: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success true, got false; msg = %s", resp.Message)
	}
	if lastAction != ActionPlayPause {
		t.Errorf("expected lastAction = %v, got %v", ActionPlayPause, lastAction)
	}
	if resp.Status == nil || resp.Status.Station != "Lofi Hip Hop Radio" {
		t.Errorf("unexpected status returned: %+v", resp.Status)
	}

	// Test status query
	resp, err = SendIPCCommand(sockPath, "status")
	if err != nil {
		t.Fatalf("SendIPCCommand status failed: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected status query success true")
	}
	if lastAction != ActionStatus {
		t.Errorf("expected lastAction = %v, got %v", ActionStatus, lastAction)
	}

	// Test handler error
	resp, err = SendIPCCommand(sockPath, "quit")
	if err != nil {
		t.Fatalf("SendIPCCommand quit failed: %v", err)
	}
	if resp.Success {
		t.Errorf("expected success false when handler returns error")
	}

	// Test invalid action
	resp, err = SendIPCCommand(sockPath, "invalid-action-xyz")
	if err != nil {
		t.Fatalf("SendIPCCommand with invalid action error: %v", err)
	}
	if resp.Success {
		t.Errorf("expected success false for invalid action, got true")
	}

	// Test invalid JSON payload
	rawConn, err := net.Dial("unix", sockPath)
	if err == nil {
		_, _ = rawConn.Write([]byte("{invalid-json\n"))
		buf := make([]byte, 1024)
		n, _ := rawConn.Read(buf)
		_ = rawConn.Close()
		if n == 0 {
			t.Errorf("expected error response from server on invalid JSON")
		}
	}

	// Test closing server removes socket file
	if err := server.Close(); err != nil {
		t.Errorf("server.Close() error = %v", err)
	}

	if _, err := os.Stat(sockPath); !os.IsNotExist(err) {
		t.Errorf("expected socket file to be deleted on Close(), but it exists")
	}

	// Sending command to closed socket should return not running error
	_, err = SendIPCCommand(sockPath, "play-pause")
	if err == nil {
		t.Errorf("expected error connecting to non-existent socket")
	}
}

func TestGetDefaultSocketPath(t *testing.T) {
	path := GetDefaultSocketPath()
	if path == "" {
		t.Errorf("expected non-empty default socket path")
	}
}
