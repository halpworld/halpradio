package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
	"github.com/halpworld/halpradio/pkg/timer"
	"github.com/halpworld/halpradio/pkg/ui/components"
	"github.com/halpworld/halpradio/pkg/util"
)

type FocusArea int

const (
	FocusMainList FocusArea = iota
	FocusSidebar
)

type TickMsg time.Time
type TrackUpdatedMsg player.TrackInfo
type RadioBrowserResultMsg struct {
	Stations []radio.Station
	Err      error
}
type FlashMessageMsg string
type PluginRegistryLoadedMsg struct {
	Plugins []plugin.RegistryPlugin
	Err     error
}
type PluginInstalledMsg struct {
	PluginID string
	Err      error
}
type PluginFlashMsg string
type PluginNotificationMsg struct {
	Title   string
	Message string
}

type CatalogUpdatedMsg struct {
	Updated       bool
	StationsCount int
	Err           error
}

// Media key and remote control messages
type MediaPlayPauseMsg struct{}
type MediaPlayMsg struct{}
type MediaPauseMsg struct{}
type MediaStopMsg struct{}
type MediaNextMsg struct{}
type MediaPrevMsg struct{}
type MediaVolUpMsg struct{}
type MediaVolDownMsg struct{}
type MediaMuteMsg struct{}
type MediaRandomMsg struct{}
type MediaQuitMsg struct{}

type Model struct {
	Width  int
	Height int

	Store      *radio.Store
	Player     player.Player
	Config     util.Config
	RBClient   *radio.RadioBrowserClient
	KeyMap     KeyMap
	Theme      theme.Theme
	Visualizer *components.Visualizer
	Timer      *timer.Timer
	Desktop    *desktop.Manager
	PluginMgr  *plugin.Manager

	ActiveTab     int // 0: Activities, 1: Catalog, 2: Countries, 3: Genres, 4: Favorites, 5: RadioBrowser, 6: Custom, 7: History
	ActiveFocus   FocusArea
	Stations      []radio.Station
	RBStations    []radio.Station
	SelectedIndex int
	HistoryIndex  int

	Activities       []radio.Activity
	SelectedActivity string
	ActivityIndex    int

	Countries       []radio.CountryInfo
	SelectedCountry string
	CountryIndex    int

	Genres        []string
	SelectedGenre string
	GenreIndex    int

	PlayingID     string
	SearchQuery   string
	IsSearching   bool
	StatusMessage string

	ShowWhichKey    bool
	ShowThemePicker bool
	ShowPRExport    bool
	ShowAddModal    bool
	ShowTimerModal  bool

	ShowPluginModal        bool
	PluginModalTab         int // 0: Installed, 1: Registry
	PluginCursor           int
	PluginRegistryList     []plugin.RegistryPlugin
	PluginStatusMsg        string
	ShowPermissionApproval bool
	ApprovalPlugin         plugin.PluginInfo

	TimerModalScreen           int // 0: Main Menu/Dashboard, 1: Custom Sleep, 2: Pomodoro Config
	TimerMenuCursor            int
	TimerCustomSleepInput      string
	TimerPomodoroInputs        []string
	TimerPomodoroFocusIdx      int
	TimerPomodoroNotifyDesktop bool
	TimerPomodoroNotifyBell    bool
	TimerFadeOriginalVol       int
	LastTickTime               time.Time

	AddInputs        []string
	AddFocusIdx      int
	AddErrMsg        string
	EditingStationID string
	ExportStation    radio.Station
}

func NewModel(
	store *radio.Store,
	pm player.Player,
	cfg util.Config,
) Model {
	th := theme.GetTheme(cfg.Theme)
	viz := components.NewVisualizer(cfg.VisualizerMode)

	pomoCfg := timer.PomodoroConfig{
		FocusDuration:         time.Duration(cfg.PomodoroFocusMin) * time.Minute,
		ShortBreakDuration:    time.Duration(cfg.PomodoroShortBreak) * time.Minute,
		LongBreakDuration:     time.Duration(cfg.PomodoroLongBreak) * time.Minute,
		CyclesBeforeLongBreak: cfg.PomodoroCycles,
		FocusStationID:        cfg.PomodoroFocusStation,
		BreakStationID:        cfg.PomodoroBreakStation,
		AutoStartBreaks:       true,
		AutoStartFocus:        true,
		NotifyDesktop:         cfg.EventNotifyDesktop,
		NotifyTerminalBell:    cfg.EventTerminalBell,
		CommandHook:           cfg.EventCommandHook,
	}
	sleepCfg := timer.SleepConfig{
		Duration:           30 * time.Minute,
		FadeDuration:       time.Duration(cfg.SleepFadeSeconds) * time.Second,
		NotifyDesktop:      cfg.EventNotifyDesktop,
		NotifyTerminalBell: cfg.EventTerminalBell,
		CommandHook:        cfg.EventCommandHook,
	}
	tm := timer.NewTimer()
	tm.PomodoroCfg = pomoCfg
	tm.SleepCfg = sleepCfg

	m := Model{
		Store:                      store,
		Player:                     pm,
		Config:                     cfg,
		RBClient:                   radio.NewRadioBrowserClient(),
		KeyMap:                     DefaultKeyMap(),
		Theme:                      th,
		Visualizer:                 viz,
		Timer:                      tm,
		ActiveTab:                  0,
		ActiveFocus:                FocusSidebar,
		Activities:                 radio.DefaultActivities,
		SelectedActivity:           "",
		ActivityIndex:              0,
		Countries:                  store.GetCountries(),
		SelectedCountry:            "",
		CountryIndex:               0,
		Genres:                     store.GetCategories(),
		SelectedGenre:              "",
		GenreIndex:                 0,
		AddInputs:                  make([]string, 7),
		HistoryIndex:               0,
		TimerPomodoroInputs:        make([]string, 7),
		TimerPomodoroNotifyDesktop: cfg.EventNotifyDesktop,
		TimerPomodoroNotifyBell:    cfg.EventTerminalBell,
		LastTickTime:               time.Now(),
	}

	m.RefreshStations()
	return m
}

func (m *Model) SwitchTab(tabIndex int) {
	if tabIndex < 0 {
		tabIndex = 0
	} else if tabIndex > 7 {
		tabIndex = 7
	}
	m.ActiveTab = tabIndex
	m.SelectedIndex = 0
	m.HistoryIndex = 0
	if m.ActiveTab == 0 || m.ActiveTab == 2 || m.ActiveTab == 3 {
		m.ActiveFocus = FocusSidebar
	} else {
		m.ActiveFocus = FocusMainList
	}
	m.RefreshStations()
}

func (m *Model) RefreshStations() {
	var baseList []radio.Station

	switch m.ActiveTab {
	case 0:
		baseList = m.Store.GetAllStations()
	case 1:
		baseList = m.Store.GetAllStations()
	case 2:
		baseList = m.Store.GetAllStations()
	case 3:
		baseList = m.Store.GetAllStations()
	case 4:
		baseList = m.Store.GetFavorites()
	case 5:
		baseList = m.RBStations
	case 6:
		baseList = m.Store.Local
	case 7:
		baseList = nil
	default:
		baseList = m.Store.GetAllStations()
	}

	selectedGenre := ""
	if m.ActiveTab == 3 {
		selectedGenre = m.SelectedGenre
	}

	selectedActivity := ""
	if m.ActiveTab == 0 {
		selectedActivity = m.SelectedActivity
	}

	selectedCountry := ""
	if m.ActiveTab == 2 {
		selectedCountry = m.SelectedCountry
	}

	m.Countries = m.Store.GetCountries()
	m.Genres = m.Store.GetCategories()

	m.Stations = radio.FilterWithLocation(baseList, m.SearchQuery, selectedGenre, selectedActivity, selectedCountry)
	if m.SelectedIndex < 0 {
		m.SelectedIndex = 0
	}
	if m.SelectedIndex >= len(m.Stations) {
		if len(m.Stations) > 0 {
			m.SelectedIndex = len(m.Stations) - 1
		} else {
			m.SelectedIndex = 0
		}
	}
}

func (m Model) WindowTitle() string {
	timerPrefix := ""
	if m.Timer != nil && m.Timer.IsActive() {
		timerPrefix = m.Timer.WindowTitleBadge()
	}

	tabNames := []string{"Activities", "Catalog", "Countries", "Genres", "Favorites", "RadioBrowser", "Custom", "History"}
	tabName := "Activities"
	if m.ActiveTab >= 0 && m.ActiveTab < len(tabNames) {
		tabName = tabNames[m.ActiveTab]
	}

	st := m.Player.CurrentStation()
	if st != nil && m.Player.Status() == player.StatusPlaying {
		track := m.Player.CurrentTrack()
		if track != "" {
			return fmt.Sprintf("%s▶ %s - %s | halpradio", timerPrefix, track, st.Name)
		}
		return fmt.Sprintf("%s▶ %s | halpradio", timerPrefix, st.Name)
	}
	if st != nil && m.Player.Status() == player.StatusConnecting {
		return fmt.Sprintf("%s⟳ Connecting: %s | halpradio", timerPrefix, st.Name)
	}
	if st != nil && m.Player.Status() == player.StatusPaused {
		return fmt.Sprintf("%s⏸ %s | halpradio", timerPrefix, st.Name)
	}

	return fmt.Sprintf("%shalpradio - %d: %s", timerPrefix, m.ActiveTab+1, tabName)
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		tickCmd(),
		tea.SetWindowTitle(m.WindowTitle()),
	}
	if m.Config.CatalogAutoUpdate {
		cmds = append(cmds, checkCatalogUpdateCmd(m.Config))
	}
	return tea.Batch(cmds...)
}

func checkCatalogUpdateCmd(cfg util.Config) tea.Cmd {
	return func() tea.Msg {
		updater := radio.NewCatalogUpdater(cfg.CatalogUpdateURL, cfg.CatalogCacheTTLHours)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		updated, count, err := updater.CheckAndUpdate(ctx, false)
		return CatalogUpdatedMsg{
			Updated:       updated,
			StationsCount: count,
			Err:           err,
		}
	}
}

func tickCmd() tea.Cmd {
	return tea.Every(150*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// SetDesktop sets the desktop manager on the model.
func (m *Model) SetDesktop(d *desktop.Manager) {
	m.Desktop = d
}

// SyncDesktop pushes current playback state to desktop MPRIS / widgets.
func (m *Model) SetPluginManager(pm *plugin.Manager) {
	m.PluginMgr = pm
}

func (m *Model) SyncDesktop() {
	st := m.Player.CurrentStation()
	stationID := ""
	stationName := ""
	genre := ""
	streamURL := ""
	bitrate := 0
	if st != nil {
		stationID = st.ID
		stationName = st.Name
		genre = st.Genre
		streamURL = st.URL
		bitrate = st.Bitrate
	}

	vizMode := ""
	if m.Visualizer != nil {
		vizMode = m.Visualizer.Mode
	}

	if m.Desktop != nil {
		m.Desktop.UpdatePlaybackFull(
			string(m.Player.Status()),
			stationID,
			stationName,
			genre,
			m.Player.CurrentTrack(),
			streamURL,
			bitrate,
			m.Player.Volume(),
			m.Player.IsMuted(),
			m.Player.ActiveBackend(),
			vizMode,
		)
	}

	if m.PluginMgr != nil {
		m.PluginMgr.DispatchPlaybackChange(plugin.PlaybackChangePayload{
			Status:  string(m.Player.Status()),
			Volume:  m.Player.Volume(),
			Backend: m.Player.ActiveBackend(),
			Station: stationName,
		})
	}
}

// PlayNextStation moves to and plays the next station in the active station list.
func (m *Model) PlayNextStation() {
	if len(m.Stations) == 0 {
		return
	}
	m.SelectedIndex = (m.SelectedIndex + 1) % len(m.Stations)
	st := m.Stations[m.SelectedIndex]
	_ = m.Player.Play(st)
	m.PlayingID = st.ID
	m.StatusMessage = fmt.Sprintf("Playing %s [%s]", st.Name, m.Player.ActiveBackend())
	m.SyncDesktop()
	if m.Config.SongNotifications && m.Desktop != nil {
		m.Desktop.NotifySong(st.Name, st.Name)
	}
}

// PlayPrevStation moves to and plays the previous station in the active station list.
func (m *Model) PlayPrevStation() {
	if len(m.Stations) == 0 {
		return
	}
	m.SelectedIndex = (m.SelectedIndex - 1 + len(m.Stations)) % len(m.Stations)
	st := m.Stations[m.SelectedIndex]
	_ = m.Player.Play(st)
	m.PlayingID = st.ID
	m.StatusMessage = fmt.Sprintf("Playing %s [%s]", st.Name, m.Player.ActiveBackend())
	m.SyncDesktop()
	if m.Config.SongNotifications && m.Desktop != nil {
		m.Desktop.NotifySong(st.Name, st.Name)
	}
}

// TogglePlayPause toggles between playing and paused or starts playing the selected station.
func (m *Model) TogglePlayPause() {
	if m.Player.Status() == player.StatusPlaying {
		_ = m.Player.Pause()
		m.PlayingID = ""
		m.StatusMessage = "Audio playback paused"
		m.SyncDesktop()
		return
	}

	if m.Player.Status() == player.StatusPaused && m.Player.CurrentStation() != nil {
		_ = m.Player.Resume()
		st := m.Player.CurrentStation()
		m.PlayingID = st.ID
		m.StatusMessage = fmt.Sprintf("Resumed %s", st.Name)
		m.SyncDesktop()
		return
	}

	if len(m.Stations) > 0 && m.SelectedIndex < len(m.Stations) {
		st := m.Stations[m.SelectedIndex]
		_ = m.Player.Play(st)
		m.PlayingID = st.ID
		m.StatusMessage = fmt.Sprintf("Playing %s [%s]", st.Name, m.Player.ActiveBackend())
		m.SyncDesktop()
		if m.Config.SongNotifications && m.Desktop != nil {
			m.Desktop.NotifySong(st.Name, st.Name)
		}
	}
}
