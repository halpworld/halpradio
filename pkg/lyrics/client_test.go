package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const testSyncedLRC = "[ar:Daft Punk]\n[00:10.00]first\n[00:20.00]second\n[00:30.00]third"

// recordingServer is a test provider that records the requests it served.
type recordingServer struct {
	*httptest.Server

	mu       sync.Mutex
	requests []*url.URL
	headers  []http.Header
	hits     atomic.Int64
}

func newRecordingServer(t *testing.T, routes map[string]http.HandlerFunc) *recordingServer {
	t.Helper()

	rec := &recordingServer{}
	mux := http.NewServeMux()
	for pattern, handler := range routes {
		h := handler
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			rec.mu.Lock()
			rec.requests = append(rec.requests, r.URL)
			rec.headers = append(rec.headers, r.Header.Clone())
			rec.mu.Unlock()
			rec.hits.Add(1)
			h(w, r)
		})
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request to %s", r.URL)
		http.NotFound(w, r)
	})

	rec.Server = httptest.NewServer(mux)
	t.Cleanup(rec.Server.Close)
	return rec
}

func (r *recordingServer) lastQuery(t *testing.T) url.Values {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) == 0 {
		t.Fatal("no requests recorded")
	}
	return r.requests[len(r.requests)-1].Query()
}

func (r *recordingServer) requestAt(t *testing.T, i int) *url.URL {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= len(r.requests) {
		t.Fatalf("request %d not recorded (have %d)", i, len(r.requests))
	}
	return r.requests[i]
}

func (r *recordingServer) headerAt(t *testing.T, i int) http.Header {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= len(r.headers) {
		t.Fatalf("request %d not recorded (have %d)", i, len(r.headers))
	}
	return r.headers[i]
}

func writeJSON(t *testing.T, w http.ResponseWriter, payload any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

// notFoundHandler stands in for a provider that has no match.
func notFoundHandler(w http.ResponseWriter, _ *http.Request) {
	http.Error(w, "not found", http.StatusNotFound)
}

// emptyArrayHandler stands in for an LRCLIB search with no results.
func emptyArrayHandler(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("[]"))
}

// testClient wires a Client to the given fake providers and disables disk
// caching unless cacheDir is set.
func testClient(cacheDir string, lrclib, netease *recordingServer) *Client {
	c := NewClient(cacheDir)
	c.HTTPClient = &http.Client{Timeout: 5 * time.Second}
	if lrclib != nil {
		c.LRCLibBaseURL = lrclib.URL
	} else {
		c.LRCLibBaseURL = "http://127.0.0.1:1/unreachable"
	}
	if netease != nil {
		c.NetEaseBaseURL = netease.URL
	} else {
		c.NetEaseBaseURL = "http://127.0.0.1:1/unreachable"
	}
	return c
}

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("  /tmp/halpradio-lyrics  ")
	if c.LRCLibBaseURL != DefaultLRCLibBaseURL {
		t.Errorf("LRCLibBaseURL = %q, want %q", c.LRCLibBaseURL, DefaultLRCLibBaseURL)
	}
	if c.NetEaseBaseURL != DefaultNetEaseBaseURL {
		t.Errorf("NetEaseBaseURL = %q, want %q", c.NetEaseBaseURL, DefaultNetEaseBaseURL)
	}
	if c.HTTPClient == nil || c.HTTPClient.Timeout != DefaultTimeout {
		t.Errorf("HTTPClient = %+v, want a %v timeout", c.HTTPClient, DefaultTimeout)
	}
	if c.CacheDir() != "/tmp/halpradio-lyrics" {
		t.Errorf("CacheDir = %q, want the trimmed path", c.CacheDir())
	}

	empty := NewClient("")
	if empty.CacheDir() != "" {
		t.Errorf("CacheDir = %q, want empty", empty.CacheDir())
	}
}

func TestClientBaseURLFallbacks(t *testing.T) {
	c := &Client{}
	if got := c.lrclibBase(); got != DefaultLRCLibBaseURL {
		t.Errorf("lrclibBase = %q, want %q", got, DefaultLRCLibBaseURL)
	}
	if got := c.netEaseBase(); got != DefaultNetEaseBaseURL {
		t.Errorf("netEaseBase = %q, want %q", got, DefaultNetEaseBaseURL)
	}
	if c.httpClient() == nil {
		t.Error("httpClient must never be nil")
	}

	trailing := &Client{LRCLibBaseURL: "http://example.test/ ", NetEaseBaseURL: " http://ne.test//"}
	if got := trailing.lrclibBase(); got != "http://example.test" {
		t.Errorf("lrclibBase = %q, want trailing slash trimmed", got)
	}
	if got := trailing.netEaseBase(); got != "http://ne.test" {
		t.Errorf("netEaseBase = %q, want trailing slashes trimmed", got)
	}
}

func TestFetchLRCLibExactSynced(t *testing.T) {
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/get": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, lrcLibTrack{
				ID:           1,
				TrackName:    "Voyager",
				ArtistName:   "Daft Punk",
				AlbumName:    "Discovery",
				Duration:     227.5,
				PlainLyrics:  "first\nsecond\nthird",
				SyncedLyrics: testSyncedLRC,
			})
		},
	})

	c := testClient("", lrclib, nil)
	sheet, err := c.Fetch(context.Background(), "Daft Punk", "Voyager", "Discovery", 227*time.Second)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if sheet.Source != SourceLRCLib {
		t.Errorf("Source = %q, want %q", sheet.Source, SourceLRCLib)
	}
	if !sheet.Synced {
		t.Error("Synced = false, want true when syncedLyrics are present")
	}
	if sheet.Instrumental {
		t.Error("Instrumental = true, want false")
	}
	if len(sheet.Lines) != 3 {
		t.Fatalf("len(Lines) = %d, want 3", len(sheet.Lines))
	}
	if sheet.Lines[0].At != 10*time.Second || sheet.Lines[0].Text != "first" {
		t.Errorf("first line = %+v", sheet.Lines[0])
	}
	if sheet.Artist != "Daft Punk" || sheet.Title != "Voyager" || sheet.Album != "Discovery" {
		t.Errorf("metadata = %q / %q / %q", sheet.Artist, sheet.Title, sheet.Album)
	}
	if sheet.Duration != 227500*time.Millisecond {
		t.Errorf("Duration = %v, want 227.5s", sheet.Duration)
	}

	q := lrclib.lastQuery(t)
	for key, want := range map[string]string{
		"artist_name": "Daft Punk",
		"track_name":  "Voyager",
		"album_name":  "Discovery",
		"duration":    "227",
	} {
		if got := q.Get(key); got != want {
			t.Errorf("query %s = %q, want %q", key, got, want)
		}
	}

	if ua := lrclib.headerAt(t, 0).Get("User-Agent"); !strings.Contains(ua, "halpradio") {
		t.Errorf("User-Agent = %q, want a descriptive halpradio agent", ua)
	}
}

func TestFetchLRCLibNormalisesQueryTerms(t *testing.T) {
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/get": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, lrcLibTrack{TrackName: "Halo", ArtistName: "Beyoncé", SyncedLyrics: testSyncedLRC})
		},
	})

	c := testClient("", lrclib, nil)
	sheet, err := c.Fetch(context.Background(), "Beyoncé", "Halo (feat. Jay-Z) [HQ]", "", 0)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	q := lrclib.lastQuery(t)
	if got := q.Get("track_name"); got != "Halo" {
		t.Errorf("track_name = %q, want the normalised %q", got, "Halo")
	}
	if q.Has("duration") {
		t.Error("duration must be omitted when unknown")
	}
	if q.Has("album_name") {
		t.Error("album_name must be omitted when unknown")
	}
	if sheet.Title != "Halo" {
		t.Errorf("Title = %q, want the provider value", sheet.Title)
	}
}

func TestFetchLRCLibPlainOnly(t *testing.T) {
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/get": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, lrcLibTrack{
				TrackName:   "Voyager",
				ArtistName:  "Daft Punk",
				PlainLyrics: "\nline one\nline two\n\n",
			})
		},
	})

	c := testClient("", lrclib, nil)
	sheet, err := c.Fetch(context.Background(), "Daft Punk", "Voyager", "", 0)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if sheet.Synced {
		t.Error("Synced = true, want false for plain lyrics")
	}
	want := []string{"line one", "line two"}
	got := sheet.PlainLines()
	if len(got) != len(want) {
		t.Fatalf("PlainLines = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
	if sheet.ActiveIndex(time.Minute) != -1 {
		t.Error("unsynced sheets must report ActiveIndex -1")
	}
}

func TestFetchLRCLibInstrumental(t *testing.T) {
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/get": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, lrcLibTrack{
				TrackName:    "Nightcall",
				ArtistName:   "Kavinsky",
				Instrumental: true,
			})
		},
	})

	c := testClient("", lrclib, nil)
	sheet, err := c.Fetch(context.Background(), "Kavinsky", "Nightcall", "", 0)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !sheet.Instrumental {
		t.Error("Instrumental = false, want true")
	}
	if sheet.Synced {
		t.Error("Synced = true, want false for an instrumental sheet")
	}
	if len(sheet.Lines) != 1 || sheet.Lines[0].Text != InstrumentalMarker {
		t.Errorf("Lines = %+v, want a single %q line", sheet.Lines, InstrumentalMarker)
	}
	if sheet.IsEmpty() {
		t.Error("an instrumental sheet is renderable")
	}
}

func TestFetchFallsBackToSearch(t *testing.T) {
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/get": notFoundHandler,
		"/api/search": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, []lrcLibTrack{
				{TrackName: "Voyager", ArtistName: "Daft Punk", Duration: 100, PlainLyrics: "plain only"},
				{TrackName: "Voyager", ArtistName: "Daft Punk", Duration: 400, SyncedLyrics: testSyncedLRC},
			})
		},
	})

	c := testClient("", lrclib, nil)
	sheet, err := c.Fetch(context.Background(), "Daft Punk", "Voyager", "", 120*time.Second)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !sheet.Synced {
		t.Error("search should prefer the synced candidate even when its duration is further away")
	}
	if got := lrclib.requestAt(t, 1).Path; got != "/api/search" {
		t.Errorf("second request path = %q, want /api/search", got)
	}
	if q := lrclib.lastQuery(t); q.Get("track_name") != "Voyager" || q.Get("artist_name") != "Daft Punk" {
		t.Errorf("search query = %v", q)
	}
}

func TestPickLRCLibCandidate(t *testing.T) {
	synced := lrcLibTrack{TrackName: "s", Duration: 300, SyncedLyrics: testSyncedLRC}
	plainNear := lrcLibTrack{TrackName: "near", Duration: 200, PlainLyrics: "x"}
	plainFar := lrcLibTrack{TrackName: "far", Duration: 500, PlainLyrics: "x"}
	instrumental := lrcLibTrack{TrackName: "inst", Duration: 199, Instrumental: true}
	useless := lrcLibTrack{TrackName: "useless", Duration: 200}

	tests := []struct {
		name       string
		candidates []lrcLibTrack
		duration   time.Duration
		wantTitle  string
	}{
		{name: "no candidates", candidates: nil, wantTitle: ""},
		{name: "all useless", candidates: []lrcLibTrack{useless}, wantTitle: ""},
		{name: "single plain", candidates: []lrcLibTrack{plainFar}, wantTitle: "far"},
		{
			name:       "synced wins over plain",
			candidates: []lrcLibTrack{plainNear, synced},
			duration:   200 * time.Second,
			wantTitle:  "s",
		},
		{
			name:       "closest duration among plain",
			candidates: []lrcLibTrack{plainFar, plainNear},
			duration:   210 * time.Second,
			wantTitle:  "near",
		},
		{
			name:       "first wins without a duration hint",
			candidates: []lrcLibTrack{plainFar, plainNear},
			wantTitle:  "far",
		},
		{
			name:       "useless candidates are skipped",
			candidates: []lrcLibTrack{useless, plainNear},
			wantTitle:  "near",
		},
		{
			name:       "instrumental counts as lyrics",
			candidates: []lrcLibTrack{useless, instrumental},
			wantTitle:  "inst",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pickLRCLibCandidate(tt.candidates, tt.duration)
			if tt.wantTitle == "" {
				if got != nil {
					t.Fatalf("picked %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("picked nil, want %q", tt.wantTitle)
			}
			if got.TrackName != tt.wantTitle {
				t.Errorf("picked %q, want %q", got.TrackName, tt.wantTitle)
			}
		})
	}
}

func TestFetchFallsBackToNetEase(t *testing.T) {
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/get":    notFoundHandler,
		"/api/search": emptyArrayHandler,
	})
	netease := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/search/get": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{
				"result": map[string]any{
					"songs": []map[string]any{
						{
							"id":       7,
							"name":     "Voyager",
							"artists":  []map[string]any{{"name": "Daft Punk"}},
							"album":    map[string]any{"name": "Discovery"},
							"duration": 227000,
						},
					},
				},
			})
		},
		"/api/song/lyric": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{
				"lrc":    map[string]any{"lyric": testSyncedLRC},
				"tlyric": map[string]any{"lyric": "[00:10.00]translated"},
			})
		},
	})

	c := testClient("", lrclib, netease)
	sheet, err := c.Fetch(context.Background(), "Daft Punk", "Voyager", "", 227*time.Second)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	if sheet.Source != SourceNetEase {
		t.Errorf("Source = %q, want %q", sheet.Source, SourceNetEase)
	}
	if !sheet.Synced || len(sheet.Lines) != 3 {
		t.Errorf("expected 3 synced lines, got synced=%v lines=%+v", sheet.Synced, sheet.Lines)
	}
	if sheet.Album != "Discovery" || sheet.Duration != 227*time.Second {
		t.Errorf("metadata = %q / %v", sheet.Album, sheet.Duration)
	}
	for _, line := range sheet.Lines {
		if line.Text == "translated" {
			t.Error("the translation track must be ignored")
		}
	}

	search := netease.requestAt(t, 0)
	if search.Path != "/api/search/get" {
		t.Fatalf("first NetEase path = %q", search.Path)
	}
	q := search.Query()
	if q.Get("s") != "Daft Punk Voyager" || q.Get("type") != "1" || q.Get("limit") != "5" {
		t.Errorf("NetEase search query = %v", q)
	}

	lyric := netease.requestAt(t, 1).Query()
	if lyric.Get("id") != "7" || lyric.Get("lv") != "1" || lyric.Get("kv") != "1" || lyric.Get("tv") != "-1" {
		t.Errorf("NetEase lyric query = %v", lyric)
	}

	headers := netease.headerAt(t, 0)
	if !strings.Contains(headers.Get("User-Agent"), "Mozilla") {
		t.Errorf("NetEase User-Agent = %q, want a browser-like agent", headers.Get("User-Agent"))
	}
	if headers.Get("Referer") != DefaultNetEaseBaseURL {
		t.Errorf("Referer = %q, want %q", headers.Get("Referer"), DefaultNetEaseBaseURL)
	}
}

func TestFetchNetEasePlainLyric(t *testing.T) {
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/get":    notFoundHandler,
		"/api/search": emptyArrayHandler,
	})
	netease := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/search/get": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{"result": map[string]any{"songs": []map[string]any{
				{"id": 9, "name": "Voyager", "artists": []map[string]any{{"name": "Daft Punk"}}},
			}}})
		},
		"/api/song/lyric": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{"lrc": map[string]any{"lyric": "no timestamps here\nsecond line"}})
		},
	})

	c := testClient("", lrclib, netease)
	sheet, err := c.Fetch(context.Background(), "Daft Punk", "Voyager", "", 0)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if sheet.Synced {
		t.Error("Synced = true, want false for a lyric without timestamps")
	}
	if len(sheet.Lines) != 2 {
		t.Errorf("Lines = %+v, want 2", sheet.Lines)
	}
}

func TestFetchNotFound(t *testing.T) {
	tests := []struct {
		name    string
		lrclib  map[string]http.HandlerFunc
		netease map[string]http.HandlerFunc
	}{
		{
			name: "every provider has no match",
			lrclib: map[string]http.HandlerFunc{
				"/api/get":    notFoundHandler,
				"/api/search": emptyArrayHandler,
			},
			netease: map[string]http.HandlerFunc{
				"/api/search/get": func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(`{"result":{"songs":[]}}`))
				},
			},
		},
		{
			name: "lrclib returns a track without lyrics",
			lrclib: map[string]http.HandlerFunc{
				"/api/get": func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(`{"trackName":"Voyager","artistName":"Daft Punk","plainLyrics":null,"syncedLyrics":null}`))
				},
				"/api/search": emptyArrayHandler,
			},
			netease: map[string]http.HandlerFunc{
				"/api/search/get": notFoundHandler,
			},
		},
		{
			name: "netease answers with an unexpected shape",
			lrclib: map[string]http.HandlerFunc{
				"/api/get":    notFoundHandler,
				"/api/search": emptyArrayHandler,
			},
			netease: map[string]http.HandlerFunc{
				"/api/search/get": func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(`<html>nope</html>`))
				},
			},
		},
		{
			name: "netease lyric payload is empty",
			lrclib: map[string]http.HandlerFunc{
				"/api/get":    notFoundHandler,
				"/api/search": emptyArrayHandler,
			},
			netease: map[string]http.HandlerFunc{
				"/api/search/get": func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(`{"result":{"songs":[{"id":3,"name":"Voyager"}]}}`))
				},
				"/api/song/lyric": func(w http.ResponseWriter, _ *http.Request) {
					_, _ = w.Write([]byte(`{"lrc":{"lyric":"   "}}`))
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lrclib := newRecordingServer(t, tt.lrclib)
			netease := newRecordingServer(t, tt.netease)

			c := testClient("", lrclib, netease)
			sheet, err := c.Fetch(context.Background(), "Daft Punk", "Voyager", "", 0)
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
			if sheet != nil {
				t.Errorf("sheet = %+v, want nil", sheet)
			}
		})
	}
}

func TestFetchEmptyTitle(t *testing.T) {
	c := testClient("", nil, nil)
	for _, title := range []string{"", "   ", "(Official Video)"} {
		sheet, err := c.Fetch(context.Background(), "Daft Punk", title, "", 0)
		if !errors.Is(err, ErrNotFound) {
			t.Errorf("Fetch(title=%q) err = %v, want ErrNotFound", title, err)
		}
		if sheet != nil {
			t.Errorf("Fetch(title=%q) sheet = %+v, want nil", title, sheet)
		}
	}
}

func TestFetchWithoutArtistSkipsExactLookup(t *testing.T) {
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/search": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, []lrcLibTrack{{TrackName: "Voyager", SyncedLyrics: testSyncedLRC}})
		},
	})

	c := testClient("", lrclib, nil)
	sheet, err := c.Fetch(context.Background(), "", "Voyager", "", 0)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !sheet.Synced {
		t.Error("expected the search result to be used")
	}
	if got := lrclib.requestAt(t, 0).Path; got != "/api/search" {
		t.Errorf("first request = %q, want /api/search (the exact lookup needs an artist)", got)
	}
}

func TestFetchProviderFailure(t *testing.T) {
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/get": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		},
		"/api/search": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusBadGateway)
		},
	})

	c := testClient("", lrclib, nil) // NetEase points at an unreachable port.
	sheet, err := c.Fetch(context.Background(), "Daft Punk", "Voyager", "", 0)
	if err == nil {
		t.Fatal("expected an error when every provider fails")
	}
	if errors.Is(err, ErrNotFound) {
		t.Error("transport failures must not be reported as ErrNotFound")
	}
	if sheet != nil {
		t.Errorf("sheet = %+v, want nil", sheet)
	}
	if !strings.Contains(err.Error(), "lrclib.get") || !strings.Contains(err.Error(), "netease") {
		t.Errorf("error should name the failing providers: %v", err)
	}

	// A failure is not cached negatively: the next call retries.
	before := lrclib.hits.Load()
	_, _ = c.Fetch(context.Background(), "Daft Punk", "Voyager", "", 0)
	if lrclib.hits.Load() == before {
		t.Error("a failed lookup must not be negatively cached")
	}
}

func TestFetchMemoryCacheHit(t *testing.T) {
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/get": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, lrcLibTrack{TrackName: "Voyager", ArtistName: "Daft Punk", SyncedLyrics: testSyncedLRC})
		},
	})

	c := testClient("", lrclib, nil)
	first, err := c.Fetch(context.Background(), "Daft Punk", "Voyager", "", 0)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := lrclib.hits.Load(); got != 1 {
		t.Fatalf("hits = %d, want 1", got)
	}

	// Noise that normalises away must still hit the same cache entry.
	second, err := c.Fetch(context.Background(), "DAFT PUNK", "Voyager (Official Video)", "", 0)
	if err != nil {
		t.Fatalf("Fetch (cached): %v", err)
	}
	if got := lrclib.hits.Load(); got != 1 {
		t.Errorf("hits = %d, want the second lookup served from memory", got)
	}
	if second.Source != SourceLRCLib {
		t.Errorf("Source = %q, want the original provider name", second.Source)
	}

	// Each call returns an independent copy.
	if first == second {
		t.Error("Fetch returned the same pointer twice")
	}
	second.Lines[0].Text = "mutated"
	third, err := c.Fetch(context.Background(), "Daft Punk", "Voyager", "", 0)
	if err != nil {
		t.Fatalf("Fetch (cached): %v", err)
	}
	if third.Lines[0].Text != "first" {
		t.Error("mutating a returned sheet corrupted the cache")
	}
}

func TestFetchNegativeCaching(t *testing.T) {
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/get":    notFoundHandler,
		"/api/search": emptyArrayHandler,
	})
	netease := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/search/get": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":{"songs":[]}}`))
		},
	})

	c := testClient("", lrclib, netease)
	for i := 0; i < 3; i++ {
		if _, err := c.Fetch(context.Background(), "Nobody", "Nothing", "", 0); !errors.Is(err, ErrNotFound) {
			t.Fatalf("call %d: err = %v, want ErrNotFound", i, err)
		}
	}

	if got := lrclib.hits.Load(); got != 2 {
		t.Errorf("LRCLIB hits = %d, want 2 (one get + one search, then cached)", got)
	}
	if got := netease.hits.Load(); got != 1 {
		t.Errorf("NetEase hits = %d, want 1 (then cached)", got)
	}

	// Negative entries are never written to disk.
	disk := NewClient(t.TempDir())
	disk.mem.putNegative("k")
	if entries, err := readDirNames(disk.CacheDir()); err != nil || len(entries) != 0 {
		t.Errorf("cache dir entries = %v (err %v), want none", entries, err)
	}
}

func TestFetchDiskCacheSurvivesNewClient(t *testing.T) {
	dir := t.TempDir()
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/get": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, lrcLibTrack{
				TrackName:    "Voyager",
				ArtistName:   "Daft Punk",
				Duration:     227,
				SyncedLyrics: testSyncedLRC,
			})
		},
	})

	first := testClient(dir, lrclib, nil)
	if _, err := first.Fetch(context.Background(), "Daft Punk", "Voyager", "", 227*time.Second); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got := lrclib.hits.Load(); got != 1 {
		t.Fatalf("hits = %d, want 1", got)
	}

	// A brand new client with a cold memory cache reads the sheet from disk.
	second := testClient(dir, lrclib, nil)
	sheet, err := second.Fetch(context.Background(), "Daft Punk", "Voyager", "", 227*time.Second)
	if err != nil {
		t.Fatalf("Fetch (disk): %v", err)
	}
	if got := lrclib.hits.Load(); got != 1 {
		t.Errorf("hits = %d, want the second client served from disk", got)
	}
	if sheet.Source != SourceLRCLib {
		t.Errorf("Source = %q, want the original provider name", sheet.Source)
	}
	if !sheet.Synced || len(sheet.Lines) != 3 || sheet.Lines[2].At != 30*time.Second {
		t.Errorf("disk sheet lost data: %+v", sheet)
	}
	if _, ok := second.mem.get(cacheKey("Daft Punk", "Voyager", "", 227*time.Second)); !ok {
		t.Error("a disk hit should populate the memory cache")
	}
}

func TestFetchContextCancelled(t *testing.T) {
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/get": func(w http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
		},
	})

	c := testClient("", lrclib, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	sheet, err := c.Fetch(ctx, "Daft Punk", "Voyager", "", 0)
	if err == nil {
		t.Fatal("expected an error for a cancelled context")
	}
	if sheet != nil {
		t.Errorf("sheet = %+v, want nil", sheet)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want a context deadline error", err)
	}
}

func TestFetchAlreadyCancelledContext(t *testing.T) {
	c := testClient("", nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := c.Fetch(ctx, "Daft Punk", "Voyager", "", 0); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestFetchNilReceiverAndContext(t *testing.T) {
	if _, err := (*Client)(nil).Fetch(context.Background(), "a", "b", "", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("nil client err = %v, want ErrNotFound", err)
	}

	// A zero-value Client must lazily build its cache rather than panic.
	zero := &Client{LRCLibBaseURL: "http://127.0.0.1:1", NetEaseBaseURL: "http://127.0.0.1:1"}
	//nolint:staticcheck // deliberately passing a nil context to prove it is tolerated.
	if _, err := zero.Fetch(nil, "Daft Punk", "Voyager", "", 0); err == nil {
		t.Error("expected an error from unreachable providers")
	}
}

func TestFetchConcurrent(t *testing.T) {
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/get": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, lrcLibTrack{TrackName: "Voyager", ArtistName: "Daft Punk", SyncedLyrics: testSyncedLRC})
		},
	})

	c := testClient(t.TempDir(), lrclib, nil)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			title := fmt.Sprintf("Voyager %d", n%4)
			if _, err := c.Fetch(context.Background(), "Daft Punk", title, "", 0); err != nil {
				t.Errorf("Fetch: %v", err)
			}
		}(i)
	}
	wg.Wait()
}

func TestPickNetEaseSong(t *testing.T) {
	song := func(id int64, name, artist string, durMS int64) netEaseSong {
		s := netEaseSong{ID: id, Name: name, Duration: durMS}
		if artist != "" {
			s.Artists = []struct {
				Name string `json:"name"`
			}{{Name: artist}}
		}
		return s
	}

	tests := []struct {
		name     string
		songs    []netEaseSong
		artist   string
		title    string
		duration time.Duration
		wantID   int64
	}{
		{name: "no songs", songs: nil, wantID: 0},
		{name: "zero id skipped", songs: []netEaseSong{song(0, "Voyager", "Daft Punk", 0)}, wantID: 0},
		{
			name:   "exact title and artist wins",
			songs:  []netEaseSong{song(1, "Other", "Someone", 0), song(2, "Voyager", "Daft Punk", 0)},
			artist: "Daft Punk", title: "Voyager", wantID: 2,
		},
		{
			name:   "closest duration breaks a tie",
			songs:  []netEaseSong{song(1, "Voyager", "Daft Punk", 400_000), song(2, "Voyager", "Daft Punk", 228_000)},
			artist: "Daft Punk", title: "Voyager", duration: 227 * time.Second, wantID: 2,
		},
		{
			name:   "first hit when nothing matches",
			songs:  []netEaseSong{song(5, "Totally Other", "Nobody", 0), song(6, "Also Other", "Nobody", 0)},
			artist: "Daft Punk", title: "Voyager", wantID: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pickNetEaseSong(tt.songs, tt.artist, tt.title, tt.duration)
			if tt.wantID == 0 {
				if got != nil {
					t.Fatalf("picked %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("picked nil, want id %d", tt.wantID)
			}
			if got.ID != tt.wantID {
				t.Errorf("picked id %d, want %d", got.ID, tt.wantID)
			}
		})
	}
}

func TestNetEaseSongArtistName(t *testing.T) {
	s := netEaseSong{Artists: []struct {
		Name string `json:"name"`
	}{{Name: "Daft Punk"}, {Name: " "}, {Name: "Pharrell"}}}
	if got := s.artistName(); got != "Daft Punk & Pharrell" {
		t.Errorf("artistName = %q", got)
	}
	if got := (netEaseSong{}).artistName(); got != "" {
		t.Errorf("artistName = %q, want empty", got)
	}
}

func TestApplyRequestMetadata(t *testing.T) {
	sheet := &Sheet{}
	applyRequestMetadata(sheet, " Daft Punk ", " Voyager ", " Discovery ", 227*time.Second)
	if sheet.Artist != "Daft Punk" || sheet.Title != "Voyager" || sheet.Album != "Discovery" {
		t.Errorf("metadata not filled in: %+v", sheet)
	}
	if sheet.Duration != 227*time.Second {
		t.Errorf("Duration = %v, want the requested duration", sheet.Duration)
	}

	provider := &Sheet{Artist: "Daft Punk", Title: "Voyager", Album: "Discovery", Duration: 200 * time.Second}
	applyRequestMetadata(provider, "other", "other", "other", time.Hour)
	if provider.Artist != "Daft Punk" || provider.Duration != 200*time.Second {
		t.Errorf("provider metadata was overwritten: %+v", provider)
	}
}

func TestLRCLibTrackToSheet(t *testing.T) {
	tests := []struct {
		name             string
		track            lrcLibTrack
		wantNil          bool
		wantSynced       bool
		wantInstrumental bool
		wantLines        int
	}{
		{name: "no lyrics", track: lrcLibTrack{TrackName: "x"}, wantNil: true},
		{
			name:             "instrumental",
			track:            lrcLibTrack{TrackName: "x", Instrumental: true},
			wantInstrumental: true, wantLines: 1,
		},
		{
			name:       "synced preferred over plain",
			track:      lrcLibTrack{TrackName: "x", SyncedLyrics: testSyncedLRC, PlainLyrics: "a"},
			wantSynced: true, wantLines: 3,
		},
		{
			name:      "plain fallback",
			track:     lrcLibTrack{TrackName: "x", PlainLyrics: "a\nb"},
			wantLines: 2,
		},
		{
			name:      "unparseable synced lyrics fall back to plain",
			track:     lrcLibTrack{TrackName: "x", SyncedLyrics: "no timestamps", PlainLyrics: "a\nb"},
			wantLines: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.track.toSheet()
			if tt.wantNil {
				if got != nil {
					t.Fatalf("toSheet = %+v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("toSheet = nil")
			}
			if got.Source != SourceLRCLib {
				t.Errorf("Source = %q", got.Source)
			}
			if got.Synced != tt.wantSynced {
				t.Errorf("Synced = %v, want %v", got.Synced, tt.wantSynced)
			}
			if got.Instrumental != tt.wantInstrumental {
				t.Errorf("Instrumental = %v, want %v", got.Instrumental, tt.wantInstrumental)
			}
			if len(got.Lines) != tt.wantLines {
				t.Errorf("len(Lines) = %d, want %d", len(got.Lines), tt.wantLines)
			}
		})
	}
}

func TestGetJSONStatusHandling(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		wantErr error
	}{
		{name: "404 is no match", handler: notFoundHandler, wantErr: errNoMatch},
		{
			name:    "204 is no match",
			handler: func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) },
			wantErr: errNoMatch,
		},
		{
			name:    "bad json is a shape error",
			handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("<html>")) },
			wantErr: errBadShape,
		},
		{
			name:    "ok",
			handler: func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"trackName":"x"}`)) },
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()

			c := NewClient("")
			var out lrcLibTrack
			err := c.getJSON(context.Background(), srv.URL, nil, &out)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestGetJSONServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient("")
	var out lrcLibTrack
	err := c.getJSON(context.Background(), srv.URL, nil, &out)
	if err == nil {
		t.Fatal("expected an error for HTTP 500")
	}
	if errors.Is(err, errNoMatch) || errors.Is(err, errBadShape) {
		t.Errorf("err = %v, want a transport-level error", err)
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v, want the status code in the message", err)
	}
}

func TestGetJSONBadURL(t *testing.T) {
	c := NewClient("")
	var out lrcLibTrack
	if err := c.getJSON(context.Background(), "http://[::1]:namedport/x", nil, &out); err == nil {
		t.Error("expected an error for an invalid URL")
	}
}

// readDirNames lists a directory, treating a missing directory as empty.
func readDirNames(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

func TestFetchRecoversFromAFailingProvider(t *testing.T) {
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/get": notFoundHandler,
		"/api/search": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusInternalServerError)
		},
	})
	netease := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/search/get": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":{"songs":[{"id":4,"name":"Voyager"}]}}`))
		},
		"/api/song/lyric": func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(t, w, map[string]any{"lrc": map[string]any{"lyric": testSyncedLRC}})
		},
	})

	c := testClient("", lrclib, netease)
	sheet, err := c.Fetch(context.Background(), "Daft Punk", "Voyager", "", 0)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if sheet.Source != SourceNetEase {
		t.Errorf("Source = %q, want %q after the LRCLIB search failed", sheet.Source, SourceNetEase)
	}
}

func TestFetchNetEaseLyricRequestFails(t *testing.T) {
	lrclib := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/get":    notFoundHandler,
		"/api/search": emptyArrayHandler,
	})
	netease := newRecordingServer(t, map[string]http.HandlerFunc{
		"/api/search/get": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"result":{"songs":[{"id":4,"name":"Voyager"}]}}`))
		},
		"/api/song/lyric": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "boom", http.StatusServiceUnavailable)
		},
	})

	c := testClient("", lrclib, netease)
	sheet, err := c.Fetch(context.Background(), "Daft Punk", "Voyager", "", 0)
	if err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want a provider failure", err)
	}
	if sheet != nil {
		t.Errorf("sheet = %+v, want nil", sheet)
	}
	if !strings.Contains(err.Error(), "netease") {
		t.Errorf("err = %v, want the NetEase failure named", err)
	}
}
