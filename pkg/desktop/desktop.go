package desktop

import (
	"runtime"
	"sync"
)

// DesktopConfig configures the desktop integration subsystem.
type DesktopConfig struct {
	NotificationsEnabled bool
	MPRISEnabled         bool
	IPCEnabled           bool
	SocketPath           string
	Runner               CommandRunner // Optional custom command runner for notifications (e.g. mock runner for tests)
}

// Manager coordinates OS notifications, MPRIS D-Bus interface, and local IPC.
type Manager struct {
	mu       sync.Mutex
	cfg      DesktopConfig
	notifier *DesktopNotifier
	mpris    *MPRISServer
	ipc      *IPCServer
	onAction func(MediaAction)

	// Cached playback state
	status       string
	stationName  string
	stationGenre string
	streamURL    string
	trackTitle   string
	volume       int
	isMuted      bool
	backend      string
	closed       bool
}

// NewManager creates and starts the desktop integration services according to config.
func NewManager(cfg DesktopConfig, onAction func(MediaAction)) *Manager {
	notifier := NewNotifier(cfg.NotificationsEnabled)
	if cfg.Runner != nil {
		notifier.SetRunner(cfg.Runner)
	}

	m := &Manager{
		cfg:      cfg,
		notifier: notifier,
		onAction: onAction,
		volume:   80,
		status:   "STOPPED",
	}

	// Start IPC Server if enabled
	if cfg.IPCEnabled {
		ipcServer, err := StartIPCServer(cfg.SocketPath, func(action MediaAction) (*PlaybackInfo, error) {
			if action == ActionStatus {
				return m.GetPlaybackInfo(), nil
			}

			if onAction != nil {
				onAction(action)
			}

			return m.GetPlaybackInfo(), nil
		})
		if err == nil {
			m.ipc = ipcServer
		}
	}

	// Start MPRIS Server on Linux (or any platform where D-Bus is available and enabled)
	if cfg.MPRISEnabled && runtime.GOOS == "linux" {
		mprisServer, err := StartMPRISServer(MPRISHandler{
			OnPlayPause: func() {
				if onAction != nil {
					onAction(ActionPlayPause)
				}
			},
			OnPlay: func() {
				if onAction != nil {
					onAction(ActionPlay)
				}
			},
			OnPause: func() {
				if onAction != nil {
					onAction(ActionPause)
				}
			},
			OnStop: func() {
				if onAction != nil {
					onAction(ActionStop)
				}
			},
			OnNext: func() {
				if onAction != nil {
					onAction(ActionNextStation)
				}
			},
			OnPrev: func() {
				if onAction != nil {
					onAction(ActionPrevStation)
				}
			},
			OnQuit: func() {
				if onAction != nil {
					onAction(ActionQuit)
				}
			},
			OnVolume: func(vol float64) {
				if onAction != nil {
					if vol > 0.5 {
						onAction(ActionVolumeUp)
					} else {
						onAction(ActionVolumeDown)
					}
				}
			},
		})
		if err == nil {
			m.mpris = mprisServer
		}
	}

	return m
}

// UpdatePlayback updates the state in DesktopManager and propagates to MPRIS.
func (m *Manager) UpdatePlayback(status, stationName, genre, trackTitle, streamURL string, volume int, isMuted bool, backend string) {
	if m == nil {
		return
	}

	m.mu.Lock()
	m.status = status
	m.stationName = stationName
	m.stationGenre = genre
	m.trackTitle = trackTitle
	m.streamURL = streamURL
	m.volume = volume
	m.isMuted = isMuted
	m.backend = backend
	mpris := m.mpris
	m.mu.Unlock()

	if mpris != nil {
		normVol := float64(volume) / 100.0
		if isMuted {
			normVol = 0.0
		}
		mpris.UpdatePlaybackState(status, stationName, genre, trackTitle, streamURL, normVol)
	}
}

// NotifySong dispatches a song notification if enabled and playback is not stopped.
func (m *Manager) NotifySong(stationName, trackTitle string) {
	if m == nil || m.notifier == nil {
		return
	}
	m.mu.Lock()
	status := m.status
	m.mu.Unlock()
	if status == "STOPPED" || status == "PAUSED" {
		return
	}
	m.notifier.NotifySong(stationName, trackTitle)
}

// GetPlaybackInfo returns a thread-safe snapshot of the current state.
func (m *Manager) GetPlaybackInfo() *PlaybackInfo {
	if m == nil {
		return &PlaybackInfo{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	return &PlaybackInfo{
		Status:  m.status,
		Station: m.stationName,
		Track:   m.trackTitle,
		Volume:  m.volume,
		Muted:   m.isMuted,
		Backend: m.backend,
	}
}

// SetNotificationsEnabled toggles desktop notifications.
func (m *Manager) SetNotificationsEnabled(enabled bool) {
	if m == nil || m.notifier == nil {
		return
	}
	m.notifier.SetEnabled(enabled)
}

// SetNotifierRunner sets the command runner for the notifier.
func (m *Manager) SetNotifierRunner(r CommandRunner) {
	if m == nil || m.notifier == nil {
		return
	}
	m.notifier.SetRunner(r)
}

// Close gracefully shuts down all desktop subsystems.
func (m *Manager) Close() error {
	if m == nil {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.closed {
		return nil
	}
	m.closed = true

	if m.ipc != nil {
		_ = m.ipc.Close()
		m.ipc = nil
	}

	if m.mpris != nil {
		_ = m.mpris.Close()
		m.mpris = nil
	}

	return nil
}
