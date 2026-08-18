package plugin

import (
	"net"
	"testing"
)

func TestManifestValidation(t *testing.T) {
	valid := Manifest{
		ID:          "discord-rpc",
		Name:        "Discord Rich Presence",
		Version:     "1.0.0",
		Author:      "test",
		Description: "A test plugin",
		Permissions: PermissionsConfig{
			Network: []string{"discord.com"},
			Storage: []string{"local"},
		},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("expected valid manifest, got error: %v", err)
	}

	invalidIDs := []string{"", "Invalid_Caps", "-starts-with-dash", "has spaces", "special@#$"}
	for _, id := range invalidIDs {
		inv := valid
		inv.ID = id
		if err := inv.Validate(); err == nil {
			t.Errorf("expected validation error for invalid ID %q", id)
		}
	}

	invName := valid
	invName.Name = ""
	if err := invName.Validate(); err == nil {
		t.Errorf("expected validation error for empty Name")
	}

	invVer := valid
	invVer.Version = ""
	if err := invVer.Validate(); err == nil {
		t.Errorf("expected validation error for empty Version")
	}

	// Path traversal in WasmFile
	invalidWasmFiles := []string{
		"../plugin.wasm",
		"/etc/passwd",
		"../../../../secret.wasm",
		"subdir/plugin.wasm",
		"plugin.wasm.exe",
		"plugin.exe",
		"plugin.wasm/../test.wasm",
	}
	for _, wf := range invalidWasmFiles {
		invWasm := valid
		invWasm.WasmFile = wf
		if err := invWasm.Validate(); err == nil {
			t.Errorf("expected validation error for path traversal wasm_file %q", wf)
		}
	}
}

func TestNetworkPermissions(t *testing.T) {
	p := PermissionsConfig{
		Network: []string{
			"*.discord.com",
			"api.last.fm",
			"192.168.1.0/24",
		},
	}

	tests := []struct {
		url     string
		allowed bool
	}{
		{"https://discord.com/api/webhooks", true},
		{"https://api.discord.com/v10/users", true},
		{"https://sub.api.discord.com/ping", true},
		{"https://api.last.fm/2.0/", true},
		{"https://other.last.fm/2.0/", false},
		{"http://192.168.1.50:8123/api/states", true},
		{"http://192.168.2.50:8123/api/states", false},
		{"https://evil.com/leak", false},
		{"file:///etc/passwd", false},
		{"gopher://127.0.0.1:70", false},
		{"http://user:pass@discord.com", false},
	}

	for _, tt := range tests {
		got := p.CanAccessNetwork(tt.url)
		if got != tt.allowed {
			t.Errorf("CanAccessNetwork(%q) = %v; want %v", tt.url, got, tt.allowed)
		}
	}

	// Wildcard allows public internet but blocks private/loopback/cloud metadata SSRF
	wildcard := PermissionsConfig{Network: []string{"*"}}
	if !wildcard.CanAccessNetwork("https://api.github.com/repos") {
		t.Errorf("expected wildcard network permission to allow public URL")
	}
	if wildcard.CanAccessNetwork("http://127.0.0.1:8080/admin") {
		t.Errorf("wildcard network MUST NOT allow loopback 127.0.0.1 SSRF")
	}
	if wildcard.CanAccessNetwork("http://localhost:8080") {
		t.Errorf("wildcard network MUST NOT allow localhost SSRF")
	}
	if wildcard.CanAccessNetwork("http://169.254.169.254/latest/meta-data") {
		t.Errorf("wildcard network MUST NOT allow cloud metadata SSRF")
	}
	if wildcard.CanAccessNetwork("http://10.0.0.1/router") {
		t.Errorf("wildcard network MUST NOT allow private 10.0.0.0/8 SSRF")
	}
	if wildcard.CanAccessNetwork("http://192.168.1.1/admin") {
		t.Errorf("wildcard network MUST NOT allow private 192.168.0.0/16 SSRF")
	}

	// Empty default deny
	empty := PermissionsConfig{}
	if empty.CanAccessNetwork("https://google.com") {
		t.Errorf("expected empty network permissions to deny by default")
	}
}

func TestStorageAndEventPermissions(t *testing.T) {
	p := PermissionsConfig{
		Storage: []string{"local"},
		Events:  []string{"on_track_change", "on_playback_change"},
	}

	if !p.HasStorage() {
		t.Errorf("expected HasStorage() to be true")
	}

	if !p.HasEvent("on_track_change") {
		t.Errorf("expected HasEvent(on_track_change) to be true")
	}
	if !p.HasEvent("ON_TRACK_CHANGE") {
		t.Errorf("expected HasEvent case insensitivity")
	}
	if p.HasEvent("on_custom_other") {
		t.Errorf("expected HasEvent(on_custom_other) to be false")
	}
}

func TestIsPrivateOrLoopbackIP(t *testing.T) {
	privateIPs := []string{
		"127.0.0.1",
		"10.0.0.1",
		"172.16.0.1",
		"192.168.1.1",
		"169.254.1.1",
		"::1",
		"fc00::1",
		"fe80::1",
	}

	for _, ipStr := range privateIPs {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			t.Fatalf("failed to parse IP: %s", ipStr)
		}
		if !IsPrivateOrLoopbackIP(ip) {
			t.Errorf("expected IsPrivateOrLoopbackIP(%s) to be true", ipStr)
		}
	}

	publicIP := net.ParseIP("8.8.8.8")
	if IsPrivateOrLoopbackIP(publicIP) {
		t.Errorf("expected IsPrivateOrLoopbackIP(8.8.8.8) to be false")
	}
}
