package util

import (
	"strings"
	"testing"
)

func TestIsValidHTTPURL(t *testing.T) {
	valid := []string{
		"http://open.spotify.com/search/Tycho",
		"https://music.youtube.com/search?q=test",
		"https://duckduckgo.com/?q=Boards+of+Canada",
	}
	for _, u := range valid {
		if !IsValidHTTPURL(u) {
			t.Errorf("Expected %q to be valid HTTP URL", u)
		}
	}

	invalid := []string{
		"",
		"   ",
		"file:///etc/passwd",
		"--option=evil",
		"javascript:alert(1)",
		"ftp://example.com",
		"http://",
		"https://",
	}
	for _, u := range invalid {
		if IsValidHTTPURL(u) {
			t.Errorf("Expected %q to be invalid HTTP URL", u)
		}
	}
}

func TestBuildSearchURL(t *testing.T) {
	tests := []struct {
		provider string
		query    string
		contains string
	}{
		{"spotify", "Tycho - A Walk", "https://open.spotify.com/search/Tycho+-+A+Walk"},
		{"youtube", "Boards of Canada - Dayvan Cowboy", "https://music.youtube.com/search?q=Boards+of+Canada+-+Dayvan+Cowboy"},
		{"apple", "Khruangbin - Texas Sun", "https://music.apple.com/us/search?term=Khruangbin+-+Texas+Sun"},
		{"duckduckgo", "Photay - Solaris", "https://duckduckgo.com/?q=Photay+-+Solaris"},
		{"google", "Kraftwerk - Computer Love", "https://www.google.com/search?q=Kraftwerk+-+Computer+Love"},
		{"", "Tycho", "https://open.spotify.com/search/Tycho"},
		{"unknown", "Tycho", "https://open.spotify.com/search/Tycho"},
	}

	for _, tt := range tests {
		got := BuildSearchURL(tt.provider, tt.query)
		if !strings.HasPrefix(got, tt.contains) && got != tt.contains {
			t.Errorf("BuildSearchURL(%q, %q) = %q, want prefix/match %q",
				tt.provider, tt.query, got, tt.contains)
		}
	}
}

func TestOpenURLRejectsInvalidURL(t *testing.T) {
	err := OpenURL("file:///tmp/evil")
	if err == nil {
		t.Errorf("Expected error when opening non-HTTP URL")
	}

	err = OpenURL("--script=evil.sh")
	if err == nil {
		t.Errorf("Expected error when opening CLI argument as URL")
	}
}

func TestOpenURLWithMock(t *testing.T) {
	var openedURL string
	cleanup := SetURLOpenerForTesting(func(rawURL string) error {
		openedURL = rawURL
		return nil
	})
	defer cleanup()

	target := "https://open.spotify.com/search/Solar+Fields"
	err := OpenURL(target)
	if err != nil {
		t.Fatalf("OpenURL failed: %v", err)
	}
	if openedURL != target {
		t.Errorf("Expected opener called with %q, got %q", target, openedURL)
	}
}
