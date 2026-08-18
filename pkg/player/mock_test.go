package player

import (
	"testing"

	"github.com/halpworld/halpradio/pkg/radio"
)

func TestMockPlayerLifecycle(t *testing.T) {
	var trackDispatched TrackInfo
	mp := NewMockPlayer(70, func(info TrackInfo) {
		trackDispatched = info
	})

	if mp.Volume() != 70 {
		t.Errorf("expected initial volume 70, got %d", mp.Volume())
	}
	if mp.Status() != StatusStopped {
		t.Errorf("expected StatusStopped, got %s", mp.Status())
	}
	if mp.ActiveBackend() != "mock" {
		t.Errorf("expected backend 'mock', got %s", mp.ActiveBackend())
	}
	if mp.IsMuted() {
		t.Errorf("expected unmuted initially")
	}

	// Test play valid station
	st := radio.Station{
		ID:   "mock-1",
		Name: "Mock Station",
		URL:  "http://example.com/stream",
	}
	if err := mp.Play(st); err != nil {
		t.Fatalf("Play failed: %v", err)
	}

	if mp.Status() != StatusPlaying {
		t.Errorf("expected StatusPlaying, got %s", mp.Status())
	}
	if mp.CurrentStation() == nil || mp.CurrentStation().ID != "mock-1" {
		t.Errorf("current station mismatch: %+v", mp.CurrentStation())
	}
	if trackDispatched.TrackTitle != "Mock Station" {
		t.Errorf("expected track title 'Mock Station', got %q", trackDispatched.TrackTitle)
	}

	// Test SetTrack
	mp.SetTrack("Artist - Custom Track")
	if mp.CurrentTrack() != "Artist - Custom Track" {
		t.Errorf("expected current track 'Artist - Custom Track', got %q", mp.CurrentTrack())
	}
	if trackDispatched.TrackTitle != "Artist - Custom Track" {
		t.Errorf("expected callback track 'Artist - Custom Track', got %q", trackDispatched.TrackTitle)
	}

	// Test Pause & Resume
	if err := mp.Pause(); err != nil {
		t.Fatalf("Pause failed: %v", err)
	}
	if mp.Status() != StatusPaused {
		t.Errorf("expected StatusPaused, got %s", mp.Status())
	}

	if err := mp.Resume(); err != nil {
		t.Fatalf("Resume failed: %v", err)
	}
	if mp.Status() != StatusPlaying {
		t.Errorf("expected StatusPlaying after resume, got %s", mp.Status())
	}

	// Test Volume & Mute
	vol := mp.SetVolume(90)
	if vol != 90 || mp.Volume() != 90 {
		t.Errorf("expected volume 90, got %d", mp.Volume())
	}
	muted := mp.ToggleMute()
	if !muted || !mp.IsMuted() || mp.Volume() != 0 {
		t.Errorf("expected muted with 0 effective volume")
	}
	unmuted := mp.ToggleMute()
	if unmuted || mp.IsMuted() || mp.Volume() != 90 {
		t.Errorf("expected unmuted with 90 effective volume")
	}

	// Test Stop
	if err := mp.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
	if mp.Status() != StatusStopped {
		t.Errorf("expected StatusStopped, got %s", mp.Status())
	}
	if mp.CurrentStation() != nil {
		t.Errorf("expected nil current station after stop")
	}

	// Test Invalid URL
	invalidSt := radio.Station{
		ID:   "invalid",
		Name: "Invalid",
		URL:  "bad-url",
	}
	_ = mp.Play(invalidSt)
	if mp.Status() != StatusError {
		t.Errorf("expected StatusError for bad URL, got %s", mp.Status())
	}
	if mp.Error() == "" {
		t.Errorf("expected non-empty Error()")
	}

	// Test SetStatus
	mp.SetStatus(StatusConnecting)
	if mp.Status() != StatusConnecting {
		t.Errorf("expected StatusConnecting, got %s", mp.Status())
	}
}
