package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/theme"
)

func RenderWhichKeyOverlay(width int, height int, th theme.Theme) string {
	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary).
		Background(th.Background).
		Align(lipgloss.Center).
		Padding(0, 1)

	sectionStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Secondary)

	keyStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Highlight)

	descStyle := lipgloss.NewStyle().
		Foreground(th.Foreground)

	formatRow := func(k, desc string, keyWidth int) string {
		return fmt.Sprintf("  %s %s", keyStyle.Render(padRight(k, keyWidth)), descStyle.Render(desc))
	}

	var content string
	var boxWidth int

	if width >= 76 && height >= 36 {
		// 2-column full layout for large screens
		col1 := []string{
			sectionStyle.Render("🧭 NAVIGATION"),
			formatRow("j / ↓", "Move down / Tilt S", 12),
			formatRow("k / ↑", "Move up / Tilt N", 12),
			formatRow("h / ←", "Prev tab / Spin W", 12),
			formatRow("l / →", "Next tab / Spin E", 12),
			formatRow("M / 9", "3D Globe Explorer", 12),
			formatRow("C / H", "Countries / History", 12),
			formatRow("n / p", "Next/Prev stn / Cluster", 12),
			formatRow("g / G", "Jump top / bottom", 12),
			formatRow("Ctrl+u/d", "Half page up / down", 12),
			"",
			sectionStyle.Render("🎵 PLAYBACK & VOLUME"),
			formatRow("Space/Enter", "Play / Pause", 12),
			formatRow("s / x", "Stop audio stream", 12),
			formatRow("z / Z", "Timer / Pomodoro", 12),
			formatRow("r / R", "Play random station", 12),
			formatRow("+ / - / =", "Volume / Globe Zoom", 12),
			formatRow("m", "Toggle mute", 12),
		}

		col2 := []string{
			sectionStyle.Render("⭐ DISCOVERY & SHARING"),
			formatRow("y", "Yank (copy) track info", 11),
			formatRow("o", "Open streaming search", 11),
			formatRow("s", "Bookmark track (in Hist)", 11),
			formatRow("f", "Toggle Favorite star", 11),
			formatRow("a / e / d", "Add / Edit / Del station", 11),
			formatRow("p", "Export PR snippet", 11),
			formatRow("P", "Plugins & Extensions", 11),
			formatRow("c", "Clear history / Category", 11),
			"",
			sectionStyle.Render("🎨 INTERFACE & SEARCH"),
			formatRow("/", "Live search bar", 11),
			formatRow("w / C / c", "Mode / Country / Genre", 11),
			formatRow("v / t", "Visualizer / Theme", 11),
			formatRow("? / F1", "Toggle this help menu", 11),
			formatRow("q / ^c", "Quit halpradio", 11),
		}

		leftBox := strings.Join(col1, "\n")
		rightBox := strings.Join(col2, "\n")
		content = lipgloss.JoinHorizontal(lipgloss.Top, leftBox, "  ", rightBox)
		boxWidth = 78
	} else if width >= 60 && height >= 18 {
		// 2-column compact layout (fits perfectly in 70x22, 80x24, and 100x30)
		col1 := []string{
			sectionStyle.Render("🧭 NAVIGATION"),
			formatRow("j/k", "Move down/up", 8),
			formatRow("h/l", "Tabs / Spin", 8),
			formatRow("M/9", "Globe Explorer", 8),
			formatRow("C/H", "Countries/Hist", 8),
			formatRow("n/p", "Next/Prev stn", 8),
			formatRow("g/G", "Top/Bottom", 8),
			formatRow("Space", "Play / Pause", 8),
			formatRow("s/x", "Stop playback", 8),
			formatRow("z/r", "Timer/Random", 8),
			formatRow("+/-/m", "Vol/Zoom/Mute", 8),
		}

		col2 := []string{
			sectionStyle.Render("⭐ ACTIONS & SEARCH"),
			formatRow("y/o", "Yank/Search web", 8),
			formatRow("f/s", "Fav/Bookmark", 8),
			formatRow("a/e/d", "Add/Edit/Del", 8),
			formatRow("p/P", "PR/Plugins", 8),
			formatRow("c", "Clear/Category", 8),
			formatRow("/", "Search bar", 8),
			formatRow("w/C/c", "Mode/Cntry/Gen", 8),
			formatRow("v/t", "Visual/Theme", 8),
			formatRow("?", "Help overlay", 8),
			formatRow("q", "Quit halpradio", 8),
		}

		leftBox := strings.Join(col1, "\n")
		rightBox := strings.Join(col2, "\n")
		content = lipgloss.JoinHorizontal(lipgloss.Top, leftBox, "  ", rightBox)
		boxWidth = 58
		if width >= 72 {
			boxWidth = 64
		}
	} else {
		// 1-column compact layout for narrow/small terminals
		rows := []string{
			formatRow("j/k", "Move selection", 8),
			formatRow("h/l", "Tabs / Spin", 8),
			formatRow("n/p", "Next/Prev stn", 8),
			formatRow("M / 9", "Globe Explorer", 8),
			formatRow("Space", "Play / Pause", 8),
			formatRow("s / x", "Stop playback", 8),
			formatRow("z / r", "Timer/Random", 8),
			formatRow("y / f", "Yank/Favorite", 8),
			formatRow("+/-/m", "Vol/Zoom/Mute", 8),
			formatRow("p / P", "PR/Plugins", 8),
			formatRow("/ / ?", "Search/Help", 8),
			formatRow("q", "Quit", 8),
		}
		content = strings.Join(rows, "\n")
		boxWidth = lipgloss.Width(content) + 4
		if boxWidth > width-2 {
			boxWidth = width - 2
		}
		if boxWidth < 32 {
			boxWidth = 32
		}
	}

	tipText := "💡 Tip: Press 'p' to copy a Pull Request snippet!"
	if boxWidth < 55 {
		tipText = "💡 Tip: Press 'p' to export PR!"
	}

	githubTip := lipgloss.NewStyle().
		Foreground(th.Muted).
		Italic(true).
		Render(tipText)

	var bodyElements []string
	bodyElements = append(bodyElements, lipgloss.NewStyle().Width(boxWidth-4).Align(lipgloss.Center).Render(titleStyle.Render("⌨  HALPRADIO SHORTCUTS")))
	if height >= 34 {
		bodyElements = append(bodyElements, "")
	}
	bodyElements = append(bodyElements, content)
	if height >= 32 {
		bodyElements = append(bodyElements, "")
		bodyElements = append(bodyElements, lipgloss.NewStyle().Width(boxWidth-4).Align(lipgloss.Center).Render(githubTip))
	}
	if height >= 20 {
		bodyElements = append(bodyElements, lipgloss.NewStyle().Width(boxWidth-4).Align(lipgloss.Center).Foreground(th.Muted).Render("Press [ Esc ] or [ ? ] to return"))
	}

	padY := 0
	if height >= 36 {
		padY = 1
	}

	modalBox := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(th.Primary).
		Padding(padY, 1).
		Width(boxWidth).
		Render(lipgloss.JoinVertical(lipgloss.Left, bodyElements...))

	return PlaceOverlay(modalBox, width, height)
}

func PlaceOverlay(dialog string, width int, height int) string {
	return lipgloss.Place(
		width,
		height,
		lipgloss.Center,
		lipgloss.Center,
		dialog,
	)
}
