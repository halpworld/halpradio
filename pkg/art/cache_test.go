package art

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeText(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"  Spaced   Out  ", "spaced out"},
		{"MiXeD CaSe", "mixed case"},
		{"Song (feat. Someone)", "song"},
		{"Song (Feat. Someone Else)", "song"},
		{"Song (ft. Guest)", "song"},
		{"Song (featuring Guest)", "song"},
		{"Song (Official Video)", "song"},
		{"Song (Official Music Video)", "song"},
		{"Song [Remastered 2011]", "song"},
		{"Song [Explicit] (feat. X)", "song"},
		{"Song (Live at Wembley)", "song (live at wembley)"}, // meaningful, kept
		{"Song (Remix)", "song (remix)"},                     // meaningful, kept
		{"Song (prod. Someone)", "song"},
		{"Unbalanced (open", "unbalanced (open"},
		{"Nested (feat. A (and B)) tail", "nested tail"},
		{"Tabs\tand\nnewlines", "tabs and newlines"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := normalizeText(tc.in); got != tc.want {
				t.Fatalf("normalizeText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCacheKeyNormalises(t *testing.T) {
	tests := []struct {
		name        string
		a1, t1, al1 string
		a2, t2, al2 string
		wantSame    bool
	}{
		{"identical", "A", "B", "C", "A", "B", "C", true},
		{"case and space", "  Daft Punk ", "One More Time", "Discovery",
			"daft punk", "one more time", "discovery", true},
		{"feat noise", "Artist", "Track (feat. Guest)", "", "Artist", "Track", "", true},
		{"official video noise", "Artist", "Track [Official Video]", "", "Artist", "Track", "", true},
		{"different track", "Artist", "Track A", "", "Artist", "Track B", "", false},
		{"different album", "Artist", "Track", "Album A", "Artist", "Track", "Album B", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k1 := cacheKey(tc.a1, tc.t1, tc.al1)
			k2 := cacheKey(tc.a2, tc.t2, tc.al2)
			if (k1 == k2) != tc.wantSame {
				t.Fatalf("cacheKey equality was %v (%q vs %q), want %v", k1 == k2, k1, k2, tc.wantSame)
			}
		})
	}
}

func TestHashKeyIsFilenameSafe(t *testing.T) {
	tests := []string{
		"",
		"artist|title|album",
		"../../etc/passwd",
		"weird/\\:*?\"<>| name",
		strings.Repeat("x", 4096),
	}
	for _, in := range tests {
		got := hashKey(in)
		if len(got) != 64 {
			t.Fatalf("hashKey(%q) length = %d, want 64", in, len(got))
		}
		if strings.ContainsAny(got, "/\\.:") {
			t.Fatalf("hashKey(%q) = %q contains path characters", in, got)
		}
		if got != hashKey(in) {
			t.Fatal("hashKey is not stable")
		}
	}
}

func TestMemCache(t *testing.T) {
	c := NewClient("", "")
	cover := &Cover{Data: []byte("bytes"), Source: "iTunes", Title: "T"}

	if _, ok := c.memGet("missing"); ok {
		t.Fatal("unexpected hit on an empty cache")
	}

	c.memPut("k", cover)
	got, ok := c.memGet("k")
	if !ok || got == nil {
		t.Fatal("expected a positive cache hit")
	}
	if got == cover {
		t.Fatal("memGet returned the caller's own pointer")
	}
	if got.Source != "iTunes" {
		t.Fatalf("Source = %q", got.Source)
	}

	c.memPut("neg", nil)
	got, ok = c.memGet("neg")
	if !ok {
		t.Fatal("expected the negative entry to be cached")
	}
	if got != nil {
		t.Fatal("negative entry should yield a nil cover")
	}
}

func TestMemCacheExpiry(t *testing.T) {
	tests := []struct {
		name     string
		negative bool
		age      time.Duration
		wantHit  bool
	}{
		{"fresh positive", false, time.Minute, true},
		{"stale positive", false, memTTL + time.Minute, false},
		{"fresh negative", true, time.Minute, true},
		{"stale negative", true, negativeTTL + time.Minute, false},
		{"positive TTL outlives negative TTL", true, memTTL - time.Minute, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient("", "")
			entry := &memEntry{
				negative: tc.negative,
				storedAt: time.Now().Add(-tc.age),
			}
			if !tc.negative {
				entry.cover = &Cover{Data: []byte("x")}
			}
			c.mem["k"] = entry
			if _, ok := c.memGet("k"); ok != tc.wantHit {
				t.Fatalf("memGet hit = %v, want %v", ok, tc.wantHit)
			}
		})
	}
}

func TestMemCacheEviction(t *testing.T) {
	c := NewClient("", "")
	for i := 0; i < memCapacity*2; i++ {
		c.memPut(string(rune('a'+i%26))+hashKey(string(rune(i))), &Cover{Data: []byte("x")})
	}
	c.mu.Lock()
	size := len(c.mem)
	c.mu.Unlock()
	if size > memCapacity {
		t.Fatalf("cache holds %d entries, want at most %d", size, memCapacity)
	}
}

func TestDiskCacheRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "covers")
	c := NewClient(dir, "")
	if c.CacheDir() != dir {
		t.Fatalf("CacheDir() = %q, want %q", c.CacheDir(), dir)
	}

	key := cacheKey("Artist", "Title", "Album")
	if _, ok := c.diskGet(key); ok {
		t.Fatal("unexpected hit on an empty disk cache")
	}

	cover := &Cover{
		Data:   testPNG(t, 8, 8),
		Source: "Deezer",
		Artist: "Artist",
		Title:  "Title",
		Album:  "Album",
		URL:    "https://example.test/cover.png",
	}
	if err := c.diskPut(key, cover); err != nil {
		t.Fatalf("diskPut: %v", err)
	}

	got, ok := c.diskGet(key)
	if !ok {
		t.Fatal("expected a disk cache hit")
	}
	if got.Source != "Deezer" || got.URL != cover.URL || got.Album != "Album" {
		t.Fatalf("metadata not round-tripped: %+v", got)
	}
	if len(got.Data) != len(cover.Data) {
		t.Fatalf("payload length = %d, want %d", len(got.Data), len(cover.Data))
	}

	// Permissions: 0700 directory, 0600 files.
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("dir mode = %o, want 700", perm)
	}
	imgPath, metaPath, _ := c.diskPaths(key)
	for _, p := range []string{imgPath, metaPath} {
		fi, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if perm := fi.Mode().Perm(); perm != 0o600 {
			t.Fatalf("%s mode = %o, want 600", p, perm)
		}
	}
	// Filenames must be derived from the hash, never from the track text.
	if !strings.HasPrefix(filepath.Base(imgPath), hashKey(key)) {
		t.Fatalf("image filename %q is not hash derived", filepath.Base(imgPath))
	}
}

func TestDiskCacheMisses(t *testing.T) {
	key := cacheKey("Artist", "Title", "")

	tests := []struct {
		name  string
		setup func(t *testing.T, c *Client)
	}{
		{"no metadata", func(t *testing.T, c *Client) {
			imgPath, _, _ := c.diskPaths(key)
			mustMkdirAll(t, c.cacheDir)
			mustWrite(t, imgPath, testPNG(t, 4, 4))
		}},
		{"no image", func(t *testing.T, c *Client) {
			_, metaPath, _ := c.diskPaths(key)
			mustMkdirAll(t, c.cacheDir)
			mustWrite(t, metaPath, mustJSON(t, diskMeta{CachedAt: time.Now()}))
		}},
		{"corrupt metadata", func(t *testing.T, c *Client) {
			imgPath, metaPath, _ := c.diskPaths(key)
			mustMkdirAll(t, c.cacheDir)
			mustWrite(t, imgPath, testPNG(t, 4, 4))
			mustWrite(t, metaPath, []byte("{not json"))
		}},
		{"stale entry", func(t *testing.T, c *Client) {
			imgPath, metaPath, _ := c.diskPaths(key)
			mustMkdirAll(t, c.cacheDir)
			mustWrite(t, imgPath, testPNG(t, 4, 4))
			mustWrite(t, metaPath, mustJSON(t, diskMeta{CachedAt: time.Now().Add(-diskTTL - time.Hour)}))
		}},
		{"zero timestamp", func(t *testing.T, c *Client) {
			imgPath, metaPath, _ := c.diskPaths(key)
			mustMkdirAll(t, c.cacheDir)
			mustWrite(t, imgPath, testPNG(t, 4, 4))
			mustWrite(t, metaPath, mustJSON(t, diskMeta{}))
		}},
		{"payload is not an image", func(t *testing.T, c *Client) {
			imgPath, metaPath, _ := c.diskPaths(key)
			mustMkdirAll(t, c.cacheDir)
			mustWrite(t, imgPath, []byte("definitely not an image"))
			mustWrite(t, metaPath, mustJSON(t, diskMeta{CachedAt: time.Now()}))
		}},
		{"empty payload", func(t *testing.T, c *Client) {
			imgPath, metaPath, _ := c.diskPaths(key)
			mustMkdirAll(t, c.cacheDir)
			mustWrite(t, imgPath, []byte{})
			mustWrite(t, metaPath, mustJSON(t, diskMeta{CachedAt: time.Now()}))
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := NewClient(filepath.Join(t.TempDir(), "covers"), "")
			tc.setup(t, c)
			if _, ok := c.diskGet(key); ok {
				t.Fatal("expected a cache miss")
			}
		})
	}
}

func TestDiskCacheDisabled(t *testing.T) {
	c := NewClient("", "")
	if _, _, ok := c.diskPaths("k"); ok {
		t.Fatal("disk paths should be unavailable without a cache dir")
	}
	if _, ok := c.diskGet("k"); ok {
		t.Fatal("unexpected disk hit")
	}
	if err := c.diskPut("k", &Cover{Data: []byte("x")}); err != nil {
		t.Fatalf("diskPut with caching disabled should be a no-op: %v", err)
	}
}

func TestCoverClone(t *testing.T) {
	if (*Cover)(nil).clone() != nil {
		t.Fatal("nil clone should stay nil")
	}
	orig := &Cover{Data: []byte("x"), Source: "iTunes"}
	dup := orig.clone()
	dup.Source = "Deezer"
	if orig.Source != "iTunes" {
		t.Fatal("clone shares the struct with the original")
	}
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return out
}
