package radio

import (
	"math"
	"strings"
	"testing"
)

func TestGeocodeStation(t *testing.T) {
	tests := []struct {
		name         string
		station      Station
		expectedCity string
		expectedLat  float64
		expectedLon  float64
		expectedOk   bool
	}{
		{
			name: "City match Tokyo",
			station: Station{
				ID:      "tokyo-fm",
				Name:    "Tokyo FM",
				Country: "JP",
				City:    "Tokyo",
			},
			expectedCity: "Tokyo",
			expectedLat:  35.6762,
			expectedLon:  139.6503,
			expectedOk:   true,
		},
		{
			name: "Name contains London",
			station: Station{
				ID:      "bbc-london",
				Name:    "BBC Radio London 94.9",
				Country: "GB",
			},
			expectedCity: "London",
			expectedLat:  51.5074,
			expectedLon:  -0.1278,
			expectedOk:   true,
		},
		{
			name: "Country fallback Germany",
			station: Station{
				ID:      "fluxfm",
				Name:    "FluxFM",
				Country: "DE",
			},
			expectedCity: "Germany",
			expectedLat:  52.5200,
			expectedLon:  13.4050,
			expectedOk:   true,
		},
		{
			name: "Unknown country",
			station: Station{
				ID:      "mystery-fm",
				Name:    "Mystery FM",
				Country: "ZZ",
			},
			expectedCity: "Global",
			expectedLat:  0.0,
			expectedLon:  0.0,
			expectedOk:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lat, lon, city, ok := GeocodeStation(tc.station)
			if ok != tc.expectedOk {
				t.Errorf("expected ok=%v, got %v", tc.expectedOk, ok)
			}
			if math.Abs(lat-tc.expectedLat) > 0.001 {
				t.Errorf("expected lat %f, got %f", tc.expectedLat, lat)
			}
			if math.Abs(lon-tc.expectedLon) > 0.001 {
				t.Errorf("expected lon %f, got %f", tc.expectedLon, lon)
			}
			if city != tc.expectedCity {
				t.Errorf("expected city %q, got %q", tc.expectedCity, city)
			}
		})
	}
}

func TestHaversineDistance(t *testing.T) {
	// London to Paris: ~343 km
	londonLat, londonLon := 51.5074, -0.1278
	parisLat, parisLon := 48.8566, 2.3522

	dist := HaversineDistance(londonLat, londonLon, parisLat, parisLon)
	if dist < 330 || dist > 360 {
		t.Errorf("expected distance London-Paris ~343 km, got %f", dist)
	}

	// Same point distance should be 0
	zeroDist := HaversineDistance(londonLat, londonLon, londonLat, londonLon)
	if zeroDist > 0.001 {
		t.Errorf("expected distance 0, got %f", zeroDist)
	}
}

func TestBuildStationClustersAndFindNearest(t *testing.T) {
	stations := []Station{
		{ID: "s1", Name: "KEXP Seattle", City: "Seattle", Country: "US"},
		{ID: "s2", Name: "Seattle Wave", City: "Seattle", Country: "US"},
		{ID: "s3", Name: "BBC London", City: "London", Country: "GB"},
		{ID: "s4", Name: "Capital FM", City: "London", Country: "GB"},
		{ID: "s5", Name: "Tokyo FM", City: "Tokyo", Country: "JP"},
	}

	clusters := BuildStationClusters(stations)
	if len(clusters) != 3 {
		t.Fatalf("expected 3 distinct clusters (Seattle, London, Tokyo), got %d", len(clusters))
	}

	// Find nearest to London coordinates (51.5, -0.1)
	nearest, idx, dist := FindNearestCluster(clusters, 51.5, -0.1)
	if nearest == nil || idx < 0 {
		t.Fatalf("expected to find nearest cluster")
	}
	if nearest.City != "London" {
		t.Errorf("expected nearest cluster to be London, got %s", nearest.City)
	}
	if len(nearest.Stations) != 2 {
		t.Errorf("expected 2 stations in London cluster, got %d", len(nearest.Stations))
	}
	if dist > 20.0 {
		t.Errorf("expected distance to London center < 20km, got %f", dist)
	}

	// Test empty clusters
	emptyNearest, emptyIdx, _ := FindNearestCluster(nil, 0, 0)
	if emptyNearest != nil || emptyIdx != -1 {
		t.Errorf("expected nil for empty clusters")
	}
}

func TestExtractOrAssignFrequency(t *testing.T) {
	// Explicit FM frequency in Frequency field
	st1 := Station{ID: "kexp", Name: "KEXP", Frequency: "90.3 FM"}
	freq1 := ExtractOrAssignFrequency(st1, "FM")
	if math.Abs(freq1-90.3) > 0.01 {
		t.Errorf("expected 90.3 FM, got %f", freq1)
	}

	// Explicit AM frequency in Name field
	st2 := Station{ID: "am720", Name: "WGN 720 AM", Broadcast: "AM"}
	freq2 := ExtractOrAssignFrequency(st2, "AM")
	if math.Abs(freq2-720.0) > 0.1 {
		t.Errorf("expected 720 AM, got %f", freq2)
	}

	// Fallback deterministic FM slot
	st3 := Station{ID: "drone-zone", Name: "SomaFM Drone Zone"}
	freq3 := ExtractOrAssignFrequency(st3, "FM")
	if freq3 < 87.5 || freq3 > 108.0 {
		t.Errorf("expected frequency in FM band 87.5-108.0, got %f", freq3)
	}

	// Fallback deterministic AM slot
	freqAM := ExtractOrAssignFrequency(st3, "AM")
	if freqAM < 530.0 || freqAM > 1710.0 {
		t.Errorf("expected frequency in AM band 530-1710, got %f", freqAM)
	}

	// Explicit SW frequency
	stSW := Station{ID: "sw1", Name: "Radio Free Europe 15.2 MHz", Frequency: "15.2 SW"}
	freqSW := ExtractOrAssignFrequency(stSW, "SW")
	if math.Abs(freqSW-15.2) > 0.01 {
		t.Errorf("expected 15.2 SW, got %f", freqSW)
	}

	// Deterministic SW slot
	freqSWFallback := ExtractOrAssignFrequency(st3, "SW")
	if freqSWFallback < 3.2 || freqSWFallback > 22.0 {
		t.Errorf("expected frequency in SW band 3.2-22.0, got %f", freqSWFallback)
	}

	// Deterministic slot must be consistent
	freq3Repeat := ExtractOrAssignFrequency(st3, "FM")
	if freq3 != freq3Repeat {
		t.Errorf("expected deterministic frequency to match on repeat, got %f vs %f", freq3, freq3Repeat)
	}
}

func TestLookupNearestLocation(t *testing.T) {
	// Paris, France (48.8, 2.3)
	code, country, city, flag, dist := LookupNearestLocation(48.8566, 2.3522)
	if code != "FR" || flag != "🇫🇷" {
		t.Errorf("expected FR and France flag for Paris coords, got %s / %s", code, flag)
	}
	if !strings.Contains(city, "Paris") && !strings.Contains(country, "France") {
		t.Errorf("expected Paris/France, got %s, %s", city, country)
	}
	if dist > 10.0 {
		t.Errorf("expected dist < 10km, got %f", dist)
	}

	// Tokyo, Japan (35.6, 139.6)
	codeJP, countryJP, _, flagJP, _ := LookupNearestLocation(35.6762, 139.6503)
	if codeJP != "JP" || countryJP != "Japan" || flagJP != "🇯🇵" {
		t.Errorf("expected JP/Japan for Tokyo coords, got %s / %s", codeJP, countryJP)
	}
}
