//go:build !(darwin || freebsd || netbsd || openbsd || dragonfly || linux || windows)

package app

import "os"

// flushTerminalInput is a no-op where no terminal flush call is available;
// the approval prompt still requires the full word "yes".
func flushTerminalInput(_ *os.File) {}
