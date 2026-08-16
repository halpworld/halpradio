package radio

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
