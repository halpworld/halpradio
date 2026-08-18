package desktop

import (
	"encoding/binary"
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
)

const (
	DefaultDiscordClientID = "1340000000000000000"

	discordOpHandshake = 0
	discordOpFrame     = 1
	discordOpClose     = 2
	discordOpPing      = 3
	discordOpPong      = 4
)

var (
	errDiscordNotRunning = errors.New("discord ipc socket not found")
	errDiscordClosed     = errors.New("discord client is closed")
)

// DiscordActivity represents a rich presence payload to send to Discord.
type DiscordActivity struct {
	State      string
	Details    string
	LargeImage string
	LargeText  string
	SmallImage string
	SmallText  string
	StartTime  *time.Time
}

// DiscordClient defines the interface for Discord Rich Presence operations.
type DiscordClient interface {
	UpdateActivity(act DiscordActivity) error
	ClearActivity() error
	Close() error
}

// GetDiscordDJAsset maps the active visualizer mode to the corresponding Animal DJ Discord asset.
func GetDiscordDJAsset(visualizerMode string) (imageKey string, hoverText string) {
	switch strings.ToLower(strings.TrimSpace(visualizerMode)) {
	case "dj-dog", "dog", "dj_dog":
		return "dj_dog", "DJ Dog"
	case "dj-bear", "bear", "dj_bear":
		return "dj_bear", "DJ Bear"
	case "dj-frog", "frog", "dj_frog":
		return "dj_frog", "DJ Frog"
	case "dj-bunny", "bunny", "dj_bunny":
		return "dj_bunny", "DJ Bunny"
	case "bars":
		return "dj_cat", "DJ Cat (Bars)"
	case "wave":
		return "dj_cat", "DJ Cat (Wave)"
	case "spectrum":
		return "dj_cat", "DJ Cat (Spectrum)"
	case "minimal":
		return "dj_cat", "DJ Cat (Minimal)"
	case "off":
		return "dj_cat", "halpradio"
	case "dj-cat", "cat", "dj_cat", "default", "":
		return "dj_cat", "DJ Cat"
	default:
		return "dj_cat", "DJ Cat"
	}
}

type discordHandshakePayload struct {
	V        int    `json:"v"`
	ClientID string `json:"client_id"`
}

type discordPayload struct {
	Cmd   string      `json:"cmd"`
	Args  discordArgs `json:"args"`
	Nonce string      `json:"nonce"`
}

type discordArgs struct {
	PID      int              `json:"pid"`
	Activity *discordActivity `json:"activity"`
}

type discordActivity struct {
	State      string             `json:"state,omitempty"`
	Details    string             `json:"details,omitempty"`
	Timestamps *discordTimestamps `json:"timestamps,omitempty"`
	Assets     *discordAssets     `json:"assets,omitempty"`
	Buttons    []discordButton    `json:"buttons,omitempty"`
}

type discordTimestamps struct {
	Start int64 `json:"start,omitempty"`
	End   int64 `json:"end,omitempty"`
}

type discordAssets struct {
	LargeImage string `json:"large_image,omitempty"`
	LargeText  string `json:"large_text,omitempty"`
	SmallImage string `json:"small_image,omitempty"`
	SmallText  string `json:"small_text,omitempty"`
}

type discordButton struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// DiscordRPCClient manages an IPC connection to the local Discord client.
type DiscordRPCClient struct {
	mu                 sync.Mutex
	clientID           string
	conn               net.Conn
	closed             bool
	lastConnectAttempt time.Time
	customDialer       func() (net.Conn, error) // For testing
}

// NewDiscordRPCClient creates a new Discord Rich Presence client.
func NewDiscordRPCClient(clientID string) *DiscordRPCClient {
	if clientID == "" {
		clientID = DefaultDiscordClientID
	}
	return &DiscordRPCClient{
		clientID: clientID,
	}
}

// SetDialer allows overriding the socket connection dialer (useful for unit testing).
func (c *DiscordRPCClient) SetDialer(dialer func() (net.Conn, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.customDialer = dialer
}

// UpdateActivity sends a new presence state to Discord.
func (c *DiscordRPCClient) UpdateActivity(act DiscordActivity) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return errDiscordClosed
	}

	if err := c.ensureConnectedLocked(); err != nil {
		return err
	}

	var dAct *discordActivity
	if act.State != "" || act.Details != "" || act.SmallImage != "" || act.LargeImage != "" {
		cleanState := SanitizeString(act.State, 128)
		cleanDetails := SanitizeString(act.Details, 128)
		cleanLargeImg := SanitizeString(act.LargeImage, 64)
		if cleanLargeImg == "" {
			cleanLargeImg = "halpradio_logo"
		}
		cleanLargeTxt := SanitizeString(act.LargeText, 128)
		if cleanLargeTxt == "" {
			cleanLargeTxt = "halpradio - Terminal Internet Radio"
		}
		cleanSmallImg := SanitizeString(act.SmallImage, 64)
		cleanSmallTxt := SanitizeString(act.SmallText, 128)

		dAct = &discordActivity{
			State:   cleanState,
			Details: cleanDetails,
		}

		if act.StartTime != nil && !act.StartTime.IsZero() {
			dAct.Timestamps = &discordTimestamps{
				Start: act.StartTime.Unix(),
			}
		}

		dAct.Assets = &discordAssets{
			LargeImage: cleanLargeImg,
			LargeText:  cleanLargeTxt,
			SmallImage: cleanSmallImg,
			SmallText:  cleanSmallTxt,
		}

		dAct.Buttons = []discordButton{
			{
				Label: "Listen / GitHub",
				URL:   "https://github.com/halpworld/halpradio",
			},
		}
	}

	payload := discordPayload{
		Cmd: "SET_ACTIVITY",
		Args: discordArgs{
			PID:      os.Getpid(),
			Activity: dAct,
		},
		Nonce: fmt.Sprintf("%d", time.Now().UnixNano()),
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	if err := c.sendFrameLocked(discordOpFrame, data); err != nil {
		_ = c.closeConnLocked()
		return err
	}

	return nil
}

// ClearActivity clears any active Rich Presence on Discord.
func (c *DiscordRPCClient) ClearActivity() error {
	return c.UpdateActivity(DiscordActivity{})
}

// Close gracefully closes the Discord IPC connection.
func (c *DiscordRPCClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}
	c.closed = true

	if c.conn != nil {
		// Send CLOSE frame
		handshake := discordHandshakePayload{
			V:        1,
			ClientID: c.clientID,
		}
		data, _ := json.Marshal(handshake)
		_ = c.sendFrameLocked(discordOpClose, data)
		return c.closeConnLocked()
	}

	return nil
}

func (c *DiscordRPCClient) closeConnLocked() error {
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return err
	}
	return nil
}

func (c *DiscordRPCClient) ensureConnectedLocked() error {
	if c.conn != nil {
		return nil
	}

	// Backoff: do not hammer connection attempts if Discord is not running
	now := time.Now()
	if now.Sub(c.lastConnectAttempt) < 3*time.Second {
		return errDiscordNotRunning
	}
	c.lastConnectAttempt = now

	var conn net.Conn
	var err error

	if c.customDialer != nil {
		conn, err = c.customDialer()
	} else {
		conn, err = dialDiscordSocket()
	}

	if err != nil {
		return err
	}

	c.conn = conn

	// Send handshake
	handshake := discordHandshakePayload{
		V:        1,
		ClientID: c.clientID,
	}
	data, err := json.Marshal(handshake)
	if err != nil {
		_ = c.closeConnLocked()
		return err
	}

	if err := c.sendFrameLocked(discordOpHandshake, data); err != nil {
		_ = c.closeConnLocked()
		return err
	}

	// Read handshake response with short timeout
	_ = c.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	_, _, err = readDiscordFrame(c.conn)
	if err != nil {
		_ = c.closeConnLocked()
		return err
	}

	return nil
}

func (c *DiscordRPCClient) sendFrameLocked(op uint32, data []byte) error {
	if c.conn == nil {
		return errDiscordNotRunning
	}

	_ = c.conn.SetWriteDeadline(time.Now().Add(500 * time.Millisecond))

	header := make([]byte, 8)
	binary.LittleEndian.PutUint32(header[0:4], op)
	binary.LittleEndian.PutUint32(header[4:8], uint32(len(data)))

	if _, err := c.conn.Write(header); err != nil {
		return err
	}
	if _, err := c.conn.Write(data); err != nil {
		return err
	}

	return nil
}

func readDiscordFrame(r io.Reader) (uint32, []byte, error) {
	header := make([]byte, 8)
	if _, err := io.ReadFull(r, header); err != nil {
		return 0, nil, err
	}

	opcode := binary.LittleEndian.Uint32(header[0:4])
	length := binary.LittleEndian.Uint32(header[4:8])

	if length > 65536 {
		return 0, nil, fmt.Errorf("frame too large: %d bytes", length)
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return 0, nil, err
	}

	return opcode, data, nil
}

func dialDiscordSocket() (net.Conn, error) {
	if runtime.GOOS == "windows" {
		for i := 0; i < 10; i++ {
			pipePath := fmt.Sprintf(`\\.\pipe\discord-ipc-%d`, i)
			conn, err := net.DialTimeout("unix", pipePath, 250*time.Millisecond)
			if err == nil {
				return conn, nil
			}
		}
		return nil, errDiscordNotRunning
	}

	var candidateDirs []string
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		candidateDirs = append(candidateDirs, xdg)
	}
	if tmpdir := os.Getenv("TMPDIR"); tmpdir != "" {
		candidateDirs = append(candidateDirs, tmpdir)
	}
	if tmp := os.Getenv("TMP"); tmp != "" {
		candidateDirs = append(candidateDirs, tmp)
	}
	if temp := os.Getenv("TEMP"); temp != "" {
		candidateDirs = append(candidateDirs, temp)
	}
	candidateDirs = append(candidateDirs, "/tmp", os.TempDir())

	for _, dir := range candidateDirs {
		for i := 0; i < 10; i++ {
			sockPath := filepath.Join(dir, fmt.Sprintf("discord-ipc-%d", i))
			fi, err := os.Lstat(sockPath)
			if err != nil {
				continue
			}

			// Security: Reject symlinks to prevent symlink hijack attacks
			if fi.Mode()&os.ModeSymlink != 0 {
				continue
			}
			// Must be a socket
			if fi.Mode()&os.ModeSocket == 0 {
				continue
			}

			// Security: In shared temporary directories, verify socket ownership
			if runtime.GOOS != "windows" {
				if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
					if stat.Uid != uint32(os.Getuid()) {
						continue // Skip socket owned by other users
					}
				}
			}

			conn, err := net.DialTimeout("unix", sockPath, 250*time.Millisecond)
			if err == nil {
				return conn, nil
			}
		}
	}

	return nil, errDiscordNotRunning
}
