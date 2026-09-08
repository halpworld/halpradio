package party

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRoomCodeGenerationAndFormatting(t *testing.T) {
	for i := 0; i < 20; i++ {
		code := GenerateRoomCode()
		if len(code) != 6 {
			t.Fatalf("expected 6-char room code, got %q (len %d)", code, len(code))
		}
		// Must not contain ambiguous characters 0, O, 1, I
		for _, forbidden := range []rune{'0', 'O', '1', 'I'} {
			if strings.ContainsRune(code, forbidden) {
				t.Errorf("room code %q contains forbidden char %c", code, forbidden)
			}
		}
	}

	norm := NormalizeRoomCode(" #8x2k9p ")
	if norm != "8X2K9P" {
		t.Errorf("NormalizeRoomCode failed: got %q, want 8X2K9P", norm)
	}

	fmted := FormatRoomCode("8x2k9p")
	if fmted != "#8X2K9P" {
		t.Errorf("FormatRoomCode failed: got %q, want #8X2K9P", fmted)
	}

	hash1 := HashRoomCode("8X2K9P")
	hash2 := HashRoomCode("#8x2k9p")
	if hash1 != hash2 {
		t.Errorf("HashRoomCode should be identical for normalized codes: %s != %s", hash1, hash2)
	}
}

func TestE2EEEncryptionAndDecryption(t *testing.T) {
	roomCode := "8X2K9P"
	key, err := DeriveRoomKey(roomCode)
	if err != nil {
		t.Fatalf("DeriveRoomKey failed: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d bytes", len(key))
	}

	pkt := &Packet{
		Type:       MsgTypeSync,
		Seq:        42,
		SenderID:   "peer-alice",
		SenderNick: "alice",
		Timestamp:  time.Now().UnixNano(),
	}
	payload, _ := json.Marshal(SyncPayload{
		StationID:   "somafm-groove",
		StationName: "SomaFM Groove Salad",
		StreamURL:   "https://ice1.somafm.com/groovesalad-128-mp3",
		Bitrate:     128,
		Status:      "playing",
	})
	pkt.Payload = payload

	aad := []byte("halpradio-party-v1:" + HashRoomCode(roomCode))

	ciphertext, err := EncryptPacket(key, pkt, aad)
	if err != nil {
		t.Fatalf("EncryptPacket failed: %v", err)
	}
	if len(ciphertext) <= 12+16 {
		t.Fatalf("ciphertext too small: %d bytes", len(ciphertext))
	}

	// Successful decryption with matching key and AAD
	decrypted, err := DecryptPacket(key, ciphertext, aad)
	if err != nil {
		t.Fatalf("DecryptPacket failed: %v", err)
	}
	if decrypted.Type != MsgTypeSync || decrypted.Seq != 42 || decrypted.SenderNick != "alice" {
		t.Errorf("Decrypted packet mismatch: %+v", decrypted)
	}

	// Decryption failure with mismatched AAD
	_, err = DecryptPacket(key, ciphertext, []byte("wrong-aad"))
	if err == nil {
		t.Fatalf("expected decryption failure with wrong AAD, got nil error")
	}

	// Decryption failure with wrong room code / key
	wrongKey, _ := DeriveRoomKey("DIFFER")
	_, err = DecryptPacket(wrongKey, ciphertext, aad)
	if err == nil {
		t.Fatalf("expected decryption failure with wrong key, got nil error")
	}

	// Decryption failure with tampered data
	tampered := bytes.Clone(ciphertext)
	tampered[len(tampered)-1] ^= 0xFF
	_, err = DecryptPacket(key, tampered, aad)
	if err == nil {
		t.Fatalf("expected authentication error on tampered ciphertext, got nil")
	}
}

func TestBeaconTokenDerivationAndVerification(t *testing.T) {
	key, _ := DeriveRoomKey("8X2K9P")
	token := ComputeBeaconToken(key, 0)
	if len(token) == 0 {
		t.Fatalf("expected non-empty beacon token")
	}

	if !VerifyBeaconToken(key, token) {
		t.Errorf("VerifyBeaconToken failed for valid token")
	}

	wrongKey, _ := DeriveRoomKey("WRONG1")
	if VerifyBeaconToken(wrongKey, token) {
		t.Errorf("VerifyBeaconToken should fail with different key")
	}
}

func TestSanitizeString(t *testing.T) {
	cases := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"  hello world  ", 20, "hello world"},
		{"\x1b[31mRed Text\x1b[0m", 20, "Red Text"},
		{"Hello\x00World\x07", 20, "HelloWorld"},
		{"This is a very long string that should be truncated", 10, "This is a"},
		{"🎧 🔥 ❤️ ☕ 🚀 👀", 4, "🎧 🔥"}, // Test multi-byte rune boundary safety
	}

	for _, c := range cases {
		got := SanitizeString(c.input, c.maxLen)
		if got != c.want {
			t.Errorf("SanitizeString(%q, %d) = %q, want %q", c.input, c.maxLen, got, c.want)
		}
	}
}

func TestMeshNetworkTCPPeering(t *testing.T) {
	roomCode := "8X2K9P"

	hostMesh, err := NewMeshNetwork("alice", roomCode)
	if err != nil {
		t.Fatalf("failed to create host mesh: %v", err)
	}
	defer hostMesh.Close()

	if err := hostMesh.StartListening(0); err != nil {
		t.Fatalf("host StartListening failed: %v", err)
	}
	hostPort := hostMesh.ListenPort()
	if hostPort == 0 {
		t.Fatalf("expected non-zero listen port")
	}

	peerMesh, err := NewMeshNetwork("bob", roomCode)
	if err != nil {
		t.Fatalf("failed to create peer mesh: %v", err)
	}
	defer peerMesh.Close()

	received := make(chan *Packet, 5)
	peerMesh.SetPacketHandler(func(pkt *Packet, from PeerConn) {
		received <- pkt
	})

	// Connect Bob to Alice
	_, err = peerMesh.ConnectPeer(hostMesh.tcpListener.Addr().String())
	if err != nil {
		t.Fatalf("peer ConnectPeer failed: %v", err)
	}

	// Give handshake time to exchange Hello
	time.Sleep(100 * time.Millisecond)

	// Broadcast message from host to peer
	chatPayload, _ := json.Marshal(ChatPayload{
		ID:        "msg-1",
		Message:   "Welcome to the room!",
		Timestamp: time.Now().UnixNano(),
	})
	testPkt := &Packet{
		Type:    MsgTypeChat,
		Payload: chatPayload,
	}

	if err := hostMesh.Broadcast(testPkt); err != nil {
		t.Fatalf("host Broadcast failed: %v", err)
	}

	select {
	case pkt := <-received:
		if pkt.Type != MsgTypeChat {
			t.Errorf("expected MsgTypeChat, got %s", pkt.Type)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for packet on peer mesh")
	}
}
