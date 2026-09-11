package lyrics

import (
	"container/list"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// memCapacity bounds the in-memory sheet cache.
	memCapacity = 128
	// memTTL is how long a successful lookup stays in memory.
	memTTL = 6 * time.Hour
	// negativeTTL is how long a "no lyrics" marker stays in memory. Negative
	// results are never written to disk.
	negativeTTL = time.Hour
	// diskTTL is how long a cached sheet on disk is considered fresh.
	diskTTL = 30 * 24 * time.Hour

	cacheDirPerm  os.FileMode = 0o700
	cacheFilePerm os.FileMode = 0o600
)

// memResult is what the in-memory cache hands back: either a sheet or a
// negative marker meaning "no provider had lyrics for this key".
type memResult struct {
	sheet    *Sheet
	negative bool
}

type memEntry struct {
	key      string
	result   memResult
	storedAt time.Time
}

// memCache is a small thread-safe LRU with separate TTLs for positive and
// negative entries.
type memCache struct {
	mu          sync.Mutex
	capacity    int
	ttl         time.Duration
	negativeTTL time.Duration
	items       map[string]*list.Element
	order       *list.List
	now         func() time.Time
}

// newMemCache creates a memCache with the given capacity and time-to-live values.
func newMemCache(capacity int, ttl, negTTL time.Duration) *memCache {
	if capacity <= 0 {
		capacity = memCapacity
	}
	if ttl <= 0 {
		ttl = memTTL
	}
	if negTTL <= 0 {
		negTTL = negativeTTL
	}
	return &memCache{
		capacity:    capacity,
		ttl:         ttl,
		negativeTTL: negTTL,
		items:       make(map[string]*list.Element),
		order:       list.New(),
		now:         time.Now,
	}
}

// get returns the cached result for key when present and still fresh.
func (c *memCache) get(key string) (memResult, bool) {
	if c == nil || key == "" {
		return memResult{}, false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return memResult{}, false
	}

	entry := elem.Value.(*memEntry)
	ttl := c.ttl
	if entry.result.negative {
		ttl = c.negativeTTL
	}
	if c.now().Sub(entry.storedAt) > ttl {
		c.order.Remove(elem)
		delete(c.items, key)
		return memResult{}, false
	}

	c.order.MoveToFront(elem)
	return entry.result, true
}

// putSheet memoises a successful lookup.
func (c *memCache) putSheet(key string, sheet *Sheet) {
	if sheet == nil {
		return
	}
	c.put(key, memResult{sheet: sheet.clone()})
}

// putNegative memoises a "no lyrics" outcome for a shorter window so a station
// without matches does not hammer the providers on every track.
func (c *memCache) putNegative(key string) {
	c.put(key, memResult{negative: true})
}

func (c *memCache) put(key string, res memResult) {
	if c == nil || key == "" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*memEntry)
		entry.result = res
		entry.storedAt = c.now()
		c.order.MoveToFront(elem)
		return
	}

	for c.order.Len() >= c.capacity {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*memEntry).key)
	}

	c.items[key] = c.order.PushFront(&memEntry{
		key:      key,
		result:   res,
		storedAt: c.now(),
	})
}

// len reports how many entries are currently held.
func (c *memCache) len() int {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.items)
}

// clear drops every entry.
func (c *memCache) clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items = make(map[string]*list.Element)
	c.order.Init()
}

// diskLine is the on-disk form of a lyric line, storing offsets in
// milliseconds so cache files stay human-readable.
type diskLine struct {
	AtMS int64  `json:"at_ms"`
	Text string `json:"text"`
}

// diskSheet is the on-disk form of a Sheet.
type diskSheet struct {
	Artist       string     `json:"artist"`
	Title        string     `json:"title"`
	Album        string     `json:"album,omitempty"`
	DurationMS   int64      `json:"duration_ms,omitempty"`
	Synced       bool       `json:"synced"`
	Instrumental bool       `json:"instrumental,omitempty"`
	Source       string     `json:"source"`
	Lines        []diskLine `json:"lines"`
}

// diskEntry wraps a cached sheet with the time it was written.
type diskEntry struct {
	CachedAt time.Time  `json:"cached_at"`
	Sheet    *diskSheet `json:"sheet"`
}

func newDiskSheet(s *Sheet) *diskSheet {
	if s == nil {
		return nil
	}
	out := &diskSheet{
		Artist:       s.Artist,
		Title:        s.Title,
		Album:        s.Album,
		DurationMS:   s.Duration.Milliseconds(),
		Synced:       s.Synced,
		Instrumental: s.Instrumental,
		Source:       s.Source,
		Lines:        make([]diskLine, 0, len(s.Lines)),
	}
	for _, l := range s.Lines {
		out.Lines = append(out.Lines, diskLine{AtMS: l.At.Milliseconds(), Text: l.Text})
	}
	return out
}

func (d *diskSheet) toSheet() *Sheet {
	if d == nil {
		return nil
	}
	out := &Sheet{
		Artist:       d.Artist,
		Title:        d.Title,
		Album:        d.Album,
		Duration:     time.Duration(d.DurationMS) * time.Millisecond,
		Synced:       d.Synced,
		Instrumental: d.Instrumental,
		Source:       d.Source,
	}
	if len(d.Lines) > 0 {
		out.Lines = make([]Line, 0, len(d.Lines))
		for _, l := range d.Lines {
			out.Lines = append(out.Lines, Line{At: time.Duration(l.AtMS) * time.Millisecond, Text: l.Text})
		}
	}
	return out
}

// diskPath maps a cache key to its cache file. The key is hashed so raw track
// metadata, which may contain path separators, never reaches the filesystem.
func (c *Client) diskPath(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.cacheDir, hex.EncodeToString(sum[:])+".json")
}

// readDisk returns the cached sheet for key, or nil when it is missing, stale,
// or unreadable. Disk problems are never fatal.
func (c *Client) readDisk(key string) *Sheet {
	if c.cacheDir == "" || key == "" {
		return nil
	}

	c.diskMu.Lock()
	defer c.diskMu.Unlock()

	raw, err := os.ReadFile(c.diskPath(key))
	if err != nil {
		return nil
	}

	var entry diskEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil
	}
	if entry.Sheet == nil {
		return nil
	}
	if entry.CachedAt.IsZero() || time.Since(entry.CachedAt) > diskTTL {
		return nil
	}

	sheet := entry.Sheet.toSheet()
	if sheet.IsEmpty() {
		return nil
	}
	return sheet
}

// writeDisk persists a sheet, creating the cache directory lazily. Failures are
// silently ignored: the cache is an optimisation, not a requirement.
func (c *Client) writeDisk(key string, sheet *Sheet) {
	if c.cacheDir == "" || key == "" || sheet == nil {
		return
	}

	c.diskMu.Lock()
	defer c.diskMu.Unlock()

	if err := os.MkdirAll(c.cacheDir, cacheDirPerm); err != nil {
		return
	}

	payload, err := json.Marshal(diskEntry{CachedAt: time.Now().UTC(), Sheet: newDiskSheet(sheet)})
	if err != nil {
		return
	}

	path := c.diskPath(key)
	tmp, err := os.CreateTemp(c.cacheDir, "sheet-*.tmp")
	if err != nil {
		// Fall back to a direct write; still non-fatal on failure.
		_ = os.WriteFile(path, payload, cacheFilePerm)
		return
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	// os.CreateTemp already creates the file with 0600.
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
	}
}
