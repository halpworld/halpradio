package ui

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/ui/components"
)

func (m Model) View() string {
	width := m.Width
	height := m.Height

	if width == 0 || height == 0 {
		width = 80
		height = 24
	}

	if m.ShowWhichKey {
		return components.RenderWhichKeyOverlay(width, height, m.Theme)
	}

	if m.ShowPRExport {
		return components.RenderPRExportModal(m.ExportStation, width, height, m.Theme)
	}

	if m.ShowThemePicker {
		return components.RenderThemePickerModal(m.Config.Theme, width, height, m.Theme)
	}

	if m.ShowAddModal {
		return components.RenderAddStationModal(m.AddInputs, m.AddFocusIdx, m.AddErrMsg, width, height, m.Theme)
	}

	if m.ShowTimerModal {
		return components.RenderTimerModal(
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

	if m.ShowPermissionApproval {
		return components.RenderPermissionApprovalModal(m.ApprovalPlugin, width, height, m.Theme)
	}

	if m.ShowPluginModal {
		var installed []plugin.PluginInfo
		if m.PluginMgr != nil {
			installed = m.PluginMgr.GetPlugins()
		}
		return components.RenderPluginManagerModal(
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

	headerView := components.RenderHeader(width, m.ActiveTab, m.Player.Status(), m.Player.ActiveBackend(), m.Theme)
	headerHeight := lipgloss.Height(headerView)

	timerBadge := ""
	if m.Timer != nil && m.Timer.IsActive() {
		timerBadge = m.Timer.BadgeText()
	}

	playerBarView := components.RenderPlayerBar(
		m.Player.CurrentStation(),
		m.Player.CurrentTrack(),
		m.Player.Status(),
		m.Player.Volume(),
		m.Player.IsMuted(),
		m.Visualizer,
		timerBadge,
		width,
		m.Theme,
	)
	playerBarHeight := lipgloss.Height(playerBarView)

	statusBarView := components.RenderStatusBar(m.SearchQuery, m.StatusMessage, m.ActiveTab, width, m.Theme)
	statusBarHeight := lipgloss.Height(statusBarView)

	mainContentHeight := height - headerHeight - playerBarHeight - statusBarHeight - 1
	if mainContentHeight < 3 {
		mainContentHeight = 3
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
		if width < 65 {
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
		listWidth := width - sidebarW - 1
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
		sidebarW := 26
		if width < 65 {
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
		listWidth := width - sidebarW - 1
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
	} else if m.ActiveTab == 6 {
		mainArea = components.RenderHistoryList(
			m.Store.GetHistory(),
			m.HistoryIndex,
			width,
			mainContentHeight,
			m.Theme,
		)
	} else {
		mainArea = components.RenderStationList(
			m.Stations,
			m.SelectedIndex,
			m.PlayingID,
			width,
			mainContentHeight,
			true,
			m.Theme,
		)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		headerView,
		mainArea,
		playerBarView,
		statusBarView,
	)
}
