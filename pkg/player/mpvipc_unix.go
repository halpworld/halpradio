//go:build !windows

package player

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
)

// maxUnixSocketPath is a conservative bound on sockaddr_un.sun_path (104 bytes on
// darwin, 108 on Linux). An over-long path would fail to bind inside mpv, so the
// endpoint is rejected up front and playback continues without live control.
const maxUnixSocketPath = 100

func newMPVIPCEndpoint() (*mpvIPCEndpoint, error) {
	dir, err := os.MkdirTemp("", "halpradio")
	if err != nil {
		return nil, err
	}
	addr := filepath.Join(dir, "s")
	if len(addr) > maxUnixSocketPath {
		_ = os.RemoveAll(dir)
		return nil, fmt.Errorf("mpv IPC socket path %q exceeds %d bytes", addr, maxUnixSocketPath)
	}
	return &mpvIPCEndpoint{Addr: addr, cleanup: func() { _ = os.RemoveAll(dir) }}, nil
}

func dialMPVIPC(addr string) (io.ReadWriteCloser, error) {
	return net.DialTimeout("unix", addr, mpvIPCWriteTimeout)
}
