package components

import (
	"fmt"
	"math"
	"math/rand"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/theme"
)

type Visualizer struct {
	Mode     string // "bars", "wave", "spectrum", "minimal", "off"
	frame    int
	heights  []float64
	peaks    []float64
	peakHold []int

	vuLeft  float64
	vuRight float64
	vuPeakL float64
	vuPeakR float64
}

func NewVisualizer(mode string) *Visualizer {
	if mode == "" {
		mode = "bars"
	}
	if mode == "waveform" {
		mode = "wave"
	}
	return &Visualizer{
		Mode:  mode,
		frame: 0,
	}
}

func (v *Visualizer) Tick() {
	v.frame++

	// Physics updates for VU meters
	f := float64(v.frame)

	// Simulate stereo VU dynamics with harmonic oscillation and beat transients
	targetL := 0.5 + 0.35*math.Sin(0.18*f) + 0.15*math.Sin(0.42*f+1.0)
	targetR := 0.5 + 0.35*math.Sin(0.18*f+0.4) + 0.15*math.Cos(0.38*f)

	beat := math.Pow(math.Sin(0.25*f), 6.0) * 0.3
	targetL = clampFloat(targetL+beat, 0.05, 1.0)
	targetR = clampFloat(targetR+beat, 0.05, 1.0)

	// Attack & decay physics
	v.vuLeft += 0.6 * (targetL - v.vuLeft)
	v.vuRight += 0.6 * (targetR - v.vuRight)

	if v.vuLeft > v.vuPeakL {
		v.vuPeakL = v.vuLeft
	} else {
		v.vuPeakL = math.Max(0.0, v.vuPeakL-0.05)
	}

	if v.vuRight > v.vuPeakR {
		v.vuPeakR = v.vuRight
	} else {
		v.vuPeakR = math.Max(0.0, v.vuPeakR-0.05)
	}
}

func (v *Visualizer) CycleMode() string {
	modes := []string{"bars", "wave", "spectrum", "minimal", "off"}
	current := v.Mode
	if current == "waveform" {
		current = "wave"
	}
	for i, m := range modes {
		if m == current {
			v.Mode = modes[(i+1)%len(modes)]
			return v.Mode
		}
	}
	v.Mode = "bars"
	return v.Mode
}

func (v *Visualizer) Render(isPlaying bool, width int, th theme.Theme) string {
	if v.Mode == "off" || width < 10 {
		return ""
	}

	if !isPlaying {
		mutedStyle := lipgloss.NewStyle().Foreground(th.Muted)
		return mutedStyle.Render("⏹ [ AUDIO STOPPED ] " + strings.Repeat("─", clamp(width-20, 4)))
	}

	mode := v.Mode
	if mode == "waveform" {
		mode = "wave"
	}

	switch mode {
	case "wave":
		return v.renderWave(width, th)
	case "spectrum":
		return v.renderSpectrum(width, th)
	case "minimal":
		return v.renderMinimal(width, th)
	default: // "bars"
		return v.renderBars(width, th)
	}
}

func (v *Visualizer) ensureBins(numBins int) {
	if numBins < 1 {
		numBins = 1
	}
	if len(v.heights) != numBins {
		v.heights = make([]float64, numBins)
		v.peaks = make([]float64, numBins)
		v.peakHold = make([]int, numBins)
	}
}

func (v *Visualizer) updateBarPhysics(numBins int) {
	v.ensureBins(numBins)
	f := float64(v.frame)

	rawTargets := make([]float64, numBins)
	for i := 0; i < numBins; i++ {
		x := float64(i) / float64(numBins)

		bass := 0.5 + 0.4*math.Sin(0.18*f+0.8*x)
		beat := math.Pow(math.Sin(0.25*f), 6.0) * math.Max(0, 0.4*(1.0-2.5*x))
		mid := 0.4*math.Sin(0.35*f+3.0*x) + 0.25*math.Cos(0.22*f-5.0*x)
		high := 0.3*math.Sin(0.6*f+8.0*x) + 0.2*(pseudoRand(v.frame, i)-0.5)

		envelope := 0.95 - 0.35*x
		raw := (bass + beat + mid + high) * envelope
		rawTargets[i] = clampFloat(raw, 0.05, 1.0)
	}

	// Spatial smoothing across adjacent bins
	for i := 0; i < numBins; i++ {
		left := rawTargets[i]
		if i > 0 {
			left = rawTargets[i-1]
		}
		right := rawTargets[i]
		if i < numBins-1 {
			right = rawTargets[i+1]
		}
		target := 0.25*left + 0.5*rawTargets[i] + 0.25*right

		// Attack / decay physics
		if target > v.heights[i] {
			v.heights[i] += 0.7 * (target - v.heights[i])
		} else {
			v.heights[i] -= 0.18 * (v.heights[i] - target)
		}
		v.heights[i] = clampFloat(v.heights[i], 0.02, 1.0)

		// Peak hold dynamics
		if v.heights[i] >= v.peaks[i] {
			v.peaks[i] = v.heights[i]
			v.peakHold[i] = 3
		} else {
			if v.peakHold[i] > 0 {
				v.peakHold[i]--
			} else {
				v.peaks[i] = math.Max(0.0, v.peaks[i]-0.12)
			}
		}
	}
}

func (v *Visualizer) renderBars(width int, th theme.Theme) string {
	prefix := "♫ "
	suffix := " ♬"
	numBars := clamp(width-lipgloss.Width(prefix)-lipgloss.Width(suffix), 8)

	v.updateBarPhysics(numBars)

	barChars := []rune{' ', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	bassStyle := lipgloss.NewStyle().Foreground(th.Primary)
	midStyle := lipgloss.NewStyle().Foreground(th.Secondary)
	trebStyle := lipgloss.NewStyle().Foreground(th.Highlight)
	peakStyle := lipgloss.NewStyle().Foreground(th.Playing).Bold(true)

	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Foreground(th.Primary).Render(prefix))

	for i := 0; i < numBars; i++ {
		h := v.heights[i]
		idx := int(math.Floor(h * float64(len(barChars)-1)))
		idx = clampRange(idx, 0, len(barChars)-1)

		ch := barChars[idx]
		x := float64(i) / float64(numBars)

		var st lipgloss.Style
		if v.peaks[i] > 0.85 && h > 0.75 {
			st = peakStyle
		} else if x < 0.33 {
			st = bassStyle
		} else if x < 0.66 {
			st = midStyle
		} else {
			st = trebStyle
		}

		sb.WriteString(st.Render(string(ch)))
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(th.Secondary).Render(suffix))
	return sb.String()
}

func (v *Visualizer) renderWave(width int, th theme.Theme) string {
	prefix := "∿ "
	suffix := " ∿"
	numChars := clamp(width-lipgloss.Width(prefix)-lipgloss.Width(suffix), 10)

	f := float64(v.frame)
	amplitude := 0.5 + 0.4*math.Sin(0.08*f)

	waveGlyphs := []string{"_", "⎽", "⎼", "─", "⎻", "⎺", "▔"}

	troughStyle := lipgloss.NewStyle().Foreground(th.Secondary)
	zeroStyle := lipgloss.NewStyle().Foreground(th.Muted)
	crestStyle := lipgloss.NewStyle().Foreground(th.Playing).Bold(true)
	peakStyle := lipgloss.NewStyle().Foreground(th.Highlight).Bold(true)

	var sb strings.Builder
	sb.WriteString(lipgloss.NewStyle().Foreground(th.Primary).Render(prefix))

	for i := 0; i < numChars; i++ {
		y := amplitude * (0.6*math.Sin(0.18*f+0.35*float64(i)) +
			0.3*math.Sin(0.32*f-0.55*float64(i)) +
			0.15*math.Cos(0.08*f+1.1*float64(i)))

		normY := (y + 1.0) / 2.0
		idx := int(math.Floor(normY * float64(len(waveGlyphs))))
		idx = clampRange(idx, 0, len(waveGlyphs)-1)

		glyph := waveGlyphs[idx]
		var st lipgloss.Style
		if idx == 3 {
			st = zeroStyle
		} else if idx > 4 {
			if idx == 6 {
				st = peakStyle
			} else {
				st = crestStyle
			}
		} else {
			st = troughStyle
		}
		sb.WriteString(st.Render(glyph))
	}

	sb.WriteString(lipgloss.NewStyle().Foreground(th.Primary).Render(suffix))
	return sb.String()
}

func (v *Visualizer) renderSpectrum(width int, th theme.Theme) string {
	f := float64(v.frame)

	bassVal := clampFloat(0.4+0.45*math.Sin(0.2*f)+0.15*math.Sin(0.4*f), 0.1, 1.0)
	midVal := clampFloat(0.4+0.4*math.Sin(0.35*f+1.0)+0.2*math.Cos(0.15*f), 0.1, 1.0)
	trebVal := clampFloat(0.35+0.35*math.Sin(0.5*f+2.0)+0.25*(pseudoRand(v.frame, 99)-0.5), 0.1, 1.0)

	bars := []string{" ", "▂", "▃", "▄", "▅", "▆", "▇", "█"}
	getBar := func(val float64) string {
		idx := clampRange(int(val*float64(len(bars)-1)), 0, len(bars)-1)
		return bars[idx]
	}

	bassStr := lipgloss.NewStyle().Foreground(th.Primary).Render("BASS ") +
		lipgloss.NewStyle().Foreground(th.Playing).Render(strings.Repeat(getBar(bassVal), 3))
	midStr := lipgloss.NewStyle().Foreground(th.Secondary).Render(" MID ") +
		lipgloss.NewStyle().Foreground(th.Highlight).Render(strings.Repeat(getBar(midVal), 3))
	trebStr := lipgloss.NewStyle().Foreground(th.Favorite).Render(" TREB ") +
		lipgloss.NewStyle().Foreground(th.Primary).Render(strings.Repeat(getBar(trebVal), 3))

	fullStr := "🔊 " + bassStr + " " + midStr + " " + trebStr
	if lipgloss.Width(fullStr) > width {
		return lipgloss.NewStyle().Foreground(th.Playing).Render(
			fmt.Sprintf("🔊 B:%s M:%s T:%s", getBar(bassVal), getBar(midVal), getBar(trebVal)),
		)
	}
	return fullStr
}

func (v *Visualizer) renderMinimal(width int, th theme.Theme) string {
	meterWidth := clamp(int((width-12)/2), 4)

	filledL := clampRange(int(v.vuLeft*float64(meterWidth)), 0, meterWidth)
	emptyL := meterWidth - filledL
	barL := strings.Repeat("█", filledL) + strings.Repeat("░", emptyL)

	filledR := clampRange(int(v.vuRight*float64(meterWidth)), 0, meterWidth)
	emptyR := meterWidth - filledR
	barR := strings.Repeat("█", filledR) + strings.Repeat("░", emptyR)

	styleL := lipgloss.NewStyle().Foreground(th.Playing)
	styleR := lipgloss.NewStyle().Foreground(th.Highlight)
	labelStyle := lipgloss.NewStyle().Foreground(th.Muted).Bold(true)

	return labelStyle.Render("L:") + styleL.Render(barL) + labelStyle.Render(" R:") + styleR.Render(barR)
}

func clamp(val, min int) int {
	if val < min {
		return min
	}
	return val
}

func clampFloat(val, min, max float64) float64 {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

func clampRange(val, min, max int) int {
	if val < min {
		return min
	}
	if val > max {
		return max
	}
	return val
}

func pseudoRand(frame, seed int) float64 {
	r := rand.New(rand.NewSource(int64(frame*1000 + seed*42)))
	return r.Float64()
}
