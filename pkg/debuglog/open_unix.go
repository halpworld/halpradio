//go:build !windows

package debuglog

import (
	"os"
	"syscall"
)

func openLogFile(name string) (*os.File, error) {
	return os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0o600)
}

// ownedByCurrentUser reports whether fi is ours and has exactly one link.
// A second link means the name may be a hard link planted (for example in a
// shared directory) to make the log append to, chmod or rotate another file.
func ownedByCurrentUser(fi os.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Uid == uint32(os.Getuid()) && st.Nlink == 1
}
