package radio

import (
	"testing"
)

func TestLookupVerifiedBroadcaster(t *testing.T) {
	tests := []struct {
		url          string
		name         string
		country      string
		wantOK       bool
		wantBroadcast string
		wantFreq     string
		wantCity     string
	}{
		{
			url:          "http://icecast.rte.ie/radio1",
			name:         "RTÉ Radio 1",
			country:      "IE",
			wantOK:       true,
			wantBroadcast: "FM/DAB",
			wantFreq:     "88.5 FM",
			wantCity:     "Dublin",
		},
		{
			url:          "https://liveaudio.rte.ie/hls-radio/radio1/chunklist.m3u8",
			name:         "RTE 1",
			country:      "IE",
			wantOK:       true,
			wantBroadcast: "FM/DAB",
			wantFreq:     "88.5 FM",
			wantCity:     "Dublin",
		},
		{
			url:          "http://icecast.rte.ie/2fm",
			name:         "RTÉ 2FM",
			country:      "IE",
			wantOK:       true,
			wantBroadcast: "FM/DAB",
			wantFreq:     "90.7 FM",
			wantCity:     "Dublin",
		},
		{
			url:          "http://icecast.rte.ie/gold",
			name:         "RTÉ Gold",
			country:      "IE",
			wantOK:       true,
			wantBroadcast: "DAB",
			wantFreq:     "DAB+",
			wantCity:     "Dublin",
		},
		{
			url:          "https://stream.audioxi.com/FM104",
			name:         "FM104",
			country:      "IE",
			wantOK:       true,
			wantBroadcast: "FM",
			wantFreq:     "104.4 FM",
			wantCity:     "Dublin",
		},
		{
			url:          "http://bbcmedia.ic.llnwd.net/stream/bbcmedia_radio1_mf_p",
			name:         "BBC Radio 1",
			country:      "GB",
			wantOK:       true,
			wantBroadcast: "FM/DAB",
			wantFreq:     "97-99 FM",
			wantCity:     "London",
		},
		{
			url:          "https://ice1.somafm.com/groovesalad-128-mp3",
			name:         "SomaFM: Groove Salad",
			country:      "US",
			wantOK:       false,
		},
	}

	for _, tt := range tests {
		info, ok := LookupVerifiedBroadcaster(tt.url, tt.name, tt.country)
		if ok != tt.wantOK {
			t.Errorf("LookupVerifiedBroadcaster(%q, %q, %q) ok = %v, want %v", tt.url, tt.name, tt.country, ok, tt.wantOK)
			continue
		}
		if ok {
			if info.Broadcast != tt.wantBroadcast {
				t.Errorf("Broadcast = %q, want %q", info.Broadcast, tt.wantBroadcast)
			}
			if info.Frequency != tt.wantFreq {
				t.Errorf("Frequency = %q, want %q", info.Frequency, tt.wantFreq)
			}
			if info.City != tt.wantCity {
				t.Errorf("City = %q, want %q", info.City, tt.wantCity)
			}
		}
	}
}

func TestEnrichStation(t *testing.T) {
	// 1. Raw station from RadioBrowser with no broadcast tags
	rawRTE := Station{
		ID:        "rb-12345",
		Name:      "RTÉ Radio 1",
		URL:       "http://icecast.rte.ie/radio1",
		Country:   "IE",
		Broadcast: "Online",
	}

	enriched := EnrichStation(rawRTE)
	if !enriched.IsTerrestrial() {
		t.Errorf("Expected enriched RTÉ Radio 1 to be terrestrial")
	}
	if enriched.Broadcast != "FM/DAB" {
		t.Errorf("Expected Broadcast 'FM/DAB', got %q", enriched.Broadcast)
	}
	if enriched.Frequency != "88.5 FM" {
		t.Errorf("Expected Frequency '88.5 FM', got %q", enriched.Frequency)
	}
	if enriched.City != "Dublin" {
		t.Errorf("Expected City 'Dublin', got %q", enriched.City)
	}

	// 2. Online only station
	rawSoma := Station{
		ID:        "soma-gs",
		Name:      "SomaFM: Groove Salad",
		URL:       "https://ice1.somafm.com/groovesalad-128-mp3",
		Country:   "US",
		Broadcast: "Online",
	}
	enrichedSoma := EnrichStation(rawSoma)
	if enrichedSoma.IsTerrestrial() {
		t.Errorf("Expected SomaFM to remain non-terrestrial")
	}
	if enrichedSoma.Broadcast != "Online" {
		t.Errorf("Expected Broadcast 'Online', got %q", enrichedSoma.Broadcast)
	}
}
