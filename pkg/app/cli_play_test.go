package app

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/radio"
)

func TestRunPlayHelp(t *testing.T) {
	var buf bytes.Buffer
	done, err := RunPlay([]string{"help"}, nil, &buf)
	if err != nil || !done {
		t.Fatalf("RunPlay help failed: %v", err)
	}
	if !strings.Contains(buf.String(), "halpradio play - Stream internet radio") {
		t.Errorf("Expected play help text, got: %s", buf.String())
	}
}

func TestResolveStation(t *testing.T) {
	stations := []radio.Station{
		{ID: "somafm_groovesalad", Name: "SomaFM Groove Salad", URL: "http://example.com/groove", Genre: "Ambient"},
		{ID: "lofigirl", Name: "Lofi Girl", URL: "http://example.com/lofi", Genre: "Lofi"},
		{ID: "bbc_1", Name: "BBC Radio 1", URL: "http://example.com/bbc1", Genre: "Pop"},
	}

	// 1. Direct URL
	st, err := resolveStation("https://stream.example.com/audio.mp3", false, "", stations)
	if err != nil || st.URL != "https://stream.example.com/audio.mp3" {
		t.Fatalf("Direct URL resolution failed: %v", err)
	}

	// 2. Index resolution (1-based: "2" -> Lofi Girl)
	st, err = resolveStation("2", false, "", stations)
	if err != nil || st.ID != "lofigirl" {
		t.Fatalf("Index resolution failed: expected lofigirl, got %v (err: %v)", st.ID, err)
	}

	// 3. Exact ID match
	st, err = resolveStation("somafm_groovesalad", false, "", stations)
	if err != nil || st.ID != "somafm_groovesalad" {
		t.Fatalf("ID resolution failed: expected somafm_groovesalad, got %v", st.ID)
	}

	// 4. Substring name match
	st, err = resolveStation("bbc", false, "", stations)
	if err != nil || st.ID != "bbc_1" {
		t.Fatalf("Substring resolution failed: expected bbc_1, got %v", st.ID)
	}

	// 5. Random station
	st, err = resolveStation("", true, "", stations)
	if err != nil || st.ID == "" {
		t.Fatalf("Random resolution failed: %v", err)
	}

	// 6. Random station with genre filter
	st, err = resolveStation("", true, "Lofi", stations)
	if err != nil || st.ID != "lofigirl" {
		t.Fatalf("Random with genre failed: expected lofigirl, got %v", st.ID)
	}

	// 7. Unknown station
	_, err = resolveStation("nonexistent_station_123", false, "", stations)
	if err == nil {
		t.Errorf("Expected error for nonexistent station")
	}
}

func TestRunPlayWithDuration(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{0xFF, 0xFB, 0x90, 0x64}) // dummy MP3 header
	}))
	defer ts.Close()

	var buf bytes.Buffer
	start := time.Now()
	done, err := RunPlay([]string{
		ts.URL,
		"--duration", "150ms",
		"--volume", "50",
	}, nil, &buf)
	elapsed := time.Since(start)

	if err != nil || !done {
		t.Fatalf("RunPlay with duration failed: %v", err)
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("Expected playback to run for ~150ms, elapsed %v", elapsed)
	}
	if !strings.Contains(buf.String(), "Streaming:") {
		t.Errorf("Expected streaming info in output, got: %s", buf.String())
	}
	if !strings.Contains(buf.String(), "Stopped streaming") {
		t.Errorf("Expected stopped streaming message, got: %s", buf.String())
	}
}

func TestRunPlayRemoteFlag(t *testing.T) {
	sockPath := desktop.GetDefaultSocketPath()

	playCalled := false
	server, err := desktop.StartIPCServer(sockPath, func(action desktop.MediaAction) (*desktop.PlaybackInfo, error) {
		if action == desktop.ActionPlay {
			playCalled = true
		}
		return &desktop.PlaybackInfo{Status: "playing"}, nil
	})
	if err != nil {
		t.Fatalf("Failed to start IPC test server: %v", err)
	}
	defer server.Close()

	var buf bytes.Buffer
	done, err := RunPlay([]string{"--remote"}, nil, &buf)
	if err != nil || !done {
		t.Fatalf("RunPlay --remote failed: %v", err)
	}
	if !playCalled {
		t.Errorf("Expected ActionPlay to be triggered on remote server")
	}
	if !strings.Contains(buf.String(), "Sent play command") {
		t.Errorf("Expected confirmation output, got: %s", buf.String())
	}
}

func TestRunPlayJSONAndVolumeBounds(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte{0xFF, 0xFB, 0x90, 0x64})
	}))
	defer ts.Close()

	var buf bytes.Buffer
	// Test --json flag and volume clamping (150 -> 100)
	done, err := RunPlay([]string{
		ts.URL,
		"--duration", "100ms",
		"--volume", "150",
		"--json",
	}, nil, &buf)
	if err != nil || !done {
		t.Fatalf("RunPlay --json failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "playback_started") || !strings.Contains(out, "100") {
		t.Errorf("Expected JSON playback event, got: %s", out)
	}

	// Test invalid duration format
	buf.Reset()
	done, err = RunPlay([]string{
		ts.URL,
		"--duration", "invalid_duration",
	}, nil, &buf)
	if err == nil || done {
		t.Errorf("Expected error for invalid duration string")
	}
}

func TestResolveStationEdgeCases(t *testing.T) {
	stations := []radio.Station{
		{ID: "ambient_1", Name: "Deep Ambient Radio", URL: "http://example.com/ambient", Genre: "Ambient"},
		{ID: "lofi_1", Name: "Chill Lofi", URL: "http://example.com/lofi", Genre: "Lofi"},
	}

	// 1. Empty target defaults to first station
	st, err := resolveStation("", false, "", stations)
	if err != nil || st.ID != "ambient_1" {
		t.Errorf("Expected first station on empty target, got %v (err: %v)", st.ID, err)
	}

	// 2. Fallback to genre query match
	st, err = resolveStation("Ambient", false, "", stations)
	if err != nil || st.ID != "ambient_1" {
		t.Errorf("Expected genre match fallback, got %v (err: %v)", st.ID, err)
	}

	// 3. Random with non-existent genre
	_, err = resolveStation("", true, "NonExistentGenre123", stations)
	if err == nil {
		t.Errorf("Expected error for random with non-matching genre")
	}

	// 4. Empty slice with empty target
	_, err = resolveStation("", false, "", nil)
	if err == nil {
		t.Errorf("Expected error for empty stations slice")
	}
}

func TestRunPlayEmptyCatalogError(t *testing.T) {
	var buf bytes.Buffer
	done, err := RunPlay([]string{"unknown_station"}, nil, &buf)
	if err == nil || done {
		t.Errorf("Expected error for empty catalog")
	}
}
