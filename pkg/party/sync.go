package party

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
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

// PeerInfo holds detailed runtime metadata for a connected peer.
type PeerInfo struct {
	ID       string
	Nickname string
	IsHost   bool
	JoinedAt time.Time
	LastSeen time.Time
	PingMs   int64
}

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
	currentSync *SyncPayload

	reactions  []FloatingReaction
	recentChat []ChatMessage

	onPlaybackSync func(SyncPayload)
	onReaction     func(FloatingReaction)
	onChat         func(ChatMessage)
	onPeerChange   func([]*PeerInfo)
	onStatusFlash  func(string)

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
}

// NewPartySession creates an active party session.
func NewPartySession(cfg SessionConfig) (*PartySession, error) {
	normCode := NormalizeRoomCode(cfg.RoomCode)
	if normCode == "" {
		normCode = GenerateRoomCode()
	}

	roomName := SanitizeString(cfg.RoomName, 48)
	if roomName == "" {
		roomName = "halpradio-party"
	}

	djPass := cfg.DJPass
	if djPass != DJPassHostOnly && djPass != DJPassOpenDemocracy {
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

	mesh.StartLANDiscovery()

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
		reactions:     make([]FloatingReaction, 0),
		recentChat:    make([]ChatMessage, 0),
		heartbeatStop: make(chan struct{}),
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
		PingMs:   0,
	}

	mesh.SetPacketHandler(s.handleIncomingPacket)
	mesh.SetPeerJoinedHandler(s.handlePeerJoined)
	mesh.SetPeerLeftHandler(s.handlePeerLeft)

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
	s.mu.Lock()
	if !s.isHost && s.djPass != DJPassOpenDemocracy {
		s.mu.Unlock()
		return fmt.Errorf("DJ Pass is Host Only (@%s is currently DJ)", s.hostNickname)
	}

	payload := &SyncPayload{
		StationID:   SanitizeString(stationID, 64),
		StationName: SanitizeString(stationName, 128),
		StreamURL:   SanitizeString(streamURL, 512),
		Bitrate:     bitrate,
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
func (s *PartySession) ConnectDirect(addr string) error {
	_, err := s.mesh.ConnectPeer(addr)
	return err
}

// PeerRoster returns the list of all peers sorted with host first, then alphabetically.
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
		return roster[i].JoinedAt.Before(roster[j].JoinedAt)
	})

	return roster
}

// Tick performs periodic session maintenance: reaction decay, peer liveness, host election.
func (s *PartySession) Tick() {
	s.mu.Lock()
	now := time.Now()

	// Prune expired reactions
	active := make([]FloatingReaction, 0, len(s.reactions))
	for _, r := range s.reactions {
		if now.Sub(r.CreatedAt) < 3500*time.Millisecond {
			active = append(active, r)
		}
	}
	s.reactions = active

	// Check dead peers
	var deadPeers []string
	var hostDied bool
	for id, p := range s.peers {
		if id == s.mesh.PeerID() {
			continue
		}
		if now.Sub(p.LastSeen) > 5*time.Second {
			deadPeers = append(deadPeers, id)
			if p.IsHost || id == s.hostPeerID {
				hostDied = true
			}
		}
	}

	for _, id := range deadPeers {
		delete(s.peers, id)
		delete(s.peerSeq, id)
	}
	s.mu.Unlock()

	if len(deadPeers) > 0 {
		s.notifyPeerChange()
	}

	if hostDied {
		s.handleHostElection("host timeout")
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
			payload, _ := json.Marshal(HeartbeatPayload{
				SentAt: time.Now().UnixNano(),
			})
			_ = s.mesh.Broadcast(&Packet{
				Type:    MsgTypeHeartbeat,
				Payload: payload,
			})
		}
	}
}

func (s *PartySession) handleIncomingPacket(pkt *Packet, from PeerConn) {
	if pkt == nil {
		return
	}

	// Replay protection: check timestamp sliding window (15 seconds)
	pktTime := time.Unix(0, pkt.Timestamp)
	timeDiff := time.Since(pktTime)
	if timeDiff < -5*time.Second || timeDiff > 15*time.Second {
		// Reject packets outside valid replay window
		return
	}

	senderID := SanitizeString(pkt.SenderID, 64)
	senderNick := SanitizeString(pkt.SenderNick, 32)
	if senderID == "" {
		return
	}

	// Update or register sender in roster
	s.mu.Lock()
	peer, exists := s.peers[senderID]
	if !exists {
		peer = &PeerInfo{
			ID:       senderID,
			Nickname: senderNick,
			JoinedAt: pktTime,
		}
		s.peers[senderID] = peer
	}
	peer.LastSeen = time.Now()
	peer.Nickname = senderNick
	s.mu.Unlock()

	switch pkt.Type {
	case MsgTypeHello:
		var hp HelloPayload
		_ = json.Unmarshal(pkt.Payload, &hp)

		s.mu.Lock()
		if hp.IsHost && s.hostPeerID == "" {
			s.hostPeerID = senderID
			s.hostNickname = senderNick
			peer.IsHost = true
		}
		roster := s.makeRosterPayload()
		currSync := s.currentSync
		s.mu.Unlock()

		ackPayload, _ := json.Marshal(HelloAckPayload{
			RoomName:    s.roomName,
			HostID:      s.hostPeerID,
			HostNick:    s.hostNickname,
			DJPass:      s.djPass,
			Peers:       roster,
			CurrentSync: currSync,
		})
		_ = s.mesh.SendDirect(from, &Packet{
			Type:       MsgTypeHelloAck,
			SenderID:   s.mesh.PeerID(),
			SenderNick: s.mesh.Nickname(),
			Payload:    ackPayload,
		})
		s.notifyPeerChange()

	case MsgTypeHelloAck:
		var ack HelloAckPayload
		if err := json.Unmarshal(pkt.Payload, &ack); err == nil {
			s.mu.Lock()
			s.roomName = SanitizeString(ack.RoomName, 48)
			s.hostPeerID = SanitizeString(ack.HostID, 64)
			s.hostNickname = SanitizeString(ack.HostNick, 32)
			s.djPass = ack.DJPass
			if ack.HostID == s.mesh.PeerID() {
				s.isHost = true
			}
			for _, rp := range ack.Peers {
				rpID := SanitizeString(rp.ID, 64)
				s.peers[rpID] = &PeerInfo{
					ID:       rpID,
					Nickname: SanitizeString(rp.Nickname, 32),
					IsHost:   rp.IsHost,
					JoinedAt: time.Unix(0, rp.JoinedAt),
					LastSeen: time.Now(),
					PingMs:   rp.PingMs,
				}
			}
			s.mu.Unlock()

			if ack.CurrentSync != nil {
				s.applySync(ack.CurrentSync, senderID, pkt.Seq)
			}
			s.notifyPeerChange()
		}

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
		_ = json.Unmarshal(pkt.Payload, &hp)
		ackPayload, _ := json.Marshal(HeartbeatPayload{
			SentAt: hp.SentAt,
		})
		_ = s.mesh.SendDirect(from, &Packet{
			Type:       MsgTypeHeartbeatAck,
			SenderID:   s.mesh.PeerID(),
			SenderNick: s.mesh.Nickname(),
			Payload:    ackPayload,
		})

	case MsgTypeHeartbeatAck:
		var hp HeartbeatPayload
		if err := json.Unmarshal(pkt.Payload, &hp); err == nil && hp.SentAt > 0 {
			rtt := time.Since(time.Unix(0, hp.SentAt)).Milliseconds()
			s.mu.Lock()
			if p, ok := s.peers[senderID]; ok {
				p.PingMs = rtt
			}
			s.mu.Unlock()
		}

	case MsgTypeHostMigrate:
		var hmp HostMigratePayload
		if err := json.Unmarshal(pkt.Payload, &hmp); err == nil {
			s.mu.Lock()
			// Validate migration: must come from current host OR current host timed out and
			// new host matches deterministic election winner!
			isFromCurrentHost := (senderID == s.hostPeerID)
			hostTimedOut := false
			if currHost, ok := s.peers[s.hostPeerID]; ok {
				hostTimedOut = time.Since(currHost.LastSeen) > 4*time.Second
			} else {
				hostTimedOut = true
			}

			expectedWinner := s.calcHostWinnerLocked()
			cleanNewHostID := SanitizeString(hmp.NewHostID, 64)
			cleanNewHostNick := SanitizeString(hmp.NewHostNick, 32)

			if isFromCurrentHost || (hostTimedOut && cleanNewHostID == expectedWinner.ID) {
				s.hostPeerID = cleanNewHostID
				s.hostNickname = cleanNewHostNick
				s.isHost = (cleanNewHostID == s.mesh.PeerID())
				for id, p := range s.peers {
					p.IsHost = (id == cleanNewHostID)
				}
				s.mu.Unlock()
				s.flash(fmt.Sprintf("👑 Host migrated to @%s", cleanNewHostNick))
				s.notifyPeerChange()
			} else {
				s.mu.Unlock()
				// Reject rogue host migration attempt
			}
		}

	case MsgTypeLeave:
		s.handlePeerLeft(senderID)
	}
}

func (s *PartySession) applySync(syncPayload *SyncPayload, senderID string, seq uint64) {
	s.mu.Lock()
	// 1. Sequence number validation per peer
	lastSeq := s.peerSeq[senderID]
	if seq > 0 && seq <= lastSeq {
		s.mu.Unlock()
		return
	}
	s.peerSeq[senderID] = seq

	// 2. Authorize sender according to DJ Pass policy
	if s.djPass == DJPassHostOnly && senderID != s.hostPeerID {
		s.mu.Unlock()
		// Unauthorized peer attempted to change station in HostOnly mode
		return
	}

	// 3. Ingress sanitization
	syncPayload.StationID = SanitizeString(syncPayload.StationID, 64)
	syncPayload.StationName = SanitizeString(syncPayload.StationName, 128)
	syncPayload.StreamURL = SanitizeString(syncPayload.StreamURL, 512)
	syncPayload.Genre = SanitizeString(syncPayload.Genre, 64)
	syncPayload.Country = SanitizeString(syncPayload.Country, 16)
	syncPayload.Codec = SanitizeString(syncPayload.Codec, 16)
	syncPayload.Status = SanitizeString(syncPayload.Status, 16)

	s.currentSync = syncPayload
	s.djPass = syncPayload.DJPass
	onSync := s.onPlaybackSync
	s.mu.Unlock()

	if onSync != nil {
		onSync(*syncPayload)
	}
}

func (s *PartySession) calcHostWinnerLocked() *PeerInfo {
	roster := make([]*PeerInfo, 0, len(s.peers))
	for _, p := range s.peers {
		roster = append(roster, p)
	}
	if len(roster) == 0 {
		return nil
	}
	sort.Slice(roster, func(i, j int) bool {
		if !roster[i].JoinedAt.Equal(roster[j].JoinedAt) {
			return roster[i].JoinedAt.Before(roster[j].JoinedAt)
		}
		return roster[i].ID < roster[j].ID
	})
	return roster[0]
}

func (s *PartySession) handleHostElection(reason string) {
	s.mu.Lock()
	winner := s.calcHostWinnerLocked()
	if winner == nil {
		s.mu.Unlock()
		return
	}

	isMe := (winner.ID == s.mesh.PeerID())
	oldHostNick := s.hostNickname
	s.hostPeerID = winner.ID
	s.hostNickname = winner.Nickname
	s.isHost = isMe

	for id, p := range s.peers {
		p.IsHost = (id == winner.ID)
	}
	s.mu.Unlock()

	if isMe {
		payload, _ := json.Marshal(HostMigratePayload{
			OldHostID:   "",
			NewHostID:   winner.ID,
			NewHostNick: winner.Nickname,
			Reason:      reason,
		})
		_ = s.mesh.Broadcast(&Packet{
			Type:    MsgTypeHostMigrate,
			Payload: payload,
		})
		s.flash(fmt.Sprintf("👑 Host @%s disconnected. You are now the room host!", oldHostNick))
	} else {
		s.flash(fmt.Sprintf("👑 Host @%s disconnected. @%s is now the room host!", oldHostNick, winner.Nickname))
	}

	s.notifyPeerChange()
}

func (s *PartySession) handlePeerJoined(peerID, nickname string) {
	cleanID := SanitizeString(peerID, 64)
	cleanNick := SanitizeString(nickname, 32)
	s.mu.Lock()
	if _, exists := s.peers[cleanID]; !exists {
		s.peers[cleanID] = &PeerInfo{
			ID:       cleanID,
			Nickname: cleanNick,
			JoinedAt: time.Now(),
			LastSeen: time.Now(),
		}
	}
	s.mu.Unlock()
	s.notifyPeerChange()
}

func (s *PartySession) handlePeerLeft(peerID string) {
	cleanID := SanitizeString(peerID, 64)
	s.mu.Lock()
	p, exists := s.peers[cleanID]
	if exists {
		delete(s.peers, cleanID)
		delete(s.peerSeq, cleanID)
	}
	wasHost := (cleanID == s.hostPeerID)
	s.mu.Unlock()

	if exists {
		s.flash(fmt.Sprintf("👋 @%s left the room", p.Nickname))
		s.notifyPeerChange()
	}

	if wasHost {
		s.handleHostElection("host departed")
	}
}

func (s *PartySession) makeRosterPayload() []PeerRosterItem {
	items := make([]PeerRosterItem, 0, len(s.peers))
	for _, p := range s.peers {
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
