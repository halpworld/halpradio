package player

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/halpworld/halpradio/pkg/radio"
)

func TestPlayerVolumeClamping(t *testing.T) {
	pm := NewManager("auto", 80, nil)

	if pm.Volume() != 80 {
		t.Errorf("Expected initial volume 80, got %d", pm.Volume())
	}

	vol := pm.SetVolume(120)
	if vol != 100 || pm.Volume() != 100 {
		t.Errorf("Expected volume clamped to 100, got %d", vol)
	}

	volMin := pm.SetVolume(-10)
	if volMin != 0 || pm.Volume() != 0 {
		t.Errorf("Expected volume clamped to 0, got %d", volMin)
	}
}

func TestPlayerToggleMute(t *testing.T) {
	pm := NewManager("auto", 75, nil)

	muted := pm.ToggleMute()
	if !muted || pm.Volume() != 0 {
		t.Errorf("Expected muted state with 0 effective volume")
	}

	unmuted := pm.ToggleMute()
	if unmuted || pm.Volume() != 75 {
		t.Errorf("Expected unmuted state with volume restored to 75")
	}
}

func TestBackendDetection(t *testing.T) {
	backend := detectBackend("native")
	if backend != "native" {
		t.Errorf("Expected native backend when preferred is 'native', got '%s'", backend)
	}

	goBackend := detectBackend("go")
	if goBackend != "native" {
		t.Errorf("Expected native backend when preferred is 'go', got '%s'", goBackend)
	}

	autoBackend := detectBackend("auto")
	if autoBackend == "" {
		t.Errorf("Expected auto backend detection to yield a valid backend name")
	}
}

func TestPlayerDefaultsAndState(t *testing.T) {
	pm := NewManager("", -5, nil)
	if pm.Volume() != 80 {
		t.Errorf("Expected default volume fallback 80, got %d", pm.Volume())
	}
	if pm.Status() != StatusStopped {
		t.Errorf("Expected initial status StatusStopped, got %s", pm.Status())
	}
	if pm.CurrentStation() != nil {
		t.Errorf("Expected nil station initially")
	}
	if pm.CurrentTrack() != "" {
		t.Errorf("Expected empty track initially")
	}
	if pm.Error() != "" {
		t.Errorf("Expected empty error initially")
	}
	if pm.IsMuted() {
		t.Errorf("Expected not muted initially")
	}
	if pm.ActiveBackend() == "" {
		t.Errorf("Expected non-empty backend")
	}

	// Test Stop on stopped manager
	if err := pm.Stop(); err != nil {
		t.Errorf("Stop failed: %v", err)
	}

	// Test Pause on stopped manager
	if err := pm.Pause(); err != nil {
		t.Errorf("Pause failed: %v", err)
	}

	// Test Resume when no current station
	if err := pm.Resume(); err != nil {
		t.Errorf("Resume failed: %v", err)
	}
}

func TestPlayerErrorHandling(t *testing.T) {
	pm := NewManager("native", 80, nil)

	// Attempt playing invalid stream URL
	invalidStation := radio.Station{
		ID:   "test-invalid",
		Name: "Invalid Station",
		URL:  "http://127.0.0.1:59999/nonexistent",
	}

	_ = pm.Play(invalidStation)
	time.Sleep(500 * time.Millisecond)

	if pm.Status() != StatusError {
		t.Errorf("Expected status ERROR for unreachable stream, got %s", pm.Status())
	}
	if pm.Error() == "" {
		t.Errorf("Expected non-empty lastError for unreachable stream")
	}
}

func TestIsValidStreamURL(t *testing.T) {
	validURLs := []string{
		"http://stream.example.com/live.mp3",
		"https://ice1.somafm.com/groovesalad-128-mp3",
		"http://192.168.1.100:8000/stream",
	}
	for _, u := range validURLs {
		if !IsValidStreamURL(u) {
			t.Errorf("Expected %q to be valid stream URL", u)
		}
	}

	invalidURLs := []string{
		"",
		"   ",
		"--script=/tmp/evil.lua",
		"-I dummy",
		"file:///etc/passwd",
		"ftp://evil.com/stream",
		"gopher://evil.com",
		"http://",
		"https://",
	}
	for _, u := range invalidURLs {
		if IsValidStreamURL(u) {
			t.Errorf("Expected %q to be invalid stream URL", u)
		}
	}
}

func TestSanitizeTrackTitle(t *testing.T) {
	input := "\x1b[31mRed Title\x1b[0m\x00\r\n - Artist"
	clean := sanitizeTrackTitle(input)
	if clean != "[31mRed Title[0m - Artist" {
		t.Errorf("Expected sanitized track title, got %q", clean)
	}
}

func TestPlayRejectsMaliciousURL(t *testing.T) {
	pm := NewManager("mpv", 80, nil)
	malicious := radio.Station{
		ID:   "exploit",
		Name: "Exploit Station",
		URL:  "--script=/tmp/pwn.lua",
	}
	_ = pm.Play(malicious)
	if pm.Status() != StatusError {
		t.Errorf("Expected StatusError when attempting to play malicious argument URL, got %s", pm.Status())
	}
}

func TestStartICYListener(t *testing.T) {
	metaInt := 16
	metaBlock := make([]byte, 48)
	copy(metaBlock, []byte("StreamTitle='Synth Artist - Neon Dreams';"))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Icy-Metaint", fmt.Sprintf("%d", metaInt))
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		audioChunk := make([]byte, metaInt)
		for i := 0; i < metaInt; i++ {
			audioChunk[i] = 0xAA
		}

		// Write 1 chunk of audio
		_, _ = w.Write(audioChunk)
		// Write metadata length (3 blocks of 16 bytes = 48 bytes)
		_, _ = w.Write([]byte{3})
		// Write metadata block
		_, _ = w.Write(metaBlock)
		if flusher != nil {
			flusher.Flush()
		}
		// Write another audio chunk
		_, _ = w.Write(audioChunk)
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer ts.Close()

	var receivedTrack TrackInfo
	trackCh := make(chan TrackInfo, 1)

	pm := NewManager("native", 80, func(info TrackInfo) {
		trackCh <- info
	})

	st := radio.Station{
		ID:   "test-icy",
		Name: "Test ICY Station",
		URL:  ts.URL,
	}

	pm.mu.Lock()
	pm.status = StatusPlaying
	pm.currentStation = &st
	pm.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go pm.startICYListener(ctx, st)

	select {
	case receivedTrack = <-trackCh:
		if receivedTrack.TrackTitle != "Synth Artist - Neon Dreams" {
			t.Errorf("Track title mismatch: got %q, want %q", receivedTrack.TrackTitle, "Synth Artist - Neon Dreams")
		}
		if pm.CurrentTrack() != "Synth Artist - Neon Dreams" {
			t.Errorf("CurrentTrack mismatch: got %q", pm.CurrentTrack())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for ICY metadata in test environment")
	}
}

func TestStopCancelsICYListenerAndClearsTrack(t *testing.T) {
	metaInt := 16
	metaBlock1 := make([]byte, 48)
	copy(metaBlock1, []byte("StreamTitle='Artist 1 - Track 1';"))
	metaBlock2 := make([]byte, 48)
	copy(metaBlock2, []byte("StreamTitle='Artist 2 - Track 2';"))

	serverProceed := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Icy-Metaint", fmt.Sprintf("%d", metaInt))
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		audioChunk := make([]byte, metaInt)
		// Write chunk 1 + metadata 1
		_, _ = w.Write(audioChunk)
		_, _ = w.Write([]byte{3})
		_, _ = w.Write(metaBlock1)
		if flusher != nil {
			flusher.Flush()
		}

		// Wait for signal from test to write second track
		<-serverProceed

		_, _ = w.Write(audioChunk)
		_, _ = w.Write([]byte{3})
		_, _ = w.Write(metaBlock2)
		_, _ = w.Write(audioChunk)
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer ts.Close()
	defer close(serverProceed)

	trackCh := make(chan TrackInfo, 10)
	pm := NewManager("native", 80, func(info TrackInfo) {
		trackCh <- info
	})

	st := radio.Station{
		ID:   "test-stop-station",
		Name: "Test Stop Station",
		URL:  ts.URL,
	}

	pm.mu.Lock()
	pm.status = StatusPlaying
	pm.currentStation = &st
	ctx, cancel := context.WithCancel(context.Background())
	pm.icyCancel = cancel
	pm.mu.Unlock()

	go pm.startICYListener(ctx, st)

	// Wait for first track to arrive
	select {
	case track := <-trackCh:
		if track.TrackTitle != "Artist 1 - Track 1" {
			t.Fatalf("Expected 'Artist 1 - Track 1', got %q", track.TrackTitle)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for first track")
	}

	// Stop player
	if err := pm.Stop(); err != nil {
		t.Fatalf("Stop error: %v", err)
	}

	if pm.Status() != StatusStopped {
		t.Errorf("Expected StatusStopped, got %s", pm.Status())
	}
	if pm.CurrentStation() != nil {
		t.Errorf("Expected CurrentStation to be nil after Stop(), got %+v", pm.CurrentStation())
	}
	if pm.CurrentTrack() != "" {
		t.Errorf("Expected CurrentTrack to be empty after Stop(), got %q", pm.CurrentTrack())
	}

	// Signal server to send track 2
	serverProceed <- struct{}{}

	// Ensure no second track is received
	select {
	case track := <-trackCh:
		t.Fatalf("Received unexpected track notification after Stop(): %+v", track)
	case <-time.After(300 * time.Millisecond):
		// Expected: no track received
	}
}

func TestPauseCancelsICYListener(t *testing.T) {
	metaInt := 16
	metaBlock1 := make([]byte, 48)
	copy(metaBlock1, []byte("StreamTitle='Artist 1 - Track 1';"))
	metaBlock2 := make([]byte, 48)
	copy(metaBlock2, []byte("StreamTitle='Artist 2 - Track 2';"))

	serverProceed := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Icy-Metaint", fmt.Sprintf("%d", metaInt))
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		audioChunk := make([]byte, metaInt)
		// Write chunk 1 + metadata 1
		_, _ = w.Write(audioChunk)
		_, _ = w.Write([]byte{3})
		_, _ = w.Write(metaBlock1)
		if flusher != nil {
			flusher.Flush()
		}

		// Wait for signal from test to write second track
		<-serverProceed

		_, _ = w.Write(audioChunk)
		_, _ = w.Write([]byte{3})
		_, _ = w.Write(metaBlock2)
		_, _ = w.Write(audioChunk)
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer ts.Close()
	defer close(serverProceed)

	trackCh := make(chan TrackInfo, 10)
	pm := NewManager("native", 80, func(info TrackInfo) {
		trackCh <- info
	})

	st := radio.Station{
		ID:   "test-pause-station",
		Name: "Test Pause Station",
		URL:  ts.URL,
	}

	pm.mu.Lock()
	pm.status = StatusPlaying
	pm.currentStation = &st
	ctx, cancel := context.WithCancel(context.Background())
	pm.icyCancel = cancel
	pm.mu.Unlock()

	go pm.startICYListener(ctx, st)

	// Wait for first track to arrive
	select {
	case track := <-trackCh:
		if track.TrackTitle != "Artist 1 - Track 1" {
			t.Fatalf("Expected 'Artist 1 - Track 1', got %q", track.TrackTitle)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for first track")
	}

	// Pause player
	if err := pm.Pause(); err != nil {
		t.Fatalf("Pause error: %v", err)
	}

	if pm.Status() != StatusPaused {
		t.Errorf("Expected StatusPaused, got %s", pm.Status())
	}
	// Station should still be remembered for Resume()
	if pm.CurrentStation() == nil || pm.CurrentStation().ID != "test-pause-station" {
		t.Errorf("Expected CurrentStation to be preserved on Pause()")
	}

	// Signal server to send track 2
	serverProceed <- struct{}{}

	// Ensure no second track is received
	select {
	case track := <-trackCh:
		t.Fatalf("Received unexpected track notification after Pause(): %+v", track)
	case <-time.After(300 * time.Millisecond):
		// Expected: no track received
	}
}

func TestICYListenerStationMismatchIgnored(t *testing.T) {
	metaInt := 16
	metaBlock := make([]byte, 48)
	copy(metaBlock, []byte("StreamTitle='Artist X - Song Y';"))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Icy-Metaint", fmt.Sprintf("%d", metaInt))
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		audioChunk := make([]byte, metaInt)
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write(audioChunk)
		_, _ = w.Write([]byte{3})
		_, _ = w.Write(metaBlock)
		_, _ = w.Write(audioChunk)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer ts.Close()

	trackCh := make(chan TrackInfo, 10)
	pm := NewManager("native", 80, func(info TrackInfo) {
		trackCh <- info
	})

	stA := radio.Station{ID: "station-a", Name: "Station A", URL: ts.URL}
	stB := radio.Station{ID: "station-b", Name: "Station B", URL: "http://example.com/b"}

	pm.mu.Lock()
	pm.status = StatusPlaying
	pm.currentStation = &stB // Station changed to B
	pm.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go pm.startICYListener(ctx, stA)

	select {
	case track := <-trackCh:
		t.Fatalf("Received unexpected track for mismatched station: %+v", track)
	case <-time.After(300 * time.Millisecond):
		// Expected: no track dispatched
	}
}
