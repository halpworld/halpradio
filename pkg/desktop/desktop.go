package desktop

import (
	"runtime"
	"strings"
	"sync"
	"time"
)

// DesktopConfig configures the desktop integration subsystem.
type DesktopConfig struct {
	NotificationsEnabled bool
	MPRISEnabled         bool
	IPCEnabled           bool
	DiscordEnabled       bool
	DiscordClientID      string
	SocketPath           string
	Runner               CommandRunner // Optional custom command runner for notifications (e.g. mock runner for tests)
	DiscordClient        DiscordClient // Optional custom Discord RPC client (e.g. for testing)
}

// Manager coordinates OS notifications, MPRIS D-Bus interface, local IPC, and Discord RPC.
type Manager struct {
	mu            sync.Mutex
	cfg           DesktopConfig
	notifier      *DesktopNotifier
	mpris         *MPRISServer
	ipc           *IPCServer
	discord       DiscordClient
	onAction      func(MediaAction)
	playbackStart time.Time

	// Cached playback state
	status       string
	stationID    string
	stationName  string
	stationGenre string
	streamURL    string
	bitrate      int
	trackTitle   string
	volume       int
	isMuted      bool
	backend      string
	visualizer   string
	closed       bool
}

// NewManager creates and starts the desktop integration services according to config.
func NewManager(cfg DesktopConfig, onAction func(MediaAction)) *Manager {
	notifier := NewNotifier(cfg.NotificationsEnabled)
	if cfg.Runner != nil {
		notifier.SetRunner(cfg.Runner)
	}

	var discordClient DiscordClient
	if cfg.DiscordClient != nil {
		discordClient = cfg.DiscordClient
	} else if cfg.DiscordEnabled {
		discordClient = NewDiscordRPCClient(cfg.DiscordClientID)
	}

	m := &Manager{
		cfg:        cfg,
		notifier:   notifier,
		discord:    discordClient,
		onAction:   onAction,
		volume:     80,
		status:     "STOPPED",
		visualizer: "dj-cat",
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
	m.UpdatePlaybackFull(status, "", stationName, genre, trackTitle, streamURL, 0, volume, isMuted, backend, "")
}

// UpdatePlaybackFull updates the complete playback state in DesktopManager and propagates to MPRIS and Discord RPC.
func (m *Manager) UpdatePlaybackFull(status, stationID, stationName, genre, trackTitle, streamURL string, bitrate, volume int, isMuted bool, backend, visualizer string) {
	if m == nil {
		return
	}

	m.mu.Lock()
	prevStatus := m.status
	prevTrack := m.trackTitle
	prevStation := m.stationName

	m.status = status
	m.stationID = stationID
	m.stationName = stationName
	m.stationGenre = genre
	m.trackTitle = trackTitle
	m.streamURL = streamURL
	m.bitrate = bitrate
	m.volume = volume
	m.isMuted = isMuted
	m.backend = backend
	if visualizer != "" {
		m.visualizer = visualizer
	}

	// Track playback duration timestamp
	if strings.ToUpper(status) == "PLAYING" {
		if strings.ToUpper(prevStatus) != "PLAYING" || prevTrack != trackTitle || prevStation != stationName || m.playbackStart.IsZero() {
			m.playbackStart = time.Now()
		}
	} else if strings.ToUpper(status) == "STOPPED" {
		m.playbackStart = time.Time{}
	}

	mpris := m.mpris
	discord := m.discord
	playbackStart := m.playbackStart
	currViz := m.visualizer
	m.mu.Unlock()

	// Update MPRIS
	if mpris != nil {
		normVol := float64(volume) / 100.0
		if isMuted {
			normVol = 0.0
		}
		mpris.UpdatePlaybackState(status, stationName, genre, trackTitle, streamURL, normVol)
	}

	// Update Discord Rich Presence
	if discord != nil {
		go func() {
			switch strings.ToUpper(status) {
			case "PLAYING":
				details := trackTitle
				if details == "" || details == stationName {
					details = "Streaming Live..."
				}
				state := stationName
				if state == "" {
					state = "Internet Radio"
				}
				imgKey, imgHover := GetDiscordDJAsset(currViz)
				var startTime *time.Time
				if !playbackStart.IsZero() {
					startTime = &playbackStart
				}
				_ = discord.UpdateActivity(DiscordActivity{
					State:      state,
					Details:    details,
					LargeImage: "halpradio_logo",
					LargeText:  "halpradio - Terminal Internet Radio",
					SmallImage: imgKey,
					SmallText:  imgHover,
					StartTime:  startTime,
				})
			case "PAUSED":
				details := trackTitle
				if details == "" {
					details = "Paused"
				} else {
					details = "[Paused] " + details
				}
				state := stationName
				if state == "" {
					state = "Paused"
				}
				imgKey, imgHover := GetDiscordDJAsset(currViz)
				_ = discord.UpdateActivity(DiscordActivity{
					State:      state,
					Details:    details,
					LargeImage: "halpradio_logo",
					LargeText:  "halpradio - Terminal Internet Radio",
					SmallImage: imgKey,
					SmallText:  imgHover,
					StartTime:  nil,
				})
			case "STOPPED":
				_ = discord.ClearActivity()
			}
		}()
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
	if strings.ToUpper(status) == "STOPPED" || strings.ToUpper(status) == "PAUSED" {
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

	artist, title := SplitArtistTitle(m.trackTitle)
	if title == "" && m.stationName != "" {
		title = m.stationName
	}
	station := m.stationName

	normStatus := strings.ToLower(m.status)
	if normStatus == "" {
		normStatus = "stopped"
	}

	viz := m.visualizer
	if viz == "" {
		viz = "dj-cat"
	}

	return &PlaybackInfo{
		Status:      normStatus,
		StationID:   m.stationID,
		StationName: m.stationName,
		Station:     station,
		Artist:      artist,
		Title:       title,
		Track:       m.trackTitle,
		Bitrate:     m.bitrate,
		Volume:      m.volume,
		Muted:       m.isMuted,
		Backend:     m.backend,
		Visualizer:  viz,
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

// SetDiscordClient sets a custom Discord client on the manager.
func (m *Manager) SetDiscordClient(d DiscordClient) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.discord = d
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

	if m.discord != nil {
		_ = m.discord.Close()
		m.discord = nil
	}

	return nil
}
