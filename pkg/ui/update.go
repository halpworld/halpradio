package ui

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
	"github.com/halpworld/halpradio/pkg/timer"
	"github.com/halpworld/halpradio/pkg/util"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

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

		return m, tickCmd()

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

	case TrackUpdatedMsg:
		if m.Player.Status() != player.StatusPlaying && m.Player.Status() != player.StatusConnecting {
			return m, nil
		}
		if m.PlayingID == "" || (msg.StationID != "" && m.PlayingID != msg.StationID) {
			return m, nil
		}
		if msg.TrackTitle != "" {
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
			switch msg.String() {
			case "esc", "q":
				m.ShowThemePicker = false
			case "1":
				m.applyTheme("tokyonight")
			case "2":
				m.applyTheme("catppuccin")
			case "3":
				m.applyTheme("synthwave")
			case "4":
				m.applyTheme("nord")
			case "5":
				m.applyTheme("gruvbox")
			case "6":
				m.applyTheme("dracula")
			}
			return m, nil
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

		switch {
		case key.Matches(msg, m.KeyMap.Quit):
			_ = m.Player.Stop()
			return m, tea.Quit

		case key.Matches(msg, m.KeyMap.Help):
			m.ShowWhichKey = true

		case key.Matches(msg, m.KeyMap.Theme):
			m.ShowThemePicker = true

		case key.Matches(msg, m.KeyMap.Timer):
			m.openTimerModal()

		case key.Matches(msg, m.KeyMap.Plugins):
			m.ShowPluginModal = true
			m.PluginCursor = 0
			m.PluginStatusMsg = ""
			return m, m.fetchRegistryCmd()

		case msg.String() == "tab":
			if m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3 {
				if m.ActiveFocus == FocusSidebar {
					m.ActiveFocus = FocusMainList
				} else {
					m.ActiveFocus = FocusSidebar
				}
			} else {
				m.SwitchTab((m.ActiveTab + 1) % 8)
			}

		case msg.String() == "shift+tab":
			if m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3 {
				if m.ActiveFocus == FocusSidebar {
					m.ActiveFocus = FocusMainList
				} else {
					m.ActiveFocus = FocusSidebar
				}
			} else {
				m.SwitchTab((m.ActiveTab - 1 + 8) % 8)
			}

		case key.Matches(msg, m.KeyMap.Up):
			if (m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3) && m.ActiveFocus == FocusSidebar {
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
			if (m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3) && m.ActiveFocus == FocusSidebar {
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
			if m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3 {
				if m.ActiveFocus == FocusMainList {
					m.ActiveFocus = FocusSidebar
				} else if m.ActiveTab > 0 {
					m.SwitchTab(m.ActiveTab - 1)
				}
			} else if m.ActiveTab > 0 {
				m.SwitchTab(m.ActiveTab - 1)
			}

		case key.Matches(msg, m.KeyMap.Right):
			if m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3 {
				if m.ActiveFocus == FocusSidebar {
					m.ActiveFocus = FocusMainList
				} else if m.ActiveTab < 7 {
					m.SwitchTab(m.ActiveTab + 1)
				}
			} else if m.ActiveTab < 7 {
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
			if m.ActiveTab != 7 {
				m.PlayNextStation()
			}

		case key.Matches(msg, m.KeyMap.PrevStation):
			if m.ActiveTab != 7 {
				m.PlayPrevStation()
			}

		case key.Matches(msg, m.KeyMap.PlayPause):
			if m.ActiveTab == 7 {
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
					trackToCopy = m.Player.CurrentTrack()
					if trackToCopy == "" && m.Player.CurrentStation() != nil {
						trackToCopy = m.Player.CurrentStation().Name
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
					trackToSearch = m.Player.CurrentTrack()
					if trackToSearch == "" && m.Player.CurrentStation() != nil {
						trackToSearch = m.Player.CurrentStation().Name
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

		case key.Matches(msg, m.KeyMap.VolUp):
			v := m.Player.SetVolume(m.Player.Volume() + 5)
			m.StatusMessage = fmt.Sprintf("Volume: %d%%", v)
			m.SyncDesktop()

		case key.Matches(msg, m.KeyMap.VolDown):
			v := m.Player.SetVolume(m.Player.Volume() - 5)
			m.StatusMessage = fmt.Sprintf("Volume: %d%%", v)
			m.SyncDesktop()

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
			if len(m.Stations) > 0 && m.SelectedIndex < len(m.Stations) {
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
	m.ShowThemePicker = false
	m.StatusMessage = fmt.Sprintf("Theme changed to %s", m.Theme.Name)
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
