package party

// Regression tests for the party security review (PTY-1 .. PTY-9). Each test
// replays an attack from the review's proof-of-concept and checks it now
// fails. All traffic stays on 127.0.0.1 or in-memory pipes.

import (
	crand "crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func mustSession(t *testing.T, code string, host bool, nick string) *PartySession {
	t.Helper()
	s, err := NewPartySession(SessionConfig{RoomCode: code, RoomName: "r", Nickname: nick, IsHost: host, DJPass: DJPassHostOnly})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// insider is an attacker who knows the room code and completes the handshake
// with its own identity, then writes arbitrary packet fields.
type insider struct {
	m  *MeshNetwork
	pc *TCPPeerConn
}

func dialInsider(t *testing.T, code string, port int) *insider {
	t.Helper()
	m, err := NewMeshNetwork("mallory", code)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	pc, err := m.handshake(conn, true)
	if err != nil {
		_ = conn.Close()
		t.Fatalf("insider handshake: %v", err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	return &insider{m: m, pc: pc}
}

func (in *insider) send(t *testing.T, typ MsgType, senderID string, seq uint64, payload any) error {
	t.Helper()
	var raw json.RawMessage
	if payload != nil {
		raw, _ = json.Marshal(payload)
	}
	data, _ := json.Marshal(&Packet{Type: typ, Seq: seq, SenderID: senderID, SenderNick: "mallory", Timestamp: time.Now().UnixNano(), Payload: raw})
	return in.pc.sendPlain(data)
}

func connectGuest(t *testing.T, guest, host *PartySession) {
	t.Helper()
	if err := guest.ConnectDirect(fmtHostAddr(host.ListenPort())); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool { return guest.HostPeerID() == host.PeerID() })
}

func expectNoSync(t *testing.T, ch <-chan SyncPayload, d time.Duration, what string) {
	t.Helper()
	select {
	case sp := <-ch:
		t.Fatalf("%s: sync was applied: %+v", what, sp)
	case <-time.After(d):
	}
}

func expectSync(t *testing.T, ch <-chan SyncPayload, what string) SyncPayload {
	t.Helper()
	select {
	case sp := <-ch:
		return sp
	case <-time.After(2 * time.Second):
		t.Fatalf("%s: no sync arrived", what)
	}
	return SyncPayload{}
}

var evilSync = SyncPayload{StationID: "x-evil", StationName: "Totally Legit FM", StreamURL: "http://127.0.0.1:9/admin?reboot=1", Status: "playing", DJPass: DJPassHostOnly}

// PTY-1: an insider spoofs the host's sender_id with seq MaxUint64. The packet
// is dropped (the connection is bound to the insider's own identity), the real
// host can still retune the guest, and the host never accepts a sync "from itself".
func TestRegressionSpoofedSenderIDRejected(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	guest := mustSession(t, code, false, "victim")

	got := make(chan SyncPayload, 8)
	guest.SetHandlers(func(sp SyncPayload) { got <- sp }, nil, nil, nil, nil)
	hostGot := make(chan SyncPayload, 8)
	host.SetHandlers(func(sp SyncPayload) { hostGot <- sp }, nil, nil, nil, nil)
	connectGuest(t, guest, host)

	atk := dialInsider(t, code, guest.ListenPort())
	waitFor(t, time.Second, func() bool { return guest.mesh.IsConnected(atk.m.PeerID()) })
	_ = atk.send(t, MsgTypeSync, host.PeerID(), math.MaxUint64, evilSync)
	expectNoSync(t, got, 300*time.Millisecond, "spoofed host sender_id")
	waitFor(t, time.Second, func() bool { return !guest.mesh.IsConnected(atk.m.PeerID()) })

	// Under its own identity the insider is not the host: rejected in Host Only.
	atk2 := dialInsider(t, code, guest.ListenPort())
	_ = atk2.send(t, MsgTypeSync, atk2.m.PeerID(), math.MaxUint64, evilSync)
	expectNoSync(t, got, 300*time.Millisecond, "non-host sync in Host Only")

	// The real host is not locked out.
	if err := host.BroadcastStationChange("somafm", "SomaFM", "https://ice1.somafm.com/groovesalad-128-mp3", "", "", "", 128, "playing"); err != nil {
		t.Fatal(err)
	}
	sp := expectSync(t, got, "real host sync")
	if sp.StreamURL != "https://ice1.somafm.com/groovesalad-128-mp3" || !sp.FromHost {
		t.Fatalf("unexpected sync %+v", sp)
	}

	// Attack the host using the host's own ID as sender.
	atk3 := dialInsider(t, code, host.ListenPort())
	_ = atk3.send(t, MsgTypeSync, host.PeerID(), math.MaxUint64, evilSync)
	expectNoSync(t, hostGot, 300*time.Millisecond, "sync 'from itself' on host")
	if host.CurrentSync().StreamURL != "https://ice1.somafm.com/groovesalad-128-mp3" {
		t.Fatalf("host sync state changed: %+v", host.CurrentSync())
	}
}

// PTY-1: a takeover via HostMigrate from a verified insider while the host is healthy.
func TestRegressionInsiderHostMigrateRejected(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	guest := mustSession(t, code, false, "victim")
	connectGuest(t, guest, host)

	atk := dialInsider(t, code, guest.ListenPort())
	_ = atk.send(t, MsgTypeHostMigrate, atk.m.PeerID(), 1, HostMigratePayload{OldHostID: host.PeerID(), NewHostID: atk.m.PeerID(), NewHostNick: "eve"})
	_ = atk.send(t, MsgTypeLeave, host.PeerID(), 2, nil) // spoofed leave for the host
	time.Sleep(300 * time.Millisecond)
	if guest.HostPeerID() != host.PeerID() || guest.IsHost() {
		t.Fatalf("host changed: host=%q isHost=%v", guest.HostPeerID(), guest.IsHost())
	}
}

// PTY-2: an unsolicited HelloAck on an inbound connection is ignored, and a
// HelloAck from a second "host" never replaces the host a guest follows.
func TestRegressionUnsolicitedHelloAckIgnored(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	guest := mustSession(t, code, false, "victim")
	got := make(chan SyncPayload, 8)
	guest.SetHandlers(func(sp SyncPayload) { got <- sp }, nil, nil, nil, nil)
	connectGuest(t, guest, host)

	atk := dialInsider(t, code, guest.ListenPort())
	evil := evilSync
	_ = atk.send(t, MsgTypeHelloAck, atk.m.PeerID(), 1, HelloAckPayload{
		RoomName: "pwned", HostID: atk.m.PeerID(), HostNick: "mallory", DJPass: DJPassHostOnly, CurrentSync: &evil,
	})
	expectNoSync(t, got, 300*time.Millisecond, "unsolicited hello-ack")
	if guest.HostPeerID() != host.PeerID() || guest.RoomName() == "pwned" {
		t.Fatalf("hello-ack changed state: host=%q room=%q", guest.HostPeerID(), guest.RoomName())
	}

	// A rogue "host" the guest dials (e.g. via a beacon) cannot take over.
	rogue := mustSession(t, code, true, "rogue")
	_ = rogue.BroadcastStationChange("x", "evil", "http://169.254.169.254/latest/meta-data/", "", "", "", 1, "playing")
	if err := guest.ConnectDirect(fmtHostAddr(rogue.ListenPort())); err != nil {
		t.Fatal(err)
	}
	expectNoSync(t, got, 300*time.Millisecond, "second host's hello-ack")
	if guest.HostPeerID() != host.PeerID() {
		t.Fatalf("guest switched host to %q", guest.HostPeerID())
	}

	// The real host still controls the guest.
	_ = host.BroadcastStationChange("somafm", "SomaFM", "https://ice1.somafm.com/groovesalad-128-mp3", "", "", "", 128, "playing")
	expectSync(t, got, "real host after hello-ack attacks")
}

// rawListener accepts one v2 connection, reads the Hello and answers with ack.
func rawListener(t *testing.T, code string, ack func(m *MeshNetwork) HelloAckPayload) (*MeshNetwork, string) {
	t.Helper()
	m, err := NewMeshNetwork("rogue", code)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		pc, err := m.handshake(conn, false)
		if err != nil {
			_ = conn.Close()
			return
		}
		t.Cleanup(func() { _ = pc.Close() })
		if b, err := readFrame(conn, maxDataFrame); err == nil {
			_, _ = pc.open(b) // Hello
		}
		payload, _ := json.Marshal(ack(m))
		data, _ := json.Marshal(&Packet{Type: MsgTypeHelloAck, Seq: 1, SenderID: m.PeerID(), SenderNick: "rogue", Payload: payload})
		_ = pc.sendPlain(data)
		// keep the connection open
		_, _ = readFrame(conn, maxDataFrame)
	}()
	return m, ln.Addr().String()
}

// PTY-2: an unknown DJ Pass value fails closed.
func TestRegressionBogusDJPassRejected(t *testing.T) {
	code := GenerateRoomCode()
	guest := mustSession(t, code, false, "victim")
	got := make(chan SyncPayload, 8)
	guest.SetHandlers(func(sp SyncPayload) { got <- sp }, nil, nil, nil, nil)

	_, addr := rawListener(t, code, func(m *MeshNetwork) HelloAckPayload {
		return HelloAckPayload{HostID: m.PeerID(), HostNick: "rogue", DJPass: "bogus",
			CurrentSync: &SyncPayload{StationID: "y", StreamURL: "http://10.0.0.1/", Status: "playing", DJPass: "bogus"}}
	})
	if err := guest.ConnectDirect(addr); err != nil {
		t.Fatal(err)
	}
	expectNoSync(t, got, 300*time.Millisecond, "bogus dj_pass hello-ack")
	if guest.DJPass() != DJPassHostOnly || guest.HostPeerID() != "" {
		t.Fatalf("bogus hello-ack applied: djpass=%q host=%q", guest.DJPass(), guest.HostPeerID())
	}

	// A legitimate host sync with an unknown DJ Pass is rejected too.
	code2 := GenerateRoomCode()
	host := mustSession(t, code2, true, "host")
	guest2 := mustSession(t, code2, false, "victim2")
	got2 := make(chan SyncPayload, 8)
	guest2.SetHandlers(func(sp SyncPayload) { got2 <- sp }, nil, nil, nil, nil)
	connectGuest(t, guest2, host)
	guest2.applySync(&SyncPayload{StreamURL: "https://a.example/s", Status: "playing", DJPass: "bogus"}, host.PeerID(), 1<<40)
	expectNoSync(t, got2, 100*time.Millisecond, "host sync with bogus dj_pass")
}

// PTY-6: roster entries from a host are capped, never verified, never
// candidates in an election, and peer JoinedAt values are ignored.
func TestRegressionRosterHintsBounded(t *testing.T) {
	code := GenerateRoomCode()
	guest := mustSession(t, code, false, "victim")

	_, addr := rawListener(t, code, func(m *MeshNetwork) HelloAckPayload {
		peers := make([]PeerRosterItem, 0, 600) // about 50 KiB, under the frame cap
		for i := 0; i < 600; i++ {
			peers = append(peers, PeerRosterItem{ID: fmt.Sprintf("%032x", i), Nickname: "n", JoinedAt: 1})
		}
		peers = append(peers, PeerRosterItem{ID: "not-a-peer-id", Nickname: "x"})
		return HelloAckPayload{HostID: m.PeerID(), HostNick: "rogue", DJPass: DJPassHostOnly, Peers: peers}
	})
	if err := guest.ConnectDirect(addr); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, func() bool { return guest.HostPeerID() != "" })

	guest.mu.RLock()
	defer guest.mu.RUnlock()
	hints := 0
	for _, p := range guest.peers {
		if !p.Verified {
			hints++
			if p.JoinedAt.Before(time.Now().Add(-time.Minute)) {
				t.Errorf("peer-supplied JoinedAt was used: %v", p.JoinedAt)
			}
		}
	}
	if hints > maxRosterHints || len(guest.peers) > maxRosterHints+2 {
		t.Fatalf("roster not bounded: %d entries, %d hints", len(guest.peers), hints)
	}
	if len(guest.succession) > MaxPeersLimit+1 {
		t.Fatalf("succession not bounded: %d", len(guest.succession))
	}
	// Hints rank first in succession, yet the election only considers
	// verified, connected peers: the guest elects itself.
	if w := guest.electionWinnerLocked(guest.hostPeerID); w != guest.PeerID() {
		t.Fatalf("election picked unverified hint %q", w)
	}
}

// PTY-6: a HelloAck flood sent *to a host* (inbound connection) is ignored and
// a later guest can still join.
func TestRegressionHelloAckToHostIgnored(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	atk := dialInsider(t, code, host.ListenPort())
	peers := make([]PeerRosterItem, 0, 1000)
	for i := 0; i < 1000; i++ {
		peers = append(peers, PeerRosterItem{ID: fmt.Sprintf("%032x", i), Nickname: "n"})
	}
	_ = atk.send(t, MsgTypeHelloAck, atk.m.PeerID(), 1, HelloAckPayload{HostID: host.PeerID(), DJPass: DJPassHostOnly, Peers: peers})
	time.Sleep(200 * time.Millisecond)
	if n := host.PeerCount(); n > 2 {
		t.Fatalf("host roster grew to %d", n)
	}
	guest := mustSession(t, code, false, "newguest")
	connectGuest(t, guest, host)
}

// PTY-4: frames captured from one handshake cannot be reflected into another.
// A keyless attacker lures the guest into dialing it, records the guest's
// auth frame and replays it to the host and to the guest's own listener.
func TestRegressionHandshakeReflectionFails(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	guest := mustSession(t, code, false, "victim")
	got := make(chan SyncPayload, 8)
	guest.SetHandlers(func(sp SyncPayload) { got <- sp }, nil, nil, nil, nil)
	connectGuest(t, guest, host)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	type capture struct{ pre, auth []byte }
	capCh := make(chan capture, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		pre, err := readFrame(c, maxHandshakeFrame)
		if err != nil {
			return
		}
		// Keyless: answer with a cleartext preamble, our own nonce and ephemeral key.
		eph, _ := x25519.GenerateKey(crand.Reader)
		_ = writePreamble(c, preamble{Proto: protocolName, Version: ProtocolVersion, Nonce: make([]byte, 32), Eph: eph.PublicKey().Bytes()})
		auth, err := readFrame(c, maxHandshakeFrame)
		if err != nil {
			return
		}
		capCh <- capture{pre, auth}
	}()
	go func() { _, _ = guest.mesh.connectPeer(ln.Addr().String(), "") }()

	var cp capture
	select {
	case cp = <-capCh:
	case <-time.After(3 * time.Second):
		t.Fatal("guest did not dial the attacker")
	}

	replay := func(port int) {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		_ = writeFrame(c, cp.pre)
		if _, err := readFrame(c, maxHandshakeFrame); err != nil {
			return
		}
		_ = writeFrame(c, cp.auth)
		_ = c.SetReadDeadline(time.Now().Add(time.Second))
		b, err := readFrame(c, maxHandshakeFrame)
		if err == nil {
			if p, perr := parsePreamble(b); perr != nil || p.Error == "" {
				t.Fatalf("replayed auth was answered with a non-error frame")
			}
		}
	}
	before := host.mesh.PeerCount()
	replay(host.ListenPort())
	replay(guest.ListenPort())
	time.Sleep(200 * time.Millisecond)
	if host.mesh.PeerCount() != before {
		t.Fatalf("host accepted a replayed handshake")
	}
	if guest.IsHost() || guest.HostPeerID() != host.PeerID() {
		t.Fatalf("room split: guest isHost=%v host=%q", guest.IsHost(), guest.HostPeerID())
	}
	_ = host.BroadcastStationChange("somafm", "SomaFM", "https://ice1.somafm.com/groovesalad-128-mp3", "", "", "", 128, "playing")
	expectSync(t, got, "host after reflection attempt")
}

// PTY-5: unauthenticated sockets are capped (total and per IP) and closed at
// the handshake deadline.
func TestRegressionUnauthenticatedConnectionCaps(t *testing.T) {
	m, err := NewMeshNetwork("victim", GenerateRoomCode())
	if err != nil {
		t.Fatal(err)
	}
	m.handshakeTimeout = 300 * time.Millisecond
	if err := m.StartListening(0); err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	var maxPending atomic.Int64
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			m.mu.RLock()
			n := int64(m.pendingTotal)
			perIP := int64(0)
			for _, v := range m.pendingPerIP {
				if int64(v) > perIP {
					perIP = int64(v)
				}
			}
			m.mu.RUnlock()
			if n > maxPending.Load() {
				maxPending.Store(n)
			}
			if perIP > maxPendingPerIP {
				maxPending.Store(1 << 20)
			}
			time.Sleep(time.Millisecond)
		}
	}()

	conns := make([]net.Conn, 0, 200)
	for i := 0; i < 200; i++ {
		c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", m.ListenPort()), time.Second)
		if err != nil {
			continue
		}
		conns = append(conns, c)
	}
	time.Sleep(1200 * time.Millisecond)
	close(stop)

	open := 0
	for _, c := range conns {
		_ = c.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
		var b [1]byte
		_, err := c.Read(b[:])
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			open++
		}
		_ = c.Close()
	}
	if open != 0 {
		t.Fatalf("%d/200 unauthenticated connections still open after the handshake deadline", open)
	}
	if mp := maxPending.Load(); mp > maxPendingHandshakes || mp > maxPendingPerIP {
		t.Fatalf("pending handshakes reached %d (caps: total %d, per IP %d)", mp, maxPendingHandshakes, maxPendingPerIP)
	}
}

// PTY-5: a verified peer flooding chat is rate limited and then disconnected.
func TestRegressionChatFloodRateLimited(t *testing.T) {
	code := GenerateRoomCode()
	victim := mustSession(t, code, true, "victim")
	var chats atomic.Int64
	victim.SetHandlers(nil, nil, func(ChatMessage) { chats.Add(1) }, nil, nil)

	atk := dialInsider(t, code, victim.ListenPort())
	waitFor(t, time.Second, func() bool { return victim.mesh.IsConnected(atk.m.PeerID()) })
	for i := 1; i <= 500; i++ {
		if err := atk.send(t, MsgTypeChat, atk.m.PeerID(), uint64(i), ChatPayload{Message: "spam"}); err != nil {
			break
		}
	}
	waitFor(t, 2*time.Second, func() bool { return !victim.mesh.IsConnected(atk.m.PeerID()) })
	if n := chats.Load(); n > socialBurst+2 {
		t.Fatalf("%d chat messages delivered from a flood (burst %d)", n, socialBurst)
	}
}

// pipeHandshake runs a handshake between two meshes over an in-memory pipe.
func pipeHandshake(t *testing.T, dialer, listener *MeshNetwork) (*TCPPeerConn, *TCPPeerConn, error, error) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	type res struct {
		pc  *TCPPeerConn
		err error
	}
	ch := make(chan res, 1)
	go func() {
		pc, err := listener.handshake(b, false)
		if err != nil {
			_ = b.Close()
		}
		ch <- res{pc, err}
	}()
	dpc, derr := dialer.handshake(a, true)
	if derr != nil {
		_ = a.Close()
	}
	r := <-ch
	return dpc, r.pc, derr, r.err
}

// PTY-7: frames are bound to one connection and one position in its stream:
// a replayed frame, or a frame from another connection, does not open.
func TestRegressionFrameReplayRejected(t *testing.T) {
	code := GenerateRoomCode()
	alice, _ := NewMeshNetwork("alice", code)
	bob, _ := NewMeshNetwork("bob", code)

	d1, l1, err1, err2 := pipeHandshake(t, alice, bob)
	if err1 != nil || err2 != nil {
		t.Fatalf("handshake: %v / %v", err1, err2)
	}
	if d1.RemotePeerID() != bob.PeerID() || l1.RemotePeerID() != alice.PeerID() {
		t.Fatalf("identities not bound to the connection")
	}
	d1.mu.Lock()
	f1 := d1.sealLocked([]byte("one"))
	f2 := d1.sealLocked([]byte("two"))
	d1.mu.Unlock()

	if pt, err := l1.open(f1); err != nil || string(pt) != "one" {
		t.Fatalf("first frame: %q %v", pt, err)
	}
	if _, err := l1.open(f1); err == nil {
		t.Fatal("replayed frame was accepted")
	}
	if pt, err := l1.open(f2); err != nil || string(pt) != "two" {
		t.Fatalf("second frame: %q %v", pt, err)
	}

	// Same two peers, new connection: old frames do not open.
	_, l2, err1, err2 := pipeHandshake(t, alice, bob)
	if err1 != nil || err2 != nil {
		t.Fatalf("second handshake: %v / %v", err1, err2)
	}
	if _, err := l2.open(f1); err == nil {
		t.Fatal("frame from another connection was accepted")
	}
}

func TestHandshakeWrongRoomCode(t *testing.T) {
	alice, _ := NewMeshNetwork("alice", GenerateRoomCode())
	bob, _ := NewMeshNetwork("bob", GenerateRoomCode())
	_, _, derr, lerr := pipeHandshake(t, alice, bob)
	if !errors.Is(derr, ErrWrongRoomCode) {
		t.Fatalf("dialer error = %v, want ErrWrongRoomCode", derr)
	}
	if lerr == nil {
		t.Fatal("listener accepted a peer with another room code")
	}
}

// Decision 1: v1 and v2 reject each other with a readable error.
func TestHandshakeVersionMismatch(t *testing.T) {
	m, _ := NewMeshNetwork("alice", GenerateRoomCode())

	// A v1 peer cannot parse the cleartext preamble and drops the connection.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_, _ = readFrame(c, maxDataFrame)
		_ = c.Close()
	}()
	_, err = m.ConnectPeer(ln.Addr().String())
	if !errors.Is(err, ErrPeerClosedHandshake) || !strings.Contains(err.Error(), "older halpradio") {
		t.Fatalf("v1 peer: got %v", err)
	}

	// A peer announcing another version gets a VersionMismatchError.
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln2.Close()
	go func() {
		c, err := ln2.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = readFrame(c, maxHandshakeFrame)
		_ = writePreamble(c, preamble{Proto: protocolName, Version: 3, Nonce: make([]byte, 32)})
	}()
	_, err = m.ConnectPeer(ln2.Addr().String())
	var vm *VersionMismatchError
	if !errors.As(err, &vm) || vm.Remote != 3 || !strings.Contains(err.Error(), "v3") {
		t.Fatalf("version mismatch: got %v", err)
	}

	// A v1 frame (encrypted packet, not a preamble) sent to a v2 listener is refused.
	b, _ := NewMeshNetwork("bob", m.RoomCode())
	a, c := net.Pipe()
	defer a.Close()
	go func() { _ = writeFrame(a, []byte{0x01, 0x02, 0x03, 0x04, 0x05}) }()
	if _, err := b.handshake(c, false); err == nil {
		t.Fatal("v2 listener accepted a v1 frame")
	}
}

// PTY-4: beacons are bound to sender address, port, identity and room, are
// short-lived and single-use, and only LAN sources are dialed.
func TestRegressionBeaconVerification(t *testing.T) {
	code := GenerateRoomCode()
	a, _ := NewMeshNetwork("a", code)
	b, _ := NewMeshNetwork("b", code)
	other, _ := NewMeshNetwork("o", GenerateRoomCode())
	now := time.Now()
	lo := net.ParseIP("127.0.0.1")
	src := &net.UDPAddr{IP: lo, Port: 50000}

	msg, err := a.buildBeacon(lo, 4242, now)
	if err != nil {
		t.Fatal(err)
	}
	id, target, err := b.verifyBeacon([]byte(msg), src, now)
	if err != nil || id != a.PeerID() || target != "127.0.0.1:4242" {
		t.Fatalf("valid beacon: id=%q target=%q err=%v", id, target, err)
	}
	if _, _, err := b.verifyBeacon([]byte(msg), src, now); err == nil {
		t.Error("replayed beacon accepted")
	}

	fresh := func() string {
		m, _ := a.buildBeacon(lo, 4242, now)
		return m
	}
	// Re-pointed at another address (source mismatch).
	if _, _, err := b.verifyBeacon([]byte(fresh()), &net.UDPAddr{IP: net.ParseIP("192.168.1.66"), Port: 1}, now); err == nil {
		t.Error("beacon with mismatching source accepted")
	}
	// Tampered port.
	tampered := strings.Replace(fresh(), "|4242|", "|4243|", 1)
	if _, _, err := b.verifyBeacon([]byte(tampered), src, now); err == nil {
		t.Error("beacon with tampered port accepted")
	}
	// Tampered peer ID.
	parts := strings.Split(fresh(), "|")
	parts[1] = b.PeerID()
	if _, _, err := other.verifyBeacon([]byte(strings.Join(parts, "|")), src, now); err == nil {
		t.Error("beacon with swapped identity accepted")
	}
	// Stale.
	if _, _, err := b.verifyBeacon([]byte(fresh()), src, now.Add(2*beaconMaxAge)); err == nil {
		t.Error("stale beacon accepted")
	}
	// Another room.
	if _, _, err := other.verifyBeacon([]byte(fresh()), src, now); err == nil {
		t.Error("beacon from another room accepted")
	}
	// Non-LAN source, even with a valid MAC over that address.
	pub := net.ParseIP("8.8.8.8")
	pm, _ := a.buildBeacon(pub, 4242, now)
	if _, _, err := b.verifyBeacon([]byte(pm), &net.UDPAddr{IP: pub, Port: 1}, now); err == nil {
		t.Error("beacon from a public address accepted")
	}
	// Own beacon.
	if _, _, err := a.verifyBeacon([]byte(fresh()), src, now); err == nil {
		t.Error("own beacon accepted")
	}
	// Legacy v1 beacon format.
	if _, _, err := b.verifyBeacon([]byte("HALPRADIO_BEACON:abc:not-a-real-peer:4242"), src, now); err == nil {
		t.Error("v1 beacon accepted")
	}
}

// PTY-4: a beacon-triggered dial is dropped unless the peer proves the
// identity the beacon announced.
func TestRegressionBeaconDialRequiresAnnouncedIdentity(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	m, _ := NewMeshNetwork("g", code)
	defer m.Close()
	_, err := m.connectPeer(fmtHostAddr(host.ListenPort()), strings.Repeat("ab", 16))
	if err == nil {
		t.Fatal("dial accepted a peer whose identity differs from the beacon")
	}
	if m.PeerCount() != 0 {
		t.Fatal("mismatching peer was registered")
	}
}

// PTY-1 / decision 7: only http(s) stream URLs are applied, and FromHost
// reports whether a sync came from the verified host.
func TestRegressionSyncStreamURLAndOrigin(t *testing.T) {
	code := GenerateRoomCode()
	host := mustSession(t, code, true, "host")
	guest := mustSession(t, code, false, "victim")
	got := make(chan SyncPayload, 8)
	guest.SetHandlers(func(sp SyncPayload) { got <- sp }, nil, nil, nil, nil)
	connectGuest(t, guest, host)

	if err := host.BroadcastStationChange("x", "x", "file:///etc/passwd", "", "", "", 1, "playing"); err == nil {
		t.Error("host could broadcast a file:// URL")
	}
	for i, u := range []string{"file:///etc/passwd", "gopher://x/", "http://", "javascript:alert(1)"} {
		guest.applySync(&SyncPayload{StreamURL: u, Status: "playing", DJPass: DJPassHostOnly}, host.PeerID(), uint64(1<<40+i))
	}
	expectNoSync(t, got, 100*time.Millisecond, "non-http stream URL")

	// Open Democracy: a verified guest may sync, but it is not marked as host
	// and cannot change the DJ Pass.
	if err := host.BroadcastStationChange("somafm", "SomaFM", "https://ice1.somafm.com/groovesalad-128-mp3", "", "", "", 128, "playing"); err != nil {
		t.Fatal(err)
	}
	expectSync(t, got, "host sync")
	if _, err := host.ToggleDJPass(); err != nil {
		t.Fatal(err)
	}
	sp := expectSync(t, got, "dj pass toggle")
	if !sp.FromHost || guest.DJPass() != DJPassOpenDemocracy {
		t.Fatalf("toggle not applied: %+v djpass=%q", sp, guest.DJPass())
	}

	atk := dialInsider(t, code, guest.ListenPort())
	waitFor(t, time.Second, func() bool { return guest.mesh.IsConnected(atk.m.PeerID()) })
	_ = atk.send(t, MsgTypeSync, atk.m.PeerID(), 5, SyncPayload{StationID: "g", StreamURL: "https://radio.example/live", Status: "playing", DJPass: DJPassHostOnly})
	sp = expectSync(t, got, "open democracy guest sync")
	if sp.FromHost || sp.FromPeerID != atk.m.PeerID() {
		t.Fatalf("guest sync marked as host: %+v", sp)
	}
	if guest.DJPass() != DJPassOpenDemocracy {
		t.Fatalf("non-host changed DJ Pass to %q", guest.DJPass())
	}
}

// Legacy 6-character codes are refused with a readable error.
func TestNewPartySessionRejectsLegacyCode(t *testing.T) {
	_, err := NewPartySession(SessionConfig{RoomCode: "8X2K9P"})
	if !errors.Is(err, ErrLegacyRoomCode) {
		t.Fatalf("got %v, want ErrLegacyRoomCode", err)
	}
}
