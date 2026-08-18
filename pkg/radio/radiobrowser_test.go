package radio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewRadioBrowserClient(t *testing.T) {
	client := NewRadioBrowserClient()
	if client == nil {
		t.Fatalf("Expected non-nil client")
	}
	if len(client.BaseURLs) == 0 {
		t.Errorf("Expected default mirror base URLs")
	}
	if client.HTTPClient == nil {
		t.Errorf("Expected default HTTP client")
	}
}

func TestRadioBrowserSearchEdgeCases(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stations := []RadioBrowserStation{
			{
				StationUUID: "a1b2c3d4-e5f6-7890-1234-56789abcdef0",
				Name:        "Long Tag Station",
				URL:         "", // empty url, should use fallback
				URLFallback: "http://fallback.stream",
				Tags:        "verylonggenrethatshouldbetruncatedbeyondthirtycharacters",
				CountryCode: "US",
				Bitrate:     192,
				Codec:       "AAC",
				Homepage:    "https://station.example.com",
			},
			{
				StationUUID: "empty-urls",
				Name:        "Skipped Station",
				URL:         "",
				URLFallback: "",
			},
		}
		_ = json.NewEncoder(w).Encode(stations)
	}))
	defer ts.Close()

	client := &RadioBrowserClient{
		BaseURLs:   []string{ts.URL},
		HTTPClient: &http.Client{Timeout: 2 * time.Second},
	}

	results, err := client.Search("jazz", 0)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	// Should have filtered out empty url station and kept 1
	if len(results) != 1 {
		t.Fatalf("Expected 1 valid station, got %d", len(results))
	}

	st := results[0]
	if st.URL != "http://fallback.stream" {
		t.Errorf("Expected fallback URL, got %s", st.URL)
	}
	if len(st.Genre) > 30 {
		t.Errorf("Expected truncated genre <= 30 chars, got length %d (%s)", len(st.Genre), st.Genre)
	}
	if len(st.ID) > 20 {
		t.Errorf("Expected ID <= 20 chars, got length %d (%s)", len(st.ID), st.ID)
	}
	if st.Source != "radiobrowser" {
		t.Errorf("Expected source radiobrowser, got %s", st.Source)
	}
}

func TestRadioBrowserSearchAllFail(t *testing.T) {
	client := &RadioBrowserClient{
		BaseURLs:   []string{"http://127.0.0.1:59998", "http://127.0.0.1:59999"},
		HTTPClient: &http.Client{Timeout: 100 * time.Millisecond},
	}

	_, err := client.Search("rock", 10)
	if err == nil {
		t.Errorf("Expected error when all mirrors fail, got nil")
	}
}

func TestParseSearchQuery(t *testing.T) {
	// Test explicit qualifiers
	p1 := ParseSearchQuery("country:IE city:Dublin tag:fm name:RTE")
	if p1.CountryCode != "IE" {
		t.Errorf("Expected CountryCode 'IE', got %q", p1.CountryCode)
	}
	if p1.State != "Dublin" {
		t.Errorf("Expected State 'Dublin', got %q", p1.State)
	}
	if p1.Tag != "fm" {
		t.Errorf("Expected Tag 'fm', got %q", p1.Tag)
	}
	if p1.Name != "RTE" {
		t.Errorf("Expected Name 'RTE', got %q", p1.Name)
	}

	// Test short qualifiers c: and t:
	p2 := ParseSearchQuery("c:ireland t:dab rte")
	if p2.CountryCode != "IE" {
		t.Errorf("Expected CountryCode 'IE', got %q", p2.CountryCode)
	}
	if p2.Tag != "dab" {
		t.Errorf("Expected Tag 'dab', got %q", p2.Tag)
	}
	if p2.Name != "rte" {
		t.Errorf("Expected Name 'rte', got %q", p2.Name)
	}

	// Test natural language multi-term parsing (e.g. "rte ireland")
	p3 := ParseSearchQuery("rte ireland")
	if p3.CountryCode != "IE" {
		t.Errorf("Expected CountryCode 'IE' from 'rte ireland', got %q", p3.CountryCode)
	}
	if p3.Name != "rte" {
		t.Errorf("Expected Name 'rte' from 'rte ireland', got %q", p3.Name)
	}

	// Test natural language with FM tag (e.g. "fm dublin")
	p4 := ParseSearchQuery("fm dublin")
	if p4.Tag != "fm" {
		t.Errorf("Expected Tag 'fm', got %q", p4.Tag)
	}
	if p4.Name != "dublin" {
		t.Errorf("Expected Name 'dublin', got %q", p4.Name)
	}

	// Test empty query
	pEmpty := ParseSearchQuery("")
	if pEmpty.Name != "" || pEmpty.CountryCode != "" {
		t.Errorf("Expected empty search params for empty input")
	}
}

func TestSearchByCountryAndLocation(t *testing.T) {
	var requestedURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedURL = r.URL.String()
		stations := []RadioBrowserStation{
			{
				StationUUID: "rte-uuid-12345",
				Name:        "RTÉ Radio 1",
				URL:         "http://icecast.rte.ie/radio1",
				CountryCode: "IE",
				State:       "Dublin",
				Bitrate:     160,
				Codec:       "MP3",
			},
		}
		_ = json.NewEncoder(w).Encode(stations)
	}))
	defer ts.Close()

	client := &RadioBrowserClient{
		BaseURLs:   []string{ts.URL},
		HTTPClient: &http.Client{Timeout: 2 * time.Second},
	}

	// Test SearchByCountry
	results, err := client.SearchByCountry("IE", 20)
	if err != nil {
		t.Fatalf("SearchByCountry failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("Expected 1 station, got %d", len(results))
	}
	if results[0].Country != "IE" || results[0].City != "Dublin" {
		t.Errorf("Expected Country IE and City Dublin, got Country %s City %s", results[0].Country, results[0].City)
	}
	if !strings.Contains(requestedURL, "countrycode=IE") {
		t.Errorf("Expected countrycode=IE in URL, got %s", requestedURL)
	}

	// Test SearchByLocation
	_, err = client.SearchByLocation("IE", "Dublin", 10)
	if err != nil {
		t.Fatalf("SearchByLocation failed: %v", err)
	}
	if !strings.Contains(requestedURL, "state=Dublin") {
		t.Errorf("Expected state=Dublin in URL, got %s", requestedURL)
	}
}

func TestRadioBrowserBroadcastTagParsing(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stations := []RadioBrowserStation{
			{
				StationUUID: "fm-tagged",
				Name:        "Verified Broadcaster",
				URL:         "http://stream.fm",
				Tags:        "news, talk, fm",
			},
			{
				StationUUID: "dab-tagged",
				Name:        "Digital Broadcaster",
				URL:         "http://stream.dab",
				Tags:        "classical, dab+",
			},
			{
				StationUUID: "untagged-name-has-fm",
				Name:        "Awesome FM Internet Webradio",
				URL:         "http://stream.web",
				Tags:        "ambient, chillout, webradio",
			},
		}
		_ = json.NewEncoder(w).Encode(stations)
	}))
	defer ts.Close()

	client := &RadioBrowserClient{
		BaseURLs:   []string{ts.URL},
		HTTPClient: &http.Client{Timeout: 2 * time.Second},
	}

	results, err := client.Search("test", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Expected 3 stations, got %d", len(results))
	}

	if results[0].Broadcast != "FM" || !results[0].IsTerrestrial() {
		t.Errorf("Expected results[0] with tag 'fm' to have Broadcast 'FM', got %q", results[0].Broadcast)
	}

	if results[1].Broadcast != "DAB+" || !results[1].IsTerrestrial() {
		t.Errorf("Expected results[1] with tag 'dab+' to have Broadcast 'DAB+', got %q", results[1].Broadcast)
	}

	// Untagged station with "FM" in name MUST strictly be "Online"
	if results[2].Broadcast != "Online" || results[2].IsTerrestrial() {
		t.Errorf("Expected results[2] without terrestrial tag to strictly be 'Online', got %q (isTerrestrial=%v)", results[2].Broadcast, results[2].IsTerrestrial())
	}
}
