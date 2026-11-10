package lyrics

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/halpworld/halpradio/pkg/radio/netguard"
	"github.com/halpworld/halpradio/pkg/util"
)

const (
	secOSC52 = "\x1b]52;c;Y3VybCBldmlsLnNoIHwgc2g=\x07"
	secCSI   = "\x1b[2J\x1b[H"
	secC1    = "\u009b2J"
	secBidi  = "‮"
)

func assertSheetClean(t *testing.T, where string, s *Sheet) {
	t.Helper()
	if s == nil {
		t.Fatalf("%s: nil sheet", where)
	}
	for _, v := range []string{s.Artist, s.Title, s.Album, s.Source} {
		if !util.IsDisplaySafe(v) || len([]rune(v)) > maxMetaRunes {
			t.Errorf("%s: unsafe metadata %q", where, v)
		}
	}
	if len(s.Lines) > maxLines {
		t.Errorf("%s: %d lines kept", where, len(s.Lines))
	}
	for _, l := range s.Lines {
		if !util.IsDisplaySafe(l.Text) || len([]rune(l.Text)) > maxLineRunes {
			t.Errorf("%s: unsafe line %q", where, l.Text)
		}
	}
}

func hostileLRCLib(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": 1, "trackName": "Song" + secOSC52, "artistName": "Artist" + secC1, "albumName": "Album" + secBidi,
			"duration":     200.0,
			"plainLyrics":  "hello " + secOSC52 + " world\nsecond line " + secCSI,
			"syncedLyrics": "[00:01.00]hello " + secOSC52 + " world\n[00:05.00]bye" + secC1 + strings.Repeat("x", 5000),
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLyricsEscapesStrippedAtParseAndPersist(t *testing.T) {
	srv := hostileLRCLib(t)
	dir := t.TempDir()
	c := NewClient(dir)
	c.LRCLibBaseURL = srv.URL
	c.NetEaseBaseURL = "http://127.0.0.1:1"
	sheet, err := c.Fetch(context.Background(), "Artist", "Song", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertSheetClean(t, "Fetch", sheet)

	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			b, _ := os.ReadFile(p)
			if bytes.Contains(b, []byte(`\u001b`)) || bytes.Contains(b, []byte("\u009b")) {
				t.Errorf("escape sequence persisted in %s", filepath.Base(p))
			}
			if info.Mode().Perm() != 0o600 {
				t.Errorf("cache file mode %v", info.Mode().Perm())
			}
		}
		return nil
	})
}

func TestLyricsNetEaseEscapesStripped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/search/get":
			_, _ = w.Write([]byte(`{"result":{"songs":[{"id":7,"name":"Song\u001b]52;c;eA==\u0007","artists":[{"name":"Artist\u009b2J"}],"album":{"name":"Al‮bum"}}]}}`))
		case "/api/song/lyric":
			_, _ = w.Write([]byte(`{"lrc":{"lyric":"[00:01.00]hi \u001b[2J there\n[00:03.00]x"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := NewClient("")
	c.LRCLibBaseURL = "http://127.0.0.1:1"
	c.NetEaseBaseURL = srv.URL
	sheet, err := c.Fetch(context.Background(), "Artist", "Song", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	assertSheetClean(t, "NetEase", sheet)
}

func TestLyricsDiskCacheReadSanitized(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(dir)
	c.LRCLibBaseURL = "http://127.0.0.1:1"
	c.NetEaseBaseURL = "http://127.0.0.1:1"
	key := cacheKey("Artist", "Song", "", 0)

	// A poisoned cache file written by an older version (or edited).
	lines := make([]diskLine, 0, maxLines+50)
	for i := 0; i < maxLines+50; i++ {
		lines = append(lines, diskLine{AtMS: int64(i), Text: "line " + secOSC52 + strings.Repeat("y", 1000)})
	}
	entry := diskEntry{CachedAt: time.Now(), Sheet: &diskSheet{Artist: "A" + secCSI, Title: "T" + secBidi, Source: "LRCLIB" + secC1, Synced: true, Lines: lines}}
	b, _ := json.Marshal(entry)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.diskPath(key), b, 0o600); err != nil {
		t.Fatal(err)
	}
	sheet, err := c.Fetch(context.Background(), "Artist", "Song", "", 0)
	if err != nil {
		t.Fatalf("cached sheet not served: %v", err)
	}
	assertSheetClean(t, "disk cache", sheet)
}

func TestLyricsBodyLimit(t *testing.T) {
	// The PoC: ~100 KB of gzip that expands to 96 MB of JSON.
	var gz bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	_, _ = zw.Write([]byte(`{"id":1,"trackName":"Song","artistName":"Artist","plainLyrics":"`))
	chunk := bytes.Repeat([]byte("A"), 1<<20)
	for i := 0; i < 96; i++ {
		_, _ = zw.Write(chunk)
	}
	_, _ = zw.Write([]byte(`"}`))
	_ = zw.Close()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(gz.Bytes())
	}))
	defer srv.Close()

	c := NewClient("")
	c.LRCLibBaseURL = srv.URL
	c.NetEaseBaseURL = "http://127.0.0.1:1"
	sheet, _ := c.Fetch(context.Background(), "Artist", "Song", "", 0)
	if sheet != nil {
		t.Fatalf("oversized body produced a sheet with %d lines", len(sheet.Lines))
	}
}

func TestLyricsThrottleAndDedup(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := NewClient("")
	c.LRCLibBaseURL = srv.URL
	c.DisableNetEase = true
	now := time.Unix(1000, 0)
	c.limiter.SetClock(func() time.Time { return now })

	// A station rotating its title every second: only the burst reaches
	// the provider; the rest are throttled.
	for i := 0; i < 20; i++ {
		_, _ = c.Fetch(context.Background(), "Artist", "Song "+string(rune('a'+i)), "", 0)
	}
	if got := hits.Load(); got > int64(limiterBurst)*2 {
		t.Errorf("provider hit %d times for 20 rapid titles; throttle not applied", got)
	}
	before := hits.Load()
	now = now.Add(limiterInterval)
	_, _ = c.Fetch(context.Background(), "Artist", "Song z", "", 0)
	if hits.Load() == before {
		t.Errorf("provider not asked again after the throttle interval")
	}
}

func TestLyricsDisableNetEase(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := NewClient("")
	c.LRCLibBaseURL = "http://127.0.0.1:1"
	c.NetEaseBaseURL = srv.URL
	c.DisableNetEase = true
	_, _ = c.Fetch(context.Background(), "Artist", "Song", "", 0)
	if hits.Load() != 0 {
		t.Errorf("NetEase contacted while disabled")
	}
}

func TestLyricsRedirectToLoopbackRefused(t *testing.T) {
	c := NewClient("")
	// The default client must refuse a hop from a public origin to a
	// loopback address (redirect or DNS), whatever the provider says.
	ctx := netguard.WithOrigin(context.Background(), "https://lrclib.net/")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:1/", nil)
	if _, err := c.httpClient().Do(req); err == nil {
		t.Fatal("loopback reachable from a public origin")
	}
	if c.HTTPClient.CheckRedirect == nil {
		t.Fatal("default lyrics client has no redirect policy")
	}
}

func TestLRCLineAndCountCaps(t *testing.T) {
	var b strings.Builder
	for i := 0; i < maxLines+100; i++ {
		b.WriteString("[00:01.00]" + strings.Repeat("z", 800) + secOSC52 + "\n")
	}
	lines := ParseLRC(b.String())
	if len(lines) > maxLines {
		t.Errorf("ParseLRC kept %d lines", len(lines))
	}
	for _, l := range lines {
		if !util.IsDisplaySafe(l.Text) || len([]rune(l.Text)) > maxLineRunes {
			t.Fatalf("unsafe line from ParseLRC")
		}
	}
	plain := plainToLines(strings.Repeat("a"+secCSI+"\n", maxLines+100))
	if len(plain) > maxLines {
		t.Errorf("plainToLines kept %d lines", len(plain))
	}
	for _, l := range plain {
		if !util.IsDisplaySafe(l.Text) {
			t.Fatalf("unsafe line from plainToLines: %q", l.Text)
		}
	}
}

func TestLyricsDiskCachePruned(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, strings.Repeat("a", 64)+".json")
	if err := os.WriteFile(stale, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * diskTTL)
	_ = os.Chtimes(stale, old, old)
	c := NewClient(dir)
	c.writeDisk("k", &Sheet{Title: "t", Lines: []Line{{Text: "x"}}})
	if _, err := os.Stat(stale); err == nil {
		t.Errorf("expired cache entry not pruned")
	}
}
