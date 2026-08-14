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
	width int,
	th theme.Theme,
) string {
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.Border).
		Padding(0, 1).
		Width(width - 2)

	if currStation == nil {
		idleStyle := lipgloss.NewStyle().
			Foreground(th.Muted).
			Italic(true)
		return boxStyle.Render(idleStyle.Render("No station selected. Navigate with j/k and press [ Space ] or [ Enter ] to start listening!"))
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
	vizRendered := viz.Render(isPlaying, clamp(width/3, 15), th)

	line1Left := fmt.Sprintf("%s %s", currStation.CountryFlag(), titleStyle.Render(currStation.Name))
	if currStation.Genre != "" {
		line1Left += " " + genreStyle.Render("("+currStation.Genre+")")
	}
	line1Right := volStyle.Render(volText)

	space1 := width - lipgloss.Width(line1Left) - lipgloss.Width(line1Right) - 6
	if space1 < 1 {
		space1 = 1
	}
	line1 := line1Left + strings.Repeat(" ", space1) + line1Right

	trackDisplay := currTrack
	if trackDisplay == "" {
		trackDisplay = currStation.Name
	}
	line2Left := trackStyle.Render("♪ " + truncate(trackDisplay, clamp(width/2, 20)))

	space2 := width - lipgloss.Width(line2Left) - lipgloss.Width(vizRendered) - 6
	if space2 < 1 {
		space2 = 1
	}
	line2 := line2Left + strings.Repeat(" ", space2) + vizRendered

	return boxStyle.Render(lipgloss.JoinVertical(lipgloss.Left, line1, line2))
}
