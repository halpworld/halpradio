package party

// Regression tests for FR-C-2 (self-nomination before a host is known),
// FR-C-3 (invite host pinning), FR-C-5 (transcript and direction keys) and
// FR-C-8 (created room code strength).

import (
	"bytes"
	crand "crypto/rand"
	"encoding/json"
	"net"
	"strings"
	"testing"
	"time"
)

func mustPinnedGuest(t *testing.T, invite, nick string) *PartySession {
	t.Helper()
	s, err := JoinWithInvite(invite, SessionConfig{RoomName: "r", Nickname: nick, DJPass: DJPassHostOnly})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// lowInsider dials target until it gets an identity that sorts below
// below, so that target's own election would pick it.
func lowInsider(t *testing.T, code string, target *PartySession, below string) *insider {
	t.Helper()
	for i := 0; i < 60; i++ {
		a := dialInsider(t, code, target.ListenPort())
		if a.m.PeerID() < below {
			waitFor(t, 2*time.Second, func() bool { return target.mesh.IsConnected(a.m.PeerID()) })
			return a
		}
		_ = a.pc.Close()
	}
	t.Skip("no low peer ID found")
	return nil
}

func sendHostMigrate(t *testing.T, from *PartySession, newID string) {
	t.Helper()
	payload, _ := json.Marshal(HostMigratePayload{OldHostID: from.PeerID(), NewHostID: newID, NewHostNick: "next", Reason: "hand-over"})
	if err := from.mesh.Broadcast(&Packet{Type: MsgTypeHostMigrate, Payload: payload}); err != nil {
		t.Fatal(err)
	}
}

// FR-C-2: a code holder that the guest never dialled cannot become the host
// of a guest that has not learned its host yet by nominating itself.
func TestPoCEmptyHostSelfNomination(t *testing.T) {
	code := GenerateRoomCode()
	guest := mustSession(t, code, false, "victim")
	got := make(chan SyncPayload, 8)
	guest.SetHandlers(func(sp SyncPayload) { got <- sp }, nil, nil, nil, nil)

	// The host starts later so that LAN discovery cannot bind it first.
	atk := lowInsider(t, code, guest, guest.PeerID())
	before := guest.RejectedHostClaims()
	_ = atk.send(t, MsgTypeHostMigrate, atk.m.PeerID(), 1, HostMigratePayload{NewHostID: atk.m.PeerID(), NewHostNick: "host"})
	waitFor(t, 2*time.Second, func() bool { return guest.RejectedHostClaims() > before })
	if h := guest.HostPeerID(); h != "" {
		t.Fatalf("guest with no host accepted a self-nominated host %s (attacker %s)", h, atk.m.PeerID())
	}
	_ = atk.send(t, MsgTypeSync, atk.m.PeerID(), 2, evilSync)
	select {
	case sp := <-got:
		if sp.FromHost {
			t.Fatalf("attacker sync applied as host: %+v", sp)
		}
	case <-time.After(300 * time.Millisecond):
	}

	host := mustSession(t, code, true, "host")
	connectGuest(t, guest, host)
	_ = host.BroadcastStationChange("somafm", "SomaFM", "https://ice1.somafm.com/groovesalad-128-mp3", "", "", "", 128, "playing")
	for {
		sp := expectSync(t, got, "real host sync")
		if sp.FromHost {
			break
		}
	}
}

// FR-C-5: a relay that forwards both nonces verbatim and replaces only the
// ephemeral keys cannot complete the handshake, because the transcript
// covers the ephemerals.
func TestPoCRelaySameNoncesEphSubstitution(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	guest := mustSession(t, code, false, "victim")
	mal, _ := NewMeshNetwork("mallory", code)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan string, 1)
	go func() {
		g, err := ln.Accept()
		if err != nil {
			done <- "accept"
			return
		}
		defer g.Close()
		b, _ := readFrame(g, maxHandshakeFrame)
		pd := mustPreamble(b)
		h, err := net.Dial("tcp", fmtHostAddr(host.ListenPort()))
		if err != nil {
			done <- "dial"
			return
		}
		defer h.Close()
		ephH, _ := x25519.GenerateKey(crand.Reader)
		ephG, _ := x25519.GenerateKey(crand.Reader)
		_ = writePreamble(h, preamble{Proto: protocolName, Version: ProtocolVersion, Nonce: pd.Nonce, Eph: ephH.PublicKey().Bytes()})
		b, _ = readFrame(h, maxHandshakeFrame)
		pl := mustPreamble(b)
		_ = writePreamble(g, preamble{Proto: protocolName, Version: ProtocolVersion, Nonce: pl.Nonce, Eph: ephG.PublicKey().Bytes()})

		pdEph, err1 := x25519.NewPublicKey(pd.Eph)
		plEph, err2 := x25519.NewPublicKey(pl.Eph)
		if err1 != nil || err2 != nil {
			done <- "bad ephemeral"
			return
		}
		sharedG, _ := ephG.ECDH(pdEph)
		sharedH, _ := ephH.ECDH(plEph)
		gD2L, gL2D := sessionKeys(mal.roomKey, sharedG, mal.transcriptHash(pd.Nonce, pl.Nonce, pd.Eph, ephG.PublicKey().Bytes()))
		hD2L, hL2D := sessionKeys(mal.roomKey, sharedH, mal.transcriptHash(pd.Nonce, pl.Nonce, ephH.PublicKey().Bytes(), pl.Eph))

		ct, _ := readFrame(g, maxHandshakeFrame) // msg 4
		pt, err := openAt(gD2L, 0, ct)
		if err != nil {
			done <- "open guest id"
			return
		}
		_ = writeFrame(h, sealAt(hD2L, 0, pt))
		ct, _ = readFrame(h, maxHandshakeFrame) // msg 5
		pt, err = openAt(hL2D, 0, ct)
		if err != nil {
			done <- "open host id"
			return
		}
		_ = writeFrame(g, sealAt(gL2D, 0, pt))
		ct, err = readFrame(g, maxHandshakeFrame) // msg 6
		if err != nil {
			done <- "guest refused the relayed host signature"
			return
		}
		pt, _ = openAt(gD2L, 1, ct)
		_ = writeFrame(h, sealAt(hD2L, 1, pt))
		_, _ = readFrame(g, maxDataFrame) // guest Hello
		payload, _ := json.Marshal(HelloAckPayload{RoomName: "r", HostID: host.PeerID(), HostNick: "host", DJPass: DJPassHostOnly, CurrentSync: &evilSync})
		data, _ := json.Marshal(&Packet{Type: MsgTypeHelloAck, Seq: 1, SenderID: host.PeerID(), SenderNick: "host", Payload: payload})
		_ = writeFrame(g, sealAt(gL2D, 1, data))
		done <- "relay completed both legs"
		_, _ = readFrame(g, maxDataFrame)
	}()

	got := make(chan SyncPayload, 2)
	guest.SetHandlers(func(sp SyncPayload) { got <- sp }, nil, nil, nil, nil)
	_, err = guest.mesh.connectPeer(ln.Addr().String(), host.PeerID())
	res := <-done
	if err == nil {
		t.Fatalf("relay with substituted ephemerals succeeded (%s); guest follows %q", res, guest.HostPeerID())
	}
	expectNoSync(t, got, 200*time.Millisecond, "relayed hello-ack")
}

// FR-C-5: each direction has its own key, so a frame cannot be reflected.
func TestSessionKeysAreDirectional(t *testing.T) {
	m, err := NewMeshNetwork("a", GenerateRoomCode())
	if err != nil {
		t.Fatal(err)
	}
	shared := bytes.Repeat([]byte{7}, 32)
	th := m.transcriptHash(bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32), bytes.Repeat([]byte{3}, 32), bytes.Repeat([]byte{4}, 32))
	d2l, l2d := sessionKeys(m.roomKey, shared, th)
	if len(d2l) != 32 || len(l2d) != 32 || bytes.Equal(d2l, l2d) {
		t.Fatalf("direction keys must differ: %x / %x", d2l, l2d)
	}
	ct := sealAt(d2l, 0, []byte("hello"))
	if _, err := openAt(l2d, 0, ct); err == nil {
		t.Fatal("a dialer->listener frame opened with the listener->dialer key")
	}
	if pt, err := openAt(d2l, 0, ct); err != nil || string(pt) != "hello" {
		t.Fatalf("own direction: %q %v", pt, err)
	}
	// The ephemerals are part of the transcript.
	th2 := m.transcriptHash(bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32), bytes.Repeat([]byte{5}, 32), bytes.Repeat([]byte{4}, 32))
	if bytes.Equal(th, th2) {
		t.Fatal("transcript hash ignores the dialer ephemeral")
	}
}

func TestFingerprintFormat(t *testing.T) {
	m, err := NewMeshNetwork("a", GenerateRoomCode())
	if err != nil {
		t.Fatal(err)
	}
	fp := PeerFingerprint(m.PeerID())
	if len(fp) != 19 || strings.Count(fp, "-") != 3 || !IsFingerprint(fp) {
		t.Fatalf("fingerprint %q", fp)
	}
	for _, r := range strings.ReplaceAll(fp, "-", "") {
		if !strings.ContainsRune(roomCodeCharset, r) {
			t.Fatalf("symbol %q outside the room-code alphabet", r)
		}
	}
	if NormalizeFingerprint(strings.ToLower(strings.ReplaceAll(fp, "-", " "))) != fp {
		t.Fatal("normalise")
	}
	if !IsFingerprint(ExampleFingerprint) || IsFingerprint("ABCD-EFGH") || IsFingerprint(fp+"A") || PeerFingerprint("nope") != "" {
		t.Fatal("shape check")
	}
	if !ContainsFingerprint("dj "+strings.ToLower(fp)) || ContainsFingerprint("dj-K4QZ") || !ContainsFingerprint("K4QZ7HXM2PRDW9NB") {
		t.Fatal("ContainsFingerprint")
	}
}

func TestParseInvite(t *testing.T) {
	code := GenerateRoomCode()
	m, _ := NewMeshNetwork("h", code)
	fp := PeerFingerprint(m.PeerID())
	inv := FormatInvite(code, fp)
	if inv != NormalizeRoomCode(code)+"/"+fp {
		t.Fatalf("invite %q", inv)
	}
	for _, in := range []string{inv, strings.ToLower(inv), " #" + strings.ReplaceAll(inv, "-", " ") + " ", strings.ReplaceAll(strings.ReplaceAll(inv, "-", ""), "/", "")} {
		c, pin, err := ParseInvite(in)
		if err != nil || c != NormalizeRoomCode(code) || pin != fp {
			t.Fatalf("ParseInvite(%q) = %q %q %v", in, c, pin, err)
		}
	}
	if c, pin, err := ParseInvite(strings.ToLower(code)); err != nil || pin != "" || c != NormalizeRoomCode(code) {
		t.Fatalf("bare code: %q %q %v", c, pin, err)
	}
	if FormatInvite(code, "") != NormalizeRoomCode(code) || FormatInvite(code, "junk") != NormalizeRoomCode(code) {
		t.Fatal("FormatInvite without a valid fingerprint")
	}
	for _, bad := range []string{"", "/", inv + "/" + fp, FormatRoomCode(code) + "/", FormatRoomCode(code) + "/ABCD", "/" + fp,
		FormatRoomCode(code) + "/" + strings.Replace(fp, fp[:1], "0", 1), "ABC/" + fp, code + fp + "A", "1111-2222-3333-4444/" + fp} {
		if c, pin, err := ParseInvite(bad); err == nil {
			t.Fatalf("ParseInvite(%q) accepted: %q %q", bad, c, pin)
		}
	}
	if _, err := JoinWithInvite("nope/nope", SessionConfig{Nickname: "x"}); err == nil {
		t.Fatal("JoinWithInvite accepted junk")
	}
	if _, err := NewPartySession(SessionConfig{RoomCode: code, Nickname: "x", IsHost: true, HostPin: fp}); err == nil {
		t.Fatal("host with a pin")
	}
}

// FR-C-3: a member that claims host first (the RR-C-2 first-claimant race)
// with a different key is ignored by a pinned guest; the real host is then
// followed and verified by the invite.
func TestPinnedGuestIgnoresFirstClaimant(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	if inv := host.Invite(); inv != FormatInvite(code, host.Fingerprint()) {
		t.Fatalf("host invite %q", inv)
	}
	guest := mustPinnedGuest(t, host.Invite(), "victim")
	if guest.HostPin() != host.Fingerprint() || guest.HostPinned() || guest.Invite() != host.Invite() {
		t.Fatalf("pin %q pinned=%v invite=%q", guest.HostPin(), guest.HostPinned(), guest.Invite())
	}
	got := make(chan SyncPayload, 8)
	guest.SetHandlers(func(sp SyncPayload) { got <- sp }, nil, nil, nil, nil)

	// A rogue host (same code, same nickname) answers the guest first.
	rogue := mustSession(t, code, true, "host")
	_ = rogue.BroadcastStationChange("x", "evil", "http://attacker.example/s", "", "", "", 1, "playing")
	if err := guest.ConnectDirect(fmtHostAddr(rogue.ListenPort())); err != nil {
		t.Fatal(err)
	}
	// An insider claims host by an unsolicited HelloAck (dropped before it is
	// parsed) and by self-nomination.
	atk := lowInsider(t, code, guest, guest.PeerID())
	_ = atk.send(t, MsgTypeHelloAck, atk.m.PeerID(), 1, HelloAckPayload{RoomName: "pwned", HostID: atk.m.PeerID(), HostNick: "host", DJPass: DJPassHostOnly, CurrentSync: &evilSync})
	_ = atk.send(t, MsgTypeHostMigrate, atk.m.PeerID(), 2, HostMigratePayload{NewHostID: atk.m.PeerID(), NewHostNick: "host"})
	_ = atk.send(t, MsgTypeSync, atk.m.PeerID(), 3, evilSync)
	waitFor(t, 2*time.Second, func() bool { return guest.RejectedHostClaims() >= 2 })
	if guest.HostPeerID() != "" || guest.RoomName() == "pwned" {
		t.Fatalf("pinned guest follows %q", guest.HostPeerID())
	}
	expectNoSync(t, got, 200*time.Millisecond, "claims before the pinned host")

	connectGuest(t, guest, host)
	if !guest.HostPinned() || guest.HostFingerprint() != host.Fingerprint() || guest.Invite() != host.Invite() {
		t.Fatalf("pinned=%v fp=%q", guest.HostPinned(), guest.HostFingerprint())
	}
	_ = host.BroadcastStationChange("somafm", "SomaFM", "https://ice1.somafm.com/groovesalad-128-mp3", "", "", "", 128, "playing")
	if sp := expectSync(t, got, "pinned host sync"); !sp.FromHost || sp.StationName != "SomaFM" {
		t.Fatalf("sync %+v", sp)
	}
}

// FR-C-3: an authenticated hand-over from the pinned host is followed and
// moves the pin; a hand-over announced by anyone else is not.
func TestPinnedHandOver(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	next := mustSession(t, code, false, "next")
	connectGuest(t, next, host)
	guest := mustPinnedGuest(t, host.Invite(), "victim")
	connectGuest(t, guest, host)
	if err := guest.ConnectDirect(fmtHostAddr(next.ListenPort())); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool {
		guest.mu.RLock()
		defer guest.mu.RUnlock()
		p, ok := guest.peers[next.PeerID()]
		return ok && p.Verified && guest.mesh.IsConnected(next.PeerID())
	})

	// Not from the host: ignored.
	atk := dialInsider(t, code, guest.ListenPort())
	waitFor(t, 2*time.Second, func() bool { return guest.mesh.IsConnected(atk.m.PeerID()) })
	before := guest.RejectedHostClaims()
	_ = atk.send(t, MsgTypeHostMigrate, atk.m.PeerID(), 1, HostMigratePayload{OldHostID: host.PeerID(), NewHostID: next.PeerID()})
	_ = atk.send(t, MsgTypeHostMigrate, atk.m.PeerID(), 2, HostMigratePayload{OldHostID: host.PeerID(), NewHostID: atk.m.PeerID()})
	waitFor(t, 2*time.Second, func() bool { return guest.RejectedHostClaims() >= before+2 })
	if guest.HostPeerID() != host.PeerID() || guest.HostPin() != host.Fingerprint() {
		t.Fatalf("non-host hand-over followed: %q", guest.HostPeerID())
	}

	sendHostMigrate(t, host, next.PeerID())
	waitFor(t, 2*time.Second, func() bool { return guest.HostPeerID() == next.PeerID() })
	if !guest.HostPinned() || guest.HostPin() != next.Fingerprint() || guest.Invite() != FormatInvite(code, next.Fingerprint()) {
		t.Fatalf("after hand-over: pinned=%v pin=%q invite=%q", guest.HostPinned(), guest.HostPin(), guest.Invite())
	}
}

// FR-C-3: a pinned guest whose host never appears follows nobody and elects
// nobody; when the pinned host leaves without a hand-over it stops following.
func TestPinnedGuestWithoutHostFollowsNobody(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	other := mustSession(t, code, false, "other")
	guest := mustPinnedGuest(t, host.Invite(), "victim")
	if err := guest.ConnectDirect(fmtHostAddr(other.ListenPort())); err != nil {
		t.Fatal(err)
	}
	atk := lowInsider(t, code, guest, guest.PeerID())
	_ = atk.send(t, MsgTypeHostMigrate, atk.m.PeerID(), 1, HostMigratePayload{NewHostID: atk.m.PeerID()})
	time.Sleep(150 * time.Millisecond)
	guest.Tick()
	if guest.HostPeerID() != "" || guest.IsHost() || guest.HostPinned() {
		t.Fatalf("hostless pinned guest follows %q (host=%v)", guest.HostPeerID(), guest.IsHost())
	}

	// The pinned host appears, then leaves without a hand-over.
	connectGuest(t, guest, host)
	_ = atk.pc.Close()
	_ = host.Close()
	waitFor(t, 3*time.Second, func() bool { return guest.HostPeerID() == "" })
	guest.Tick()
	other.Tick()
	time.Sleep(200 * time.Millisecond)
	if guest.HostPeerID() != "" || guest.IsHost() || guest.HostPinned() {
		t.Fatalf("after the pinned host left: follows %q host=%v", guest.HostPeerID(), guest.IsHost())
	}
	if guest.HostPin() != PeerFingerprint(host.PeerID()) || guest.Invite() != FormatInvite(code, guest.HostPin()) {
		t.Fatalf("pin lost: %q", guest.HostPin())
	}
}

// FR-C-8: a created code needs 8 distinct symbols; a joined code does not.
func TestCreatedRoomCodeStrength(t *testing.T) {
	weak := "2345-6723-4567-2345" // 6 distinct symbols: joinable, not creatable
	if _, err := NewPartySession(SessionConfig{RoomCode: weak, Nickname: "h", IsHost: true}); err == nil {
		t.Fatal("weak created code accepted")
	}
	g, err := NewPartySession(SessionConfig{RoomCode: weak, Nickname: "g"})
	if err != nil {
		t.Fatalf("joining a weak code: %v", err)
	}
	_ = g.Close()
	if ValidateNewRoomCode("2345-6789-ABCD-EFGH") != nil || ValidateNewRoomCode("2345-6782-3456-7823") == nil {
		t.Fatal("ValidateNewRoomCode")
	}
	for i := 0; i < 200; i++ {
		if err := ValidateNewRoomCode(GenerateRoomCode()); err != nil {
			t.Fatal(err)
		}
	}
}

// FR-C-3: in the window where the pinned host's connection is closed but
// handlePeerLeft has not run yet, a self-nomination that would win this
// guest's own election is still refused.
func TestPinnedGuestRefusesSelfNominationWhileHostGone(t *testing.T) {
	code := GenerateRoomCode()
	gone, _ := NewMeshNetwork("gone", code)
	guest := mustPinnedGuest(t, FormatInvite(code, PeerFingerprint(gone.PeerID())), "victim")
	atk := lowInsider(t, code, guest, guest.PeerID())
	waitFor(t, 2*time.Second, func() bool {
		guest.mu.RLock()
		defer guest.mu.RUnlock()
		p, ok := guest.peers[atk.m.PeerID()]
		return ok && p.Verified
	})
	guest.mu.Lock()
	guest.setHostLocked(gone.PeerID(), "host") // followed, now disconnected
	winner := guest.electionWinnerLocked(gone.PeerID())
	guest.mu.Unlock()
	if winner != atk.m.PeerID() {
		t.Skipf("election winner %s is not the attacker", winner)
	}
	guest.handleHostMigrate(atk.m.PeerID(), HostMigratePayload{NewHostID: atk.m.PeerID()})
	if guest.HostPeerID() == atk.m.PeerID() || guest.HostPin() != PeerFingerprint(gone.PeerID()) {
		t.Fatalf("pinned guest accepted a self-nominated host: %q pin %q", guest.HostPeerID(), guest.HostPin())
	}
}
