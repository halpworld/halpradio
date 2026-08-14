package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/theme"
)

func RenderWhichKeyOverlay(width int, height int, th theme.Theme) string {
	boxWidth := 74
	if width < 78 {
		boxWidth = width - 4
	}

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

	formatRow := func(k, desc string) string {
		return fmt.Sprintf("  %s %s", keyStyle.Render(padRight(k, 14)), descStyle.Render(desc))
	}

	col1 := []string{
		sectionStyle.Render("🧭 NAVIGATION"),
		formatRow("j / ↓", "Move down"),
		formatRow("k / ↑", "Move up"),
		formatRow("h / ←", "Prev tab / Sidebar"),
		formatRow("l / →", "Next tab / Main list"),
		formatRow("g / G", "Jump top / bottom"),
		formatRow("Ctrl+u / d", "Half page up / down"),
		"",
		sectionStyle.Render("🎵 PLAYBACK & VOLUME"),
		formatRow("Space / Enter", "Play / Pause station"),
		formatRow("s", "Stop audio stream"),
		formatRow("r", "Play random station"),
		formatRow("+ / -", "Volume up / down (5%)"),
		formatRow("m", "Toggle mute / unmute"),
	}

	col2 := []string{
		sectionStyle.Render("⭐ STATIONS & CONTRIBUTING"),
		formatRow("f", "Toggle Favorite star"),
		formatRow("a", "Add custom station"),
		formatRow("e", "Edit local station"),
		formatRow("d", "Delete local station"),
		formatRow("p", "Export PR snippet for GitHub!"),
		"",
		sectionStyle.Render("🎨 INTERFACE & SEARCH"),
		formatRow("/", "Focus live search bar"),
		formatRow("w", "Work / activity mode"),
		formatRow("c", "Filter by genre category"),
		formatRow("v", "Cycle visualizer animation"),
		formatRow("t", "Open Theme Picker modal"),
		formatRow("? / F1", "Toggle this WhichKey overlay"),
		formatRow("q / Ctrl+c", "Quit halpradio"),
	}

	leftBox := strings.Join(col1, "\n")
	rightBox := strings.Join(col2, "\n")

	content := lipgloss.JoinHorizontal(lipgloss.Top, leftBox, "    ", rightBox)

	githubTip := lipgloss.NewStyle().
		Foreground(th.Muted).
		Italic(true).
		Render("💡 Tip: Want to add a station permanently? Press 'p' to generate a GitHub PR snippet!")

	modalBox := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(th.Primary).
		Padding(1, 2).
		Width(boxWidth).
		Render(lipgloss.JoinVertical(
			lipgloss.Center,
			titleStyle.Render("⌨  HALPRADIO WHICH-KEY MAP (LAZYVIM STYLE)"),
			"",
			content,
			"",
			githubTip,
			"",
			lipgloss.NewStyle().Foreground(th.Muted).Render("Press [ Esc ] or [ ? ] to return"),
		))

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
