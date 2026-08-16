package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
)

func RenderPlayerBar(
	currStation *radio.Station,
	currTrack string,
	status player.PlayStatus,
	volume int,
	isMuted bool,
	viz *Visualizer,
	timerBadge string,
	width int,
	th theme.Theme,
) string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.Border).
		Padding(0, 1).
		Width(width - 2)

	timerBadgeStyle := lipgloss.NewStyle().
		Background(th.Highlight).
		Foreground(th.BadgeText).
		Bold(true).
		Padding(0, 1)

	if currStation == nil {
		idleStyle := lipgloss.NewStyle().
			Foreground(th.Muted).
			Italic(true)
		msg := idleStyle.Render("No station selected. Navigate with j/k and press [ Space ] or [ Enter ] to start listening!")
		if timerBadge != "" {
			return boxStyle.Render(lipgloss.JoinHorizontal(lipgloss.Center,
				timerBadgeStyle.Render(timerBadge),
				" ",
				msg,
			))
		}
		return boxStyle.Render(msg)
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary)

	trackStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Foreground)

	genreStyle := lipgloss.NewStyle().
		Foreground(th.Secondary)

	var volText string
	if isMuted {
		volText = lipgloss.NewStyle().Foreground(th.Favorite).Bold(true).Render("[ 🔇 MUTED ]")
	} else {
		barLen := 10
		filled := (volume * barLen) / 100
		empty := barLen - filled
		gauge := strings.Repeat("█", filled) + strings.Repeat("░", empty)
		volText = fmt.Sprintf("🔊 [%s] %d%%", gauge, volume)
	}

	volStyle := lipgloss.NewStyle().
		Foreground(th.Highlight)

	isPlaying := (status == player.PlayStatus(player.StatusPlaying))
	vizWidth := clamp(width*2/5, 20)
	vizRendered := viz.Render(isPlaying, vizWidth, th)

	innerW := width - 6
	if innerW < 20 {
		innerW = 20
	}

	rightParts := []string{}
	if timerBadge != "" {
		rightParts = append(rightParts, timerBadgeStyle.Render(timerBadge))
	}
	rightParts = append(rightParts, volStyle.Render(volText))
	line1Right := strings.Join(rightParts, "  ")

	volWidth := lipgloss.Width(line1Right)
	maxLine1Left := innerW - volWidth - 2
	if maxLine1Left < 10 {
		maxLine1Left = 10
	}

	stationName := truncate(currStation.Name, maxLine1Left-4)
	line1Left := fmt.Sprintf("%s %s", currStation.CountryFlag(), titleStyle.Render(stationName))
	if currStation.Genre != "" && lipgloss.Width(line1Left)+lipgloss.Width(" ("+currStation.Genre+")") <= maxLine1Left {
		line1Left += " " + genreStyle.Render("("+currStation.Genre+")")
	}

	space1 := innerW - lipgloss.Width(line1Left) - volWidth
	if space1 < 1 {
		space1 = 1
	}
	line1 := line1Left + strings.Repeat(" ", space1) + line1Right

	trackDisplay := currTrack
	if trackDisplay == "" {
		trackDisplay = currStation.Name
	}
	vizW := lipgloss.Width(vizRendered)
	maxTrackWidth := innerW - vizW - 4
	if maxTrackWidth < 10 {
		maxTrackWidth = 10
	}
	line2Left := trackStyle.Render("♪ " + truncate(trackDisplay, maxTrackWidth))

	space2 := innerW - lipgloss.Width(line2Left) - vizW
	if space2 < 1 {
		space2 = 1
	}
	line2 := line2Left + strings.Repeat(" ", space2) + vizRendered

	return boxStyle.Render(lipgloss.JoinVertical(lipgloss.Left, line1, line2))
}
