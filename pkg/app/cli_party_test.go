package app

import (
	"bytes"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/util"
)

func TestPartyHelp(t *testing.T) {
	buf := new(bytes.Buffer)
	_, done, err := RunParty([]string{"--help"}, nil, buf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !done {
		t.Errorf("expected done to be true for help")
	}

	out := buf.String()
	requiredSnippets := []string{
		"P2P Mesh Synchronized Radio Rooms",
		"halpradio party create",
		"halpradio party join",
		"halpradio party status",
		"halpradio party leave",
		"halpradio party react",
		"halpradio party chat",
		"--dj-pass",
		"--nickname",
		"--headless",
	}
	for _, snippet := range requiredSnippets {
		if !strings.Contains(out, snippet) {
			t.Errorf("expected help output to contain %q, but got:\n%s", snippet, out)
		}
	}
}

func TestRouteHelpParty(t *testing.T) {
	buf := new(bytes.Buffer)
	handled := RouteHelp([]string{"party"}, nil, buf)
	if !handled {
		t.Errorf("expected RouteHelp to handle 'party'")
	}
	if !strings.Contains(buf.String(), "halpradio party") {
		t.Errorf("expected RouteHelp to print party help")
	}
}

func TestPartyJoinValidation(t *testing.T) {
	buf := new(bytes.Buffer)

	// 1. Missing room code
	_, _, err := RunParty([]string{"join"}, nil, buf)
	if err == nil {
		t.Errorf("expected error when joining without room code")
	}

	// 2. Invalid room code length
	buf.Reset()
	_, _, err = RunParty([]string{"join", "123"}, nil, buf)
	if err == nil {
		t.Errorf("expected error when joining with short room code")
	}
}

func TestPartyReactAndChatArgValidation(t *testing.T) {
	buf := new(bytes.Buffer)

	// Empty react prints usage
	buf.Reset()
	_, done, err := RunParty([]string{"react"}, nil, buf)
	if err != nil || !done {
		t.Errorf("expected usage display on empty react, got err: %v", err)
	}
	if !strings.Contains(buf.String(), "Usage:") {
		t.Errorf("expected react usage text, got: %s", buf.String())
	}

	// Empty chat prints usage
	buf.Reset()
	_, done, err = RunParty([]string{"chat"}, nil, buf)
	if err != nil || !done {
		t.Errorf("expected usage display on empty chat, got err: %v", err)
	}
	if !strings.Contains(buf.String(), "Usage:") {
		t.Errorf("expected chat usage text, got: %s", buf.String())
	}
}

func TestPartyStatusOffline(t *testing.T) {
	buf := new(bytes.Buffer)
	_, _, err := RunParty([]string{"status"}, nil, buf)
	// Halpradio is not running, so IPC query fails
	if err == nil {
		t.Errorf("expected error querying status when halpradio is offline")
	}

	// JSON mode when offline
	buf.Reset()
	_, _, err = RunParty([]string{"status", "--json"}, nil, buf)
	if err == nil {
		t.Errorf("expected error in JSON mode when offline")
	}
	var errResp map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &errResp); err != nil {
		t.Errorf("expected valid JSON error payload, got: %s", buf.String())
	}
	if errResp["active"] != false {
		t.Errorf("expected active=false in error payload, got %+v", errResp)
	}
}

func TestPartyIPCForwarding(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pty-ipc-*")
	if err != nil {
		t.Fatalf("failed creating temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)
	sockPath := filepath.Join(tempDir, "ipc.sock")

	var lastAction desktop.MediaAction
	var lastPayload string

	fakeParty := &desktop.PartyInfo{
		Active:    true,
		RoomCode:  "8X2K9P",
		RoomName:  "team-focus",
		IsHost:    true,
		Host:      "alice",
		DJPass:    "host",
		Listeners: 2,
		Peers:     []string{"alice", "bob"},
	}

	fakeStatus := &desktop.PlaybackInfo{
		Status:      "playing",
		StationName: "SomaFM Groove Salad",
		Party:       fakeParty,
	}

	server, err := desktop.StartIPCServerWithPayload(sockPath, func(action desktop.MediaAction, payload string) (*desktop.PlaybackInfo, error) {
		lastAction = action
		lastPayload = payload
		return fakeStatus, nil
	})
	if err != nil {
		t.Fatalf("StartIPCServerWithPayload failed: %v", err)
	}
	defer server.Close()

	// Wait for socket
	for i := 0; i < 20; i++ {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Set socket environment / config override
	// Test sending commands with SendIPCCommandWithPayload directly to the test socket
	resp, err := desktop.SendIPCCommandWithPayload(sockPath, "party-create", "my-room")
	if err != nil {
		t.Fatalf("SendIPCCommandWithPayload party-create failed: %v", err)
	}
	if !resp.Success {
		t.Errorf("expected success true, got %v", resp.Success)
	}
	if lastAction != desktop.ActionPartyCreate || lastPayload != "my-room" {
		t.Errorf("expected party-create with 'my-room', got %s / %s", lastAction, lastPayload)
	}

	// Test party-join
	resp, err = desktop.SendIPCCommandWithPayload(sockPath, "party-join", "8X2K9P")
	if err != nil {
		t.Fatalf("SendIPCCommandWithPayload party-join failed: %v", err)
	}
	if lastAction != desktop.ActionPartyJoin || lastPayload != "8X2K9P" {
		t.Errorf("expected party-join with '8X2K9P', got %s / %s", lastAction, lastPayload)
	}

	// Test party-react
	resp, err = desktop.SendIPCCommandWithPayload(sockPath, "party-react", "🔥")
	if err != nil {
		t.Fatalf("SendIPCCommandWithPayload party-react failed: %v", err)
	}
	if lastAction != desktop.ActionPartyReact || lastPayload != "🔥" {
		t.Errorf("expected party-react with 🔥, got %s / %s", lastAction, lastPayload)
	}

	// Test party-chat
	resp, err = desktop.SendIPCCommandWithPayload(sockPath, "party-chat", "hello squad")
	if err != nil {
		t.Fatalf("SendIPCCommandWithPayload party-chat failed: %v", err)
	}
	if lastAction != desktop.ActionPartyChat || lastPayload != "hello squad" {
		t.Errorf("expected party-chat with 'hello squad', got %s / %s", lastAction, lastPayload)
	}

	// Test party-leave
	resp, err = desktop.SendIPCCommandWithPayload(sockPath, "party-leave", "")
	if err != nil {
		t.Fatalf("SendIPCCommandWithPayload party-leave failed: %v", err)
	}
	if lastAction != desktop.ActionPartyLeave {
		t.Errorf("expected party-leave, got %s", lastAction)
	}
}

func TestSetupPartyAppCreation(t *testing.T) {
	// Test creating an AppInstance directly with a party room configured
	yamlCatalog := []byte(`
stations:
  - id: lofi-beats
    name: "Lofi Beats"
    url: "http://example.com/lofi"
    genre: "Lofi"
    country: "US"
    bitrate: 128
    codec: "MP3"
`)

	cfg := util.DefaultConfig()
	// Disable real audio outputs and desktop notifications in test
	cfg.SongNotifications = false
	cfg.IPCEnabled = false
	cfg.MPRISEnabled = false
	cfg.DiscordRPC = false
	cfg.PlayerBackend = "mock"

	// Find free local port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on test port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()

	sessionCfg := party.SessionConfig{
		RoomCode: "7K9P2X",
		RoomName: "dev-sync",
		Nickname: "test-dj",
		IsHost:   true,
		DJPass:   party.DJPassHostOnly,
		Port:     port,
	}

	buf := new(bytes.Buffer)
	appInst, err := setupPartyApp(sessionCfg, yamlCatalog, cfg, buf)
	if err != nil {
		t.Fatalf("setupPartyApp failed: %v", err)
	}
	defer func() {
		if appInst.Player != nil {
			_ = appInst.Player.Close()
		}
		if appInst.Desktop != nil {
			_ = appInst.Desktop.Close()
		}
		if appInst.PluginMgr != nil {
			_ = appInst.PluginMgr.Close()
		}
		if appInst.PartySession != nil {
			_ = appInst.PartySession.Close()
		}
	}()

	if appInst.PartySession == nil {
		t.Fatalf("expected PartySession to be initialized on appInst")
	}
	if appInst.PartySession.RoomCode() != "7K9P2X" {
		t.Errorf("expected room code 7K9P2X, got %s", appInst.PartySession.RoomCode())
	}
	if !appInst.PartySession.IsHost() {
		t.Errorf("expected appInst to be room host")
	}
	if appInst.PartySession.RoomName() != "dev-sync" {
		t.Errorf("expected room name 'dev-sync', got %s", appInst.PartySession.RoomName())
	}
}
