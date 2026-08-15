package ui

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
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
		return m, tickCmd()

	case TrackUpdatedMsg:
		return m, tea.SetWindowTitle(m.WindowTitle())

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

		if m.IsSearching {
			switch msg.String() {
			case "esc":
				m.IsSearching = false
			case "enter":
				m.IsSearching = false
				if m.ActiveTab == 3 && m.SearchQuery != "" {
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

		case msg.String() == "tab":
			if m.ActiveTab == 1 || m.ActiveTab == 2 {
				if m.ActiveFocus == FocusSidebar {
					m.ActiveFocus = FocusMainList
				} else {
					m.ActiveFocus = FocusSidebar
				}
			} else {
				m.SwitchTab((m.ActiveTab + 1) % 6)
			}

		case msg.String() == "shift+tab":
			if m.ActiveTab == 1 || m.ActiveTab == 2 {
				if m.ActiveFocus == FocusSidebar {
					m.ActiveFocus = FocusMainList
				} else {
					m.ActiveFocus = FocusSidebar
				}
			} else {
				m.SwitchTab((m.ActiveTab - 1 + 6) % 6)
			}

		case key.Matches(msg, m.KeyMap.Up):
			if (m.ActiveTab == 1 || m.ActiveTab == 2) && m.ActiveFocus == FocusSidebar {
				if m.ActiveTab == 1 {
					if m.ActivityIndex > 0 {
						m.ActivityIndex--
						if m.ActivityIndex == 0 {
							m.SelectedActivity = ""
						} else {
							m.SelectedActivity = m.Activities[m.ActivityIndex-1].ID
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
			} else {
				if m.SelectedIndex > 0 {
					m.SelectedIndex--
				}
			}

		case key.Matches(msg, m.KeyMap.Down):
			if (m.ActiveTab == 1 || m.ActiveTab == 2) && m.ActiveFocus == FocusSidebar {
				if m.ActiveTab == 1 {
					if m.ActivityIndex < len(m.Activities) {
						m.ActivityIndex++
						m.SelectedActivity = m.Activities[m.ActivityIndex-1].ID
						m.RefreshStations()
					}
				} else {
					if m.GenreIndex < len(m.Genres) {
						m.GenreIndex++
						m.SelectedGenre = m.Genres[m.GenreIndex-1]
						m.RefreshStations()
					}
				}
			} else {
				if m.SelectedIndex < len(m.Stations)-1 {
					m.SelectedIndex++
				}
			}

		case key.Matches(msg, m.KeyMap.Left):
			if m.ActiveTab == 1 || m.ActiveTab == 2 {
				if m.ActiveFocus == FocusMainList {
					m.ActiveFocus = FocusSidebar
				} else if m.ActiveTab > 0 {
					m.SwitchTab(m.ActiveTab - 1)
				}
			} else if m.ActiveTab > 0 {
				m.SwitchTab(m.ActiveTab - 1)
			}

		case key.Matches(msg, m.KeyMap.Right):
			if m.ActiveTab == 1 || m.ActiveTab == 2 {
				if m.ActiveFocus == FocusSidebar {
					m.ActiveFocus = FocusMainList
				} else if m.ActiveTab < 5 {
					m.SwitchTab(m.ActiveTab + 1)
				}
			} else if m.ActiveTab < 5 {
				m.SwitchTab(m.ActiveTab + 1)
			}

		case key.Matches(msg, m.KeyMap.Top):
			if (m.ActiveTab == 1 || m.ActiveTab == 2) && m.ActiveFocus == FocusSidebar {
				if m.ActiveTab == 1 {
					m.ActivityIndex = 0
					m.SelectedActivity = ""
				} else {
					m.GenreIndex = 0
					m.SelectedGenre = ""
				}
				m.RefreshStations()
			} else {
				m.SelectedIndex = 0
			}

		case key.Matches(msg, m.KeyMap.Bottom):
			if (m.ActiveTab == 1 || m.ActiveTab == 2) && m.ActiveFocus == FocusSidebar {
				if m.ActiveTab == 1 {
					m.ActivityIndex = len(m.Activities)
					if len(m.Activities) > 0 {
						m.SelectedActivity = m.Activities[len(m.Activities)-1].ID
					}
				} else {
					m.GenreIndex = len(m.Genres)
					if len(m.Genres) > 0 {
						m.SelectedGenre = m.Genres[len(m.Genres)-1]
					}
				}
				m.RefreshStations()
			} else if len(m.Stations) > 0 {
				m.SelectedIndex = len(m.Stations) - 1
			}

		case key.Matches(msg, m.KeyMap.HalfPageUp):
			if (m.ActiveTab == 1 || m.ActiveTab == 2) && m.ActiveFocus == FocusSidebar {
				if m.ActiveTab == 1 {
					m.ActivityIndex -= 5
					if m.ActivityIndex < 0 {
						m.ActivityIndex = 0
					}
					if m.ActivityIndex == 0 {
						m.SelectedActivity = ""
					} else {
						m.SelectedActivity = m.Activities[m.ActivityIndex-1].ID
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
			} else {
				m.SelectedIndex -= 5
				if m.SelectedIndex < 0 {
					m.SelectedIndex = 0
				}
			}

		case key.Matches(msg, m.KeyMap.HalfPageDown):
			if (m.ActiveTab == 1 || m.ActiveTab == 2) && m.ActiveFocus == FocusSidebar {
				if m.ActiveTab == 1 {
					m.ActivityIndex += 5
					if m.ActivityIndex > len(m.Activities) {
						m.ActivityIndex = len(m.Activities)
					}
					if m.ActivityIndex == 0 {
						m.SelectedActivity = ""
					} else {
						m.SelectedActivity = m.Activities[m.ActivityIndex-1].ID
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
			if len(m.RBStations) == 0 {
				m.StatusMessage = "Fetching top stations from RadioBrowser..."
				return m, m.searchRadioBrowserCmd("")
			}
		case msg.String() == "6":
			m.SwitchTab(5)

		case key.Matches(msg, m.KeyMap.Activity):
			if m.ActiveTab != 1 {
				m.SwitchTab(1)
			} else {
				if m.ActiveFocus == FocusSidebar {
					m.ActiveFocus = FocusMainList
				} else {
					m.ActiveFocus = FocusSidebar
				}
			}

		case key.Matches(msg, m.KeyMap.Category):
			if m.ActiveTab != 2 {
				m.SwitchTab(2)
			} else {
				if m.ActiveFocus == FocusSidebar {
					m.ActiveFocus = FocusMainList
				} else {
					m.ActiveFocus = FocusSidebar
				}
			}

		case key.Matches(msg, m.KeyMap.PlayPause):
			if (m.ActiveTab == 1 || m.ActiveTab == 2) && m.ActiveFocus == FocusSidebar {
				m.ActiveFocus = FocusMainList
				m.SelectedIndex = 0
			} else if len(m.Stations) > 0 && m.SelectedIndex < len(m.Stations) {
				st := m.Stations[m.SelectedIndex]
				if m.PlayingID == st.ID && m.Player.Status() == player.StatusPlaying {
					_ = m.Player.Pause()
					m.PlayingID = ""
					m.StatusMessage = fmt.Sprintf("Paused %s", st.Name)
				} else {
					_ = m.Player.Play(st)
					m.PlayingID = st.ID
					m.StatusMessage = fmt.Sprintf("Playing %s [%s]", st.Name, m.Player.ActiveBackend())
				}
			}

		case key.Matches(msg, m.KeyMap.Stop):
			_ = m.Player.Stop()
			m.PlayingID = ""
			m.StatusMessage = "Audio playback stopped"

		case key.Matches(msg, m.KeyMap.RandomPlay):
			if len(m.Stations) > 0 {
				idx := rand.Intn(len(m.Stations))
				m.SelectedIndex = idx
				st := m.Stations[idx]
				_ = m.Player.Play(st)
				m.PlayingID = st.ID
				m.StatusMessage = fmt.Sprintf("Playing Random: %s", st.Name)
			}

		case key.Matches(msg, m.KeyMap.VolUp):
			v := m.Player.SetVolume(m.Player.Volume() + 5)
			m.StatusMessage = fmt.Sprintf("Volume: %d%%", v)

		case key.Matches(msg, m.KeyMap.VolDown):
			v := m.Player.SetVolume(m.Player.Volume() - 5)
			m.StatusMessage = fmt.Sprintf("Volume: %d%%", v)

		case key.Matches(msg, m.KeyMap.Mute):
			isMuted := m.Player.ToggleMute()
			if isMuted {
				m.StatusMessage = "Muted"
			} else {
				m.StatusMessage = fmt.Sprintf("Unmuted (%d%%)", m.Player.Volume())
			}

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
			m.AddInputs = make([]string, 5)
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
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *Model) applyTheme(name string) {
	m.Config.Theme = name
	m.Theme = theme.GetTheme(name)
	m.ShowThemePicker = false
	m.StatusMessage = fmt.Sprintf("Theme changed to %s", m.Theme.Name)
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
		bitrate, _ := strconv.Atoi(m.AddInputs[4])
		if bitrate == 0 {
			bitrate = 128
		}

		st := radio.Station{
			ID:      m.EditingStationID,
			Name:    name,
			URL:     streamURL,
			Genre:   strings.TrimSpace(m.AddInputs[2]),
			Country: strings.ToUpper(strings.TrimSpace(m.AddInputs[3])),
			Bitrate: bitrate,
			Codec:   "MP3",
			Source:  "local",
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
