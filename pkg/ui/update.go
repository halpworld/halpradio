package ui

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/debuglog"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/player/fingerprint"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
	"github.com/halpworld/halpradio/pkg/timer"
	"github.com/halpworld/halpradio/pkg/ui/components/tuner"
	"github.com/halpworld/halpradio/pkg/util"
)

// describeUpdateMsg renders a short, log-safe label for an incoming message and
// reports whether it is high-frequency enough to keep out of the log unless it
// misbehaves.
func describeUpdateMsg(msg tea.Msg) (label string, quiet bool) {
	switch msg := msg.(type) {
	case TickMsg:
		return "TickMsg", true
	case tea.MouseMsg:
		return "MouseMsg", true
	case tea.KeyMsg:
		return fmt.Sprintf("KeyMsg %q", msg.String()), false
	case tea.WindowSizeMsg:
		return fmt.Sprintf("WindowSizeMsg %dx%d", msg.Width, msg.Height), false
	default:
		return fmt.Sprintf("%T", msg), false
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	if debuglog.Enabled() {
		// The update loop is single-threaded: if a handler blocks here, the
		// whole TUI stops responding to the keyboard. Bracketing every message
		// means a frozen session leaves the culprit as the last "→" line in
		// the log, and the watchdog dumps stacks on top of it (issue #26).
		label, quiet := describeUpdateMsg(msg)
		defer debuglog.Watch("Update "+label, quiet)()
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width = msg.Width
		m.Height = msg.Height
		return m, nil

	case TickMsg:
		m.Visualizer.Tick()
		if m.Player.Status() == player.StatusError && m.Player.Error() != "" {
			m.StatusMessage = fmt.Sprintf("Error: %s", m.Player.Error())
		}

		now := time.Time(msg)
		if m.LastTickTime.IsZero() {
			m.LastTickTime = now
		}
		delta := now.Sub(m.LastTickTime)
		if delta <= 0 || delta > 2*time.Second {
			delta = 150 * time.Millisecond
		}
		m.LastTickTime = now

		if m.Timer != nil && m.Timer.IsActive() {
			events := m.Timer.Tick(delta)
			for _, ev := range events {
				m.handleTimerEvent(ev)
			}
			if m.PluginMgr != nil {
				modeStr := "none"
				if m.Timer.Type == timer.TimerSleep {
					modeStr = "sleep"
				} else if m.Timer.Type == timer.TimerPomodoro {
					modeStr = "pomodoro"
				}
				m.PluginMgr.DispatchTimerTick(plugin.TimerTickPayload{
					Mode:             modeStr,
					State:            string(m.Timer.State),
					RemainingSeconds: int(m.Timer.TimeRemaining.Seconds()),
					TotalSeconds:     int(m.Timer.TotalDuration.Seconds()),
				})
			}
		}

		var tickCmds []tea.Cmd
		tickCmds = append(tickCmds, tickCmd())

		// Background Acoustic Stream Fingerprint Engine (Auto-Identify)
		if m.Config.FingerprintEnabled && m.Config.AutoIdentify && !m.IsIdentifying {
			st := m.Player.CurrentStation()
			if st != nil && m.Player.Status() == player.StatusPlaying {
				currTrack := m.Player.CurrentTrack()
				isGeneric := currTrack == "" || radio.IsDirtyOrGeneric(currTrack, st.Name)
				if isGeneric && m.IdentifiedResult == nil {
					if !m.PlaybackStartTime.IsZero() && time.Since(m.PlaybackStartTime) >= 20*time.Second &&
						(m.LastFingerprintTime.IsZero() || time.Since(m.LastFingerprintTime) >= 60*time.Second) {
						m.IsIdentifying = true
						m.LastFingerprintTime = time.Now()
						m.StatusMessage = "🔍 Auto-fingerprinting stream (no metadata broadcast)..."
						if cmd := m.identifyTrackCmd(); cmd != nil {
							tickCmds = append(tickCmds, cmd)
						}
					}
				}
			}
		}

		if m.PartySession != nil && m.PartySession.IsActive() {
			m.PartySession.Tick()
		}

		return m, tea.Batch(tickCmds...)

	case PartyPlaybackSyncMsg:
		sp := party.SyncPayload(msg)
		st := m.Store.FindStationByID(sp.StationID)
		if st == nil {
			st = &radio.Station{
				ID:      sp.StationID,
				Name:    sp.StationName,
				URL:     sp.StreamURL,
				Genre:   sp.Genre,
				Country: sp.Country,
				Bitrate: sp.Bitrate,
				Codec:   sp.Codec,
			}
		}
		if sp.Status == "playing" {
			_ = m.Player.Play(*st)
			m.PlayingID = st.ID
			m.StatusMessage = fmt.Sprintf("📻 Party sync: %s", st.Name)
			m.SyncDesktop()
		} else if sp.Status == "paused" {
			_ = m.Player.Pause()
			m.PlayingID = ""
			m.StatusMessage = fmt.Sprintf("⏸️ Party paused: %s", st.Name)
			m.SyncDesktop()
		} else if sp.Status == "stopped" {
			_ = m.Player.Stop()
			m.PlayingID = ""
			m.StatusMessage = "⏹️ Party audio stopped"
			m.SyncDesktop()
		}
		return m, nil

	case PartyReactionMsg:
		return m, nil

	case PartyChatMsg:
		return m, nil

	case PartyPeerChangeMsg:
		m.SyncDesktop()
		return m, nil

	case PartyCreateRoomMsg:
		if m.PartySession != nil {
			_ = m.PartySession.Close()
		}
		roomName := msg.RoomName
		if roomName == "" {
			roomName = "team-focus"
		}
		nick := msg.Nickname
		if nick == "" {
			nick = m.Config.PartyNickname
		}
		if nick == "" {
			nick = "host"
		}
		djPass := party.DJPassHostOnly
		if msg.DJPass == "open" {
			djPass = party.DJPassOpenDemocracy
		}
		sess, err := party.NewPartySession(party.SessionConfig{
			RoomName: roomName,
			Nickname: nick,
			IsHost:   true,
			DJPass:   djPass,
			Port:     m.Config.PartyPort,
		})
		if err == nil {
			m.PartySession = sess
			m.SetupPartyHandlers(sess)
			if curr := m.Player.CurrentStation(); curr != nil {
				_ = m.PartySession.BroadcastStationChange(curr.ID, curr.Name, curr.URL, curr.Genre, curr.Country, curr.Codec, curr.Bitrate, string(m.Player.Status()))
			}
			m.StatusMessage = fmt.Sprintf("✓ Created party room %s!", sess.FormattedCode())
			m.SyncDesktop()
		} else {
			m.StatusMessage = fmt.Sprintf("Party create error: %v", err)
		}
		return m, nil

	case PartyJoinRoomMsg:
		if m.PartySession != nil {
			_ = m.PartySession.Close()
		}
		code := party.NormalizeRoomCode(msg.RoomCode)
		nick := msg.Nickname
		if nick == "" {
			nick = m.Config.PartyNickname
		}
		if nick == "" {
			nick = "listener"
		}
		sess, err := party.NewPartySession(party.SessionConfig{
			RoomCode: code,
			Nickname: nick,
			IsHost:   false,
			Port:     0,
		})
		if err == nil {
			m.PartySession = sess
			m.SetupPartyHandlers(sess)
			if msg.Address != "" {
				_ = sess.ConnectDirect(msg.Address)
			}
			m.StatusMessage = fmt.Sprintf("✓ Connected to party room %s!", sess.FormattedCode())
			m.SyncDesktop()
		} else {
			m.StatusMessage = fmt.Sprintf("Party join error: %v", err)
		}
		return m, nil

	case PartyLeaveRoomMsg:
		if m.PartySession != nil {
			_ = m.PartySession.Close()
			m.PartySession = nil
			m.StatusMessage = "Left party room"
			m.SyncDesktop()
		}
		return m, nil

	case PartySendReactionMsg:
		if m.PartySession != nil && m.PartySession.IsActive() {
			_, _ = m.PartySession.BroadcastReaction(string(msg))
		}
		return m, nil

	case PartySendChatMsg:
		if m.PartySession != nil && m.PartySession.IsActive() {
			_, _ = m.PartySession.BroadcastChat(string(msg))
		}
		return m, nil

	case PartyStatusFlashMsg:
		m.StatusMessage = string(msg)
		return m, nil

	case PluginRegistryLoadedMsg:
		if msg.Err != nil {
			m.PluginStatusMsg = fmt.Sprintf("Registry error: %v", msg.Err)
		} else {
			m.PluginRegistryList = msg.Plugins
			m.PluginStatusMsg = ""
		}
		return m, nil

	case PluginInstalledMsg:
		if msg.Err != nil {
			m.PluginStatusMsg = fmt.Sprintf("Install error: %v", msg.Err)
		} else {
			m.PluginStatusMsg = fmt.Sprintf("✓ Plugin %s installed!", msg.PluginID)
			if m.PluginMgr != nil {
				if info, ok := m.PluginMgr.GetPlugin(msg.PluginID); ok {
					m.ApprovalPlugin = info
					m.ShowPermissionApproval = true
				}
			}
		}
		return m, nil

	case PluginFlashMsg:
		m.StatusMessage = string(msg)
		return m, nil

	case PluginNotificationMsg:
		if m.Config.SongNotifications && m.Desktop != nil {
			m.Desktop.NotifySong(msg.Title, msg.Message)
		}
		m.StatusMessage = fmt.Sprintf("[%s] %s", msg.Title, msg.Message)
		return m, nil

	case ThemeRegistryLoadedMsg:
		if msg.Err != nil && len(msg.Themes) == 0 {
			client := m.ThemeClient
			if client == nil {
				client = theme.NewRegistryClient(m.Config.ThemeRegistryURL)
			}
			reg, _ := client.FetchRegistry(context.Background())
			m.ThemeRegistryList = reg.Themes
			m.ThemeStatusMsg = "Registry offline (using offline catalog)"
		} else {
			m.ThemeRegistryList = msg.Themes
			m.ThemeStatusMsg = ""
		}
		return m, nil

	case ThemeInstalledMsg:
		if msg.Err != nil {
			m.ThemeStatusMsg = fmt.Sprintf("Install error: %v", msg.Err)
		} else {
			m.ThemeStatusMsg = fmt.Sprintf("✓ Theme %s installed!", msg.ThemeID)
			m.StatusMessage = fmt.Sprintf("✓ Downloaded and applied theme %s", msg.ThemeID)
			m.applyTheme(msg.ThemeID)
			m.ShowThemePicker = false
		}
		return m, nil

	case ThemeDeletedMsg:
		if msg.Err != nil {
			m.ThemeStatusMsg = fmt.Sprintf("Delete error: %v", msg.Err)
		} else {
			m.ThemeStatusMsg = fmt.Sprintf("✓ Deleted theme %s", msg.ThemeID)
			m.StatusMessage = fmt.Sprintf("✓ Deleted theme %s", msg.ThemeID)
			if m.Config.Theme == msg.ThemeID || m.Theme.ID == msg.ThemeID {
				m.applyTheme("tokyonight")
			}
			allThemes := theme.GetAllThemes()
			if m.ThemeCursor >= len(allThemes) {
				m.ThemeCursor = len(allThemes) - 1
			}
			if m.ThemeCursor < 0 {
				m.ThemeCursor = 0
			}
		}
		return m, nil

	case ThemeFlashMsg:
		m.ThemeStatusMsg = string(msg)
		return m, nil

	case TrackUpdatedMsg:
		if m.Player.Status() != player.StatusPlaying && m.Player.Status() != player.StatusConnecting {
			return m, nil
		}
		if m.PlayingID == "" || (msg.StationID != "" && m.PlayingID != msg.StationID) {
			return m, nil
		}
		if msg.TrackTitle != "" {
			if m.IdentifiedResult != nil && !strings.EqualFold(msg.TrackTitle, m.IdentifiedResult.SimpleTitle()) {
				m.IdentifiedResult = nil
			}
			m.Store.AddHistory(msg.StationID, msg.StationName, msg.TrackTitle)
			if m.Config.SongNotifications && m.Desktop != nil {
				m.Desktop.NotifySong(msg.StationName, msg.TrackTitle)
			}
		}
		if m.PluginMgr != nil {
			artist := ""
			title := msg.TrackTitle
			if parts := strings.SplitN(msg.TrackTitle, " - ", 2); len(parts) == 2 {
				artist = strings.TrimSpace(parts[0])
				title = strings.TrimSpace(parts[1])
			}
			bitrate := 0
			codec := "MP3"
			if st := m.Player.CurrentStation(); st != nil {
				bitrate = st.Bitrate
				if st.Codec != "" {
					codec = st.Codec
				}
			}
			m.PluginMgr.DispatchTrackChange(plugin.TrackChangePayload{
				Station:   msg.StationName,
				Artist:    artist,
				Title:     title,
				Bitrate:   bitrate,
				Codec:     codec,
				Timestamp: time.Now().Format(time.RFC3339),
			})
		}
		if m.Desktop != nil {
			st := m.Player.CurrentStation()
			stName := msg.StationName
			genre := ""
			streamURL := ""
			if st != nil {
				if stName == "" {
					stName = st.Name
				}
				genre = st.Genre
				streamURL = st.URL
			}
			trackTitle := msg.TrackTitle
			if trackTitle == "" {
				trackTitle = m.Player.CurrentTrack()
			}
			m.Desktop.UpdatePlayback(
				string(m.Player.Status()),
				stName,
				genre,
				trackTitle,
				streamURL,
				m.Player.Volume(),
				m.Player.IsMuted(),
				m.Player.ActiveBackend(),
			)
		}
		return m, tea.SetWindowTitle(m.WindowTitle())

	case TrackIdentifiedMsg:
		m.IsIdentifying = false
		if m.Player.Status() != player.StatusPlaying && m.Player.Status() != player.StatusConnecting {
			return m, nil
		}
		if m.PlayingID == "" || (msg.StationID != "" && m.PlayingID != msg.StationID) {
			return m, nil
		}
		if msg.Err != nil || msg.Result == nil {
			m.StatusMessage = "No acoustic match found for stream audio"
			return m, nil
		}

		m.IdentifiedResult = msg.Result
		simpleTitle := msg.Result.SimpleTitle()
		fullDisplay := msg.Result.FullDisplay()

		// Record in store history
		m.Store.AddIdentifiedHistory(
			msg.StationID,
			msg.StationName,
			msg.Result.Artist,
			msg.Result.Title,
			msg.Result.Album,
			msg.Result.Year,
			msg.Result.Source,
			msg.Result.Confidence,
		)

		// Desktop Notification
		if m.Config.SongNotifications && m.Desktop != nil {
			m.Desktop.NotifySong(msg.StationName, "✨ "+simpleTitle)
		}

		// Dispatch to plugins
		if m.PluginMgr != nil {
			bitrate := 0
			codec := "MP3"
			if st := m.Player.CurrentStation(); st != nil {
				bitrate = st.Bitrate
				if st.Codec != "" {
					codec = st.Codec
				}
			}
			m.PluginMgr.DispatchTrackChange(plugin.TrackChangePayload{
				Station:   msg.StationName,
				Artist:    msg.Result.Artist,
				Title:     msg.Result.Title,
				Bitrate:   bitrate,
				Codec:     codec,
				Timestamp: time.Now().Format(time.RFC3339),
			})
		}

		// Desktop MPRIS / Discord RPC
		if m.Desktop != nil {
			st := m.Player.CurrentStation()
			stName := msg.StationName
			genre := ""
			streamURL := ""
			if st != nil {
				if stName == "" {
					stName = st.Name
				}
				genre = st.Genre
				streamURL = st.URL
			}
			m.Desktop.UpdatePlayback(
				string(m.Player.Status()),
				stName,
				genre,
				fullDisplay,
				streamURL,
				m.Player.Volume(),
				m.Player.IsMuted(),
				m.Player.ActiveBackend(),
			)
		}

		m.StatusMessage = fmt.Sprintf("✨ Identified: %s [%s %d%%]", simpleTitle, msg.Result.Source, int(msg.Result.Confidence*100))
		return m, tea.SetWindowTitle(m.WindowTitle())

	case MediaPlayPauseMsg:
		m.TogglePlayPause()
		return m, tea.SetWindowTitle(m.WindowTitle())

	case MediaPlayMsg:
		if m.Player.Status() == player.StatusPaused && m.Player.CurrentStation() != nil {
			_ = m.Player.Resume()
			st := m.Player.CurrentStation()
			m.PlayingID = st.ID
			m.StatusMessage = fmt.Sprintf("Resumed %s", st.Name)
		} else if len(m.Stations) > 0 && m.SelectedIndex < len(m.Stations) {
			st := m.Stations[m.SelectedIndex]
			_ = m.Player.Play(st)
			m.PlayingID = st.ID
			m.StatusMessage = fmt.Sprintf("Playing %s [%s]", st.Name, m.Player.ActiveBackend())
			if m.Config.SongNotifications && m.Desktop != nil {
				m.Desktop.NotifySong(st.Name, st.Name)
			}
		}
		m.SyncDesktop()
		return m, tea.SetWindowTitle(m.WindowTitle())

	case MediaPauseMsg:
		if m.Player.Status() == player.StatusPlaying {
			_ = m.Player.Pause()
			m.PlayingID = ""
			m.StatusMessage = "Audio playback paused"
		}
		m.SyncDesktop()
		return m, tea.SetWindowTitle(m.WindowTitle())

	case AutoPauseMsg:
		m.PlayingID = ""
		m.StatusMessage = "Paused - headphones disconnected"
		m.SyncDesktop()
		return m, tea.SetWindowTitle(m.WindowTitle())

	case MediaStopMsg:
		_ = m.Player.Stop()
		m.PlayingID = ""
		m.StatusMessage = "Audio playback stopped"
		m.SyncDesktop()
		return m, tea.SetWindowTitle(m.WindowTitle())

	case MediaNextMsg:
		m.PlayNextStation()
		return m, tea.SetWindowTitle(m.WindowTitle())

	case MediaPrevMsg:
		m.PlayPrevStation()
		return m, tea.SetWindowTitle(m.WindowTitle())

	case MediaVolUpMsg:
		v := m.Player.SetVolume(m.Player.Volume() + 5)
		m.StatusMessage = fmt.Sprintf("Volume: %d%%", v)
		m.SyncDesktop()
		return m, nil

	case MediaVolDownMsg:
		v := m.Player.SetVolume(m.Player.Volume() - 5)
		m.StatusMessage = fmt.Sprintf("Volume: %d%%", v)
		m.SyncDesktop()
		return m, nil

	case MediaMuteMsg:
		isMuted := m.Player.ToggleMute()
		if isMuted {
			m.StatusMessage = "Muted"
		} else {
			m.StatusMessage = fmt.Sprintf("Unmuted (%d%%)", m.Player.Volume())
		}
		m.SyncDesktop()
		return m, nil

	case MediaRandomMsg:
		if len(m.Stations) > 0 {
			idx := rand.Intn(len(m.Stations))
			m.SelectedIndex = idx
			st := m.Stations[idx]
			_ = m.Player.Play(st)
			m.PlayingID = st.ID
			m.StatusMessage = fmt.Sprintf("Playing Random: %s", st.Name)
			m.SyncDesktop()
			if m.Config.SongNotifications && m.Desktop != nil {
				m.Desktop.NotifySong(st.Name, st.Name)
			}
		}
		return m, tea.SetWindowTitle(m.WindowTitle())

	case MediaQuitMsg:
		_ = m.Player.Stop()
		m.SyncDesktop()
		return m, tea.Quit

	case FlashMessageMsg:
		m.StatusMessage = string(msg)
		return m, nil

	case RadioBrowserResultMsg:
		if msg.Err != nil {
			m.StatusMessage = fmt.Sprintf("RadioBrowser error: %v", msg.Err)
		} else {
			m.RBStations = msg.Stations
			m.RefreshStations()
			m.StatusMessage = fmt.Sprintf("Loaded %d stations from RadioBrowser", len(msg.Stations))
		}
		return m, nil

	case CatalogUpdatedMsg:
		if msg.Updated {
			m.Store.ReloadBundledFromCache()
			m.Genres = m.Store.GetCategories()
			m.RefreshStations()
			m.StatusMessage = fmt.Sprintf("Station catalog updated (%d stations)", msg.StationsCount)
		}
		return m, nil

	case tea.KeyMsg:
		if m.ShowWhichKey {
			if key.Matches(msg, m.KeyMap.Clear) || key.Matches(msg, m.KeyMap.Help) || key.Matches(msg, m.KeyMap.Quit) {
				m.ShowWhichKey = false
			}
			return m, nil
		}

		if m.ShowPRExport {
			if key.Matches(msg, m.KeyMap.Clear) || key.Matches(msg, m.KeyMap.PlayPause) || key.Matches(msg, m.KeyMap.Quit) {
				m.ShowPRExport = false
			}
			return m, nil
		}

		if m.ShowThemePicker {
			return m.handleThemePickerKey(msg)
		}

		if m.ShowAddModal {
			return m.handleAddModalKey(msg)
		}

		if m.ShowTimerModal {
			return m.handleTimerModalKey(msg)
		}

		if m.ShowPermissionApproval {
			return m.handlePermissionApprovalKey(msg)
		}

		if m.ShowPluginModal {
			return m.handlePluginModalKey(msg)
		}

		if m.ShowPartyModal {
			return m.handlePartyModalKey(msg)
		}

		if m.IsChatting {
			return m.handlePartyChatKey(msg)
		}

		if m.IsSearching {
			switch msg.String() {
			case "esc":
				m.IsSearching = false
			case "enter":
				m.IsSearching = false
				if m.ActiveTab == 5 && m.SearchQuery != "" {
					return m, m.searchRadioBrowserCmd(m.SearchQuery)
				}
			case "backspace":
				if len(m.SearchQuery) > 0 {
					m.SearchQuery = m.SearchQuery[:len(m.SearchQuery)-1]
					m.RefreshStations()
				}
			default:
				if len(msg.String()) == 1 {
					m.SearchQuery += msg.String()
					m.RefreshStations()
				}
			}
			return m, nil
		}

		// Party Mode Live ASCII Reactions (1-5) and Chat Ping (t)
		if m.PartySession != nil && m.PartySession.IsActive() {
			switch msg.String() {
			case "1", "2", "3", "4", "5":
				r, err := m.PartySession.BroadcastReaction(msg.String())
				if err == nil {
					m.StatusMessage = fmt.Sprintf("Reacted %s", r.Emoji)
				}
				return m, nil
			case "t":
				m.IsChatting = true
				m.ChatInput = ""
				return m, nil
			}
		}

		switch {
		case key.Matches(msg, m.KeyMap.Quit):
			_ = m.Player.Stop()
			return m, tea.Quit

		case key.Matches(msg, m.KeyMap.Help):
			m.ShowWhichKey = true

		case key.Matches(msg, m.KeyMap.Party):
			m.openPartyModal()
			return m, nil

		case key.Matches(msg, m.KeyMap.Theme) || msg.String() == "T":
			m.ShowThemePicker = true
			m.ThemeModalTab = 0
			m.IsPreviewingTheme = false
			m.ThemeStatusMsg = ""
			m.ThemeSearchQuery = ""
			m.ThemeIsSearching = false
			allThemes := theme.GetAllThemes()
			m.ThemeCursor = 0
			for i, t := range allThemes {
				if t.ID == m.Config.Theme || strings.EqualFold(t.Name, m.Config.Theme) || t.ID == m.Theme.ID {
					m.ThemeCursor = i
					break
				}
			}
			return m, m.fetchThemeRegistryCmd()

		case key.Matches(msg, m.KeyMap.Timer):
			m.openTimerModal()

		case key.Matches(msg, m.KeyMap.Plugins):
			m.ShowPluginModal = true
			m.PluginCursor = 0
			m.PluginStatusMsg = ""
			return m, m.fetchRegistryCmd()

		case msg.String() == "tab":
			if m.ActiveTab == 8 && m.Config.ExperimentalTuner {
				m.ActiveTuner = !m.ActiveTuner
				if m.ActiveTuner {
					if st := m.Player.CurrentStation(); st != nil && (m.Player.Status() == player.StatusPlaying || m.Player.Status() == player.StatusConnecting) {
						m.TunerFreq = radio.ExtractOrAssignFrequency(*st, m.TunerBand)
					}
					rssi, _ := tuner.CalculateSignalRSSI(m.Store.GetAllStations(), m.TunerBand, m.TunerFreq)
					m.Player.SetTunerMode(true, rssi, m.TunerFreq, m.TunerBand)
					m.onTunerFreqChanged()
				} else {
					m.Player.SetTunerMode(false, 1.0, m.TunerFreq, m.TunerBand)
				}
			} else if m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3 {
				if m.ActiveFocus == FocusSidebar {
					m.ActiveFocus = FocusMainList
				} else {
					m.ActiveFocus = FocusSidebar
				}
			} else {
				m.SwitchTab((m.ActiveTab + 1) % 9)
			}

		case msg.String() == "shift+tab":
			if m.ActiveTab == 8 && m.Config.ExperimentalTuner {
				m.ActiveTuner = !m.ActiveTuner
				if m.ActiveTuner {
					if st := m.Player.CurrentStation(); st != nil && (m.Player.Status() == player.StatusPlaying || m.Player.Status() == player.StatusConnecting) {
						m.TunerFreq = radio.ExtractOrAssignFrequency(*st, m.TunerBand)
					}
					rssi, _ := tuner.CalculateSignalRSSI(m.Store.GetAllStations(), m.TunerBand, m.TunerFreq)
					m.Player.SetTunerMode(true, rssi, m.TunerFreq, m.TunerBand)
					m.onTunerFreqChanged()
				} else {
					m.Player.SetTunerMode(false, 1.0, m.TunerFreq, m.TunerBand)
				}
			} else if m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3 {
				if m.ActiveFocus == FocusSidebar {
					m.ActiveFocus = FocusMainList
				} else {
					m.ActiveFocus = FocusSidebar
				}
			} else {
				m.SwitchTab((m.ActiveTab - 1 + 9) % 9)
			}

		case key.Matches(msg, m.KeyMap.Up):
			if m.ActiveTab == 8 {
				if m.Config.ExperimentalTuner && m.ActiveTuner {
					cfg := tuner.Bands[m.TunerBand]
					step := 0.5
					if m.TunerBand == "AM" {
						step = 20.0
					} else if m.TunerBand == "SW" {
						step = 0.2
					}
					m.TunerFreq = math.Round((m.TunerFreq+step)*100) / 100
					if m.TunerFreq > cfg.MaxFreq {
						m.TunerFreq = cfg.MaxFreq
					}
					m.onTunerFreqChanged()
				} else {
					m.GlobeLat += 5.0
					if m.GlobeLat > 85.0 {
						m.GlobeLat = 85.0
					}
					m.GlobeStationIndex = 0
				}
			} else if (m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3) && m.ActiveFocus == FocusSidebar {
				if m.ActiveTab == 0 {
					if m.ActivityIndex > 0 {
						m.ActivityIndex--
						if m.ActivityIndex == 0 {
							m.SelectedActivity = ""
						} else {
							m.SelectedActivity = m.Activities[m.ActivityIndex-1].ID
						}
						m.RefreshStations()
					}
				} else if m.ActiveTab == 2 {
					if m.CountryIndex > 0 {
						m.CountryIndex--
						if m.CountryIndex == 0 {
							m.SelectedCountry = ""
						} else {
							m.SelectedCountry = m.Countries[m.CountryIndex-1].Code
						}
						m.RefreshStations()
					}
				} else {
					if m.GenreIndex > 0 {
						m.GenreIndex--
						if m.GenreIndex == 0 {
							m.SelectedGenre = ""
						} else {
							m.SelectedGenre = m.Genres[m.GenreIndex-1]
						}
						m.RefreshStations()
					}
				}
			} else if m.ActiveTab == 7 {
				if m.HistoryIndex > 0 {
					m.HistoryIndex--
				}
			} else {
				if m.SelectedIndex > 0 {
					m.SelectedIndex--
				}
			}

		case key.Matches(msg, m.KeyMap.Down):
			if m.ActiveTab == 8 {
				if m.Config.ExperimentalTuner && m.ActiveTuner {
					cfg := tuner.Bands[m.TunerBand]
					step := 0.5
					if m.TunerBand == "AM" {
						step = 20.0
					} else if m.TunerBand == "SW" {
						step = 0.2
					}
					m.TunerFreq = math.Round((m.TunerFreq-step)*100) / 100
					if m.TunerFreq < cfg.MinFreq {
						m.TunerFreq = cfg.MinFreq
					}
					m.onTunerFreqChanged()
				} else {
					m.GlobeLat -= 5.0
					if m.GlobeLat < -85.0 {
						m.GlobeLat = -85.0
					}
					m.GlobeStationIndex = 0
				}
			} else if (m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3) && m.ActiveFocus == FocusSidebar {
				if m.ActiveTab == 0 {
					if m.ActivityIndex < len(m.Activities) {
						m.ActivityIndex++
						m.SelectedActivity = m.Activities[m.ActivityIndex-1].ID
						m.RefreshStations()
					}
				} else if m.ActiveTab == 2 {
					if m.CountryIndex < len(m.Countries) {
						m.CountryIndex++
						m.SelectedCountry = m.Countries[m.CountryIndex-1].Code
						m.RefreshStations()
					}
				} else {
					if m.GenreIndex < len(m.Genres) {
						m.GenreIndex++
						m.SelectedGenre = m.Genres[m.GenreIndex-1]
						m.RefreshStations()
					}
				}
			} else if m.ActiveTab == 7 {
				hist := m.Store.GetHistory()
				if m.HistoryIndex < len(hist)-1 {
					m.HistoryIndex++
				}
			} else {
				if m.SelectedIndex < len(m.Stations)-1 {
					m.SelectedIndex++
				}
			}
		case key.Matches(msg, m.KeyMap.Left):
			if m.ActiveTab == 8 {
				if m.Config.ExperimentalTuner && m.ActiveTuner {
					cfg := tuner.Bands[m.TunerBand]
					step := 0.1
					if m.TunerBand == "AM" {
						step = 10.0
					} else if m.TunerBand == "SW" {
						step = 0.05
					}
					m.TunerFreq = math.Round((m.TunerFreq-step)*100) / 100
					if m.TunerFreq < cfg.MinFreq {
						m.TunerFreq = cfg.MinFreq
					}
					m.onTunerFreqChanged()
				} else {
					m.GlobeLon -= 6.0
					if m.GlobeLon < -180.0 {
						m.GlobeLon += 360.0
					}
					m.GlobeStationIndex = 0
				}
			} else if m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3 {
				if m.ActiveFocus == FocusMainList {
					m.ActiveFocus = FocusSidebar
				} else if m.ActiveTab > 0 {
					m.SwitchTab(m.ActiveTab - 1)
				}
			} else if m.ActiveTab > 0 {
				m.SwitchTab(m.ActiveTab - 1)
			}

		case key.Matches(msg, m.KeyMap.Right):
			if m.ActiveTab == 8 {
				if m.Config.ExperimentalTuner && m.ActiveTuner {
					cfg := tuner.Bands[m.TunerBand]
					step := 0.1
					if m.TunerBand == "AM" {
						step = 10.0
					} else if m.TunerBand == "SW" {
						step = 0.05
					}
					m.TunerFreq = math.Round((m.TunerFreq+step)*100) / 100
					if m.TunerFreq > cfg.MaxFreq {
						m.TunerFreq = cfg.MaxFreq
					}
					m.onTunerFreqChanged()
				} else {
					m.GlobeLon += 6.0
					if m.GlobeLon > 180.0 {
						m.GlobeLon -= 360.0
					}
					m.GlobeStationIndex = 0
				}
			} else if m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3 {
				if m.ActiveFocus == FocusSidebar {
					m.ActiveFocus = FocusMainList
				} else if m.ActiveTab < 8 {
					m.SwitchTab(m.ActiveTab + 1)
				}
			} else if m.ActiveTab < 8 {
				m.SwitchTab(m.ActiveTab + 1)
			}

		case key.Matches(msg, m.KeyMap.Top):
			if (m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3) && m.ActiveFocus == FocusSidebar {
				if m.ActiveTab == 0 {
					m.ActivityIndex = 0
					m.SelectedActivity = ""
				} else if m.ActiveTab == 2 {
					m.CountryIndex = 0
					m.SelectedCountry = ""
				} else {
					m.GenreIndex = 0
					m.SelectedGenre = ""
				}
				m.RefreshStations()
			} else if m.ActiveTab == 7 {
				m.HistoryIndex = 0
			} else {
				m.SelectedIndex = 0
			}

		case key.Matches(msg, m.KeyMap.Bottom):
			if (m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3) && m.ActiveFocus == FocusSidebar {
				if m.ActiveTab == 0 {
					m.ActivityIndex = len(m.Activities)
					if len(m.Activities) > 0 {
						m.SelectedActivity = m.Activities[len(m.Activities)-1].ID
					}
				} else if m.ActiveTab == 2 {
					m.CountryIndex = len(m.Countries)
					if len(m.Countries) > 0 {
						m.SelectedCountry = m.Countries[len(m.Countries)-1].Code
					}
				} else {
					m.GenreIndex = len(m.Genres)
					if len(m.Genres) > 0 {
						m.SelectedGenre = m.Genres[len(m.Genres)-1]
					}
				}
				m.RefreshStations()
			} else if m.ActiveTab == 7 {
				hist := m.Store.GetHistory()
				if len(hist) > 0 {
					m.HistoryIndex = len(hist) - 1
				}
			} else if len(m.Stations) > 0 {
				m.SelectedIndex = len(m.Stations) - 1
			}

		case key.Matches(msg, m.KeyMap.HalfPageUp):
			if (m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3) && m.ActiveFocus == FocusSidebar {
				if m.ActiveTab == 0 {
					m.ActivityIndex -= 5
					if m.ActivityIndex < 0 {
						m.ActivityIndex = 0
					}
					if m.ActivityIndex == 0 {
						m.SelectedActivity = ""
					} else {
						m.SelectedActivity = m.Activities[m.ActivityIndex-1].ID
					}
				} else if m.ActiveTab == 2 {
					m.CountryIndex -= 5
					if m.CountryIndex < 0 {
						m.CountryIndex = 0
					}
					if m.CountryIndex == 0 {
						m.SelectedCountry = ""
					} else {
						m.SelectedCountry = m.Countries[m.CountryIndex-1].Code
					}
				} else {
					m.GenreIndex -= 5
					if m.GenreIndex < 0 {
						m.GenreIndex = 0
					}
					if m.GenreIndex == 0 {
						m.SelectedGenre = ""
					} else {
						m.SelectedGenre = m.Genres[m.GenreIndex-1]
					}
				}
				m.RefreshStations()
			} else if m.ActiveTab == 7 {
				m.HistoryIndex -= 5
				if m.HistoryIndex < 0 {
					m.HistoryIndex = 0
				}
			} else {
				m.SelectedIndex -= 5
				if m.SelectedIndex < 0 {
					m.SelectedIndex = 0
				}
			}

		case key.Matches(msg, m.KeyMap.HalfPageDown):
			if (m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3) && m.ActiveFocus == FocusSidebar {
				if m.ActiveTab == 0 {
					m.ActivityIndex += 5
					if m.ActivityIndex > len(m.Activities) {
						m.ActivityIndex = len(m.Activities)
					}
					if m.ActivityIndex == 0 {
						m.SelectedActivity = ""
					} else {
						m.SelectedActivity = m.Activities[m.ActivityIndex-1].ID
					}
				} else if m.ActiveTab == 2 {
					m.CountryIndex += 5
					if m.CountryIndex > len(m.Countries) {
						m.CountryIndex = len(m.Countries)
					}
					if m.CountryIndex == 0 {
						m.SelectedCountry = ""
					} else {
						m.SelectedCountry = m.Countries[m.CountryIndex-1].Code
					}
				} else {
					m.GenreIndex += 5
					if m.GenreIndex > len(m.Genres) {
						m.GenreIndex = len(m.Genres)
					}
					if m.GenreIndex == 0 {
						m.SelectedGenre = ""
					} else {
						m.SelectedGenre = m.Genres[m.GenreIndex-1]
					}
				}
				m.RefreshStations()
			} else if m.ActiveTab == 7 {
				hist := m.Store.GetHistory()
				m.HistoryIndex += 5
				if m.HistoryIndex >= len(hist) {
					if len(hist) > 0 {
						m.HistoryIndex = len(hist) - 1
					} else {
						m.HistoryIndex = 0
					}
				}
			} else {
				m.SelectedIndex += 5
				if m.SelectedIndex >= len(m.Stations) {
					if len(m.Stations) > 0 {
						m.SelectedIndex = len(m.Stations) - 1
					} else {
						m.SelectedIndex = 0
					}
				}
			}

		case msg.String() == "1":
			m.SwitchTab(0)
		case msg.String() == "2":
			m.SwitchTab(1)
		case msg.String() == "3":
			m.SwitchTab(2)
		case msg.String() == "4":
			m.SwitchTab(3)
		case msg.String() == "5":
			m.SwitchTab(4)
		case msg.String() == "6":
			m.SwitchTab(5)
			if len(m.RBStations) == 0 {
				m.StatusMessage = "Fetching top stations from RadioBrowser..."
				return m, m.searchRadioBrowserCmd("")
			}
		case msg.String() == "7":
			m.SwitchTab(6)
		case msg.String() == "8", key.Matches(msg, m.KeyMap.HistoryTab):
			m.SwitchTab(7)
		case msg.String() == "9", key.Matches(msg, m.KeyMap.GlobeTab):
			m.SwitchTab(8)
			m.ActiveTuner = false
			m.Player.SetTunerMode(false, 1.0, m.TunerFreq, m.TunerBand)
		case msg.String() == "0":
			if m.Config.ExperimentalTuner {
				m.SwitchTab(8)
				m.ActiveTuner = true
				if st := m.Player.CurrentStation(); st != nil && (m.Player.Status() == player.StatusPlaying || m.Player.Status() == player.StatusConnecting) {
					m.TunerFreq = radio.ExtractOrAssignFrequency(*st, m.TunerBand)
				}
				rssi, _ := tuner.CalculateSignalRSSI(m.Store.GetAllStations(), m.TunerBand, m.TunerFreq)
				m.Player.SetTunerMode(true, rssi, m.TunerFreq, m.TunerBand)
				m.onTunerFreqChanged()
			} else {
				isMuted := m.Player.ToggleMute()
				if isMuted {
					m.StatusMessage = "Muted"
				} else {
					m.StatusMessage = "Unmuted"
				}
				m.SyncDesktop()
			}
		case key.Matches(msg, m.KeyMap.FrequencyMode):
			if m.Config.ExperimentalTuner {
				if m.ActiveTab != 8 || !m.ActiveTuner {
					m.SwitchTab(8)
					m.ActiveTuner = true
					if st := m.Player.CurrentStation(); st != nil && (m.Player.Status() == player.StatusPlaying || m.Player.Status() == player.StatusConnecting) {
						m.TunerFreq = radio.ExtractOrAssignFrequency(*st, m.TunerBand)
					}
					rssi, _ := tuner.CalculateSignalRSSI(m.Store.GetAllStations(), m.TunerBand, m.TunerFreq)
					m.Player.SetTunerMode(true, rssi, m.TunerFreq, m.TunerBand)
					m.onTunerFreqChanged()
				} else {
					m.ActiveTuner = false
					m.Player.SetTunerMode(false, 1.0, m.TunerFreq, m.TunerBand)
				}
			}
		case key.Matches(msg, m.KeyMap.BandSwitch):
			if m.ActiveTab == 8 && m.Config.ExperimentalTuner && m.ActiveTuner {
				m.TunerBand = tuner.NextBand(m.TunerBand)
				if m.TunerBand == "AM" {
					m.TunerFreq = 720.0
				} else if m.TunerBand == "SW" {
					m.TunerFreq = 10.0
				} else {
					m.TunerFreq = 93.9
				}
				m.onTunerFreqChanged()
			}

		case key.Matches(msg, m.KeyMap.FastSweepLeft):
			if m.ActiveTab == 8 && m.Config.ExperimentalTuner && m.ActiveTuner {
				step := 1.0
				if m.TunerBand == "AM" {
					step = 50.0
				} else if m.TunerBand == "SW" {
					step = 0.5
				}
				m.TunerFreq = math.Round((m.TunerFreq-step)*100) / 100
				cfg := tuner.Bands[m.TunerBand]
				if m.TunerFreq < cfg.MinFreq {
					m.TunerFreq = cfg.MinFreq
				}
				m.onTunerFreqChanged()
			} else {
				m.SwitchTab(7)
			}

		case key.Matches(msg, m.KeyMap.FastSweepRight):
			if m.ActiveTab == 8 && m.Config.ExperimentalTuner && m.ActiveTuner {
				step := 1.0
				if m.TunerBand == "AM" {
					step = 50.0
				} else if m.TunerBand == "SW" {
					step = 0.5
				}
				m.TunerFreq = math.Round((m.TunerFreq-step)*100) / 100
				cfg := tuner.Bands[m.TunerBand]
				if m.TunerFreq > cfg.MaxFreq {
					m.TunerFreq = cfg.MaxFreq
				}
				m.onTunerFreqChanged()
			}

		case key.Matches(msg, m.KeyMap.Activity):
			if m.ActiveTab != 0 {
				m.SwitchTab(0)
			} else {
				if m.ActiveFocus == FocusSidebar {
					m.ActiveFocus = FocusMainList
				} else {
					m.ActiveFocus = FocusSidebar
				}
			}

		case key.Matches(msg, m.KeyMap.CountryTab):
			if m.ActiveTab != 2 {
				m.SwitchTab(2)
			} else {
				if m.ActiveFocus == FocusSidebar {
					m.ActiveFocus = FocusMainList
				} else {
					m.ActiveFocus = FocusSidebar
				}
			}

		case key.Matches(msg, m.KeyMap.Category):
			if m.ActiveTab == 7 {
				m.Store.ClearHistory()
				m.HistoryIndex = 0
				m.StatusMessage = "Cleared track history log"
			} else if m.ActiveTab != 3 {
				m.SwitchTab(3)
			} else {
				if m.ActiveFocus == FocusSidebar {
					m.ActiveFocus = FocusMainList
				} else {
					m.ActiveFocus = FocusSidebar
				}
			}

		case key.Matches(msg, m.KeyMap.NextStation):
			if m.ActiveTab == 8 {
				if m.Config.ExperimentalTuner && m.ActiveTuner {
					m.seekTunerStation(true)
				} else {
					m.cycleGlobeStation(true)
				}
			} else if m.ActiveTab != 7 {
				m.PlayNextStation()
			}

		case key.Matches(msg, m.KeyMap.PrevStation):
			if m.ActiveTab == 8 {
				if m.Config.ExperimentalTuner && m.ActiveTuner {
					m.seekTunerStation(false)
				} else {
					m.cycleGlobeStation(false)
				}
			} else if m.ActiveTab != 7 {
				m.PlayPrevStation()
			}

		case key.Matches(msg, m.KeyMap.PlayPause):
			if m.ActiveTab == 8 {
				if m.Config.ExperimentalTuner && m.ActiveTuner {
					if m.Player.Status() == player.StatusPlaying || m.Player.Status() == player.StatusConnecting {
						_ = m.Player.Pause()
						m.PlayingID = ""
						m.StatusMessage = "Tuner audio stream muted / paused [Space: Resume]"
						m.SyncDesktop()
					} else {
						rssi, locked := tuner.CalculateSignalRSSI(m.Store.GetAllStations(), m.TunerBand, m.TunerFreq)
						if locked != nil && rssi >= 0.20 {
							m.PlayingID = locked.ID
							_ = m.Player.Play(*locked)
							m.Player.UpdateTunerSignal(rssi, m.TunerFreq, m.TunerBand)
							m.StatusMessage = fmt.Sprintf("Resumed %s (Signal: %.0f%%)", locked.Name, rssi*100)
							m.SyncDesktop()
						} else {
							m.StatusMessage = fmt.Sprintf("Dial at %.1f %s - Atmospheric Static (sweep dial to receive)", m.TunerFreq, tuner.Bands[m.TunerBand].Unit)
						}
					}
				} else {
					nearest, _, distKm := radio.FindNearestCluster(m.GlobeClusters, m.GlobeLat, m.GlobeLon)
					if nearest != nil && distKm <= 1200 && len(nearest.Stations) > 0 {
						idx := m.GlobeStationIndex
						if idx < 0 || idx >= len(nearest.Stations) {
							idx = 0
						}
						st := nearest.Stations[idx]
						if m.PlayingID == st.ID && m.Player.Status() == player.StatusPlaying {
							_ = m.Player.Pause()
							m.PlayingID = ""
							m.StatusMessage = fmt.Sprintf("Paused %s", st.Name)
							m.SyncDesktop()
						} else {
							_ = m.Player.Play(st)
							m.PlayingID = st.ID
							m.StatusMessage = fmt.Sprintf("Playing %s [%s]", st.Name, m.Player.ActiveBackend())
							m.SyncDesktop()
							if m.Config.SongNotifications && m.Desktop != nil {
								m.Desktop.NotifySong(st.Name, st.Name)
							}
						}
					} else {
						m.StatusMessage = "No broadcast stations near crosshair (rotate globe to explore)"
					}
				}
			} else if m.ActiveTab == 7 {
				hist := m.Store.GetHistory()
				if len(hist) > 0 && m.HistoryIndex < len(hist) {
					entry := hist[m.HistoryIndex]
					// Find station in store to tune in
					var targetStation *radio.Station
					for _, st := range m.Store.GetAllStations() {
						if (entry.StationID != "" && st.ID == entry.StationID) ||
							strings.EqualFold(st.Name, entry.StationName) {
							targetStation = &st
							break
						}
					}
					if targetStation != nil {
						_ = m.Player.Play(*targetStation)
						m.PlayingID = targetStation.ID
						m.StatusMessage = fmt.Sprintf("Playing %s [%s]", targetStation.Name, m.Player.ActiveBackend())
						m.SyncDesktop()
						if m.Config.SongNotifications && m.Desktop != nil {
							m.Desktop.NotifySong(targetStation.Name, targetStation.Name)
						}
					} else {
						m.StatusMessage = fmt.Sprintf("Track '%s' recorded from %s", entry.FullDisplay(), entry.StationName)
					}
				}
			} else if (m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3) && m.ActiveFocus == FocusSidebar {
				m.ActiveFocus = FocusMainList
				m.SelectedIndex = 0
			} else if len(m.Stations) > 0 && m.SelectedIndex < len(m.Stations) {
				st := m.Stations[m.SelectedIndex]
				if m.PlayingID == st.ID && m.Player.Status() == player.StatusPlaying {
					if m.PartySession != nil && m.PartySession.IsActive() && !m.PartySession.CanChangeStation() {
						m.StatusMessage = fmt.Sprintf("🚫 DJ Pass is Host Only (@%s is DJ)", m.PartySession.HostNickname())
						return m, nil
					}
					_ = m.Player.Pause()
					m.PlayingID = ""
					m.StatusMessage = fmt.Sprintf("Paused %s", st.Name)
					m.SyncDesktop()
					if m.PartySession != nil && m.PartySession.IsActive() {
						_ = m.PartySession.BroadcastStationChange(st.ID, st.Name, st.URL, st.Genre, st.Country, st.Codec, st.Bitrate, "paused")
					}
				} else {
					if m.PartySession != nil && m.PartySession.IsActive() && !m.PartySession.CanChangeStation() {
						m.StatusMessage = fmt.Sprintf("🚫 DJ Pass is Host Only (@%s is DJ)", m.PartySession.HostNickname())
						return m, nil
					}
					_ = m.Player.Play(st)
					m.PlayingID = st.ID
					m.StatusMessage = fmt.Sprintf("Playing %s [%s]", st.Name, m.Player.ActiveBackend())
					m.SyncDesktop()
					if m.PartySession != nil && m.PartySession.IsActive() {
						_ = m.PartySession.BroadcastStationChange(st.ID, st.Name, st.URL, st.Genre, st.Country, st.Codec, st.Bitrate, "playing")
					}
					if m.Config.SongNotifications && m.Desktop != nil {
						m.Desktop.NotifySong(st.Name, st.Name)
					}
				}
			}

		case key.Matches(msg, m.KeyMap.Stop):
			if m.ActiveTab == 7 {
				hist := m.Store.GetHistory()
				if len(hist) > 0 && m.HistoryIndex < len(hist) {
					entry := hist[m.HistoryIndex]
					err := m.Store.SaveTrackBookmark(entry)
					if err == nil {
						m.StatusMessage = fmt.Sprintf("⭐ Bookmarked '%s' to %s", entry.FullDisplay(), util.GetSavedTracksFile())
					} else {
						m.StatusMessage = fmt.Sprintf("Error saving bookmark: %v", err)
					}
				}
			} else {
				_ = m.Player.Stop()
				m.PlayingID = ""
				m.IdentifiedResult = nil
				m.IsIdentifying = false
				m.StatusMessage = "Audio playback stopped"
				m.SyncDesktop()
			}

		case key.Matches(msg, m.KeyMap.YankTrack):
			var trackToCopy string
			if m.ActiveTab == 7 {
				hist := m.Store.GetHistory()
				if len(hist) > 0 && m.HistoryIndex < len(hist) {
					trackToCopy = hist[m.HistoryIndex].FullDisplay()
				}
			} else {
				if m.Player.Status() == player.StatusPlaying || m.Player.Status() == player.StatusConnecting {
					if m.IdentifiedResult != nil {
						trackToCopy = m.IdentifiedResult.SimpleTitle()
					} else {
						trackToCopy = m.Player.CurrentTrack()
						if trackToCopy == "" && m.Player.CurrentStation() != nil {
							trackToCopy = m.Player.CurrentStation().Name
						}
					}
				} else if len(m.Stations) > 0 && m.SelectedIndex < len(m.Stations) {
					trackToCopy = m.Stations[m.SelectedIndex].Name
				}
			}

			if trackToCopy != "" {
				_ = util.CopyToClipboard(trackToCopy)
				m.StatusMessage = fmt.Sprintf("📋 Copied '%s' to clipboard!", trackToCopy)
			} else {
				m.StatusMessage = "No track metadata available to copy"
			}

		case key.Matches(msg, m.KeyMap.OpenSearch):
			var trackToSearch string
			if m.ActiveTab == 7 {
				hist := m.Store.GetHistory()
				if len(hist) > 0 && m.HistoryIndex < len(hist) {
					trackToSearch = hist[m.HistoryIndex].FullDisplay()
				}
			} else {
				if m.Player.Status() == player.StatusPlaying || m.Player.Status() == player.StatusConnecting {
					if m.IdentifiedResult != nil {
						trackToSearch = m.IdentifiedResult.SimpleTitle()
					} else {
						trackToSearch = m.Player.CurrentTrack()
						if trackToSearch == "" && m.Player.CurrentStation() != nil {
							trackToSearch = m.Player.CurrentStation().Name
						}
					}
				} else if len(m.Stations) > 0 && m.SelectedIndex < len(m.Stations) {
					trackToSearch = m.Stations[m.SelectedIndex].Name
				}
			}

			if trackToSearch != "" {
				provider := m.Config.SearchProvider
				searchURL := util.BuildSearchURL(provider, trackToSearch)
				go func(targetURL string) {
					_ = util.OpenURL(targetURL)
				}(searchURL)
				m.StatusMessage = fmt.Sprintf("🌐 Opening search for '%s'...", trackToSearch)
			} else {
				m.StatusMessage = "No track metadata available to search"
			}

		case key.Matches(msg, m.KeyMap.IdentifyTrack):
			if m.Player.Status() != player.StatusPlaying && m.Player.Status() != player.StatusConnecting {
				m.StatusMessage = "Cannot identify track: audio is not playing"
				return m, nil
			}
			st := m.Player.CurrentStation()
			if st == nil {
				m.StatusMessage = "No active station to identify"
				return m, nil
			}
			if m.IsIdentifying {
				m.StatusMessage = "Already identifying stream audio..."
				return m, nil
			}
			m.IsIdentifying = true
			m.LastFingerprintTime = time.Now()
			m.StatusMessage = "🔍 Sampling stream & identifying with AcoustID..."
			return m, m.identifyTrackCmd()

		case key.Matches(msg, m.KeyMap.RandomPlay):
			if len(m.Stations) > 0 {
				idx := rand.Intn(len(m.Stations))
				m.SelectedIndex = idx
				st := m.Stations[idx]
				_ = m.Player.Play(st)
				m.PlayingID = st.ID
				m.StatusMessage = fmt.Sprintf("Playing Random: %s", st.Name)
				m.SyncDesktop()
				if m.Config.SongNotifications && m.Desktop != nil {
					m.Desktop.NotifySong(st.Name, st.Name)
				}
			}

		case key.Matches(msg, m.KeyMap.VolUp), key.Matches(msg, m.KeyMap.ZoomIn):
			if m.ActiveTab == 8 && !(m.Config.ExperimentalTuner && m.ActiveTuner) {
				m.GlobeZoom += 0.25
				if m.GlobeZoom > 3.0 {
					m.GlobeZoom = 3.0
				}
				zoomDesc := "Continents"
				if m.GlobeZoom >= 2.0 {
					zoomDesc = "Cities / Local"
				} else if m.GlobeZoom >= 1.25 {
					zoomDesc = "Countries"
				} else if m.GlobeZoom >= 0.8 {
					zoomDesc = "Hemisphere"
				}
				m.StatusMessage = fmt.Sprintf("Globe Zoom: %.2fx (%s)", m.GlobeZoom, zoomDesc)
			} else {
				v := m.Player.SetVolume(m.Player.Volume() + 5)
				m.StatusMessage = fmt.Sprintf("Volume: %d%%", v)
				m.SyncDesktop()
			}

		case key.Matches(msg, m.KeyMap.VolDown), key.Matches(msg, m.KeyMap.ZoomOut):
			if m.ActiveTab == 8 && !(m.Config.ExperimentalTuner && m.ActiveTuner) {
				m.GlobeZoom -= 0.25
				if m.GlobeZoom < 0.5 {
					m.GlobeZoom = 0.5
				}
				zoomDesc := "Continents"
				if m.GlobeZoom >= 2.0 {
					zoomDesc = "Cities / Local"
				} else if m.GlobeZoom >= 1.25 {
					zoomDesc = "Countries"
				} else if m.GlobeZoom >= 0.8 {
					zoomDesc = "Hemisphere"
				}
				m.StatusMessage = fmt.Sprintf("Globe Zoom: %.2fx (%s)", m.GlobeZoom, zoomDesc)
			} else {
				v := m.Player.SetVolume(m.Player.Volume() - 5)
				m.StatusMessage = fmt.Sprintf("Volume: %d%%", v)
				m.SyncDesktop()
			}

		case key.Matches(msg, m.KeyMap.Mute):
			isMuted := m.Player.ToggleMute()
			if isMuted {
				m.StatusMessage = "Muted"
			} else {
				m.StatusMessage = fmt.Sprintf("Unmuted (%d%%)", m.Player.Volume())
			}
			m.SyncDesktop()

		case key.Matches(msg, m.KeyMap.Search):
			m.IsSearching = true

		case key.Matches(msg, m.KeyMap.Clear):
			if m.SearchQuery != "" {
				m.SearchQuery = ""
				m.RefreshStations()
			}

		case key.Matches(msg, m.KeyMap.Favorite):
			if len(m.Stations) > 0 && m.SelectedIndex < len(m.Stations) {
				st := m.Stations[m.SelectedIndex]
				isFav := m.Store.ToggleFavorite(st)
				m.RefreshStations()
				if isFav {
					m.StatusMessage = fmt.Sprintf("Added '%s' to Favorites ⭐", st.Name)
				} else {
					m.StatusMessage = fmt.Sprintf("Removed '%s' from Favorites", st.Name)
				}
			}

		case key.Matches(msg, m.KeyMap.ExportPR):
			if m.ActiveTab == 8 {
				if m.Config.ExperimentalTuner && m.ActiveTuner {
					m.seekTunerStation(false)
				} else {
					m.cycleGlobeStation(false)
				}
			} else if len(m.Stations) > 0 && m.SelectedIndex < len(m.Stations) {
				st := m.Stations[m.SelectedIndex]
				m.ExportStation = st
				snippet := st.ToYAMLSnippet()
				_ = util.CopyToClipboard(snippet)
				m.ShowPRExport = true
			}

		case key.Matches(msg, m.KeyMap.AddStation):
			m.EditingStationID = ""
			m.AddInputs = make([]string, 7)
			m.AddFocusIdx = 0
			m.AddErrMsg = ""
			m.ShowAddModal = true

		case key.Matches(msg, m.KeyMap.EditStation):
			if len(m.Stations) > 0 && m.SelectedIndex < len(m.Stations) {
				st := m.Stations[m.SelectedIndex]
				m.EditingStationID = st.ID
				m.AddInputs = []string{
					st.Name,
					st.URL,
					st.Genre,
					st.Country,
					st.City,
					st.Broadcast,
					fmt.Sprintf("%d", st.Bitrate),
				}
				m.AddFocusIdx = 0
				m.AddErrMsg = ""
				m.ShowAddModal = true
			}

		case key.Matches(msg, m.KeyMap.DelStation):
			if len(m.Stations) > 0 && m.SelectedIndex < len(m.Stations) {
				st := m.Stations[m.SelectedIndex]
				if st.Source == "local" {
					_ = m.Store.DeleteLocalStation(st.ID)
					m.RefreshStations()
					m.StatusMessage = fmt.Sprintf("Deleted local station '%s'", st.Name)
				} else {
					m.StatusMessage = "Cannot delete bundled stations. (You can remove local stations only)"
				}
			}

		case key.Matches(msg, m.KeyMap.Visualizer):
			mode := m.Visualizer.CycleMode()
			m.StatusMessage = fmt.Sprintf("Visualizer mode: %s", mode)
			m.SyncDesktop()
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) openTimerModal() {
	m.ShowTimerModal = true
	m.TimerModalScreen = 0
	m.TimerMenuCursor = 0
	m.TimerPomodoroInputs = []string{
		fmt.Sprintf("%d", int(m.Timer.PomodoroCfg.FocusDuration.Minutes())),
		fmt.Sprintf("%d", int(m.Timer.PomodoroCfg.ShortBreakDuration.Minutes())),
		fmt.Sprintf("%d", int(m.Timer.PomodoroCfg.LongBreakDuration.Minutes())),
		fmt.Sprintf("%d", m.Timer.PomodoroCfg.CyclesBeforeLongBreak),
		m.Timer.PomodoroCfg.FocusStationID,
		m.Timer.PomodoroCfg.BreakStationID,
		m.Timer.PomodoroCfg.CommandHook,
	}
	m.TimerPomodoroFocusIdx = 0
	m.TimerPomodoroNotifyDesktop = m.Timer.PomodoroCfg.NotifyDesktop
	m.TimerPomodoroNotifyBell = m.Timer.PomodoroCfg.NotifyTerminalBell
}

func (m *Model) applyTheme(name string) {
	m.Config.Theme = name
	m.Theme = theme.GetTheme(name)
	m.IsPreviewingTheme = false
	m.ShowThemePicker = false
	m.StatusMessage = fmt.Sprintf("Theme changed to %s", m.Theme.Name)
	_ = util.SaveConfig(m.Config)
}

func (m Model) getFilteredRegistryThemes() []theme.RegistryTheme {
	var filtered []theme.RegistryTheme
	q := strings.ToLower(strings.TrimSpace(m.ThemeSearchQuery))
	for _, rt := range m.ThemeRegistryList {
		if q == "" ||
			strings.Contains(strings.ToLower(rt.Name), q) ||
			strings.Contains(strings.ToLower(rt.Author), q) ||
			strings.Contains(strings.ToLower(rt.Description), q) ||
			strings.Contains(strings.ToLower(rt.Category), q) ||
			strings.Contains(strings.ToLower(rt.ID), q) {
			filtered = append(filtered, rt)
		}
	}
	return filtered
}

func (m Model) fetchThemeRegistryCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		client := m.ThemeClient
		if client == nil {
			client = theme.NewRegistryClient(m.Config.ThemeRegistryURL)
		}

		index, err := client.FetchRegistry(ctx)
		if err != nil {
			return ThemeRegistryLoadedMsg{
				Themes: index.Themes,
				Err:    err,
			}
		}
		return ThemeRegistryLoadedMsg{
			Themes: index.Themes,
			Err:    nil,
		}
	}
}

func (m Model) installThemeCmd(th theme.RegistryTheme) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		client := m.ThemeClient
		if client == nil {
			client = theme.NewRegistryClient(m.Config.ThemeRegistryURL)
		}

		err := client.DownloadAndInstall(ctx, th, util.GetThemesDir())
		if err != nil {
			return ThemeInstalledMsg{ThemeID: th.ID, Err: err}
		}
		return ThemeInstalledMsg{ThemeID: th.ID, Err: nil}
	}
}

func (m Model) deleteThemeCmd(themeID string) tea.Cmd {
	return func() tea.Msg {
		client := m.ThemeClient
		if client == nil {
			client = theme.NewRegistryClient(m.Config.ThemeRegistryURL)
		}

		err := client.Uninstall(themeID, util.GetThemesDir())
		return ThemeDeletedMsg{ThemeID: themeID, Err: err}
	}
}

func (m Model) handleThemePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	allThemes := theme.GetAllThemes()
	filteredReg := m.getFilteredRegistryThemes()

	// Search filter mode in Community Hub
	if m.ThemeIsSearching {
		switch msg.String() {
		case "esc", "enter":
			m.ThemeIsSearching = false
			return m, nil
		case "backspace":
			if len(m.ThemeSearchQuery) > 0 {
				m.ThemeSearchQuery = m.ThemeSearchQuery[:len(m.ThemeSearchQuery)-1]
				m.ThemeRegistryCursor = 0
			} else {
				m.ThemeIsSearching = false
			}
			return m, nil
		default:
			if len(msg.String()) == 1 {
				m.ThemeSearchQuery += msg.String()
				m.ThemeRegistryCursor = 0
			}
			return m, nil
		}
	}

	switch msg.String() {
	case "esc", "q":
		if m.IsPreviewingTheme {
			m.Theme = m.PreviewOriginalTheme
			m.IsPreviewingTheme = false
		}
		m.ShowThemePicker = false
		m.ThemeStatusMsg = ""
		m.ThemeSearchQuery = ""
		return m, nil

	case "tab":
		m.ThemeModalTab = (m.ThemeModalTab + 1) % 2
		if m.IsPreviewingTheme {
			if m.ThemeModalTab == 0 && len(allThemes) > 0 && m.ThemeCursor < len(allThemes) {
				m.Theme = allThemes[m.ThemeCursor]
			} else if m.ThemeModalTab == 1 && len(filteredReg) > 0 && m.ThemeRegistryCursor < len(filteredReg) {
				m.Theme = filteredReg[m.ThemeRegistryCursor].ToTheme()
			}
		}
		return m, nil

	case "shift+tab":
		m.ThemeModalTab = (m.ThemeModalTab - 1 + 2) % 2
		if m.IsPreviewingTheme {
			if m.ThemeModalTab == 0 && len(allThemes) > 0 && m.ThemeCursor < len(allThemes) {
				m.Theme = allThemes[m.ThemeCursor]
			} else if m.ThemeModalTab == 1 && len(filteredReg) > 0 && m.ThemeRegistryCursor < len(filteredReg) {
				m.Theme = filteredReg[m.ThemeRegistryCursor].ToTheme()
			}
		}
		return m, nil

	case "p", "P":
		// Toggle interactive live preview in TUI
		if !m.IsPreviewingTheme {
			m.PreviewOriginalTheme = theme.GetTheme(m.Config.Theme)
			m.IsPreviewingTheme = true
			if m.ThemeModalTab == 0 && len(allThemes) > 0 && m.ThemeCursor < len(allThemes) {
				m.Theme = allThemes[m.ThemeCursor]
			} else if m.ThemeModalTab == 1 && len(filteredReg) > 0 && m.ThemeRegistryCursor < len(filteredReg) {
				m.Theme = filteredReg[m.ThemeRegistryCursor].ToTheme()
			}
			m.ThemeStatusMsg = fmt.Sprintf("👁️ Live previewing %s! Press [p/Esc] to revert, [Enter/i] to apply.", m.Theme.Name)
		} else {
			m.Theme = m.PreviewOriginalTheme
			m.IsPreviewingTheme = false
			m.ThemeStatusMsg = "Reverted live preview"
		}
		return m, nil

	case "j", "down":
		if m.ThemeModalTab == 0 {
			if m.ThemeCursor < len(allThemes)-1 {
				m.ThemeCursor++
			}
			if m.IsPreviewingTheme && m.ThemeCursor < len(allThemes) {
				m.Theme = allThemes[m.ThemeCursor]
			}
		} else {
			if m.ThemeRegistryCursor < len(filteredReg)-1 {
				m.ThemeRegistryCursor++
			}
			if m.IsPreviewingTheme && len(filteredReg) > 0 && m.ThemeRegistryCursor < len(filteredReg) {
				m.Theme = filteredReg[m.ThemeRegistryCursor].ToTheme()
			}
		}
		return m, nil

	case "k", "up":
		if m.ThemeModalTab == 0 {
			if m.ThemeCursor > 0 {
				m.ThemeCursor--
			}
			if m.IsPreviewingTheme && m.ThemeCursor < len(allThemes) {
				m.Theme = allThemes[m.ThemeCursor]
			}
		} else {
			if m.ThemeRegistryCursor > 0 {
				m.ThemeRegistryCursor--
			}
			if m.IsPreviewingTheme && len(filteredReg) > 0 && m.ThemeRegistryCursor < len(filteredReg) {
				m.Theme = filteredReg[m.ThemeRegistryCursor].ToTheme()
			}
		}
		return m, nil

	case "g", "home":
		if m.ThemeModalTab == 0 {
			m.ThemeCursor = 0
			if m.IsPreviewingTheme && len(allThemes) > 0 {
				m.Theme = allThemes[0]
			}
		} else {
			m.ThemeRegistryCursor = 0
			if m.IsPreviewingTheme && len(filteredReg) > 0 {
				m.Theme = filteredReg[0].ToTheme()
			}
		}
		return m, nil

	case "G", "end":
		if m.ThemeModalTab == 0 {
			if len(allThemes) > 0 {
				m.ThemeCursor = len(allThemes) - 1
			}
			if m.IsPreviewingTheme && len(allThemes) > 0 {
				m.Theme = allThemes[m.ThemeCursor]
			}
		} else {
			if len(filteredReg) > 0 {
				m.ThemeRegistryCursor = len(filteredReg) - 1
			}
			if m.IsPreviewingTheme && len(filteredReg) > 0 {
				m.Theme = filteredReg[m.ThemeRegistryCursor].ToTheme()
			}
		}
		return m, nil

	case "ctrl+u", "pgup":
		if m.ThemeModalTab == 0 {
			m.ThemeCursor -= 5
			if m.ThemeCursor < 0 {
				m.ThemeCursor = 0
			}
			if m.IsPreviewingTheme && len(allThemes) > 0 {
				m.Theme = allThemes[m.ThemeCursor]
			}
		} else {
			m.ThemeRegistryCursor -= 5
			if m.ThemeRegistryCursor < 0 {
				m.ThemeRegistryCursor = 0
			}
			if m.IsPreviewingTheme && len(filteredReg) > 0 {
				m.Theme = filteredReg[m.ThemeRegistryCursor].ToTheme()
			}
		}
		return m, nil

	case "ctrl+d", "pgdown":
		if m.ThemeModalTab == 0 {
			m.ThemeCursor += 5
			if m.ThemeCursor >= len(allThemes) {
				if len(allThemes) > 0 {
					m.ThemeCursor = len(allThemes) - 1
				} else {
					m.ThemeCursor = 0
				}
			}
			if m.IsPreviewingTheme && len(allThemes) > 0 {
				m.Theme = allThemes[m.ThemeCursor]
			}
		} else {
			m.ThemeRegistryCursor += 5
			if m.ThemeRegistryCursor >= len(filteredReg) {
				if len(filteredReg) > 0 {
					m.ThemeRegistryCursor = len(filteredReg) - 1
				} else {
					m.ThemeRegistryCursor = 0
				}
			}
			if m.IsPreviewingTheme && len(filteredReg) > 0 {
				m.Theme = filteredReg[m.ThemeRegistryCursor].ToTheme()
			}
		}
		return m, nil

	case "enter", " ":
		if m.ThemeModalTab == 0 {
			if len(allThemes) > 0 && m.ThemeCursor < len(allThemes) {
				m.IsPreviewingTheme = false
				m.applyTheme(allThemes[m.ThemeCursor].ID)
			}
			return m, nil
		}
		// In Community Hub, Enter downloads and applies
		if len(filteredReg) > 0 && m.ThemeRegistryCursor < len(filteredReg) {
			target := filteredReg[m.ThemeRegistryCursor]
			m.IsPreviewingTheme = false
			m.ThemeStatusMsg = fmt.Sprintf("Downloading %s...", target.Name)
			return m, m.installThemeCmd(target)
		}
		return m, nil

	case "i", "I":
		if m.ThemeModalTab == 1 {
			if len(filteredReg) > 0 && m.ThemeRegistryCursor < len(filteredReg) {
				target := filteredReg[m.ThemeRegistryCursor]
				m.IsPreviewingTheme = false
				m.ThemeStatusMsg = fmt.Sprintf("Downloading %s...", target.Name)
				return m, m.installThemeCmd(target)
			}
		} else {
			if len(allThemes) > 0 && m.ThemeCursor < len(allThemes) {
				m.IsPreviewingTheme = false
				m.applyTheme(allThemes[m.ThemeCursor].ID)
			}
		}
		return m, nil

	case "/":
		if m.ThemeModalTab == 1 {
			m.ThemeIsSearching = true
			return m, nil
		}

	case "r", "R":
		if m.ThemeModalTab == 1 {
			m.ThemeStatusMsg = "Refreshing community themes from repository..."
			return m, m.fetchThemeRegistryCmd()
		}

	case "d", "D":
		if m.ThemeModalTab == 0 {
			if len(allThemes) > 0 && m.ThemeCursor < len(allThemes) {
				target := allThemes[m.ThemeCursor]
				if target.IsCustom {
					m.ThemeStatusMsg = fmt.Sprintf("Deleting %s...", target.Name)
					return m, m.deleteThemeCmd(target.ID)
				}
				m.ThemeStatusMsg = "Cannot delete built-in theme"
			}
		} else {
			if len(filteredReg) > 0 && m.ThemeRegistryCursor < len(filteredReg) {
				target := filteredReg[m.ThemeRegistryCursor]
				m.ThemeStatusMsg = fmt.Sprintf("Deleting %s...", target.Name)
				return m, m.deleteThemeCmd(target.ID)
			}
		}
		return m, nil

	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		if m.ThemeModalTab == 0 {
			idx := int(msg.String()[0] - '1')
			if idx >= 0 && idx < len(allThemes) {
				m.ThemeCursor = idx
				m.IsPreviewingTheme = false
				m.applyTheme(allThemes[idx].ID)
			}
			return m, nil
		}
		if msg.String() == "1" {
			m.ThemeModalTab = 0
			return m, nil
		}

	case "e", "E":
		exportPath, err := theme.ExportActiveTheme(util.GetThemesDir(), m.Theme)
		if err == nil {
			m.StatusMessage = fmt.Sprintf("✓ Exported theme to %s", exportPath)
			m.ThemeStatusMsg = fmt.Sprintf("✓ Exported to %s", filepath.Base(exportPath))
			_, _ = theme.LoadCustomThemes(util.GetThemesDir())
		} else {
			m.StatusMessage = fmt.Sprintf("Error exporting theme: %v", err)
			m.ThemeStatusMsg = fmt.Sprintf("Export error: %v", err)
		}
		return m, nil
	}

	return m, nil
}

func (m *Model) handleTimerEvent(ev timer.Event) {
	switch ev.Type {
	case timer.EventSleepFadeStart:
		if m.TimerFadeOriginalVol == 0 {
			m.TimerFadeOriginalVol = m.Player.Volume()
		}
		fadedVol := int(float64(m.TimerFadeOriginalVol) * ev.FadeVolumePercent)
		if fadedVol < 0 {
			fadedVol = 0
		}
		_ = m.Player.SetVolume(fadedVol)

	case timer.EventSleepComplete:
		_ = m.Player.Stop()
		m.PlayingID = ""
		if m.TimerFadeOriginalVol > 0 {
			_ = m.Player.SetVolume(m.TimerFadeOriginalVol)
			m.TimerFadeOriginalVol = 0
		}
		m.StatusMessage = "🌙 Sleep timer completed. Playback stopped."
		timer.DispatchEvent(ev, m.Timer.SleepCfg.NotifyDesktop, m.Timer.SleepCfg.NotifyTerminalBell, m.Timer.SleepCfg.CommandHook)

	case timer.EventFocusStart:
		if ev.StationID != "" && ev.StationID != m.PlayingID {
			m.tuneToStationByID(ev.StationID)
		}
		m.StatusMessage = fmt.Sprintf("🍅 %s", ev.Message)
		timer.DispatchEvent(ev, m.Timer.PomodoroCfg.NotifyDesktop, m.Timer.PomodoroCfg.NotifyTerminalBell, m.Timer.PomodoroCfg.CommandHook)

	case timer.EventShortBreakStart, timer.EventLongBreakStart:
		if ev.StationID == "__pause__" || ev.StationID == "pause" {
			_ = m.Player.Pause()
			m.PlayingID = ""
		} else if ev.StationID != "" && ev.StationID != m.PlayingID {
			m.tuneToStationByID(ev.StationID)
		}
		m.StatusMessage = fmt.Sprintf("☕ %s", ev.Message)
		timer.DispatchEvent(ev, m.Timer.PomodoroCfg.NotifyDesktop, m.Timer.PomodoroCfg.NotifyTerminalBell, m.Timer.PomodoroCfg.CommandHook)

	case timer.EventFocusComplete, timer.EventShortBreakComplete, timer.EventLongBreakComplete:
		m.StatusMessage = fmt.Sprintf("⚡ %s", ev.Message)
		timer.DispatchEvent(ev, m.Timer.PomodoroCfg.NotifyDesktop, m.Timer.PomodoroCfg.NotifyTerminalBell, m.Timer.PomodoroCfg.CommandHook)
	}
}

func (m *Model) tuneToStationByID(stationID string) {
	for _, st := range m.Store.GetAllStations() {
		if st.ID == stationID || strings.EqualFold(st.Name, stationID) {
			_ = m.Player.Play(st)
			m.PlayingID = st.ID
			return
		}
	}
}

func (m Model) handleTimerModalKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.TimerModalScreen == 1 {
		// Custom sleep minute input
		switch msg.String() {
		case "esc":
			m.TimerModalScreen = 0
		case "enter":
			mins, err := strconv.Atoi(strings.TrimSpace(m.TimerCustomSleepInput))
			if err == nil && mins > 0 {
				ev := m.Timer.StartSleep(
					time.Duration(mins)*time.Minute,
					time.Duration(m.Config.SleepFadeSeconds)*time.Second,
					m.Config.EventNotifyDesktop,
					m.Config.EventTerminalBell,
					m.Config.EventCommandHook,
					m.Player.Volume(),
				)
				timer.DispatchEvent(ev, m.Config.EventNotifyDesktop, m.Config.EventTerminalBell, m.Config.EventCommandHook)
				m.StatusMessage = fmt.Sprintf("⏳ Sleep timer set for %d minutes", mins)
				m.ShowTimerModal = false
				m.TimerModalScreen = 0
			}
		case "backspace":
			if len(m.TimerCustomSleepInput) > 0 {
				m.TimerCustomSleepInput = m.TimerCustomSleepInput[:len(m.TimerCustomSleepInput)-1]
			}
		default:
			if len(msg.String()) == 1 && msg.String() >= "0" && msg.String() <= "9" {
				m.TimerCustomSleepInput += msg.String()
			}
		}
		return m, nil
	}

	if m.TimerModalScreen == 2 {
		// Pomodoro & Events Config Editor
		switch msg.String() {
		case "esc":
			m.TimerModalScreen = 0
		case "tab", "down":
			m.TimerPomodoroFocusIdx = (m.TimerPomodoroFocusIdx + 1) % len(m.TimerPomodoroInputs)
		case "shift+tab", "up":
			m.TimerPomodoroFocusIdx = (m.TimerPomodoroFocusIdx - 1 + len(m.TimerPomodoroInputs)) % len(m.TimerPomodoroInputs)
		case "d":
			m.TimerPomodoroNotifyDesktop = !m.TimerPomodoroNotifyDesktop
		case "b":
			m.TimerPomodoroNotifyBell = !m.TimerPomodoroNotifyBell
		case "f":
			if m.Player.CurrentStation() != nil {
				m.TimerPomodoroInputs[4] = m.Player.CurrentStation().ID
			}
		case "k", "K":
			if msg.String() == "K" || m.TimerPomodoroFocusIdx > 3 {
				if m.Player.CurrentStation() != nil {
					m.TimerPomodoroInputs[5] = m.Player.CurrentStation().ID
				}
			}
		case "enter":
			fMin, _ := strconv.Atoi(strings.TrimSpace(m.TimerPomodoroInputs[0]))
			if fMin <= 0 {
				fMin = 25
			}
			sMin, _ := strconv.Atoi(strings.TrimSpace(m.TimerPomodoroInputs[1]))
			if sMin <= 0 {
				sMin = 5
			}
			lMin, _ := strconv.Atoi(strings.TrimSpace(m.TimerPomodoroInputs[2]))
			if lMin <= 0 {
				lMin = 15
			}
			cycles, _ := strconv.Atoi(strings.TrimSpace(m.TimerPomodoroInputs[3]))
			if cycles <= 0 {
				cycles = 4
			}

			focusStation := strings.TrimSpace(m.TimerPomodoroInputs[4])
			breakStation := strings.TrimSpace(m.TimerPomodoroInputs[5])
			shellHook := strings.TrimSpace(m.TimerPomodoroInputs[6])

			m.Config.PomodoroFocusMin = fMin
			m.Config.PomodoroShortBreak = sMin
			m.Config.PomodoroLongBreak = lMin
			m.Config.PomodoroCycles = cycles
			m.Config.PomodoroFocusStation = focusStation
			m.Config.PomodoroBreakStation = breakStation
			m.Config.EventCommandHook = shellHook
			m.Config.EventNotifyDesktop = m.TimerPomodoroNotifyDesktop
			m.Config.EventTerminalBell = m.TimerPomodoroNotifyBell
			_ = util.SaveConfig(m.Config)

			m.Timer.PomodoroCfg = timer.PomodoroConfig{
				FocusDuration:         time.Duration(fMin) * time.Minute,
				ShortBreakDuration:    time.Duration(sMin) * time.Minute,
				LongBreakDuration:     time.Duration(lMin) * time.Minute,
				CyclesBeforeLongBreak: cycles,
				FocusStationID:        focusStation,
				BreakStationID:        breakStation,
				AutoStartBreaks:       true,
				AutoStartFocus:        true,
				NotifyDesktop:         m.TimerPomodoroNotifyDesktop,
				NotifyTerminalBell:    m.TimerPomodoroNotifyBell,
				CommandHook:           shellHook,
			}

			ev := m.Timer.StartPomodoro(m.Timer.PomodoroCfg)
			m.handleTimerEvent(ev)
			m.ShowTimerModal = false
			m.TimerModalScreen = 0
			m.StatusMessage = fmt.Sprintf("🍅 Pomodoro Focus started (%d min sprint)", fMin)

		case "backspace":
			val := m.TimerPomodoroInputs[m.TimerPomodoroFocusIdx]
			if len(val) > 0 {
				m.TimerPomodoroInputs[m.TimerPomodoroFocusIdx] = val[:len(val)-1]
			}
		default:
			if len(msg.String()) == 1 {
				m.TimerPomodoroInputs[m.TimerPomodoroFocusIdx] += msg.String()
			}
		}
		return m, nil
	}

	// Screen 0: Main menu or Active Dashboard
	if m.Timer.IsActive() {
		switch msg.String() {
		case "esc", "q":
			m.ShowTimerModal = false
		case "space", "p":
			ev := m.Timer.TogglePause()
			m.StatusMessage = ev.Message
		case "s":
			events := m.Timer.SkipPhase()
			for _, ev := range events {
				m.handleTimerEvent(ev)
			}
		case "r":
			m.Timer.ResetCurrentInterval()
			m.StatusMessage = "Reset current timer interval"
		case "+", "=":
			m.Timer.AddMinutes(5)
			m.StatusMessage = fmt.Sprintf("Added +5m (%s remaining)", m.Timer.FormattedTime())
		case "-", "_":
			m.Timer.AddMinutes(-5)
			m.StatusMessage = fmt.Sprintf("Subtracted 5m (%s remaining)", m.Timer.FormattedTime())
		case "c":
			ev := m.Timer.Stop()
			m.StatusMessage = ev.Message
			m.ShowTimerModal = false
		case "e":
			m.TimerModalScreen = 2
			m.TimerPomodoroFocusIdx = 0
		}
		return m, nil
	}

	// Inactive menu selection
	switch msg.String() {
	case "esc", "q":
		m.ShowTimerModal = false
	case "j", "down":
		m.TimerMenuCursor = (m.TimerMenuCursor + 1) % 8
	case "k", "up":
		m.TimerMenuCursor = (m.TimerMenuCursor - 1 + 8) % 8
	case "1":
		m.startPomodoroFromMenu()
	case "2":
		m.startSleepFromMenu(15)
	case "3":
		m.startSleepFromMenu(30)
	case "4":
		m.startSleepFromMenu(45)
	case "5":
		m.startSleepFromMenu(60)
	case "6":
		m.startSleepFromMenu(90)
	case "7":
		m.TimerModalScreen = 1
		m.TimerCustomSleepInput = "45"
	case "8":
		m.TimerModalScreen = 2
		m.TimerPomodoroFocusIdx = 0
	case "enter", "space":
		switch m.TimerMenuCursor {
		case 0:
			m.startPomodoroFromMenu()
		case 1:
			m.startSleepFromMenu(15)
		case 2:
			m.startSleepFromMenu(30)
		case 3:
			m.startSleepFromMenu(45)
		case 4:
			m.startSleepFromMenu(60)
		case 5:
			m.startSleepFromMenu(90)
		case 6:
			m.TimerModalScreen = 1
			m.TimerCustomSleepInput = "45"
		case 7:
			m.TimerModalScreen = 2
			m.TimerPomodoroFocusIdx = 0
		}
	}

	return m, nil
}

func (m *Model) startPomodoroFromMenu() {
	ev := m.Timer.StartPomodoro(m.Timer.PomodoroCfg)
	m.handleTimerEvent(ev)
	m.ShowTimerModal = false
	m.StatusMessage = fmt.Sprintf("🍅 Pomodoro Focus Mode started (%d min sprint)", int(m.Timer.PomodoroCfg.FocusDuration.Minutes()))
}

func (m *Model) startSleepFromMenu(mins int) {
	ev := m.Timer.StartSleep(
		time.Duration(mins)*time.Minute,
		time.Duration(m.Config.SleepFadeSeconds)*time.Second,
		m.Config.EventNotifyDesktop,
		m.Config.EventTerminalBell,
		m.Config.EventCommandHook,
		m.Player.Volume(),
	)
	timer.DispatchEvent(ev, m.Config.EventNotifyDesktop, m.Config.EventTerminalBell, m.Config.EventCommandHook)
	m.ShowTimerModal = false
	m.StatusMessage = fmt.Sprintf("⏳ Sleep timer set for %d minutes", mins)
}

func (m Model) handleAddModalKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.ShowAddModal = false
	case "tab", "j", "down":
		m.AddFocusIdx = (m.AddFocusIdx + 1) % len(m.AddInputs)
	case "shift+tab", "k", "up":
		m.AddFocusIdx = (m.AddFocusIdx - 1 + len(m.AddInputs)) % len(m.AddInputs)
	case "enter":
		name := strings.TrimSpace(m.AddInputs[0])
		streamURL := strings.TrimSpace(m.AddInputs[1])
		if name == "" || streamURL == "" {
			m.AddErrMsg = "Name and Stream URL are required fields!"
			return m, nil
		}
		if !player.IsValidStreamURL(streamURL) {
			m.AddErrMsg = "Invalid stream URL! Must start with http:// or https://"
			return m, nil
		}
		bitrate := 128
		if len(m.AddInputs) > 6 {
			if b, err := strconv.Atoi(strings.TrimSpace(m.AddInputs[6])); err == nil && b > 0 {
				bitrate = b
			}
		} else if len(m.AddInputs) > 5 {
			if b, err := strconv.Atoi(strings.TrimSpace(m.AddInputs[5])); err == nil && b > 0 {
				bitrate = b
			}
		}
		city := ""
		if len(m.AddInputs) > 4 {
			city = strings.TrimSpace(m.AddInputs[4])
		}
		broadcast := ""
		if len(m.AddInputs) > 5 {
			broadcast = strings.TrimSpace(m.AddInputs[5])
		}

		st := radio.Station{
			ID:        m.EditingStationID,
			Name:      name,
			URL:       streamURL,
			Genre:     strings.TrimSpace(m.AddInputs[2]),
			Country:   strings.ToUpper(strings.TrimSpace(m.AddInputs[3])),
			City:      city,
			Broadcast: broadcast,
			Bitrate:   bitrate,
			Codec:     "MP3",
			Source:    "local",
		}
		_ = m.Store.AddOrUpdateLocalStation(st)
		m.ShowAddModal = false
		m.RefreshStations()
		m.StatusMessage = fmt.Sprintf("Saved custom station '%s'", st.Name)
	case "backspace":
		val := m.AddInputs[m.AddFocusIdx]
		if len(val) > 0 {
			m.AddInputs[m.AddFocusIdx] = val[:len(val)-1]
		}
	default:
		if len(msg.String()) == 1 {
			m.AddInputs[m.AddFocusIdx] += msg.String()
		}
	}
	return m, nil
}

func (m Model) searchRadioBrowserCmd(query string) tea.Cmd {
	return func() tea.Msg {
		stations, err := m.RBClient.Search(query, 40)
		return RadioBrowserResultMsg{Stations: stations, Err: err}
	}
}

func (m Model) handlePermissionApprovalKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "y", "enter":
		if m.PluginMgr != nil && m.ApprovalPlugin.Manifest.ID != "" {
			_ = m.PluginMgr.ApprovePermissions(m.ApprovalPlugin.Manifest.ID, true)
			_ = m.PluginMgr.EnablePlugin(m.ApprovalPlugin.Manifest.ID)
			m.StatusMessage = fmt.Sprintf("✓ Permissions approved for %s", m.ApprovalPlugin.Manifest.Name)
		}
		m.ShowPermissionApproval = false
	case "n", "esc", "q":
		if m.PluginMgr != nil && m.ApprovalPlugin.Manifest.ID != "" {
			_ = m.PluginMgr.ApprovePermissions(m.ApprovalPlugin.Manifest.ID, false)
			_ = m.PluginMgr.DisablePlugin(m.ApprovalPlugin.Manifest.ID)
			m.StatusMessage = fmt.Sprintf("Plugin %s kept disabled", m.ApprovalPlugin.Manifest.Name)
		}
		m.ShowPermissionApproval = false
	}
	return m, nil
}

func (m Model) handlePluginModalKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	var installed []plugin.PluginInfo
	if m.PluginMgr != nil {
		installed = m.PluginMgr.GetPlugins()
	}

	switch msg.String() {
	case "esc", "q":
		m.ShowPluginModal = false
		m.PluginStatusMsg = ""
		return m, nil

	case "tab", "1", "2", "h", "l":
		if msg.String() == "1" {
			m.PluginModalTab = 0
		} else if msg.String() == "2" {
			m.PluginModalTab = 1
		} else {
			m.PluginModalTab = 1 - m.PluginModalTab
		}
		m.PluginCursor = 0
		m.PluginStatusMsg = ""
		if m.PluginModalTab == 1 && len(m.PluginRegistryList) == 0 {
			return m, m.fetchRegistryCmd()
		}
		return m, nil

	case "j", "down":
		maxLen := len(installed)
		if m.PluginModalTab == 1 {
			maxLen = len(m.PluginRegistryList)
		}
		if maxLen > 0 && m.PluginCursor < maxLen-1 {
			m.PluginCursor++
		}
		return m, nil

	case "k", "up":
		if m.PluginCursor > 0 {
			m.PluginCursor--
		}
		return m, nil

	case "space", "enter":
		if m.PluginModalTab == 0 {
			// Installed plugins tab: toggle enable/disable or prompt perms
			if len(installed) > 0 && m.PluginCursor < len(installed) {
				p := installed[m.PluginCursor]
				if !p.State.PermissionsApproved {
					m.ApprovalPlugin = p
					m.ShowPermissionApproval = true
					return m, nil
				}
				if p.State.Enabled {
					_ = m.PluginMgr.DisablePlugin(p.Manifest.ID)
					m.PluginStatusMsg = fmt.Sprintf("Disabled %s", p.Manifest.Name)
				} else {
					_ = m.PluginMgr.EnablePlugin(p.Manifest.ID)
					m.PluginStatusMsg = fmt.Sprintf("Enabled %s", p.Manifest.Name)
				}
			}
		} else {
			// Registry tab: install plugin
			if len(m.PluginRegistryList) > 0 && m.PluginCursor < len(m.PluginRegistryList) {
				reg := m.PluginRegistryList[m.PluginCursor]
				m.PluginStatusMsg = fmt.Sprintf("⏳ Downloading and installing %s...", reg.Name)
				return m, m.installPluginCmd(reg)
			}
		}
		return m, nil

	case "i":
		if m.PluginModalTab == 1 && len(m.PluginRegistryList) > 0 && m.PluginCursor < len(m.PluginRegistryList) {
			reg := m.PluginRegistryList[m.PluginCursor]
			m.PluginStatusMsg = fmt.Sprintf("⏳ Installing %s...", reg.Name)
			return m, m.installPluginCmd(reg)
		}

	case "p":
		if m.PluginModalTab == 0 && len(installed) > 0 && m.PluginCursor < len(installed) {
			p := installed[m.PluginCursor]
			m.ApprovalPlugin = p
			m.ShowPermissionApproval = true
		}

	case "d", "x":
		if m.PluginModalTab == 0 && len(installed) > 0 && m.PluginCursor < len(installed) {
			p := installed[m.PluginCursor]
			_ = m.PluginMgr.UninstallPlugin(p.Manifest.ID)
			m.PluginStatusMsg = fmt.Sprintf("Uninstalled %s", p.Manifest.Name)
			if m.PluginCursor > 0 && m.PluginCursor >= len(m.PluginMgr.GetPlugins()) {
				m.PluginCursor--
			}
		}

	case "u":
		if len(m.PluginRegistryList) == 0 {
			m.PluginStatusMsg = "⏳ Checking for plugin updates..."
			return m, m.fetchRegistryCmd()
		}
		if m.PluginModalTab == 0 && len(installed) > 0 && m.PluginCursor < len(installed) {
			p := installed[m.PluginCursor]
			for _, reg := range m.PluginRegistryList {
				if reg.ID == p.Manifest.ID {
					m.PluginStatusMsg = fmt.Sprintf("⏳ Updating %s...", reg.Name)
					return m, m.installPluginCmd(reg)
				}
			}
			m.PluginStatusMsg = fmt.Sprintf("Plugin %s not found in registry", p.Manifest.Name)
		}
	}

	return m, nil
}

func (m Model) fetchRegistryCmd() tea.Cmd {
	return func() tea.Msg {
		if m.PluginMgr == nil {
			return PluginRegistryLoadedMsg{Plugins: nil, Err: nil}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		index, err := m.PluginMgr.RegistryClient().FetchRegistry(ctx)
		if err != nil {
			return PluginRegistryLoadedMsg{Plugins: nil, Err: err}
		}
		return PluginRegistryLoadedMsg{Plugins: index.Plugins, Err: nil}
	}
}

func (m Model) installPluginCmd(p plugin.RegistryPlugin) tea.Cmd {
	return func() tea.Msg {
		if m.PluginMgr == nil {
			return PluginInstalledMsg{PluginID: p.ID, Err: fmt.Errorf("plugin manager not initialized")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		err := m.PluginMgr.InstallFromRegistry(ctx, p)
		return PluginInstalledMsg{PluginID: p.ID, Err: err}
	}
}

func (m *Model) cycleGlobeStation(forward bool) {
	nearest, _, distKm := radio.FindNearestCluster(m.GlobeClusters, m.GlobeLat, m.GlobeLon)
	if nearest != nil && distKm <= 1200 && len(nearest.Stations) > 0 {
		if forward {
			m.GlobeStationIndex = (m.GlobeStationIndex + 1) % len(nearest.Stations)
		} else {
			m.GlobeStationIndex = (m.GlobeStationIndex - 1 + len(nearest.Stations)) % len(nearest.Stations)
		}
		st := nearest.Stations[m.GlobeStationIndex]
		badge := st.BroadcastBadge()
		if st.Frequency != "" {
			badge = st.Frequency
		}
		m.StatusMessage = fmt.Sprintf("Cluster [%d/%d]: %s (%s)", m.GlobeStationIndex+1, len(nearest.Stations), st.Name, badge)
	} else {
		m.StatusMessage = "No broadcast stations near crosshair (rotate globe to explore)"
	}
}

func (m *Model) seekTunerStation(forward bool) {
	if !m.Config.ExperimentalTuner || !m.ActiveTuner {
		return
	}
	all := m.Store.GetAllStations()
	if forward {
		var nextFreq float64
		found := false
		minFreq := 99999.0
		for _, st := range all {
			f := radio.ExtractOrAssignFrequency(st, m.TunerBand)
			if f < minFreq {
				minFreq = f
			}
			if f > m.TunerFreq+0.1 && (!found || f < nextFreq) {
				nextFreq = f
				found = true
			}
		}
		if found {
			m.TunerFreq = nextFreq
		} else if minFreq < 99999.0 {
			m.TunerFreq = minFreq
		}
	} else {
		var prevFreq float64
		found := false
		maxFreq := -1.0
		for _, st := range all {
			f := radio.ExtractOrAssignFrequency(st, m.TunerBand)
			if f > maxFreq {
				maxFreq = f
			}
			if f < m.TunerFreq-0.1 && (!found || f > prevFreq) {
				prevFreq = f
				found = true
			}
		}
		if found {
			m.TunerFreq = prevFreq
		} else if maxFreq > 0 {
			m.TunerFreq = maxFreq
		}
	}
	m.onTunerFreqChanged()
}

func (m *Model) onTunerFreqChanged() {
	if !m.Config.ExperimentalTuner || !m.ActiveTuner {
		return
	}
	cfg := tuner.Bands[m.TunerBand]
	all := m.Store.GetAllStations()
	rssi, locked := tuner.CalculateSignalRSSI(all, m.TunerBand, m.TunerFreq)
	m.Player.UpdateTunerSignal(rssi, m.TunerFreq, m.TunerBand)

	if locked != nil && rssi >= 0.20 {
		sMeter := tuner.FormatSMeter(rssi, m.Theme)
		if rssi >= 0.55 {
			m.StatusMessage = fmt.Sprintf("Dial locked: %s (%.1f %s) • Signal: %.0f%% • Audio: Broadcast %s", locked.Name, m.TunerFreq, cfg.Unit, rssi*100, sMeter)
		} else {
			m.StatusMessage = fmt.Sprintf("Receiving: %s (%.1f %s) • Signal: %.0f%% (Static crossfade) %s", locked.Name, m.TunerFreq, cfg.Unit, rssi*100, sMeter)
		}

		// Authentic analog radio: continuous sound output without requiring keypresses.
		// As the dial sweeps into a station carrier frequency, audio begins streaming automatically.
		if m.PlayingID != locked.ID || (m.Player.Status() != player.StatusPlaying && m.Player.Status() != player.StatusConnecting) {
			m.PlayingID = locked.ID
			_ = m.Player.Play(*locked)
			m.Player.UpdateTunerSignal(rssi, m.TunerFreq, m.TunerBand)
			m.SyncDesktop()
		}
	} else {
		// Off-frequency / dead air: pause stream so pure atmospheric static is heard
		if m.PlayingID != "" && (m.Player.Status() == player.StatusPlaying || m.Player.Status() == player.StatusConnecting) {
			_ = m.Player.Pause()
			m.PlayingID = ""
		}
		nearestSt, delta := tuner.FindNearestStation(all, m.TunerBand, m.TunerFreq)
		if nearestSt != nil {
			deltaSign := "+"
			if delta < 0 {
				deltaSign = ""
			}
			m.StatusMessage = fmt.Sprintf("Atmospheric Static (%.1f %s) • Nearest: %s (%s%.1f %s)", m.TunerFreq, cfg.Unit, nearestSt.Name, deltaSign, delta, cfg.Unit)
		} else {
			m.StatusMessage = fmt.Sprintf("Atmospheric Static Noise (%.1f %s)", m.TunerFreq, cfg.Unit)
		}
	}
}

func (m *Model) identifyTrackCmd() tea.Cmd {
	st := m.Player.CurrentStation()
	if st == nil {
		return nil
	}
	stID := st.ID
	stName := st.Name
	stURL := st.URL
	cand := m.Player.CurrentTrack()
	client := m.FingerprintClient
	if client == nil {
		client = fingerprint.NewClient(m.Config.AcoustidAPIKey)
		m.FingerprintClient = client
	}

	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		res, err := client.Identify(ctx, stURL, stID, stName, cand)
		return TrackIdentifiedMsg{
			StationID:   stID,
			StationName: stName,
			Result:      res,
			Err:         err,
		}
	}
}

func (m Model) handlePartyChatKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.IsChatting = false
		m.ChatInput = ""
	case "enter":
		if m.PartySession != nil && m.PartySession.IsActive() && strings.TrimSpace(m.ChatInput) != "" {
			_, _ = m.PartySession.BroadcastChat(m.ChatInput)
		}
		m.IsChatting = false
		m.ChatInput = ""
	case "backspace":
		if len(m.ChatInput) > 0 {
			runes := []rune(m.ChatInput)
			m.ChatInput = string(runes[:len(runes)-1])
		}
	default:
		if len(msg.String()) == 1 {
			m.ChatInput += msg.String()
		}
	}
	return m, nil
}

func (m Model) handlePartyModalKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	if m.PartySession != nil && m.PartySession.IsActive() && m.PartyModalScreen == 3 {
		// Active Dashboard
		switch msg.String() {
		case "esc", "enter", "q":
			m.ShowPartyModal = false
		case "c", "y":
			_ = util.CopyToClipboard(m.PartySession.FormattedCode())
			m.PartyStatusMsg = "✓ Copied room code to clipboard!"
		case "d":
			if m.PartySession.IsHost() {
				newMode, err := m.PartySession.ToggleDJPass()
				if err != nil {
					m.PartyStatusMsg = "Error: " + err.Error()
				} else {
					m.PartyStatusMsg = "✓ DJ Pass set to: " + string(newMode)
				}
			} else {
				m.PartyStatusMsg = "Only host can change DJ Pass"
			}
		case "l":
			_ = m.PartySession.Close()
			m.PartySession = nil
			m.ShowPartyModal = false
			m.StatusMessage = "Left party room"
		}
		return m, nil
	}

	switch m.PartyModalScreen {
	case 1: // Create Room Form
		switch msg.String() {
		case "esc":
			m.PartyModalScreen = 0
			m.PartyStatusMsg = ""
		case "tab", "down":
			m.PartyInputFocus = (m.PartyInputFocus + 1) % 3
		case "shift+tab", "up":
			m.PartyInputFocus = (m.PartyInputFocus + 2) % 3
		case "space":
			if m.PartyInputFocus == 2 {
				if len(m.PartyInputs) > 2 && m.PartyInputs[2] == "open" {
					m.PartyInputs[2] = "host"
				} else {
					m.PartyInputs[2] = "open"
				}
			} else if m.PartyInputFocus < 2 {
				m.PartyInputs[m.PartyInputFocus] += " "
			}
		case "enter":
			roomName := "team-focus"
			if len(m.PartyInputs) > 0 && strings.TrimSpace(m.PartyInputs[0]) != "" {
				roomName = strings.TrimSpace(m.PartyInputs[0])
			}
			nick := m.Config.PartyNickname
			if len(m.PartyInputs) > 1 && strings.TrimSpace(m.PartyInputs[1]) != "" {
				nick = strings.TrimSpace(m.PartyInputs[1])
			}
			djPass := party.DJPassHostOnly
			if len(m.PartyInputs) > 2 && m.PartyInputs[2] == "open" {
				djPass = party.DJPassOpenDemocracy
			}

			sess, err := party.NewPartySession(party.SessionConfig{
				RoomName: roomName,
				Nickname: nick,
				IsHost:   true,
				DJPass:   djPass,
				Port:     m.Config.PartyPort,
			})
			if err != nil {
				m.PartyStatusMsg = "Create error: " + err.Error()
				return m, nil
			}

			m.PartySession = sess
			m.SetupPartyHandlers(sess)
			if curr := m.Player.CurrentStation(); curr != nil {
				_ = m.PartySession.BroadcastStationChange(curr.ID, curr.Name, curr.URL, curr.Genre, curr.Country, curr.Codec, curr.Bitrate, string(m.Player.Status()))
			}
			m.ShowPartyModal = false
			m.StatusMessage = fmt.Sprintf("✓ Created party room %s!", sess.FormattedCode())

		case "backspace":
			if m.PartyInputFocus < 2 && len(m.PartyInputs[m.PartyInputFocus]) > 0 {
				runes := []rune(m.PartyInputs[m.PartyInputFocus])
				m.PartyInputs[m.PartyInputFocus] = string(runes[:len(runes)-1])
			}
		default:
			if m.PartyInputFocus < 2 && len(msg.String()) == 1 {
				m.PartyInputs[m.PartyInputFocus] += msg.String()
			}
		}
		return m, nil

	case 2: // Join Room Form
		switch msg.String() {
		case "esc":
			m.PartyModalScreen = 0
			m.PartyStatusMsg = ""
		case "tab", "down":
			m.PartyInputFocus = (m.PartyInputFocus + 1) % 3
		case "shift+tab", "up":
			m.PartyInputFocus = (m.PartyInputFocus + 2) % 3
		case "enter":
			code := ""
			if len(m.PartyInputs) > 0 {
				code = party.NormalizeRoomCode(m.PartyInputs[0])
			}
			if len(code) != 6 {
				m.PartyStatusMsg = "Room code must be 6 characters (e.g. 8X2K9P)"
				return m, nil
			}
			nick := m.Config.PartyNickname
			if len(m.PartyInputs) > 1 && strings.TrimSpace(m.PartyInputs[1]) != "" {
				nick = strings.TrimSpace(m.PartyInputs[1])
			}
			sess, err := party.NewPartySession(party.SessionConfig{
				RoomCode: code,
				Nickname: nick,
				IsHost:   false,
				Port:     0,
			})
			if err != nil {
				m.PartyStatusMsg = "Join error: " + err.Error()
				return m, nil
			}

			m.PartySession = sess
			m.SetupPartyHandlers(sess)
			if len(m.PartyInputs) > 2 && strings.TrimSpace(m.PartyInputs[2]) != "" {
				_ = sess.ConnectDirect(strings.TrimSpace(m.PartyInputs[2]))
			}
			m.ShowPartyModal = false
			m.StatusMessage = fmt.Sprintf("✓ Connected to party room %s!", sess.FormattedCode())

		case "backspace":
			if len(m.PartyInputs[m.PartyInputFocus]) > 0 {
				runes := []rune(m.PartyInputs[m.PartyInputFocus])
				m.PartyInputs[m.PartyInputFocus] = string(runes[:len(runes)-1])
			}
		default:
			if len(msg.String()) == 1 {
				m.PartyInputs[m.PartyInputFocus] += msg.String()
			}
		}
		return m, nil

	default: // Screen 0: Initial Welcome & Selection
		switch msg.String() {
		case "esc":
			m.ShowPartyModal = false
		case "1":
			m.PartyModalScreen = 1
			m.PartyInputFocus = 0
			m.PartyStatusMsg = ""
		case "2":
			m.PartyModalScreen = 2
			m.PartyInputFocus = 0
			m.PartyStatusMsg = ""
		case "j", "down":
			m.PartyModalCursor = (m.PartyModalCursor + 1) % 2
		case "k", "up":
			m.PartyModalCursor = (m.PartyModalCursor + 1) % 2
		case "enter":
			if m.PartyModalCursor == 0 {
				m.PartyModalScreen = 1
			} else {
				m.PartyModalScreen = 2
			}
			m.PartyInputFocus = 0
			m.PartyStatusMsg = ""
		}
		return m, nil
	}
}
