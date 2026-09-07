package player

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// mpv has no stdin command channel: the old MPlayer-style "slave mode" was
// removed, and terminal input is only ever interpreted as keypresses. Text
// commands written to mpv's stdin are therefore silently discarded regardless of
// --input-terminal, which is why runtime control goes over mpv's JSON IPC
// channel (--input-ipc-server) instead. mpv is launched with --no-terminal so it
// still never reads the user's terminal or competes with the TUI for keystrokes.
const (
	mpvIPCConnectTimeout  = 5 * time.Second
	mpvIPCConnectInterval = 50 * time.Millisecond
	mpvIPCWriteTimeout    = 2 * time.Second
)

var errMPVNotConnected = errors.New("mpv IPC channel not connected")

// extControl is the runtime command channel to an external player backend. It is
// used to apply volume and mute changes to an already-running process.
type extControl interface {
	SetVolume(vol int)
	SetMute(muted bool)
	Close() error
}

// mpvIPCEndpoint is the address a single mpv process exposes its JSON IPC
// channel on, together with any teardown the address needs.
type mpvIPCEndpoint struct {
	Addr    string
	cleanup func()
}

// Cleanup releases the endpoint's address. It is safe to call more than once.
func (e *mpvIPCEndpoint) Cleanup() {
	if e == nil || e.cleanup == nil {
		return
	}
	cleanup := e.cleanup
	e.cleanup = nil
	cleanup()
}

// mpvControl talks JSON IPC to a running mpv process. mpv creates the IPC
// endpoint shortly after start, so the connection is established in the
// background; volume and mute changes made before it is up are remembered and
// flushed once it is.
type mpvControl struct {
	mu       sync.Mutex
	endpoint *mpvIPCEndpoint
	conn     io.ReadWriteCloser
	closed   bool

	wantVolume int
	haveVolume bool
	wantMute   bool
	haveMute   bool
}

func newMPVControl(endpoint *mpvIPCEndpoint) *mpvControl {
	c := &mpvControl{endpoint: endpoint}
	go c.connectLoop()
	return c
}

func (c *mpvControl) connectLoop() {
	deadline := time.Now().Add(mpvIPCConnectTimeout)
	for {
		c.mu.Lock()
		closed := c.closed
		addr := c.endpoint.Addr
		c.mu.Unlock()
		if closed {
			return
		}

		conn, err := dialMPVIPC(addr)
		if err == nil {
			c.attach(conn)
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(mpvIPCConnectInterval)
	}
}

// attach adopts an established IPC connection and replays any state the user
// changed while mpv was still starting up.
func (c *mpvControl) attach(conn io.ReadWriteCloser) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		_ = conn.Close()
		return
	}
	c.conn = conn
	vol, haveVol := c.wantVolume, c.haveVolume
	muted, haveMute := c.wantMute, c.haveMute
	c.mu.Unlock()

	// mpv replies to every command; drain the responses so its socket buffer
	// cannot fill up and block playback.
	go func() { _, _ = io.Copy(io.Discard, conn) }()

	if haveVol {
		_ = c.setProperty("volume", vol)
	}
	if haveMute {
		_ = c.setProperty("mute", muted)
	}
}

func (c *mpvControl) SetVolume(vol int) {
	c.mu.Lock()
	c.wantVolume, c.haveVolume = vol, true
	c.mu.Unlock()
	_ = c.setProperty("volume", vol)
}

func (c *mpvControl) SetMute(muted bool) {
	c.mu.Lock()
	c.wantMute, c.haveMute = muted, true
	c.mu.Unlock()
	_ = c.setProperty("mute", muted)
}

func (c *mpvControl) setProperty(name string, value any) error {
	payload, err := json.Marshal(map[string]any{"command": []any{"set_property", name, value}})
	if err != nil {
		return err
	}

	c.mu.Lock()
	conn := c.conn
	closed := c.closed
	c.mu.Unlock()
	if closed || conn == nil {
		return errMPVNotConnected
	}

	if dl, ok := conn.(interface{ SetWriteDeadline(time.Time) error }); ok {
		_ = dl.SetWriteDeadline(time.Now().Add(mpvIPCWriteTimeout))
	}
	if _, err := conn.Write(append(payload, '\n')); err != nil {
		// mpv is gone or wedged; drop the connection rather than retrying into it.
		c.mu.Lock()
		if c.conn == conn {
			c.conn = nil
		}
		c.mu.Unlock()
		_ = conn.Close()
		return fmt.Errorf("mpv IPC write failed: %w", err)
	}
	return nil
}

func (c *mpvControl) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	conn := c.conn
	c.conn = nil
	endpoint := c.endpoint
	c.mu.Unlock()

	var err error
	if conn != nil {
		err = conn.Close()
	}
	endpoint.Cleanup()
	return err
}

// mplayerControl drives mplayer through its stdin slave-mode command channel.
// This only works because mplayer is launched with -slave; without that flag
// mplayer ignores stdin commands the same way mpv does.
type mplayerControl struct {
	mu    sync.Mutex
	stdin io.WriteCloser
}

func (c *mplayerControl) SetVolume(vol int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin == nil {
		return
	}
	_, _ = fmt.Fprintf(c.stdin, "volume %d 1\n", vol)
}

func (c *mplayerControl) SetMute(muted bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin == nil {
		return
	}
	flag := 0
	if muted {
		flag = 1
	}
	_, _ = fmt.Fprintf(c.stdin, "mute %d\n", flag)
}

func (c *mplayerControl) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin == nil {
		return nil
	}
	err := c.stdin.Close()
	c.stdin = nil
	return err
}
