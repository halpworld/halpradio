//go:build darwin || freebsd || netbsd || openbsd || dragonfly

package app

import (
	"os"

	"golang.org/x/sys/unix"
)

// flushTerminalInput drops keystrokes typed ahead of the plugin approval
// prompt, so an answer must be typed after the permissions were shown.
// TIOCSETAF re-applies the current settings after discarding pending input,
// which is tcsetattr(TCSAFLUSH).
func flushTerminalInput(f *os.File) {
	fd := int(f.Fd())
	t, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return
	}
	_ = unix.IoctlSetTermios(fd, unix.TIOCSETAF, t)
}
