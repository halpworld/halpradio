package radio

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CheckStreamHealth performs a fast HTTP GET request to verify if a stream URL is live and responsive.
func CheckStreamHealth(streamURL string, timeout time.Duration) bool {
	streamURL = strings.TrimSpace(streamURL)
	if streamURL == "" {
		return false
	}

	u, err := url.Parse(streamURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}

	if timeout <= 0 {
		timeout = 3 * time.Second
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", streamURL, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "halpradio/1.0")

	client := &http.Client{
		Timeout: timeout,
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		cType := strings.ToLower(resp.Header.Get("Content-Type"))
		if strings.Contains(cType, "audio") || strings.Contains(cType, "mpeg") || strings.Contains(cType, "aac") || strings.Contains(cType, "ogg") || strings.Contains(cType, "octet-stream") || cType == "" {
			return true
		}
		return true
	}

	return false
}
