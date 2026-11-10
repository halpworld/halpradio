//go:build windows

package app

import (
	"os"

	"golang.org/x/sys/windows"
)

// flushTerminalInput drops keystrokes typed ahead of the plugin approval
// prompt, so an answer must be typed after the permissions were shown.
func flushTerminalInput(f *os.File) {
	_ = windows.FlushConsoleInputBuffer(windows.Handle(f.Fd()))
}
