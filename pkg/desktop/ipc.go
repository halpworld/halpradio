package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/halpworld/halpradio/pkg/util"
)

// SanitizeString strips ASCII control characters (including ANSI CSI & OSC escape codes) and limits max length.
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
				next := s[i+1]
				if next == '[' { // CSI sequence: \x1b[ ... [A-Za-z]
					i += 2
					for i < len(s) && (s[i] < 'A' || (s[i] > 'Z' && s[i] < 'a') || s[i] > 'z') {
						i++
					}
					continue
				} else if next == ']' { // OSC sequence: \x1b] ... (BEL \x07 or ST \x1b\\)
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
					// 2-byte escape sequence: \x1b<char>
					i++
					continue
				}
			}
			continue
		}

		// Strip ASCII control characters (0x00 - 0x1F except printable, and 0x7F DEL)
		if c < 0x20 || c == 0x7f {
			continue
		}
		b.WriteByte(c)
	}

	res := b.String()
	if maxLen > 0 && len(res) > maxLen {
		res = res[:maxLen]
	}
	return strings.TrimSpace(res)
}

// PlaybackInfo holds snapshot state for remote queries.
type PlaybackInfo struct {
	Status      string `json:"status"`
	StationID   string `json:"station_id,omitempty"`
	StationName string `json:"station_name,omitempty"`
	Station     string `json:"station,omitempty"` // For backwards compatibility
	Artist      string `json:"artist,omitempty"`
	Title       string `json:"title,omitempty"`
	Track       string `json:"track,omitempty"` // For backwards compatibility
	Bitrate     int    `json:"bitrate,omitempty"`
	Volume      int    `json:"volume"`
	Muted       bool   `json:"muted,omitempty"`
	Backend     string `json:"backend,omitempty"`
	Visualizer  string `json:"visualizer,omitempty"`
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
	Action string `json:"action"`
}

// IPCResponse represents the result of an IPC command.
type IPCResponse struct {
	Success bool          `json:"success"`
	Message string        `json:"message"`
	Status  *PlaybackInfo `json:"status,omitempty"`
}

// ActionHandler processes an action and returns the current playback state.
type ActionHandler func(action MediaAction) (*PlaybackInfo, error)

// IPCServer provides local Unix socket IPC for CLI and media key control.
type IPCServer struct {
	mu         sync.Mutex
	listener   net.Listener
	socketPath string
	handler    ActionHandler
	closed     bool
}

// GetDefaultSocketPath returns the standard path for the halpradio IPC socket.
func GetDefaultSocketPath() string {
	configDir := util.GetConfigDir()
	if configDir != "" {
		_ = os.MkdirAll(configDir, 0700)
		return filepath.Join(configDir, "ipc.sock")
	}

	tempDir := os.TempDir()
	if runtime.GOOS != "windows" {
		return filepath.Join(tempDir, fmt.Sprintf("halpradio-%d.sock", os.Getuid()))
	}
	return filepath.Join(tempDir, "halpradio-ipc.sock")
}

// StartIPCServer starts listening on the specified socket path or default.
func StartIPCServer(socketPath string, handler ActionHandler) (*IPCServer, error) {
	if socketPath == "" {
		socketPath = GetDefaultSocketPath()
	}

	// Remove stale socket if present
	_ = os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on socket %s: %w", socketPath, err)
	}

	// Restrict socket permissions so only the owner process can connect
	_ = os.Chmod(socketPath, 0600)

	server := &IPCServer{
		listener:   listener,
		socketPath: socketPath,
		handler:    handler,
	}

	go server.serve()

	return server, nil
}

func (s *IPCServer) serve() {
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed {
				return
			}
			continue
		}

		go s.handleConn(conn)
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

	cleanAction := SanitizeString(req.Action, 64)
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
	handler := s.handler
	s.mu.Unlock()

	var pInfo *PlaybackInfo
	var err error
	if handler != nil {
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
	if s.socketPath != "" {
		_ = os.Remove(s.socketPath)
	}
	return err
}

// SendIPCCommand sends a command to the running halpradio IPC socket.
func SendIPCCommand(socketPath string, action string) (*IPCResponse, error) {
	if socketPath == "" {
		socketPath = GetDefaultSocketPath()
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
	if runtime.GOOS != "windows" {
		if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
			if stat.Uid != uint32(os.Getuid()) {
				return nil, fmt.Errorf("socket %s is not owned by current user (security violation)", socketPath)
			}
		}
	}

	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to halpradio socket: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	cleanAction := SanitizeString(action, 64)
	req := IPCRequest{Action: cleanAction}
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
