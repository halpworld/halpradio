package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/theme"
	"github.com/halpworld/halpradio/pkg/ui/components"
	"github.com/halpworld/halpradio/pkg/ui/components/globe"
	"github.com/halpworld/halpradio/pkg/ui/components/tuner"
	"github.com/halpworld/halpradio/pkg/ui/modals"
)

func (m Model) View() string {
	// A terminal image placed by Kitty outlives a text repaint, so a frame
	// where artwork just disappeared has to carry the delete escape.
	artClear := m.ArtClearSequence()

	width := m.Width
	height := m.Height

	if width == 0 || height == 0 {
		width = 80
		height = 24
	}

	if m.ShowWhichKey {
		return artClear + components.RenderWhichKeyOverlay(width, height, m.Theme)
	}

	if m.ShowPRExport {
		return artClear + components.RenderPRExportModal(m.ExportStation, width, height, m.Theme)
	}

	if m.ShowArtModal {
		return artClear + components.RenderAlbumArtModal(components.AlbumArtModalInput{
			Cover:      m.Cover,
			Lines:      m.ArtLines,
			Status:     m.ArtStatus,
			Fetching:   m.IsFetchingArt,
			Protocol:   m.ArtProtocol,
			TrackLabel: m.nowPlayingTrack(),
			Width:      width,
			Height:     height,
		}, m.Theme)
	}

	if m.ShowThemePicker {
		installed := theme.GetAllThemes()
		cursor := m.ThemeCursor
		if m.ThemeModalTab == 1 {
			cursor = m.ThemeRegistryCursor
		}
		return artClear + components.RenderThemePickerModal(
			installed,
			m.ThemeRegistryList,
			m.ThemeModalTab,
			cursor,
			m.Config.Theme,
			m.IsPreviewingTheme,
			m.ThemeStatusMsg,
			m.ThemeSearchQuery,
			width,
			height,
			m.Theme,
		)
	}

	if m.ShowAddModal {
		return artClear + components.RenderAddStationModal(m.AddInputs, m.AddFocusIdx, m.AddErrMsg, width, height, m.Theme)
	}

	if m.ShowTimerModal {
		return artClear + components.RenderTimerModal(
			m.Timer,
			m.TimerModalScreen,
			m.TimerMenuCursor,
			m.TimerPomodoroInputs,
			m.TimerPomodoroFocusIdx,
			m.TimerCustomSleepInput,
			m.TimerPomodoroNotifyDesktop,
			m.TimerPomodoroNotifyBell,
			width,
			height,
			m.Theme,
		)
	}

	if m.ShowPartyModal {
		return artClear + components.RenderPartyManagerModal(
			m.PartySession,
			m.PartyModalScreen,
			m.PartyModalCursor,
			m.PartyInputs,
			m.PartyInputFocus,
			m.PartyStatusMsg,
			width,
			height,
			m.Theme,
		)
	}

	if m.ShowPermissionApproval {
		return artClear + components.RenderPermissionApprovalModal(m.ApprovalPlugin, width, height, m.Theme)
	}

	if m.ShowPluginModal {
		var installed []plugin.PluginInfo
		if m.PluginMgr != nil {
			installed = m.PluginMgr.GetPlugins()
		}
		return artClear + components.RenderPluginManagerModal(
			installed,
			m.PluginRegistryList,
			m.PluginModalTab,
			m.PluginCursor,
			m.PluginStatusMsg,
			width,
			height,
			m.Theme,
		)
	}

	if m.ShowDJModal {
		activeLabel := ""
		if m.DJ != nil {
			activeLabel = m.DJ.EngineLabel()
		}
		return artClear + modals.RenderDJSettingsModal(modals.DJSettingsModalInput{
			Enabled:           m.Config.AIDJEnabled,
			PersonaID:         m.Config.AIDJPersona,
			TTSEngine:         m.Config.AIDJTTSEngine,
			ActiveEngineLabel: activeLabel,
			SelectedVoiceID:   m.Config.AIDJVoiceModel,
			OllamaURL:         m.Config.AIDJOllamaURL,
			OllamaModel:       m.Config.AIDJOllamaModel,
			DuckLevel:         m.Config.AIDJDuckLevel,
			InstalledModels:   m.DJInstalledVoices,
			Cursor:            m.DJModalCursor,
			Downloading:       m.IsDownloadingVoice,
			Status:            m.DJModalStatus,
			Width:             width,
			Height:            height,
		}, m.Theme)
	}

	isTunerActive := m.Config.ExperimentalTuner && m.ActiveTuner
	headerView := components.RenderHeader(width, m.ActiveTab, m.Player.Status(), m.Player.ActiveBackend(), m.Theme, isTunerActive)
	headerHeight := lipgloss.Height(headerView)

	timerBadge := ""
	if m.Timer != nil && m.Timer.IsActive() {
		timerBadge = m.Timer.BadgeText()
	}

	var playerBarView string
	if m.PartySession != nil && m.PartySession.IsActive() {
		playerBarView = components.RenderPartyBar(
			m.PartySession.RoomCode(),
			m.PartySession.RoomName(),
			m.PartySession.HostNickname(),
			m.PartySession.IsHost(),
			m.PartySession.DJPass(),
			m.PartySession.PeerCount(),
			m.Player.CurrentStation(),
			m.Player.CurrentTrack(),
			m.Player.Status(),
			m.Player.Volume(),
			m.Player.IsMuted(),
			m.Visualizer,
			m.PartySession.ActiveReactions(),
			m.PartySession.RecentChat(),
			width,
			m.Theme,
			m.IsChatting,
			m.ChatInput,
		)
	} else {
		playerBarView = components.RenderPlayerBar(
			m.Player.CurrentStation(),
			m.Player.CurrentTrack(),
			m.Player.Status(),
			m.Player.Volume(),
			m.Player.IsMuted(),
			m.Visualizer,
			timerBadge,
			width,
			m.Theme,
			m.IdentifiedResult,
			m.IsIdentifying,
		)
	}
	playerBarHeight := lipgloss.Height(playerBarView)

	statusBarView := components.RenderStatusBar(m.SearchQuery, m.StatusMessage, m.ActiveTab, width, m.Theme, isTunerActive)
	statusBarHeight := lipgloss.Height(statusBarView)

	var djBannerView string
	djBannerHeight := 0
	if m.DJBannerActive {
		candidate := modals.RenderDJBanner(modals.DJBannerInput{
			Persona:     m.DJBannerPersona,
			Engine:      m.DJBannerEngine,
			Script:      m.DJBannerScript,
			DuckPercent: m.Config.AIDJDuckLevel,
			Fading:      m.DJBannerFading,
			Width:       width,
		}, m.Theme)
		candH := lipgloss.Height(candidate)
		if height-headerHeight-playerBarHeight-statusBarHeight-1-candH >= 6 {
			djBannerView = candidate
			djBannerHeight = candH
		}
	}

	mainContentHeight := height - headerHeight - playerBarHeight - statusBarHeight - djBannerHeight - 1
	if mainContentHeight < 3 {
		mainContentHeight = 3
	}

	// Beside the station list the drawer steals columns so the list keeps its
	// own layout. On a terminal too narrow for both, the sheet takes over the
	// content area instead of silently declining to appear.
	lyricsSurface, lyricsWidth := m.lyricsSurface()
	linerSurface, linerWidth := m.linerNotesSurface()
	contentWidth := width
	if lyricsSurface == LyricsSurfaceDrawer {
		contentWidth = width - lyricsWidth - 1
		if contentWidth < 24 {
			lyricsSurface = LyricsSurfaceOverlay
			lyricsWidth = width
			contentWidth = width
		}
	} else if linerSurface == LyricsSurfaceDrawer {
		contentWidth = width - linerWidth - 1
		if contentWidth < 24 {
			linerSurface = LyricsSurfaceOverlay
			linerWidth = width
			contentWidth = width
		}
	}

	var mainArea string
	if m.ActiveTab == 0 {
		var actItems []string
		var selectedStr string
		for i, act := range m.Activities {
			str := act.Icon + " " + act.Name
			actItems = append(actItems, str)
			if m.ActivityIndex == i+1 {
				selectedStr = str
			}
		}
		sidebarW := 26
		if contentWidth < 65 {
			sidebarW = 18
		}
		sidebarView := components.RenderSidebar(
			" WORK MODES ",
			actItems,
			selectedStr,
			m.ActivityIndex,
			sidebarW,
			mainContentHeight,
			m.ActiveFocus == FocusSidebar,
			m.Theme,
		)
		listWidth := contentWidth - sidebarW - 1
		if listWidth < 20 {
			listWidth = 20
		}
		stationListView := components.RenderStationList(
			m.Stations,
			m.SelectedIndex,
			m.PlayingID,
			listWidth,
			mainContentHeight,
			m.ActiveFocus == FocusMainList,
			m.Theme,
		)
		mainArea = lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, " ", stationListView)
	} else if m.ActiveTab == 2 {
		var countryItems []string
		var selectedStr string
		for i, c := range m.Countries {
			str := fmt.Sprintf("%s %s (%d)", c.Flag, c.Name, c.Count)
			countryItems = append(countryItems, str)
			if m.CountryIndex == i+1 {
				selectedStr = str
			}
		}
		sidebarW := 28
		if contentWidth < 70 {
			sidebarW = 18
		}
		sidebarView := components.RenderSidebar(
			" COUNTRIES / FM ",
			countryItems,
			selectedStr,
			m.CountryIndex,
			sidebarW,
			mainContentHeight,
			m.ActiveFocus == FocusSidebar,
			m.Theme,
		)
		listWidth := contentWidth - sidebarW - 1
		if listWidth < 20 {
			listWidth = 20
		}
		stationListView := components.RenderStationList(
			m.Stations,
			m.SelectedIndex,
			m.PlayingID,
			listWidth,
			mainContentHeight,
			m.ActiveFocus == FocusMainList,
			m.Theme,
		)
		mainArea = lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, " ", stationListView)
	} else if m.ActiveTab == 3 {
		sidebarW := 26
		if contentWidth < 65 {
			sidebarW = 18
		}
		sidebarView := components.RenderSidebar(
			" GENRES / TAGS ",
			m.Genres,
			m.SelectedGenre,
			m.GenreIndex,
			sidebarW,
			mainContentHeight,
			m.ActiveFocus == FocusSidebar,
			m.Theme,
		)
		listWidth := contentWidth - sidebarW - 1
		if listWidth < 20 {
			listWidth = 20
		}
		stationListView := components.RenderStationList(
			m.Stations,
			m.SelectedIndex,
			m.PlayingID,
			listWidth,
			mainContentHeight,
			m.ActiveFocus == FocusMainList,
			m.Theme,
		)
		mainArea = lipgloss.JoinHorizontal(lipgloss.Top, sidebarView, " ", stationListView)
	} else if m.ActiveTab == 7 {
		mainArea = components.RenderHistoryList(
			m.Store.GetHistory(),
			m.HistoryIndex,
			contentWidth,
			mainContentHeight,
			m.Theme,
		)
	} else if m.ActiveTab == 8 {
		if m.Config.ExperimentalTuner && m.ActiveTuner {
			mainArea = tuner.RenderAnalogTunerView(
				m.Store.GetAllStations(),
				m.TunerFreq,
				m.TunerBand,
				m.PlayingID,
				contentWidth,
				mainContentHeight,
				m.Theme,
			)
		} else {
			mainArea = globe.RenderGlobeView(
				m.GlobeClusters,
				m.GlobeLat,
				m.GlobeLon,
				m.GlobeZoom,
				m.GlobeStationIndex,
				m.PlayingID,
				contentWidth,
				mainContentHeight,
				m.Theme,
			)
		}
	} else {
		mainArea = components.RenderStationList(
			m.Stations,
			m.SelectedIndex,
			m.PlayingID,
			contentWidth,
			mainContentHeight,
			true,
			m.Theme,
		)
	}

	if lyricsSurface != LyricsSurfaceHidden {
		lyricsView := components.RenderLyricsDrawer(components.LyricsDrawerInput{
			Sheet:      m.LyricsSheet,
			Status:     m.LyricsStatus,
			Fetching:   m.IsFetchingLyrics,
			TrackLabel: m.nowPlayingTrack(),
			ArtLines:   m.ArtLines,
			ArtCols:    m.ArtCols,
			ArtSource:  m.coverSourceLabel(),
			Elapsed:    m.lyricElapsed(),
			Offset:     m.LyricsOffset,
			Scroll:     m.LyricsScroll,
			Focused:    m.ActiveFocus == FocusLyrics,
			Width:      lyricsWidth,
			Height:     mainContentHeight,
		}, m.Theme)
		if lyricsSurface == LyricsSurfaceDrawer {
			mainArea = lipgloss.JoinHorizontal(lipgloss.Top, mainArea, " ", lyricsView)
		} else {
			mainArea = lyricsView
		}
	} else if linerSurface != LyricsSurfaceHidden {
		var stName, stGenre, stCountry string
		if st := m.activeOrSelectedStation(); st != nil {
			stName = st.Name
			stGenre = st.Genre
			stCountry = st.Country
		}
		linerView := modals.RenderLinerNotesDrawer(modals.LinerNotesDrawerInput{
			Notes:       m.LinerNotes,
			Status:      m.LinerNotesStatus,
			Fetching:    m.IsFetchingLinerNotes,
			TrackLabel:  m.nowPlayingTrack(),
			StationName: stName,
			Genre:       stGenre,
			Country:     stCountry,
			ArtLines:    m.ArtLines,
			ArtCols:     m.ArtCols,
			ArtSource:   m.coverSourceLabel(),
			Scroll:      m.LinerNotesScroll,
			Focused:     m.ActiveFocus == FocusLinerNotes,
			Width:       linerWidth,
			Height:      mainContentHeight,
		}, m.Theme)
		if linerSurface == LyricsSurfaceDrawer {
			mainArea = lipgloss.JoinHorizontal(lipgloss.Top, mainArea, " ", linerView)
		} else {
			mainArea = linerView
		}
	}

	if djBannerView != "" {
		return artClear + lipgloss.JoinVertical(
			lipgloss.Left,
			headerView,
			mainArea,
			djBannerView,
			playerBarView,
			statusBarView,
		)
	}

	return artClear + lipgloss.JoinVertical(
		lipgloss.Left,
		headerView,
		mainArea,
		playerBarView,
		statusBarView,
	)
}
