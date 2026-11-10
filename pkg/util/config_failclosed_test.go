package util

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const brokenConfigYAML = "plugins_enabled: false\nlyrics_netease_enabled: false\ncatalog_auto_update: false\nlastfm_api_key: \"my-key\"\nparty_port: auto\n"

func writeBrokenConfig(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	if err := EnsureConfigDir(); err != nil {
		t.Fatal(err)
	}
	p := GetConfigFile()
	if err := os.WriteFile(p, []byte(brokenConfigYAML), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// FR-A-3: a config.yaml that fails to parse must not turn the safety
// switches back on, and must never be overwritten by a later save.
func TestBrokenConfigFailsClosedAndIsNotOverwritten(t *testing.T) {
	p := writeBrokenConfig(t)
	cfg, err := LoadConfig()
	if err == nil {
		t.Fatal("expected a decode error")
	}
	if cfg.LoadError() == nil || cfg.LoadWarning() == "" {
		t.Fatal("load error not recorded")
	}
	if cfg.PluginsEnabled || cfg.CatalogAutoUpdate || cfg.LyricsNetEaseEnabled ||
		cfg.ArtITunesEnabled || cfg.ArtDeezerEnabled || cfg.ArtMusicBrainzEnabled || cfg.EventCommandHook != "" {
		t.Fatalf("safety switches not failed closed: %+v", cfg)
	}
	if !strings.Contains(cfg.LoadWarning(), "Plugins and network extras are off") {
		t.Fatalf("warning %q", cfg.LoadWarning())
	}

	cfg.Volume = 50
	if err := SaveConfig(cfg); !errors.Is(err, ErrConfigUnreadable) {
		t.Fatalf("SaveConfig err = %v, want ErrConfigUnreadable", err)
	}
	// A fresh default config must not overwrite the broken file either.
	if err := SaveConfig(DefaultConfig()); !errors.Is(err, ErrConfigUnreadable) {
		t.Fatalf("SaveConfig(defaults) err = %v", err)
	}
	got, _ := os.ReadFile(p)
	if !bytes.Equal(got, []byte(brokenConfigYAML)) {
		t.Fatalf("config.yaml was rewritten:\n%s", got)
	}
}

func TestMissingConfigIsNormalDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	cfg, _ := LoadConfig()
	if cfg.LoadError() != nil || !cfg.PluginsEnabled {
		t.Fatalf("missing file should give normal defaults: %v", cfg.LoadError())
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("first save: %v", err)
	}
}
