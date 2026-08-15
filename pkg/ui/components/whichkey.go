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

	if width >= 72 {
		// 2-column layout (calibrated for standard 72+ column terminals)
		col1 := []string{
			sectionStyle.Render("🧭 NAVIGATION"),
			formatRow("j / ↓", "Move down", 12),
			formatRow("k / ↑", "Move up", 12),
			formatRow("h / ←", "Prev tab / Sidebar", 12),
			formatRow("l / →", "Next tab / Main list", 12),
			formatRow("g / G", "Jump top / bottom", 12),
			formatRow("Ctrl+u/d", "Half page up / down", 12),
			"",
			sectionStyle.Render("🎵 PLAYBACK & VOLUME"),
			formatRow("Space/Enter", "Play / Pause stream", 12),
			formatRow("s", "Stop audio stream", 12),
			formatRow("r", "Play random station", 12),
			formatRow("+ / -", "Volume ±5%", 12),
			formatRow("m", "Toggle mute", 12),
		}

		col2 := []string{
			sectionStyle.Render("⭐ STATIONS & SHARING"),
			formatRow("f", "Toggle Favorite star", 11),
			formatRow("a", "Add custom station", 11),
			formatRow("e", "Edit local station", 11),
			formatRow("d", "Delete local station", 11),
			formatRow("p", "Export PR snippet", 11),
			"",
			sectionStyle.Render("🎨 INTERFACE & SEARCH"),
			formatRow("/", "Live search bar", 11),
			formatRow("w", "Filter activity mode", 11),
			formatRow("c", "Filter genre category", 11),
			formatRow("v", "Cycle visualizer mode", 11),
			formatRow("t", "Open Theme Picker", 11),
			formatRow("? / F1", "Toggle this help menu", 11),
			formatRow("q / ^c", "Quit halpradio", 11),
		}

		leftBox := strings.Join(col1, "\n")
		rightBox := strings.Join(col2, "\n")
		content = lipgloss.JoinHorizontal(lipgloss.Top, leftBox, "  ", rightBox)
		boxWidth = 70
		if width >= 80 {
			boxWidth = 76
		}
	} else {
		// 1-column compact layout for narrow/small terminals
		rows := []string{
			formatRow("j / k (↑/↓)", "Move selection", 14),
			formatRow("h / l (←/→)", "Switch tab / pane", 14),
			formatRow("Space/Enter", "Play / Pause stream", 14),
			formatRow("s / r", "Stop / Random", 14),
			formatRow("+ / - / m", "Volume ±5% / Mute", 14),
			formatRow("f / a / e", "Fav / Add / Edit", 14),
			formatRow("p", "Export PR snippet", 14),
			formatRow("/ / w / c", "Search / Mode / Genre", 14),
			formatRow("v / t", "Visualizer / Theme", 14),
			formatRow("? / q", "Help / Quit", 14),
		}
		content = strings.Join(rows, "\n")
		boxWidth = lipgloss.Width(content) + 4
		if boxWidth > width-2 {
			boxWidth = width - 2
		}
		if boxWidth < 40 {
			boxWidth = 40
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
	if height >= 24 {
		bodyElements = append(bodyElements, "")
	}
	bodyElements = append(bodyElements, content)
	if height >= 24 {
		bodyElements = append(bodyElements, "")
	}
	bodyElements = append(bodyElements, lipgloss.NewStyle().Width(boxWidth-4).Align(lipgloss.Center).Render(githubTip))
	if height >= 22 {
		bodyElements = append(bodyElements, lipgloss.NewStyle().Width(boxWidth-4).Align(lipgloss.Center).Foreground(th.Muted).Render("Press [ Esc ] or [ ? ] to return"))
	}

	padY := 1
	if height < 26 {
		padY = 0
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
