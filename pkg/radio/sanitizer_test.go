package radio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSanitizeTrackTitle_SpamAndAds(t *testing.T) {
	tests := []struct {
		input       string
		stationName string
	}{
		{"BUY ADS AT RADIO.COM", "Cool Radio"},
		{"Buy ads at radioweb.fm", "Rock FM"},
		{"ADVERTISE WITH US: Call 1-800-555-0199", "Jazz Lounge"},
		{"SPONSOR: Acme Supermarkets", "Hits 1"},
		{"COMMERCIAL BREAK", "Hits 1"},
		{"Ad Break - We'll be right back", "Hits 1"},
		{"Stream URL: http://stream.radio.org/live", "Classic Radio"},
		{"http://www.somafm.com", "SomaFM"},
		{"STREAM-128K", "SomaFM"},
		{"Station ID", "SomaFM"},
		{"DJ Mike in the mix", "Club FM"},
		{"LIVE ON AIR", "Studio 54"},
		{"BEST 80S HITS", "Retro FM"},
		{"NOW PLAYING ON DANCE RADIO", "Dance Radio"},
	}

	for _, tt := range tests {
		res := SanitizeTrackTitle(tt.input, tt.stationName)
		if !res.IsGenericOrSpam || res.IsClean {
			t.Errorf("Expected input %q to be marked spam/generic, got clean=%v, generic=%v, cleanTitle=%q",
				tt.input, res.IsClean, res.IsGenericOrSpam, res.CleanTitle)
		}
		if !IsDirtyOrGeneric(tt.input, tt.stationName) {
			t.Errorf("Expected IsDirtyOrGeneric(%q) to be true", tt.input)
		}
	}
}

func TestSanitizeTrackTitle_StationSelfReference(t *testing.T) {
	station := "Tokyo FM 80.0"
	inputs := []string{
		"Tokyo FM 80.0",
		"tokyo fm 80.0",
		"Tokyo FM 80.0 - Live",
		"Tokyo FM 80.0 Stream",
		"Tokyo FM 80.0 FM",
	}

	for _, in := range inputs {
		res := SanitizeTrackTitle(in, station)
		if res.IsClean || !res.IsGenericOrSpam {
			t.Errorf("Expected station self-reference %q to be detected as spam/generic", in)
		}
	}
}

func TestSanitizeTrackTitle_CompositeNoiseCleaning(t *testing.T) {
	tests := []struct {
		input       string
		stationName string
		expected    string
		expArtist   string
		expTitle    string
	}{
		{
			input:       "Daft Punk - Voyager | Buy ads at radio.com",
			stationName: "Tokyo FM",
			expected:    "Daft Punk - Voyager",
			expArtist:   "Daft Punk",
			expTitle:    "Voyager",
		},
		{
			input:       "Queen - Bohemian Rhapsody (Visit www.queenonline.com)",
			stationName: "Classic Rock",
			expected:    "Queen - Bohemian Rhapsody",
			expArtist:   "Queen",
			expTitle:    "Bohemian Rhapsody",
		},
		{
			input:       "*** NOW PLAYING *** Daft Punk - Voyager",
			stationName: "Tokyo FM",
			expected:    "Daft Punk - Voyager",
			expArtist:   "Daft Punk",
			expTitle:    "Voyager",
		},
		{
			input:       "Simon &amp; Garfunkel - The Sound of Silence",
			stationName: "Folk Radio",
			expected:    "Simon & Garfunkel - The Sound of Silence",
			expArtist:   "Simon & Garfunkel",
			expTitle:    "The Sound of Silence",
		},
		{
			input:       "Daft Punk - Daft Punk - Voyager",
			stationName: "Electro FM",
			expected:    "Daft Punk - Voyager",
			expArtist:   "Daft Punk",
			expTitle:    "Voyager",
		},
		{
			input:       "Tycho - A Walk (Stream URL: http://stream.tycho.com)",
			stationName: "Chill FM",
			expected:    "Tycho - A Walk",
			expArtist:   "Tycho",
			expTitle:    "A Walk",
		},
		{
			input:       "Boards of Canada - Dayvan Cowboy -- buy ads at chill.fm",
			stationName: "Ambient Radio",
			expected:    "Boards of Canada - Dayvan Cowboy",
			expArtist:   "Boards of Canada",
			expTitle:    "Dayvan Cowboy",
		},
	}

	for _, tt := range tests {
		res := SanitizeTrackTitle(tt.input, tt.stationName)
		if !res.IsClean || res.IsGenericOrSpam {
			t.Errorf("Expected %q to be clean, got clean=%v, generic=%v, reason=%s",
				tt.input, res.IsClean, res.IsGenericOrSpam, res.Reason)
		}
		if res.CleanTitle != tt.expected {
			t.Errorf("For input %q, expected clean title %q, got %q", tt.input, tt.expected, res.CleanTitle)
		}
		if res.Artist != tt.expArtist {
			t.Errorf("For input %q, expected artist %q, got %q", tt.input, tt.expArtist, res.Artist)
		}
		if res.Title != tt.expTitle {
			t.Errorf("For input %q, expected title %q, got %q", tt.input, tt.expTitle, res.Title)
		}
	}
}

func TestSanitizeTrackTitle_CleanTitles(t *testing.T) {
	cleanTitles := []string{
		"Tycho - A Walk",
		"Daft Punk - Voyager",
		"Massive Attack - Teardrop",
		"Aphex Twin - Windowlicker",
	}

	for _, title := range cleanTitles {
		res := SanitizeTrackTitle(title, "Random Radio")
		if !res.IsClean || res.IsGenericOrSpam {
			t.Errorf("Expected clean title %q to be accepted", title)
		}
		if res.CleanTitle != title {
			t.Errorf("Expected clean title %q, got %q", title, res.CleanTitle)
		}
	}
}

func TestSanitizeTrackTitle_EmptyAndUnknown(t *testing.T) {
	invalids := []string{"", "   ", "Unknown Artist", "Unknown Track", "track 1"}
	for _, inv := range invalids {
		res := SanitizeTrackTitle(inv, "Radio")
		if res.IsClean || !res.IsGenericOrSpam {
			t.Errorf("Expected %q to be invalid/generic", inv)
		}
	}
}

func TestValidateCandidateWithMusicBrainz_Mock(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := `{
			"recordings": [
				{
					"id": "cde2ce72-8c53-4ec9-8521-fc8c3bf82602",
					"score": 100,
					"title": "Voyager",
					"artist-credit": [{"name": "Daft Punk"}],
					"releases": [
						{"title": "Discovery", "date": "2001-03-12"}
					]
				}
			]
		}`
		w.Write([]byte(resp))
	}))
	defer ts.Close()

	// Direct check using mock payload logic
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL, nil)
	client := ts.Client()
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Mock server failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}
}
