package app

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/util"
)

const testEmbedded = `stations:
  - id: emb-1
    name: Embedded One
    url: https://example.com/one
  - id: emb-2
    name: Embedded Two
    url: https://example.com/two
`

const testCache = `stations:
  - id: cache-1
    name: Cache One
    url: https://example.com/c1
  - id: cache-2
    name: Cache Two
    url: https://example.com/c2
`

func isolateHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "LocalAppData"))
}

func TestNewCatalogUpdaterUsesEmbeddedCount(t *testing.T) {
	if n := embeddedStationCount([]byte(testEmbedded)); n != 2 {
		t.Fatalf("embeddedStationCount = %d, want 2", n)
	}
	u := newCatalogUpdater(util.DefaultConfig(), []byte(testEmbedded))
	if u.MinStations != 2 {
		t.Fatalf("MinStations = %d, want 2", u.MinStations)
	}
}

// catalog_auto_update: false keeps the embedded catalog even when a cached
// download exists.
func TestLoadStationStoreHonoursAutoUpdate(t *testing.T) {
	isolateHome(t)
	cache := util.GetCatalogCacheFile()
	if err := os.MkdirAll(filepath.Dir(cache), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cache, []byte(testCache), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := util.DefaultConfig()
	cfg.CatalogAutoUpdate = true
	on := loadStationStore([]byte(testEmbedded), cfg)
	usesCache := on.FindStationByID("cache-1") != nil

	cfg.CatalogAutoUpdate = false
	off := loadStationStore([]byte(testEmbedded), cfg)
	if off.FindStationByID("cache-1") != nil {
		t.Fatal("cached catalog used although catalog_auto_update is false")
	}
	if off.FindStationByID("emb-1") == nil {
		t.Fatal("embedded catalog missing")
	}
	if !usesCache {
		t.Log("cache was not accepted with auto-update on (signature policy); the off case is still checked")
	}
}

func TestStationsAddPrintsStoredID(t *testing.T) {
	isolateHome(t)
	var out bytes.Buffer
	ok, err := RunStations([]string{"add", "--name", "My\x1b[2J Station", "--url", "https://stream.example.com/live"}, []byte(testEmbedded), &out)
	if !ok || err != nil {
		t.Fatalf("add failed: %v\n%s", err, out.String())
	}
	s := out.String()
	if !strings.Contains(s, "with ID custom-1") || !strings.Contains(s, "halpradio play custom-1") {
		t.Errorf("stored ID not printed:\n%s", s)
	}
	if strings.Contains(s, "\x1b") {
		t.Errorf("escape sequence printed: %q", s)
	}
}

func TestStationsAddInvalidURLFails(t *testing.T) {
	isolateHome(t)
	var out bytes.Buffer
	ok, err := RunStations([]string{"add", "--name", "Bad", "--url", "file:///etc/passwd"}, []byte(testEmbedded), &out)
	if ok || err == nil {
		t.Fatalf("invalid URL accepted:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "Failed to save station") {
		t.Errorf("no error shown:\n%s", out.String())
	}
}
