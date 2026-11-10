package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/ai"
	"github.com/halpworld/halpradio/pkg/art"
	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/lyrics"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/player/dsp"
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
	FocusLinerNotes
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

// PartyJoinRoomMsg joins a room by invite (see party.ParseInvite): a bare
// room code, or the code and the host fingerprint, which pins the host. It
// is sent from the local IPC
// socket, so it deliberately has no peer address field: addresses are only
// taken from the user's own input in the party Join form or CLI flags.
type PartyJoinRoomMsg struct {
	RoomCode string // a room code or a full invite
	Nickname string
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
	// LocaleCountry is the listener's country from their locale; it is
	// listed first on the Country tab.
	LocaleCountry string

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
	// PluginSelectedID is the installed plugin the cursor is on. Keys act on
	// this ID, never on a row index, so a list that changes between draw and
	// key press cannot redirect an action (FR-A-4).
	PluginSelectedID string
	// PendingApprovalID is a plugin installed while another modal or a text
	// input was active. Its prompt opens only from the plugin manager.
	PendingApprovalID string
	// ApprovalScroll is the first visible line of the rule list.
	ApprovalScroll int
	// ApprovalSeenEnd is set once the last line of the rule list has been on
	// screen; "y" is refused before that.
	ApprovalSeenEnd bool
	// ApprovalArmed is set by "y"; Enter then approves, any other key cancels.
	ApprovalArmed bool
	// ApprovalShownAt is when the prompt became visible. Keys are ignored
	// for approvalKeyGuard after it.
	ApprovalShownAt time.Time
	// ApprovalHint is the prompt's feedback line ("scroll to the end…").
	ApprovalHint string
	// ConfigWarning is shown on every frame while config.yaml could not be
	// read (fail-closed config, FR-A-3).
	ConfigWarning string
	// Now is the model's clock. Tests replace it; nil means time.Now.
	Now func() time.Time
	// CatalogMinStations is the embedded catalog's station count. The
	// background catalog update refuses a download much smaller than it.
	CatalogMinStations int

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
	// LyricsLookupFailed marks LyricsTrackKey as a failed lookup (providers
	// unreachable). The key is kept so ticks do not resend it (NET-7); the
	// next explicit open of the drawer retries.
	LyricsLookupFailed bool
	TrackStartTime     time.Time

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
	// ArtLookupFailed is the cover art counterpart of LyricsLookupFailed.
	ArtLookupFailed bool

	// ArtClearFrames counts down the frames that still carry the protocol's
	// image-delete escape. Kitty placements survive a text repaint, so a
	// closed drawer has to explicitly evict them.
	ArtClearFrames int

	// Graphic equalizer & DSP rack modal. DSP mirrors the player's settings;
	// every edit is pushed to the player at once and saved to dsp.yaml on close.
	ShowEQModal bool
	EQBand      int
	DSP         dsp.Settings

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

	// Local AI Radio DJ & Interactive Liner Notes Companion state
	DJ                   *ai.DJ
	LinerNotesClient     *ai.LinerNotesClient
	ShowLinerNotes       bool
	LinerNotes           *ai.LinerNotes
	LinerNotesStatus     string
	IsFetchingLinerNotes bool
	LinerNotesScroll     int
	LinerNotesTrackKey   string
	DJBannerActive       bool
	DJBannerFading       bool
	DJBannerScript       string
	DJBannerEngine       string
	DJBannerPersona      string
	DJBannerUntil        time.Time
	DJAnnouncedTrackKey  string
	DJAnnounceSeq        uint64
	ShowDJModal          bool
	DJModalCursor        int
	DJModalStatus        string
	DJInstalledVoices    map[string]bool
	IsDownloadingVoice   bool

	// partySyncSeq numbers incoming party syncs so a slow host-name check
	// cannot start a stream after a newer sync arrived.
	partySyncSeq uint64
	// partyHostAnnounced remembers which session/host fingerprint the
	// "Following host" notice was shown for.
	partyHostAnnounced string
	// partyPendingSync is the latest host sync not applied yet (held for a
	// new host, or coalesced); the latest one wins.
	partyPendingSync *party.SyncPayload
	// partyHoldUntil is when syncs from a newly followed host may apply.
	partyHoldUntil time.Time
	// partyLastApply is when a host sync last changed playback.
	partyLastApply time.Time
	// partyFlushPending is set while a partySyncFlushMsg is scheduled.
	partyFlushPending bool
}

// now returns the model's clock.
func (m Model) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
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

	localeCountry := radio.LocaleCountry()

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
		Countries:                  radio.PrioritizeCountry(store.GetCountries(), localeCountry),
		LocaleCountry:              localeCountry,
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
		// Strict (AllowPrivate nil); identifyTrackCmd sets the policy per
		// request from the current station's Source.
		FingerprintClient: fingerprint.NewClient(cfg.AcoustidAPIKey),
		PartyInputs:       make([]string, 3),
		LyricsOffset:      time.Duration(cfg.LyricsOffsetMs) * time.Millisecond,
		ShowLyrics:        cfg.LyricsEnabled && cfg.LyricsAutoOpen,
	}

	if pm != nil {
		m.DSP = pm.DSPSettings()
	} else {
		m.DSP = dsp.DefaultSettings()
	}

	if cfg.LyricsEnabled {
		m.LyricsClient = lyrics.NewClient(util.GetLyricsCacheDir())
		m.LyricsClient.DisableNetEase = !cfg.LyricsNetEaseEnabled
	}
	if cfg.AlbumArtEnabled {
		m.ArtProtocol = art.Resolve(cfg.AlbumArtProtocol)
		m.ArtRenderer = art.NewRenderer(m.ArtProtocol)
		m.ArtClient = art.NewClient(util.GetAlbumArtCacheDir(), cfg.LastFMAPIKey)
		m.ArtClient.DisableITunes = !cfg.ArtITunesEnabled
		m.ArtClient.DisableDeezer = !cfg.ArtDeezerEnabled
		m.ArtClient.DisableMusicBrainz = !cfg.ArtMusicBrainzEnabled
	} else {
		m.ArtProtocol = art.ProtocolNone
	}

	m.DJ = ai.NewDJ(ai.DJConfig{
		PersonaID:   cfg.AIDJPersona,
		TTSEngine:   cfg.AIDJTTSEngine,
		VoiceModel:  cfg.AIDJVoiceModel,
		OllamaURL:   cfg.AIDJOllamaURL,
		OllamaModel: cfg.AIDJOllamaModel,
		DuckLevel:   cfg.AIDJDuckLevel,
	})
	m.LinerNotesClient = ai.NewLinerNotesClient()
	m.refreshDJInstalledVoices()

	allThemes := theme.GetAllThemes()
	for i, t := range allThemes {
		if t.ID == cfg.Theme || t.Name == th.Name || t.ID == th.ID {
			m.ThemeCursor = i
			break
		}
	}

	if cfg.AIDJEnabled && strings.TrimSpace(cfg.AIDJOllamaURL) != "" {
		if _, err := ai.ValidateOllamaURL(cfg.AIDJOllamaURL); err != nil {
			m.StatusMessage = util.SanitizeDisplay("⚠ AI DJ: Ollama URL not used ("+err.Error()+"); using offline templates", 300)
		}
	}

	if w := cfg.LoadWarning(); w != "" {
		m.ConfigWarning = "⚠ " + w
		m.StatusMessage = m.ConfigWarning
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

// clampSidebarIndices keeps GenreIndex and CountryIndex inside the current
// lists after the catalog changed (a smaller catalog update used to leave an
// index past the end, and the next key press indexed out of range). The
// selection follows its value when it still exists; otherwise it falls back
// to "All".
func (m *Model) clampSidebarIndices() {
	if m.GenreIndex < 0 || m.GenreIndex > len(m.Genres) ||
		(m.GenreIndex > 0 && m.Genres[m.GenreIndex-1] != m.SelectedGenre) {
		m.GenreIndex = 0
		for i, g := range m.Genres {
			if m.SelectedGenre != "" && g == m.SelectedGenre {
				m.GenreIndex = i + 1
				break
			}
		}
		if m.GenreIndex == 0 {
			m.SelectedGenre = ""
		}
	}
	if m.CountryIndex < 0 || m.CountryIndex > len(m.Countries) ||
		(m.CountryIndex > 0 && m.Countries[m.CountryIndex-1].Code != m.SelectedCountry) {
		m.CountryIndex = 0
		for i, c := range m.Countries {
			if m.SelectedCountry != "" && c.Code == m.SelectedCountry {
				m.CountryIndex = i + 1
				break
			}
		}
		if m.CountryIndex == 0 {
			m.SelectedCountry = ""
		}
	}
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

	m.Countries = radio.PrioritizeCountry(m.Store.GetCountries(), m.LocaleCountry)
	m.Genres = m.Store.GetCategories()
	m.clampSidebarIndices()
	if m.ActiveTab == 3 {
		selectedGenre = m.SelectedGenre
	}
	if m.ActiveTab == 2 {
		selectedCountry = m.SelectedCountry
	}

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

// WindowTitle is the terminal title. It embeds ICY titles and station
// names from the network, so it is sanitized: an OSC title sequence ends at
// BEL or ESC, and a raw one would let a stream inject escape sequences
// (TTY-2).
func (m Model) WindowTitle() string {
	return util.SanitizeDisplay(m.rawWindowTitle(), 200)
}

func (m Model) rawWindowTitle() string {
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
		minStations := m.CatalogMinStations
		if minStations == 0 && m.Store != nil {
			minStations = len(m.Store.Bundled)
		}
		cmds = append(cmds, checkCatalogUpdateCmd(m.Config, minStations))
	}
	return tea.Batch(cmds...)
}

func checkCatalogUpdateCmd(cfg util.Config, minStations int) tea.Cmd {
	return func() tea.Msg {
		updater := radio.NewCatalogUpdater(cfg.CatalogUpdateURL, cfg.CatalogCacheTTLHours)
		updater.MinStations = minStations
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

// enterPartyFormScreen switches the party modal to the create (1) or join (2)
// form. The forms share PartyInputs, so each gets its own defaults: the join
// form starts with an empty code and address (the create form's "team-focus"
// and "host" are not a room code or a peer address).
func (m *Model) enterPartyFormScreen(screen int) {
	nick := m.Config.PartyNickname
	if len(m.PartyInputs) > 1 && strings.TrimSpace(m.PartyInputs[1]) != "" {
		nick = m.PartyInputs[1]
	}
	if nick == "" {
		nick = "listener"
	}
	if screen == 2 {
		m.PartyInputs = []string{"", nick, ""}
	} else {
		m.PartyInputs = []string{"team-focus", nick, "host"}
	}
	m.PartyModalScreen = screen
	m.PartyInputFocus = 0
	m.PartyStatusMsg = ""
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
	m.cancelDJAnnouncement()
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
	m.cancelDJAnnouncement()
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
		m.cancelDJAnnouncement()
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
		m.cancelDJAnnouncement()
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

// statusLine is the status bar text: the config warning, when there is one,
// stays in front of every other message until config.yaml is fixed.
func (m Model) statusLine() string {
	if m.ConfigWarning == "" || strings.Contains(m.StatusMessage, m.ConfigWarning) {
		return m.StatusMessage
	}
	if m.StatusMessage == "" {
		return m.ConfigWarning
	}
	return m.ConfigWarning + " • " + m.StatusMessage
}
