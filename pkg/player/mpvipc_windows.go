//go:build windows

package player

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
)

// On Windows mpv exposes --input-ipc-server as a named pipe rather than a unix
// socket. The pipe is owned by the mpv process, so there is nothing to clean up.
//
// The name carries 128 random bits so another local process cannot predict it
// and create the pipe first to receive mpv's control channel (PLY-8).
func newMPVIPCEndpoint() (*mpvIPCEndpoint, error) {
	name := mpvPipeName()
	if name == "" {
		return nil, fmt.Errorf("mpv ipc: no randomness for pipe name")
	}
	return &mpvIPCEndpoint{Addr: name}, nil
}

func mpvPipeName() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on supported Windows versions; if it ever
		// did, a fixed name would be predictable, so make the endpoint unusable
		// instead. newMPVIPCEndpoint's caller treats errors as "no IPC".
		return ""
	}
	return fmt.Sprintf(`\\.\pipe\halpradio-mpv-%d-%s`, os.Getpid(), hex.EncodeToString(b[:]))
}

func dialMPVIPC(addr string) (io.ReadWriteCloser, error) {
	return os.OpenFile(addr, os.O_RDWR, 0)
}
