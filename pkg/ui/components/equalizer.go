package components

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/player/dsp"
	"github.com/halpworld/halpradio/pkg/theme"
)

// eqSliderLevels are the dB rows drawn for each band slider, top to bottom.
var eqSliderLevels = []float64{12, 9, 6, 3, 0, -3, -6, -9, -12}

// EqualizerModalInput is everything the graphic equalizer & DSP rack modal
// renders. It is plain data so the view stays a pure function.
type EqualizerModalInput struct {
	Settings     dsp.Settings
	SelectedBand int
	Backend      string
	Support      player.DSPSupport

	// Live normalizer meter, when the backend reports one.
	MeterOK     bool
	MeterLUFS   float64
	MeterGainDB float64

	Width  int
	Height int
}

// RenderEqualizerModal draws the 10-band graphic equalizer, presets and the
// normalizer / crossfeed / lo-fi toggles as a centred overlay.
func RenderEqualizerModal(in EqualizerModalInput, th theme.Theme) string {
	width, height := in.Width, in.Height
	s := in.Settings.Normalize()

	boxWidth := 80
	if boxWidth > width-2 {
		boxWidth = width - 2
	}
	if boxWidth < 50 {
		boxWidth = 50
	}
	inner := boxWidth - 4 // Width() includes the 2-column horizontal padding

	const labelW = 6
	colW := (inner - labelW) / dsp.NumBands
	if colW > 7 {
		colW = 7
	}
	if colW < 4 {
		colW = 4
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Primary)
	mutedStyle := lipgloss.NewStyle().Foreground(th.Muted)
	textStyle := lipgloss.NewStyle().Foreground(th.Foreground)
	barStyle := lipgloss.NewStyle().Foreground(th.Playing)
	selBarStyle := lipgloss.NewStyle().Foreground(th.Highlight).Bold(true)
	selLabelStyle := lipgloss.NewStyle().Background(th.Primary).Foreground(th.BadgeText).Bold(true)

	var rows []string
	rows = append(rows, titleStyle.Render("🎧 GRAPHIC EQUALIZER & DSP RACK"), "")
	rows = append(rows, renderPresetRows(s.Preset, inner, th)...)
	rows = append(rows, "")

	// Slider grid: one row per dB level, one column per band.
	for _, level := range eqSliderLevels {
		levelLabel := fmt.Sprintf("%+.0fdB", level)
		if level == 0 {
			levelLabel = "0dB"
		}
		var b strings.Builder
		b.WriteString(mutedStyle.Render(padLeft(levelLabel, labelW-1) + " "))
		for i, g := range s.Bands {
			cell := "───"
			style := mutedStyle
			if sliderFilled(g, level) {
				cell = "─█─"
				style = barStyle
				if i == in.SelectedBand {
					style = selBarStyle
				}
			} else if i == in.SelectedBand {
				style = lipgloss.NewStyle().Foreground(th.Primary)
			}
			b.WriteString(style.Render(centerText(cell, colW)))
		}
		rows = append(rows, b.String())
	}

	// Band labels and exact gains underneath.
	var labels, values strings.Builder
	labels.WriteString(strings.Repeat(" ", labelW))
	values.WriteString(strings.Repeat(" ", labelW))
	for i, g := range s.Bands {
		label := centerText(dsp.BandLabels[i], colW)
		value := centerText(fmt.Sprintf("%+.0f", g), colW)
		if i == in.SelectedBand {
			labels.WriteString(selLabelStyle.Render(label))
			values.WriteString(selBarStyle.Render(value))
		} else {
			labels.WriteString(textStyle.Render(label))
			values.WriteString(mutedStyle.Render(value))
		}
	}
	rows = append(rows, labels.String(), values.String(), "")

	// Toggles.
	normLabel := fmt.Sprintf("EBU R128 Normalizer (%.0f LUFS)", s.TargetLUFS)
	rows = append(rows, packRow([]string{
		renderToggle(s.Normalizer, "n", normLabel, th),
		renderToggle(s.Crossfeed, "c", "Binaural Crossfeed", th),
		renderToggle(s.LoFi, "t", "Lo-Fi Cassette Tape", th),
	}, "   ", inner)...)
	if s.Normalizer && in.MeterOK {
		rows = append(rows, mutedStyle.Render(fmt.Sprintf("    measuring %.1f LUFS → applying %+.1f dB", in.MeterLUFS, in.MeterGainDB)))
	}
	rows = append(rows, "", renderSupportNote(in.Backend, in.Support, th))

	hintStyle := lipgloss.NewStyle().Foreground(th.Playing).Bold(true)
	hints := []string{"[Tab/h/l] Band", "[j/k] ±1dB", "[J/K] ±3dB", "[0] Zero", "[r] Flat",
		"[p/P] Preset", "[n/c/t] Toggle", "[Esc] Save & Close"}
	for _, line := range packRow(hints, "  ", inner) {
		rows = append(rows, hintStyle.Render(line))
	}

	padY := 1
	if height < 30 {
		padY = 0
	}
	// On short terminals drop spacer rows, top first, until the box fits.
	for avail := height - 2 - 2*padY; len(rows) > avail; {
		i := indexOf(rows, "")
		if i < 0 {
			break
		}
		rows = append(rows[:i], rows[i+1:]...)
	}
	modalBox := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(th.Primary).
		Padding(padY, 2).
		Width(boxWidth).
		Render(strings.Join(rows, "\n"))

	return PlaceOverlay(modalBox, width, height)
}

// sliderFilled reports whether the slider cell at level is lit for a band at
// gain g: the bar grows from the 0 dB line towards the gain, and the 0 dB cell
// is always lit as the resting position.
func sliderFilled(g, level float64) bool {
	if level == 0 {
		return true
	}
	if level > 0 {
		return g > 0 && level <= g+1.5
	}
	return g < 0 && level >= g-1.5
}

// renderPresetRows lists every preset, highlighting the active one, wrapped
// to the modal's inner width.
func renderPresetRows(active string, inner int, th theme.Theme) []string {
	activeStyle := lipgloss.NewStyle().Background(th.Primary).Foreground(th.BadgeText).Bold(true)
	idleStyle := lipgloss.NewStyle().Foreground(th.Muted)

	names := make([]string, 0, len(dsp.Presets)+1)
	for _, p := range dsp.Presets {
		names = append(names, p.Name)
	}
	names = append(names, dsp.PresetCustom)

	parts := []string{lipgloss.NewStyle().Foreground(th.Secondary).Bold(true).Render("Presets:")}
	for _, n := range names {
		if strings.EqualFold(n, active) {
			parts = append(parts, activeStyle.Render("["+n+"]"))
		} else {
			parts = append(parts, idleStyle.Render(n))
		}
	}
	return packRow(parts, "  ", inner)
}

// packRow joins items with sep, starting a new line whenever the next item
// would push the current one past width.
func packRow(items []string, sep string, width int) []string {
	var lines []string
	line := ""
	for _, it := range items {
		switch {
		case line == "":
			line = it
		case lipgloss.Width(line)+lipgloss.Width(sep)+lipgloss.Width(it) <= width:
			line += sep + it
		default:
			lines = append(lines, line)
			line = it
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func indexOf(rows []string, want string) int {
	for i, r := range rows {
		if r == want {
			return i
		}
	}
	return -1
}

func renderToggle(on bool, key, label string, th theme.Theme) string {
	box := "[ ]"
	style := lipgloss.NewStyle().Foreground(th.Muted)
	if on {
		box = "[x]"
		style = lipgloss.NewStyle().Foreground(th.Playing).Bold(true)
	}
	keyStyle := lipgloss.NewStyle().Foreground(th.Highlight).Bold(true)
	return style.Render(box) + " " + keyStyle.Render(key) + " " + style.Render(label)
}

func renderSupportNote(backend string, support player.DSPSupport, th theme.Theme) string {
	style := lipgloss.NewStyle().Foreground(th.Muted).Italic(true)
	switch support {
	case player.DSPLive:
		return style.Render(fmt.Sprintf("💡 Changes are live on the %s backend.", backend))
	case player.DSPNextStation:
		return style.Render(fmt.Sprintf("💡 %s applies changes when the next station starts.", backend))
	default:
		return lipgloss.NewStyle().Foreground(th.Favorite).Render(
			fmt.Sprintf("⚠ The %s backend cannot run the DSP rack — use native, mpv or ffplay.", backend))
	}
}

func centerText(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	left := int(math.Floor(float64(width-w) / 2))
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", width-w-left)
}

func padLeft(s string, width int) string {
	w := lipgloss.Width(s)
	if w >= width {
		return s
	}
	return strings.Repeat(" ", width-w) + s
}
