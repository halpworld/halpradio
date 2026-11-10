package radio

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/halpworld/halpradio/pkg/radio/netguard"
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
	// Endpoint overrides the channel list URL (tests); empty means the
	// official https://somafm.com/channels.json.
	Endpoint string
}

// maxSomaFMBytes caps the channel list response (the real one is ~40 KB).
const maxSomaFMBytes = 1 << 20

func NewSomaFMClient() *SomaFMClient {
	return &SomaFMClient{
		HTTPClient: netguard.NewClient(6 * time.Second),
	}
}

// FetchChannels fetches the live list of channels directly from SomaFM official JSON API
func (c *SomaFMClient) FetchChannels() ([]Station, error) {
	endpoint := "https://somafm.com/channels.json"
	if c.Endpoint != "" {
		endpoint = c.Endpoint
	}

	req, err := http.NewRequestWithContext(netguard.WithOrigin(context.Background(), endpoint), "GET", endpoint, nil)
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

	body, err := netguard.ReadLimited(resp.Body, maxSomaFMBytes)
	if err != nil {
		return nil, err
	}
	var data SomaFMResponse
	if err := json.Unmarshal(body, &data); err != nil {
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

	// Channel IDs and titles come from the network: the same gate as every
	// other station source drops bad IDs and strips control sequences.
	stations, _ = sanitizeStations(stations, "somafm")
	return stations, nil
}
