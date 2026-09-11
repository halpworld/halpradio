package ui

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/art"
	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/lyrics"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/player/fingerprint"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
	"github.com/halpworld/halpradio/pkg/timer"
	"github.com/halpworld/halpradio/pkg/ui/components"
	"github.com/halpworld/halpradio/pkg/ui/components/tuner"
	"github.com/halpworld/halpradio/pkg/util"
)

type FocusArea int

const (
	FocusMainList FocusArea = iota
	FocusSidebar
	FocusLyrics
)

type TickMsg time.Time
type TrackUpdatedMsg player.TrackInfo
type TrackIdentifiedMsg struct {
	StationID   string
	StationName string
	Result      *fingerprint.Result
	Err         error
}
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

type ThemeRegistryLoadedMsg struct {
	Themes []theme.RegistryTheme
	Err    error
}
type ThemeInstalledMsg struct {
	ThemeID string
	Err     error
}
type ThemeDeletedMsg struct {
	ThemeID string
	Err     error
}
type ThemeFlashMsg string

type CatalogUpdatedMsg struct {
	Updated       bool
	StationsCount int
	Err           error
}

// Party room messages
type PartyPlaybackSyncMsg party.SyncPayload
type PartyReactionMsg party.FloatingReaction
type PartyChatMsg party.ChatMessage
type PartyPeerChangeMsg []*party.PeerInfo
type PartyStatusFlashMsg string

type PartyCreateRoomMsg struct {
	RoomName string
	Nickname string
	DJPass   string
}
type PartyJoinRoomMsg struct {
	RoomCode string
	Nickname string
	Address  string
}
type PartyLeaveRoomMsg struct{}
type PartySendReactionMsg string
type PartySendChatMsg string

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

// AutoPauseMsg is sent when the player paused itself after the audio output
// device left Bluetooth (e.g. AirPods taken out of the ears).
type AutoPauseMsg struct{}

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

	ShowWhichKey         bool
	ShowThemePicker      bool
	ThemeModalTab        int // 0: Installed Themes, 1: Community Hub
	ThemeCursor          int // Cursor in installed list
	ThemeRegistryCursor  int // Cursor in community hub list
	ThemeRegistryList    []theme.RegistryTheme
	ThemeStatusMsg       string
	ThemeSearchQuery     string
	ThemeIsSearching     bool
	IsPreviewingTheme    bool
	PreviewOriginalTheme theme.Theme
	ThemeClient          *theme.RegistryClient
	ShowPRExport         bool
	ShowAddModal         bool
	ShowTimerModal       bool

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

	// Globe & Frequency Tuner State
	GlobeLat          float64
	GlobeLon          float64
	GlobeZoom         float64
	GlobeStationIndex int
	GlobeClusters     []radio.StationCluster
	ActiveTuner       bool
	TunerFreq         float64
	TunerBand         string // "FM", "AM", "SW"

	// Acoustic stream fingerprinting state
	IsIdentifying       bool
	IdentifiedResult    *fingerprint.Result
	PlaybackStartTime   time.Time
	LastFingerprintTime time.Time
	FingerprintClient   *fingerprint.Client

	// Synced lyrics & terminal album art state
	ShowLyrics       bool
	ShowArtModal     bool
	LyricsClient     *lyrics.Client
	LyricsSheet      *lyrics.Sheet
	LyricsStatus     string
	IsFetchingLyrics bool
	LyricsScroll     int
	LyricsOffset     time.Duration
	LyricsTrackKey   string
	TrackStartTime   time.Time

	ArtClient     *art.Client
	ArtRenderer   *art.Renderer
	ArtProtocol   art.Protocol
	Cover         *art.Cover
	ArtLines      []string
	ArtCols       int
	ArtRows       int
	ArtStatus     string
	IsFetchingArt bool
	ArtTrackKey   string

	// ArtClearFrames counts down the frames that still carry the protocol's
	// image-delete escape. Kitty placements survive a text repaint, so a
	// closed drawer has to explicitly evict them.
	ArtClearFrames int

	// Terminal Party Room State
	PartySession     *party.PartySession
	ShowPartyModal   bool
	PartyModalScreen int // 0: Menu, 1: Create, 2: Join, 3: Active Dashboard
	PartyModalCursor int
	PartyInputs      []string
	PartyInputFocus  int
	PartyStatusMsg   string
	IsChatting       bool
	ChatInput        string
	sendMsgFn        func(tea.Msg)

	// nowPlayingSig is the station + track signature the lyric sheet and
	// artwork currently belong to, used to notice a change on air.
	nowPlayingSig string
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
		ThemeClient:                theme.NewRegistryClient(cfg.ThemeRegistryURL),
		GlobeLat:                   35.6762, // Tokyo / East Asia default view
		GlobeLon:                   139.6503,
		GlobeZoom:                  1.0,
		GlobeStationIndex:          0,
		ActiveTuner:                false,
		TunerFreq:                  93.9,
		TunerBand:                  "FM",
		PlaybackStartTime:          time.Now(),
		FingerprintClient:          fingerprint.NewClient(cfg.AcoustidAPIKey),
		PartyInputs:                make([]string, 3),
		LyricsOffset:               time.Duration(cfg.LyricsOffsetMs) * time.Millisecond,
		ShowLyrics:                 cfg.LyricsEnabled && cfg.LyricsAutoOpen,
	}

	if cfg.LyricsEnabled {
		m.LyricsClient = lyrics.NewClient(util.GetLyricsCacheDir())
	}
	if cfg.AlbumArtEnabled {
		m.ArtProtocol = art.Resolve(cfg.AlbumArtProtocol)
		m.ArtRenderer = art.NewRenderer(m.ArtProtocol)
		m.ArtClient = art.NewClient(util.GetAlbumArtCacheDir(), cfg.LastFMAPIKey)
	} else {
		m.ArtProtocol = art.ProtocolNone
	}

	allThemes := theme.GetAllThemes()
	for i, t := range allThemes {
		if t.ID == cfg.Theme || t.Name == th.Name || t.ID == th.ID {
			m.ThemeCursor = i
			break
		}
	}

	m.RefreshStations()
	return m
}

func (m *Model) SwitchTab(tabIndex int) {
	if tabIndex < 0 {
		tabIndex = 0
	} else if tabIndex > 8 {
		tabIndex = 8
	}
	if tabIndex != 8 && m.ActiveTuner {
		m.ActiveTuner = false
		if m.Player != nil {
			m.Player.SetTunerMode(false, 1.0, m.TunerFreq, m.TunerBand)
		}
	}
	m.ActiveTab = tabIndex
	m.SelectedIndex = 0
	m.HistoryIndex = 0
	m.GlobeStationIndex = 0
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
	case 8:
		baseList = m.Store.GetAllStations()
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

	// Update Globe spatial clusters
	allStations := m.Store.GetAllStations()
	m.GlobeClusters = radio.BuildStationClusters(allStations)

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

	tabNames := []string{"Activities", "Catalog", "Countries", "Genres", "Favorites", "RadioBrowser", "Custom", "History", "Globe"}
	tabName := "Activities"
	if m.ActiveTab >= 0 && m.ActiveTab < len(tabNames) {
		tabName = tabNames[m.ActiveTab]
	}

	st := m.Player.CurrentStation()

	if m.ActiveTab == 8 && m.Config.ExperimentalTuner && m.ActiveTuner {
		cfg := tuner.Bands[m.TunerBand]
		if st != nil && m.Player.Status() == player.StatusPlaying {
			return fmt.Sprintf("%s▶ %s [0: Tuner %.1f %s]", timerPrefix, st.Name, m.TunerFreq, cfg.Unit)
		}
		if st != nil && m.Player.Status() == player.StatusPaused {
			return fmt.Sprintf("%s⏸ %s [0: Tuner %.1f %s]", timerPrefix, st.Name, m.TunerFreq, cfg.Unit)
		}
		return fmt.Sprintf("%shalpradio - 0: Tuner (%.1f %s %s)", timerPrefix, m.TunerFreq, cfg.Unit, m.TunerBand)
	}
	if st != nil && m.Player.Status() == player.StatusPlaying {
		track := m.Player.CurrentTrack()
		if m.IdentifiedResult != nil {
			track = m.IdentifiedResult.SimpleTitle()
		}
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

// SetMsgSender sets the callback for sending messages to Bubble Tea runtime.
func (m *Model) SetMsgSender(fn func(tea.Msg)) {
	m.sendMsgFn = fn
}

// SetPartySession sets an active party session on the model.
func (m *Model) SetPartySession(sess *party.PartySession) {
	m.PartySession = sess
	if sess != nil {
		m.SetupPartyHandlers(sess)
	}
}

// SetupPartyHandlers wires up session callbacks to Bubble Tea messages.
func (m *Model) SetupPartyHandlers(sess *party.PartySession) {
	if sess == nil {
		return
	}
	send := m.sendMsgFn
	sess.SetHandlers(
		func(sp party.SyncPayload) {
			if send != nil {
				send(PartyPlaybackSyncMsg(sp))
			}
		},
		func(r party.FloatingReaction) {
			if send != nil {
				send(PartyReactionMsg(r))
			}
		},
		func(c party.ChatMessage) {
			if send != nil {
				send(PartyChatMsg(c))
			}
		},
		func(p []*party.PeerInfo) {
			if send != nil {
				send(PartyPeerChangeMsg(p))
			}
		},
		func(msg string) {
			if send != nil {
				send(PartyStatusFlashMsg(msg))
			}
		},
	)
}

func (m *Model) openPartyModal() {
	m.ShowPartyModal = true
	m.PartyStatusMsg = ""
	if m.PartySession != nil && m.PartySession.IsActive() {
		m.PartyModalScreen = 3 // Active Dashboard
	} else {
		m.PartyModalScreen = 0 // Menu
		m.PartyModalCursor = 0
		defaultNick := m.Config.PartyNickname
		if defaultNick == "" {
			defaultNick = "listener"
		}
		m.PartyInputs = []string{"team-focus", defaultNick, "host"}
		m.PartyInputFocus = 0
	}
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
		if m.PartySession != nil && m.PartySession.IsActive() {
			peers := m.PartySession.PeerRoster()
			peerNames := make([]string, 0, len(peers))
			for _, p := range peers {
				peerNames = append(peerNames, p.Nickname)
			}
			m.Desktop.SetPartyInfo(&desktop.PartyInfo{
				Active:    true,
				RoomCode:  m.PartySession.RoomCode(),
				RoomName:  m.PartySession.RoomName(),
				IsHost:    m.PartySession.IsHost(),
				Host:      m.PartySession.HostNickname(),
				DJPass:    string(m.PartySession.DJPass()),
				Listeners: m.PartySession.PeerCount(),
				Peers:     peerNames,
			})
		} else {
			m.Desktop.SetPartyInfo(&desktop.PartyInfo{
				Active: false,
			})
		}
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
	m.IdentifiedResult = nil
	m.IsIdentifying = false
	m.PlaybackStartTime = time.Now()
	m.LastFingerprintTime = time.Time{}
	m.resetNowPlaying()
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
	m.IdentifiedResult = nil
	m.IsIdentifying = false
	m.PlaybackStartTime = time.Now()
	m.LastFingerprintTime = time.Time{}
	m.resetNowPlaying()
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
		m.PlaybackStartTime = time.Now()
		m.StatusMessage = fmt.Sprintf("Resumed %s", st.Name)
		m.SyncDesktop()
		return
	}

	if len(m.Stations) > 0 && m.SelectedIndex < len(m.Stations) {
		st := m.Stations[m.SelectedIndex]
		m.IdentifiedResult = nil
		m.IsIdentifying = false
		m.PlaybackStartTime = time.Now()
		m.LastFingerprintTime = time.Time{}
		m.resetNowPlaying()
		_ = m.Player.Play(st)
		m.PlayingID = st.ID
		m.StatusMessage = fmt.Sprintf("Playing %s [%s]", st.Name, m.Player.ActiveBackend())
		m.SyncDesktop()
		if m.Config.SongNotifications && m.Desktop != nil {
			m.Desktop.NotifySong(st.Name, st.Name)
		}
	}
}
