//go:build !windows

package desktop

import (
	"os"
	"syscall"
)

// isOwnedByCurrentUser checks if the file is owned by the current running user.
func isOwnedByCurrentUser(fi os.FileInfo) bool {
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok {
		return stat.Uid == uint32(os.Getuid())
	}
	return true
}
