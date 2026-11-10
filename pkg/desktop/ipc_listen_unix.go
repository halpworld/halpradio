//go:build !windows

package desktop

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
)

// listenPrivate binds the socket inside a fresh 0700 directory (so it is
// never reachable with a permissive mode), sets 0600 on it, and then
// hard-links it to socketPath. link(2) fails if socketPath appeared in the
// meantime, so a concurrent instance is never replaced.
func listenPrivate(socketPath string) (net.Listener, error) {
	tmpDir, err := makePrivateBindDir(filepath.Dir(socketPath))
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	tmpSock := filepath.Join(tmpDir, "s")
	l, err := net.Listen("unix", tmpSock)
	if err != nil {
		return nil, err
	}
	if ul, ok := l.(*net.UnixListener); ok {
		// The bound name is temporary; Close removes the final path itself.
		ul.SetUnlinkOnClose(false)
	}
	if err := os.Chmod(tmpSock, 0600); err != nil {
		_ = l.Close()
		return nil, err
	}
	if err := os.Link(tmpSock, socketPath); err != nil {
		_ = l.Close()
		return nil, err
	}
	return l, nil
}

// makePrivateBindDir creates a 0700 directory with a short random name
// ("/.xxxx/s" is no longer than "/ipc.sock"), because sun_path is limited to
// about 104 bytes and the bind path must fit wherever the final path fits.
func makePrivateBindDir(parent string) (string, error) {
	for i := 0; i < 16; i++ {
		var b [2]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		dir := filepath.Join(parent, "."+hex.EncodeToString(b[:]))
		err := os.Mkdir(dir, 0o700)
		if err == nil {
			return dir, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", errors.New("could not create a private bind directory")
}

// checkSocketDir requires the socket's directory to be owned by the current
// user and not writable by group or others, so no other user can swap the
// socket path.
func checkSocketDir(dir string) error {
	fi, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("socket directory %s: %w", dir, err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("socket directory %s is not a directory", dir)
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("socket directory %s is not owned by the current user", dir)
	}
	if fi.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("socket directory %s is writable by group or others (%v)", dir, fi.Mode().Perm())
	}
	return nil
}
