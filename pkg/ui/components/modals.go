package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
)

func RenderPRExportModal(st radio.Station, width int, height int, th theme.Theme) string {
	boxWidth := 66
	if width < 70 {
		boxWidth = width - 4
	}
	if boxWidth < 36 {
		boxWidth = 36
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary).
		Align(lipgloss.Center)

	descStyle := lipgloss.NewStyle().
		Foreground(th.Foreground)

	codeStyle := lipgloss.NewStyle().
		Background(th.Border).
		Foreground(th.Highlight).
		Padding(0, 1)

	instructionStyle := lipgloss.NewStyle().
		Foreground(th.Secondary).
		Italic(true)

	yamlSnippet := st.ToYAMLSnippet()

	msg := lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render("🐙 CONTRIBUTE TO HALPRADIO ON GITHUB"),
		"",
		descStyle.Render(fmt.Sprintf("Station '%s' copied to clipboard!", truncate(st.Name, boxWidth-8))),
		instructionStyle.Render("1. Fork https://github.com/halpworld/halpradio"),
		instructionStyle.Render("2. Paste snippet into 'stations.yaml' & open PR!"),
		"",
		codeStyle.Render(yamlSnippet),
		"",
		lipgloss.NewStyle().Foreground(th.Playing).Bold(true).Render("✓ Copied snippet to system clipboard!"),
		lipgloss.NewStyle().Foreground(th.Muted).Render("Press [ Esc ] or [ Enter ] to close"),
	)

	padY := 1
	if height < 24 {
		padY = 0
	}

	modalBox := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(th.Primary).
		Padding(padY, 2).
		Width(boxWidth).
		Render(msg)

	return PlaceOverlay(modalBox, width, height)
}

func RenderThemePickerModal(currentTheme string, width int, height int, th theme.Theme) string {
	boxWidth := 46
	if width < 50 {
		boxWidth = width - 4
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary).
		Align(lipgloss.Center)

	themeNames := []string{"tokyonight", "catppuccin", "synthwave", "nord", "gruvbox", "dracula"}
	var rows []string

	for i, name := range themeNames {
		t := theme.GetTheme(name)
		isSelected := (name == currentTheme)

		cursor := "  "
		if isSelected {
			cursor = "❯ "
		}

		colorSample := lipgloss.NewStyle().
			Background(t.Primary).
			Foreground(t.BadgeText).
			Bold(true).
			Render(" " + t.Name + " ")

		row := fmt.Sprintf("[%d] %s%s", i+1, cursor, colorSample)
		if isSelected {
			row = lipgloss.NewStyle().Bold(true).Render(row)
		}
		rows = append(rows, row)
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render("🎨 SELECT COLOR THEME"),
		"",
		strings.Join(rows, "\n"),
		"",
		lipgloss.NewStyle().Foreground(th.Muted).Render("Press [ 1-6 ] or [ j/k ] and [ Enter ] to apply"),
		lipgloss.NewStyle().Foreground(th.Muted).Render("Press [ Esc ] to close"),
	)

	modalBox := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(th.Primary).
		Padding(1, 2).
		Width(boxWidth).
		Render(content)

	return PlaceOverlay(modalBox, width, height)
}

func RenderAddStationModal(
	inputs []string,
	focusIdx int,
	errMsg string,
	width int,
	height int,
	th theme.Theme,
) string {
	boxWidth := 58
	if width < 62 {
		boxWidth = width - 4
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary).
		Align(lipgloss.Center)

	fieldWidth := boxWidth - 16
	if fieldWidth < 15 {
		fieldWidth = 15
	}

	shortLabels := []string{
		"Name *:",
		"URL *:",
		"Genre:",
		"Country:",
		"Bitrate:",
	}

	var rows []string
	for i := range shortLabels {
		val := ""
		if i < len(inputs) {
			val = inputs[i]
		}

		lblStyle := lipgloss.NewStyle().Foreground(th.Secondary)
		if i == focusIdx {
			lblStyle = lblStyle.Bold(true).Foreground(th.Primary)
		}

		cursor := " "
		if i == focusIdx {
			cursor = "█"
		}

		fieldText := padRight(truncate(val, fieldWidth-2)+cursor, fieldWidth)
		fieldBox := lipgloss.NewStyle().
			Background(th.Border).
			Foreground(th.Foreground).
			Padding(0, 1)
		if i == focusIdx {
			fieldBox = fieldBox.Background(th.Primary).Foreground(th.BadgeText).Bold(true)
		}

		row := lipgloss.JoinHorizontal(
			lipgloss.Center,
			lblStyle.Render(padRight(shortLabels[i], 10)),
			" ",
			fieldBox.Render(fieldText),
		)
		rows = append(rows, row)
	}

	errView := ""
	if errMsg != "" {
		errView = lipgloss.NewStyle().Foreground(th.Favorite).Bold(true).Render("⚠️ " + errMsg)
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render("📻 ADD / EDIT CUSTOM RADIO STATION"),
		"",
		strings.Join(rows, "\n"),
		errView,
		"",
		lipgloss.NewStyle().Foreground(th.Playing).Render("[ Tab / j / k ] Next field   [ Enter ] Save   [ Esc ] Cancel"),
	)

	padY := 1
	if height < 24 {
		padY = 0
	}

	modalBox := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(th.Primary).
		Padding(padY, 2).
		Width(boxWidth).
		Render(content)

	return PlaceOverlay(modalBox, width, height)
}
