package util

import (
	"testing"
)

func TestCopyToClipboard(t *testing.T) {
	var capturedText string
	cleanup := SetClipboardWriterForTesting(func(text string) error {
		capturedText = text
		return nil
	})
	defer cleanup()

	err := CopyToClipboard("test clipboard content")
	if err != nil {
		t.Fatalf("CopyToClipboard failed: %v", err)
	}
	if capturedText != "test clipboard content" {
		t.Errorf("Expected 'test clipboard content', got %q", capturedText)
	}
}
