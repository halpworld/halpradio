package radio

import (
	"strings"
	"testing"
)

func TestCountryFlag(t *testing.T) {
	tests := []struct {
		country  string
		expected string
	}{
		{"US", "🇺🇸"},
		{"gb", "🇬🇧"},
		{"SE", "🇸🇪"},
		{"JPN", "JPN"},
		{"", "🌐"},
	}

	for _, tt := range tests {
		st := Station{Country: tt.country}
		got := st.CountryFlag()
		if got != tt.expected {
			t.Errorf("CountryFlag(%q) = %q; want %q", tt.country, got, tt.expected)
		}
	}
}

func TestToYAMLSnippet(t *testing.T) {
	st := Station{
		ID:       "lofi-station",
		Name:     "Lofi Chill",
		URL:      "http://lofi.stream",
		Genre:    "Lofi / Beats",
		Country:  "jp",
		Bitrate:  0,  // Should default to 128
		Codec:    "", // Should default to MP3
		Homepage: "https://lofi.stream",
	}

	snippet := st.ToYAMLSnippet()
	if !strings.Contains(snippet, "id: lofi-station") {
		t.Errorf("Snippet missing ID: %s", snippet)
	}
	if !strings.Contains(snippet, "bitrate: 128") {
		t.Errorf("Snippet missing default bitrate: %s", snippet)
	}
	if !strings.Contains(snippet, "codec: MP3") {
		t.Errorf("Snippet missing default codec: %s", snippet)
	}
	if !strings.Contains(snippet, "country: JP") {
		t.Errorf("Snippet missing capitalized country: %s", snippet)
	}
}
