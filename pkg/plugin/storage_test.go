package plugin

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStorageOperations(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "halpradio-storage-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	s, err := NewStorage(tempDir, "test-plugin")
	if err != nil {
		t.Fatalf("NewStorage failed: %v", err)
	}

	// Test Set & Get
	if err := s.Set("api_key", "secret123"); err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	val, found := s.Get("api_key")
	if !found || val != "secret123" {
		t.Errorf("Get(api_key) = %q, %v; want secret123, true", val, found)
	}

	// Test Persistence (re-open storage)
	s2, err := NewStorage(tempDir, "test-plugin")
	if err != nil {
		t.Fatalf("re-opening NewStorage failed: %v", err)
	}

	val2, found2 := s2.Get("api_key")
	if !found2 || val2 != "secret123" {
		t.Errorf("re-opened Get(api_key) = %q, %v; want secret123, true", val2, found2)
	}

	// Test Delete
	if err := s2.Delete("api_key"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	_, foundAfterDelete := s2.Get("api_key")
	if foundAfterDelete {
		t.Errorf("expected key to be deleted")
	}

	// Test Dir
	expectedDir := filepath.Join(tempDir, "test-plugin")
	if s.Dir() != expectedDir {
		t.Errorf("Dir() = %q; want %q", s.Dir(), expectedDir)
	}
}
