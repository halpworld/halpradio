package radio

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/halpworld/halpradio/pkg/radio/netguard"
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
	// CatalogVersion is the catalog_version of the cached catalog; a
	// download with a lower version is refused as a rollback.
	CatalogVersion int `json:"catalog_version,omitempty"`
}

// CatalogUpdater handles lightweight, aggressively cached background updates for stations.yaml.
type CatalogUpdater struct {
	UpdateURL    string
	CacheTTL     time.Duration
	HTTPClient   *http.Client
	MetadataFile string
	CacheFile    string
	// MinStations, when set (for example to the size of the embedded
	// catalog), is a floor for the plausibility check: a download with
	// fewer than half this many valid stations is refused.
	MinStations int
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
		HTTPClient:   netguard.NewClient(5 * time.Second),
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
	return netguard.WriteFileAtomic(u.MetadataFile, data, 0600)
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
//
// The download is treated as untrusted until it passes every check:
//   - the update URL must be https;
//   - the body must fit in MaxCatalogDownloadBytes (an oversized body is an
//     error, never silently truncated);
//   - when CatalogSigningPublicKey is set, "<url>.sig" must carry a valid
//     Ed25519 signature of the exact body;
//   - every entry passes SanitizeStation, and at least one must survive;
//   - a catalog with a lower catalog_version than the cached one, or an older
//     Last-Modified date, is refused as a rollback;
//   - a catalog with fewer than half the stations of the cached one is
//     refused as implausible.
//
// Only then is it written to the cache, atomically and with mode 0600.
func (u *CatalogUpdater) CheckAndUpdate(ctx context.Context, force bool) (bool, int, error) {
	if !u.ShouldCheck(force) {
		meta := u.LoadMetadata()
		return false, meta.StationCount, nil
	}

	meta := u.LoadMetadata()

	if err := checkCatalogURL(u.UpdateURL); err != nil {
		return false, meta.StationCount, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = netguard.WithOrigin(ctx, u.UpdateURL)

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

	resp, err := u.httpClient().Do(req)
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

	body, err := netguard.ReadLimited(resp.Body, MaxCatalogDownloadBytes)
	if err != nil {
		return false, meta.StationCount, fmt.Errorf("catalog download: %w", err)
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

	var sig []byte
	if catalogSigningKey() != nil {
		sig, err = u.fetchSignature(ctx)
		if err != nil {
			return false, meta.StationCount, err
		}
		if err := verifyCatalogSignature(body, sig); err != nil {
			return false, meta.StationCount, err
		}
	}

	// Strictly validate YAML schema before accepting
	var catalog StationCatalog
	if err := yaml.Unmarshal(body, &catalog); err != nil {
		return false, meta.StationCount, fmt.Errorf("invalid catalog YAML: %w", err)
	}
	if len(catalog.Stations) == 0 {
		return false, meta.StationCount, fmt.Errorf("downloaded catalog contains no stations")
	}
	valid, _ := sanitizeStations(catalog.Stations, "bundled")
	if len(valid) == 0 {
		return false, meta.StationCount, fmt.Errorf("downloaded catalog contains no valid stations")
	}

	// Rollback and plausibility checks against what is already cached.
	if catalog.Version < meta.CatalogVersion {
		return false, meta.StationCount, fmt.Errorf("downloaded catalog version %d is older than cached version %d", catalog.Version, meta.CatalogVersion)
	}
	newLM := resp.Header.Get("Last-Modified")
	if older, err := isOlderHTTPDate(newLM, meta.LastModified); err == nil && older {
		return false, meta.StationCount, fmt.Errorf("downloaded catalog is older than the cached one")
	}
	if baseline := max(meta.StationCount, u.MinStations); baseline > 0 && len(valid)*2 < baseline {
		return false, meta.StationCount, fmt.Errorf("downloaded catalog has %d valid stations, implausibly fewer than %d", len(valid), baseline)
	}

	// Safely save cache file
	_ = util.EnsureConfigDir()
	if sig != nil {
		if err := netguard.WriteFileAtomic(u.CacheFile+".sig", sig, 0600); err != nil {
			return false, meta.StationCount, err
		}
	}
	if err := netguard.WriteFileAtomic(u.CacheFile, body, 0600); err != nil {
		return false, meta.StationCount, err
	}

	// Update and save metadata
	meta.LastCheckedAt = time.Now()
	meta.ETag = resp.Header.Get("ETag")
	meta.LastModified = newLM
	meta.SHA256 = hashStr
	meta.StationCount = len(valid)
	meta.CatalogVersion = catalog.Version
	_ = u.SaveMetadata(meta)

	return true, meta.StationCount, nil
}

func (u *CatalogUpdater) httpClient() *http.Client {
	if u.HTTPClient != nil {
		return u.HTTPClient
	}
	return netguard.NewClient(5 * time.Second)
}

// checkCatalogURL accepts only https catalog URLs.
func checkCatalogURL(raw string) error {
	parsed, err := netguard.CheckURL(raw)
	if err != nil {
		return fmt.Errorf("catalog update URL rejected: %w", err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("catalog update URL must use https")
	}
	return nil
}

// isOlderHTTPDate reports whether HTTP date a is strictly before b. It
// returns an error when either is missing or unparsable.
func isOlderHTTPDate(a, b string) (bool, error) {
	if a == "" || b == "" {
		return false, fmt.Errorf("missing date")
	}
	ta, err := http.ParseTime(a)
	if err != nil {
		return false, err
	}
	tb, err := http.ParseTime(b)
	if err != nil {
		return false, err
	}
	return ta.Before(tb), nil
}

// fetchSignature downloads "<UpdateURL>.sig": the base64 (standard
// encoding) Ed25519 signature of the catalog body.
func (u *CatalogUpdater) fetchSignature(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.UpdateURL+".sig", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "halpradio/1.0 (lightweight-catalog-sync)")
	resp, err := u.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("catalog signature: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("catalog signature: server returned HTTP %d", resp.StatusCode)
	}
	sig, err := netguard.ReadLimited(resp.Body, 1024)
	if err != nil {
		return nil, fmt.Errorf("catalog signature: %w", err)
	}
	return sig, nil
}
