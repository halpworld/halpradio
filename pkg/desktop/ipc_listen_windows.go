//go:build windows

package desktop

import "net"

// listenPrivate binds the AF_UNIX socket directly. On Windows access is
// governed by the ACL inherited from the per-user %AppData% directory.
func listenPrivate(socketPath string) (net.Listener, error) {
	return net.Listen("unix", socketPath)
}

// checkSocketDir is a no-op on Windows (see listenPrivate).
func checkSocketDir(string) error { return nil }
