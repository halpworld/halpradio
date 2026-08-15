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
		emptyWidth := width - 4
		if emptyWidth < 20 {
			emptyWidth = 20
		}
		emptyStyle := lipgloss.NewStyle().
			Foreground(th.Muted).
			Padding(1, 2).
			Width(emptyWidth).
			Italic(true)
		return emptyStyle.Render("No stations found. Press [ / ] to search or [ a ] to add a new custom station.")
	}

	showBitrateAndCodec := (width >= 55)
	showCountry := (width >= 40)

	countryWidth := 5
	bitrateWidth := 7
	codecWidth := 5
	favWidth := 3

	fixedWidth := 5 + 2 + favWidth // prefix + fav sep + fav
	if showCountry {
		fixedWidth += 2 + countryWidth
	}
	if showBitrateAndCodec {
		fixedWidth += 2 + bitrateWidth + 2 + codecWidth
	}
	fixedWidth += 2 // sep between name and genre

	avail := width - fixedWidth
	if avail < 16 {
		avail = 16
	}

	nameWidth := int(float64(avail) * 0.58)
	if nameWidth < 8 {
		nameWidth = 8
	}
	genreWidth := avail - nameWidth
	if genreWidth < 6 {
		genreWidth = 6
	}

	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary).
		BorderBottom(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(th.Border)

	var headerCols []string
	headerCols = append(headerCols, padRight("STATION NAME", nameWidth))
	headerCols = append(headerCols, padRight("GENRE", genreWidth))
	if showCountry {
		headerCols = append(headerCols, padRight("FLAG", countryWidth))
	}
	if showBitrateAndCodec {
		headerCols = append(headerCols, padRight("BITRATE", bitrateWidth))
		headerCols = append(headerCols, padRight("CODEC", codecWidth))
	}
	headerCols = append(headerCols, padRight("FAV", favWidth))
	headerLine := "     " + strings.Join(headerCols, "  ")
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

		var cols []string
		cols = append(cols, padRight(name, nameWidth))
		cols = append(cols, padRight(genre, genreWidth))
		if showCountry {
			cols = append(cols, padRight(flag, countryWidth))
		}
		if showBitrateAndCodec {
			cols = append(cols, padRight(bitrate, bitrateWidth))
			cols = append(cols, padRight(codec, codecWidth))
		}
		cols = append(cols, padRight(favIcon, favWidth))

		rowText := fmt.Sprintf("%s%s %s", cursor, playIcon, strings.Join(cols, "  "))

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
