package art

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubProviders is an httptest-backed stand-in for all four cover providers.
// Every base URL on the Client is pointed at a distinct path prefix on the
// same server so the test can tell the providers apart.
type stubProviders struct {
	t   *testing.T
	srv *httptest.Server

	mu   sync.Mutex
	hits map[string]int

	// itunesArtwork is returned verbatim as artworkUrl100; empty means the
	// provider reports no results.
	itunesArtwork string
	deezerCover   string
	mbid          string
	lastFMImage   string

	// imageBody/imageType control what the image endpoint serves.
	imageBody   []byte
	imageType   string
	imageStatus int

	// lastArtworkPath records the path the client actually requested for the
	// image, so the 600x600 rewrite can be asserted.
	lastArtworkPath string
	lastQueries     map[string]url.Values
}

func newStubProviders(t *testing.T) *stubProviders {
	t.Helper()
	s := &stubProviders{
		t:           t,
		hits:        map[string]int{},
		lastQueries: map[string]url.Values{},
		imageBody:   testPNG(t, 32, 32),
		imageType:   "image/png",
		imageStatus: http.StatusOK,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/itunes/search", s.handleITunes)
	mux.HandleFunc("/deezer/search", s.handleDeezer)
	mux.HandleFunc("/mb/ws/2/recording", s.handleMusicBrainz)
	mux.HandleFunc("/caa/release/", s.handleImage)
	mux.HandleFunc("/lastfm/2.0/", s.handleLastFM)
	mux.HandleFunc("/img/", s.handleImage)
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func (s *stubProviders) record(name string, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.hits[name]++
	s.lastQueries[name] = r.URL.Query()
	if ua := r.Header.Get("User-Agent"); ua != userAgent {
		s.t.Errorf("%s: User-Agent = %q, want %q", name, ua, userAgent)
	}
}

func (s *stubProviders) count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[name]
}

func (s *stubProviders) query(name string) url.Values {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastQueries[name]
}

func (s *stubProviders) handleITunes(w http.ResponseWriter, r *http.Request) {
	s.record("itunes", r)
	w.Header().Set("Content-Type", "application/json")
	if s.itunesArtwork == "" {
		fmt.Fprint(w, `{"resultCount":0,"results":[]}`)
		return
	}
	fmt.Fprintf(w, `{"resultCount":1,"results":[{"artistName":"Stub Artist","trackName":"Stub Track","collectionName":"Stub Album","artworkUrl100":%q}]}`, s.itunesArtwork)
}

func (s *stubProviders) handleDeezer(w http.ResponseWriter, r *http.Request) {
	s.record("deezer", r)
	w.Header().Set("Content-Type", "application/json")
	if s.deezerCover == "" {
		fmt.Fprint(w, `{"data":[]}`)
		return
	}
	fmt.Fprintf(w, `{"data":[{"title":"Deezer Track","artist":{"name":"Deezer Artist"},"album":{"title":"Deezer Album","cover_xl":%q,"cover_big":"ignored"}}]}`, s.deezerCover)
}

func (s *stubProviders) handleMusicBrainz(w http.ResponseWriter, r *http.Request) {
	s.record("musicbrainz", r)
	w.Header().Set("Content-Type", "application/json")
	if s.mbid == "" {
		fmt.Fprint(w, `{"recordings":[]}`)
		return
	}
	fmt.Fprintf(w, `{"recordings":[{"title":"MB Track","artist-credit":[{"name":"MB Artist"}],"releases":[{"id":%q,"title":"MB Album"}]}]}`, s.mbid)
}

func (s *stubProviders) handleLastFM(w http.ResponseWriter, r *http.Request) {
	s.record("lastfm", r)
	w.Header().Set("Content-Type", "application/json")
	if s.lastFMImage == "" {
		fmt.Fprint(w, `{"track":{}}`)
		return
	}
	fmt.Fprintf(w, `{"track":{"name":"LFM Track","album":{"artist":"LFM Artist","title":"LFM Album","image":[{"#text":"small.png","size":"small"},{"#text":%q,"size":"mega"},{"#text":"med.png","size":"medium"}]}}}`, s.lastFMImage)
}

func (s *stubProviders) handleImage(w http.ResponseWriter, r *http.Request) {
	s.record("image", r)
	s.mu.Lock()
	s.lastArtworkPath = r.URL.Path
	s.mu.Unlock()
	if s.imageType != "" {
		w.Header().Set("Content-Type", s.imageType)
	}
	w.WriteHeader(s.imageStatus)
	_, _ = w.Write(s.imageBody)
}

// client wires a Client to the stub server, with disk caching in dir.
func (s *stubProviders) client(dir, lastFMKey string) *Client {
	c := NewClient(dir, lastFMKey)
	c.ITunesBaseURL = s.srv.URL + "/itunes"
	c.DeezerBaseURL = s.srv.URL + "/deezer"
	c.MusicBrainzBaseURL = s.srv.URL + "/mb"
	c.CoverArtBaseURL = s.srv.URL + "/caa"
	c.LastFMBaseURL = s.srv.URL + "/lastfm"
	c.HTTPClient = s.srv.Client()
	return c
}

func TestFetchProviderChain(t *testing.T) {
	tests := []struct {
		name       string
		configure  func(s *stubProviders)
		lastFMKey  string
		wantSource string
		wantAlbum  string
		wantErr    error
		wantHits   map[string]int
	}{
		{
			name: "itunes wins first",
			configure: func(s *stubProviders) {
				s.itunesArtwork = s.srv.URL + "/img/a/100x100bb.jpg"
			},
			wantSource: "iTunes",
			wantAlbum:  "Stub Album",
			wantHits:   map[string]int{"itunes": 1, "deezer": 0, "musicbrainz": 0, "lastfm": 0},
		},
		{
			name: "falls through to deezer",
			configure: func(s *stubProviders) {
				s.deezerCover = s.srv.URL + "/img/deezer.png"
			},
			wantSource: "Deezer",
			wantAlbum:  "Deezer Album",
			wantHits:   map[string]int{"itunes": 1, "deezer": 1, "musicbrainz": 0},
		},
		{
			name: "falls through to cover art archive",
			configure: func(s *stubProviders) {
				s.mbid = "11111111-2222-3333-4444-555555555555"
			},
			wantSource: "Cover Art Archive",
			wantAlbum:  "MB Album",
			wantHits:   map[string]int{"itunes": 1, "deezer": 1, "musicbrainz": 1},
		},
		{
			name: "falls through to last.fm when a key is set",
			configure: func(s *stubProviders) {
				s.lastFMImage = s.srv.URL + "/img/lastfm.png"
			},
			lastFMKey:  "secret-key",
			wantSource: "Last.fm",
			wantAlbum:  "LFM Album",
			wantHits:   map[string]int{"itunes": 1, "deezer": 1, "musicbrainz": 1, "lastfm": 1},
		},
		{
			name: "last.fm skipped without a key",
			configure: func(s *stubProviders) {
				s.lastFMImage = s.srv.URL + "/img/lastfm.png"
			},
			wantErr:  ErrNotFound,
			wantHits: map[string]int{"itunes": 1, "deezer": 1, "musicbrainz": 1, "lastfm": 0},
		},
		{
			name:      "no provider has artwork",
			configure: func(s *stubProviders) {},
			wantErr:   ErrNotFound,
			wantHits:  map[string]int{"itunes": 1, "deezer": 1, "musicbrainz": 1, "lastfm": 0},
		},
		{
			name: "invalid mbid is rejected before the CAA request",
			configure: func(s *stubProviders) {
				s.mbid = "../../etc/passwd"
			},
			wantErr:  ErrNotFound,
			wantHits: map[string]int{"musicbrainz": 1, "image": 0},
		},
		{
			name: "non-image content type falls through",
			configure: func(s *stubProviders) {
				s.itunesArtwork = s.srv.URL + "/img/a/100x100bb.jpg"
				s.imageType = "text/html"
			},
			wantErr:  ErrNotFound,
			wantHits: map[string]int{"itunes": 1, "deezer": 1, "musicbrainz": 1},
		},
		{
			name: "undecodable payload falls through",
			configure: func(s *stubProviders) {
				s.itunesArtwork = s.srv.URL + "/img/a/100x100bb.jpg"
				s.imageBody = []byte("<html>nope</html>")
			},
			wantErr:  ErrNotFound,
			wantHits: map[string]int{"itunes": 1, "deezer": 1},
		},
		{
			name: "image error status falls through",
			configure: func(s *stubProviders) {
				s.deezerCover = s.srv.URL + "/img/deezer.png"
				s.imageStatus = http.StatusNotFound
			},
			wantErr:  ErrNotFound,
			wantHits: map[string]int{"deezer": 1, "musicbrainz": 1},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStubProviders(t)
			tc.configure(s)
			c := s.client(filepath.Join(t.TempDir(), "covers"), tc.lastFMKey)

			cover, err := c.Fetch(context.Background(), "Daft Punk", "One More Time", "Discovery")
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if cover != nil {
					t.Fatalf("expected a nil cover alongside %v", tc.wantErr)
				}
			} else {
				if err != nil {
					t.Fatalf("Fetch: %v", err)
				}
				if cover.Source != tc.wantSource {
					t.Fatalf("Source = %q, want %q", cover.Source, tc.wantSource)
				}
				if len(cover.Data) == 0 {
					t.Fatal("cover has no data")
				}
				// The caller-supplied album wins over the provider's.
				if cover.Album != "Discovery" {
					t.Fatalf("Album = %q, want the caller's %q", cover.Album, "Discovery")
				}
				if cover.URL == "" {
					t.Fatal("cover has no origin URL")
				}
			}

			for name, want := range tc.wantHits {
				if got := s.count(name); got != want {
					t.Errorf("%s hits = %d, want %d", name, got, want)
				}
			}
		})
	}
}

func TestFetchITunesUpgradesArtworkSize(t *testing.T) {
	s := newStubProviders(t)
	s.itunesArtwork = s.srv.URL + "/img/cover/100x100bb.jpg"
	c := s.client("", "")

	if _, err := c.Fetch(context.Background(), "Artist", "Title", ""); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	s.mu.Lock()
	got := s.lastArtworkPath
	s.mu.Unlock()
	if !strings.HasSuffix(got, "/600x600bb.jpg") {
		t.Fatalf("artwork path = %q, want the 600x600bb rewrite", got)
	}
}

func TestFetchProviderQueries(t *testing.T) {
	s := newStubProviders(t)
	s.mbid = "11111111-2222-3333-4444-555555555555"
	c := s.client("", "lfm-key")

	// Noise in the track fields must be stripped before it reaches a provider.
	if _, err := c.Fetch(context.Background(), "Daft Punk", "Aerodynamic (Official Video)", ""); err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	tests := []struct {
		provider string
		param    string
		want     string
	}{
		{"itunes", "term", "daft punk aerodynamic"},
		{"itunes", "media", "music"},
		{"itunes", "entity", "song"},
		{"itunes", "limit", "1"},
		{"deezer", "q", "daft punk aerodynamic"},
		{"deezer", "limit", "1"},
		{"musicbrainz", "query", `artist:"daft punk" AND recording:"aerodynamic"`},
		{"musicbrainz", "fmt", "json"},
		{"musicbrainz", "limit", "1"},
	}
	for _, tc := range tests {
		t.Run(tc.provider+"_"+tc.param, func(t *testing.T) {
			if got := s.query(tc.provider).Get(tc.param); got != tc.want {
				t.Fatalf("%s %s = %q, want %q", tc.provider, tc.param, got, tc.want)
			}
		})
	}
}

func TestFetchLastFMPicksLargestImage(t *testing.T) {
	s := newStubProviders(t)
	s.lastFMImage = s.srv.URL + "/img/mega.png"
	c := s.client("", "lfm-key")

	cover, err := c.Fetch(context.Background(), "Artist", "Title", "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !strings.HasSuffix(cover.URL, "/img/mega.png") {
		t.Fatalf("URL = %q, want the mega-sized image", cover.URL)
	}
	if got := s.query("lastfm").Get("api_key"); got != "lfm-key" {
		t.Fatalf("api_key = %q", got)
	}
	if got := s.query("lastfm").Get("method"); got != "track.getInfo" {
		t.Fatalf("method = %q", got)
	}
}

func TestFetchUsesMemoryCache(t *testing.T) {
	s := newStubProviders(t)
	s.itunesArtwork = s.srv.URL + "/img/a/100x100bb.jpg"
	c := s.client("", "")

	for i := 0; i < 3; i++ {
		if _, err := c.Fetch(context.Background(), "Artist", "Title (feat. X)", ""); err != nil {
			t.Fatalf("Fetch %d: %v", i, err)
		}
	}
	// The feat. suffix normalises away, so this is the same cache key.
	if _, err := c.Fetch(context.Background(), " artist ", "title", ""); err != nil {
		t.Fatalf("Fetch normalised: %v", err)
	}
	if got := s.count("itunes"); got != 1 {
		t.Fatalf("itunes was queried %d times, want 1", got)
	}
}

func TestFetchCachesNegativeLookups(t *testing.T) {
	s := newStubProviders(t)
	c := s.client("", "")

	for i := 0; i < 3; i++ {
		if _, err := c.Fetch(context.Background(), "Artist", "Title", ""); !errors.Is(err, ErrNotFound) {
			t.Fatalf("Fetch %d err = %v, want ErrNotFound", i, err)
		}
	}
	if got := s.count("itunes"); got != 1 {
		t.Fatalf("itunes was queried %d times, want 1 (negative cache)", got)
	}
}

func TestFetchUsesDiskCacheAcrossClients(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "covers")
	s := newStubProviders(t)
	s.itunesArtwork = s.srv.URL + "/img/a/100x100bb.jpg"

	first := s.client(dir, "")
	want, err := first.Fetch(context.Background(), "Artist", "Title", "Album")
	if err != nil {
		t.Fatalf("first Fetch: %v", err)
	}

	// A brand new client with an empty RAM cache must read from disk.
	second := s.client(dir, "")
	got, err := second.Fetch(context.Background(), "Artist", "Title", "Album")
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if got.Source != want.Source || got.URL != want.URL || len(got.Data) != len(want.Data) {
		t.Fatalf("disk cache returned %+v, want %+v", got, want)
	}
	if hits := s.count("itunes"); hits != 1 {
		t.Fatalf("itunes was queried %d times, want 1", hits)
	}
}

func TestFetchEmptyQuery(t *testing.T) {
	s := newStubProviders(t)
	s.itunesArtwork = s.srv.URL + "/img/a/100x100bb.jpg"
	c := s.client("", "")

	tests := []struct{ artist, title string }{
		{"", ""},
		{"   ", "  "},
		{"(Official Video)", "[Explicit]"},
	}
	for _, tc := range tests {
		t.Run(tc.artist+"/"+tc.title, func(t *testing.T) {
			if _, err := c.Fetch(context.Background(), tc.artist, tc.title, ""); !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
		})
	}
	if got := s.count("itunes"); got != 0 {
		t.Fatalf("providers were queried %d times for an empty track", got)
	}
}

func TestFetchRespectsContext(t *testing.T) {
	s := newStubProviders(t)
	s.itunesArtwork = s.srv.URL + "/img/a/100x100bb.jpg"
	c := s.client("", "")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.Fetch(ctx, "Artist", "Title", ""); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	// A cancelled lookup must not be remembered as a negative result.
	if _, err := c.Fetch(context.Background(), "Artist", "Title", ""); err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
}

func TestFetchNilReceiverAndContext(t *testing.T) {
	if _, err := (*Client)(nil).Fetch(context.Background(), "a", "b", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("nil client err = %v, want ErrNotFound", err)
	}

	s := newStubProviders(t)
	s.itunesArtwork = s.srv.URL + "/img/a/100x100bb.jpg"
	c := s.client("", "")
	//nolint:staticcheck // deliberately exercising the nil-context guard
	if _, err := c.Fetch(nil, "Artist", "Title", ""); err != nil {
		t.Fatalf("nil context Fetch: %v", err)
	}
}

func TestFetchIsConcurrencySafe(t *testing.T) {
	s := newStubProviders(t)
	s.itunesArtwork = s.srv.URL + "/img/a/100x100bb.jpg"
	c := s.client(filepath.Join(t.TempDir(), "covers"), "")

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, _ = c.Fetch(context.Background(), "Artist", fmt.Sprintf("Title %d", i%4), "")
		}(i)
	}
	wg.Wait()
}

func TestNewClientDefaults(t *testing.T) {
	c := NewClient("/tmp/does-not-need-to-exist", "key")
	tests := []struct{ got, want string }{
		{c.MusicBrainzBaseURL, DefaultMusicBrainzBaseURL},
		{c.CoverArtBaseURL, DefaultCoverArtBaseURL},
		{c.ITunesBaseURL, DefaultITunesBaseURL},
		{c.DeezerBaseURL, DefaultDeezerBaseURL},
		{c.LastFMBaseURL, DefaultLastFMBaseURL},
	}
	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("base URL = %q, want %q", tc.got, tc.want)
		}
	}
	if c.HTTPClient == nil || c.HTTPClient.Timeout <= 0 {
		t.Fatal("expected a default HTTP client with a timeout")
	}
	if c.lastFMKey != "key" {
		t.Fatalf("lastFMKey = %q", c.lastFMKey)
	}
	if (*Client)(nil).CacheDir() != "" {
		t.Fatal("nil client CacheDir should be empty")
	}
}

func TestBaseOr(t *testing.T) {
	tests := []struct {
		configured, def, want string
	}{
		{"", "https://d.example", "https://d.example"},
		{"   ", "https://d.example", "https://d.example"},
		{"https://x.example/", "https://d.example", "https://x.example"},
		{"https://x.example///", "https://d.example", "https://x.example"},
		{" https://x.example/sub ", "https://d.example", "https://x.example/sub"},
		{"", "https://d.example/", "https://d.example"},
	}
	for _, tc := range tests {
		if got := baseOr(tc.configured, tc.def); got != tc.want {
			t.Errorf("baseOr(%q, %q) = %q, want %q", tc.configured, tc.def, got, tc.want)
		}
	}
}

func TestIsMBID(t *testing.T) {
	tests := map[string]bool{
		"11111111-2222-3333-4444-555555555555":  true,
		"AABBCCDD-2222-3333-4444-555555555555":  true,
		"":                                      false,
		"not-a-uuid":                            false,
		"11111111222233334444555555555555":      false,
		"11111111-2222-3333-4444-55555555555g":  false,
		"../../../etc/passwd":                   false,
		"11111111-2222-3333-4444-5555555555555": false,
	}
	for in, want := range tests {
		if got := isMBID(in); got != want {
			t.Errorf("isMBID(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestContentTypeHelpers(t *testing.T) {
	tests := []struct {
		raw     string
		norm    string
		isImage bool
	}{
		{"image/png", "image/png", true},
		{"image/jpeg; charset=binary", "image/jpeg", true},
		{"  IMAGE/PNG  ", "image/png", true},
		{"application/octet-stream", "application/octet-stream", true},
		{"binary/octet-stream", "binary/octet-stream", true},
		{"text/html; charset=utf-8", "text/html", false},
		{"application/json", "application/json", false},
		{"", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			norm := contentType(tc.raw)
			if norm != tc.norm {
				t.Fatalf("contentType(%q) = %q, want %q", tc.raw, norm, tc.norm)
			}
			if got := isImageContentType(norm); got != tc.isImage {
				t.Fatalf("isImageContentType(%q) = %v, want %v", norm, got, tc.isImage)
			}
		})
	}
}

func TestFirstNonEmpty(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"", "  ", "x"}, "x"},
		{[]string{"a", "b"}, "a"},
		{[]string{"", ""}, ""},
	}
	for _, tc := range tests {
		if got := firstNonEmpty(tc.in...); got != tc.want {
			t.Errorf("firstNonEmpty(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLastFMSizeRank(t *testing.T) {
	order := []string{"unknown", "small", "medium", "large", "extralarge", "mega"}
	for i := 1; i < len(order); i++ {
		if lastFMSizeRank(order[i]) <= lastFMSizeRank(order[i-1]) {
			t.Fatalf("%q should rank above %q", order[i], order[i-1])
		}
	}
	if lastFMSizeRank(" MEGA ") != lastFMSizeRank("mega") {
		t.Fatal("size rank should be case and space insensitive")
	}
}

func TestGetJSONErrors(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"server error", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}},
		{"rate limited", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusTooManyRequests)
		}},
		{"malformed json", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"results":`)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			c := NewClient("", "")
			c.HTTPClient = srv.Client()
			var out struct{}
			if err := c.getJSON(context.Background(), srv.URL, &out); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestDownloadCapsBody(t *testing.T) {
	// Serve far more than the cap; the client must stop reading and the
	// truncated payload must fail to decode rather than blow up.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		chunk := make([]byte, 1<<20)
		for i := 0; i < 12; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer srv.Close()

	c := NewClient("", "")
	c.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	data, err := c.download(context.Background(), srv.URL)
	if err == nil {
		t.Fatalf("expected a decode failure, got %d bytes", len(data))
	}
	if len(data) != 0 {
		t.Fatalf("expected no data on error, got %d bytes", len(data))
	}
}

func TestDownloadAcceptsOctetStream(t *testing.T) {
	body := testJPEG(t, 16, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	c := NewClient("", "")
	c.HTTPClient = srv.Client()
	data, err := c.download(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("download: %v", err)
	}
	if len(data) != len(body) {
		t.Fatalf("got %d bytes, want %d", len(data), len(body))
	}
}

// TestFetchThenRender exercises the whole pipeline: fetch a cover from the
// stub providers and render it through every protocol.
func TestFetchThenRender(t *testing.T) {
	s := newStubProviders(t)
	s.itunesArtwork = s.srv.URL + "/img/a/100x100bb.jpg"
	c := s.client(filepath.Join(t.TempDir(), "covers"), "")

	cover, err := c.Fetch(context.Background(), "Artist", "Title", "Album")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}

	for _, proto := range []Protocol{
		ProtocolHalfBlock, ProtocolBraille, ProtocolKitty, ProtocolITerm2, ProtocolSixel,
	} {
		t.Run(string(proto), func(t *testing.T) {
			lines, err := NewRenderer(proto).Render(cover.Data, 18, 9)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			if len(lines) != 9 {
				t.Fatalf("got %d lines, want 9", len(lines))
			}
		})
	}
}
