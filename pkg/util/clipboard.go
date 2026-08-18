package util

import (
	"sync"

	"github.com/atotto/clipboard"
)

// ClipboardWriter defines the function type for writing text to system clipboard.
type ClipboardWriter func(text string) error

var (
	clipboardMu     sync.RWMutex
	clipboardWriter ClipboardWriter = clipboard.WriteAll
)

// SetClipboardWriterForTesting allows mocking clipboard writes during unit and UI testing.
// It returns a cleanup function to restore the previous writer.
func SetClipboardWriterForTesting(writer ClipboardWriter) func() {
	clipboardMu.Lock()
	prev := clipboardWriter
	clipboardWriter = writer
	clipboardMu.Unlock()

	return func() {
		clipboardMu.Lock()
		clipboardWriter = prev
		clipboardMu.Unlock()
	}
}

// CopyToClipboard copies a string to the OS system clipboard using the configured writer.
func CopyToClipboard(text string) error {
	clipboardMu.RLock()
	writer := clipboardWriter
	clipboardMu.RUnlock()

	if writer == nil {
		writer = clipboard.WriteAll
	}
	return writer(text)
}
