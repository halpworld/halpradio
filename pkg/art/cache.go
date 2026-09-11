package art

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

// Cache tuning. Cover payloads are large, so the RAM cache stays small while
// negative lookups are remembered long enough to stop stations without a
// match from hammering the providers.
const (
	memTTL      = 6 * time.Hour
	negativeTTL = time.Hour
	memCapacity = 32
	diskTTL     = 30 * 24 * time.Hour
)

// memEntry is one in-memory cache slot. A negative entry records that no
// provider had artwork for the key.
type memEntry struct {
	cover    *Cover
	negative bool
	storedAt time.Time
}

// expired reports whether the entry has outlived its time-to-live.
func (e memEntry) expired(now time.Time) bool {
	ttl := memTTL
	if e.negative {
		ttl = negativeTTL
	}
	return now.Sub(e.storedAt) > ttl
}

// diskMeta is the sidecar JSON written next to every cached image.
type diskMeta struct {
	Source   string    `json:"source"`
	URL      string    `json:"url"`
	Artist   string    `json:"artist"`
	Title    string    `json:"title"`
	Album    string    `json:"album"`
	CachedAt time.Time `json:"cached_at"`
}

// cacheKey builds the normalised lookup key for a track. It is never used as a
// filename directly; see hashKey.
func cacheKey(artist, title, album string) string {
	return normalizeText(artist) + "|" + normalizeText(title) + "|" + normalizeText(album)
}

// hashKey returns the hex SHA-256 of a cache key, used as the on-disk basename.
func hashKey(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// noisyGroupPrefixes are parenthesised suffixes that carry no identity, such
// as "(feat. X)" or "(Official Video)".
var noisyGroupPrefixes = []string{"feat", "ft.", "ft ", "featuring", "official", "with ", "prod.", "prod "}

// normalizeText lowercases a track field, drops bracketed noise such as
// "[Remastered]", "(feat. …)" and "(Official Video)", and collapses runs of
// whitespace so equivalent titles hash to the same key.
func normalizeText(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	s = stripGroups(s, '[', ']', func(string) bool { return true })
	s = stripGroups(s, '(', ')', func(inner string) bool {
		inner = strings.TrimSpace(inner)
		for _, p := range noisyGroupPrefixes {
			if strings.HasPrefix(inner, p) {
				return true
			}
		}
		return false
	})
	return collapseSpace(s)
}

// stripGroups removes balanced open/close groups for which drop reports true.
// Unbalanced input is returned with the dangling opener kept verbatim.
func stripGroups(s string, open, closeCh byte, drop func(inner string) bool) string {
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != open {
			out.WriteByte(s[i])
			i++
			continue
		}
		depth := 0
		end := -1
		for j := i; j < len(s); j++ {
			switch s[j] {
			case open:
				depth++
			case closeCh:
				depth--
				if depth == 0 {
					end = j
				}
			}
			if end >= 0 {
				break
			}
		}
		if end < 0 {
			out.WriteByte(s[i])
			i++
			continue
		}
		inner := s[i+1 : end]
		if !drop(inner) {
			out.WriteString(s[i : end+1])
		} else {
			out.WriteByte(' ')
		}
		i = end + 1
	}
	return out.String()
}

// collapseSpace trims the string and reduces internal whitespace runs to one
// space each.
func collapseSpace(s string) string {
	var out strings.Builder
	out.Grow(len(s))
	space := true
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !space {
				out.WriteByte(' ')
				space = true
			}
			continue
		}
		out.WriteRune(r)
		space = false
	}
	return strings.TrimSpace(out.String())
}

// memGet returns a cached cover for key. The second result reports whether the
// key was cached at all; a cached negative yields (nil, true).
func (c *Client) memGet(key string) (*Cover, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.mem[key]
	if !ok {
		return nil, false
	}
	if entry.expired(time.Now()) {
		delete(c.mem, key)
		return nil, false
	}
	if entry.negative {
		return nil, true
	}
	return entry.cover.clone(), true
}

// memPut stores a cover (or a negative marker when cover is nil) and evicts
// the oldest slot once the cache is full.
func (c *Client) memPut(key string, cover *Cover) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.mem == nil {
		c.mem = make(map[string]*memEntry, memCapacity)
	}
	now := time.Now()
	for k, e := range c.mem {
		if e.expired(now) {
			delete(c.mem, k)
		}
	}
	if _, exists := c.mem[key]; !exists && len(c.mem) >= memCapacity {
		oldestKey := ""
		var oldest time.Time
		for k, e := range c.mem {
			if oldestKey == "" || e.storedAt.Before(oldest) {
				oldestKey, oldest = k, e.storedAt
			}
		}
		if oldestKey != "" {
			delete(c.mem, oldestKey)
		}
	}
	c.mem[key] = &memEntry{cover: cover.clone(), negative: cover == nil, storedAt: now}
}

// diskPaths returns the image and metadata paths for a cache key, or false
// when disk caching is disabled.
func (c *Client) diskPaths(key string) (string, string, bool) {
	if c.cacheDir == "" {
		return "", "", false
	}
	h := hashKey(key)
	return filepath.Join(c.cacheDir, h+".img"), filepath.Join(c.cacheDir, h+".json"), true
}

// diskGet loads a cached cover from disk. Every failure — missing, stale,
// corrupt or unreadable — is treated as a cache miss and never surfaced as an
// error.
func (c *Client) diskGet(key string) (*Cover, bool) {
	imgPath, metaPath, ok := c.diskPaths(key)
	if !ok {
		return nil, false
	}

	metaRaw, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, false
	}
	var meta diskMeta
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return nil, false
	}
	if meta.CachedAt.IsZero() || time.Since(meta.CachedAt) > diskTTL {
		return nil, false
	}

	data, err := os.ReadFile(imgPath)
	if err != nil || len(data) == 0 {
		return nil, false
	}
	if _, _, err := image.DecodeConfig(bytes.NewReader(data)); err != nil {
		return nil, false
	}

	return &Cover{
		Data:   data,
		Source: meta.Source,
		Artist: meta.Artist,
		Title:  meta.Title,
		Album:  meta.Album,
		URL:    meta.URL,
	}, true
}

// diskPut writes a cover and its metadata sidecar. Disk errors are returned so
// callers can log them, but they are never fatal to a fetch.
func (c *Client) diskPut(key string, cover *Cover) error {
	imgPath, metaPath, ok := c.diskPaths(key)
	if !ok || cover == nil || len(cover.Data) == 0 {
		return nil
	}
	if err := os.MkdirAll(c.cacheDir, 0o700); err != nil {
		return fmt.Errorf("art: create cache dir: %w", err)
	}
	if err := os.WriteFile(imgPath, cover.Data, 0o600); err != nil {
		return fmt.Errorf("art: write cache image: %w", err)
	}
	meta, err := json.Marshal(diskMeta{
		Source:   cover.Source,
		URL:      cover.URL,
		Artist:   cover.Artist,
		Title:    cover.Title,
		Album:    cover.Album,
		CachedAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("art: encode cache metadata: %w", err)
	}
	if err := os.WriteFile(metaPath, meta, 0o600); err != nil {
		return fmt.Errorf("art: write cache metadata: %w", err)
	}
	return nil
}
