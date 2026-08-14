package radio

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type RadioBrowserStation struct {
	StationUUID string `json:"stationuuid"`
	Name        string `json:"name"`
	URL         string `json:"url_resolved"`
	URLFallback string `json:"url"`
	Homepage    string `json:"homepage"`
	Favicon     string `json:"favicon"`
	Tags        string `json:"tags"`
	CountryCode string `json:"countrycode"`
	State       string `json:"state"`
	Language    string `json:"language"`
	Votes       int    `json:"votes"`
	Codec       string `json:"codec"`
	Bitrate     int    `json:"bitrate"`
	LastCheckOk int    `json:"lastcheckok"`
}

type RadioBrowserClient struct {
	BaseURLs   []string
	HTTPClient *http.Client
}

func NewRadioBrowserClient() *RadioBrowserClient {
	return &RadioBrowserClient{
		BaseURLs: []string{
			"https://de1.api.radio-browser.info",
			"https://nl1.api.radio-browser.info",
			"https://at1.api.radio-browser.info",
			"https://fr1.api.radio-browser.info",
		},
		HTTPClient: &http.Client{
			Timeout: 6 * time.Second,
		},
	}
}

func (rb *RadioBrowserClient) Search(searchTerm string, limit int) ([]Station, error) {
	if limit <= 0 {
		limit = 40
	}

	params := url.Values{}
	params.Set("limit", fmt.Sprintf("%d", limit))
	params.Set("order", "votes")
	params.Set("reverse", "true")
	params.Set("hidebroken", "true")
	params.Set("lastcheckok", "1")

	if searchTerm != "" {
		params.Set("name", searchTerm)
	}

	var lastErr error
	for _, baseURL := range rb.BaseURLs {
		endpoint := fmt.Sprintf("%s/json/stations/search?%s", baseURL, params.Encode())

		req, err := http.NewRequest("GET", endpoint, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "halpradio/1.0")

		resp, err := rb.HTTPClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("server %s status %d", baseURL, resp.StatusCode)
			continue
		}

		var rawStations []RadioBrowserStation
		if err := json.NewDecoder(resp.Body).Decode(&rawStations); err != nil {
			resp.Body.Close()
			lastErr = err
			continue
		}
		resp.Body.Close()

		var stations []Station
		for _, r := range rawStations {
			streamURL := r.URL
			if streamURL == "" {
				streamURL = r.URLFallback
			}
			if streamURL == "" {
				continue
			}

			genre := r.Tags
			if len(genre) > 30 {
				genre = genre[:30]
			}

			id := fmt.Sprintf("rb-%s", r.StationUUID)
			if len(id) > 20 {
				id = id[:20]
			}

			st := Station{
				ID:       id,
				Name:     r.Name,
				URL:      streamURL,
				Genre:    genre,
				Country:  r.CountryCode,
				Bitrate:  r.Bitrate,
				Codec:    r.Codec,
				Homepage: r.Homepage,
				Source:   "radiobrowser",
			}
			stations = append(stations, st)
		}

		return stations, nil
	}

	return nil, fmt.Errorf("RadioBrowser API failed: %v", lastErr)
}
