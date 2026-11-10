package party

// Regression tests for the re-review findings RR-C-1 (relay MITM by a room
// code holder), RR-C-2 (host binding visibility) and RR-C-3 (slow reader
// stalls the host). All traffic stays on 127.0.0.1.

import (
	crand "crypto/rand"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

func sealAt(key []byte, ctr uint64, pt []byte) []byte {
	a, _ := newGCM(key)
	return a.Seal(nil, counterNonce(ctr, a.NonceSize()), pt, frameAAD)
}

func openAt(key []byte, ctr uint64, ct []byte) ([]byte, error) {
	a, _ := newGCM(key)
	return a.Open(nil, counterNonce(ctr, a.NonceSize()), ct, frameAAD)
}

func relayFrame(t *testing.T, from, to net.Conn) []byte {
	t.Helper()
	b, err := readFrame(from, maxDataFrame)
	if err != nil {
		return nil
	}
	_ = writeFrame(to, b)
	return b
}

func mustPreamble(b []byte) preamble {
	var p preamble
	_ = json.Unmarshal(b, &p)
	return p
}

// RR-C-1, strategy 1: a relay that holds the room code and forwards both
// preambles (so both ephemeral keys) verbatim. Before the fix it derived both
// direction keys from the room key and the cleartext nonces, read the guest's
// frames and injected a "playing" sync sealed as the host (demonstrated:
// "relay injected host sync ... FromHost=true"). Now it is only a TCP proxy:
// it cannot open any frame, and an injected frame makes the guest hang up.
func TestRegressionRelayCannotReadOrInject(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	guest := mustSession(t, code, false, "victim")
	got := make(chan SyncPayload, 16)
	guest.SetHandlers(func(sp SyncPayload) { got <- sp }, nil, nil, nil, nil)
	mal, _ := NewMeshNetwork("mallory", code)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	type relayState struct {
		mu       sync.Mutex
		g        net.Conn
		l2dCtr   uint64 // next listener->dialer counter the guest expects
		guessL2D [][]byte
		canRead  bool
	}
	st := &relayState{}
	ready := make(chan struct{})
	go func() {
		g, err := ln.Accept()
		if err != nil {
			return
		}
		h, err := net.Dial("tcp", fmtHostAddr(host.ListenPort()))
		if err != nil {
			_ = g.Close()
			return
		}
		t.Cleanup(func() { _ = g.Close(); _ = h.Close() })
		pd := mustPreamble(relayFrame(t, g, h))
		pl := mustPreamble(relayFrame(t, h, g))
		th := mal.transcriptHash(pd.Nonce, pl.Nonce, pd.Eph, pl.Eph)

		// Every key a code holder can derive without an ephemeral private key:
		// the pre-fix formula, and the new one with no or a guessed secret.
		oldD2L := labelKey(mal.roomKey, "halpradio-party-v2/key|dialer->listener|", th)
		oldL2D := labelKey(mal.roomKey, "halpradio-party-v2/key|listener->dialer|", th)
		noD2L, noL2D := sessionKeys(mal.roomKey, nil, th)
		ownEph, _ := x25519.GenerateKey(crand.Reader)
		pub, _ := x25519.NewPublicKey(pd.Eph)
		guess, _ := ownEph.ECDH(pub)
		gD2L, gL2D := sessionKeys(mal.roomKey, guess, th)

		m4 := relayFrame(t, g, h)
		relayFrame(t, h, g) // msg 5
		relayFrame(t, g, h) // msg 6
		canRead := false
		for _, k := range [][]byte{oldD2L, noD2L, gD2L} {
			if _, err := openAt(k, 0, m4); err == nil {
				canRead = true
			}
		}
		st.mu.Lock()
		st.g, st.l2dCtr, st.canRead = g, 1, canRead
		st.guessL2D = [][]byte{oldL2D, noL2D, gL2D}
		st.mu.Unlock()
		close(ready)

		go func() { // guest -> host, verbatim
			for relayFrame(t, g, h) != nil {
			}
		}()
		for { // host -> guest, verbatim, counting frames
			b, err := readFrame(h, maxDataFrame)
			if err != nil {
				return
			}
			st.mu.Lock()
			_ = writeFrame(g, b)
			st.l2dCtr++
			st.mu.Unlock()
		}
	}()

	// As if a forged beacon announced the host's ID at the relay's address.
	if _, err := guest.mesh.connectPeer(ln.Addr().String(), host.PeerID()); err != nil {
		t.Fatalf("connect through a transparent relay: %v", err)
	}
	<-ready
	waitFor(t, 2*time.Second, func() bool { return guest.HostPeerID() == host.PeerID() })
	st.mu.Lock()
	canRead := st.canRead
	st.mu.Unlock()
	if canRead {
		t.Fatal("relay opened a frame of the guest's connection")
	}

	// The real host still reaches the guest through the proxy.
	if err := host.BroadcastStationChange("somafm", "SomaFM", "https://ice1.somafm.com/groovesalad-128-mp3", "", "", "", 128, "playing"); err != nil {
		t.Fatal(err)
	}
	expectSync(t, got, "host sync through a transparent relay")

	// Inject a host-attributed sync with each key the relay can derive.
	payload, _ := json.Marshal(evilSync)
	evil, _ := json.Marshal(&Packet{Type: MsgTypeSync, Seq: 1 << 50, SenderID: host.PeerID(), Payload: payload})
	st.mu.Lock()
	for _, k := range st.guessL2D {
		_ = writeFrame(st.g, sealAt(k, st.l2dCtr, evil))
	}
	st.mu.Unlock()
	deadline := time.After(500 * time.Millisecond)
	for done := false; !done; {
		select {
		case sp := <-got:
			if sp.StreamURL == evilSync.StreamURL {
				t.Fatalf("relay injected a host sync: %+v", sp)
			}
		case <-deadline:
			done = true
		}
	}
	waitFor(t, 2*time.Second, func() bool { return !guest.mesh.IsConnected(host.PeerID()) })
}

// RR-C-1, strategy 2: a relay that holds the room code replaces both
// ephemeral keys, so it knows the keys of each leg, and forwards the host's
// real identity and signature to the guest (claiming the guest's identity
// towards the host, so the signed identities match). The signature covers the
// transcript, which differs per leg, so the guest refuses the connection and
// never binds the host's PeerID.
func TestRegressionRelayKeySubstitutionFails(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	guest := mustSession(t, code, false, "victim")
	mal, _ := NewMeshNetwork("mallory", code)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	forwarded := make(chan bool, 1)
	go func() {
		g, err := ln.Accept()
		if err != nil {
			forwarded <- false
			return
		}
		defer g.Close()
		// Guest leg: we play the listener with our own ephemeral key.
		b, err := readFrame(g, maxHandshakeFrame)
		if err != nil {
			forwarded <- false
			return
		}
		pd := mustPreamble(b)
		ephG, _ := x25519.GenerateKey(crand.Reader)
		nonceG := make([]byte, 32)
		_, _ = crand.Read(nonceG)
		_ = writePreamble(g, preamble{Proto: protocolName, Version: ProtocolVersion, Nonce: nonceG, Eph: ephG.PublicKey().Bytes()})
		pdEph, _ := x25519.NewPublicKey(pd.Eph)
		sharedG, _ := ephG.ECDH(pdEph)
		thG := mal.transcriptHash(pd.Nonce, nonceG, pd.Eph, ephG.PublicKey().Bytes())
		gD2L, gL2D := sessionKeys(mal.roomKey, sharedG, thG)
		ct, _ := readFrame(g, maxHandshakeFrame)
		pt, err := openAt(gD2L, 0, ct)
		if err != nil {
			forwarded <- false
			return
		}
		var guestID identityMsg
		_ = json.Unmarshal(pt, &guestID)

		// Host leg: we play the dialer and claim the guest's identity key.
		h, err := net.Dial("tcp", fmtHostAddr(host.ListenPort()))
		if err != nil {
			forwarded <- false
			return
		}
		defer h.Close()
		ephH, _ := x25519.GenerateKey(crand.Reader)
		nonceH := make([]byte, 32)
		_, _ = crand.Read(nonceH)
		_ = writePreamble(h, preamble{Proto: protocolName, Version: ProtocolVersion, Nonce: nonceH, Eph: ephH.PublicKey().Bytes()})
		b, err = readFrame(h, maxHandshakeFrame)
		if err != nil {
			forwarded <- false
			return
		}
		pl := mustPreamble(b)
		plEph, _ := x25519.NewPublicKey(pl.Eph)
		sharedH, _ := ephH.ECDH(plEph)
		thH := mal.transcriptHash(nonceH, pl.Nonce, ephH.PublicKey().Bytes(), pl.Eph)
		hD2L, hL2D := sessionKeys(mal.roomKey, sharedH, thH)
		claim, _ := json.Marshal(identityMsg{Version: ProtocolVersion, PubKey: guestID.PubKey, Nick: guestID.Nick, ListenPort: guestID.ListenPort})
		_ = writeFrame(h, sealAt(hD2L, 0, claim))
		ct, err = readFrame(h, maxHandshakeFrame)
		if err != nil {
			forwarded <- false
			return
		}
		hostAuth, err := openAt(hL2D, 0, ct)
		if err != nil {
			forwarded <- false
			return
		}
		// Forward the host's signed identity, re-sealed for the guest leg.
		_ = writeFrame(g, sealAt(gL2D, 0, hostAuth))
		forwarded <- true
		_, _ = readFrame(g, maxHandshakeFrame)
	}()

	_, err = guest.mesh.connectPeer(ln.Addr().String(), host.PeerID())
	if ok := <-forwarded; !ok {
		t.Fatal("relay did not get as far as forwarding the host's signature")
	}
	if err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("guest accepted a relayed host signature: err=%v", err)
	}
	if guest.mesh.IsConnected(host.PeerID()) || guest.HostPeerID() != "" {
		t.Fatalf("guest bound the host's identity through the relay: host=%q", guest.HostPeerID())
	}
}

// RR-C-1: a signature is bound to both identity keys, so a valid signature
// cannot be replayed with another peer's identity claim on the same transcript.
func TestAuthSignatureBindsBothIdentities(t *testing.T) {
	code := GenerateRoomCode()
	a, _ := NewMeshNetwork("a", code)
	b, _ := NewMeshNetwork("b", code)
	c, _ := NewMeshNetwork("c", code)
	th := make([]byte, 32)
	sig := signAuth(a, roleListener, th, b.pub, a.pub)
	if !verifyAuth(a.pub, roleListener, th, b.pub, a.pub, sig) {
		t.Fatal("valid signature rejected")
	}
	for name, ok := range map[string]bool{
		"other dialer":   verifyAuth(a.pub, roleListener, th, c.pub, a.pub, sig),
		"other role":     verifyAuth(a.pub, roleDialer, th, b.pub, a.pub, sig),
		"other channel":  verifyAuth(a.pub, roleListener, append([]byte{1}, th[1:]...), b.pub, a.pub, sig),
		"swapped owners": verifyAuth(a.pub, roleListener, th, a.pub, b.pub, sig),
	} {
		if ok {
			t.Errorf("signature verified with %s", name)
		}
	}
}

// RR-C-3: a verified peer that never reads cannot stall the host. Sends to it
// return at once, it is dropped when its queue fills, and the other guest
// keeps following the host.
func TestRegressionSlowPeerDoesNotStallHost(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	guest := mustSession(t, code, false, "victim")
	got := make(chan SyncPayload, 16)
	guest.SetHandlers(func(sp SyncPayload) { got <- sp }, nil, nil, nil, nil)
	connectGuest(t, guest, host)

	stuck := dialInsider(t, code, host.ListenPort()) // completes the handshake, then never reads
	waitFor(t, time.Second, func() bool { return host.mesh.IsConnected(stuck.m.PeerID()) })
	host.mesh.mu.RLock()
	stuckConn := host.mesh.peers[stuck.m.PeerID()]
	host.mesh.mu.RUnlock()

	big := make([]byte, 60000)
	for i := range big {
		big[i] = 'a'
	}
	payload, _ := json.Marshal(ChatPayload{Message: string(big)})
	start := time.Now()
	for i := 0; i < 400 && host.mesh.IsConnected(stuck.m.PeerID()); i++ {
		_ = host.mesh.SendDirect(stuckConn, &Packet{Type: MsgTypeChat, Payload: payload})
		// A heartbeat broadcast must stay fast while the slow peer backs up
		// (sent now and then only, to stay under the guest's flood limit).
		if i%25 != 0 {
			continue
		}
		t0 := time.Now()
		_ = host.mesh.Broadcast(&Packet{Type: MsgTypeHeartbeat, Payload: json.RawMessage(`{"sent_at":1}`)})
		if d := time.Since(t0); d > 200*time.Millisecond {
			t.Fatalf("Broadcast blocked for %v behind a peer that does not read", d)
		}
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("sending to a peer that does not read took %v", d)
	}
	waitFor(t, 2*time.Second, func() bool { return !host.mesh.IsConnected(stuck.m.PeerID()) })

	if guest.HostPeerID() != host.PeerID() || guest.IsHost() {
		t.Fatalf("guest lost its host: host=%q isHost=%v", guest.HostPeerID(), guest.IsHost())
	}
	_ = host.BroadcastStationChange("somafm", "SomaFM", "https://ice1.somafm.com/groovesalad-128-mp3", "", "", "", 128, "playing")
	expectSync(t, got, "host sync after dropping the slow peer")
}

// RR-C-3: a host that is quiet for longer than the peer timeout, but whose
// connection is still open, is not voted out.
func TestRegressionQuietHostNotElectedOut(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	guest := mustSession(t, code, false, "victim")
	connectGuest(t, guest, host)

	guest.mu.Lock()
	guest.peers[host.PeerID()].LastSeen = time.Now().Add(-3 * peerTimeout)
	guest.mu.Unlock()
	guest.Tick()
	if guest.IsHost() || guest.HostPeerID() != host.PeerID() {
		t.Fatalf("quiet but connected host was replaced: host=%q isHost=%v", guest.HostPeerID(), guest.IsHost())
	}

	// Another verified guest cannot claim the room while the host link is open.
	atk := dialInsider(t, code, guest.ListenPort())
	waitFor(t, time.Second, func() bool { return guest.mesh.IsConnected(atk.m.PeerID()) })
	guest.mu.Lock()
	guest.succession = append([]string{atk.m.PeerID()}, guest.succession...)
	guest.peers[host.PeerID()].LastSeen = time.Now().Add(-3 * peerTimeout)
	guest.mu.Unlock()
	_ = atk.send(t, MsgTypeHostMigrate, atk.m.PeerID(), 1, HostMigratePayload{OldHostID: host.PeerID(), NewHostID: atk.m.PeerID()})
	time.Sleep(200 * time.Millisecond)
	if guest.HostPeerID() != host.PeerID() {
		t.Fatalf("migration accepted while the host connection is open: host=%q", guest.HostPeerID())
	}
}

// RR-C-2: the UI can show which host identity a guest follows, and a bound
// guest never switches to another host identity by dialing it.
func TestHostFingerprintAndNoRebind(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	guest := mustSession(t, code, false, "victim")
	if guest.HostFingerprint() != "" {
		t.Fatalf("fingerprint before joining: %q", guest.HostFingerprint())
	}
	if fp := host.Fingerprint(); !IsFingerprint(fp) || len(fp) != 19 || host.HostFingerprint() != fp {
		t.Fatalf("host fingerprint = %q / %q", fp, host.HostFingerprint())
	}
	connectGuest(t, guest, host)
	if guest.HostFingerprint() != host.Fingerprint() {
		t.Fatalf("guest shows host %q, host is %q", guest.HostFingerprint(), host.Fingerprint())
	}

	rogue := mustSession(t, code, true, "host") // same nickname as the real host
	if err := guest.ConnectDirect(fmtHostAddr(rogue.ListenPort())); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	if guest.HostPeerID() != host.PeerID() || guest.HostFingerprint() != host.Fingerprint() {
		t.Fatalf("guest rebound to %q", guest.HostFingerprint())
	}
	if PeerFingerprint("not-a-peer-id") != "" {
		t.Fatal("fingerprint of an invalid ID")
	}
}
