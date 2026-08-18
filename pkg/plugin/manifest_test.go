package plugin

import (
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
	}

	for _, tt := range tests {
		got := p.CanAccessNetwork(tt.url)
		if got != tt.allowed {
			t.Errorf("CanAccessNetwork(%q) = %v; want %v", tt.url, got, tt.allowed)
		}
	}

	// Wildcard allow-all
	wildcard := PermissionsConfig{Network: []string{"*"}}
	if !wildcard.CanAccessNetwork("https://anything.com") {
		t.Errorf("expected wildcard network permission to allow any URL")
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
