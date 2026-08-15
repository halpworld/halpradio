package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/theme"
)

func RenderStatusBar(searchQuery string, message string, activeTab int, width int, th theme.Theme) string {
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

	var items []struct {
		key  string
		desc string
	}

	if activeTab == 6 {
		// History & Song Discovery Hub legend
		if width >= 90 {
			items = []struct {
				key  string
				desc string
			}{
				{"j/k", "Nav"},
				{"y", "Yank (Copy)"},
				{"o", "Open Search"},
				{"s", "Bookmark"},
				{"c", "Clear"},
				{"+/-", "Vol"},
				{"?", "WhichKey"},
				{"q", "Quit"},
			}
		} else if width >= 65 {
			items = []struct {
				key  string
				desc string
			}{
				{"j/k", "Nav"},
				{"y", "Yank"},
				{"o", "Open"},
				{"s", "Star"},
				{"c", "Clear"},
				{"?", "Help"},
				{"q", "Quit"},
			}
		} else {
			items = []struct {
				key  string
				desc string
			}{
				{"j/k", "Nav"},
				{"y", "Yank"},
				{"o", "Open"},
				{"s", "Star"},
				{"q", "Quit"},
			}
		}
	} else {
		// Standard tabs legend
		if width >= 90 {
			items = []struct {
				key  string
				desc string
			}{
				{"j/k", "Nav"},
				{"Space", "Play/Pause"},
				{"f", "Fav"},
				{"y", "Yank"},
				{"+/-", "Vol"},
				{"/", "Search"},
				{"a", "Add"},
				{"p", "Export PR"},
				{"?", "WhichKey"},
				{"q", "Quit"},
			}
		} else if width >= 65 {
			items = []struct {
				key  string
				desc string
			}{
				{"j/k", "Nav"},
				{"Space", "Play"},
				{"f", "Fav"},
				{"y", "Yank"},
				{"+/-", "Vol"},
				{"/", "Search"},
				{"?", "Help"},
				{"q", "Quit"},
			}
		} else {
			items = []struct {
				key  string
				desc string
			}{
				{"j/k", "Nav"},
				{"Space", "Play"},
				{"/", "Search"},
				{"?", "Help"},
				{"q", "Quit"},
			}
		}
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
		MaxWidth(width)

	return barStyle.Render(legend)
}
