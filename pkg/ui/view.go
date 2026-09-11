package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/theme"
	"github.com/halpworld/halpradio/pkg/ui/components"
	"github.com/halpworld/halpradio/pkg/ui/components/globe"
	"github.com/halpworld/halpradio/pkg/ui/components/tuner"
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

	mainContentHeight := height - headerHeight - playerBarHeight - statusBarHeight - 1
	if mainContentHeight < 3 {
		mainContentHeight = 3
	}

	// Beside the station list the drawer steals columns so the list keeps its
	// own layout. On a terminal too narrow for both, the sheet takes over the
	// content area instead of silently declining to appear.
	lyricsSurface, lyricsWidth := m.lyricsSurface()
	contentWidth := width
	if lyricsSurface == LyricsSurfaceDrawer {
		contentWidth = width - lyricsWidth - 1
		if contentWidth < 24 {
			lyricsSurface = LyricsSurfaceOverlay
			lyricsWidth = width
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
	}

	return artClear + lipgloss.JoinVertical(
		lipgloss.Left,
		headerView,
		mainArea,
		playerBarView,
		statusBarView,
	)
}
