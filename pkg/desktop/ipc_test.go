package desktop

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
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

func TestSplitArtistTitle(t *testing.T) {
	tests := []struct {
		input      string
		wantArtist string
		wantTitle  string
	}{
		{"Tycho - A Walk", "Tycho", "A Walk"},
		{"Kavinsky — Nightcall", "Kavinsky", "Nightcall"},
		{"Daft Punk – One More Time", "Daft Punk", "One More Time"},
		{"Solo Track Name", "", "Solo Track Name"},
		{"   Artist Name   -   Track Name   ", "Artist Name", "Track Name"},
		{"", "", ""},
		{"Multiple - Delimiters - In - Track", "Multiple", "Delimiters - In - Track"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			artist, title := SplitArtistTitle(tt.input)
			if artist != tt.wantArtist || title != tt.wantTitle {
				t.Errorf("SplitArtistTitle(%q) = (%q, %q), want (%q, %q)", tt.input, artist, title, tt.wantArtist, tt.wantTitle)
			}
		})
	}
}

func TestPlaybackInfoJSONSerialization(t *testing.T) {
	info := &PlaybackInfo{
		Status:      "playing",
		StationID:   "somafm_groovesalad",
		StationName: "SomaFM Groove Salad",
		Station:     "SomaFM Groove Salad",
		Artist:      "Tycho",
		Title:       "A Walk",
		Track:       "Tycho - A Walk",
		Bitrate:     128,
		Volume:      80,
		Muted:       false,
		Backend:     "mpv",
		Visualizer:  "dj-cat",
	}

	data, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if parsed["status"] != "playing" {
		t.Errorf("expected status playing, got %v", parsed["status"])
	}
	if parsed["station_id"] != "somafm_groovesalad" {
		t.Errorf("expected station_id somafm_groovesalad, got %v", parsed["station_id"])
	}
	if parsed["station_name"] != "SomaFM Groove Salad" {
		t.Errorf("expected station_name SomaFM Groove Salad, got %v", parsed["station_name"])
	}
	if parsed["artist"] != "Tycho" {
		t.Errorf("expected artist Tycho, got %v", parsed["artist"])
	}
	if parsed["title"] != "A Walk" {
		t.Errorf("expected title A Walk, got %v", parsed["title"])
	}
	if parsed["bitrate"] != float64(128) {
		t.Errorf("expected bitrate 128, got %v", parsed["bitrate"])
	}
	if parsed["volume"] != float64(80) {
		t.Errorf("expected volume 80, got %v", parsed["volume"])
	}
	if parsed["visualizer"] != "dj-cat" {
		t.Errorf("expected visualizer dj-cat, got %v", parsed["visualizer"])
	}
	if parsed["backend"] != "mpv" {
		t.Errorf("expected backend mpv, got %v", parsed["backend"])
	}
}

func TestParseActionCurrent(t *testing.T) {
	action, ok := ParseAction("current")
	if !ok || action != ActionStatus {
		t.Errorf("ParseAction('current') = (%v, %v), want (ActionStatus, true)", action, ok)
	}
}

func TestSanitizeStringSecurity(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "clean normal string",
			input:    "Tycho - A Walk",
			maxLen:   100,
			expected: "Tycho - A Walk",
		},
		{
			name:     "strip ANSI color escape codes",
			input:    "\x1b[31mRed Alert\x1b[0m Track",
			maxLen:   100,
			expected: "Red Alert Track",
		},
		{
			name:     "strip OSC-52 clipboard hijack sequence",
			input:    "Artist\x1b]52;c;evil\x07Title",
			maxLen:   100,
			expected: "ArtistTitle",
		},
		{
			name:     "strip ASCII control characters and DEL",
			input:    "Artist\x00\x01\x08\x0b\x1f - Title\x7f",
			maxLen:   100,
			expected: "Artist - Title",
		},
		{
			name:     "enforce max length truncation",
			input:    "Very Long Station Name",
			maxLen:   9,
			expected: "Very Long",
		},
		{
			name:     "empty string",
			input:    "",
			maxLen:   50,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeString(tt.input, tt.maxLen)
			if got != tt.expected {
				t.Errorf("SanitizeString(%q, %d) = %q; want %q", tt.input, tt.maxLen, got, tt.expected)
			}
		})
	}
}

func TestIPCSocketSymlinkSecurity(t *testing.T) {
	tempDir := t.TempDir()
	realSock := filepath.Join(tempDir, "real.sock")
	symlinkSock := filepath.Join(tempDir, "symlink.sock")

	server, err := StartIPCServer(realSock, func(action MediaAction) (*PlaybackInfo, error) {
		return &PlaybackInfo{Status: "playing"}, nil
	})
	if err != nil {
		t.Fatalf("StartIPCServer failed: %v", err)
	}
	defer server.Close()

	// Create symlink pointing to real socket
	if err := os.Symlink(realSock, symlinkSock); err != nil {
		t.Fatalf("failed to create symlink: %v", err)
	}

	// Attempting to connect via symlink MUST fail for security
	_, err = SendIPCCommand(symlinkSock, "status")
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Errorf("expected error rejecting symlink socket, got %v", err)
	}
}
