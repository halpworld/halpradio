package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestSetupAppVersion(t *testing.T) {
	var buf bytes.Buffer
	appInst, isVersion, err := SetupApp([]string{"-version"}, nil, &buf)
	if err != nil {
		t.Fatalf("Unexpected error for -version: %v", err)
	}
	if !isVersion {
		t.Errorf("Expected isVersion to be true")
	}
	if appInst != nil {
		t.Errorf("Expected appInst to be nil when version flag is passed")
	}
	if !strings.Contains(buf.String(), Version) {
		t.Errorf("Expected version output to contain %s, got: %s", Version, buf.String())
	}
}

func TestSetupAppFlagsAndDefaults(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	var buf bytes.Buffer
	appInst, isVersion, err := SetupApp([]string{"-backend", "native", "-theme", "dracula"}, []byte{}, &buf)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if isVersion {
		t.Errorf("Expected isVersion to be false")
	}
	if appInst == nil {
		t.Fatalf("Expected non-nil AppInstance")
	}
	if appInst.Config.PlayerBackend != "native" {
		t.Errorf("Expected backend 'native', got %s", appInst.Config.PlayerBackend)
	}
	if appInst.Config.Theme != "dracula" {
		t.Errorf("Expected theme 'dracula', got %s", appInst.Config.Theme)
	}
	if appInst.Player == nil || appInst.Program == nil || appInst.Store == nil {
		t.Errorf("AppInstance has uninitialized components: %+v", appInst)
	}
}

func TestSetupAppInvalidFlag(t *testing.T) {
	var buf bytes.Buffer
	_, _, err := SetupApp([]string{"-invalid-flag"}, nil, &buf)
	if err == nil {
		t.Errorf("Expected error for invalid CLI flag, got nil")
	}
}
