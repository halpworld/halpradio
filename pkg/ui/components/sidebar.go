package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/theme"
)

func RenderSidebar(
	title string,
	items []string,
	selectedItem string,
	selectedIndex int,
	height int,
	isFocused bool,
	th theme.Theme,
) string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.Border).
		Padding(0, 1).
		Width(24).
		Height(height)

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(th.Border)

	if title == "" {
		title = " GENRES / TAGS "
	}
	renderedTitle := headerStyle.Render(title)

	var rows []string
	allSelected := (selectedItem == "" || selectedItem == "all")

	allItemStyle := lipgloss.NewStyle()
	if allSelected && selectedIndex == 0 {
		if isFocused {
			allItemStyle = allItemStyle.Background(th.Primary).Foreground(th.BadgeText).Bold(true)
		} else {
			allItemStyle = allItemStyle.Background(th.Border).Foreground(th.Foreground)
		}
	} else if allSelected {
		allItemStyle = allItemStyle.Foreground(th.Playing).Bold(true)
	} else {
		allItemStyle = allItemStyle.Foreground(th.Foreground)
	}

	rows = append(rows, allItemStyle.Render(" • All Stations"))

	for i, item := range items {
		idx := i + 1
		isSelected := (item == selectedItem)
		isHovered := (selectedIndex == idx)

		var style lipgloss.Style
		if isHovered {
			if isFocused {
				style = lipgloss.NewStyle().Background(th.Primary).Foreground(th.BadgeText).Bold(true)
			} else {
				style = lipgloss.NewStyle().Background(th.Border).Foreground(th.Foreground)
			}
		} else if isSelected {
			style = lipgloss.NewStyle().Foreground(th.Playing).Bold(true)
		} else {
			style = lipgloss.NewStyle().Foreground(th.Foreground)
		}

		rows = append(rows, style.Render(fmt.Sprintf(" • %s", truncate(item, 18))))
	}

	maxVisible := height - 3
	if maxVisible < 3 {
		maxVisible = 3
	}

	startIdx := 0
	if selectedIndex >= maxVisible {
		startIdx = selectedIndex - maxVisible + 1
	}
	endIdx := startIdx + maxVisible
	if endIdx > len(rows) {
		endIdx = len(rows)
	}

	visibleRows := rows[startIdx:endIdx]

	return boxStyle.Render(lipgloss.JoinVertical(lipgloss.Left, renderedTitle, strings.Join(visibleRows, "\n")))
}
