package radio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/halpworld/halpradio/pkg/util"
)

func TestCatalogUpdater_CacheHit_NoNetwork(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	requestsReceived := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsReceived++
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("stations: []\n"))
	}))
	defer server.Close()

	updater := NewCatalogUpdater(server.URL, 24)
	updater.MetadataFile = filepath.Join(tempDir, "catalog_metadata.json")
	updater.CacheFile = filepath.Join(tempDir, "catalog_cache.yaml")

	// Save recent metadata
	meta := CatalogMetadata{
		LastCheckedAt: time.Now().Add(-1 * time.Hour), // checked 1 hour ago (TTL is 24h)
		ETag:          `"abc123"`,
		StationCount:  50,
	}
	if err := updater.SaveMetadata(meta); err != nil {
		t.Fatalf("SaveMetadata failed: %v", err)
	}

	updated, count, err := updater.CheckAndUpdate(context.Background(), false)
	if err != nil {
		t.Fatalf("CheckAndUpdate unexpected error: %v", err)
	}
	if updated {
		t.Errorf("Expected updated to be false on cache hit")
	}
	if count != 50 {
		t.Errorf("Expected count 50, got %d", count)
	}
	if requestsReceived != 0 {
		t.Errorf("Expected 0 HTTP requests within TTL, got %d", requestsReceived)
	}
}

func TestCatalogUpdater_Conditional304NotModified(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("If-None-Match") == `"etag-test"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`
stations:
  - id: s1
    name: "Station 1"
    url: "http://stream1"
`))
	}))
	defer server.Close()

	updater := NewCatalogUpdater(server.URL, 24)
	updater.MetadataFile = filepath.Join(tempDir, "catalog_metadata.json")
	updater.CacheFile = filepath.Join(tempDir, "catalog_cache.yaml")

	// Expired metadata with ETag
	meta := CatalogMetadata{
		LastCheckedAt: time.Now().Add(-48 * time.Hour),
		ETag:          `"etag-test"`,
		StationCount:  1,
	}
	_ = updater.SaveMetadata(meta)

	updated, count, err := updater.CheckAndUpdate(context.Background(), false)
	if err != nil {
		t.Fatalf("CheckAndUpdate failed: %v", err)
	}
	if updated {
		t.Errorf("Expected updated to be false on 304 Not Modified")
	}
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	// Verify LastCheckedAt was updated
	newMeta := updater.LoadMetadata()
	if time.Since(newMeta.LastCheckedAt) > 5*time.Second {
		t.Errorf("Expected LastCheckedAt to be refreshed to now")
	}
}

func TestCatalogUpdater_Successful200Update(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	validCatalogYAML := `
stations:
  - id: cyber-1
    name: "Cyber Station"
    url: "https://stream.cyber.fm"
    genre: "Cyberpunk / Industrial"
    country: "US"
    bitrate: 320
    codec: "MP3"
`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"new-etag-999"`)
		w.Header().Set("Last-Modified", "Tue, 18 Aug 2026 08:00:00 GMT")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(validCatalogYAML))
	}))
	defer server.Close()

	updater := NewCatalogUpdater(server.URL, 24)
	updater.MetadataFile = filepath.Join(tempDir, "catalog_metadata.json")
	updater.CacheFile = filepath.Join(tempDir, "catalog_cache.yaml")

	updated, count, err := updater.CheckAndUpdate(context.Background(), true)
	if err != nil {
		t.Fatalf("CheckAndUpdate failed: %v", err)
	}
	if !updated {
		t.Errorf("Expected updated to be true")
	}
	if count != 1 {
		t.Errorf("Expected station count 1, got %d", count)
	}

	// Verify cache file exists and is readable
	cacheData, err := os.ReadFile(updater.CacheFile)
	if err != nil {
		t.Fatalf("Failed to read cache file: %v", err)
	}
	if len(cacheData) == 0 {
		t.Errorf("Cache file is empty")
	}

	meta := updater.LoadMetadata()
	if meta.ETag != `"new-etag-999"` {
		t.Errorf("Expected ETag new-etag-999, got %s", meta.ETag)
	}
	if meta.StationCount != 1 {
		t.Errorf("Expected StationCount 1, got %d", meta.StationCount)
	}
}

func TestCatalogUpdater_CorruptedYAML_Rejected(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("invalid: yaml: [corrupted"))
	}))
	defer server.Close()

	updater := NewCatalogUpdater(server.URL, 24)
	updater.MetadataFile = filepath.Join(tempDir, "catalog_metadata.json")
	updater.CacheFile = filepath.Join(tempDir, "catalog_cache.yaml")

	// Pre-create valid existing cache
	existingData := []byte("stations:\n  - id: valid-1\n    name: Valid\n")
	_ = os.WriteFile(updater.CacheFile, existingData, 0600)

	updated, _, err := updater.CheckAndUpdate(context.Background(), true)
	if err == nil {
		t.Errorf("Expected error on invalid YAML, got nil")
	}
	if updated {
		t.Errorf("Expected updated to be false on invalid YAML")
	}

	// Verify existing cache file was NOT overwritten with corruption
	data, _ := os.ReadFile(updater.CacheFile)
	if string(data) != string(existingData) {
		t.Errorf("Cache was overwritten with corrupted data")
	}
}

func TestStore_LoadsCachedCatalogPrioritized(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	_ = util.EnsureConfigDir()

	cachedYAML := `
stations:
  - id: cached-station
    name: "Cached Dark Station"
    url: "https://stream.example.com"
    genre: "Darksynth"
`
	_ = os.WriteFile(util.GetCatalogCacheFile(), []byte(cachedYAML), 0600)

	embeddedYAML := []byte(`
stations:
  - id: embedded-station
    name: "Old Embedded Station"
    url: "https://old.example.com"
`)

	store := NewStore()
	if err := store.Load(embeddedYAML); err != nil {
		t.Fatalf("Store.Load failed: %v", err)
	}
	reloaded := store.ReloadBundledFromCache()
	if !reloaded {
		t.Fatalf("Expected ReloadBundledFromCache to return true")
	}

	stations := store.GetAllStations()
	if len(stations) != 1 {
		t.Fatalf("Expected 1 station from cache, got %d", len(stations))
	}
	if stations[0].ID != "cached-station" {
		t.Errorf("Expected station 'cached-station' from cache, got %s", stations[0].ID)
	}
}
