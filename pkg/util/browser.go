package util

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
)

// IsValidHTTPURL checks that a URL is a valid http or https URL with non-empty host.
func IsValidHTTPURL(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// BuildSearchURL generates a web search URL for a given track query and search provider.
func BuildSearchURL(provider string, query string) string {
	query = strings.TrimSpace(query)
	escaped := url.QueryEscape(query)

	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "youtube", "yt", "youtubemusic":
		return fmt.Sprintf("https://music.youtube.com/search?q=%s", escaped)
	case "apple", "applemusic":
		return fmt.Sprintf("https://music.apple.com/us/search?term=%s", escaped)
	case "duckduckgo", "ddg":
		return fmt.Sprintf("https://duckduckgo.com/?q=%s", escaped)
	case "google":
		return fmt.Sprintf("https://www.google.com/search?q=%s", escaped)
	case "spotify":
		fallthrough
	default:
		return fmt.Sprintf("https://open.spotify.com/search/%s", escaped)
	}
}

// OpenURL opens the given HTTP/HTTPS URL in the system's default web browser.
// It verifies the URL format to protect against command-injection vulnerabilities.
func OpenURL(rawURL string) error {
	if !IsValidHTTPURL(rawURL) {
		return fmt.Errorf("invalid URL (only http and https supported): %s", rawURL)
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", rawURL)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL)
	default:
		// Linux, FreeBSD, OpenBSD, Solaris
		cmd = exec.Command("xdg-open", rawURL)
	}

	return cmd.Start()
}
