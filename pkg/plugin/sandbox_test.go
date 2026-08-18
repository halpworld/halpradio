package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
)

func TestSandboxSecurityIsolation(t *testing.T) {
	tempHome, err := os.MkdirTemp("", "halpradio-sandbox-sec-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempHome)

	storage, err := NewStorage(tempHome, "sec-mod")
	if err != nil {
		t.Fatalf("NewStorage failed: %v", err)
	}

	var notifyMu sync.Mutex
	var notifyCalls []string

	manifest := Manifest{
		ID:          "sec-mod",
		Name:        "Security Test Module",
		Version:     "1.0.0",
		Author:      "security-tester",
		Description: "Tests sandbox bounds and host functions",
		WasmFile:    "plugin.wasm",
		Permissions: PermissionsConfig{
			Storage: []string{"local"},
			Network: []string{"example.com"},
			Events:  []string{"on_track_change"},
		},
	}

	sb, err := NewSandbox(
		context.Background(),
		manifest,
		PluginState{Enabled: true, PermissionsApproved: true},
		minimalWasmBytes,
		storage,
		func(title, msg string) {
			notifyMu.Lock()
			notifyCalls = append(notifyCalls, title+": "+msg)
			notifyMu.Unlock()
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewSandbox failed: %v", err)
	}
	defer sb.Close()

	if err := sb.Start(map[string]string{"test_key": "test_val"}); err != nil {
		t.Fatalf("sb.Start failed: %v", err)
	}

	// Non-existent hook invocation returns cleanly without panic or hang
	if err := sb.InvokeHook("unknown_hook", []byte(`{"ping": true}`)); err != nil {
		t.Errorf("InvokeHook failed: %v", err)
	}
}

func TestSandboxSSRFAndNetworkFiltering(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("secret internal data"))
	}))
	defer ts.Close()

	manifest := Manifest{
		ID:      "ssrf-mod",
		Name:    "SSRF Test",
		Version: "1.0.0",
		Permissions: PermissionsConfig{
			Network: []string{"*"}, // Wildcard network
		},
	}

	// Attempting to access test server (which runs on 127.0.0.1 loopback)
	// Even with "*", loopback / private IP is blocked by default!
	if manifest.Permissions.CanAccessNetwork(ts.URL) {
		t.Errorf("CanAccessNetwork MUST deny 127.0.0.1 under wildcard network permission")
	}

	// Explicit loopback permission allows it
	explicitPerms := PermissionsConfig{
		Network: []string{"127.0.0.1"},
	}
	if !explicitPerms.CanAccessNetwork(ts.URL) {
		t.Errorf("CanAccessNetwork MUST allow 127.0.0.1 when explicitly granted in network permissions")
	}
}

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		input  string
		maxLen int
		want   string
	}{
		{"Normal String", 50, "Normal String"},
		{"Bad\x00Control\x1bChars", 50, "BadControlChars"},
		{"   Trimmed   ", 50, "Trimmed"},
		{"Long String Here", 4, "Long"},
	}

	for _, tt := range tests {
		got := sanitizeString(tt.input, tt.maxLen)
		if got != tt.want {
			t.Errorf("sanitizeString(%q, %d) = %q; want %q", tt.input, tt.maxLen, got, tt.want)
		}
	}
}
