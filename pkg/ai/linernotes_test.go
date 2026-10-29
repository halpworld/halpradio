package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestLinerNotesMusicBrainzAndWikipediaWithCaching(t *testing.T) {
	var mbHits int32
	var wikiHits int32

	mbServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&mbHits, 1)
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/ws/2/recording") {
			_, _ = w.Write([]byte(`{
				"recordings": [
					{
						"id": "rec-123",
						"title": "A Walk",
						"first-release-date": "2011-11-01",
						"artist-credit": [
							{
								"name": "Tycho",
								"artist": {
									"id": "artist-tycho-id",
									"name": "Tycho",
									"disambiguation": "ambient music project of Scott Hansen"
								}
							}
						],
						"releases": [
							{
								"title": "Dive",
								"date": "2011-11-01",
								"country": "US"
							}
						],
						"tags": [
							{"name": "ambient", "count": 10},
							{"name": "downtempo", "count": 8},
							{"name": "chillwave", "count": 5}
						]
					}
				]
			}`))
			return
		}
		if strings.HasPrefix(r.URL.Path, "/ws/2/artist/") {
			_, _ = w.Write([]byte(`{
				"id": "artist-tycho-id",
				"name": "Tycho",
				"country": "US",
				"begin-area": {"name": "San Francisco"},
				"life-span": {"begin": "2002"},
				"release-groups": [
					{"title": "Past Is Prologue", "primary-type": "Album", "first-release-date": "2006-10-20"},
					{"title": "Dive", "primary-type": "Album", "first-release-date": "2011-11-01"},
					{"title": "Awake", "primary-type": "Album", "first-release-date": "2014-03-18"}
				],
				"tags": [
					{"name": "ambient", "count": 12},
					{"name": "electronic", "count": 9}
				]
			}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer mbServer.Close()

	wikiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&wikiHits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"type": "standard",
			"title": "Tycho (musician)",
			"extract": "Tycho is an American ambient music project led by Scott Hansen as primary composer, songwriter and producer. Known also as ISO50 for his photographic and design works, his music blends downtempo vintage-style synthesizers with organic instrumentation."
		}`))
	}))
	defer wikiServer.Close()

	cacheDir := t.TempDir()
	client := NewLinerNotesClient(
		WithLinerNotesCacheDir(cacheDir),
		WithMusicBrainzBaseURL(mbServer.URL),
		WithWikipediaBaseURL(wikiServer.URL),
	)

	q := LinerNotesQuery{
		RawTrack:    "Tycho - A Walk",
		StationName: "SomaFM Groove Salad",
		Genre:       "Ambient / Chill",
		Country:     "USA",
	}

	notes, err := client.Fetch(context.Background(), q)
	if err != nil {
		t.Fatalf("Fetch failed: %v", err)
	}
	if notes.Artist != "Tycho" || notes.Title != "A Walk" {
		t.Errorf("unexpected artist/title: %s - %s", notes.Artist, notes.Title)
	}
	if notes.Album != "Dive" || notes.ReleaseYear != "2011" {
		t.Errorf("expected Album=Dive, ReleaseYear=2011, got %q (%q)", notes.Album, notes.ReleaseYear)
	}
	if notes.OriginCity != "San Francisco" {
		t.Errorf("expected OriginCity 'San Francisco', got %q", notes.OriginCity)
	}
	if notes.Source != "MusicBrainz + Wikipedia" {
		t.Errorf("expected Source 'MusicBrainz + Wikipedia', got %q", notes.Source)
	}
	if !strings.Contains(notes.Biography, "Scott Hansen") {
		t.Errorf("expected Wikipedia biography mentioning Scott Hansen, got %q", notes.Biography)
	}
	if len(notes.Discography) != 3 {
		t.Errorf("expected 3 discography items, got %d", len(notes.Discography))
	}
	if len(notes.TrackTrivia) < 2 {
		t.Errorf("expected rich track trivia bullets, got %v", notes.TrackTrivia)
	}

	// Verify ToTrackContext
	tc := notes.ToTrackContext("SomaFM Groove Salad", "Ambient", "USA")
	if tc.Album != "Dive" || tc.ReleaseYear != "2011" || tc.OriginCity != "San Francisco" {
		t.Errorf("ToTrackContext mismatch: %+v", tc)
	}

	// Second call should hit in-memory LRU without hitting HTTP servers
	beforeMB := atomic.LoadInt32(&mbHits)
	beforeWiki := atomic.LoadInt32(&wikiHits)
	cachedNotes, err := client.Fetch(context.Background(), q)
	if err != nil {
		t.Fatalf("second Fetch failed: %v", err)
	}
	if atomic.LoadInt32(&mbHits) != beforeMB || atomic.LoadInt32(&wikiHits) != beforeWiki {
		t.Errorf("expected in-memory cache hit with 0 extra HTTP requests")
	}
	if cachedNotes.Album != "Dive" {
		t.Errorf("cached notes mismatch: %+v", cachedNotes)
	}

	// New client sharing the same disk cacheDir should read from disk without HTTP hits
	client2 := NewLinerNotesClient(
		WithLinerNotesCacheDir(cacheDir),
		WithMusicBrainzBaseURL(mbServer.URL),
		WithWikipediaBaseURL(wikiServer.URL),
	)
	diskNotes, err := client2.Fetch(context.Background(), q)
	if err != nil {
		t.Fatalf("disk cache Fetch failed: %v", err)
	}
	if atomic.LoadInt32(&mbHits) != beforeMB || atomic.LoadInt32(&wikiHits) != beforeWiki {
		t.Errorf("expected on-disk cache hit with 0 extra HTTP requests")
	}
	if diskNotes.OriginCity != "San Francisco" {
		t.Errorf("disk cached OriginCity mismatch: %q", diskNotes.OriginCity)
	}
}

func TestLinerNotesOfflineFallback(t *testing.T) {
	// Point to closed servers to simulate 100% offline environment
	closedServer := httptest.NewServer(http.NotFoundHandler())
	closedServer.Close()

	client := NewLinerNotesClient(
		WithLinerNotesCacheDir(t.TempDir()),
		WithMusicBrainzBaseURL(closedServer.URL),
		WithWikipediaBaseURL(closedServer.URL),
	)

	notes, err := client.Fetch(context.Background(), LinerNotesQuery{
		RawTrack:    "Boards of Canada - Roygbiv",
		StationName: "SomaFM Drone Zone",
		Genre:       "Ambient / IDM",
		Country:     "Scotland",
		Bitrate:     128,
		Codec:       "MP3",
	})
	if err != nil {
		t.Fatalf("Fetch offline fallback failed: %v", err)
	}
	if notes.Artist != "Boards of Canada" || notes.Title != "Roygbiv" {
		t.Errorf("expected parsed artist/title, got %q - %q", notes.Artist, notes.Title)
	}
	if notes.Source != "Offline Station Metadata" {
		t.Errorf("expected Source 'Offline Station Metadata', got %q", notes.Source)
	}
	if notes.Biography == "" || len(notes.TrackTrivia) == 0 {
		t.Errorf("expected non-empty offline biography and trivia: %+v", notes)
	}
}
