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
	var tabs []string
	padX := 1
	if width >= 112 {
		tabs = []string{
			"1: Activities",
			"2: Catalog",
			"3: Countries",
			"4: Genres",
			"5: Favorites",
			"6: RadioBrowser",
			"7: Custom",
			"8: History",
		}
	} else if width >= 86 {
		tabs = []string{
			"1:Activities",
			"2:Catalog",
			"3:Countries",
			"4:Genres",
			"5:Favs",
			"6:Online",
			"7:Custom",
			"8:History",
		}
		if width < 94 {
			padX = 0
		}
	} else {
		padX = 0
		tabs = []string{
			"1:Act",
			"2:Cat",
			"3:Cntry",
			"4:Gen",
			"5:Fav",
			"6:Web",
			"7:Add",
			"8:Hist",
		}
	}

	var tabViews []string
	for i, t := range tabs {
		if i == activeTab {
			style := lipgloss.NewStyle().
				Background(th.Primary).
				Foreground(th.BadgeText).
				Bold(true).
				Padding(0, padX)
			tabViews = append(tabViews, style.Render(t))
		} else {
			style := lipgloss.NewStyle().
				Foreground(th.Foreground).
				Padding(0, padX)
			tabViews = append(tabViews, style.Render(t))
		}
	}

	tabBar := strings.Join(tabViews, " ")

	topRight := lipgloss.JoinHorizontal(lipgloss.Center,
		badgeStyle.Render(statusStr),
		" ",
		backendStyle.Render(fmt.Sprintf("[%s]", backend)),
	)

	logoWidth := lipgloss.Width(renderedLogo)
	topRightWidth := lipgloss.Width(topRight)
	neededWidth := logoWidth + topRightWidth + 2

	var headerBox string
	if width >= neededWidth {
		spaceCount := width - logoWidth - topRightWidth
		if spaceCount < 1 {
			spaceCount = 1
		}
		headerBox = lipgloss.JoinHorizontal(
			lipgloss.Center,
			renderedLogo,
			strings.Repeat(" ", spaceCount),
			topRight,
		)
	} else {
		compactTitle := halpStyle.Render("HALPRADIO") + iconStyle.Render(" 📻")
		spaceCount := width - lipgloss.Width(compactTitle) - topRightWidth
		if spaceCount < 1 {
			spaceCount = 1
		}
		headerBox = lipgloss.JoinHorizontal(
			lipgloss.Center,
			compactTitle,
			strings.Repeat(" ", spaceCount),
			topRight,
		)
	}

	divider := ""
	if width > 0 {
		divider = lipgloss.NewStyle().
			Foreground(th.Border).
			Render(strings.Repeat("─", width))
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		headerBox,
		"",
		tabBar,
		divider,
	)
}
