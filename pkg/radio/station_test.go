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

func TestCountryNameAndCode(t *testing.T) {
	if got := CountryNameToCode("ireland"); got != "IE" {
		t.Errorf("CountryNameToCode('ireland') = %q, want 'IE'", got)
	}
	if got := CountryNameToCode("United Kingdom"); got != "GB" {
		t.Errorf("CountryNameToCode('United Kingdom') = %q, want 'GB'", got)
	}
	if got := CountryNameToCode("DE"); got != "DE" {
		t.Errorf("CountryNameToCode('DE') = %q, want 'DE'", got)
	}

	st := Station{
		Country: "IE",
		City:    "Dublin",
	}
	if st.CountryName() != "Ireland" {
		t.Errorf("st.CountryName() = %q, want 'Ireland'", st.CountryName())
	}
	if !strings.Contains(st.LocationString(), "Dublin") {
		t.Errorf("st.LocationString() = %q, want to contain 'Dublin'", st.LocationString())
	}
}

func TestStationBroadcastAndTerrestrial(t *testing.T) {
	// 1. FM terrestrial station
	stFM := Station{
		Name:      "RTÉ Radio 1",
		Country:   "IE",
		City:      "Dublin",
		Broadcast: "FM/DAB",
		Frequency: "88.5 FM",
	}
	if !stFM.IsTerrestrial() {
		t.Errorf("Expected RTÉ Radio 1 to be terrestrial")
	}
	if stFM.BroadcastType() != "FM/DAB" {
		t.Errorf("Expected BroadcastType 'FM/DAB', got %q", stFM.BroadcastType())
	}
	if !strings.Contains(stFM.BroadcastBadge(), "88.5 FM") {
		t.Errorf("Expected badge with frequency, got %q", stFM.BroadcastBadge())
	}
	if !strings.Contains(stFM.DisplayLocationAndBand(), "Dublin") || !strings.Contains(stFM.DisplayLocationAndBand(), "88.5 FM") {
		t.Errorf("Expected DisplayLocationAndBand to contain Dublin and 88.5 FM, got %q", stFM.DisplayLocationAndBand())
	}

	// 2. DAB terrestrial station
	stDAB := Station{
		Name:      "RTÉ Gold",
		Country:   "IE",
		Broadcast: "DAB",
		Frequency: "DAB+",
	}
	if !stDAB.IsTerrestrial() {
		t.Errorf("Expected RTÉ Gold to be terrestrial")
	}
	if stDAB.ShortBroadcastBadge() != "📡 DAB" {
		t.Errorf("Expected ShortBroadcastBadge '📡 DAB', got %q", stDAB.ShortBroadcastBadge())
	}

	// 3. Online-only station
	stOnline := Station{
		Name:      "SomaFM: Groove Salad",
		Country:   "US",
		Broadcast: "Online",
	}
	if stOnline.IsTerrestrial() {
		t.Errorf("Expected SomaFM to NOT be terrestrial")
	}
	if stOnline.BroadcastType() != "Online" {
		t.Errorf("Expected BroadcastType 'Online', got %q", stOnline.BroadcastType())
	}
	if stOnline.ShortBroadcastBadge() != "🌐 Web" {
		t.Errorf("Expected ShortBroadcastBadge '🌐 Web', got %q", stOnline.ShortBroadcastBadge())
	}
	if stOnline.BroadcastBadge() != "🌐 Online" {
		t.Errorf("Expected BroadcastBadge '🌐 Online', got %q", stOnline.BroadcastBadge())
	}

	// 4. Untagged station with "FM" in name must NOT be assumed terrestrial (strictly defaults to Online)
	stUntagged := Station{
		Name:    "Cyberpunk FM Internet Stream",
		Country: "DE",
	}
	if stUntagged.IsTerrestrial() {
		t.Errorf("Expected untagged station to strictly NOT be terrestrial, even with 'FM' in name")
	}
	if stUntagged.BroadcastType() != "Online" {
		t.Errorf("Expected BroadcastType to default to 'Online', got %q", stUntagged.BroadcastType())
	}
}
