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
	width int,
	height int,
	isFocused bool,
	th theme.Theme,
) string {
	boxWidth := width - 2 // Account for 2 border columns
	if boxWidth < 12 {
		boxWidth = 12
	}
	contentWidth := boxWidth - 2 // Account for 2 padding columns (Padding 0, 1)
	if contentWidth < 8 {
		contentWidth = 8
	}

	innerHeight := height - 2
	if innerHeight < 1 {
		innerHeight = 1
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.Border).
		Padding(0, 1).
		Width(boxWidth).
		Height(innerHeight)

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(th.Border)

	if title == "" {
		title = " GENRES / TAGS "
	}
	renderedTitle := headerStyle.Render(padRight(truncate(title, contentWidth), contentWidth))

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

	allLabel := padRight(truncate(" • All Stations", contentWidth), contentWidth)
	rows = append(rows, allItemStyle.Render(allLabel))

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

		maxItemLen := contentWidth - 4
		if maxItemLen < 4 {
			maxItemLen = 4
		}
		itemLabel := padRight(fmt.Sprintf(" • %s", truncate(item, maxItemLen)), contentWidth)
		rows = append(rows, style.Render(itemLabel))
	}

	maxVisible := innerHeight - 2
	if maxVisible < 1 {
		maxVisible = 1
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
