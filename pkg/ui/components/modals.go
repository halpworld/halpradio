package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
	"github.com/halpworld/halpradio/pkg/timer"
)

func RenderPRExportModal(st radio.Station, width int, height int, th theme.Theme) string {
	boxWidth := 66
	if width < 70 {
		boxWidth = width - 4
	}
	if boxWidth < 36 {
		boxWidth = 36
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary).
		Align(lipgloss.Center)

	descStyle := lipgloss.NewStyle().
		Foreground(th.Foreground)

	codeStyle := lipgloss.NewStyle().
		Background(th.Border).
		Foreground(th.Highlight).
		Padding(0, 1)

	instructionStyle := lipgloss.NewStyle().
		Foreground(th.Secondary).
		Italic(true)

	yamlSnippet := st.ToYAMLSnippet()

	msg := lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render("🐙 CONTRIBUTE TO HALPRADIO ON GITHUB"),
		"",
		descStyle.Render(fmt.Sprintf("Station '%s' copied to clipboard!", truncate(st.Name, boxWidth-8))),
		instructionStyle.Render("1. Fork https://github.com/halpworld/halpradio"),
		instructionStyle.Render("2. Paste snippet into 'stations.yaml' & open PR!"),
		"",
		codeStyle.Render(yamlSnippet),
		"",
		lipgloss.NewStyle().Foreground(th.Playing).Bold(true).Render("✓ Copied snippet to system clipboard!"),
		lipgloss.NewStyle().Foreground(th.Muted).Render("Press [ Esc ] or [ Enter ] to close"),
	)

	padY := 1
	if height < 24 {
		padY = 0
	}

	modalBox := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(th.Primary).
		Padding(padY, 2).
		Width(boxWidth).
		Render(msg)

	return PlaceOverlay(modalBox, width, height)
}

func RenderThemePickerModal(currentTheme string, width int, height int, th theme.Theme) string {
	boxWidth := 46
	if width < 50 {
		boxWidth = width - 4
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary).
		Align(lipgloss.Center)

	themeNames := []string{"tokyonight", "catppuccin", "synthwave", "nord", "gruvbox", "dracula"}
	var rows []string

	for i, name := range themeNames {
		t := theme.GetTheme(name)
		isSelected := (name == currentTheme)

		cursor := "  "
		if isSelected {
			cursor = "❯ "
		}

		colorSample := lipgloss.NewStyle().
			Background(t.Primary).
			Foreground(t.BadgeText).
			Bold(true).
			Render(" " + t.Name + " ")

		row := fmt.Sprintf("[%d] %s%s", i+1, cursor, colorSample)
		if isSelected {
			row = lipgloss.NewStyle().Bold(true).Render(row)
		}
		rows = append(rows, row)
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render("🎨 SELECT COLOR THEME"),
		"",
		strings.Join(rows, "\n"),
		"",
		lipgloss.NewStyle().Foreground(th.Muted).Render("Press [ 1-6 ] or [ j/k ] and [ Enter ] to apply"),
		lipgloss.NewStyle().Foreground(th.Muted).Render("Press [ Esc ] to close"),
	)

	modalBox := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(th.Primary).
		Padding(1, 2).
		Width(boxWidth).
		Render(content)

	return PlaceOverlay(modalBox, width, height)
}

func RenderAddStationModal(
	inputs []string,
	focusIdx int,
	errMsg string,
	width int,
	height int,
	th theme.Theme,
) string {
	boxWidth := 58
	if width < 62 {
		boxWidth = width - 4
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary).
		Align(lipgloss.Center)

	fieldWidth := boxWidth - 16
	if fieldWidth < 15 {
		fieldWidth = 15
	}

	shortLabels := []string{
		"Name *:",
		"URL *:",
		"Genre:",
		"Country:",
		"Bitrate:",
	}

	var rows []string
	for i := range shortLabels {
		val := ""
		if i < len(inputs) {
			val = inputs[i]
		}

		lblStyle := lipgloss.NewStyle().Foreground(th.Secondary)
		if i == focusIdx {
			lblStyle = lblStyle.Bold(true).Foreground(th.Primary)
		}

		cursor := " "
		if i == focusIdx {
			cursor = "█"
		}

		fieldText := padRight(truncate(val, fieldWidth-2)+cursor, fieldWidth)
		fieldBox := lipgloss.NewStyle().
			Background(th.Border).
			Foreground(th.Foreground).
			Padding(0, 1)
		if i == focusIdx {
			fieldBox = fieldBox.Background(th.Primary).Foreground(th.BadgeText).Bold(true)
		}

		row := lipgloss.JoinHorizontal(
			lipgloss.Center,
			lblStyle.Render(padRight(shortLabels[i], 10)),
			" ",
			fieldBox.Render(fieldText),
		)
		rows = append(rows, row)
	}

	errView := ""
	if errMsg != "" {
		errView = lipgloss.NewStyle().Foreground(th.Favorite).Bold(true).Render("⚠️ " + errMsg)
	}

	content := lipgloss.JoinVertical(
		lipgloss.Left,
		titleStyle.Render("📻 ADD / EDIT CUSTOM RADIO STATION"),
		"",
		strings.Join(rows, "\n"),
		errView,
		"",
		lipgloss.NewStyle().Foreground(th.Playing).Render("[ Tab / j / k ] Next field   [ Enter ] Save   [ Esc ] Cancel"),
	)

	padY := 1
	if height < 24 {
		padY = 0
	}

	modalBox := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(th.Primary).
		Padding(padY, 2).
		Width(boxWidth).
		Render(content)

	return PlaceOverlay(modalBox, width, height)
}

// RenderTimerModal renders the timer dashboard, preset selection, custom sleep input, or Pomodoro config.
func RenderTimerModal(
	tm *timer.Timer,
	screen int,
	cursor int,
	configInputs []string,
	configFocusIdx int,
	customSleepInput string,
	notifyDesktop bool,
	notifyBell bool,
	width int,
	height int,
	th theme.Theme,
) string {
	boxWidth := 64
	if width < 68 {
		boxWidth = width - 4
	}
	if boxWidth < 36 {
		boxWidth = 36
	}

	titleStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary).
		Align(lipgloss.Center)

	secStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Secondary)

	var content string

	if screen == 1 {
		// Screen 1: Custom Sleep Timer Duration
		inputWidth := 20
		cursorChar := "█"
		fieldBox := lipgloss.NewStyle().
			Background(th.Primary).
			Foreground(th.BadgeText).
			Bold(true).
			Padding(0, 1).
			Render(padRight(customSleepInput+cursorChar, inputWidth))

		content = lipgloss.JoinVertical(
			lipgloss.Left,
			titleStyle.Render("⏳ CUSTOM SLEEP TIMER"),
			"",
			lipgloss.NewStyle().Foreground(th.Foreground).Render("Enter sleep countdown duration:"),
			"",
			lipgloss.JoinHorizontal(lipgloss.Center,
				secStyle.Render("Duration (minutes): "),
				fieldBox,
			),
			"",
			lipgloss.NewStyle().Foreground(th.Muted).Italic(true).Render("💡 Audio smoothly fades out in the last 10s before stopping."),
			"",
			lipgloss.NewStyle().Foreground(th.Playing).Bold(true).Render("[ Enter ] Start Sleep Timer   [ Esc ] Back"),
		)
	} else if screen == 2 {
		// Screen 2: Pomodoro & System Events Configuration
		labels := []string{
			"1. Focus Min:   ",
			"2. Short Brk Min:",
			"3. Long Brk Min: ",
			"4. Total Cycles: ",
			"5. Focus Station:",
			"6. Break Station:",
			"7. Shell Hook:   ",
		}

		fieldW := boxWidth - 22
		if fieldW < 14 {
			fieldW = 14
		}

		var rows []string
		for i, lbl := range labels {
			val := ""
			if i < len(configInputs) {
				val = configInputs[i]
			}
			isFocused := (i == configFocusIdx)
			lblSt := lipgloss.NewStyle().Foreground(th.Secondary)
			if isFocused {
				lblSt = lblSt.Bold(true).Foreground(th.Primary)
			}

			cursorChar := " "
			if isFocused {
				cursorChar = "█"
			}
			fieldText := padRight(truncate(val, fieldW-2)+cursorChar, fieldW)
			fBox := lipgloss.NewStyle().
				Background(th.Border).
				Foreground(th.Foreground).
				Padding(0, 1)
			if isFocused {
				fBox = fBox.Background(th.Primary).Foreground(th.BadgeText).Bold(true)
			}

			rows = append(rows, lipgloss.JoinHorizontal(lipgloss.Center,
				lblSt.Render(padRight(lbl, 17)),
				" ",
				fBox.Render(fieldText),
			))
		}

		desktopStatus := "OFF"
		if notifyDesktop {
			desktopStatus = "ON ✓"
		}
		bellStatus := "OFF"
		if notifyBell {
			bellStatus = "ON ✓"
		}

		togglesLine := fmt.Sprintf(
			"Events: [ d ] Desktop: %s   [ b ] Bell: %s",
			lipgloss.NewStyle().Foreground(th.Highlight).Bold(true).Render(desktopStatus),
			lipgloss.NewStyle().Foreground(th.Highlight).Bold(true).Render(bellStatus),
		)

		helpersLine := lipgloss.NewStyle().Foreground(th.Muted).Render("[ f ] Bind current as Focus   [ k ] Bind current as Break")

		content = lipgloss.JoinVertical(
			lipgloss.Left,
			titleStyle.Render("⚙️  CONFIGURE POMODORO & SYSTEM EVENTS"),
			"",
			strings.Join(rows, "\n"),
			"",
			togglesLine,
			helpersLine,
			"",
			lipgloss.NewStyle().Foreground(th.Playing).Bold(true).Render("[ Tab / j / k ] Navigate   [ Enter ] Save & Start   [ Esc ] Back"),
		)
	} else if tm != nil && tm.IsActive() {
		// Screen 0 (Active Timer Dashboard)
		modeText := tm.PhaseDescription()
		statusBadge := tm.BadgeText()

		// Large digital clock display
		clockStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(th.Primary).
			Background(th.Border).
			Foreground(th.Highlight).
			Bold(true).
			Padding(0, 4).
			Align(lipgloss.Center)
		clockBox := clockStyle.Render(tm.FormattedTime())

		// Progress bar
		barLen := boxWidth - 18
		if barLen > 30 {
			barLen = 30
		}
		if barLen < 10 {
			barLen = 10
		}
		pct := tm.Progress()
		filled := int(pct * float64(barLen))
		if filled > barLen {
			filled = barLen
		}
		empty := barLen - filled
		if empty < 0 {
			empty = 0
		}
		progBar := lipgloss.NewStyle().Foreground(th.Playing).Render(strings.Repeat("█", filled)) +
			lipgloss.NewStyle().Foreground(th.Border).Render(strings.Repeat("░", empty))
		progressLine := fmt.Sprintf("Progress: [%s] %d%%", progBar, int(pct*100))

		var infoLines []string
		if tm.Type == timer.TimerPomodoro {
			fName := tm.PomodoroCfg.FocusStationName
			if fName == "" {
				fName = "Current Playing Station"
			}
			bName := tm.PomodoroCfg.BreakStationName
			if bName == "" {
				bName = "Current / Pause"
			}
			infoLines = append(infoLines, fmt.Sprintf("Focus Station: %s", lipgloss.NewStyle().Foreground(th.Highlight).Render(truncate(fName, boxWidth-20))))
			infoLines = append(infoLines, fmt.Sprintf("Break Station: %s", lipgloss.NewStyle().Foreground(th.Secondary).Render(truncate(bName, boxWidth-20))))
		} else {
			infoLines = append(infoLines, "Action on 00:00: Stop stream with 10s volume fade-out")
		}

		actionsLine1 := lipgloss.NewStyle().Foreground(th.Highlight).Bold(true).Render("[ Space / p ] Pause/Resume   [ s ] Skip Phase   [ r ] Reset")
		actionsLine2 := lipgloss.NewStyle().Foreground(th.Foreground).Render("[ + / - ] ±5 min   [ c ] Cancel Timer   [ e ] Edit Config   [ Esc ] Close")

		content = lipgloss.JoinVertical(
			lipgloss.Left,
			titleStyle.Render("⏱️  ACTIVE TIMER DASHBOARD"),
			"",
			lipgloss.JoinHorizontal(lipgloss.Center,
				secStyle.Render("Status: "),
				lipgloss.NewStyle().Background(th.Primary).Foreground(th.BadgeText).Bold(true).Padding(0, 1).Render(statusBadge),
				"  ",
				lipgloss.NewStyle().Foreground(th.Foreground).Render(modeText),
			),
			"",
			lipgloss.NewStyle().Width(boxWidth-4).Align(lipgloss.Center).Render(clockBox),
			"",
			lipgloss.NewStyle().Width(boxWidth-4).Align(lipgloss.Center).Render(progressLine),
			"",
			strings.Join(infoLines, "\n"),
			"",
			actionsLine1,
			actionsLine2,
		)
	} else {
		// Screen 0 (Preset Selection Menu)
		options := []struct {
			num  string
			icon string
			text string
		}{
			{"1", "🍅", "Start Pomodoro Mode (25m Focus / 5m Break / #4 cycles)"},
			{"2", "⏳", "Sleep Timer - 15 Minutes (Fade-out)"},
			{"3", "⏳", "Sleep Timer - 30 Minutes (Fade-out)"},
			{"4", "⏳", "Sleep Timer - 45 Minutes (Fade-out)"},
			{"5", "⏳", "Sleep Timer - 60 Minutes (Fade-out)"},
			{"6", "⏳", "Sleep Timer - 90 Minutes (Fade-out)"},
			{"7", "✏️", "Custom Sleep Duration..."},
			{"8", "⚙️", "Configure Pomodoro & System Event Hooks"},
		}

		var menuRows []string
		for i, opt := range options {
			isSelected := (i == cursor)
			curStr := "  "
			if isSelected {
				curStr = "❯ "
			}

			row := fmt.Sprintf("[%s] %s%s %s", opt.num, curStr, opt.icon, opt.text)
			if isSelected {
				row = lipgloss.NewStyle().
					Background(th.Primary).
					Foreground(th.BadgeText).
					Bold(true).
					Padding(0, 1).
					Render(row)
			} else {
				row = lipgloss.NewStyle().
					Foreground(th.Foreground).
					Padding(0, 1).
					Render(row)
			}
			menuRows = append(menuRows, row)
		}

		content = lipgloss.JoinVertical(
			lipgloss.Left,
			titleStyle.Render("⏱️  TIMER & POMODORO FOCUS MODE"),
			"",
			lipgloss.NewStyle().Foreground(th.Secondary).Render("Select a focus sprint timer or sleep countdown:"),
			"",
			strings.Join(menuRows, "\n"),
			"",
			lipgloss.NewStyle().Foreground(th.Playing).Bold(true).Render("[ 1-8 / j/k ] Select   [ Enter / Space ] Start   [ Esc ] Close"),
		)
	}

	padY := 1
	if height < 24 {
		padY = 0
	}

	modalBox := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(th.Primary).
		Padding(padY, 2).
		Width(boxWidth).
		Render(content)

	return PlaceOverlay(modalBox, width, height)
}
