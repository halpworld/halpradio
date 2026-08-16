package util

import (
	"testing"
)

func TestCopyToClipboard(t *testing.T) {
	// Call CopyToClipboard (may fail or succeed depending on CI / display environment)
	// We ensure it executes and does not panic.
	_ = CopyToClipboard("test clipboard content")
}
