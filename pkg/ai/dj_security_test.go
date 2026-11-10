package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/halpworld/halpradio/pkg/util"
)

type nopEngine struct{}

func (nopEngine) Name() string                                { return "silent" }
func (nopEngine) Label() string                               { return "stub" }
func (nopEngine) Speak(context.Context, string, string) error { return nil }

func assertMultilineSafe(t *testing.T, field, v string) {
	t.Helper()
	for _, line := range strings.Split(v, "\n") {
		if !util.IsDisplaySafe(line) {
			t.Errorf("%s not display safe: %q", field, v)
			return
		}
	}
}

func assertLinerNotesSafe(t *testing.T, n *LinerNotes) {
	t.Helper()
	if n == nil {
		t.Fatal("nil notes")
	}
	for name, v := range map[string]string{
		"artist": n.Artist, "title": n.Title, "album": n.Album, "year": n.ReleaseYear, "country": n.Country,
		"origin": n.OriginCity, "genre": n.Genre, "label": n.Label, "mbid": n.MBID, "album_notes": n.AlbumNotes, "source": n.Source,
	} {
		if !util.IsDisplaySafe(v) {
			t.Errorf("%s not display safe: %q", name, v)
		}
	}
	assertMultilineSafe(t, "biography", n.Biography)
	for _, d := range n.Discography {
		if !util.IsDisplaySafe(d.Title) || !util.IsDisplaySafe(d.Year) || !util.IsDisplaySafe(d.Type) {
			t.Errorf("discography item not display safe: %+v", d)
		}
	}
	for _, tr := range n.TrackTrivia {
		if !util.IsDisplaySafe(tr) {
			t.Errorf("trivia not display safe: %q", tr)
		}
	}
}

// AI-2 / AI-3: hostile LLM output is cleaned and cannot start with a dash.
func TestOllamaScriptSanitized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"response": "--help \x1b]52;c;ZWNobyBwd25lZA==\x07\x1b[2J\x1b]0;owned\x07 tune in ‮" + strings.Repeat("loooong ", 200),
		})
	}))
	defer srv.Close()
	dj := NewDJ(DJConfig{OllamaURL: srv.URL, OllamaModel: "x", Engine: nopEngine{}})
	script, _, err := dj.GenerateIntro(context.Background(), TrackContext{StationName: "S", Artist: "A", Title: "T"})
	if err != nil {
		t.Fatal(err)
	}
	if !util.IsDisplaySafe(script) || strings.HasPrefix(script, "-") || !strings.Contains(script, "tune in") {
		t.Fatalf("unsafe script: %q", script)
	}
	if len(strings.Fields(script)) > maxDJScriptWords || len([]rune(script)) > maxDJScriptRunes {
		t.Fatalf("script not capped: %d words, %d runes", len(strings.Fields(script)), len([]rune(script)))
	}
}

// AI-4: a huge Ollama body is not read past the cap; the DJ falls back offline.
func TestOllamaResponseSizeCapped(t *testing.T) {
	var sent int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, _ := fmt.Fprintf(w, `{"response":"%s"}`, strings.Repeat("A", 4<<20))
		atomic.StoreInt64(&sent, int64(n))
	}))
	defer srv.Close()
	dj := NewDJ(DJConfig{OllamaURL: srv.URL, OllamaModel: "x", Engine: nopEngine{}})
	script, _, _ := dj.GenerateIntro(context.Background(), TrackContext{StationName: "S", Artist: "Art", Title: "Song"})
	if strings.Contains(script, "AAAA") || !strings.Contains(script, "Art") {
		t.Fatalf("oversized response was used: %.80q", script)
	}
}

func TestValidateOllamaURL(t *testing.T) {
	ok := []string{"http://127.0.0.1:11434", "http://localhost:11434/", "http://[::1]:11434", "http://127.0.0.2", "https://ollama.example.com", "https://10.0.0.5:11434"}
	bad := []string{"http://192.168.1.5:11434", "http://ollama.example.com", "ftp://127.0.0.1", "http://user:pw@127.0.0.1:11434", "127.0.0.1:11434", "http://127.0.0.1:11434/?x=1", "file:///etc/passwd", ""}
	for _, u := range ok {
		if _, err := ValidateOllamaURL(u); err != nil {
			t.Errorf("ValidateOllamaURL(%q) refused: %v", u, err)
		}
	}
	for _, u := range bad {
		if _, err := ValidateOllamaURL(u); err == nil {
			t.Errorf("ValidateOllamaURL(%q) accepted", u)
		}
	}
}

type countingTransport struct{ n int32 }

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	atomic.AddInt32(&c.n, 1)
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"response":"remote says hi"}`)), Header: http.Header{}, Request: r}, nil
}

// AI-8: plain http to a non-loopback Ollama host is never contacted.
func TestNonLoopbackHTTPOllamaRefused(t *testing.T) {
	ct := &countingTransport{}
	dj := NewDJ(DJConfig{OllamaURL: "http://ollama.lan:11434", OllamaModel: "x", Engine: nopEngine{}, HTTPClient: &http.Client{Transport: ct}})
	if dj.OllamaURLError() == nil {
		t.Fatal("expected OllamaURLError for plain http remote host")
	}
	script, _, _ := dj.GenerateIntro(context.Background(), TrackContext{Artist: "A", Title: "T"})
	if atomic.LoadInt32(&ct.n) != 0 || strings.Contains(script, "remote says hi") {
		t.Fatal("refused Ollama URL was contacted")
	}
}

// AI-8: the DJ client never follows redirects.
func TestOllamaRedirectNotFollowed(t *testing.T) {
	var hit int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hit, 1)
		fmt.Fprint(w, `{"response":"redirected script"}`)
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/api/generate", http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	dj := NewDJ(DJConfig{OllamaURL: srv.URL, OllamaModel: "x", Engine: nopEngine{}, HTTPClient: &http.Client{Timeout: 2 * time.Second}})
	script, _, _ := dj.GenerateIntro(context.Background(), TrackContext{Artist: "A", Title: "T"})
	if atomic.LoadInt32(&hit) != 0 || strings.Contains(script, "redirected") {
		t.Fatalf("redirect was followed (hits=%d, script=%q)", hit, script)
	}
}

func TestOfflineScriptSanitizesTrackContext(t *testing.T) {
	dj := NewDJ(DJConfig{Engine: nopEngine{}})
	script, _, _ := dj.GenerateIntro(context.Background(), TrackContext{
		StationName: "St\x1b]0;x\x07", Artist: "A\x1b[2J" + strings.Repeat("x", 5000), Title: "T\u009b31m", Album: "Alb‮", ReleaseYear: "1999\x07",
	})
	if !util.IsDisplaySafe(script) || len([]rune(script)) > maxOfflineScriptRunes {
		t.Fatalf("offline script unsafe or uncapped: %.120q", script)
	}
}

const hostileMBRecording = `{"recordings":[{"id":"r","title":"T\u001b[2J","first-release-date":"1999\u0007","artist-credit":[{"name":"A","artist":{"id":"mbid-1","name":"A\u001b]0;owned\u0007","disambiguation":"dis\u001b[2Jx"}}],"releases":[{"title":"Alb\u001b]52;c;QUJD\u0007","date":"1999","country":"G\u009bB"}],"tags":[{"name":"tag\u001b[31m","count":1}]}]}`

const hostileMBArtist = `{"id":"mbid-1","name":"A","country":"U\u001bS","disambiguation":"d‮is","begin-area":{"name":"City\u0007"},"life-span":{"begin":"1990"},"tags":[{"name":"t\u001b[1m"}],"release-groups":[{"title":"RG\u001b]0;x\u0007","primary-type":"Al\u001bbum","first-release-date":"2001"}]}`

// AI-2 / TTY-4: MusicBrainz and Wikipedia strings are sanitized where parsed.
func TestLinerNotesSanitizedAtParse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/ws/2/recording"):
			fmt.Fprint(w, hostileMBRecording)
		case strings.Contains(r.URL.Path, "/ws/2/artist"):
			fmt.Fprint(w, hostileMBArtist)
		case strings.Contains(r.URL.Path, "/page/summary/"):
			fmt.Fprint(w, `{"type":"standard","extract":"Bio\u001b]52;c;QUJD\u0007 line one.\nline\u001b[2J two."}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := NewLinerNotesClient(WithLinerNotesCacheDir(t.TempDir()), WithMusicBrainzBaseURL(srv.URL), WithWikipediaBaseURL(srv.URL))
	n, err := c.Fetch(context.Background(), LinerNotesQuery{Artist: "A", Title: "T", StationName: "S\x1b[2J"})
	if err != nil {
		t.Fatal(err)
	}
	assertLinerNotesSafe(t, n)
	if !strings.Contains(n.Biography, "Bio") || !strings.Contains(n.Source, "MusicBrainz") {
		t.Fatalf("unexpected notes: %+v", n)
	}
	mb, ok := c.queryMusicBrainz(context.Background(), LinerNotesQuery{Artist: "A", Title: "T"})
	if !ok {
		t.Fatal("MusicBrainz query failed")
	}
	assertLinerNotesSafe(t, mb)
}

// AI-4: oversized MusicBrainz bodies are cut off and ignored.
func TestLinerNotesResponseSizeCapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/ws/2/recording") {
			fmt.Fprintf(w, `{"recordings":[{"id":"r","title":"%s"}]}`, strings.Repeat("A", 2<<20))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := NewLinerNotesClient(WithLinerNotesCacheDir(t.TempDir()), WithMusicBrainzBaseURL(srv.URL), WithWikipediaBaseURL(srv.URL))
	if _, ok := c.queryMusicBrainz(context.Background(), LinerNotesQuery{Artist: "A", Title: "T"}); ok {
		t.Fatal("oversized MusicBrainz response was accepted")
	}
}

// TTY-4: a poisoned disk cache entry is sanitized when read back.
func TestLinerNotesDiskCacheSanitizedOnRead(t *testing.T) {
	dir := t.TempDir()
	c := NewLinerNotesClient(WithLinerNotesCacheDir(dir), WithMusicBrainzBaseURL(""), WithWikipediaBaseURL(""))
	key := linerCacheKey("A", "T", "S")
	poisoned := LinerNotes{
		Artist: "A\x1b]0;owned\x07", Title: "T", Biography: "bio\x1b[2J‮" + strings.Repeat("b", 10000),
		Discography: []DiscographyItem{{Title: "D\x1b]52;c;QUJD\x07"}},
		TrackTrivia: []string{"tr\x1b[31m"}, Source: "s\x07", FetchedAt: time.Now().UTC(),
	}
	raw, _ := json.Marshal(poisoned)
	if err := os.WriteFile(c.diskPath(key), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := c.Fetch(context.Background(), LinerNotesQuery{Artist: "A", Title: "T", StationName: "S"})
	if err != nil {
		t.Fatal(err)
	}
	assertLinerNotesSafe(t, n)
	if !strings.HasPrefix(n.Biography, "bio") || len([]rune(n.Biography)) > linerBioRunes {
		t.Fatalf("cached biography not read back or not capped: %d runes", len([]rune(n.Biography)))
	}
}
