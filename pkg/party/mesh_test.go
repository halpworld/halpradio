package party

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"
)

var groupedCodeRE = regexp.MustCompile(`^[23456789ABCDEFGHJKLMNPQRSTUVWXYZ]{4}(-[23456789ABCDEFGHJKLMNPQRSTUVWXYZ]{4}){3}$`)

func TestRoomCodeGenerationAndFormatting(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		code := GenerateRoomCode()
		if !groupedCodeRE.MatchString(code) {
			t.Fatalf("room code %q is not 4 groups of 4 from the unambiguous alphabet", code)
		}
		for _, forbidden := range []rune{'0', 'O', '1', 'I'} {
			if strings.ContainsRune(code, forbidden) {
				t.Errorf("room code %q contains ambiguous char %c", code, forbidden)
			}
		}
		if err := ValidateRoomCode(code); err != nil {
			t.Errorf("generated code %q does not validate: %v", code, err)
		}
		if seen[code] {
			t.Errorf("duplicate room code %q", code)
		}
		seen[code] = true
	}

	norm := NormalizeRoomCode(" #7k9p2xqm-hf4d wnr8 ")
	if norm != ExampleRoomCode {
		t.Errorf("NormalizeRoomCode failed: got %q, want %s", norm, ExampleRoomCode)
	}
	if got := FormatRoomCode("7k9p2xqmhf4dwnr8"); got != "#"+ExampleRoomCode {
		t.Errorf("FormatRoomCode failed: got %q", got)
	}
}

// PTY-3: the room code carries at least 60 bits (16 symbols of 32 = 80 bits).
func TestRoomCodeEntropy(t *testing.T) {
	bitsPerSymbol := 5 // len(roomCodeCharset) == 32
	if len(roomCodeCharset) != 32 {
		t.Fatalf("charset size changed: %d", len(roomCodeCharset))
	}
	if roomCodeLen*bitsPerSymbol < 60 {
		t.Fatalf("room code carries only %d bits", roomCodeLen*bitsPerSymbol)
	}
}

func TestValidateRoomCode(t *testing.T) {
	cases := []struct {
		code    string
		wantErr bool
		legacy  bool
	}{
		{ExampleRoomCode, false, false},
		{"#7k9p-2xqm-hf4d-wnr8", false, false},
		{"8X2K9P", true, true},
		{"#8x2k9p", true, true},
		{"", true, false},
		{"7K9P-2XQM-HF4D", true, false},
		{"7K9P-2XQM-HF4D-WNR0", true, false},   // ambiguous 0
		{"AAAA-AAAA-AAAA-AAAA", true, false},   // too predictable
		{"7K9P-2XQM-HF4D-WNR8-", false, false}, // trailing dash is ignored
	}
	for _, c := range cases {
		err := ValidateRoomCode(c.code)
		if (err != nil) != c.wantErr {
			t.Errorf("ValidateRoomCode(%q) err=%v, wantErr=%v", c.code, err, c.wantErr)
		}
		if c.legacy && !errors.Is(err, ErrLegacyRoomCode) {
			t.Errorf("ValidateRoomCode(%q) = %v, want ErrLegacyRoomCode", c.code, err)
		}
	}
	if !strings.Contains(ErrLegacyRoomCode.Error(), "older halpradio") {
		t.Errorf("legacy error should tell the user why: %q", ErrLegacyRoomCode)
	}
}

func TestDeriveRoomKey(t *testing.T) {
	k1, err := DeriveRoomKey(ExampleRoomCode)
	if err != nil {
		t.Fatalf("DeriveRoomKey: %v", err)
	}
	if len(k1) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(k1))
	}
	k2, _ := DeriveRoomKey("#7k9p2xqmhf4dwnr8")
	if string(k1) != string(k2) {
		t.Errorf("equivalent spellings must derive the same key")
	}
	k3, _ := DeriveRoomKey("7K9P-2XQM-HF4D-WNR9")
	if string(k1) == string(k3) {
		t.Errorf("different codes derived the same key")
	}
	if _, err := DeriveRoomKey("8X2K9P"); err == nil {
		t.Errorf("DeriveRoomKey must refuse a legacy code")
	}
}

func TestSanitizeString(t *testing.T) {
	cases := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"  hello world  ", 20, "hello world"},
		{"\x1b[31mRed Text\x1b[0m", 20, "[31mRed Text[0m"}, // ESC removed, text inert
		{"Hello\x00World\x07", 20, "HelloWorld"},
		{"This is a very long string that should be truncated", 10, "This is a"},
		{"🎧 🔥 ❤️ ☕ 🚀 👀", 4, "🎧 🔥"},
	}

	for _, c := range cases {
		got := SanitizeString(c.input, c.maxLen)
		if got != c.want {
			t.Errorf("SanitizeString(%q, %d) = %q, want %q", c.input, c.maxLen, got, c.want)
		}
	}
}

// PTY-8 / TTY-6: C1 controls (8-bit CSI/OSC) and bidi overrides are removed.
func TestSanitizeStringC1AndBidi(t *testing.T) {
	for _, in := range []string{"\u009b2J\u009b31mowned", "abc‮gnp.exe", "\u009d0;title\u009c", "a⁦b​c"} {
		out := SanitizeString(in, 64)
		for _, r := range out {
			if (r >= 0x80 && r <= 0x9f) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) || r == 0x200b {
				t.Errorf("SanitizeString(%+q) = %+q still contains %U", in, out, r)
			}
		}
	}
}

func TestIsAllowedStreamURL(t *testing.T) {
	ok := []string{"https://ice1.somafm.com/groovesalad-128-mp3", "http://10.0.0.5:8000/live"}
	bad := []string{"", "file:///etc/passwd", "ftp://x/y", "http://", "https:///path", "javascript:alert(1)",
		"http://a b/", "http://x/\x1b[2J", "//host/path", "http://x/" + strings.Repeat("a", 3000)}
	for _, u := range ok {
		if !IsAllowedStreamURL(u) {
			t.Errorf("IsAllowedStreamURL(%q) = false, want true", u)
		}
	}
	for _, u := range bad {
		if IsAllowedStreamURL(u) {
			t.Errorf("IsAllowedStreamURL(%q) = true, want false", u)
		}
	}
}

func TestMeshNetworkTCPPeering(t *testing.T) {
	roomCode := GenerateRoomCode()

	hostMesh, err := NewMeshNetwork("alice", roomCode)
	if err != nil {
		t.Fatalf("failed to create host mesh: %v", err)
	}
	defer hostMesh.Close()

	if err := hostMesh.StartListening(0); err != nil {
		t.Fatalf("host StartListening failed: %v", err)
	}
	if hostMesh.ListenPort() == 0 {
		t.Fatalf("expected non-zero listen port")
	}

	peerMesh, err := NewMeshNetwork("bob", roomCode)
	if err != nil {
		t.Fatalf("failed to create peer mesh: %v", err)
	}
	defer peerMesh.Close()

	received := make(chan *Packet, 5)
	peerMesh.SetPacketHandler(func(pkt *Packet, from PeerConn) {
		if pkt.Type == MsgTypeChat {
			received <- pkt
		}
	})

	pc, err := peerMesh.ConnectPeer(hostMesh.tcpListener.Addr().String())
	if err != nil {
		t.Fatalf("peer ConnectPeer failed: %v", err)
	}
	if pc.RemotePeerID() != hostMesh.PeerID() {
		t.Fatalf("verified remote ID %q, want host %q", pc.RemotePeerID(), hostMesh.PeerID())
	}
	if !isValidPeerID(hostMesh.PeerID()) || hostMesh.PeerID() != PeerIDFromPublicKey(hostMesh.pub) {
		t.Fatalf("peer ID must be derived from the public key")
	}

	waitFor(t, time.Second, func() bool { return hostMesh.IsConnected(peerMesh.PeerID()) })

	chatPayload, _ := json.Marshal(ChatPayload{ID: "msg-1", Message: "Welcome to the room!", Timestamp: time.Now().UnixNano()})
	if err := hostMesh.Broadcast(&Packet{Type: MsgTypeChat, Payload: chatPayload}); err != nil {
		t.Fatalf("host Broadcast failed: %v", err)
	}

	select {
	case pkt := <-received:
		if pkt.SenderID != hostMesh.PeerID() {
			t.Errorf("sender %q, want %q", pkt.SenderID, hostMesh.PeerID())
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for packet on peer mesh")
	}
}

func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %v", d)
}

// writeFrame must refuse a body larger than any peer accepts before it
// allocates, and a frame at the limit must round-trip through readFrame.
func TestWriteFrameBounds(t *testing.T) {
	var sink strings.Builder
	if err := writeFrame(&sink, make([]byte, maxDataFrame+1)); err == nil {
		t.Fatal("writeFrame accepted an oversized body")
	}
	if sink.Len() != 0 {
		t.Fatalf("oversized frame wrote %d bytes", sink.Len())
	}
	body := make([]byte, maxDataFrame)
	body[0], body[maxDataFrame-1] = 1, 2
	if err := writeFrame(&sink, body); err != nil {
		t.Fatalf("writeFrame at the limit: %v", err)
	}
	got, err := readFrame(strings.NewReader(sink.String()), maxDataFrame)
	if err != nil || len(got) != maxDataFrame || got[0] != 1 || got[maxDataFrame-1] != 2 {
		t.Fatalf("round trip failed: len=%d err=%v", len(got), err)
	}
}
