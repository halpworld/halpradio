package components

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/theme"
)

func RenderStatusBar(searchQuery string, message string, activeTab int, width int, th theme.Theme, activeTuner ...bool) string {
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

	isTuner := len(activeTuner) > 0 && activeTuner[0]

	var items []struct {
		key  string
		desc string
	}

	if activeTab == 8 && isTuner {
		// Dedicated Tuner status bar legend
		if width >= 95 {
			items = []struct {
				key  string
				desc string
			}{
				{"h/l", "Sweep"},
				{"H/L", "Fast"},
				{"b", "Band"},
				{"n/N", "Seek"},
				{"Space", "Mute"},
				{"9/M", "Globe"},
				{"+/-", "Vol"},
				{"?", "Help"},
				{"q", "Quit"},
			}
		} else if width >= 65 {
			items = []struct {
				key  string
				desc string
			}{
				{"h/l", "Sweep"},
				{"b", "Band"},
				{"n/N", "Seek"},
				{"Space", "Mute"},
				{"9", "Globe"},
				{"+/-", "Vol"},
				{"?", "Help"},
				{"q", "Quit"},
			}
		} else {
			items = []struct {
				key  string
				desc string
			}{
				{"h/l", "Sweep"},
				{"b", "Band"},
				{"Space", "Mute"},
				{"9", "Globe"},
				{"q", "Quit"},
			}
		}
	} else if activeTab == 8 {
		// Dedicated Globe status bar legend
		if width >= 95 {
			items = []struct {
				key  string
				desc string
			}{
				{"h/j/k/l", "Spin/Tilt"},
				{"+/-", "Zoom"},
				{"n/p", "Cluster"},
				{"Enter", "Play"},
				{"?", "Help"},
				{"q", "Quit"},
			}
		} else if width >= 65 {
			items = []struct {
				key  string
				desc string
			}{
				{"h/j/k/l", "Spin"},
				{"+/-", "Zoom"},
				{"Enter", "Play"},
				{"?", "Help"},
				{"q", "Quit"},
			}
		} else {
			items = []struct {
				key  string
				desc string
			}{
				{"h/j/k/l", "Spin"},
				{"Enter", "Play"},
				{"q", "Quit"},
			}
		}
	} else if activeTab == 7 {
		// History & Song Discovery Hub legend
		if width >= 95 {
			items = []struct {
				key  string
				desc string
			}{
				{"j/k", "Nav"},
				{"y", "Yank (Copy)"},
				{"o", "Open Search"},
				{"s", "Bookmark"},
				{"z", "Timer"},
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
				{"z", "Timer"},
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
				{"z", "Timer"},
				{"q", "Quit"},
			}
		}
	} else {
		// Standard tabs legend
		if width >= 95 {
			items = []struct {
				key  string
				desc string
			}{
				{"j/k", "Nav"},
				{"Space", "Play/Pause"},
				{"I", "Identify"},
				{"z", "Timer/Pomo"},
				{"f", "Fav"},
				{"y", "Yank"},
				{"+/-", "Vol"},
				{"/", "Search"},
				{"a", "Add"},
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
				{"z", "Timer"},
				{"f", "Fav"},
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
