package party

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/halpworld/halpradio/pkg/util"
	"golang.org/x/crypto/argon2"
)

// ProtocolVersion is the party wire protocol spoken by this build. Version 2
// introduced per-peer ed25519 identities, an authenticated handshake with
// per-connection keys, and 16-character room codes. Version 1 peers cannot
// talk to version 2 peers: the handshake rejects them with a readable error.
const ProtocolVersion = 2

// protocolName is sent in the cleartext handshake preamble so a v2 peer can
// tell a halpradio party peer apart from anything else that connects.
const protocolName = "halpradio-party"

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

// ValidDJPass reports whether mode is one of the known DJ Pass values.
// Anything else received from a peer must be rejected, never stored.
func ValidDJPass(mode DJPassMode) bool {
	return mode == DJPassHostOnly || mode == DJPassOpenDemocracy
}

// AllowedReactions for the ASCII reaction bar (1-5).
var AllowedReactions = map[string]string{
	"1": "🔥",
	"2": "❤️",
	"3": "☕",
	"4": "🚀",
	"5": "👀",
}

// Room codes.
//
// A room code is 16 symbols from a 32-symbol alphabet that leaves out the
// look-alike characters 0, O, 1 and I, written in four groups of four
// (e.g. "7K9P-2XQM-HF4D-WNR8"). That is 80 bits from crypto/rand. The code is
// the room's only shared secret: it is never sent over the network, and the
// room key is derived from it with Argon2id. The first group also selects the
// Argon2id salt, so every room gets its own salt instead of one global constant.
const (
	roomCodeCharset    = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
	roomCodeLen        = 16
	roomCodeGroupLen   = 4
	legacyRoomCodeLen  = 6
	minRoomCodeSymbols = 6 // distinct symbols; rejects "2222-2222-..." style custom codes
	// minNewRoomCodeSymbols is the stricter rule for a code chosen at room
	// creation (FR-C-8). Joining never applies it, so existing rooms stay reachable.
	minNewRoomCodeSymbols = 8
)

// ExampleRoomCode is a correctly formatted code for help text and tests.
// Never use it for a real room.
const ExampleRoomCode = "7K9P-2XQM-HF4D-WNR8"

// ErrLegacyRoomCode is returned for 6-character codes from party protocol v1.
var ErrLegacyRoomCode = errors.New("this is a 6-character room code from an older halpradio (party protocol v1); " +
	"rooms now use 16-character codes such as " + ExampleRoomCode + ". " +
	"Everyone in the room needs the same, updated halpradio: ask the host to update and share the new code")

// GenerateRoomCode returns a random room code in canonical grouped form
// (e.g. "7K9P-2XQM-HF4D-WNR8"). It panics if the CSPRNG fails, so it can never
// fall back to a predictable code.
func GenerateRoomCode() string {
	charsetLen := big.NewInt(int64(len(roomCodeCharset)))
	for {
		b := make([]byte, roomCodeLen)
		for i := range b {
			num, err := rand.Int(rand.Reader, charsetLen)
			if err != nil {
				panic(fmt.Sprintf("critical security failure: CSPRNG rand.Reader unavailable: %v", err))
			}
			b[i] = roomCodeCharset[num.Int64()]
		}
		code := groupRoomCode(string(b))
		if ValidateNewRoomCode(code) == nil {
			return code
		}
	}
}

// stripRoomCode uppercases the input and removes the leading '#', spaces and
// group separators, leaving only the symbols the user typed.
func stripRoomCode(code string) string {
	c := strings.TrimSpace(code)
	c = strings.TrimPrefix(c, "#")
	c = strings.ToUpper(c)
	return strings.Map(func(r rune) rune {
		if r == '-' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, c)
}

func groupRoomCode(symbols string) string {
	if len(symbols) != roomCodeLen {
		return symbols
	}
	parts := make([]string, 0, roomCodeLen/roomCodeGroupLen)
	for i := 0; i < roomCodeLen; i += roomCodeGroupLen {
		parts = append(parts, symbols[i:i+roomCodeGroupLen])
	}
	return strings.Join(parts, "-")
}

// NormalizeRoomCode removes a leading '#', spaces and dashes, uppercases the
// code and, when it has the right length, returns the canonical grouped form
// ("7K9P-2XQM-HF4D-WNR8"). Use ValidateRoomCode to check the result.
func NormalizeRoomCode(code string) string {
	return groupRoomCode(stripRoomCode(code))
}

// ValidateRoomCode checks that code is a well-formed v2 room code. The error
// text is written for end users.
func ValidateRoomCode(code string) error {
	s := stripRoomCode(code)
	if s == "" {
		return errors.New("room code is empty")
	}
	for _, r := range s {
		if r > unicode.MaxASCII || !strings.ContainsRune(roomCodeCharset, r) {
			return fmt.Errorf("room code contains %q, which is not used in room codes (codes never contain 0, 1, O or I)", r)
		}
	}
	if len(s) == legacyRoomCodeLen {
		return ErrLegacyRoomCode
	}
	if len(s) != roomCodeLen {
		return fmt.Errorf("room code must have 16 characters, e.g. %s (got %d)", ExampleRoomCode, len(s))
	}
	seen := make(map[rune]bool, len(s))
	for _, r := range s {
		seen[r] = true
	}
	if len(seen) < minRoomCodeSymbols {
		return errors.New("room code is too predictable; leave the code out and let halpradio generate one")
	}
	return nil
}

// ValidateNewRoomCode checks a code for a room being created: it must pass
// ValidateRoomCode and use at least 8 distinct symbols, because hand-picked
// patterns are much easier to guess offline from a LAN beacon. Use
// ValidateRoomCode, not this, when joining.
func ValidateNewRoomCode(code string) error {
	if err := ValidateRoomCode(code); err != nil {
		return err
	}
	seen := make(map[rune]bool, roomCodeLen)
	for _, r := range stripRoomCode(code) {
		seen[r] = true
	}
	if len(seen) < minNewRoomCodeSymbols {
		return errors.New("room code is too predictable; leave the code out and let halpradio generate one")
	}
	return nil
}

// FormatRoomCode formats a room code for display with the # prefix
// (e.g. "#7K9P-2XQM-HF4D-WNR8").
func FormatRoomCode(code string) string {
	norm := NormalizeRoomCode(code)
	if norm == "" {
		return ""
	}
	return "#" + norm
}

// Argon2id cost for the room key. 64 MiB / 3 passes costs tens of
// milliseconds per guess on a laptop; with 80-bit codes an offline search is
// out of reach even at GPU speeds.
const (
	argonTime    = 3
	argonMemory  = 64 * 1024
	argonThreads = 2
)

// DeriveRoomKey derives the 32-byte room master key from a v2 room code with
// Argon2id. The salt is derived from the code's first group, so it differs per
// room; it is never transmitted. The whole code is the Argon2id password.
func DeriveRoomKey(roomCode string) ([]byte, error) {
	if err := ValidateRoomCode(roomCode); err != nil {
		return nil, err
	}
	s := stripRoomCode(roomCode)
	saltSum := sha256.Sum256([]byte("halpradio-party-v2/argon2id-salt|" + s[:roomCodeGroupLen]))
	key := argon2.IDKey([]byte("halpradio-party-v2/room-code|"+s), saltSum[:], argonTime, argonMemory, argonThreads, 32)
	return key, nil
}

// labelKey derives a sub-key from the room master key with HMAC-SHA256.
func labelKey(master []byte, label string, extra ...[]byte) []byte {
	mac := hmac.New(sha256.New, master)
	mac.Write([]byte(label))
	for _, e := range extra {
		mac.Write(e)
	}
	return mac.Sum(nil)
}

// Packet is the plaintext protocol envelope sent over an authenticated
// connection. SenderID must equal the identity proven in the handshake on
// that connection; receivers drop the connection otherwise.
type Packet struct {
	Type       MsgType         `json:"type"`
	Seq        uint64          `json:"seq"`
	SenderID   string          `json:"sender_id"`
	SenderNick string          `json:"sender_nick"`
	Timestamp  int64           `json:"timestamp"`
	Payload    json.RawMessage `json:"payload,omitempty"`
}

// HelloPayload is sent by the dialing peer right after the handshake.
type HelloPayload struct {
	ProtocolVersion int `json:"protocol_version"`
	ListenPort      int `json:"listen_port,omitempty"`
}

// HelloAckPayload answers a Hello with the room state. Receivers accept it
// only as the single answer to a Hello they sent on that connection, and take
// host / DJ Pass / sync state from it only when the sender is the host.
type HelloAckPayload struct {
	RoomName    string           `json:"room_name"`
	HostID      string           `json:"host_id"`
	HostNick    string           `json:"host_nick"`
	DJPass      DJPassMode       `json:"dj_pass"`
	Peers       []PeerRosterItem `json:"peers"`
	CurrentSync *SyncPayload     `json:"current_sync,omitempty"`
}

// PeerRosterItem summarizes a connected peer. Roster items from other peers are
// display hints only; they never become verified peers or election candidates.
type PeerRosterItem struct {
	ID       string `json:"id"`
	Nickname string `json:"nickname"`
	IsHost   bool   `json:"is_host"`
	JoinedAt int64  `json:"joined_at"`
	PingMs   int64  `json:"ping_ms"`
}

// SyncPayload synchronizes station tuning, stream URL, and playback status.
//
// FromPeerID, FromNick and FromHost are filled in locally by the receiver from
// the verified identity of the connection the sync arrived on. They are never
// read from or written to the wire.
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

	FromPeerID string `json:"-"`
	FromNick   string `json:"-"`
	FromHost   bool   `json:"-"`
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

// HeartbeatPayload is exchanged periodically for liveness and latency/RTT
// measurement. The host also sends its roster, in join order; receivers use it
// for display and as the host-succession order.
type HeartbeatPayload struct {
	SentAt int64            `json:"sent_at"`
	Roster []PeerRosterItem `json:"roster,omitempty"`
}

// HostMigratePayload announces a host change.
type HostMigratePayload struct {
	OldHostID   string `json:"old_host_id"`
	NewHostID   string `json:"new_host_id"`
	NewHostNick string `json:"new_host_nick"`
	Reason      string `json:"reason"`
}

// SanitizeString makes peer-supplied text safe to display. It delegates to
// util.SanitizeDisplay (drops C0/C1 controls, DEL, bidi and zero-width format
// runes, collapses whitespace) and caps the result at maxLen runes.
func SanitizeString(s string, maxLen int) string {
	return util.SanitizeDisplay(s, maxLen)
}

// maxStreamURLLen bounds stream URLs accepted from peers.
const maxStreamURLLen = 2048

// IsAllowedStreamURL reports whether a stream URL received from a peer may be
// handed to the player: http or https, a non-empty host, no whitespace or
// control characters, at most 2048 bytes. It does not judge whether the host
// is private or public; the player/UI decide that.
func IsAllowedStreamURL(raw string) bool {
	if raw == "" || len(raw) > maxStreamURLLen {
		return false
	}
	for _, r := range raw {
		if unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return false
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return false
	}
	return u.Host != "" && u.Hostname() != ""
}

// isValidPeerID reports whether id has the shape of a v2 peer ID
// (32 lowercase hex characters derived from an ed25519 public key).
func isValidPeerID(id string) bool {
	if len(id) != 32 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// PeerIDFromPublicKey derives a peer ID from an ed25519 public key: the first
// 16 bytes of its SHA-256, hex encoded. A peer cannot pick its ID without
// also holding the matching private key.
func PeerIDFromPublicKey(pub []byte) string {
	sum := sha256.Sum256(append([]byte("halpradio-party-v2/peer-id|"), pub...))
	return hex.EncodeToString(sum[:16])
}

// FingerprintLen is the number of symbols in a peer fingerprint (80 bits).
const FingerprintLen = 16

// ExampleFingerprint shows the fingerprint format in help and error text.
const ExampleFingerprint = "K4QZ-7HXM-2PRD-W9NB"

// PeerFingerprint returns the human-comparable fingerprint of a peer identity:
// the first 80 bits of its PeerID (a SHA-256 hash of the ed25519 public key),
// written in the room-code alphabet (no 0, 1, O or I) as four groups of four,
// e.g. "K4QZ-7HXM-2PRD-W9NB". Invalid IDs give "".
func PeerFingerprint(peerID string) string {
	if !isValidPeerID(peerID) {
		return ""
	}
	raw, err := hex.DecodeString(peerID[:20]) // 10 bytes = 80 bits
	if err != nil {
		return ""
	}
	out := make([]byte, 0, FingerprintLen)
	var acc uint64
	bits := 0
	for _, b := range raw {
		acc = acc<<8 | uint64(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out = append(out, roomCodeCharset[(acc>>uint(bits))&31])
		}
	}
	return groupRoomCode(string(out))
}

// NormalizeFingerprint returns the canonical grouped form of a fingerprint
// typed by a user (case, spaces, dashes and a leading '#' are ignored), or ""
// if it is not fingerprint-shaped.
func NormalizeFingerprint(s string) string {
	st := stripRoomCode(s)
	if len(st) != FingerprintLen {
		return ""
	}
	for _, r := range st {
		if !strings.ContainsRune(roomCodeCharset, r) {
			return ""
		}
	}
	return groupRoomCode(st)
}

// IsFingerprint reports whether s has the shape of a peer fingerprint, with
// the same tolerance as NormalizeFingerprint.
func IsFingerprint(s string) bool {
	return NormalizeFingerprint(s) != ""
}

// ContainsFingerprint reports whether s contains text that could be read as a
// fingerprint: a run of 16 or more alphabet symbols, ignoring case, spaces and
// dashes. The UI can use it to refuse nicknames that imitate a fingerprint.
func ContainsFingerprint(s string) bool {
	run := 0
	for _, r := range strings.ToUpper(s) {
		switch {
		case r == '-' || unicode.IsSpace(r):
			continue
		case r <= unicode.MaxASCII && strings.ContainsRune(roomCodeCharset, r):
			run++
			if run >= FingerprintLen {
				return true
			}
		default:
			run = 0
		}
	}
	return false
}

// invitePinSep separates the room code from the host fingerprint in an invite.
const invitePinSep = "/"

// FormatInvite returns an invite: the room code followed by the host's
// fingerprint, e.g. "7K9P-2XQM-HF4D-WNR8/K4QZ-7HXM-2PRD-W9NB". With an empty or
// invalid fingerprint it returns the bare room code.
func FormatInvite(code, hostFingerprint string) string {
	c := NormalizeRoomCode(code)
	fp := NormalizeFingerprint(hostFingerprint)
	if fp == "" {
		return c
	}
	return c + invitePinSep + fp
}

// ParseInvite accepts a bare room code (returning an empty pin) or a full
// invite (room code and host fingerprint, with or without the '/' between
// them). Case, spaces, dashes and a leading '#' are ignored, as in
// ValidateRoomCode. Anything else is an error written for end users.
func ParseInvite(s string) (code, hostPin string, err error) {
	codePart, pinPart := s, ""
	if strings.Count(s, invitePinSep) > 1 {
		return "", "", errors.New("invite must be a room code, optionally followed by '/' and the host fingerprint")
	}
	if i := strings.Index(s, invitePinSep); i >= 0 {
		codePart, pinPart = s[:i], s[i+1:]
		if strings.TrimSpace(pinPart) == "" {
			return "", "", errors.New("invite has a '/' but no host fingerprint after it")
		}
	} else if st := stripRoomCode(s); len(st) == roomCodeLen+FingerprintLen {
		codePart, pinPart = st[:roomCodeLen], st[roomCodeLen:]
	}
	if err := ValidateRoomCode(codePart); err != nil {
		return "", "", err
	}
	code = NormalizeRoomCode(codePart)
	if pinPart != "" {
		hostPin = NormalizeFingerprint(pinPart)
		if hostPin == "" {
			return "", "", fmt.Errorf("the host fingerprint in the invite must have %d characters from the room-code alphabet, e.g. %s", FingerprintLen, ExampleFingerprint)
		}
	}
	return code, hostPin, nil
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

// tokenBucket is a small rate limiter used per connection and per peer.
type tokenBucket struct {
	mu     sync.Mutex
	tokens float64
	max    float64
	rate   float64 // tokens per second
	last   time.Time
}

func newTokenBucket(ratePerSec, burst float64) *tokenBucket {
	return &tokenBucket{tokens: burst, max: burst, rate: ratePerSec, last: time.Now()}
}

func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens += now.Sub(b.last).Seconds() * b.rate
	if b.tokens > b.max {
		b.tokens = b.max
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
