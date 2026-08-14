package radio

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type SomaFMChannel struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Genre       string `json:"genre"`
	DJ          string `json:"dj"`
}

type SomaFMResponse struct {
	Channels []SomaFMChannel `json:"channels"`
}

type SomaFMClient struct {
	HTTPClient *http.Client
}

func NewSomaFMClient() *SomaFMClient {
	return &SomaFMClient{
		HTTPClient: &http.Client{
			Timeout: 6 * time.Second,
		},
	}
}

// FetchChannels fetches the live list of channels directly from SomaFM official JSON API
func (c *SomaFMClient) FetchChannels() ([]Station, error) {
	endpoint := "https://somafm.com/channels.json"

	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "halpradio/1.0")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SomaFM API status code %d", resp.StatusCode)
	}

	var data SomaFMResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}

	var stations []Station
	for _, ch := range data.Channels {
		if ch.ID == "" {
			continue
		}

		genre := strings.ReplaceAll(ch.Genre, "|", " / ")
		if genre == "" {
			genre = "SomaFM / Commercial Free"
		}

		streamURL := fmt.Sprintf("https://ice1.somafm.com/%s-128-mp3", ch.ID)

		st := Station{
			ID:       fmt.Sprintf("somafm-%s", ch.ID),
			Name:     fmt.Sprintf("SomaFM: %s", ch.Title),
			URL:      streamURL,
			Genre:    genre,
			Country:  "US",
			Bitrate:  128,
			Codec:    "MP3",
			Homepage: fmt.Sprintf("https://somafm.com/%s/", ch.ID),
			Source:   "somafm",
		}
		stations = append(stations, st)
	}

	return stations, nil
}
