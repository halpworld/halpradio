package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

var sampleCatalogYAML = []byte(`
stations:
  - id: "somafm_groovesalad"
    name: "SomaFM Groove Salad"
    url: "https://ice1.somafm.com/groovesalad-128-mp3"
    genre: "Ambient / Chillout"
    country: "US"
    bitrate: 128
    codec: "MP3"
    activities: ["coding", "relax"]
  - id: "lofigirl"
    name: "Lofi Girl"
    url: "https://play.streamafrica.net/lofigirl"
    genre: "Lofi / Chill Beats"
    country: "FR"
    bitrate: 128
    codec: "MP3"
    activities: ["study", "focus"]
  - id: "bbc_radio1"
    name: "BBC Radio 1"
    url: "https://stream.live.vc.bbcmedia.co.uk/bbc_radio_one"
    genre: "Pop / Top 40"
    country: "GB"
    bitrate: 128
    codec: "AAC"
`)

func TestRunStationsHelp(t *testing.T) {
	var buf bytes.Buffer
	done, err := RunStations([]string{"help"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations help failed: %v", err)
	}
	if !strings.Contains(buf.String(), "halpradio stations") || !strings.Contains(buf.String(), "Usage:") {
		t.Errorf("Expected stations help text, got: %s", buf.String())
	}
}

func TestRunStationsList(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	// 1. Default table output
	var buf bytes.Buffer
	done, err := RunStations([]string{"list"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations list failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "SomaFM Groove Salad") || !strings.Contains(out, "Lofi Girl") {
		t.Errorf("Expected stations in list output, got: %s", out)
	}

	// 2. Filter by genre
	buf.Reset()
	done, err = RunStations([]string{"list", "--genre", "Ambient"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations list genre filter failed: %v", err)
	}
	if !strings.Contains(buf.String(), "SomaFM Groove Salad") || strings.Contains(buf.String(), "BBC Radio 1") {
		t.Errorf("Expected only ambient station, got: %s", buf.String())
	}

	// 3. Filter by country
	buf.Reset()
	done, err = RunStations([]string{"list", "-c", "GB"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations list country filter failed: %v", err)
	}
	if !strings.Contains(buf.String(), "BBC Radio 1") || strings.Contains(buf.String(), "Lofi Girl") {
		t.Errorf("Expected only GB station, got: %s", buf.String())
	}

	// 4. Filter by limit
	buf.Reset()
	done, err = RunStations([]string{"list", "-n", "1"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations list limit failed: %v", err)
	}
	if !strings.Contains(buf.String(), "(1 stations)") {
		t.Errorf("Expected limit of 1 station, got: %s", buf.String())
	}

	// 5. Plain TSV output
	buf.Reset()
	done, err = RunStations([]string{"list", "--plain"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations list plain failed: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) < 3 {
		t.Errorf("Expected at least 3 TSV lines, got %d", len(lines))
	}
	parts := strings.Split(lines[0], "\t")
	if len(parts) < 7 || parts[1] != "somafm_groovesalad" {
		t.Errorf("Unexpected TSV format: %q", lines[0])
	}

	// 6. JSON output
	buf.Reset()
	done, err = RunStations([]string{"list", "--json"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations list json failed: %v", err)
	}
	var entries []StationJSONEntry
	if err := json.Unmarshal(buf.Bytes(), &entries); err != nil {
		t.Fatalf("Failed to parse JSON output: %v", err)
	}
	if len(entries) < 3 || entries[0].ID != "somafm_groovesalad" {
		t.Errorf("Unexpected JSON entries count or ID: %+v", entries)
	}
}

func TestRunStationsSearch(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	var buf bytes.Buffer
	done, err := RunStations([]string{"search", "lofi"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations search failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Lofi Girl") || strings.Contains(buf.String(), "BBC Radio 1") {
		t.Errorf("Expected search match for 'lofi', got: %s", buf.String())
	}

	// Search with no matches
	buf.Reset()
	done, err = RunStations([]string{"search", "nonexistentstationxyz"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations search empty failed: %v", err)
	}
	if !strings.Contains(buf.String(), "No stations found") {
		t.Errorf("Expected no stations found message, got: %s", buf.String())
	}
}

func TestRunStationsFavorites(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	var buf bytes.Buffer

	// 1. Initial list (empty)
	done, err := RunStations([]string{"fav", "list"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations fav list failed: %v", err)
	}
	if !strings.Contains(buf.String(), "No favorite stations yet") {
		t.Errorf("Expected empty favorites message, got: %s", buf.String())
	}

	// 2. Add to favorites
	buf.Reset()
	done, err = RunStations([]string{"fav", "add", "somafm_groovesalad"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations fav add failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Added") {
		t.Errorf("Expected added favorite confirmation, got: %s", buf.String())
	}

	// 3. List favorites (should contain Groove Salad)
	buf.Reset()
	done, err = RunStations([]string{"fav", "list"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations fav list failed: %v", err)
	}
	if !strings.Contains(buf.String(), "SomaFM Groove Salad") {
		t.Errorf("Expected Groove Salad in favorites list, got: %s", buf.String())
	}

	// 4. Toggle favorite (removes it)
	buf.Reset()
	done, err = RunStations([]string{"fav", "toggle", "somafm_groovesalad"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations fav toggle failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Removed") {
		t.Errorf("Expected removed favorite confirmation, got: %s", buf.String())
	}

	// 5. Remove station that is not in favorites
	buf.Reset()
	done, err = RunStations([]string{"fav", "remove", "somafm_groovesalad"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations fav remove when not in favs failed: %v", err)
	}
	if !strings.Contains(buf.String(), "not in favorites") {
		t.Errorf("Expected 'not in favorites' message, got: %s", buf.String())
	}

	// 6. Add station then add again (duplicate add)
	buf.Reset()
	_, _ = RunStations([]string{"fav", "add", "somafm_groovesalad"}, sampleCatalogYAML, &buf)
	buf.Reset()
	done, err = RunStations([]string{"fav", "add", "somafm_groovesalad"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations duplicate fav add failed: %v", err)
	}
	if !strings.Contains(buf.String(), "already in favorites") {
		t.Errorf("Expected 'already in favorites' message, got: %s", buf.String())
	}

	// 7. Explicit fav remove
	buf.Reset()
	done, err = RunStations([]string{"fav", "remove", "somafm_groovesalad"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations fav remove failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Removed") {
		t.Errorf("Expected removed message, got: %s", buf.String())
	}

	// 8. Unknown fav action
	buf.Reset()
	done, err = RunStations([]string{"fav", "unknown_action", "somafm_groovesalad"}, sampleCatalogYAML, &buf)
	if err == nil || done {
		t.Errorf("Expected error for unknown fav action")
	}

	// 9. Fav without ID argument
	buf.Reset()
	done, err = RunStations([]string{"fav", "add"}, sampleCatalogYAML, &buf)
	if err == nil || done {
		t.Errorf("Expected error for fav add without ID")
	}

	// 10. Fav with unknown ID
	buf.Reset()
	done, err = RunStations([]string{"fav", "add", "unknown-station-id-xyz"}, sampleCatalogYAML, &buf)
	if err == nil || done {
		t.Errorf("Expected error for unknown station ID")
	}
}

func TestRunStationsAddCustom(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	var buf bytes.Buffer

	// 1. Missing required flags
	done, err := RunStations([]string{"add", "--name", "Test Station"}, sampleCatalogYAML, &buf)
	if err == nil || done {
		t.Errorf("Expected error when adding station without --url")
	}

	// 2. Add valid station with explicit ID
	buf.Reset()
	done, err = RunStations([]string{
		"add",
		"--name", "My Synthwave Station",
		"--url", "https://stream.synthwave.example/live.mp3",
		"--genre", "Synthwave",
		"--country", "US",
		"--id", "custom-synthwave",
	}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations add failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Successfully added") {
		t.Errorf("Expected success confirmation, got: %s", buf.String())
	}

	// 3. Add valid station without ID (auto-generated ID)
	buf.Reset()
	done, err = RunStations([]string{
		"add",
		"--name", "Autogenerated ID Station",
		"--url", "https://stream.auto.example/live.mp3",
	}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations add auto ID failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Successfully added") {
		t.Errorf("Expected success confirmation for auto ID, got: %s", buf.String())
	}

	// 4. Verify custom station appears in list
	buf.Reset()
	done, err = RunStations([]string{"list", "--genre", "Synthwave"}, sampleCatalogYAML, &buf)
	if err != nil || !done {
		t.Fatalf("RunStations list after add failed: %v", err)
	}
	if !strings.Contains(buf.String(), "My Synthwave Station") {
		t.Errorf("Expected custom station in catalog list, got: %s", buf.String())
	}
}

func TestSetupAppStationsRouting(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	var buf bytes.Buffer
	appInst, isDone, err := SetupApp([]string{"stations", "list", "--limit", "2"}, sampleCatalogYAML, &buf)
	if err != nil {
		t.Fatalf("SetupApp stations list failed: %v", err)
	}
	if !isDone {
		t.Errorf("Expected isDone true for stations subcommand")
	}
	if appInst != nil {
		t.Errorf("Expected nil appInst for CLI subcommand")
	}
	if !strings.Contains(buf.String(), "STATION CATALOG") {
		t.Errorf("Expected station catalog table in output, got: %s", buf.String())
	}

	// Test alias: `halpradio search <query>`
	buf.Reset()
	_, isDone, err = SetupApp([]string{"search", "lofi"}, sampleCatalogYAML, &buf)
	if err != nil || !isDone {
		t.Fatalf("SetupApp search alias failed: %v", err)
	}
	if !strings.Contains(buf.String(), "Lofi Girl") {
		t.Errorf("Expected search results for 'lofi', got: %s", buf.String())
	}
}
