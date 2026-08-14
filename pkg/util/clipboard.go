package util

import (
	"github.com/atotto/clipboard"
)

// CopyToClipboard copies a string to the OS system clipboard.
func CopyToClipboard(text string) error {
	return clipboard.WriteAll(text)
}
