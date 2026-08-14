package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/theme"
)

func RenderStatusBar(searchQuery string, message string, width int, th theme.Theme) string {
	if searchQuery != "" {
		searchStyle := lipgloss.NewStyle().
			Background(th.Highlight).
			Foreground(th.BadgeText).
			Bold(true).
			Padding(0, 1)
		return searchStyle.Render("🔍 SEARCH: " + searchQuery + "█ (Press Esc to exit search)")
	}

	if message != "" {
		msgStyle := lipgloss.NewStyle().
			Background(th.Secondary).
			Foreground(th.BadgeText).
			Bold(true).
			Padding(0, 1)
		return msgStyle.Render("⚡ " + message)
	}

	keyBadgeStyle := lipgloss.NewStyle().
		Foreground(th.Primary).
		Bold(true)

	descStyle := lipgloss.NewStyle().
		Foreground(th.Muted)

	items := []struct {
		key  string
		desc string
	}{
		{"j/k", "Nav"},
		{"Space", "Play/Pause"},
		{"f", "Fav"},
		{"+/-", "Vol"},
		{"/", "Search"},
		{"a", "Add"},
		{"p", "Export PR"},
		{"t", "Theme"},
		{"?", "WhichKey"},
		{"q", "Quit"},
	}

	var pills []string
	for _, item := range items {
		pill := keyBadgeStyle.Render("["+item.key+"]") + " " + descStyle.Render(item.desc)
		pills = append(pills, pill)
	}

	legend := strings.Join(pills, "  ")
	barStyle := lipgloss.NewStyle().
		Background(th.Background).
		Foreground(th.Foreground).
		Width(width)

	return barStyle.Render(legend)
}
