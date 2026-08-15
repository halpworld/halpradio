package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
)

// RenderHistoryList renders the track history log & song discovery hub table.
func RenderHistoryList(
	entries []radio.HistoryEntry,
	selectedIndex int,
	width int,
	height int,
	th theme.Theme,
) string {
	if len(entries) == 0 {
		emptyWidth := width - 4
		if emptyWidth < 20 {
			emptyWidth = 20
		}
		emptyStyle := lipgloss.NewStyle().
			Foreground(th.Muted).
			Padding(0, 1).
			Width(emptyWidth).
			Italic(true)
		if height >= 8 && width >= 65 {
			return emptyStyle.Render("No track history yet. Tune into any radio station and live song metadata will appear here!\n\n💡 Shortcuts: [ y ] Yank track info   [ o ] Open search   [ s ] Bookmark   [ c ] Clear")
		}
		return emptyStyle.Render("No track history yet. Play a station to record songs!")
	}

	showStation := (width >= 48)
	timeWidth := 8
	stationWidth := 20
	if width < 70 {
		stationWidth = 14
	}

	fixedWidth := 3 + timeWidth + 2 // cursor + time + sep
	if showStation {
		fixedWidth += stationWidth + 2
	}

	avail := width - fixedWidth
	if avail < 16 {
		avail = 16
	}
	trackWidth := avail

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(th.Border)

	var headerCols []string
	headerCols = append(headerCols, padRight("TIME", timeWidth))
	if showStation {
		headerCols = append(headerCols, padRight("STATION", stationWidth))
	}
	headerCols = append(headerCols, padRight("ARTIST - TRACK TITLE", trackWidth))
	headerLine := "   " + strings.Join(headerCols, "  ")
	renderedHeader := headerStyle.Render(headerLine)

	maxVisibleRows := height - 2
	if maxVisibleRows < 1 {
		maxVisibleRows = 1
	}

	startIdx := 0
	if selectedIndex >= maxVisibleRows {
		startIdx = selectedIndex - maxVisibleRows + 1
	}
	endIdx := startIdx + maxVisibleRows
	if endIdx > len(entries) {
		endIdx = len(entries)
	}

	var rows []string
	for i := startIdx; i < endIdx; i++ {
		entry := entries[i]
		isSelected := (i == selectedIndex)

		cursor := "  "
		if isSelected {
			cursor = "❯ "
		}

		timeStr := entry.PlayedAt.Format("15:04:05")
		trackText := entry.FullDisplay()

		var cols []string
		cols = append(cols, padRight(timeStr, timeWidth))
		if showStation {
			cols = append(cols, padRight(truncate(entry.StationName, stationWidth), stationWidth))
		}
		cols = append(cols, padRight(truncate(trackText, trackWidth), trackWidth))

		rowText := fmt.Sprintf("%s %s", cursor, strings.Join(cols, "  "))

		var lineStyle lipgloss.Style
		if isSelected {
			lineStyle = lipgloss.NewStyle().
				Background(th.Primary).
				Foreground(th.BadgeText).
				Bold(true)
		} else {
			lineStyle = lipgloss.NewStyle().
				Foreground(th.Foreground)
		}

		rows = append(rows, lineStyle.Render(rowText))
	}

	scrollInfo := fmt.Sprintf("Showing %d-%d of %d tracks  •  [y] Yank  [o] Search  [s] Star  [c] Clear", startIdx+1, endIdx, len(entries))
	if width < 75 {
		scrollInfo = fmt.Sprintf("%d-%d of %d tracks | [y] Yank [o] Open [s] Save", startIdx+1, endIdx, len(entries))
	}
	footerStyle := lipgloss.NewStyle().
		Foreground(th.Muted).
		Italic(true).
		Align(lipgloss.Right)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		renderedHeader,
		strings.Join(rows, "\n"),
		footerStyle.Render(scrollInfo),
	)
}
