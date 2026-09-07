package fingerprint

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/halpworld/halpradio/pkg/radio"
)

const (
	DefaultAcoustIDKey = "v8pQ6oyB"
	DefaultAcoustIDURL = "https://api.acoustid.org/v2/lookup"
)

// AcoustIDResponse mirrors the JSON response returned by the AcoustID lookup endpoint.
type AcoustIDResponse struct {
	Status string `json:"status"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
	Results []struct {
		ID         string  `json:"id"`
		Score      float64 `json:"score"`
		Recordings []struct {
			ID      string `json:"id"`
			Title   string `json:"title"`
			Artists []struct {
				Name       string `json:"name"`
				Joinphrase string `json:"joinphrase,omitempty"`
			} `json:"artists"`
			ReleaseGroups []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
				Type  string `json:"type"`
			} `json:"releasegroups"`
		} `json:"recordings"`
	} `json:"results"`
}

// Client coordinates stream capturing, fingerprinting, API lookup, and fallback.
type Client struct {
	AcoustidAPIKey string
	AcoustIDURL    string
	HTTPClient     *http.Client
	Cache          *LRUCache
}

// NewClient initializes a fingerprint recognition client with caching.
func NewClient(apiKey string) *Client {
	if apiKey == "" {
		if envKey := os.Getenv("ACOUSTID_API_KEY"); envKey != "" {
			apiKey = envKey
		} else {
			apiKey = DefaultAcoustIDKey
		}
	}
	return &Client{
		AcoustidAPIKey: apiKey,
		AcoustIDURL:    DefaultAcoustIDURL,
		HTTPClient:     &http.Client{Timeout: 8 * time.Second},
		Cache:          NewLRUCache(100, 30*time.Minute),
	}
}

// Identify samples the stream at streamURL, fingerprints it, and queries AcoustID / MusicBrainz.
func (c *Client) Identify(ctx context.Context, streamURL string, stationID string, stationName string, candidateTitle string) (*Result, error) {
	if streamURL == "" {
		return nil, fmt.Errorf("empty stream URL")
	}

	// 1. Check URL cache
	if cached, ok := c.Cache.Get(streamURL); ok {
		return cached, nil
	}

	// 2. Capture a 5-second PCM audio buffer
	wavPath, cleanup, err := CaptureAudioSample(ctx, streamURL, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("audio capture failed: %w", err)
	}
	defer cleanup()

	// 3. Compute Chromaprint fingerprint
	fpStr, duration, err := ComputeFingerprint(ctx, wavPath)
	if err != nil {
		return nil, fmt.Errorf("fingerprint generation failed: %w", err)
	}

	// 4. Check fingerprint cache
	if cached, ok := c.Cache.Get(fpStr); ok {
		return cached, nil
	}

	// 5. Query AcoustID API
	result, acoustidErr := c.queryAcoustID(ctx, fpStr, duration, stationID, stationName)
	if acoustidErr == nil && result != nil {
		result.Fingerprint = fpStr
		result.Duration = duration
		c.Cache.Put(streamURL, result)
		c.Cache.Put(fpStr, result)
		return result, nil
	}

	// 6. Fallback: MusicBrainz candidate metadata search
	if candidateTitle != "" {
		san := radio.SanitizeTrackTitle(candidateTitle, stationName)
		if san.IsClean && san.CleanTitle != "" {
			mbMatch, mbErr := radio.ValidateCandidateWithMusicBrainz(ctx, san.Artist, san.Title)
			if mbErr == nil && mbMatch != nil {
				res := &Result{
					Artist:      mbMatch.Artist,
					Title:       mbMatch.Title,
					Album:       mbMatch.Album,
					Year:        mbMatch.Year,
					Confidence:  mbMatch.Confidence,
					Source:      "MusicBrainz",
					StationID:   stationID,
					StationName: stationName,
					Fingerprint: fpStr,
					Duration:    duration,
				}
				if res.Confidence < 0.5 {
					res.Confidence = 0.85
				}
				c.Cache.Put(streamURL, res)
				c.Cache.Put(fpStr, res)
				return res, nil
			}
		}
	}

	if acoustidErr != nil {
		return nil, acoustidErr
	}
	return nil, fmt.Errorf("no acoustic match found for stream audio")
}

func (c *Client) queryAcoustID(ctx context.Context, fingerprint string, duration float64, stationID, stationName string) (*Result, error) {
	if c.AcoustidAPIKey == "" {
		return nil, fmt.Errorf("missing AcoustID API key")
	}

	form := url.Values{}
	form.Set("client", c.AcoustidAPIKey)
	form.Set("meta", "recordings releasegroups")
	form.Set("duration", fmt.Sprintf("%d", int(duration)))
	form.Set("fingerprint", fingerprint)

	req, err := http.NewRequestWithContext(ctx, "POST", c.AcoustIDURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "halpradio/1.0 (https://github.com/halpworld/halpradio)")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var payload AcoustIDResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	if payload.Status != "ok" {
		if payload.Error != nil {
			return nil, fmt.Errorf("AcoustID error %d: %s", payload.Error.Code, payload.Error.Message)
		}
		return nil, fmt.Errorf("AcoustID request failed with status: %s", payload.Status)
	}

	if len(payload.Results) == 0 {
		return nil, fmt.Errorf("no match found in AcoustID database")
	}

	top := payload.Results[0]
	if len(top.Recordings) == 0 {
		return nil, fmt.Errorf("no recording data attached to match")
	}

	rec := top.Recordings[0]
	var artistParts []string
	for _, a := range rec.Artists {
		artistParts = append(artistParts, a.Name)
	}
	artist := strings.Join(artistParts, " & ")

	album := ""
	if len(rec.ReleaseGroups) > 0 {
		album = rec.ReleaseGroups[0].Title
	}

	confidence := top.Score
	if confidence <= 0 {
		confidence = 0.90
	}

	return &Result{
		Artist:      artist,
		Title:       rec.Title,
		Album:       album,
		Confidence:  confidence,
		Source:      "AcoustID",
		StationID:   stationID,
		StationName: stationName,
	}, nil
}
