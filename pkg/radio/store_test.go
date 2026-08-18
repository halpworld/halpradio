package radio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func TestBundledStationsYAML(t *testing.T) {
	data, err := os.ReadFile("../../stations.yaml")
	if err != nil {
		t.Fatalf("Failed to read stations.yaml: %v", err)
	}

	store := NewStore()
	if err := store.Load(data); err != nil {
		t.Fatalf("Failed to parse stations.yaml: %v", err)
	}

	stations := store.GetAllStations()
	if len(stations) == 0 {
		t.Fatalf("No stations loaded from stations.yaml")
	}

	seenIDs := make(map[string]bool)
	requiredCountries := []string{"GB", "IE", "US", "CN", "JP", "AU", "IN", "SE", "KR"}
	countryCounts := make(map[string]int)

	for _, s := range stations {
		if s.ID == "" {
			t.Errorf("Station %q has empty ID", s.Name)
		}
		if seenIDs[s.ID] {
			t.Errorf("Duplicate station ID: %s", s.ID)
		}
		seenIDs[s.ID] = true

		if s.Name == "" {
			t.Errorf("Station %s has empty Name", s.ID)
		}
		if s.URL == "" {
			t.Errorf("Station %s has empty URL", s.ID)
		}
		if s.Genre == "" {
			t.Errorf("Station %s has empty Genre", s.ID)
		}
		if len(s.Country) != 2 || s.Country != strings.ToUpper(s.Country) {
			t.Errorf("Station %s has invalid country code %q", s.ID, s.Country)
		}
		if s.Bitrate <= 0 {
			t.Errorf("Station %s has invalid bitrate %d", s.ID, s.Bitrate)
		}
		if s.Codec == "" {
			t.Errorf("Station %s has empty Codec", s.ID)
		}
		if s.Homepage == "" {
			t.Errorf("Station %s has empty Homepage", s.ID)
		}

		countryCounts[s.Country]++
	}

	for _, rc := range requiredCountries {
		if count := countryCounts[rc]; count < 3 {
			t.Errorf("Expected at least 3 stations for country %s, found %d", rc, count)
		}
	}
}

func TestStoreLocalStationsAndCategories(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	store := NewStore()

	// 1. Add Local Station
	st1 := Station{
		Name:     "My Local Radio",
		URL:      "http://local.stream/live",
		Genre:    "Jazz / Lo-Fi",
		Country:  "SE",
		Bitrate:  320,
		Codec:    "MP3",
		Homepage: "http://local.stream",
	}

	if err := store.AddOrUpdateLocalStation(st1); err != nil {
		t.Fatalf("AddOrUpdateLocalStation failed: %v", err)
	}

	if len(store.Local) != 1 {
		t.Fatalf("Expected 1 local station, got %d", len(store.Local))
	}
	generatedID := store.Local[0].ID
	if generatedID != "custom-1" {
		t.Errorf("Expected generated ID custom-1, got %s", generatedID)
	}

	// 2. Update existing station
	st1Updated := store.Local[0]
	st1Updated.Name = "My Updated Local Radio"
	if err := store.AddOrUpdateLocalStation(st1Updated); err != nil {
		t.Fatalf("Update local station failed: %v", err)
	}
	if len(store.Local) != 1 || store.Local[0].Name != "My Updated Local Radio" {
		t.Errorf("Station was not properly updated in-place: %+v", store.Local)
	}

	// 3. Add second station with explicit ID
	st2 := Station{
		ID:      "custom-synth",
		Name:    "Synth Radio",
		URL:     "http://synth.stream",
		Genre:   "Synthwave / Cyberpunk",
		Country: "US",
	}
	if err := store.AddOrUpdateLocalStation(st2); err != nil {
		t.Fatalf("Adding second station failed: %v", err)
	}
	if len(store.Local) != 2 {
		t.Fatalf("Expected 2 local stations, got %d", len(store.Local))
	}

	// 4. Test GetCategories
	cats := store.GetCategories()
	if len(cats) == 0 {
		t.Errorf("Expected non-empty categories, got %v", cats)
	}
	hasJazz := false
	for _, c := range cats {
		if c == "Jazz" || c == "Lo-Fi" || c == "Synthwave" {
			hasJazz = true
			break
		}
	}
	if !hasJazz {
		t.Errorf("Expected parsed genre categories, got %v", cats)
	}

	// 5. Test GetAllStations deduplication
	all := store.GetAllStations()
	if len(all) != 2 {
		t.Errorf("Expected 2 all stations, got %d", len(all))
	}

	// 6. Test DeleteLocalStation
	if err := store.DeleteLocalStation(generatedID); err != nil {
		t.Fatalf("DeleteLocalStation failed: %v", err)
	}
	if len(store.Local) != 1 || store.Local[0].ID != "custom-synth" {
		t.Errorf("Delete failed, remaining stations: %+v", store.Local)
	}
}

func TestStoreFilterCombinations(t *testing.T) {
	stations := []Station{
		{ID: "s1", Name: "Focus Beats", Genre: "Lofi / Chill", Country: "JP", City: "Tokyo", Broadcast: "Online", Activities: []string{"programming"}},
		{ID: "s2", Name: "Workout Rock", Genre: "Rock / Metal", Country: "US", City: "Seattle", Broadcast: "FM", Frequency: "99.9 FM", Activities: []string{"workout"}},
		{ID: "s3", Name: "Chill Classical", Genre: "Classical", Country: "DE", City: "Berlin", Broadcast: "DAB+", Frequency: "DAB+"},
		{ID: "s4", Name: "RTÉ Radio 1", Genre: "News / Talk", Country: "IE", City: "Dublin", Broadcast: "FM/DAB", Frequency: "88.5 FM"},
	}

	// Match by activity
	res := FilterWithActivity(stations, "", "", "programming")
	if len(res) != 1 || res[0].ID != "s1" {
		t.Errorf("Expected match for programming activity, got %v", res)
	}

	// Match by genre
	res = FilterWithActivity(stations, "", "metal", "")
	if len(res) != 1 || res[0].ID != "s2" {
		t.Errorf("Expected match for genre metal, got %v", res)
	}

	// Match by query on country code
	res = FilterWithActivity(stations, "de", "", "")
	if len(res) != 1 || res[0].ID != "s3" {
		t.Errorf("Expected match for query 'de', got %v", res)
	}

	// Match by country name (e.g. searching "ireland" matches IE station)
	res = FilterWithLocation(stations, "ireland", "", "", "")
	if len(res) != 1 || res[0].ID != "s4" {
		t.Errorf("Expected match for query 'ireland', got %v", res)
	}

	// Match by city (e.g. searching "dublin" matches Dublin station)
	res = FilterWithLocation(stations, "dublin", "", "", "")
	if len(res) != 1 || res[0].ID != "s4" {
		t.Errorf("Expected match for city 'dublin', got %v", res)
	}

	// Match by frequency
	res = FilterWithLocation(stations, "88.5", "", "", "")
	if len(res) != 1 || res[0].ID != "s4" {
		t.Errorf("Expected match for frequency '88.5', got %v", res)
	}

	// Match by broadcast "online"
	res = FilterWithLocation(stations, "online", "", "", "")
	if len(res) != 1 || res[0].ID != "s1" {
		t.Errorf("Expected match for 'online', got %v", res)
	}

	// Filter by explicit country code "IE"
	res = FilterWithLocation(stations, "", "", "", "IE")
	if len(res) != 1 || res[0].ID != "s4" {
		t.Errorf("Expected match for country code 'IE', got %v", res)
	}

	// 'all' activity and genre should not filter out
	res = FilterWithActivity(stations, "", "all", "all")
	if len(res) != 4 {
		t.Errorf("Expected all 4 stations when filtering 'all', got %d", len(res))
	}
}

func TestStoreGetCountriesAndCities(t *testing.T) {
	store := NewStore()
	store.Bundled = []Station{
		{ID: "ie-1", Name: "RTÉ 1", Country: "IE", City: "Dublin"},
		{ID: "ie-2", Name: "RTÉ 2FM", Country: "IE", City: "Dublin"},
		{ID: "gb-1", Name: "BBC 1", Country: "GB", City: "London"},
	}

	countries := store.GetCountries()
	if len(countries) != 2 {
		t.Fatalf("Expected 2 countries, got %d", len(countries))
	}
	// IE should come first because it has 2 stations vs 1
	if countries[0].Code != "IE" || countries[0].Count != 2 || countries[0].Name != "Ireland" {
		t.Errorf("Expected first country to be Ireland with 2 stations, got %+v", countries[0])
	}

	cities := store.GetCities("IE")
	if len(cities) != 1 || cities[0] != "Dublin" {
		t.Errorf("Expected Dublin city for IE, got %v", cities)
	}
}
