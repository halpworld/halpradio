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
	"sync"
	"time"

	"github.com/halpworld/halpradio/pkg/util"
)

// PlaybackInfo holds snapshot state for remote queries.
type PlaybackInfo struct {
	Status  string `json:"status"`
	Station string `json:"station"`
	Track   string `json:"track"`
	Volume  int    `json:"volume"`
	Muted   bool   `json:"muted"`
	Backend string `json:"backend"`
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
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	var req IPCRequest
	dec := json.NewDecoder(conn)
	if err := dec.Decode(&req); err != nil {
		resp := IPCResponse{
			Success: false,
			Message: fmt.Sprintf("invalid json payload: %v", err),
		}
		_ = json.NewEncoder(conn).Encode(resp)
		return
	}

	action, ok := ParseAction(req.Action)
	if !ok {
		resp := IPCResponse{
			Success: false,
			Message: fmt.Sprintf("unknown action '%s'", req.Action),
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

	if _, err := os.Stat(socketPath); errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("halpradio is not currently running (socket %s not found)", socketPath)
	}

	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to halpradio socket: %w", err)
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	req := IPCRequest{Action: action}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return nil, fmt.Errorf("failed to send command: %w", err)
	}

	var resp IPCResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("received empty response from halpradio")
		}
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &resp, nil
}
