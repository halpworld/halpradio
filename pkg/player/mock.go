package player

import (
	"sync"

	"github.com/halpworld/halpradio/pkg/radio"
)

// MockPlayer is an in-memory, non-intrusive implementation of Player for unit and UI testing.
// It tracks player state, volume, and playback without spawning external audio processes
// (mpv, vlc, ffplay) or initializing hardware audio devices.
type MockPlayer struct {
	mu             sync.Mutex
	status         PlayStatus
	currentStation *radio.Station
	currentTrack   string
	activeBackend  string
	volume         int
	isMuted        bool
	lastError      string
	onTrackUpd     func(TrackInfo)
}

// NewMockPlayer creates a new MockPlayer with the specified initial volume and callback.
func NewMockPlayer(initialVolume int, onTrackUpd func(TrackInfo)) *MockPlayer {
	if initialVolume <= 0 || initialVolume > 100 {
		initialVolume = 80
	}
	return &MockPlayer{
		status:        StatusStopped,
		volume:        initialVolume,
		activeBackend: "mock",
		onTrackUpd:    onTrackUpd,
	}
}

// Play simulates starting playback for a given station in-memory.
func (m *MockPlayer) Play(st radio.Station) error {
	m.mu.Lock()
	if !IsValidStreamURL(st.URL) {
		m.status = StatusError
		m.lastError = "Invalid stream URL"
		m.mu.Unlock()
		return nil
	}

	m.status = StatusPlaying
	m.currentStation = &st
	m.currentTrack = st.Name
	m.lastError = ""
	cb := m.onTrackUpd
	m.mu.Unlock()

	if cb != nil {
		cb(TrackInfo{
			StationID:   st.ID,
			StationName: st.Name,
			TrackTitle:  st.Name,
		})
	}
	return nil
}

// Stop stops playback and clears current station/track.
func (m *MockPlayer) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = StatusStopped
	m.currentStation = nil
	m.currentTrack = ""
	return nil
}

// Pause pauses playback.
func (m *MockPlayer) Pause() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status == StatusPlaying || m.status == StatusConnecting {
		m.status = StatusPaused
	}
	return nil
}

// Resume resumes playback if a station is selected.
func (m *MockPlayer) Resume() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.currentStation != nil {
		m.status = StatusPlaying
	}
	return nil
}

// SetVolume sets playback volume (clamped 0..100) and unmutes.
func (m *MockPlayer) SetVolume(vol int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if vol < 0 {
		vol = 0
	}
	if vol > 100 {
		vol = 100
	}
	m.volume = vol
	m.isMuted = false
	return m.volume
}

// Volume returns effective volume (0 if muted).
func (m *MockPlayer) Volume() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.isMuted {
		return 0
	}
	return m.volume
}

// ToggleMute toggles mute state and returns the new muted condition.
func (m *MockPlayer) ToggleMute() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.isMuted = !m.isMuted
	return m.isMuted
}

// IsMuted reports whether the player is currently muted.
func (m *MockPlayer) IsMuted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isMuted
}

// Status returns current playback status.
func (m *MockPlayer) Status() PlayStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// CurrentStation returns a pointer to the active station.
func (m *MockPlayer) CurrentStation() *radio.Station {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentStation
}

// CurrentTrack returns the current track title.
func (m *MockPlayer) CurrentTrack() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentTrack
}

// ActiveBackend returns the mock backend identifier.
func (m *MockPlayer) ActiveBackend() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeBackend
}

// Error returns the last recorded error message.
func (m *MockPlayer) Error() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastError
}

// SetTrack manually updates the current track title (useful for testing track callbacks).
func (m *MockPlayer) SetTrack(title string) {
	m.mu.Lock()
	m.currentTrack = title
	st := m.currentStation
	cb := m.onTrackUpd
	m.mu.Unlock()

	if cb != nil && st != nil {
		cb(TrackInfo{
			StationID:   st.ID,
			StationName: st.Name,
			TrackTitle:  title,
		})
	}
}

// SetStatus manually updates the status.
func (m *MockPlayer) SetStatus(s PlayStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = s
}
