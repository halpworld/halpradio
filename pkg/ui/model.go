package ui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
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

type Model struct {
	Width  int
	Height int

	Store      *radio.Store
	Player     *player.Manager
	Config     util.Config
	RBClient   *radio.RadioBrowserClient
	KeyMap     KeyMap
	Theme      theme.Theme
	Visualizer *components.Visualizer

	ActiveTab     int // 0: Catalog, 1: Activities, 2: Genres, 3: Favorites, 4: RadioBrowser, 5: Custom, 6: History
	ActiveFocus   FocusArea
	Stations      []radio.Station
	RBStations    []radio.Station
	SelectedIndex int
	HistoryIndex  int

	Activities       []radio.Activity
	SelectedActivity string
	ActivityIndex    int

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

	AddInputs        []string
	AddFocusIdx      int
	AddErrMsg        string
	EditingStationID string
	ExportStation    radio.Station
}

func NewModel(
	store *radio.Store,
	pm *player.Manager,
	cfg util.Config,
) Model {
	th := theme.GetTheme(cfg.Theme)
	viz := components.NewVisualizer(cfg.VisualizerMode)

	m := Model{
		Store:            store,
		Player:           pm,
		Config:           cfg,
		RBClient:         radio.NewRadioBrowserClient(),
		KeyMap:           DefaultKeyMap(),
		Theme:            th,
		Visualizer:       viz,
		ActiveTab:        0,
		ActiveFocus:      FocusMainList,
		Activities:       radio.DefaultActivities,
		SelectedActivity: "",
		ActivityIndex:    0,
		Genres:           store.GetCategories(),
		AddInputs:        make([]string, 5),
		HistoryIndex:     0,
	}

	m.RefreshStations()
	return m
}

func (m *Model) SwitchTab(tabIndex int) {
	if tabIndex < 0 {
		tabIndex = 0
	} else if tabIndex > 6 {
		tabIndex = 6
	}
	m.ActiveTab = tabIndex
	m.SelectedIndex = 0
	m.HistoryIndex = 0
	if m.ActiveTab == 1 || m.ActiveTab == 2 {
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
		baseList = m.Store.GetFavorites()
	case 4:
		baseList = m.RBStations
	case 5:
		baseList = m.Store.Local
	case 6:
		baseList = nil
	default:
		baseList = m.Store.GetAllStations()
	}

	selectedGenre := ""
	if m.ActiveTab == 2 {
		selectedGenre = m.SelectedGenre
	}

	selectedActivity := ""
	if m.ActiveTab == 1 {
		selectedActivity = m.SelectedActivity
	}

	m.Stations = radio.FilterWithActivity(baseList, m.SearchQuery, selectedGenre, selectedActivity)
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
	tabNames := []string{"Catalog", "Activities", "Genres", "Favorites", "RadioBrowser", "Custom", "History"}
	tabName := "Catalog"
	if m.ActiveTab >= 0 && m.ActiveTab < len(tabNames) {
		tabName = tabNames[m.ActiveTab]
	}

	st := m.Player.CurrentStation()
	if st != nil && m.Player.Status() == player.StatusPlaying {
		track := m.Player.CurrentTrack()
		if track != "" {
			return fmt.Sprintf("▶ %s - %s | halpradio", track, st.Name)
		}
		return fmt.Sprintf("▶ %s | halpradio", st.Name)
	}
	if st != nil && m.Player.Status() == player.StatusConnecting {
		return fmt.Sprintf("⟳ Connecting: %s | halpradio", st.Name)
	}
	if st != nil && m.Player.Status() == player.StatusPaused {
		return fmt.Sprintf("⏸ %s | halpradio", st.Name)
	}

	return fmt.Sprintf("halpradio - %d: %s", m.ActiveTab+1, tabName)
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		tea.SetWindowTitle(m.WindowTitle()),
	)
}

func tickCmd() tea.Cmd {
	return tea.Every(150*time.Millisecond, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}
