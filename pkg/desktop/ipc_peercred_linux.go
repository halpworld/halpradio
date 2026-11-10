//go:build linux

package desktop

import (
	"errors"
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

// checkPeerCredentials rejects IPC clients running as a different uid
// (SO_PEERCRED). It fails closed when the credentials cannot be read.
func checkPeerCredentials(conn net.Conn) error {
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		return errors.New("ipc: not a unix connection")
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return err
	}
	var cred *unix.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
	}); err != nil {
		return err
	}
	if credErr != nil {
		return credErr
	}
	if cred.Uid != uint32(os.Getuid()) {
		return fmt.Errorf("ipc: peer uid %d is not the current user", cred.Uid)
	}
	return nil
}
