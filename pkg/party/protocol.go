package party

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/argon2"
)

// MsgType identifies the kind of mesh packet being transmitted.
type MsgType string

const (
	MsgTypeHello        MsgType = "hello"
	MsgTypeHelloAck     MsgType = "hello-ack"
	MsgTypeSync         MsgType = "sync"
	MsgTypeReaction     MsgType = "reaction"
	MsgTypeChat         MsgType = "chat"
	MsgTypeHeartbeat    MsgType = "heartbeat"
	MsgTypeHeartbeatAck MsgType = "heartbeat-ack"
	MsgTypeHostMigrate  MsgType = "host-migrate"
	MsgTypeLeave        MsgType = "leave"
)

// DJPassMode controls who can tune stations or modify playback state.
type DJPassMode string

const (
	DJPassHostOnly      DJPassMode = "host-only"
	DJPassOpenDemocracy DJPassMode = "open-democracy"
)

// AllowedReactions for the ASCII reaction bar (1-5).
var AllowedReactions = map[string]string{
	"1": "🔥",
	"2": "❤️",
	"3": "☕",
	"4": "🚀",
	"5": "👀",
}

// Room code character set (alphanumeric without ambiguous chars 0, O, 1, I).
const roomCodeCharset = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

// GenerateRoomCode returns a random 6-character room code (e.g. "8X2K9P").
// Fails closed by panicking if CSPRNG is compromised, preventing predictable fallbacks.
func GenerateRoomCode() string {
	b := make([]byte, 6)
	charsetLen := big.NewInt(int64(len(roomCodeCharset)))
	for i := range b {
		num, err := rand.Int(rand.Reader, charsetLen)
		if err != nil {
			panic(fmt.Sprintf("critical security failure: CSPRNG rand.Reader unavailable: %v", err))
		}
		b[i] = roomCodeCharset[num.Int64()]
	}
	return string(b)
}

// NormalizeRoomCode removes leading #, spaces, and converts to uppercase.
func NormalizeRoomCode(code string) string {
	c := strings.TrimSpace(code)
	c = strings.TrimPrefix(c, "#")
	return strings.ToUpper(c)
}

// FormatRoomCode formats a room code with the # prefix (e.g. "#8X2K9P").
func FormatRoomCode(code string) string {
	norm := NormalizeRoomCode(code)
	if norm == "" {
		return ""
	}
	return "#" + norm
}

// HashRoomCode computes a SHA-256 hex string for internal binding.
func HashRoomCode(code string) string {
	norm := NormalizeRoomCode(code)
	h := sha256.Sum256([]byte("halpradio-room-salt-v1:" + norm))
	return hex.EncodeToString(h[:8])
}

// ComputeBeaconToken calculates a time-bucketed (30s window) HMAC token for LAN discovery.
// This prevents passive eavesdroppers from rainbow-tabling or offline-cracking the room code.
func ComputeBeaconToken(key []byte, epochOffset int64) string {
	epoch := (time.Now().Unix() / 30) + epochOffset
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(fmt.Sprintf("halpradio-beacon-epoch:%d", epoch)))
	sum := mac.Sum(nil)
	return hex.EncodeToString(sum[:12])
}

// VerifyBeaconToken verifies whether a received LAN beacon token is valid for current or adjacent time window.
func VerifyBeaconToken(key []byte, token string) bool {
	for _, offset := range []int64{0, -1, 1} {
		expected := ComputeBeaconToken(key, offset)
		if hmac.Equal([]byte(expected), []byte(token)) {
			return true
		}
	}
	return false
}

// DeriveRoomKey derives a 32-byte AES-256 encryption key from the room code using Argon2id.
// Argon2id provides memory-hardness (64MB RAM, 3 iterations) to resist GPU/ASIC brute-force
// on 6-character room codes.
func DeriveRoomKey(roomCode string) ([]byte, error) {
	norm := NormalizeRoomCode(roomCode)
	if len(norm) == 0 {
		return nil, errors.New("empty room code")
	}

	salt := []byte("halpradio-party-argon2id-salt-v1")
	key := argon2.IDKey([]byte(norm), salt, 3, 64*1024, 2, 32)
	return key, nil
}

// Packet is the plaintext protocol envelope sent over the encrypted P2P data mesh.
type Packet struct {
	Type       MsgType         `json:"type"`
	Seq        uint64          `json:"seq"`
	SenderID   string          `json:"sender_id"`
	SenderNick string          `json:"sender_nick"`
	Timestamp  int64           `json:"timestamp"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// HelloPayload is sent when a peer announces itself to the room.
type HelloPayload struct {
	RoomName      string     `json:"room_name"`
	ClientVersion string     `json:"client_version"`
	ListenPort    int        `json:"listen_port,omitempty"`
	IsHost        bool       `json:"is_host"`
	DJPass        DJPassMode `json:"dj_pass"`
}

// HelloAckPayload is sent back to the connecting peer with room state.
type HelloAckPayload struct {
	RoomName    string           `json:"room_name"`
	HostID      string           `json:"host_id"`
	HostNick    string           `json:"host_nick"`
	DJPass      DJPassMode       `json:"dj_pass"`
	Peers       []PeerRosterItem `json:"peers"`
	CurrentSync *SyncPayload     `json:"current_sync,omitempty"`
}

// PeerRosterItem summarizes a connected peer.
type PeerRosterItem struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
	IsHost   bool   `json:"is_host"`
	JoinedAt int64  `json:"joined_at"`
	PingMs   int64  `json:"ping_ms"`
}

// SyncPayload synchronizes station tuning, stream URL, and playback status.
type SyncPayload struct {
	StationID   string     `json:"station_id"`
	StationName string     `json:"station_name"`
	StreamURL   string     `json:"stream_url"`
	Bitrate     int        `json:"bitrate"`
	Genre       string     `json:"genre"`
	Country     string     `json:"country"`
	Codec       string     `json:"codec"`
	Status      string     `json:"status"` // "playing", "paused", "stopped"
	DJPass      DJPassMode `json:"dj_pass"`
	ClockOffset int64      `json:"clock_offset_ns"`
}

// ReactionPayload broadcasts an interactive emoji reaction (e.g. 🔥, ❤️, ☕, 🚀, 👀).
type ReactionPayload struct {
	ID        string `json:"id"`
	Emoji     string `json:"emoji"`
	Timestamp int64  `json:"timestamp"`
}

// ChatPayload broadcasts a 1-line terminal chat ping.
type ChatPayload struct {
	ID        string `json:"id"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

// HeartbeatPayload is exchanged periodically for liveness and latency/RTT measurement.
type HeartbeatPayload struct {
	SentAt int64 `json:"sent_at"`
}

// HostMigratePayload announces a deterministic host election outcome.
type HostMigratePayload struct {
	OldHostID   string `json:"old_host_id"`
	NewHostID   string `json:"new_host_id"`
	NewHostNick string `json:"new_host_nick"`
	Reason      string `json:"reason"`
}

// SanitizeString cleans input by removing ANSI escapes, control chars, and bounding length.
// Uses []rune conversion to prevent cutting multi-byte UTF-8 emojis or characters in half.
func SanitizeString(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}

	var b strings.Builder
	b.Grow(len(s))

	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == 0x1b { // ESC
			if i+1 < len(s) {
				if s[i+1] == '[' { // CSI
					i += 2
					for i < len(s) && (s[i] < 'A' || (s[i] > 'Z' && s[i] < 'a') || s[i] > 'z') {
						i++
					}
					continue
				} else if s[i+1] == ']' { // OSC
					i += 2
					for i < len(s) {
						if s[i] == 0x07 {
							break
						}
						if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
							i++
							break
						}
						i++
					}
					continue
				} else {
					i++
					continue
				}
			}
			continue
		}

		// Strip ASCII control characters except printable
		if c < 0x20 || c == 0x7f {
			continue
		}
		b.WriteByte(c)
	}

	res := strings.TrimSpace(b.String())
	if maxLen > 0 {
		runes := []rune(res)
		if len(runes) > maxLen {
			res = string(runes[:maxLen])
		}
	}
	return strings.TrimSpace(res)
}

// EncryptPacket serializes a Packet to JSON and encrypts it using AES-256-GCM.
// Binds Authenticated Associated Data (AAD) to prevent cross-room replay attacks.
// Output: [12-byte Nonce][Ciphertext + 16-byte Auth Tag].
func EncryptPacket(key []byte, pkt *Packet, aad []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("invalid key size: must be 32 bytes for AES-256")
	}

	plaintext, err := json.Marshal(pkt)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal packet: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("failed to generate random nonce: %w", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, plaintext, aad)
	return ciphertext, nil
}

// DecryptPacket decrypts an AES-256-GCM ciphertext using the derived room key and AAD.
func DecryptPacket(key []byte, data []byte, aad []byte) (*Packet, error) {
	if len(key) != 32 {
		return nil, errors.New("invalid key size: must be 32 bytes for AES-256")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize+gcm.Overhead() {
		return nil, errors.New("ciphertext too short")
	}

	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("authentication/decryption failed (wrong room code or tampered data): %w", err)
	}

	var pkt Packet
	if err := json.Unmarshal(plaintext, &pkt); err != nil {
		return nil, fmt.Errorf("failed to unmarshal decrypted packet: %w", err)
	}

	return &pkt, nil
}

// SequenceCounter provides a thread-safe incrementing sequence generator.
type SequenceCounter struct {
	val atomic.Uint64
}

func (s *SequenceCounter) Next() uint64 {
	return s.val.Add(1)
}

func (s *SequenceCounter) Current() uint64 {
	return s.val.Load()
}
