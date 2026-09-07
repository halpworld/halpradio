//go:build windows

package player

import (
	"fmt"
	"io"
	"os"
	"sync/atomic"
)

var mpvPipeSeq atomic.Uint64

// On Windows mpv exposes --input-ipc-server as a named pipe rather than a unix
// socket. The pipe is owned by the mpv process, so there is nothing to clean up.
func newMPVIPCEndpoint() (*mpvIPCEndpoint, error) {
	name := fmt.Sprintf(`\\.\pipe\halpradio-mpv-%d-%d`, os.Getpid(), mpvPipeSeq.Add(1))
	return &mpvIPCEndpoint{Addr: name}, nil
}

func dialMPVIPC(addr string) (io.ReadWriteCloser, error) {
	return os.OpenFile(addr, os.O_RDWR, 0)
}
