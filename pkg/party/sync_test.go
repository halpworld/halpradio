package party

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestPartySessionSubSecondPlaybackSync(t *testing.T) {
	roomCode := "8X2K9P"

	// 1. Create Host session
	host, err := NewPartySession(SessionConfig{
		RoomCode: roomCode,
		RoomName: "team-focus",
		Nickname: "arkalon76",
		IsHost:   true,
		DJPass:   DJPassHostOnly,
		Port:     0,
	})
	if err != nil {
		t.Fatalf("failed to create host session: %v", err)
	}
	defer host.Close()

	// 2. Create Listener session
	listener, err := NewPartySession(SessionConfig{
		RoomCode: roomCode,
		RoomName: "team-focus",
		Nickname: "alice",
		IsHost:   false,
		DJPass:   DJPassHostOnly,
		Port:     0,
	})
	if err != nil {
		t.Fatalf("failed to create listener session: %v", err)
	}
	defer listener.Close()

	syncChan := make(chan SyncPayload, 2)
	listener.SetHandlers(
		func(sp SyncPayload) {
			syncChan <- sp
		},
		nil, nil, nil, nil,
	)

	// Connect listener to host
	hostAddr := fmtHostAddr(host.ListenPort())
	if err := listener.ConnectDirect(hostAddr); err != nil {
		t.Fatalf("listener connect failed: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	// Host changes station
	start := time.Now()
	err = host.BroadcastStationChange(
		"somafm-groove",
		"SomaFM Groove Salad",
		"https://ice1.somafm.com/groovesalad-128-mp3",
		"Downtempo / Ambient",
		"US",
		"mp3",
		128,
		"playing",
	)
	if err != nil {
		t.Fatalf("host BroadcastStationChange failed: %v", err)
	}

	select {
	case sp := <-syncChan:
		latency := time.Since(start)
		if latency > 500*time.Millisecond {
			t.Errorf("playback sync exceeded 500ms limit: took %v", latency)
		}
		if sp.StationName != "SomaFM Groove Salad" {
			t.Errorf("expected station 'SomaFM Groove Salad', got %s", sp.StationName)
		}
		if sp.Status != "playing" {
			t.Errorf("expected status 'playing', got %s", sp.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for sub-second playback sync")
	}
}

func TestPartySessionDJPassPermissions(t *testing.T) {
	roomCode := "8X2K9P"

	host, err := NewPartySession(SessionConfig{
		RoomCode: roomCode,
		Nickname: "host",
		IsHost:   true,
		DJPass:   DJPassHostOnly,
		Port:     0,
	})
	if err != nil {
		t.Fatalf("failed to create host: %v", err)
	}
	defer host.Close()

	listener, err := NewPartySession(SessionConfig{
		RoomCode: roomCode,
		Nickname: "listener",
		IsHost:   false,
		DJPass:   DJPassHostOnly,
		Port:     0,
	})
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	// In HostOnly mode: listener cannot change station
	if listener.CanChangeStation() {
		t.Errorf("listener should NOT be able to change station in HostOnly mode")
	}
	err = listener.BroadcastStationChange("station-1", "Station 1", "http://stream", "", "", "", 128, "playing")
	if err == nil {
		t.Errorf("expected error when listener attempts station change in HostOnly mode")
	}

	// Host toggles DJ pass to Open Democracy
	newMode, err := host.ToggleDJPass()
	if err != nil {
		t.Fatalf("host ToggleDJPass failed: %v", err)
	}
	if newMode != DJPassOpenDemocracy {
		t.Errorf("expected DJPassOpenDemocracy, got %s", newMode)
	}

	// Host can always change station
	if !host.CanChangeStation() {
		t.Errorf("host should always be able to change station")
	}
}

func TestPartySessionReactionsAndChat(t *testing.T) {
	roomCode := "8X2K9P"

	session, err := NewPartySession(SessionConfig{
		RoomCode: roomCode,
		Nickname: "alice",
		IsHost:   true,
		Port:     0,
	})
	if err != nil {
		t.Fatalf("failed to create session: %v", err)
	}
	defer session.Close()

	// Valid reactions (1-5)
	r1, err := session.BroadcastReaction("1")
	if err != nil || r1.Emoji != "🔥" {
		t.Errorf("expected 🔥 for reaction 1, got %s (%v)", r1.Emoji, err)
	}

	r2, err := session.BroadcastReaction("❤️")
	if err != nil || r2.Emoji != "❤️" {
		t.Errorf("expected ❤️ for direct emoji reaction, got %s (%v)", r2.Emoji, err)
	}

	// Invalid reaction
	_, err = session.BroadcastReaction("invalid-reaction")
	if err == nil {
		t.Errorf("expected error on invalid reaction")
	}

	active := session.ActiveReactions()
	if len(active) != 2 {
		t.Errorf("expected 2 active reactions, got %d", len(active))
	}

	// Chat
	msg, err := session.BroadcastChat("Loving this track! 🎵")
	if err != nil {
		t.Fatalf("BroadcastChat failed: %v", err)
	}
	if msg.Message != "Loving this track! 🎵" || msg.Sender != "alice" {
		t.Errorf("chat message content mismatch: %+v", msg)
	}

	recent := session.RecentChat()
	if len(recent) != 1 || recent[0].Message != "Loving this track! 🎵" {
		t.Errorf("unexpected recent chat list: %+v", recent)
	}
}

func TestPartySessionHostMigration(t *testing.T) {
	roomCode := "8X2K9P"

	host, err := NewPartySession(SessionConfig{
		RoomCode: roomCode,
		Nickname: "alice",
		IsHost:   true,
		Port:     0,
	})
	if err != nil {
		t.Fatalf("failed to create host: %v", err)
	}

	listener, err := NewPartySession(SessionConfig{
		RoomCode: roomCode,
		Nickname: "bob",
		IsHost:   false,
		Port:     0,
	})
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	if err := listener.ConnectDirect(fmtHostAddr(host.ListenPort())); err != nil {
		t.Fatalf("listener connect failed: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	// Host leaves room
	_ = host.Close()

	// Trigger tick on listener to detect host disconnection and run election
	time.Sleep(100 * time.Millisecond)
	listener.Tick()

	// Bob should migrate and become host!
	listener.mu.RLock()
	isHostNow := listener.isHost
	hostNick := listener.hostNickname
	listener.mu.RUnlock()

	if !isHostNow {
		t.Errorf("expected Bob to become new host after Alice left, got isHost=%v (hostNick=%s)", isHostNow, hostNick)
	}
}

func TestPartySessionSecurityEnforcement(t *testing.T) {
	roomCode := "8X2K9P"

	host, err := NewPartySession(SessionConfig{
		RoomCode: roomCode,
		Nickname: "alice",
		IsHost:   true,
		DJPass:   DJPassHostOnly,
		Port:     0,
	})
	if err != nil {
		t.Fatalf("failed to create host: %v", err)
	}
	defer host.Close()

	listener, err := NewPartySession(SessionConfig{
		RoomCode: roomCode,
		Nickname: "bob",
		IsHost:   false,
		DJPass:   DJPassHostOnly,
		Port:     0,
	})
	if err != nil {
		t.Fatalf("failed to create listener: %v", err)
	}
	defer listener.Close()

	appliedSync := make(chan SyncPayload, 2)
	listener.SetHandlers(func(sp SyncPayload) {
		appliedSync <- sp
	}, nil, nil, nil, nil)

	if err := listener.ConnectDirect(fmtHostAddr(host.ListenPort())); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	time.Sleep(150 * time.Millisecond)

	// 1. Rogue non-host trying to send a station change in HostOnly mode
	roguePayload := &SyncPayload{
		StationID:   "rogue-station",
		StationName: "Malicious Injection",
		StreamURL:   "http://attacker.com/stream",
		DJPass:      DJPassHostOnly,
	}
	listener.applySync(roguePayload, "rogue-peer-id", 100)

	select {
	case sp := <-appliedSync:
		t.Fatalf("security violation: rogue sync packet was applied! %+v", sp)
	case <-time.After(100 * time.Millisecond):
		// Success: packet was correctly rejected
	}

	// 2. Rogue peer attempting an unauthorized host takeover while host is healthy
	rogueTakeover := &Packet{
		Type:       MsgTypeHostMigrate,
		SenderID:   "rogue-peer-id",
		SenderNick: "eve",
		Timestamp:  time.Now().UnixNano(),
	}
	payloadBytes, _ := json.Marshal(HostMigratePayload{
		OldHostID:   host.PeerID(),
		NewHostID:   "rogue-peer-id",
		NewHostNick: "eve",
		Reason:      "malicious coup",
	})
	rogueTakeover.Payload = payloadBytes

	listener.handleIncomingPacket(rogueTakeover, nil)

	// Verify listener did NOT promote the rogue peer
	listener.mu.RLock()
	currentHost := listener.hostPeerID
	listener.mu.RUnlock()

	if currentHost == "rogue-peer-id" {
		t.Fatalf("security violation: rogue host takeover was accepted!")
	}
}

func fmtHostAddr(port int) string {
	return fmt.Sprintf("127.0.0.1:%d", port)
}
