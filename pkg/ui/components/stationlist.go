package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
)

func RenderStationList(
	stations []radio.Station,
	selectedIndex int,
	playingID string,
	width int,
	height int,
	isFocused bool,
	th theme.Theme,
) string {
	if len(stations) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(th.Muted).
			Padding(2, 4).
			Italic(true)
		return emptyStyle.Render("No stations found. Press [ / ] to search or [ a ] to add a new custom station.")
	}

	contentWidth := width - 4
	if contentWidth < 40 {
		contentWidth = 40
	}

	nameWidth := clamp(int(float64(contentWidth)*0.36), 15)
	genreWidth := clamp(int(float64(contentWidth)*0.28), 12)
	countryWidth := 8
	bitrateWidth := 8
	codecWidth := 6
	favWidth := 4

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(th.Border)

	colName := padRight("STATION NAME", nameWidth)
	colGenre := padRight("GENRE", genreWidth)
	colCountry := padRight("FLAG", countryWidth)
	colBitrate := padRight("BITRATE", bitrateWidth)
	colCodec := padRight("CODEC", codecWidth)
	colFav := padRight("FAV", favWidth)

	headerLine := fmt.Sprintf("   %s  %s  %s  %s  %s  %s", colName, colGenre, colCountry, colBitrate, colCodec, colFav)
	renderedHeader := headerStyle.Render(headerLine)

	maxVisibleRows := height - 3
	if maxVisibleRows < 3 {
		maxVisibleRows = 3
	}

	startIdx := 0
	if selectedIndex >= maxVisibleRows {
		startIdx = selectedIndex - maxVisibleRows + 1
	}
	endIdx := startIdx + maxVisibleRows
	if endIdx > len(stations) {
		endIdx = len(stations)
	}

	var rows []string

	for i := startIdx; i < endIdx; i++ {
		st := stations[i]
		isSelected := (i == selectedIndex)
		isPlaying := (st.ID == playingID)

		cursor := "  "
		if isSelected {
			cursor = "❯ "
		}

		playIcon := "  "
		if isPlaying {
			playIcon = "▶ "
		}

		favIcon := "  "
		if st.IsFavorite {
			favIcon = "★ "
		}

		name := truncate(st.Name, nameWidth)
		genre := truncate(st.Genre, genreWidth)
		flag := st.CountryFlag()
		bitrate := "-"
		if st.Bitrate > 0 {
			bitrate = fmt.Sprintf("%dk", st.Bitrate)
		}
		codec := st.Codec
		if codec == "" {
			codec = "MP3"
		}

		rowText := fmt.Sprintf("%s%s %s  %s  %s  %s  %s  %s",
			cursor,
			playIcon,
			padRight(name, nameWidth),
			padRight(genre, genreWidth),
			padRight(flag, countryWidth),
			padRight(bitrate, bitrateWidth),
			padRight(codec, codecWidth),
			padRight(favIcon, favWidth),
		)

		var lineStyle lipgloss.Style

		if isSelected {
			if isFocused {
				lineStyle = lipgloss.NewStyle().
					Background(th.Primary).
					Foreground(th.BadgeText).
					Bold(true)
			} else {
				lineStyle = lipgloss.NewStyle().
					Background(th.Border).
					Foreground(th.Foreground)
			}
		} else if isPlaying {
			lineStyle = lipgloss.NewStyle().
				Foreground(th.Playing).
				Bold(true)
		} else {
			lineStyle = lipgloss.NewStyle().
				Foreground(th.Foreground)
		}

		rows = append(rows, lineStyle.Render(rowText))
	}

	scrollInfo := fmt.Sprintf("Showing %d-%d of %d stations", startIdx+1, endIdx, len(stations))
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

func padRight(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		runes := []rune(s)
		for len(runes) > 0 && lipgloss.Width(string(runes)) > width {
			runes = runes[:len(runes)-1]
		}
		return string(runes)
	}
	return s + strings.Repeat(" ", width-w)
}

func truncate(s string, max int) string {
	w := lipgloss.Width(s)
	if w <= max {
		return s
	}
	if max <= 3 {
		runes := []rune(s)
		for len(runes) > 0 && lipgloss.Width(string(runes)) > max {
			runes = runes[:len(runes)-1]
		}
		return string(runes)
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)+"...") > max {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "..."
}
