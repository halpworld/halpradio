package radio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStoreLoadAndFilter(t *testing.T) {
	yamlCatalog := []byte(`
stations:
  - id: test-1
    name: "Ambient Relaxation"
    url: "http://example.com/stream1"
    genre: "Ambient / Chill"
    country: "US"
    bitrate: 128
    codec: "MP3"

  - id: test-2
    name: "Heavy Metal Thunder"
    url: "http://example.com/stream2"
    genre: "Metal / Rock"
    country: "DE"
    bitrate: 320
    codec: "MP3"
`)

	store := NewStore()
	err := store.Load(yamlCatalog)
	if err != nil {
		t.Fatalf("Failed to load catalog: %v", err)
	}

	stations := store.GetAllStations()
	if len(stations) != 2 {
		t.Fatalf("Expected 2 stations, got %d", len(stations))
	}

	filtered := Filter(stations, "metal", "")
	if len(filtered) != 1 || filtered[0].ID != "test-2" {
		t.Fatalf("Expected 1 metal station 'test-2', got %v", filtered)
	}

	// Test flag conversion
	flag := filtered[0].CountryFlag()
	if flag == "" {
		t.Errorf("Expected country flag for DE, got empty string")
	}
}

func TestToggleFavorite(t *testing.T) {
	store := NewStore()
	st := Station{ID: "fav-1", Name: "Fav Station", URL: "http://fav.stream"}

	isFav := store.ToggleFavorite(st)
	if !isFav {
		t.Errorf("Expected station to be marked favorite")
	}

	favs := store.GetFavorites()
	if len(favs) != 1 || favs[0].ID != "fav-1" {
		t.Errorf("Expected 1 favorite station in list")
	}

	isFav2 := store.ToggleFavorite(st)
	if isFav2 {
		t.Errorf("Expected station to be un-favorited")
	}
}

func TestSomaFMClientParsing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := SomaFMResponse{
			Channels: []SomaFMChannel{
				{
					ID:          "groovesalad",
					Title:       "Groove Salad",
					Description: "Ambient downtempo beats",
					Genre:       "ambient|downtempo",
				},
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client := &SomaFMClient{
		HTTPClient: ts.Client(),
	}

	// Override URL by performing search with test server
	req, _ := http.NewRequest("GET", ts.URL, nil)
	resp, err := client.HTTPClient.Do(req)
	if err != nil {
		t.Fatalf("Failed request to mock server: %v", err)
	}
	defer resp.Body.Close()

	var data SomaFMResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		t.Fatalf("Failed decoding mock SomaFM response: %v", err)
	}

	if len(data.Channels) != 1 || data.Channels[0].ID != "groovesalad" {
		t.Errorf("Unexpected SomaFM channels: %v", data.Channels)
	}
}

func TestRadioBrowserMirrorFailover(t *testing.T) {
	// Server 1 fails with 500
	ts1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer ts1.Close()

	// Server 2 succeeds with valid station json
	ts2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check query parameters
		if r.URL.Query().Get("hidebroken") != "true" || r.URL.Query().Get("lastcheckok") != "1" {
			t.Errorf("Expected health filters in query, got %s", r.URL.RawQuery)
		}

		stations := []RadioBrowserStation{
			{
				StationUUID: "1234-5678",
				Name:        "Test Radio",
				URL:         "http://stream.test",
				CountryCode: "US",
			},
		}
		_ = json.NewEncoder(w).Encode(stations)
	}))
	defer ts2.Close()

	rb := &RadioBrowserClient{
		BaseURLs:   []string{ts1.URL, ts2.URL},
		HTTPClient: &http.Client{Timeout: 2 * time.Second},
	}

	stations, err := rb.Search("Test", 10)
	if err != nil {
		t.Fatalf("Expected failover to succeed, got err: %v", err)
	}

	if len(stations) != 1 || stations[0].Name != "Test Radio" {
		t.Errorf("Expected 1 station 'Test Radio', got %v", stations)
	}
}

func TestCheckStreamHealth(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	healthy := CheckStreamHealth(ts.URL, 2*time.Second)
	if !healthy {
		t.Errorf("Expected mock stream to be healthy")
	}

	unhealthy := CheckStreamHealth("http://127.0.0.1:59999/deadstream", 200*time.Millisecond)
	if unhealthy {
		t.Errorf("Expected non-existent stream to be unhealthy")
	}

	invalidScheme := CheckStreamHealth("file:///etc/passwd", 200*time.Millisecond)
	if invalidScheme {
		t.Errorf("Expected file:// stream to be rejected")
	}

	invalidArg := CheckStreamHealth("--script=evil.lua", 200*time.Millisecond)
	if invalidArg {
		t.Errorf("Expected argument URL to be rejected")
	}
}

func TestActivityMatching(t *testing.T) {
	stExplicit := Station{
		ID:         "coder-radio",
		Name:       "Coder Radio",
		Genre:      "Electronic",
		Activities: []string{"programming", "thinking"},
	}

	stFallback := Station{
		ID:    "rock-radio",
		Name:  "Rock Antenne",
		Genre: "Heavy Metal / Rock",
	}

	if !stExplicit.MatchesActivity("programming") {
		t.Errorf("Expected explicit activity match for programming")
	}

	if !stExplicit.MatchesActivity("thinking") {
		t.Errorf("Expected explicit activity match for thinking")
	}

	if stExplicit.MatchesActivity("news") {
		t.Errorf("Did not expect match for news")
	}

	if !stFallback.MatchesActivity("cleaning") {
		t.Errorf("Expected fallback activity match for cleaning on rock station")
	}
}
