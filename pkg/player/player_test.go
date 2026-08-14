package player

import (
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

	autoBackend := detectBackend("auto")
	if autoBackend == "" {
		t.Errorf("Expected auto backend detection to yield a valid backend name")
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
