package party

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// FloatingReaction represents a live reaction animating over the terminal visualizer.
type FloatingReaction struct {
	ID        string    `json:"id"`
	Emoji     string    `json:"emoji"`
	Sender    string    `json:"sender"`
	CreatedAt time.Time `json:"created_at"`
}

// Age returns the time elapsed since the reaction was broadcast.
func (r FloatingReaction) Age() time.Duration {
	return time.Since(r.CreatedAt)
}

// ChatMessage represents a 1-line mini-chat ping in the room.
type ChatMessage struct {
	ID        string    `json:"id"`
	Sender    string    `json:"sender"`
	Message   string    `json:"message"`
	Timestamp time.Time `json:"timestamp"`
}

// PeerInfo holds detailed runtime metadata for a peer.
type PeerInfo struct {
	ID       string
	Nickname string
	IsHost   bool
	JoinedAt time.Time
	LastSeen time.Time
	PingMs   int64
	// Verified is true for peers with an authenticated direct connection (and
	// for this peer). Unverified entries are roster hints from the host: they
	// are shown, but never authorised for anything and never elected.
	Verified bool
}

const (
	// maxRosterHints caps unverified roster entries learned from the host.
	maxRosterHints = MaxPeersLimit
	// peerTimeout is how long a peer may stay silent before it is dropped.
	peerTimeout = 5 * time.Second
	// Per-peer chat and reaction rate limit.
	socialRatePerSec = 3
	socialBurst      = 6
	maxBitrateKbps   = 100000
)

// PartySession coordinates room synchronization, permissions, reactions, and chat.
type PartySession struct {
	mu           sync.RWMutex
	mesh         *MeshNetwork
	roomCode     string
	roomName     string
	isHost       bool
	hostPeerID   string
	hostNickname string
	djPass       DJPassMode
	joinedAt     time.Time

	peers       map[string]*PeerInfo
	peerSeq     map[string]uint64
	socialLimit map[string]*tokenBucket
	// succession is the host's roster order (join order); it ranks the
	// candidates when the host leaves.
	succession  []string
	currentSync *SyncPayload

	reactions  []FloatingReaction
	recentChat []ChatMessage

	onPlaybackSync func(SyncPayload)
	onReaction     func(FloatingReaction)
	onChat         func(ChatMessage)
	onPeerChange   func([]*PeerInfo)
	onStatusFlash  func(string)

	// hostPin is the fingerprint of the only host identity this session will
	// follow (from the invite, then moved along authenticated hand-overs).
	// Empty for a join without a pin.
	hostPin string
	// rejectedHostClaims counts HelloAck and HostMigrate messages ignored
	// because they did not come from (or were not allowed by) the trusted host.
	rejectedHostClaims atomic.Uint64

	closed        bool
	heartbeatStop chan struct{}
}

// SessionConfig configures a new PartySession.
type SessionConfig struct {
	RoomCode string
	RoomName string
	Nickname string
	IsHost   bool
	DJPass   DJPassMode
	Port     int
	// HostPin is the host fingerprint from an invite (see ParseInvite). When
	// set, the session only accepts a host whose authenticated identity has
	// this fingerprint, or one that host hands over to. Guests only.
	HostPin string
}

// JoinWithInvite joins a room from an invite: a bare room code (behaves like
// NewPartySession) or a room code with the host's fingerprint, which pins the
// host. cfg.RoomCode, cfg.HostPin and cfg.IsHost are taken from the invite.
func JoinWithInvite(invite string, cfg SessionConfig) (*PartySession, error) {
	code, pin, err := ParseInvite(invite)
	if err != nil {
		return nil, err
	}
	cfg.RoomCode, cfg.HostPin, cfg.IsHost = code, pin, false
	return NewPartySession(cfg)
}

// NewPartySession creates an active party session. An empty room code
// generates a new one; any other code must pass ValidateRoomCode.
func NewPartySession(cfg SessionConfig) (*PartySession, error) {
	var normCode string
	if strings.TrimSpace(cfg.RoomCode) == "" {
		normCode = GenerateRoomCode()
	} else {
		check := ValidateRoomCode
		if cfg.IsHost {
			check = ValidateNewRoomCode // FR-C-8: only a created code
		}
		if err := check(cfg.RoomCode); err != nil {
			return nil, err
		}
		normCode = NormalizeRoomCode(cfg.RoomCode)
	}
	hostPin := ""
	if strings.TrimSpace(cfg.HostPin) != "" {
		if cfg.IsHost {
			return nil, errors.New("a host pin only applies when joining a room")
		}
		if hostPin = NormalizeFingerprint(cfg.HostPin); hostPin == "" {
			return nil, fmt.Errorf("host fingerprint must have %d characters, e.g. %s", FingerprintLen, ExampleFingerprint)
		}
	}

	roomName := SanitizeString(cfg.RoomName, 48)
	if roomName == "" {
		roomName = "halpradio-party"
	}

	djPass := cfg.DJPass
	if !ValidDJPass(djPass) {
		djPass = DJPassHostOnly
	}

	mesh, err := NewMeshNetwork(cfg.Nickname, normCode)
	if err != nil {
		return nil, err
	}

	if err := mesh.StartListening(cfg.Port); err != nil {
		_ = mesh.Close()
		return nil, err
	}

	s := &PartySession{
		mesh:          mesh,
		roomCode:      normCode,
		roomName:      roomName,
		isHost:        cfg.IsHost,
		hostPeerID:    mesh.PeerID(),
		hostNickname:  mesh.Nickname(),
		djPass:        djPass,
		joinedAt:      time.Now(),
		peers:         make(map[string]*PeerInfo),
		peerSeq:       make(map[string]uint64),
		socialLimit:   make(map[string]*tokenBucket),
		reactions:     make([]FloatingReaction, 0),
		recentChat:    make([]ChatMessage, 0),
		heartbeatStop: make(chan struct{}),
		hostPin:       hostPin,
	}

	if !cfg.IsHost {
		s.hostPeerID = ""
		s.hostNickname = ""
	}

	// Add self to peer roster
	s.peers[mesh.PeerID()] = &PeerInfo{
		ID:       mesh.PeerID(),
		Nickname: mesh.Nickname(),
		IsHost:   cfg.IsHost,
		JoinedAt: s.joinedAt,
		LastSeen: time.Now(),
		Verified: true,
	}

	mesh.SetPacketHandler(s.handleIncomingPacket)
	mesh.SetPeerJoinedHandler(s.handlePeerJoined)
	mesh.SetPeerLeftHandler(s.handlePeerLeft)
	// Only guests follow LAN beacons; a host waits to be dialed.
	mesh.SetAutoDialPolicy(func() bool { return !s.IsHost() })
	mesh.StartLANDiscovery()

	go s.heartbeatLoop()

	return s, nil
}

// SetHandlers registers event callbacks for the UI or player.
func (s *PartySession) SetHandlers(
	onSync func(SyncPayload),
	onReact func(FloatingReaction),
	onChat func(ChatMessage),
	onPeerChange func([]*PeerInfo),
	onStatus func(string),
) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onPlaybackSync = onSync
	s.onReaction = onReact
	s.onChat = onChat
	s.onPeerChange = onPeerChange
	s.onStatusFlash = onStatus
}

func (s *PartySession) RoomCode() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.roomCode
}

func (s *PartySession) FormattedCode() string {
	return FormatRoomCode(s.RoomCode())
}

func (s *PartySession) RoomName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.roomName
}

func (s *PartySession) IsHost() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isHost
}

func (s *PartySession) HostNickname() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hostNickname
}

// HostPeerID returns the verified identity this peer follows as host ("" if unknown).
func (s *PartySession) HostPeerID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hostPeerID
}

// HostFingerprint returns the 80-bit fingerprint of the identity key of the host
// this session follows (or its own, when it is the host), for the UI to show
// next to the host's nickname. It is empty until a host is known. Two people
// can compare it out loud to check they are in the same host's room.
func (s *PartySession) HostFingerprint() string {
	return PeerFingerprint(s.HostPeerID())
}

// HostPinned reports whether the current host was verified by the invite's
// host pin: it is the pinned identity, or the peer that identity handed the
// host role to. The UI can show "verified by invite" instead of asking for a
// manual fingerprint check. False for joins without a pin and for hosts.
func (s *PartySession) HostPinned() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hostPin != "" && s.hostPeerID != "" && !s.isHost && PeerFingerprint(s.hostPeerID) == s.hostPin
}

// HostPin returns the host fingerprint this session is pinned to, or "".
func (s *PartySession) HostPin() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hostPin
}

// RejectedHostClaims returns how many HelloAck and HostMigrate messages were
// ignored because they did not come from the host this session trusts.
func (s *PartySession) RejectedHostClaims() uint64 {
	return s.rejectedHostClaims.Load()
}

// Invite returns the invite to share: the room code plus the fingerprint of
// the current host (on the host, its own). Before a host is known it carries
// the pin this session joined with, or is the bare room code.
func (s *PartySession) Invite() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	switch {
	case s.hostPeerID != "":
		return FormatInvite(s.roomCode, PeerFingerprint(s.hostPeerID))
	case s.hostPin != "":
		return FormatInvite(s.roomCode, s.hostPin)
	}
	return NormalizeRoomCode(s.roomCode)
}

// Fingerprint returns the fingerprint of this peer's own identity key.
func (s *PartySession) Fingerprint() string {
	return PeerFingerprint(s.mesh.PeerID())
}

func (s *PartySession) DJPass() DJPassMode {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.djPass
}

func (s *PartySession) PeerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.peers)
}

func (s *PartySession) ListenPort() int {
	return s.mesh.ListenPort()
}

func (s *PartySession) PeerID() string {
	return s.mesh.PeerID()
}

func (s *PartySession) Nickname() string {
	return s.mesh.Nickname()
}

func (s *PartySession) IsActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.closed
}

// CanChangeStation returns whether the local peer is authorized to tune stations.
func (s *PartySession) CanChangeStation() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.isHost {
		return true
	}
	return s.djPass == DJPassOpenDemocracy
}

// ToggleDJPass switches the room between Host Only and Open Democracy (host only).
func (s *PartySession) ToggleDJPass() (DJPassMode, error) {
	s.mu.Lock()
	if !s.isHost {
		s.mu.Unlock()
		return s.djPass, errors.New("only room host can change DJ Pass permission")
	}

	if s.djPass == DJPassHostOnly {
		s.djPass = DJPassOpenDemocracy
	} else {
		s.djPass = DJPassHostOnly
	}
	newMode := s.djPass

	var syncCopy *SyncPayload
	if s.currentSync != nil {
		s.currentSync.DJPass = newMode
		sc := *s.currentSync
		syncCopy = &sc
	}
	s.mu.Unlock()

	if syncCopy != nil {
		_ = s.broadcastSync(syncCopy)
	}

	s.flash(fmt.Sprintf("🎵 DJ Pass updated to: %s", newMode))
	return newMode, nil
}

// BroadcastStationChange synchronizes a station change across all connected peers in < 500ms.
func (s *PartySession) BroadcastStationChange(
	stationID, stationName, streamURL, genre, country, codec string,
	bitrate int,
	status string,
) error {
	if streamURL != "" && !IsAllowedStreamURL(streamURL) {
		return errors.New("only http and https stream URLs can be shared with the party")
	}
	s.mu.Lock()
	if !s.isHost && s.djPass != DJPassOpenDemocracy {
		hostNick := s.hostNickname
		s.mu.Unlock()
		return fmt.Errorf("DJ Pass is Host Only (@%s is currently DJ)", hostNick)
	}

	payload := &SyncPayload{
		StationID:   SanitizeString(stationID, 64),
		StationName: SanitizeString(stationName, 128),
		StreamURL:   streamURL,
		Bitrate:     clampBitrate(bitrate),
		Genre:       SanitizeString(genre, 64),
		Country:     SanitizeString(country, 16),
		Codec:       SanitizeString(codec, 16),
		Status:      SanitizeString(status, 16),
		DJPass:      s.djPass,
		ClockOffset: 0,
	}
	s.currentSync = payload
	s.mu.Unlock()

	return s.broadcastSync(payload)
}

func clampBitrate(b int) int {
	if b < 0 {
		return 0
	}
	if b > maxBitrateKbps {
		return maxBitrateKbps
	}
	return b
}

func (s *PartySession) broadcastSync(payload *SyncPayload) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	pkt := &Packet{
		Type:    MsgTypeSync,
		Payload: data,
	}
	return s.mesh.Broadcast(pkt)
}

// CurrentSync returns the active playback synchronization state.
func (s *PartySession) CurrentSync() *SyncPayload {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.currentSync == nil {
		return nil
	}
	copy := *s.currentSync
	return &copy
}

// BroadcastReaction sends a live ASCII emoji reaction to the entire room.
func (s *PartySession) BroadcastReaction(reactionKeyOrEmoji string) (FloatingReaction, error) {
	emoji, ok := AllowedReactions[reactionKeyOrEmoji]
	if !ok {
		clean := SanitizeString(reactionKeyOrEmoji, 8)
		for _, val := range AllowedReactions {
			if val == clean {
				emoji = clean
				break
			}
		}
	}
	if emoji == "" {
		return FloatingReaction{}, fmt.Errorf("invalid reaction %q (press 1:🔥 2:❤️ 3:☕ 4:🚀 5:👀)", reactionKeyOrEmoji)
	}

	react := FloatingReaction{
		ID:        uuid.New().String(),
		Emoji:     emoji,
		Sender:    s.mesh.Nickname(),
		CreatedAt: time.Now(),
	}

	s.mu.Lock()
	s.reactions = append(s.reactions, react)
	if len(s.reactions) > 10 {
		s.reactions = s.reactions[len(s.reactions)-10:]
	}
	onReact := s.onReaction
	s.mu.Unlock()

	if onReact != nil {
		onReact(react)
	}

	payload, _ := json.Marshal(ReactionPayload{
		ID:        react.ID,
		Emoji:     react.Emoji,
		Timestamp: react.CreatedAt.UnixNano(),
	})
	_ = s.mesh.Broadcast(&Packet{
		Type:    MsgTypeReaction,
		Payload: payload,
	})

	return react, nil
}

// ActiveReactions returns non-expired floating reactions for rendering over the DJ visualizer.
func (s *PartySession) ActiveReactions() []FloatingReaction {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	active := make([]FloatingReaction, 0, len(s.reactions))
	for _, r := range s.reactions {
		if now.Sub(r.CreatedAt) < 3500*time.Millisecond {
			active = append(active, r)
		}
	}
	s.reactions = active
	return active
}

// BroadcastChat sends a 1-line terminal chat ping to the room.
func (s *PartySession) BroadcastChat(message string) (ChatMessage, error) {
	clean := SanitizeString(message, 120)
	if clean == "" {
		return ChatMessage{}, errors.New("empty chat message")
	}

	chat := ChatMessage{
		ID:        uuid.New().String(),
		Sender:    s.mesh.Nickname(),
		Message:   clean,
		Timestamp: time.Now(),
	}

	s.mu.Lock()
	s.recentChat = append(s.recentChat, chat)
	if len(s.recentChat) > 10 {
		s.recentChat = s.recentChat[len(s.recentChat)-10:]
	}
	onChat := s.onChat
	s.mu.Unlock()

	if onChat != nil {
		onChat(chat)
	}

	payload, _ := json.Marshal(ChatPayload{
		ID:        chat.ID,
		Message:   chat.Message,
		Timestamp: chat.Timestamp.UnixNano(),
	})
	_ = s.mesh.Broadcast(&Packet{
		Type:    MsgTypeChat,
		Payload: payload,
	})

	return chat, nil
}

// RecentChat returns recent chat messages.
func (s *PartySession) RecentChat() []ChatMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	res := make([]ChatMessage, len(s.recentChat))
	copy(res, s.recentChat)
	return res
}

// ConnectDirect connects directly to another peer address (e.g. "192.168.1.50:4242").
// Errors are user-readable (wrong room code, version mismatch, ...) and are
// also flashed on the status line.
func (s *PartySession) ConnectDirect(addr string) error {
	_, err := s.mesh.ConnectPeer(addr)
	if err != nil {
		s.flash(fmt.Sprintf("⚠️ Could not join the party: %v", err))
	}
	return err
}

// PeerRoster returns the list of all peers sorted with host first, then by join time.
func (s *PartySession) PeerRoster() []*PeerInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	roster := make([]*PeerInfo, 0, len(s.peers))
	for _, p := range s.peers {
		copy := *p
		roster = append(roster, &copy)
	}

	sort.Slice(roster, func(i, j int) bool {
		if roster[i].IsHost != roster[j].IsHost {
			return roster[i].IsHost
		}
		if !roster[i].JoinedAt.Equal(roster[j].JoinedAt) {
			return roster[i].JoinedAt.Before(roster[j].JoinedAt)
		}
		return roster[i].ID < roster[j].ID
	})

	return roster
}

// Tick performs periodic session maintenance: reaction decay, peer liveness, host election.
func (s *PartySession) Tick() {
	s.mu.Lock()
	now := time.Now()
	self := s.mesh.PeerID()

	// Prune expired reactions
	active := make([]FloatingReaction, 0, len(s.reactions))
	for _, r := range s.reactions {
		if now.Sub(r.CreatedAt) < 3500*time.Millisecond {
			active = append(active, r)
		}
	}
	s.reactions = active

	// Drop silent peers and stale roster hints.
	var deadPeers []string
	departedHost := ""
	for id, p := range s.peers {
		if id == self {
			continue
		}
		if now.Sub(p.LastSeen) > peerTimeout {
			// A quiet host whose connection is still open is not dead: the
			// connection's idle timeout closes it if it really stopped, and
			// that runs the election (RR-C-3). This stops a slow guest from
			// getting the host voted out by delaying its heartbeats.
			if p.Verified && id == s.hostPeerID && s.mesh.IsConnected(id) {
				continue
			}
			deadPeers = append(deadPeers, id)
			if p.Verified && id == s.hostPeerID {
				departedHost = id
			}
		}
	}

	for _, id := range deadPeers {
		delete(s.peers, id)
		delete(s.peerSeq, id)
		delete(s.socialLimit, id)
	}
	s.mu.Unlock()

	if len(deadPeers) > 0 {
		s.notifyPeerChange()
	}

	if departedHost != "" {
		s.handleHostElection("host timeout", departedHost)
	}
}

func (s *PartySession) heartbeatLoop() {
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.heartbeatStop:
			return
		case <-ticker.C:
			s.Tick()
			hb := HeartbeatPayload{SentAt: time.Now().UnixNano()}
			s.mu.RLock()
			if s.isHost {
				hb.Roster = s.makeRosterPayloadLocked()
			}
			s.mu.RUnlock()
			payload, _ := json.Marshal(hb)
			_ = s.mesh.Broadcast(&Packet{
				Type:    MsgTypeHeartbeat,
				Payload: payload,
			})
		}
	}
}

// allowSocial applies the per-peer chat/reaction rate limit. Caller holds s.mu.
func (s *PartySession) allowSocialLocked(peerID string) bool {
	b, ok := s.socialLimit[peerID]
	if !ok {
		if len(s.socialLimit) >= MaxPeersLimit*2 {
			return false
		}
		b = newTokenBucket(socialRatePerSec, socialBurst)
		s.socialLimit[peerID] = b
	}
	return b.allow()
}

// setHostLocked records hostID as the room host. Caller holds s.mu.
func (s *PartySession) setHostLocked(hostID, nick string) {
	s.hostPeerID = hostID
	s.hostNickname = nick
	s.isHost = hostID != "" && hostID == s.mesh.PeerID()
	for id, p := range s.peers {
		p.IsHost = id == hostID
	}
}

// handleIncomingPacket processes a packet from an authenticated connection.
// The sender is the identity proven in the handshake, never a claimed field.
func (s *PartySession) handleIncomingPacket(pkt *Packet, from PeerConn) {
	if pkt == nil || from == nil {
		return
	}
	senderID := from.RemotePeerID()
	if senderID == "" || pkt.SenderID != senderID || senderID == s.mesh.PeerID() {
		return
	}
	senderNick := SanitizeString(pkt.SenderNick, 32)
	if senderNick == "" {
		senderNick = "dj-" + senderID[:4]
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	peer, exists := s.peers[senderID]
	if !exists || !peer.Verified {
		now := time.Now()
		peer = &PeerInfo{ID: senderID, JoinedAt: now, Verified: true}
		s.peers[senderID] = peer
	}
	peer.LastSeen = time.Now()
	peer.Nickname = senderNick
	peer.IsHost = senderID == s.hostPeerID
	if senderID == s.hostPeerID {
		s.hostNickname = senderNick
	}
	s.mu.Unlock()

	switch pkt.Type {
	case MsgTypeHello:
		if !from.markHelloReceived() {
			return
		}
		var hp HelloPayload
		if err := json.Unmarshal(pkt.Payload, &hp); err != nil || hp.ProtocolVersion != ProtocolVersion {
			_ = from.Close()
			return
		}

		s.mu.RLock()
		ack := HelloAckPayload{
			RoomName:    s.roomName,
			HostID:      s.hostPeerID,
			HostNick:    s.hostNickname,
			DJPass:      s.djPass,
			Peers:       s.makeRosterPayloadLocked(),
			CurrentSync: s.currentSyncCopyLocked(),
		}
		s.mu.RUnlock()

		ackPayload, _ := json.Marshal(ack)
		_ = s.mesh.SendDirect(from, &Packet{
			Type:    MsgTypeHelloAck,
			Payload: ackPayload,
		})
		s.notifyPeerChange()

	case MsgTypeHelloAck:
		// Only the answer to our own Hello on a connection we dialed, once.
		if !from.consumeHelloAck() {
			return
		}
		var ack HelloAckPayload
		if err := json.Unmarshal(pkt.Payload, &ack); err != nil {
			return
		}
		if !ValidDJPass(ack.DJPass) {
			return
		}
		s.mu.Lock()
		// Learn the host only from the host itself, and never replace a host
		// we already follow (that needs an authenticated migration).
		// With a pin, only the pinned identity can be learned as host.
		if s.isHost || ack.HostID != senderID || (s.hostPeerID != "" && s.hostPeerID != senderID) ||
			(s.hostPin != "" && PeerFingerprint(senderID) != s.hostPin) {
			s.mu.Unlock()
			s.rejectedHostClaims.Add(1)
			return
		}
		if name := SanitizeString(ack.RoomName, 48); name != "" {
			s.roomName = name
		}
		s.djPass = ack.DJPass
		s.setHostLocked(senderID, senderNick)
		s.applyRosterHintsLocked(ack.Peers)
		s.mu.Unlock()

		if ack.CurrentSync != nil {
			s.applySync(ack.CurrentSync, senderID, pkt.Seq)
		}
		s.notifyPeerChange()

	case MsgTypeSync:
		var syncPayload SyncPayload
		if err := json.Unmarshal(pkt.Payload, &syncPayload); err == nil {
			s.applySync(&syncPayload, senderID, pkt.Seq)
		}

	case MsgTypeReaction:
		var rp ReactionPayload
		if err := json.Unmarshal(pkt.Payload, &rp); err == nil {
			cleanEmoji := SanitizeString(rp.Emoji, 8)
			isValid := false
			for _, val := range AllowedReactions {
				if val == cleanEmoji {
					isValid = true
					break
				}
			}
			if !isValid {
				return
			}

			react := FloatingReaction{
				ID:        uuid.New().String(),
				Emoji:     cleanEmoji,
				Sender:    senderNick,
				CreatedAt: time.Now(),
			}
			s.mu.Lock()
			if !s.allowSocialLocked(senderID) {
				s.mu.Unlock()
				return
			}
			s.reactions = append(s.reactions, react)
			if len(s.reactions) > 10 {
				s.reactions = s.reactions[len(s.reactions)-10:]
			}
			onReact := s.onReaction
			s.mu.Unlock()

			if onReact != nil {
				onReact(react)
			}
		}

	case MsgTypeChat:
		var cp ChatPayload
		if err := json.Unmarshal(pkt.Payload, &cp); err == nil {
			cleanMsg := SanitizeString(cp.Message, 120)
			if cleanMsg == "" {
				return
			}

			chat := ChatMessage{
				ID:        uuid.New().String(),
				Sender:    senderNick,
				Message:   cleanMsg,
				Timestamp: time.Now(),
			}
			s.mu.Lock()
			if !s.allowSocialLocked(senderID) {
				s.mu.Unlock()
				return
			}
			s.recentChat = append(s.recentChat, chat)
			if len(s.recentChat) > 10 {
				s.recentChat = s.recentChat[len(s.recentChat)-10:]
			}
			onChat := s.onChat
			s.mu.Unlock()

			if onChat != nil {
				onChat(chat)
			}
		}

	case MsgTypeHeartbeat:
		var hp HeartbeatPayload
		if err := json.Unmarshal(pkt.Payload, &hp); err != nil {
			return
		}
		if len(hp.Roster) > 0 {
			s.mu.Lock()
			if senderID == s.hostPeerID && !s.isHost {
				s.applyRosterHintsLocked(hp.Roster)
			}
			s.mu.Unlock()
		}
		ackPayload, _ := json.Marshal(HeartbeatPayload{
			SentAt: hp.SentAt,
		})
		_ = s.mesh.SendDirect(from, &Packet{
			Type:    MsgTypeHeartbeatAck,
			Payload: ackPayload,
		})

	case MsgTypeHeartbeatAck:
		var hp HeartbeatPayload
		if err := json.Unmarshal(pkt.Payload, &hp); err == nil && hp.SentAt > 0 {
			rtt := time.Since(time.Unix(0, hp.SentAt)).Milliseconds()
			if rtt < 0 || rtt > 60000 {
				return
			}
			s.mu.Lock()
			if p, ok := s.peers[senderID]; ok {
				p.PingMs = rtt
			}
			s.mu.Unlock()
		}

	case MsgTypeHostMigrate:
		var hmp HostMigratePayload
		if err := json.Unmarshal(pkt.Payload, &hmp); err != nil {
			return
		}
		s.handleHostMigrate(senderID, hmp)

	case MsgTypeLeave:
		// A peer can only announce its own departure. Closing the connection
		// makes the mesh remove it, which runs handlePeerLeft once.
		_ = from.Close()
	}
}

// handleHostMigrate accepts a host change only when:
//   - it comes from the current (verified) host and names this peer or a
//     verified, directly connected peer; or
//   - the current host is gone, the sender names itself, and the sender is
//     the winner of this peer's own election over verified, connected peers.
func (s *PartySession) handleHostMigrate(senderID string, hmp HostMigratePayload) {
	self := s.mesh.PeerID()
	newID := hmp.NewHostID

	s.mu.Lock()
	accept := false
	if s.hostPeerID != "" && senderID == s.hostPeerID {
		if newID == self {
			accept = true
		} else if p, ok := s.peers[newID]; ok && p.Verified && s.mesh.IsConnected(newID) {
			accept = true
		}
	} else if newID == senderID && !s.isHost && s.hostPin == "" {
		// The host is gone only when a known host's connection is closed
		// (FR-C-2: a guest that has no host yet accepts no self-nomination).
		// A quiet but open connection is closed by the idle read timeout if
		// it is dead. A pinned session never accepts a self-nominated host.
		hostGone := s.hostPeerID != "" && !s.mesh.IsConnected(s.hostPeerID)
		if hostGone && s.electionWinnerLocked(s.hostPeerID) == senderID {
			accept = true
		}
	}
	if !accept {
		s.mu.Unlock()
		s.rejectedHostClaims.Add(1)
		return
	}
	if s.hostPin != "" {
		// An authenticated hand-over from the trusted host moves the pin.
		s.hostPin = PeerFingerprint(newID)
	}
	nick := ""
	if p, ok := s.peers[newID]; ok {
		nick = p.Nickname
	}
	if newID == self {
		nick = s.mesh.Nickname()
	}
	s.setHostLocked(newID, nick)
	s.mu.Unlock()

	s.flash(fmt.Sprintf("👑 Host migrated to @%s", nick))
	s.notifyPeerChange()
}

var validSyncStatus = map[string]bool{
	"":           true,
	"playing":    true,
	"paused":     true,
	"stopped":    true,
	"connecting": true,
	"error":      true,
}

// applySync validates and applies a playback sync from a verified peer.
// The host may always sync; other verified peers only in Open Democracy, and
// they can never change the DJ Pass. Stream URLs must be http(s) with a host.
func (s *PartySession) applySync(syncPayload *SyncPayload, senderID string, seq uint64) {
	if syncPayload == nil || senderID == "" || senderID == s.mesh.PeerID() {
		return
	}

	s.mu.Lock()
	// 1. Per-identity sequence: strictly increasing, only advanced after the
	// sync is authorised (an unauthorised packet cannot poison the counter).
	if seq == 0 || seq <= s.peerSeq[senderID] {
		s.mu.Unlock()
		return
	}

	// 2. Authorise the verified sender.
	fromHost := s.hostPeerID != "" && senderID == s.hostPeerID
	peer, known := s.peers[senderID]
	if !known || !peer.Verified {
		s.mu.Unlock()
		return
	}
	if fromHost {
		if !ValidDJPass(syncPayload.DJPass) {
			s.mu.Unlock()
			return
		}
	} else {
		if s.djPass != DJPassOpenDemocracy {
			s.mu.Unlock()
			return
		}
		syncPayload.DJPass = s.djPass
	}

	// 3. Validate and sanitise.
	if !validSyncStatus[strings.ToLower(syncPayload.Status)] {
		s.mu.Unlock()
		return
	}
	if syncPayload.StreamURL != "" && !IsAllowedStreamURL(syncPayload.StreamURL) {
		s.mu.Unlock()
		return
	}
	if strings.EqualFold(syncPayload.Status, "playing") && syncPayload.StreamURL == "" {
		s.mu.Unlock()
		return
	}
	syncPayload.StationID = SanitizeString(syncPayload.StationID, 64)
	syncPayload.StationName = SanitizeString(syncPayload.StationName, 128)
	syncPayload.Genre = SanitizeString(syncPayload.Genre, 64)
	syncPayload.Country = SanitizeString(syncPayload.Country, 16)
	syncPayload.Codec = SanitizeString(syncPayload.Codec, 16)
	syncPayload.Bitrate = clampBitrate(syncPayload.Bitrate)
	syncPayload.FromPeerID = senderID
	syncPayload.FromNick = peer.Nickname
	syncPayload.FromHost = fromHost

	s.peerSeq[senderID] = seq
	s.currentSync = syncPayload
	s.djPass = syncPayload.DJPass
	onSync := s.onPlaybackSync
	s.mu.Unlock()

	if onSync != nil {
		onSync(*syncPayload)
	}
}

func (s *PartySession) currentSyncCopyLocked() *SyncPayload {
	if s.currentSync == nil {
		return nil
	}
	c := *s.currentSync
	return &c
}

// applyRosterHintsLocked records the host's roster as display hints and as
// the succession order. Hints are capped, sanitised, never overwrite verified
// peers, and get local timestamps (peer-supplied JoinedAt is ignored).
func (s *PartySession) applyRosterHintsLocked(items []PeerRosterItem) {
	self := s.mesh.PeerID()
	now := time.Now()

	hints := 0
	for _, p := range s.peers {
		if !p.Verified {
			hints++
		}
	}

	seen := make(map[string]bool, len(items))
	order := make([]string, 0, MaxPeersLimit+1)
	for _, it := range items {
		if len(order) >= MaxPeersLimit+1 {
			break
		}
		if !isValidPeerID(it.ID) || seen[it.ID] {
			continue
		}
		seen[it.ID] = true
		order = append(order, it.ID)
		if it.ID == self || it.ID == s.hostPeerID {
			continue
		}
		nick := SanitizeString(it.Nickname, 32)
		if nick == "" {
			nick = "dj-" + it.ID[:4]
		}
		if p, ok := s.peers[it.ID]; ok {
			if !p.Verified {
				p.Nickname = nick
				p.LastSeen = now
			}
			continue
		}
		if hints >= maxRosterHints {
			continue
		}
		s.peers[it.ID] = &PeerInfo{ID: it.ID, Nickname: nick, JoinedAt: now, LastSeen: now}
		hints++
	}
	s.succession = order
}

// electionWinnerLocked ranks this peer and every verified, directly connected
// peer (except exclude) by position in the host's succession list, then by
// peer ID. Roster hints are never candidates.
func (s *PartySession) electionWinnerLocked(exclude string) string {
	self := s.mesh.PeerID()
	rank := make(map[string]int, len(s.succession))
	for i, id := range s.succession {
		rank[id] = i
	}
	candidates := []string{self}
	for id, p := range s.peers {
		if id == self || id == exclude || !p.Verified {
			continue
		}
		if !s.mesh.IsConnected(id) {
			continue
		}
		candidates = append(candidates, id)
	}
	sort.Slice(candidates, func(i, j int) bool {
		ri, iok := rank[candidates[i]]
		rj, jok := rank[candidates[j]]
		if iok != jok {
			return iok
		}
		if iok && ri != rj {
			return ri < rj
		}
		return candidates[i] < candidates[j]
	})
	return candidates[0]
}

func (s *PartySession) handleHostElection(reason, departedHost string) {
	s.mu.Lock()
	if s.closed || departedHost == "" || s.hostPeerID != departedHost {
		s.mu.Unlock()
		return
	}
	if s.hostPin != "" {
		// A pinned session follows no host it cannot verify: it stops
		// following and waits for the pinned host to come back.
		oldHostNick := s.hostNickname
		s.setHostLocked("", "")
		s.mu.Unlock()
		s.flash(fmt.Sprintf("👋 Host @%s left the room. Waiting for the host from your invite.", oldHostNick))
		s.notifyPeerChange()
		return
	}
	winner := s.electionWinnerLocked(departedHost)
	self := s.mesh.PeerID()
	isMe := winner == self
	oldHostNick := s.hostNickname
	winnerNick := s.mesh.Nickname()
	if p, ok := s.peers[winner]; ok {
		winnerNick = p.Nickname
	}
	s.setHostLocked(winner, winnerNick)
	s.mu.Unlock()

	if isMe {
		payload, _ := json.Marshal(HostMigratePayload{
			OldHostID:   departedHost,
			NewHostID:   winner,
			NewHostNick: winnerNick,
			Reason:      reason,
		})
		_ = s.mesh.Broadcast(&Packet{
			Type:    MsgTypeHostMigrate,
			Payload: payload,
		})
		s.flash(fmt.Sprintf("👑 Host @%s disconnected. You are now the room host!", oldHostNick))
	} else {
		s.flash(fmt.Sprintf("👑 Host @%s disconnected. @%s is now the room host!", oldHostNick, winnerNick))
	}

	s.notifyPeerChange()
}

// handlePeerJoined is called by the mesh for every authenticated connection.
func (s *PartySession) handlePeerJoined(peerID, nickname string) {
	if !isValidPeerID(peerID) {
		return
	}
	cleanNick := SanitizeString(nickname, 32)
	if cleanNick == "" {
		cleanNick = "dj-" + peerID[:4]
	}
	now := time.Now()
	s.mu.Lock()
	if p, exists := s.peers[peerID]; exists {
		if !p.Verified {
			p.Verified = true
			p.JoinedAt = now
		}
		p.Nickname = cleanNick
		p.LastSeen = now
	} else {
		s.peers[peerID] = &PeerInfo{
			ID:       peerID,
			Nickname: cleanNick,
			IsHost:   peerID == s.hostPeerID,
			JoinedAt: now,
			LastSeen: now,
			Verified: true,
		}
	}
	s.mu.Unlock()
	s.notifyPeerChange()
}

// handlePeerLeft is called by the mesh when an authenticated connection closes.
func (s *PartySession) handlePeerLeft(peerID string) {
	s.mu.Lock()
	p, exists := s.peers[peerID]
	if exists {
		delete(s.peers, peerID)
		delete(s.peerSeq, peerID)
		delete(s.socialLimit, peerID)
	}
	wasHost := peerID != "" && peerID == s.hostPeerID
	closed := s.closed
	s.mu.Unlock()

	if closed {
		return
	}
	if exists {
		s.flash(fmt.Sprintf("👋 @%s left the room", p.Nickname))
		s.notifyPeerChange()
	}

	if wasHost {
		s.handleHostElection("host departed", peerID)
	}
}

// makeRosterPayloadLocked lists this peer and its verified peers in join
// order, capped. Caller holds s.mu (read or write).
func (s *PartySession) makeRosterPayloadLocked() []PeerRosterItem {
	list := make([]*PeerInfo, 0, len(s.peers))
	for _, p := range s.peers {
		if p.Verified {
			list = append(list, p)
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if !list[i].JoinedAt.Equal(list[j].JoinedAt) {
			return list[i].JoinedAt.Before(list[j].JoinedAt)
		}
		return list[i].ID < list[j].ID
	})
	if len(list) > MaxPeersLimit+1 {
		list = list[:MaxPeersLimit+1]
	}
	items := make([]PeerRosterItem, 0, len(list))
	for _, p := range list {
		items = append(items, PeerRosterItem{
			ID:       p.ID,
			Nickname: p.Nickname,
			IsHost:   p.IsHost,
			JoinedAt: p.JoinedAt.UnixNano(),
			PingMs:   p.PingMs,
		})
	}
	return items
}

func (s *PartySession) notifyPeerChange() {
	s.mu.RLock()
	onPeer := s.onPeerChange
	roster := make([]*PeerInfo, 0, len(s.peers))
	for _, p := range s.peers {
		copy := *p
		roster = append(roster, &copy)
	}
	s.mu.RUnlock()

	if onPeer != nil {
		onPeer(roster)
	}
}

func (s *PartySession) flash(msg string) {
	s.mu.RLock()
	onStatus := s.onStatusFlash
	s.mu.RUnlock()

	if onStatus != nil {
		onStatus(msg)
	}
}

// Close gracefully disconnects from the mesh and releases network sockets.
func (s *PartySession) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.heartbeatStop)
	s.mu.Unlock()

	_ = s.mesh.Broadcast(&Packet{
		Type: MsgTypeLeave,
	})
	time.Sleep(30 * time.Millisecond)

	return s.mesh.Close()
}
