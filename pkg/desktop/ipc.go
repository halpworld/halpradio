package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/halpworld/halpradio/pkg/util"
)

// SanitizeString makes untrusted text safe for terminals, IPC payloads and
// Discord fields. ESC-introduced sequences (CSI, OSC, two-byte escapes) are
// removed whole so their parameters do not leak as text; everything else is
// delegated to util.SanitizeDisplay (C0/C1 controls, DEL, bidi and
// zero-width format runes dropped, whitespace collapsed, capped at maxLen
// runes).
func SanitizeString(s string, maxLen int) string {
	return util.SanitizeDisplay(stripEscapeSequences(s), maxLen)
}

// stripEscapeSequences removes 7-bit ESC-introduced control sequences.
func stripEscapeSequences(s string) string {
	if !strings.Contains(s, "\x1b") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != 0x1b {
			b.WriteByte(c)
			continue
		}
		if i+1 >= len(s) {
			continue
		}
		switch s[i+1] {
		case '[': // CSI: ESC [ params final-byte
			i += 2
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
		case ']', 'P', '_', '^': // OSC / DCS / APC / PM: up to BEL or ST
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
		default: // two-byte escape
			i++
		}
	}
	return b.String()
}

// PartyInfo holds snapshot state for party listening rooms.
type PartyInfo struct {
	Active    bool     `json:"active"`
	RoomCode  string   `json:"room_code,omitempty"`
	RoomName  string   `json:"room_name,omitempty"`
	IsHost    bool     `json:"is_host"`
	Host      string   `json:"host,omitempty"`
	DJPass    string   `json:"dj_pass,omitempty"`
	Listeners int      `json:"listeners"`
	Peers     []string `json:"peers,omitempty"`
}

// PlaybackInfo holds snapshot state for remote queries.
type PlaybackInfo struct {
	Status      string     `json:"status"`
	StationID   string     `json:"station_id,omitempty"`
	StationName string     `json:"station_name,omitempty"`
	Station     string     `json:"station,omitempty"` // For backwards compatibility
	Artist      string     `json:"artist,omitempty"`
	Title       string     `json:"title,omitempty"`
	Track       string     `json:"track,omitempty"` // For backwards compatibility
	Bitrate     int        `json:"bitrate,omitempty"`
	Volume      int        `json:"volume"`
	Muted       bool       `json:"muted,omitempty"`
	Backend     string     `json:"backend,omitempty"`
	Visualizer  string     `json:"visualizer,omitempty"`
	Party       *PartyInfo `json:"party,omitempty"`
}

// SplitArtistTitle extracts artist and song title from a standard "Artist - Title" track string.
func SplitArtistTitle(track string) (artist, title string) {
	t := strings.TrimSpace(track)
	if t == "" {
		return "", ""
	}

	if strings.Contains(t, " - ") {
		parts := strings.SplitN(t, " - ", 2)
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	if strings.Contains(t, " — ") {
		parts := strings.SplitN(t, " — ", 2)
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	if strings.Contains(t, " – ") {
		parts := strings.SplitN(t, " – ", 2)
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}

	return "", t
}

// IPCRequest represents an incoming command over IPC.
type IPCRequest struct {
	Action  string `json:"action"`
	Payload string `json:"payload,omitempty"`
}

// IPCResponse represents the result of an IPC command.
type IPCResponse struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Status  *PlaybackInfo `json:"status,omitempty"`
}

// ActionHandler processes an action and returns the current playback state.
type ActionHandler func(action MediaAction) (*PlaybackInfo, error)

// PayloadActionHandler processes an action with optional payload and returns the current playback state.
type PayloadActionHandler func(action MediaAction, payload string) (*PlaybackInfo, error)

// ErrIPCAlreadyRunning is returned by StartIPCServer when another live
// halpradio instance already answers on the socket path. The existing socket is
// left alone instead of being unlinked and taken over.
var ErrIPCAlreadyRunning = errors.New("another halpradio instance is already listening on the IPC socket")

// maxIPCConns bounds concurrently served IPC connections.
const maxIPCConns = 16

// IPCServer provides local Unix socket IPC for CLI and media key control.
type IPCServer struct {
	mu             sync.Mutex
	listener       net.Listener
	socketPath     string
	socketInfo     os.FileInfo // identity of the socket we created, for Close
	handler        ActionHandler
	payloadHandler PayloadActionHandler
	closed         bool
	sem            chan struct{}
}

// GetDefaultSocketPath returns the standard path for the halpradio IPC socket,
// or "" when no per-user config directory is available. There is deliberately
// no fallback to a shared temp directory.
func GetDefaultSocketPath() string {
	configDir := util.GetConfigDir()
	if configDir == "" {
		return ""
	}
	_ = os.MkdirAll(configDir, 0700)
	return filepath.Join(configDir, "ipc.sock")
}

// prepareSocketPath makes socketPath free for a new listener without ever
// hijacking a live instance: a socket that accepts a connection means another
// halpradio is running (ErrIPCAlreadyRunning); a dead socket owned by the
// current user is removed; anything else (regular file, symlink, foreign
// owner) is refused.
func prepareSocketPath(socketPath string) error {
	fi, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect socket %s: %w", socketPath, err)
	}
	if fi.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("refusing to replace %s: not a socket (%s)", socketPath, fi.Mode().Type())
	}
	if !isOwnedByCurrentUser(fi) {
		return fmt.Errorf("refusing to replace %s: socket is owned by another user", socketPath)
	}
	if conn, err := net.DialTimeout("unix", socketPath, 200*time.Millisecond); err == nil {
		_ = conn.Close()
		return ErrIPCAlreadyRunning
	}
	if err := os.Remove(socketPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove stale socket %s: %w", socketPath, err)
	}
	return nil
}

// StartIPCServerWithPayload starts listening on the specified socket path with a PayloadActionHandler.
func StartIPCServerWithPayload(socketPath string, handler PayloadActionHandler) (*IPCServer, error) {
	if socketPath == "" {
		socketPath = GetDefaultSocketPath()
	}
	if socketPath == "" {
		return nil, errors.New("no per-user config directory for the IPC socket")
	}
	if err := checkSocketDir(filepath.Dir(socketPath)); err != nil {
		return nil, err
	}
	if err := prepareSocketPath(socketPath); err != nil {
		return nil, err
	}

	// listenPrivate creates the socket with owner-only permissions from the
	// start (bound inside a fresh 0700 directory, then renamed into place on
	// Unix), so there is no window with a permissive mode.
	listener, err := listenPrivate(socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on socket %s: %w", socketPath, err)
	}
	info, _ := os.Lstat(socketPath)

	server := &IPCServer{
		listener:       listener,
		socketPath:     socketPath,
		socketInfo:     info,
		payloadHandler: handler,
		sem:            make(chan struct{}, maxIPCConns),
	}

	go server.serve()

	return server, nil
}

// StartIPCServer starts listening on the specified socket path or default with standard ActionHandler.
func StartIPCServer(socketPath string, handler ActionHandler) (*IPCServer, error) {
	return StartIPCServerWithPayload(socketPath, func(action MediaAction, _ string) (*PlaybackInfo, error) {
		if handler != nil {
			return handler(action)
		}
		return nil, nil
	})
}

func (s *IPCServer) serve() {
	var backoff time.Duration
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed || errors.Is(err, net.ErrClosed) {
				return
			}
			// Persistent errors (EMFILE, ...) must not spin a core: back off
			// exponentially from 5 ms up to 1 s, as net/http does.
			if backoff == 0 {
				backoff = 5 * time.Millisecond
			} else {
				backoff *= 2
			}
			if backoff > time.Second {
				backoff = time.Second
			}
			time.Sleep(backoff)
			continue
		}
		backoff = 0

		// Only the user who owns this process may drive it.
		if err := checkPeerCredentials(conn); err != nil {
			_ = conn.Close()
			continue
		}

		select {
		case s.sem <- struct{}{}:
		default:
			_ = conn.Close() // too many concurrent clients
			continue
		}
		go func() {
			defer func() { <-s.sem }()
			s.handleConn(conn)
		}()
	}
}

func (s *IPCServer) handleConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))

	var req IPCRequest
	// Limit read payload size to 4KB to prevent unbounded memory allocation
	dec := json.NewDecoder(io.LimitReader(conn, 4096))
	if err := dec.Decode(&req); err != nil {
		resp := IPCResponse{
			Success: false,
			Message: fmt.Sprintf("invalid json payload: %v", err),
		}
		_ = json.NewEncoder(conn).Encode(resp)
		return
	}

	cleanAction := SanitizeString(req.Action, 128)
	cleanPayload := SanitizeString(req.Payload, 512)

	// If no payload field was specified, but action has a space (e.g. "party-join 8X2K9P" or "party-chat Hello!"), split it
	if cleanPayload == "" && strings.Contains(cleanAction, " ") {
		parts := strings.SplitN(cleanAction, " ", 2)
		cleanAction = parts[0]
		cleanPayload = parts[1]
	}

	action, ok := ParseAction(cleanAction)
	if !ok {
		resp := IPCResponse{
			Success: false,
			Message: fmt.Sprintf("unknown action '%s'", cleanAction),
		}
		_ = json.NewEncoder(conn).Encode(resp)
		return
	}

	s.mu.Lock()
	payloadHandler := s.payloadHandler
	handler := s.handler
	s.mu.Unlock()

	var pInfo *PlaybackInfo
	var err error
	if payloadHandler != nil {
		pInfo, err = payloadHandler(action, cleanPayload)
	} else if handler != nil {
		pInfo, err = handler(action)
	}

	if err != nil {
		resp := IPCResponse{
			Success: false,
			Message: err.Error(),
			Status:  pInfo,
		}
		_ = json.NewEncoder(conn).Encode(resp)
		return
	}

	resp := IPCResponse{
		Success: true,
		Message: fmt.Sprintf("executed action: %s", action),
		Status:  pInfo,
	}
	_ = json.NewEncoder(conn).Encode(resp)
}

// Close terminates the IPC listener and removes the socket file.
func (s *IPCServer) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true

	var err error
	if s.listener != nil {
		err = s.listener.Close()
	}
	// Remove the path only if it is still the socket this server created,
	// so closing never unlinks another instance's socket.
	if s.socketPath != "" {
		if cur, statErr := os.Lstat(s.socketPath); statErr == nil && s.socketInfo != nil && os.SameFile(cur, s.socketInfo) {
			_ = os.Remove(s.socketPath)
		}
	}
	return err
}

// SendIPCCommand sends a command to the running halpradio IPC socket.
func SendIPCCommand(socketPath string, action string) (*IPCResponse, error) {
	return SendIPCCommandWithPayload(socketPath, action, "")
}

// SendIPCCommandWithPayload sends a command with payload to the running halpradio IPC socket.
func SendIPCCommandWithPayload(socketPath string, action string, payload string) (*IPCResponse, error) {
	if socketPath == "" {
		socketPath = GetDefaultSocketPath()
	}
	if socketPath == "" {
		return nil, errors.New("halpradio is not currently running (no config directory)")
	}

	fi, err := os.Lstat(socketPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("halpradio is not currently running (socket %s not found)", socketPath)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to access socket %s: %w", socketPath, err)
	}

	// Security: Reject symlinks to prevent symlink attacks on socket paths
	if fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("insecure socket path: %s is a symlink", socketPath)
	}

	// Security: Verify socket file ownership on Unix systems
	if !isOwnedByCurrentUser(fi) {
		return nil, fmt.Errorf("socket %s is not owned by current user (security violation)", socketPath)
	}

	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to halpradio socket: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	cleanAction := SanitizeString(action, 128)
	cleanPayload := SanitizeString(payload, 512)
	req := IPCRequest{Action: cleanAction, Payload: cleanPayload}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	var resp IPCResponse
	// Limit response decode size to 64KB
	if err := json.NewDecoder(io.LimitReader(conn, 65536)).Decode(&resp); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("received empty response from halpradio")
		}
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &resp, nil
}
