package desktop

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestGetDiscordDJAsset(t *testing.T) {
	tests := []struct {
		mode         string
		expectedKey  string
		expectedText string
	}{
		{"dj-cat", "dj_cat", "DJ Cat"},
		{"cat", "dj_cat", "DJ Cat"},
		{"dj_cat", "dj_cat", "DJ Cat"},
		{"default", "dj_cat", "DJ Cat"},
		{"", "dj_cat", "DJ Cat"},
		{"dj-dog", "dj_dog", "DJ Dog"},
		{"dog", "dj_dog", "DJ Dog"},
		{"dj-bear", "dj_bear", "DJ Bear"},
		{"bear", "dj_bear", "DJ Bear"},
		{"dj-frog", "dj_frog", "DJ Frog"},
		{"frog", "dj_frog", "DJ Frog"},
		{"dj-bunny", "dj_bunny", "DJ Bunny"},
		{"bunny", "dj_bunny", "DJ Bunny"},
		{"bars", "dj_cat", "DJ Cat (Bars)"},
		{"wave", "dj_cat", "DJ Cat (Wave)"},
		{"spectrum", "dj_cat", "DJ Cat (Spectrum)"},
		{"minimal", "dj_cat", "DJ Cat (Minimal)"},
		{"off", "dj_cat", "halpradio"},
		{"unknown-visualizer", "dj_cat", "DJ Cat"},
	}

	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			key, text := GetDiscordDJAsset(tt.mode)
			if key != tt.expectedKey {
				t.Errorf("GetDiscordDJAsset(%q) key = %q, want %q", tt.mode, key, tt.expectedKey)
			}
			if text != tt.expectedText {
				t.Errorf("GetDiscordDJAsset(%q) text = %q, want %q", tt.mode, text, tt.expectedText)
			}
		})
	}
}

func TestDiscordRPCClientProtocol(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	defer serverConn.Close()

	var receivedFrames []struct {
		Op      uint32
		Payload map[string]interface{}
	}
	var serverMu sync.Mutex
	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)
		for {
			header := make([]byte, 8)
			_, err := serverConn.Read(header)
			if err != nil {
				return
			}
			op := binary.LittleEndian.Uint32(header[0:4])
			length := binary.LittleEndian.Uint32(header[4:8])

			data := make([]byte, length)
			_, err = serverConn.Read(data)
			if err != nil {
				return
			}

			var payload map[string]interface{}
			_ = json.Unmarshal(data, &payload)

			serverMu.Lock()
			receivedFrames = append(receivedFrames, struct {
				Op      uint32
				Payload map[string]interface{}
			}{Op: op, Payload: payload})
			serverMu.Unlock()

			// If handshake (Op 0), reply with Op 1 READY frame
			if op == discordOpHandshake {
				respPayload, _ := json.Marshal(map[string]interface{}{
					"cmd": "DISPATCH",
					"evt": "READY",
					"data": map[string]interface{}{
						"v": 1,
					},
				})
				respHeader := make([]byte, 8)
				binary.LittleEndian.PutUint32(respHeader[0:4], discordOpFrame)
				binary.LittleEndian.PutUint32(respHeader[4:8], uint32(len(respPayload)))
				_, _ = serverConn.Write(respHeader)
				_, _ = serverConn.Write(respPayload)
			}
		}
	}()

	client := NewDiscordRPCClient("123456789012345678")
	client.SetDialer(func() (net.Conn, error) {
		return clientConn, nil
	})

	startTime := time.Unix(1700000000, 0)
	err := client.UpdateActivity(DiscordActivity{
		State:      "SomaFM Groove Salad",
		Details:    "Tycho - A Walk",
		LargeImage: "halpradio_logo",
		LargeText:  "halpradio - Terminal Internet Radio",
		SmallImage: "dj_cat",
		SmallText:  "DJ Cat",
		StartTime:  &startTime,
	})
	if err != nil {
		t.Fatalf("UpdateActivity failed: %v", err)
	}

	// Give a few ms for frame reception
	time.Sleep(50 * time.Millisecond)

	serverMu.Lock()
	if len(receivedFrames) < 2 {
		serverMu.Unlock()
		t.Fatalf("expected at least 2 frames (handshake + set_activity), got %d", len(receivedFrames))
	}
	handshake := receivedFrames[0]
	activityFrame := receivedFrames[1]
	serverMu.Unlock()

	if handshake.Op != discordOpHandshake {
		t.Errorf("expected handshake op 0, got %d", handshake.Op)
	}
	if handshake.Payload["client_id"] != "123456789012345678" {
		t.Errorf("unexpected client_id in handshake: %v", handshake.Payload["client_id"])
	}

	if activityFrame.Op != discordOpFrame {
		t.Errorf("expected frame op 1, got %d", activityFrame.Op)
	}
	if activityFrame.Payload["cmd"] != "SET_ACTIVITY" {
		t.Errorf("expected cmd SET_ACTIVITY, got %v", activityFrame.Payload["cmd"])
	}

	// Test Clear Activity
	err = client.ClearActivity()
	if err != nil {
		t.Fatalf("ClearActivity failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)

	serverMu.Lock()
	if len(receivedFrames) < 3 {
		serverMu.Unlock()
		t.Fatalf("expected at least 3 frames after ClearActivity, got %d", len(receivedFrames))
	}
	clearFrame := receivedFrames[2]
	serverMu.Unlock()

	args, ok := clearFrame.Payload["args"].(map[string]interface{})
	if !ok || args["activity"] != nil {
		t.Errorf("expected clear activity to have null activity payload, got %+v", args)
	}

	// Test Close
	_ = client.Close()

	// Repeated close should be safe
	if err := client.Close(); err != nil {
		t.Errorf("second Close() returned error: %v", err)
	}
}

func TestDiscordRPCClientUnavailable(t *testing.T) {
	client := NewDiscordRPCClient("")
	client.SetDialer(func() (net.Conn, error) {
		return nil, errDiscordNotRunning
	})

	// When Discord is unavailable, UpdateActivity returns an error gracefully without crashing
	err := client.UpdateActivity(DiscordActivity{
		State:   "Test Station",
		Details: "Test Track",
	})
	if err == nil {
		t.Errorf("expected error when discord is offline, got nil")
	}

	// ClearActivity on offline discord should not panic
	_ = client.ClearActivity()
	_ = client.Close()
}

type MockDiscordClient struct {
	mu           sync.Mutex
	lastActivity DiscordActivity
	cleared      bool
	closed       bool
}

func (m *MockDiscordClient) UpdateActivity(act DiscordActivity) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.lastActivity = act
	m.cleared = false
	return nil
}

func (m *MockDiscordClient) ClearActivity() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cleared = true
	m.lastActivity = DiscordActivity{}
	return nil
}

func (m *MockDiscordClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *MockDiscordClient) GetLastActivity() DiscordActivity {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastActivity
}

func (m *MockDiscordClient) IsCleared() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cleared
}

func TestDesktopManagerWithDiscord(t *testing.T) {
	mockDiscord := &MockDiscordClient{}

	cfg := DesktopConfig{
		NotificationsEnabled: false,
		MPRISEnabled:         false,
		IPCEnabled:           false,
		DiscordEnabled:       true,
		DiscordClient:        mockDiscord,
	}

	mgr := NewManager(cfg, nil)
	defer mgr.Close()

	// Update to PLAYING state with station and track
	mgr.UpdatePlaybackFull("PLAYING", "somafm_groovesalad", "SomaFM Groove Salad", "Downtempo", "Tycho - A Walk", "http://stream.url", 128, 80, false, "mpv", "dj-dog")

	// Discord update runs in background goroutine, give it time
	time.Sleep(50 * time.Millisecond)

	act := mockDiscord.GetLastActivity()
	if act.State != "SomaFM Groove Salad" {
		t.Errorf("expected state 'SomaFM Groove Salad', got %q", act.State)
	}
	if act.Details != "Tycho - A Walk" {
		t.Errorf("expected details 'Tycho - A Walk', got %q", act.Details)
	}
	if act.SmallImage != "dj_dog" || act.SmallText != "DJ Dog" {
		t.Errorf("expected small image dj_dog / DJ Dog, got %q / %q", act.SmallImage, act.SmallText)
	}
	if act.StartTime == nil {
		t.Errorf("expected non-nil StartTime for playing status")
	}

	// Update to PAUSED state
	mgr.UpdatePlaybackFull("PAUSED", "somafm_groovesalad", "SomaFM Groove Salad", "Downtempo", "Tycho - A Walk", "http://stream.url", 128, 80, false, "mpv", "dj-dog")
	time.Sleep(50 * time.Millisecond)

	act = mockDiscord.GetLastActivity()
	if !strings.Contains(act.Details, "[Paused]") {
		t.Errorf("expected paused details to contain '[Paused]', got %q", act.Details)
	}
	if act.StartTime != nil {
		t.Errorf("expected nil StartTime for paused status")
	}

	// Update to STOPPED state
	mgr.UpdatePlaybackFull("STOPPED", "", "", "", "", "", 0, 80, false, "mpv", "dj-cat")
	time.Sleep(50 * time.Millisecond)

	if !mockDiscord.IsCleared() {
		t.Errorf("expected ClearActivity on STOPPED state")
	}

	// Close manager
	_ = mgr.Close()
	if !mockDiscord.closed {
		t.Errorf("expected discord client to be closed on manager Close()")
	}
}

func TestReadDiscordFrameErrors(t *testing.T) {
	// 1. Partial header
	r := strings.NewReader("short")
	_, _, err := readDiscordFrame(r)
	if err == nil {
		t.Errorf("expected error on truncated header")
	}

	// 2. Length too large (> 65536)
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint32(buf[0:4], 1)
	binary.LittleEndian.PutUint32(buf[4:8], 70000)
	_, _, err = readDiscordFrame(bytes.NewReader(buf))
	if err == nil || !strings.Contains(err.Error(), "frame too large") {
		t.Errorf("expected frame too large error, got %v", err)
	}

	// 3. Truncated payload
	binary.LittleEndian.PutUint32(buf[4:8], 20)
	_, _, err = readDiscordFrame(bytes.NewReader(buf))
	if err == nil {
		t.Errorf("expected error on truncated body")
	}
}

func TestDialDiscordSocket(t *testing.T) {
	// Should not crash or panic when scanning candidate socket directories
	conn, err := dialDiscordSocket()
	if conn != nil {
		_ = conn.Close()
	}
	// Error is expected if Discord is not running
	_ = err
}

func TestClosedDiscordRPCClient(t *testing.T) {
	client := NewDiscordRPCClient("")
	_ = client.Close()

	err := client.UpdateActivity(DiscordActivity{State: "Test"})
	if err != errDiscordClosed {
		t.Errorf("expected errDiscordClosed, got %v", err)
	}
}
