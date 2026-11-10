package party

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Connection limits. See PTY-5 in the security review.
const (
	// MaxPeersLimit caps authenticated peers per mesh.
	MaxPeersLimit = 32
	// maxPendingHandshakes caps inbound sockets that have not finished the
	// handshake yet, across all sources.
	maxPendingHandshakes = 64
	// maxPendingPerIP caps unauthenticated sockets from one source address.
	maxPendingPerIP = 4

	maxHandshakeFrame = 4096
	maxDataFrame      = 65536
	idleReadTimeout   = 15 * time.Second
	writeTimeout      = 5 * time.Second

	// Per-connection inbound packet rate limit (after the handshake).
	connRatePerSec = 20
	connRateBurst  = 40
	// A connection that keeps flooding past the limit is dropped.
	connMaxDropped = 3 * connRateBurst

	beaconPort       = 54242
	beaconInterval   = 2 * time.Second
	beaconMaxAge     = 30 * time.Second
	beaconCacheMax   = 4096
	dialDebounceTime = 5 * time.Second
	dialDebounceMax  = 256
)

// defaultHandshakeTimeout bounds the whole handshake on one connection.
const defaultHandshakeTimeout = 5 * time.Second

// Handshake errors shown to users.
var (
	ErrPeerClosedHandshake = errors.New("the peer closed the connection during the handshake: " +
		"it may run an older halpradio (party protocol v1), which cannot join v2 rooms — update halpradio on both sides")
	ErrNotPartyPeer = errors.New("the address did not answer as a halpradio party peer " +
		"(it may run an older halpradio with party protocol v1 — update halpradio on both sides)")
	ErrWrongRoomCode = errors.New("handshake failed: the peer is in a different room (check the room code)")
	ErrRoomFull      = errors.New("the room is full")
)

// VersionMismatchError reports a peer that speaks a different protocol version.
type VersionMismatchError struct {
	Local, Remote int
}

func (e *VersionMismatchError) Error() string {
	return fmt.Sprintf("the peer speaks party protocol v%d but this halpradio speaks v%d: "+
		"everyone in the room needs the same halpradio release", e.Remote, e.Local)
}

// PeerConn is an authenticated connection to another peer in the party mesh.
// RemotePeerID is the identity proven in the handshake and never changes.
type PeerConn interface {
	RemotePeerID() string
	RemoteAddr() string
	// Outbound reports whether this side dialed the connection.
	Outbound() bool
	Close() error

	sendPlain(plaintext []byte) error
	markHelloSent()
	consumeHelloAck() bool
	markHelloReceived() bool
}

// TCPPeerConn implements PeerConn over a framed TCP socket protected by
// per-connection, per-direction AES-256-GCM keys with counter nonces.
//
// Outbound frames go through a bounded queue drained by one writer goroutine,
// so a peer that stops reading never blocks the sender (RR-C-3). The writer is
// the only goroutine that seals and writes after the handshake, which keeps
// frame order equal to counter order.
type TCPPeerConn struct {
	conn     net.Conn
	outbound bool

	// Set by the handshake before the connection is published; read-only afterwards.
	remoteID   string
	remotePub  ed25519.PublicKey
	remoteNick string
	remotePort int

	mu       sync.Mutex // guards send state and closed
	sendAEAD cipher.AEAD
	sendCtr  uint64
	closed   bool

	recvAEAD cipher.AEAD // used only by the reader goroutine
	recvCtr  uint64

	sendQ   chan []byte
	done    chan struct{}
	pending atomic.Int64 // queued or in-flight frames

	helloMu          sync.Mutex
	helloSent        bool
	helloAckConsumed bool
	helloReceived    bool
}

// sendQueueLen bounds the frames queued for one peer. A peer whose queue is
// full is not reading and is disconnected.
const sendQueueLen = 64

// ErrSendQueueFull is returned when a peer is too slow to keep up and is dropped.
var ErrSendQueueFull = errors.New("peer is not reading: send queue full, connection dropped")

func newTCPPeerConn(conn net.Conn, outbound bool) *TCPPeerConn {
	return &TCPPeerConn{
		conn:     conn,
		outbound: outbound,
		sendQ:    make(chan []byte, sendQueueLen),
		done:     make(chan struct{}),
	}
}

// startWriter launches the goroutine that seals and writes queued frames.
func (c *TCPPeerConn) startWriter() {
	go c.writeLoop()
}

func (c *TCPPeerConn) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case pt := <-c.sendQ:
			err := c.writeNow(pt)
			c.pending.Add(-1)
			if err != nil {
				_ = c.Close()
				return
			}
		}
	}
}

func (c *TCPPeerConn) writeNow(pt []byte) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("connection closed")
	}
	ct := c.sealLocked(pt)
	c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	return writeFrame(c.conn, ct)
}

// flush waits up to d for queued frames to be written.
func (c *TCPPeerConn) flush(d time.Duration) {
	deadline := time.Now().Add(d)
	for c.pending.Load() > 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
}

func (c *TCPPeerConn) RemotePeerID() string { return c.remoteID }

func (c *TCPPeerConn) RemoteAddr() string {
	if c.conn == nil {
		return ""
	}
	return c.conn.RemoteAddr().String()
}

func (c *TCPPeerConn) Outbound() bool { return c.outbound }

func (c *TCPPeerConn) markHelloSent() {
	c.helloMu.Lock()
	c.helloSent = true
	c.helloMu.Unlock()
}

// consumeHelloAck returns true exactly once, and only on a connection this
// side dialed and sent a Hello on.
func (c *TCPPeerConn) consumeHelloAck() bool {
	c.helloMu.Lock()
	defer c.helloMu.Unlock()
	if !c.outbound || !c.helloSent || c.helloAckConsumed {
		return false
	}
	c.helloAckConsumed = true
	return true
}

// markHelloReceived returns true the first time a Hello arrives on an inbound connection.
func (c *TCPPeerConn) markHelloReceived() bool {
	c.helloMu.Lock()
	defer c.helloMu.Unlock()
	if c.outbound || c.helloReceived {
		return false
	}
	c.helloReceived = true
	return true
}

var frameAAD = []byte("halpradio-party-v2/frame")

func counterNonce(ctr uint64, size int) []byte {
	n := make([]byte, size)
	binary.BigEndian.PutUint64(n[size-8:], ctr)
	return n
}

// seal encrypts plaintext with the next send counter. Caller holds c.mu.
func (c *TCPPeerConn) sealLocked(plaintext []byte) []byte {
	nonce := counterNonce(c.sendCtr, c.sendAEAD.NonceSize())
	c.sendCtr++
	return c.sendAEAD.Seal(nil, nonce, plaintext, frameAAD)
}

// open decrypts a frame with the next expected receive counter. A replayed,
// reordered, dropped or forged frame fails authentication.
func (c *TCPPeerConn) open(frame []byte) ([]byte, error) {
	nonce := counterNonce(c.recvCtr, c.recvAEAD.NonceSize())
	pt, err := c.recvAEAD.Open(nil, nonce, frame, frameAAD)
	if err != nil {
		return nil, err
	}
	c.recvCtr++
	return pt, nil
}

// sendPlain queues plaintext for the writer goroutine without blocking. If the
// queue is full the peer is not reading, and the connection is closed.
func (c *TCPPeerConn) sendPlain(plaintext []byte) error {
	c.mu.Lock()
	if c.closed || c.conn == nil || c.sendAEAD == nil {
		c.mu.Unlock()
		return errors.New("connection closed")
	}
	c.pending.Add(1)
	select {
	case c.sendQ <- plaintext:
		c.mu.Unlock()
		return nil
	default:
		c.pending.Add(-1)
		c.mu.Unlock()
		_ = c.Close()
		return ErrSendQueueFull
	}
}

func (c *TCPPeerConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.done != nil {
		close(c.done)
	}
	// Frames still queued are discarded; release their flush accounting.
	if c.sendQ != nil {
	drain:
		for {
			select {
			case <-c.sendQ:
				c.pending.Add(-1)
			default:
				break drain
			}
		}
	}
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func writeFrame(w io.Writer, body []byte) error {
	buf := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(buf[:4], uint32(len(body)))
	copy(buf[4:], body)
	_, err := w.Write(buf)
	return err
}

func readFrame(r io.Reader, max int) ([]byte, error) {
	var lenBuf [4]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(lenBuf[:])
	if n == 0 || n > uint32(max) {
		return nil, fmt.Errorf("frame length %d out of range", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

// MeshNetwork manages discovery, authenticated connections and message routing.
// Packets are never relayed: each packet is delivered only to directly
// connected peers, so the verified identity of the connection is the origin.
type MeshNetwork struct {
	mu          sync.RWMutex
	peerID      string
	pub         ed25519.PublicKey
	priv        ed25519.PrivateKey
	nickname    string
	roomCode    string
	roomKey     []byte
	binding     []byte
	beaconKey   []byte
	listenPort  int
	tcpListener net.Listener
	peers       map[string]PeerConn
	seq         SequenceCounter
	closed      bool

	pendingTotal     int
	pendingPerIP     map[string]int
	handshakeTimeout time.Duration

	onPacket     func(pkt *Packet, from PeerConn)
	onPeerJoined func(peerID string, nickname string)
	onPeerLeft   func(peerID string)
	autoDial     func() bool

	beaconCancel context.CancelFunc

	dialDebounceMu sync.Mutex
	dialDebounce   map[string]time.Time

	beaconSeenMu sync.Mutex
	beaconSeen   map[string]time.Time
}

// NewMeshNetwork creates a mesh for a v2 room code. Each mesh gets a fresh
// ed25519 identity; its PeerID is derived from the public key.
func NewMeshNetwork(nickname, roomCode string) (*MeshNetwork, error) {
	if err := ValidateRoomCode(roomCode); err != nil {
		return nil, err
	}
	normCode := NormalizeRoomCode(roomCode)

	key, err := DeriveRoomKey(normCode)
	if err != nil {
		return nil, err
	}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generating party identity: %w", err)
	}
	peerID := PeerIDFromPublicKey(pub)

	nick := SanitizeString(nickname, 32)
	if nick == "" {
		nick = "dj-" + peerID[:4]
	}

	m := &MeshNetwork{
		peerID:           peerID,
		pub:              pub,
		priv:             priv,
		nickname:         nick,
		roomCode:         normCode,
		roomKey:          key,
		binding:          labelKey(key, "halpradio-party-v2/room-binding"),
		beaconKey:        labelKey(key, "halpradio-party-v2/beacon-key"),
		peers:            make(map[string]PeerConn),
		pendingPerIP:     make(map[string]int),
		handshakeTimeout: defaultHandshakeTimeout,
		dialDebounce:     make(map[string]time.Time),
		beaconSeen:       make(map[string]time.Time),
	}
	return m, nil
}

func (m *MeshNetwork) PeerID() string {
	return m.peerID
}

func (m *MeshNetwork) Nickname() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.nickname
}

func (m *MeshNetwork) SetNickname(nick string) {
	clean := SanitizeString(nick, 32)
	if clean == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nickname = clean
}

func (m *MeshNetwork) RoomCode() string {
	return m.roomCode
}

func (m *MeshNetwork) ListenPort() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.listenPort
}

func (m *MeshNetwork) SetPacketHandler(fn func(pkt *Packet, from PeerConn)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onPacket = fn
}

func (m *MeshNetwork) SetPeerJoinedHandler(fn func(peerID string, nickname string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onPeerJoined = fn
}

func (m *MeshNetwork) SetPeerLeftHandler(fn func(peerID string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onPeerLeft = fn
}

// SetAutoDialPolicy sets a predicate consulted before dialing a peer found by
// a LAN beacon. A nil predicate allows auto-dial.
func (m *MeshNetwork) SetAutoDialPolicy(fn func() bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.autoDial = fn
}

// StartListening starts a TCP listener on the specified port (or 0 for system-assigned).
func (m *MeshNetwork) StartListening(port int) error {
	addr := fmt.Sprintf("0.0.0.0:%d", port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	actualPort := l.Addr().(*net.TCPAddr).Port
	m.mu.Lock()
	m.tcpListener = l
	m.listenPort = actualPort
	m.mu.Unlock()

	go m.acceptLoop(l)
	return nil
}

func remoteIP(conn net.Conn) string {
	if a, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
		return a.IP.String()
	}
	host, _, err := net.SplitHostPort(conn.RemoteAddr().String())
	if err != nil {
		return conn.RemoteAddr().String()
	}
	return host
}

func (m *MeshNetwork) acceptLoop(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			m.mu.RLock()
			closed := m.closed
			m.mu.RUnlock()
			if closed {
				return
			}
			time.Sleep(50 * time.Millisecond)
			continue
		}

		ip := remoteIP(conn)
		m.mu.Lock()
		if m.closed || m.pendingTotal >= maxPendingHandshakes ||
			m.pendingPerIP[ip] >= maxPendingPerIP || len(m.peers) >= MaxPeersLimit {
			m.mu.Unlock()
			_ = conn.Close()
			continue
		}
		m.pendingTotal++
		m.pendingPerIP[ip]++
		m.mu.Unlock()

		go m.handleIncomingTCP(conn, ip)
	}
}

func (m *MeshNetwork) releasePending(ip string) {
	m.mu.Lock()
	m.pendingTotal--
	if m.pendingPerIP[ip] <= 1 {
		delete(m.pendingPerIP, ip)
	} else {
		m.pendingPerIP[ip]--
	}
	m.mu.Unlock()
}

func (m *MeshNetwork) handleIncomingTCP(conn net.Conn, ip string) {
	pc, err := m.handshake(conn, false)
	m.releasePending(ip)
	if err != nil {
		_ = conn.Close()
		return
	}
	if err := m.registerPeer(pc); err != nil {
		_ = pc.Close()
		return
	}
	m.readLoop(pc)
}

// ConnectPeer dials another peer (e.g. "192.168.1.50:4242"), runs the
// authenticated handshake and sends a Hello. It returns once the peer has
// proven its identity, or with a user-readable error.
func (m *MeshNetwork) ConnectPeer(addr string) (PeerConn, error) {
	return m.connectPeer(addr, "")
}

func (m *MeshNetwork) connectPeer(addr, expectedID string) (PeerConn, error) {
	conn, err := net.DialTimeout("tcp", addr, 4*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to dial peer %s: %w", addr, err)
	}

	pc, err := m.handshake(conn, true)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if expectedID != "" && pc.remoteID != expectedID {
		_ = pc.Close()
		return nil, errors.New("peer identity does not match its beacon")
	}
	if err := m.registerPeer(pc); err != nil {
		_ = pc.Close()
		return nil, err
	}

	payload, _ := json.Marshal(HelloPayload{
		ProtocolVersion: ProtocolVersion,
		ListenPort:      m.ListenPort(),
	})
	pc.markHelloSent()
	if err := m.SendDirect(pc, &Packet{Type: MsgTypeHello, Payload: payload}); err != nil {
		_ = pc.Close()
		m.removePeer(pc.remoteID, pc)
		return nil, err
	}

	go m.readLoop(pc)
	return pc, nil
}

// --- handshake ---------------------------------------------------------------

type preamble struct {
	Proto   string `json:"proto"`
	Version int    `json:"v"`
	Nonce   []byte `json:"nonce,omitempty"`
	// Eph is a fresh X25519 public key for this connection only.
	Eph   []byte `json:"eph,omitempty"`
	Error string `json:"error,omitempty"`
}

// identityMsg carries a peer's long-term (per-session) identity. Sig is empty
// in the dialer's first message and set in every other handshake message.
type identityMsg struct {
	Version    int    `json:"v"`
	PubKey     []byte `json:"pub,omitempty"`
	Sig        []byte `json:"sig,omitempty"`
	Nick       string `json:"nick,omitempty"`
	ListenPort int    `json:"listen_port,omitempty"`
}

const (
	roleDialer   = "dialer"
	roleListener = "listener"
)

var x25519 = ecdh.X25519()

// transcriptHash binds the handshake to the protocol label and version, the
// room, both fresh nonces and both ephemeral X25519 public keys.
func (m *MeshNetwork) transcriptHash(nonceD, nonceL, ephD, ephL []byte) []byte {
	h := sha256.New()
	h.Write([]byte("halpradio-party-v2/transcript|"))
	h.Write([]byte(strconv.Itoa(ProtocolVersion)))
	h.Write([]byte("|"))
	for _, part := range [][]byte{m.binding, nonceD, nonceL, ephD, ephL} {
		var l [4]byte
		binary.BigEndian.PutUint32(l[:], uint32(len(part)))
		h.Write(l[:])
		h.Write(part)
	}
	return h.Sum(nil)
}

// sessionKeys derives the two direction keys from the room key, the X25519
// shared secret and the transcript. Both the room code and one side's
// ephemeral private key are needed to compute them.
func sessionKeys(roomKey, shared, th []byte) (d2l, l2d []byte) {
	prk := labelKey(roomKey, "halpradio-party-v2/handshake-secret|", shared, th)
	return labelKey(prk, "halpradio-party-v2/key|dialer->listener"),
		labelKey(prk, "halpradio-party-v2/key|listener->dialer")
}

// authSigMessage is what each side signs: its role, the transcript (which
// covers both ephemeral keys) and both identity public keys, dialer first. A
// signature therefore only verifies on this channel between these two peers.
func authSigMessage(role string, th, dialerPub, listenerPub []byte) []byte {
	msg := make([]byte, 0, 64+len(th)+len(dialerPub)+len(listenerPub))
	msg = append(msg, "halpradio-party-v2/auth|"+role+"|"...)
	msg = append(msg, th...)
	msg = append(msg, dialerPub...)
	return append(msg, listenerPub...)
}

func signAuth(m *MeshNetwork, role string, th, dialerPub, listenerPub []byte) []byte {
	return ed25519.Sign(m.priv, authSigMessage(role, th, dialerPub, listenerPub))
}

func verifyAuth(signer ed25519.PublicKey, role string, th, dialerPub, listenerPub, sig []byte) bool {
	return len(signer) == ed25519.PublicKeySize && len(sig) == ed25519.SignatureSize &&
		ed25519.Verify(signer, authSigMessage(role, th, dialerPub, listenerPub), sig)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func writePreamble(conn net.Conn, p preamble) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return writeFrame(conn, b)
}

func parsePreamble(b []byte) (*preamble, error) {
	var p preamble
	if err := json.Unmarshal(b, &p); err != nil || p.Proto != protocolName {
		return nil, ErrNotPartyPeer
	}
	return &p, nil
}

func isClosedErr(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || isConnReset(err)
}

// handshake runs the v2 authenticated key exchange:
//
//  1. D -> L: cleartext preamble {proto, v, nonceD, ephD}
//  2. L -> D: cleartext preamble {proto, v, nonceL, ephL}
//     (or {error} and close on a version mismatch)
//  3. both: th = SHA-256(label | v | roomBinding | nonceD | nonceL | ephD | ephL),
//     ss = X25519(eph, peer eph), prk = HMAC(roomKey, label | ss | th),
//     K_d2l, K_l2d = HMAC(prk, label | direction)
//  4. D -> L: AEAD_Kd2l(ctr 0){pubD, nick, port}
//  5. L -> D: AEAD_Kl2d(ctr 0){pubL, nick, port, Sign(L, "auth|listener|" th pubD pubL)}
//  6. D -> L: AEAD_Kd2l(ctr 1){Sign(D, "auth|dialer|" th pubD pubL)}
//
// A peer that cannot open message 4 or 5 holds a different room code. The
// verified PeerID = H(pub) is bound to the connection; every later frame uses
// the same direction keys with strictly increasing counter nonces. A relay
// that holds the room code but forwards the ephemeral keys cannot compute the
// keys; one that replaces them cannot reuse either side's signature.
func (m *MeshNetwork) handshake(conn net.Conn, outbound bool) (*TCPPeerConn, error) {
	_ = conn.SetDeadline(time.Now().Add(m.handshakeTimeout))
	defer func() { _ = conn.SetDeadline(time.Time{}) }()

	myNonce := make([]byte, 32)
	if _, err := rand.Read(myNonce); err != nil {
		return nil, err
	}
	ephPriv, err := x25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	myEph := ephPriv.PublicKey().Bytes()
	hello := preamble{Proto: protocolName, Version: ProtocolVersion, Nonce: myNonce, Eph: myEph}

	var peer *preamble
	if outbound {
		if err := writePreamble(conn, hello); err != nil {
			return nil, err
		}
		b, err := readFrame(conn, maxHandshakeFrame)
		if err != nil {
			if isClosedErr(err) {
				return nil, ErrPeerClosedHandshake
			}
			return nil, fmt.Errorf("handshake: %w", err)
		}
		if peer, err = parsePreamble(b); err != nil {
			return nil, err
		}
		if peer.Version != ProtocolVersion {
			return nil, &VersionMismatchError{Local: ProtocolVersion, Remote: peer.Version}
		}
		if peer.Error != "" {
			return nil, fmt.Errorf("the peer refused the connection: %s", SanitizeString(peer.Error, 120))
		}
	} else {
		b, err := readFrame(conn, maxHandshakeFrame)
		if err != nil {
			return nil, err
		}
		if peer, err = parsePreamble(b); err != nil {
			return nil, err
		}
		if peer.Version != ProtocolVersion {
			_ = writePreamble(conn, preamble{Proto: protocolName, Version: ProtocolVersion,
				Error: fmt.Sprintf("unsupported party protocol v%d", peer.Version)})
			return nil, &VersionMismatchError{Local: ProtocolVersion, Remote: peer.Version}
		}
	}
	if len(peer.Nonce) != 32 || len(peer.Eph) != 32 {
		return nil, ErrNotPartyPeer
	}
	peerEph, err := x25519.NewPublicKey(peer.Eph)
	if err != nil {
		return nil, ErrNotPartyPeer
	}
	// ECDH fails on low-order points (an all-zero shared secret).
	shared, err := ephPriv.ECDH(peerEph)
	if err != nil {
		return nil, errors.New("handshake: invalid ephemeral key")
	}
	if !outbound {
		if err := writePreamble(conn, hello); err != nil {
			return nil, err
		}
	}

	nonceD, nonceL, ephD, ephL := myNonce, peer.Nonce, myEph, peer.Eph
	if !outbound {
		nonceD, nonceL, ephD, ephL = peer.Nonce, myNonce, peer.Eph, myEph
	}
	th := m.transcriptHash(nonceD, nonceL, ephD, ephL)
	kd2l, kl2d := sessionKeys(m.roomKey, shared, th)
	d2l, err := newGCM(kd2l)
	if err != nil {
		return nil, err
	}
	l2d, err := newGCM(kl2d)
	if err != nil {
		return nil, err
	}

	pc := newTCPPeerConn(conn, outbound)
	if outbound {
		pc.sendAEAD, pc.recvAEAD = d2l, l2d
	} else {
		pc.sendAEAD, pc.recvAEAD = l2d, d2l
	}

	send := func(im identityMsg) error {
		im.Version = ProtocolVersion
		b, _ := json.Marshal(im)
		pc.mu.Lock()
		ct := pc.sealLocked(b)
		pc.mu.Unlock()
		return writeFrame(conn, ct)
	}
	// recv opens the next handshake message. A frame that does not open means
	// the peer derived other keys: a different room code (or a relay).
	recv := func() (*identityMsg, error) {
		b, err := readFrame(conn, maxHandshakeFrame)
		if err != nil {
			if outbound && isClosedErr(err) {
				return nil, ErrWrongRoomCode
			}
			return nil, err
		}
		pt, err := pc.open(b)
		if err != nil {
			if !outbound {
				_ = writePreamble(conn, preamble{Proto: protocolName, Version: ProtocolVersion, Error: "room code mismatch"})
			}
			return nil, ErrWrongRoomCode
		}
		var im identityMsg
		if err := json.Unmarshal(pt, &im); err != nil {
			return nil, errors.New("handshake: malformed auth message")
		}
		if im.Version != ProtocolVersion {
			return nil, &VersionMismatchError{Local: ProtocolVersion, Remote: im.Version}
		}
		return &im, nil
	}
	bindPeer := func(im *identityMsg) error {
		if len(im.PubKey) != ed25519.PublicKeySize {
			return errors.New("handshake: malformed identity")
		}
		id := PeerIDFromPublicKey(im.PubKey)
		if id == m.peerID || bytes.Equal(im.PubKey, m.pub) {
			return errors.New("handshake: connected to ourselves")
		}
		pc.remoteID = id
		pc.remotePub = ed25519.PublicKey(append([]byte(nil), im.PubKey...))
		pc.remoteNick = SanitizeString(im.Nick, 32)
		if im.ListenPort > 0 && im.ListenPort <= 65535 {
			pc.remotePort = im.ListenPort
		}
		return nil
	}
	verifySig := func(role string, sig, dialerPub, listenerPub []byte) error {
		if !verifyAuth(pc.remotePub, role, th, dialerPub, listenerPub, sig) {
			return errors.New("handshake: identity signature does not verify")
		}
		return nil
	}

	if outbound {
		if err := send(identityMsg{PubKey: m.pub, Nick: m.Nickname(), ListenPort: m.ListenPort()}); err != nil {
			return nil, err
		}
		im, err := recv()
		if err != nil {
			return nil, err
		}
		if err := bindPeer(im); err != nil {
			return nil, err
		}
		if err := verifySig(roleListener, im.Sig, m.pub, pc.remotePub); err != nil {
			return nil, err
		}
		sig := signAuth(m, roleDialer, th, m.pub, pc.remotePub)
		if err := send(identityMsg{Sig: sig}); err != nil {
			return nil, err
		}
	} else {
		im, err := recv()
		if err != nil {
			return nil, err
		}
		if err := bindPeer(im); err != nil {
			return nil, err
		}
		sig := signAuth(m, roleListener, th, pc.remotePub, m.pub)
		if err := send(identityMsg{PubKey: m.pub, Sig: sig, Nick: m.Nickname(), ListenPort: m.ListenPort()}); err != nil {
			return nil, err
		}
		fin, err := recv()
		if err != nil {
			return nil, err
		}
		if err := verifySig(roleDialer, fin.Sig, pc.remotePub, m.pub); err != nil {
			return nil, err
		}
	}
	pc.startWriter()
	return pc, nil
}

func isConnReset(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "connection reset") || strings.Contains(err.Error(), "broken pipe"))
}

// registerPeer adds an authenticated connection to the peer table. A second
// connection for an already connected identity is refused.
func (m *MeshNetwork) registerPeer(pc *TCPPeerConn) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return errors.New("mesh closed")
	}
	if _, exists := m.peers[pc.remoteID]; exists {
		m.mu.Unlock()
		return errors.New("already connected to this peer")
	}
	if len(m.peers) >= MaxPeersLimit {
		m.mu.Unlock()
		return ErrRoomFull
	}
	m.peers[pc.remoteID] = pc
	onJoined := m.onPeerJoined
	m.mu.Unlock()

	if onJoined != nil {
		onJoined(pc.remoteID, pc.remoteNick)
	}
	return nil
}

func (m *MeshNetwork) readLoop(pc *TCPPeerConn) {
	defer func() {
		_ = pc.Close()
		m.removePeer(pc.remoteID, pc)
	}()

	limiter := newTokenBucket(connRatePerSec, connRateBurst)
	dropped := 0
	for {
		_ = pc.conn.SetReadDeadline(time.Now().Add(idleReadTimeout))
		frame, err := readFrame(pc.conn, maxDataFrame)
		if err != nil {
			return
		}
		plaintext, err := pc.open(frame)
		if err != nil {
			// Forged, replayed or reordered frame: the stream is no longer trustworthy.
			return
		}
		if !limiter.allow() {
			dropped++
			if dropped > connMaxDropped {
				return
			}
			continue
		}
		dropped = 0

		var pkt Packet
		if err := json.Unmarshal(plaintext, &pkt); err != nil {
			return
		}
		// The sender must be the identity proven on this connection.
		if pkt.SenderID != pc.remoteID {
			return
		}
		// Replay and reordering inside this connection are already ruled out
		// by the AEAD counter nonces; frames from another connection cannot
		// be opened because every connection has its own keys. Seq is kept
		// for the session's per-identity ordering of sync messages.
		if pkt.Seq == 0 {
			return
		}
		pkt.SenderNick = SanitizeString(pkt.SenderNick, 32)

		m.dispatchPacket(&pkt, pc)
	}
}

// IsConnected reports whether an authenticated connection to peerID is open.
func (m *MeshNetwork) IsConnected(peerID string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.peers[peerID]
	return ok
}

// removePeer drops peerID only if conn is the connection registered for it,
// so closing a rejected duplicate never tears down the live connection.
func (m *MeshNetwork) removePeer(peerID string, conn PeerConn) {
	if peerID == "" {
		return
	}
	m.mu.Lock()
	cur, exists := m.peers[peerID]
	if exists && cur == conn {
		delete(m.peers, peerID)
	} else {
		exists = false
	}
	onLeft := m.onPeerLeft
	m.mu.Unlock()

	if exists {
		_ = cur.Close()
		if onLeft != nil {
			onLeft(peerID)
		}
	}
}

func (m *MeshNetwork) dispatchPacket(pkt *Packet, from PeerConn) {
	m.mu.RLock()
	handler := m.onPacket
	m.mu.RUnlock()

	if handler != nil {
		handler(pkt, from)
	}
}

// stamp fills in this peer's identity, sequence number and timestamp.
func (m *MeshNetwork) stamp(pkt *Packet) {
	pkt.Seq = m.seq.Next()
	pkt.SenderID = m.peerID
	if pkt.SenderNick == "" {
		pkt.SenderNick = m.Nickname()
	}
	pkt.Timestamp = time.Now().UnixNano()
}

// SendDirect sends a packet to one peer connection. The sender identity and
// sequence number are always this peer's own.
func (m *MeshNetwork) SendDirect(conn PeerConn, pkt *Packet) error {
	if conn == nil {
		return errors.New("nil peer connection")
	}
	m.stamp(pkt)
	data, err := json.Marshal(pkt)
	if err != nil {
		return err
	}
	return conn.sendPlain(data)
}

// Broadcast queues a packet for every connected peer. It never blocks on a
// slow peer: a peer whose send queue is full is disconnected instead.
func (m *MeshNetwork) Broadcast(pkt *Packet) error {
	m.stamp(pkt)
	data, err := json.Marshal(pkt)
	if err != nil {
		return fmt.Errorf("failed to marshal packet: %w", err)
	}

	m.mu.RLock()
	conns := make([]PeerConn, 0, len(m.peers))
	for _, c := range m.peers {
		conns = append(conns, c)
	}
	m.mu.RUnlock()

	for _, c := range conns {
		_ = c.sendPlain(data)
	}
	return nil
}

// PeerCount returns the number of authenticated peers in the mesh.
func (m *MeshNetwork) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peers)
}

// PeerIDs returns all currently connected (verified) peer IDs.
func (m *MeshNetwork) PeerIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.peers))
	for id := range m.peers {
		ids = append(ids, id)
	}
	return ids
}

// --- LAN discovery -------------------------------------------------------------

const beaconPrefix = "HPRB2"

// beaconMAC authenticates every field of a beacon with a key derived from the
// room key, so a beacon cannot be re-pointed at another address, port or peer.
func (m *MeshNetwork) beaconMAC(peerID, ip string, port int, ts int64, nonce string) string {
	mac := hmac.New(sha256.New, m.beaconKey)
	fmt.Fprintf(mac, "halpradio-party-v2/beacon|%s|%s|%d|%d|%s", peerID, ip, port, ts, nonce)
	return hex.EncodeToString(mac.Sum(nil))
}

// buildBeacon returns the beacon announcing this peer at ip:port.
func (m *MeshNetwork) buildBeacon(ip net.IP, port int, now time.Time) (string, error) {
	nb := make([]byte, 16)
	if _, err := rand.Read(nb); err != nil {
		return "", err
	}
	nonce := hex.EncodeToString(nb)
	ipStr := ip.String()
	ts := now.Unix()
	return strings.Join([]string{beaconPrefix, m.peerID, ipStr, strconv.Itoa(port),
		strconv.FormatInt(ts, 10), nonce, m.beaconMAC(m.peerID, ipStr, port, ts, nonce)}, "|"), nil
}

// isLANAddr reports whether ip is a loopback, private (RFC 1918 / ULA) or
// link-local unicast address. Beacons from anything else are ignored.
func isLANAddr(ip net.IP) bool {
	if ip == nil || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil && ip4.Equal(net.IPv4bcast) {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// verifyBeacon checks a received beacon and returns the advertised peer ID and
// the address to dial. It enforces: MAC over all fields, advertised IP equal to
// the UDP source, a LAN source, a sane port, freshness and single use.
func (m *MeshNetwork) verifyBeacon(msg []byte, src *net.UDPAddr, now time.Time) (string, string, error) {
	if src == nil {
		return "", "", errors.New("no source")
	}
	parts := strings.Split(string(msg), "|")
	if len(parts) != 7 || parts[0] != beaconPrefix {
		return "", "", errors.New("not a v2 beacon")
	}
	peerID, ipStr, portStr, tsStr, nonce, mac := parts[1], parts[2], parts[3], parts[4], parts[5], parts[6]
	if !isValidPeerID(peerID) {
		return "", "", errors.New("bad peer id")
	}
	if peerID == m.peerID {
		return "", "", errors.New("own beacon")
	}
	ip := net.ParseIP(ipStr)
	if ip == nil || !ip.Equal(src.IP) {
		return "", "", errors.New("advertised address does not match sender")
	}
	if !isLANAddr(src.IP) {
		return "", "", errors.New("beacon from a non-LAN address")
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1024 || port > 65535 {
		return "", "", errors.New("bad port")
	}
	ts, err := strconv.ParseInt(tsStr, 10, 64)
	if err != nil {
		return "", "", errors.New("bad timestamp")
	}
	age := now.Sub(time.Unix(ts, 0))
	if age > beaconMaxAge || age < -beaconMaxAge {
		return "", "", errors.New("stale beacon")
	}
	if len(nonce) != 32 {
		return "", "", errors.New("bad nonce")
	}
	want := m.beaconMAC(peerID, ipStr, port, ts, nonce)
	if !hmac.Equal([]byte(want), []byte(mac)) {
		return "", "", errors.New("bad beacon MAC")
	}

	// Single use: remember nonces until they are too old to pass the age check.
	m.beaconSeenMu.Lock()
	defer m.beaconSeenMu.Unlock()
	if _, seen := m.beaconSeen[nonce]; seen {
		return "", "", errors.New("replayed beacon")
	}
	if len(m.beaconSeen) >= beaconCacheMax {
		for k, t := range m.beaconSeen {
			if now.Sub(t) > 2*beaconMaxAge {
				delete(m.beaconSeen, k)
			}
		}
		if len(m.beaconSeen) >= beaconCacheMax {
			return "", "", errors.New("beacon cache full")
		}
	}
	m.beaconSeen[nonce] = now
	return peerID, net.JoinHostPort(ip.String(), portStr), nil
}

// allowDial applies a per-target debounce with a bounded table.
func (m *MeshNetwork) allowDial(target string, now time.Time) bool {
	m.dialDebounceMu.Lock()
	defer m.dialDebounceMu.Unlock()
	if last, ok := m.dialDebounce[target]; ok && now.Sub(last) < dialDebounceTime {
		return false
	}
	if len(m.dialDebounce) >= dialDebounceMax {
		for k, t := range m.dialDebounce {
			if now.Sub(t) >= dialDebounceTime {
				delete(m.dialDebounce, k)
			}
		}
		if len(m.dialDebounce) >= dialDebounceMax {
			return false
		}
	}
	m.dialDebounce[target] = now
	return true
}

// handleBeacon verifies a beacon and, if allowed, dials the advertised peer.
// The dial is accepted only if the peer proves the advertised identity.
func (m *MeshNetwork) handleBeacon(msg []byte, src *net.UDPAddr) {
	peerID, target, err := m.verifyBeacon(msg, src, time.Now())
	if err != nil {
		return
	}
	m.mu.RLock()
	_, connected := m.peers[peerID]
	policy := m.autoDial
	closed := m.closed
	m.mu.RUnlock()
	if connected || closed || (policy != nil && !policy()) {
		return
	}
	if !m.allowDial(target, time.Now()) {
		return
	}
	go func() {
		_, _ = m.connectPeer(target, peerID)
	}()
}

// StartLANDiscovery runs periodic UDP broadcast beacons on the LAN.
func (m *MeshNetwork) StartLANDiscovery() {
	m.mu.Lock()
	if m.beaconCancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.beaconCancel = cancel
	m.mu.Unlock()

	// Broadcast sender. Dialing the broadcast address lets the OS pick the
	// source address, which is advertised (and authenticated) in the beacon.
	go func() {
		bcast := &net.UDPAddr{IP: net.IPv4bcast, Port: beaconPort}
		ticker := time.NewTicker(beaconInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				port := m.ListenPort()
				if port == 0 {
					continue
				}
				conn, err := net.DialUDP("udp4", nil, bcast)
				if err != nil {
					continue
				}
				local, _ := conn.LocalAddr().(*net.UDPAddr)
				if local != nil && !local.IP.IsUnspecified() {
					if msg, err := m.buildBeacon(local.IP, port, time.Now()); err == nil {
						_, _ = conn.Write([]byte(msg))
					}
				}
				_ = conn.Close()
			}
		}
	}()

	// Broadcast listener
	go func() {
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{Port: beaconPort})
		if err != nil {
			return
		}
		defer conn.Close()

		buf := make([]byte, 512)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				continue
			}
			m.handleBeacon(append([]byte(nil), buf[:n]...), src)
		}
	}()
}

// Close stops all networking services and closes all peer connections.
func (m *MeshNetwork) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true

	if m.beaconCancel != nil {
		m.beaconCancel()
	}

	if m.tcpListener != nil {
		_ = m.tcpListener.Close()
	}

	conns := make([]PeerConn, 0, len(m.peers))
	for _, c := range m.peers {
		conns = append(conns, c)
	}
	m.peers = make(map[string]PeerConn)
	m.mu.Unlock()

	// Give queued frames (such as a Leave) a short chance to go out.
	deadline := time.Now().Add(250 * time.Millisecond)
	for _, c := range conns {
		if tc, ok := c.(*TCPPeerConn); ok {
			if d := time.Until(deadline); d > 0 {
				tc.flush(d)
			}
		}
		_ = c.Close()
	}
	return nil
}
