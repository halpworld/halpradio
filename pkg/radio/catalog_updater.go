package radio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/halpworld/halpradio/pkg/util"
	"gopkg.in/yaml.v3"
)

const (
	DefaultCatalogUpdateURL     = "https://raw.githubusercontent.com/halpworld/halpradio/main/stations.yaml"
	DefaultCatalogCacheTTLHours = 24
	MaxCatalogDownloadBytes     = 5 * 1024 * 1024 // 5 MB limit
)

// CatalogMetadata tracks caching headers and timestamps to eliminate unnecessary server load.
type CatalogMetadata struct {
	LastCheckedAt time.Time `json:"last_checked_at"`
	ETag          string    `json:"etag,omitempty"`
	LastModified  string    `json:"last_modified,omitempty"`
	SHA256        string    `json:"sha256,omitempty"`
	StationCount  int       `json:"station_count,omitempty"`
}

// CatalogUpdater handles lightweight, aggressively cached background updates for stations.yaml.
type CatalogUpdater struct {
	UpdateURL    string
	CacheTTL     time.Duration
	HTTPClient   *http.Client
	MetadataFile string
	CacheFile    string
}

// NewCatalogUpdater creates a new CatalogUpdater with the specified URL and TTL.
func NewCatalogUpdater(url string, ttlHours int) *CatalogUpdater {
	if url == "" {
		url = DefaultCatalogUpdateURL
	}
	if ttlHours <= 0 {
		ttlHours = DefaultCatalogCacheTTLHours
	}
	return &CatalogUpdater{
		UpdateURL:    url,
		CacheTTL:     time.Duration(ttlHours) * time.Hour,
		MetadataFile: util.GetCatalogMetadataFile(),
		CacheFile:    util.GetCatalogCacheFile(),
		HTTPClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// LoadMetadata reads the cached metadata from disk.
func (u *CatalogUpdater) LoadMetadata() CatalogMetadata {
	var meta CatalogMetadata
	data, err := os.ReadFile(u.MetadataFile)
	if err == nil {
		_ = json.Unmarshal(data, &meta)
	}
	return meta
}

// SaveMetadata writes the metadata to disk.
func (u *CatalogUpdater) SaveMetadata(meta CatalogMetadata) error {
	_ = util.EnsureConfigDir()
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(u.MetadataFile, data, 0600)
}

// ShouldCheck returns true only if the cache TTL has expired or force is true.
func (u *CatalogUpdater) ShouldCheck(force bool) bool {
	if force {
		return true
	}
	meta := u.LoadMetadata()
	if meta.LastCheckedAt.IsZero() {
		return true
	}
	return time.Since(meta.LastCheckedAt) >= u.CacheTTL
}

// CheckAndUpdate performs an ultra-lightweight conditional HTTP check and updates cache if new stations exist.
// Returns (updated bool, stationCount int, err error).
func (u *CatalogUpdater) CheckAndUpdate(ctx context.Context, force bool) (bool, int, error) {
	if !u.ShouldCheck(force) {
		meta := u.LoadMetadata()
		return false, meta.StationCount, nil
	}

	meta := u.LoadMetadata()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.UpdateURL, nil)
	if err != nil {
		return false, meta.StationCount, err
	}
	req.Header.Set("User-Agent", "halpradio/1.0 (lightweight-catalog-sync)")

	// Conditional HTTP headers: server returns 304 if unchanged, saving bandwidth & load
	if meta.ETag != "" {
		req.Header.Set("If-None-Match", meta.ETag)
	}
	if meta.LastModified != "" {
		req.Header.Set("If-Modified-Since", meta.LastModified)
	}

	resp, err := u.HTTPClient.Do(req)
	if err != nil {
		return false, meta.StationCount, err
	}
	defer resp.Body.Close()

	// 304 Not Modified: 0 payload transfer, server unchanged!
	if resp.StatusCode == http.StatusNotModified {
		meta.LastCheckedAt = time.Now()
		_ = u.SaveMetadata(meta)
		return false, meta.StationCount, nil
	}

	if resp.StatusCode != http.StatusOK {
		return false, meta.StationCount, fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}

	// Read body with limit
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxCatalogDownloadBytes))
	if err != nil {
		return false, meta.StationCount, err
	}

	// Check if checksum matches existing cache (in case server doesn't support ETag)
	hash := sha256.Sum256(body)
	hashStr := hex.EncodeToString(hash[:])
	if hashStr == meta.SHA256 && meta.StationCount > 0 {
		meta.LastCheckedAt = time.Now()
		if etag := resp.Header.Get("ETag"); etag != "" {
			meta.ETag = etag
		}
		if lm := resp.Header.Get("Last-Modified"); lm != "" {
			meta.LastModified = lm
		}
		_ = u.SaveMetadata(meta)
		return false, meta.StationCount, nil
	}

	// Strictly validate YAML schema before accepting
	var catalog StationCatalog
	if err := yaml.Unmarshal(body, &catalog); err != nil {
		return false, meta.StationCount, fmt.Errorf("invalid catalog YAML: %w", err)
	}
	if len(catalog.Stations) == 0 {
		return false, meta.StationCount, fmt.Errorf("downloaded catalog contains no stations")
	}

	// Safely save cache file
	_ = util.EnsureConfigDir()
	if err := os.WriteFile(u.CacheFile, body, 0600); err != nil {
		return false, meta.StationCount, err
	}

	// Update and save metadata
	meta.LastCheckedAt = time.Now()
	meta.ETag = resp.Header.Get("ETag")
	meta.LastModified = resp.Header.Get("Last-Modified")
	meta.SHA256 = hashStr
	meta.StationCount = len(catalog.Stations)
	_ = u.SaveMetadata(meta)

	return true, meta.StationCount, nil
}
