package lyrics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestMemCachePositiveAndNegative(t *testing.T) {
	c := newMemCache(4, time.Hour, time.Minute)

	if _, ok := c.get("missing"); ok {
		t.Error("empty cache reported a hit")
	}
	if _, ok := c.get(""); ok {
		t.Error("empty key reported a hit")
	}

	sheet := syncedSheet()
	c.putSheet("k", sheet)

	res, ok := c.get("k")
	if !ok {
		t.Fatal("expected a hit after putSheet")
	}
	if res.negative {
		t.Error("positive entry reported as negative")
	}
	if !reflect.DeepEqual(res.sheet, sheet) {
		t.Errorf("cached sheet = %+v, want %+v", res.sheet, sheet)
	}

	c.putNegative("n")
	res, ok = c.get("n")
	if !ok || !res.negative || res.sheet != nil {
		t.Errorf("negative entry = %+v, ok=%v", res, ok)
	}

	c.putSheet("nil-sheet", nil)
	if _, ok := c.get("nil-sheet"); ok {
		t.Error("nil sheets must not be cached")
	}
}

func TestMemCacheStoresCopies(t *testing.T) {
	c := newMemCache(4, time.Hour, time.Minute)
	sheet := syncedSheet()
	c.putSheet("k", sheet)

	sheet.Lines[0].Text = "mutated after put"
	sheet.Title = "mutated after put"

	res, _ := c.get("k")
	if res.sheet.Lines[0].Text != "first" || res.sheet.Title != "Voyager" {
		t.Error("cache kept a reference to the caller's sheet")
	}
}

func TestMemCacheTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := newMemCache(4, time.Hour, time.Minute)
	c.now = func() time.Time { return now }

	c.putSheet("pos", syncedSheet())
	c.putNegative("neg")

	tests := []struct {
		name       string
		advance    time.Duration
		wantPosHit bool
		wantNegHit bool
	}{
		{name: "fresh", advance: 0, wantPosHit: true, wantNegHit: true},
		{name: "after 30s", advance: 30 * time.Second, wantPosHit: true, wantNegHit: true},
		{name: "negative expired", advance: 2 * time.Minute, wantPosHit: true, wantNegHit: false},
		{name: "both expired", advance: 2 * time.Hour, wantPosHit: false, wantNegHit: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now = time.Unix(1_700_000_000, 0).Add(tt.advance)
			if _, ok := c.get("pos"); ok != tt.wantPosHit {
				t.Errorf("positive hit = %v, want %v", ok, tt.wantPosHit)
			}
			if _, ok := c.get("neg"); ok != tt.wantNegHit {
				t.Errorf("negative hit = %v, want %v", ok, tt.wantNegHit)
			}
		})
	}
}

func TestMemCacheExpiredEntryIsEvicted(t *testing.T) {
	now := time.Unix(0, 0)
	c := newMemCache(4, time.Minute, time.Minute)
	c.now = func() time.Time { return now }

	c.putSheet("k", syncedSheet())
	now = now.Add(2 * time.Minute)

	if _, ok := c.get("k"); ok {
		t.Fatal("expected a miss for the expired entry")
	}
	if c.len() != 0 {
		t.Errorf("expired entry was not dropped, len = %d", c.len())
	}
}

func TestMemCacheLRUEviction(t *testing.T) {
	c := newMemCache(3, time.Hour, time.Hour)
	for i := 0; i < 3; i++ {
		c.putSheet(strconv.Itoa(i), syncedSheet())
	}

	// Touch "0" so that "1" becomes the least recently used entry.
	if _, ok := c.get("0"); !ok {
		t.Fatal("expected key 0 to be cached")
	}
	c.putSheet("3", syncedSheet())

	if c.len() != 3 {
		t.Errorf("len = %d, want 3", c.len())
	}
	if _, ok := c.get("1"); ok {
		t.Error("least recently used entry was not evicted")
	}
	for _, key := range []string{"0", "2", "3"} {
		if _, ok := c.get(key); !ok {
			t.Errorf("key %q should still be cached", key)
		}
	}
}

func TestMemCacheOverwriteAndClear(t *testing.T) {
	c := newMemCache(4, time.Hour, time.Hour)
	c.putSheet("k", syncedSheet())
	c.putNegative("k")

	res, ok := c.get("k")
	if !ok || !res.negative {
		t.Errorf("overwrite did not replace the entry: %+v", res)
	}
	if c.len() != 1 {
		t.Errorf("len = %d, want 1", c.len())
	}

	c.clear()
	if c.len() != 0 {
		t.Errorf("len after clear = %d, want 0", c.len())
	}
}

func TestMemCacheDefaults(t *testing.T) {
	c := newMemCache(0, 0, 0)
	if c.capacity != memCapacity || c.ttl != memTTL || c.negativeTTL != negativeTTL {
		t.Errorf("defaults not applied: %d %v %v", c.capacity, c.ttl, c.negativeTTL)
	}
}

func TestMemCacheConcurrentAccess(t *testing.T) {
	c := newMemCache(16, time.Hour, time.Hour)
	var wg sync.WaitGroup

	for i := 0; i < runtime.NumCPU()+4; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := strconv.Itoa(n % 8)
			for j := 0; j < 200; j++ {
				c.putSheet(key, syncedSheet())
				c.get(key)
				c.putNegative(key + "-neg")
				c.len()
			}
		}(i)
	}
	wg.Wait()
}

func TestDiskCacheRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "lyrics")
	c := NewClient(dir)

	if got := c.readDisk("absent"); got != nil {
		t.Errorf("readDisk on an empty cache = %+v, want nil", got)
	}

	want := &Sheet{
		Artist:   "Daft Punk",
		Title:    "Voyager",
		Album:    "Discovery",
		Duration: 227 * time.Second,
		Synced:   true,
		Source:   SourceLRCLib,
		Lines: []Line{
			{At: 1500 * time.Millisecond, Text: "one"},
			{At: 4 * time.Second, Text: "two"},
		},
	}

	c.writeDisk("key", want)

	got := c.readDisk("key")
	if got == nil {
		t.Fatal("readDisk returned nil after writeDisk")
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round trip mismatch\n got: %+v\nwant: %+v", got, want)
	}

	// The directory and file carry restrictive permissions.
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat cache dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != cacheDirPerm {
		t.Errorf("cache dir perm = %o, want %o", perm, cacheDirPerm)
	}

	fileInfo, err := os.Stat(c.diskPath("key"))
	if err != nil {
		t.Fatalf("stat cache file: %v", err)
	}
	if perm := fileInfo.Mode().Perm(); perm != cacheFilePerm {
		t.Errorf("cache file perm = %o, want %o", perm, cacheFilePerm)
	}

	// No temporary files are left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("cache dir holds %d entries, want 1", len(entries))
	}
}

func TestDiskPathHashesTheKey(t *testing.T) {
	c := NewClient(t.TempDir())
	nasty := "../../etc|pa/ss wd|../x|0"

	path := c.diskPath(nasty)
	if filepath.Dir(path) != c.CacheDir() {
		t.Errorf("diskPath escaped the cache dir: %q", path)
	}

	name := filepath.Base(path)
	if len(name) != 64+len(".json") {
		t.Errorf("cache file name %q is not a sha256 hex digest", name)
	}
	if c.diskPath(nasty) != path {
		t.Error("diskPath is not deterministic")
	}
	if c.diskPath("other") == path {
		t.Error("different keys collided")
	}
}

func TestDiskCacheStaleAndCorrupt(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(dir)

	writeEntry := func(key string, entry diskEntry) {
		t.Helper()
		payload, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if err := os.WriteFile(c.diskPath(key), payload, cacheFilePerm); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	fresh := newDiskSheet(syncedSheet())

	tests := []struct {
		name    string
		key     string
		setup   func(key string)
		wantHit bool
	}{
		{
			name: "fresh entry",
			key:  "fresh",
			setup: func(key string) {
				writeEntry(key, diskEntry{CachedAt: time.Now(), Sheet: fresh})
			},
			wantHit: true,
		},
		{
			name: "just inside the window",
			key:  "recent",
			setup: func(key string) {
				writeEntry(key, diskEntry{CachedAt: time.Now().Add(-29 * 24 * time.Hour), Sheet: fresh})
			},
			wantHit: true,
		},
		{
			name: "stale entry",
			key:  "stale",
			setup: func(key string) {
				writeEntry(key, diskEntry{CachedAt: time.Now().Add(-31 * 24 * time.Hour), Sheet: fresh})
			},
			wantHit: false,
		},
		{
			name: "missing timestamp",
			key:  "no-time",
			setup: func(key string) {
				writeEntry(key, diskEntry{Sheet: fresh})
			},
			wantHit: false,
		},
		{
			name: "missing sheet",
			key:  "no-sheet",
			setup: func(key string) {
				writeEntry(key, diskEntry{CachedAt: time.Now()})
			},
			wantHit: false,
		},
		{
			name: "sheet without renderable lines",
			key:  "blank",
			setup: func(key string) {
				writeEntry(key, diskEntry{
					CachedAt: time.Now(),
					Sheet:    &diskSheet{Title: "x", Lines: []diskLine{{Text: "  "}}},
				})
			},
			wantHit: false,
		},
		{
			name: "corrupt json",
			key:  "corrupt",
			setup: func(key string) {
				if err := os.WriteFile(c.diskPath(key), []byte("{not json"), cacheFilePerm); err != nil {
					t.Fatalf("write: %v", err)
				}
			},
			wantHit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(tt.key)
			got := c.readDisk(tt.key)
			if (got != nil) != tt.wantHit {
				t.Errorf("readDisk(%q) hit = %v, want %v", tt.key, got != nil, tt.wantHit)
			}
		})
	}
}

func TestDiskCacheDisabledAndNonFatal(t *testing.T) {
	// An empty cache dir disables disk caching entirely.
	c := NewClient("")
	if c.CacheDir() != "" {
		t.Errorf("CacheDir = %q, want empty", c.CacheDir())
	}
	c.writeDisk("k", syncedSheet())
	if got := c.readDisk("k"); got != nil {
		t.Errorf("readDisk with caching disabled = %+v, want nil", got)
	}

	// An unusable cache dir must not be fatal.
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	blocked := NewClient(filepath.Join(file, "cache"))
	blocked.writeDisk("k", syncedSheet())
	if got := blocked.readDisk("k"); got != nil {
		t.Errorf("readDisk on an unusable dir = %+v, want nil", got)
	}

	// Empty keys and nil sheets are ignored.
	ok := NewClient(t.TempDir())
	ok.writeDisk("", syncedSheet())
	ok.writeDisk("k", nil)
	if got := ok.readDisk(""); got != nil {
		t.Errorf("readDisk(\"\") = %+v, want nil", got)
	}
}

func TestDiskSheetConversionEdges(t *testing.T) {
	if newDiskSheet(nil) != nil {
		t.Error("newDiskSheet(nil) should be nil")
	}
	if (*diskSheet)(nil).toSheet() != nil {
		t.Error("(*diskSheet)(nil).toSheet() should be nil")
	}

	empty := newDiskSheet(&Sheet{Title: "t"})
	if empty == nil || len(empty.Lines) != 0 {
		t.Fatalf("unexpected conversion: %+v", empty)
	}
	if back := empty.toSheet(); back.Lines != nil {
		t.Errorf("empty lines should stay nil, got %v", back.Lines)
	}
}

func TestDiskCacheConcurrentWrites(t *testing.T) {
	c := NewClient(t.TempDir())
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := "key-" + strconv.Itoa(n%3)
			for j := 0; j < 20; j++ {
				c.writeDisk(key, syncedSheet())
				c.readDisk(key)
			}
		}(i)
	}
	wg.Wait()

	if got := c.readDisk("key-0"); got == nil {
		t.Error("expected key-0 to be cached on disk")
	}
}
