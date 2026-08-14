package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/theme"
)

func RenderHeader(width int, activeTab int, status player.PlayStatus, backend string, th theme.Theme) string {
	halpStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary)

	radioStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.HeaderAscii)

	iconStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Secondary)

	line1 := halpStyle.Render("█  █ █▀█ █   █▀█") + "   " + radioStyle.Render("█▀▄ █▀█ █▀▄ ▀█▀ █▀█") + iconStyle.Render(" 📻")
	line2 := halpStyle.Render("█▀▀█ █▀█ █   █▀▀") + "   " + radioStyle.Render("█▀▄ █▀█ █ █  █  █ █")
	line3 := halpStyle.Render("▀  ▀ ▀ ▀ ▀▀▀ ▀  ") + "   " + radioStyle.Render("▀ ▀ ▀ ▀ ▀▀  ▀▀▀ ▀▀▀")

	renderedLogo := line1 + "\n" + line2 + "\n" + line3

	// Status badge
	var statusStr string
	var statusBg lipgloss.Color
	switch status {
	case player.StatusPlaying:
		statusStr = " ● PLAYING "
		statusBg = th.Playing
	case player.StatusConnecting:
		statusStr = " ⟳ CONNECTING "
		statusBg = th.Primary
	case player.StatusPaused:
		statusStr = " ⏸ PAUSED "
		statusBg = th.Secondary
	case player.StatusError:
		statusStr = " ✖ ERROR "
		statusBg = th.Favorite
	default:
		statusStr = " ⏹ STOPPED "
		statusBg = th.Muted
	}

	badgeStyle := lipgloss.NewStyle().
		Background(statusBg).
		Foreground(th.BadgeText).
		Bold(true).
		Padding(0, 1)

	backendStyle := lipgloss.NewStyle().
		Foreground(th.Muted).
		Italic(true)

	// Tabs
	tabs := []string{
		"1: Catalog",
		"2: Activities",
		"3: Genres",
		"4: Favorites",
		"5: RadioBrowser",
		"6: Custom",
	}

	var tabViews []string
	for i, t := range tabs {
		if i == activeTab {
			style := lipgloss.NewStyle().
				Background(th.Primary).
				Foreground(th.BadgeText).
				Bold(true).
				Padding(0, 1)
			tabViews = append(tabViews, style.Render(t))
		} else {
			style := lipgloss.NewStyle().
				Foreground(th.Foreground).
				Padding(0, 1)
			tabViews = append(tabViews, style.Render(t))
		}
	}

	tabBar := strings.Join(tabViews, " ")

	topRight := lipgloss.JoinHorizontal(lipgloss.Center,
		badgeStyle.Render(statusStr),
		" ",
		backendStyle.Render(fmt.Sprintf("[%s]", backend)),
	)

	headerBox := lipgloss.JoinHorizontal(
		lipgloss.Center,
		renderedLogo,
		strings.Repeat(" ", clamp(width-65, 2)),
		topRight,
	)

	divider := lipgloss.NewStyle().
		Foreground(th.Border).
		Render(strings.Repeat("─", width))

	return lipgloss.JoinVertical(
		lipgloss.Left,
		headerBox,
		"",
		tabBar,
		divider,
	)
}
