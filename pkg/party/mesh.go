package party

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
)

const MaxPeersLimit = 32

// PeerConn is an abstract connection to another peer in the party mesh.
type PeerConn interface {
	RemotePeerID() string
	RemoteAddr() string
	Send(encryptedData []byte) error
	Close() error
}

// TCPPeerConn implements PeerConn over a framed TCP socket.
type TCPPeerConn struct {
	mu           sync.Mutex
	conn         net.Conn
	remotePeerID string
	closed       bool
}

func NewTCPPeerConn(conn net.Conn, peerID string) *TCPPeerConn {
	return &TCPPeerConn{
		conn:         conn,
		remotePeerID: peerID,
	}
}

func (c *TCPPeerConn) RemotePeerID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.remotePeerID
}

func (c *TCPPeerConn) SetRemotePeerID(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.remotePeerID = id
}

func (c *TCPPeerConn) RemoteAddr() string {
	if c.conn == nil {
		return ""
	}
	return c.conn.RemoteAddr().String()
}

func (c *TCPPeerConn) Send(data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed || c.conn == nil {
		return errors.New("connection closed")
	}

	length := uint32(len(data))
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], length)

	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.conn.Write(lenBuf[:]); err != nil {
		return err
	}
	_, err := c.conn.Write(data)
	return err
}

func (c *TCPPeerConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// WebRTCPeerConn implements PeerConn over a WebRTC DataChannel.
type WebRTCPeerConn struct {
	mu           sync.Mutex
	pc           *webrtc.PeerConnection
	dc           *webrtc.DataChannel
	remotePeerID string
	closed       bool
}

func NewWebRTCPeerConn(pc *webrtc.PeerConnection, dc *webrtc.DataChannel, peerID string) *WebRTCPeerConn {
	return &WebRTCPeerConn{
		pc:           pc,
		dc:           dc,
		remotePeerID: peerID,
	}
}

func (w *WebRTCPeerConn) RemotePeerID() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.remotePeerID
}

func (w *WebRTCPeerConn) RemoteAddr() string {
	return "webrtc-datachannel"
}

func (w *WebRTCPeerConn) Send(data []byte) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.dc == nil {
		return errors.New("datachannel closed")
	}
	return w.dc.Send(data)
}

func (w *WebRTCPeerConn) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	var err1, err2 error
	if w.dc != nil {
		err1 = w.dc.Close()
	}
	if w.pc != nil {
		err2 = w.pc.Close()
	}
	if err1 != nil {
		return err1
	}
	return err2
}

// MeshNetwork manages peer discovery, direct connections, encryption, and message routing.
type MeshNetwork struct {
	mu          sync.RWMutex
	peerID      string
	nickname    string
	roomCode    string
	roomKey     []byte
	aad         []byte
	listenPort  int
	tcpListener net.Listener
	peers       map[string]PeerConn
	seq         SequenceCounter
	closed      bool

	onPacket     func(pkt *Packet, from PeerConn)
	onPeerJoined func(peerID string, nickname string)
	onPeerLeft   func(peerID string)

	beaconCancel context.CancelFunc

	dialDebounceMu sync.Mutex
	dialDebounce   map[string]time.Time
}

// NewMeshNetwork creates a new P2P mesh network instance for the given room code.
func NewMeshNetwork(nickname, roomCode string) (*MeshNetwork, error) {
	normCode := NormalizeRoomCode(roomCode)
	if normCode == "" {
		return nil, errors.New("empty room code")
	}

	key, err := DeriveRoomKey(normCode)
	if err != nil {
		return nil, err
	}

	nick := SanitizeString(nickname, 32)
	if nick == "" {
		nick = "dj-" + strings.ToLower(normCode[:min(3, len(normCode))])
	}

	aad := []byte("halpradio-party-v1:" + HashRoomCode(normCode))

	m := &MeshNetwork{
		peerID:       uuid.New().String(),
		nickname:     nick,
		roomCode:     normCode,
		roomKey:      key,
		aad:          aad,
		peers:        make(map[string]PeerConn),
		dialDebounce: make(map[string]time.Time),
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
			continue
		}

		m.mu.RLock()
		peerCount := len(m.peers)
		m.mu.RUnlock()

		// Protect against resource exhaustion: reject if peer limit reached
		if peerCount >= MaxPeersLimit {
			_ = conn.Close()
			continue
		}

		go m.handleIncomingTCP(conn)
	}
}

func (m *MeshNetwork) handleIncomingTCP(conn net.Conn) {
	pConn := NewTCPPeerConn(conn, "")
	m.readFramedPackets(pConn, conn)
}

func (m *MeshNetwork) readFramedPackets(pConn *TCPPeerConn, conn net.Conn) {
	defer func() {
		_ = pConn.Close()
		remoteID := pConn.RemotePeerID()
		if remoteID != "" {
			m.removePeer(remoteID)
		}
	}()

	var lenBuf [4]byte
	for {
		// Enforce read deadline on every packet to neutralize Slowloris DoS
		_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))

		if _, err := io.ReadFull(conn, lenBuf[:]); err != nil {
			return
		}

		length := binary.BigEndian.Uint32(lenBuf[:])
		// Protect against oversized packets (max 64KB)
		if length == 0 || length > 65536 {
			return
		}

		buf := make([]byte, length)
		if _, err := io.ReadFull(conn, buf); err != nil {
			return
		}

		// Decrypt packet using AES-256-GCM and bound AAD
		pkt, err := DecryptPacket(m.roomKey, buf, m.aad)
		if err != nil {
			// Authentication failure or wrong room code / replay attack
			return
		}

		// Ingress sanitization on sender metadata
		pkt.SenderID = SanitizeString(pkt.SenderID, 64)
		pkt.SenderNick = SanitizeString(pkt.SenderNick, 32)

		// Associate connection with remote peer ID on first valid packet
		if pConn.RemotePeerID() == "" && pkt.SenderID != "" {
			pConn.SetRemotePeerID(pkt.SenderID)
			m.addPeer(pkt.SenderID, pConn)
			if m.onPeerJoined != nil {
				m.onPeerJoined(pkt.SenderID, pkt.SenderNick)
			}
		}

		m.dispatchPacket(pkt, pConn)
	}
}

// ConnectPeer dials another peer via TCP address (e.g. "192.168.1.50:4242" or "localhost:4242").
func (m *MeshNetwork) ConnectPeer(addr string) (PeerConn, error) {
	conn, err := net.DialTimeout("tcp", addr, 4*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to dial peer %s: %w", addr, err)
	}

	pConn := NewTCPPeerConn(conn, "")
	go m.readFramedPackets(pConn, conn)

	// Send initial Hello packet over the encrypted channel
	hello := &Packet{
		Type:       MsgTypeHello,
		Seq:        m.seq.Next(),
		SenderID:   m.peerID,
		SenderNick: m.Nickname(),
		Timestamp:  time.Now().UnixNano(),
	}
	payload, _ := json.Marshal(HelloPayload{
		RoomName:      m.roomCode,
		ClientVersion: "0.4.0",
		ListenPort:    m.ListenPort(),
	})
	hello.Payload = payload

	if err := m.SendDirect(pConn, hello); err != nil {
		_ = pConn.Close()
		return nil, err
	}

	return pConn, nil
}

func (m *MeshNetwork) addPeer(peerID string, conn PeerConn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers[peerID] = conn
}

func (m *MeshNetwork) removePeer(peerID string) {
	m.mu.Lock()
	conn, exists := m.peers[peerID]
	if exists {
		delete(m.peers, peerID)
	}
	onLeft := m.onPeerLeft
	m.mu.Unlock()

	if exists {
		_ = conn.Close()
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

// SendDirect encrypts and sends a packet to a specific peer connection.
func (m *MeshNetwork) SendDirect(conn PeerConn, pkt *Packet) error {
	if conn == nil {
		return errors.New("nil peer connection")
	}

	if pkt.Seq == 0 {
		pkt.Seq = m.seq.Next()
	}
	if pkt.SenderID == "" {
		pkt.SenderID = m.peerID
	}
	if pkt.SenderNick == "" {
		pkt.SenderNick = m.Nickname()
	}
	if pkt.Timestamp == 0 {
		pkt.Timestamp = time.Now().UnixNano()
	}

	encrypted, err := EncryptPacket(m.roomKey, pkt, m.aad)
	if err != nil {
		return fmt.Errorf("encryption error: %w", err)
	}

	return conn.Send(encrypted)
}

// Broadcast sends an encrypted packet to all currently connected peers concurrently.
func (m *MeshNetwork) Broadcast(pkt *Packet) error {
	pkt.Seq = m.seq.Next()
	pkt.SenderID = m.peerID
	pkt.SenderNick = m.Nickname()
	pkt.Timestamp = time.Now().UnixNano()

	encrypted, err := EncryptPacket(m.roomKey, pkt, m.aad)
	if err != nil {
		return fmt.Errorf("failed to encrypt packet: %w", err)
	}

	m.mu.RLock()
	conns := make([]PeerConn, 0, len(m.peers))
	for _, c := range m.peers {
		conns = append(conns, c)
	}
	m.mu.RUnlock()

	var wg sync.WaitGroup
	for _, c := range conns {
		wg.Add(1)
		go func(conn PeerConn) {
			defer wg.Done()
			_ = conn.Send(encrypted)
		}(c)
	}
	wg.Wait()
	return nil
}

// PeerCount returns the number of active peers in the mesh.
func (m *MeshNetwork) PeerCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.peers)
}

// PeerIDs returns all currently connected peer IDs.
func (m *MeshNetwork) PeerIDs() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ids := make([]string, 0, len(m.peers))
	for id := range m.peers {
		ids = append(ids, id)
	}
	return ids
}

// StartLANDiscovery runs periodic UDP broadcast beacons with time-bucketed HMAC tokens.
func (m *MeshNetwork) StartLANDiscovery() {
	m.mu.Lock()
	if m.beaconCancel != nil {
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.beaconCancel = cancel
	m.mu.Unlock()

	// Broadcast sender
	go func() {
		broadcastAddr, err := net.ResolveUDPAddr("udp", "255.255.255.255:54242")
		if err != nil {
			return
		}

		conn, err := net.ListenUDP("udp", nil)
		if err != nil {
			return
		}
		defer conn.Close()

		ticker := time.NewTicker(2 * time.Second)
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
				// Ephemeral HMAC token derived from roomKey and rolling 30s epoch
				beaconToken := ComputeBeaconToken(m.roomKey, 0)
				msg := fmt.Sprintf("HALPRADIO_BEACON:%s:%s:%d", beaconToken, m.peerID, port)
				_, _ = conn.WriteTo([]byte(msg), broadcastAddr)
			}
		}
	}()

	// Broadcast listener
	go func() {
		listenAddr, err := net.ResolveUDPAddr("udp", ":54242")
		if err != nil {
			return
		}

		conn, err := net.ListenUDP("udp", listenAddr)
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
			n, rAddr, err := conn.ReadFrom(buf)
			if err != nil {
				continue
			}

			msg := string(buf[:n])
			parts := strings.Split(msg, ":")
			if len(parts) == 4 && parts[0] == "HALPRADIO_BEACON" {
				rcvdToken := parts[1]
				rcvdPeerID := parts[2]
				var rcvdPort int
				_, _ = fmt.Sscanf(parts[3], "%d", &rcvdPort)

				if rcvdPeerID == m.peerID || rcvdPort <= 0 || rcvdPort > 65535 {
					continue
				}

				// Verify token using room key
				if !VerifyBeaconToken(m.roomKey, rcvdToken) {
					continue
				}

				m.mu.RLock()
				_, connected := m.peers[rcvdPeerID]
				m.mu.RUnlock()

				if !connected {
					host, _, err := net.SplitHostPort(rAddr.String())
					if err == nil {
						target := fmt.Sprintf("%s:%d", host, rcvdPort)

						// Rate limit outbound dials to prevent UDP beacon amplification / SSRF
						m.dialDebounceMu.Lock()
						lastDial, exists := m.dialDebounce[target]
						now := time.Now()
						if exists && now.Sub(lastDial) < 5*time.Second {
							m.dialDebounceMu.Unlock()
							continue
						}
						m.dialDebounce[target] = now
						m.dialDebounceMu.Unlock()

						go func() {
							_, _ = m.ConnectPeer(target)
						}()
					}
				}
			}
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

	for _, c := range conns {
		_ = c.Close()
	}
	return nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
