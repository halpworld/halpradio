package components

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/theme"
)

var VisualizerModes = []string{
	"dj-cat",
	"dj-dog",
	"dj-bear",
	"dj-frog",
	"dj-bunny",
	"bars",
	"wave",
	"spectrum",
	"minimal",
	"off",
}

type Visualizer struct {
	Mode     string // "dj-cat", "dj-dog", "dj-bear", "dj-frog", "dj-bunny", "bars", "wave", "spectrum", "minimal", "off"
	frame    int
	heights  []float64
	peaks    []float64
	peakHold []int

	vuLeft  float64
	vuRight float64
	vuPeakL float64
	vuPeakR float64
}

func normalizeMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "", "dj", "cat", "dj-cat", "dj_cat", "default":
		return "dj-cat"
	case "dog", "dj-dog", "dj_dog":
		return "dj-dog"
	case "bear", "dj-bear", "dj_bear":
		return "dj-bear"
	case "frog", "dj-frog", "dj_frog":
		return "dj-frog"
	case "bunny", "dj-bunny", "dj_bunny":
		return "dj-bunny"
	case "waveform":
		return "wave"
	case "bars", "wave", "spectrum", "minimal", "off":
		return strings.ToLower(strings.TrimSpace(mode))
	default:
		return "dj-cat"
	}
}

func NewVisualizer(mode string) *Visualizer {
	return &Visualizer{
		Mode:  normalizeMode(mode),
		frame: 0,
	}
}

func (v *Visualizer) Tick() {
	v.frame++

	f := float64(v.frame)

	// Stereo VU meters with smooth dynamics
	targetL := 0.5 + 0.35*math.Sin(0.12*f) + 0.15*math.Sin(0.28*f+1.0)
	targetR := 0.5 + 0.35*math.Sin(0.12*f+0.4) + 0.15*math.Cos(0.24*f)

	beat := math.Pow(math.Sin(0.18*f), 4.0) * 0.25
	targetL = clampFloat(targetL+beat, 0.05, 1.0)
	targetR = clampFloat(targetR+beat, 0.05, 1.0)

	v.vuLeft += 0.5 * (targetL - v.vuLeft)
	v.vuRight += 0.5 * (targetR - v.vuRight)

	if v.vuLeft > v.vuPeakL {
		v.vuPeakL = v.vuLeft
	} else {
		v.vuPeakL = math.Max(0.0, v.vuPeakL-0.03)
	}

	if v.vuRight > v.vuPeakR {
		v.vuPeakR = v.vuRight
	} else {
		v.vuPeakR = math.Max(0.0, v.vuPeakR-0.03)
	}
}

func (v *Visualizer) CycleMode() string {
	current := normalizeMode(v.Mode)
	for i, m := range VisualizerModes {
		if m == current {
			v.Mode = VisualizerModes[(i+1)%len(VisualizerModes)]
			return v.Mode
		}
	}
	v.Mode = VisualizerModes[0]
	return v.Mode
}

func (v *Visualizer) Render(isPlaying bool, width int, th theme.Theme) string {
	mode := normalizeMode(v.Mode)
	if mode == "off" || width < 8 {
		return ""
	}

	if !isPlaying {
		return v.renderStopped(mode, width, th)
	}

	switch mode {
	case "dj-cat", "dj-dog", "dj-bear", "dj-frog", "dj-bunny":
		return v.renderAnimalDJ(mode, width, th)
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

func (v *Visualizer) renderStopped(mode string, width int, th theme.Theme) string {
	var sleepingHead string
	switch mode {
	case "dj-dog":
		sleepingHead = " (∪ - ω - ∪) "
	case "dj-bear":
		sleepingHead = " ʕ - ᴥ - ʔ  "
	case "dj-frog":
		sleepingHead = " ( - ⊖ - ) "
	case "dj-bunny":
		sleepingHead = " ( - ㅅ - ) "
	default: // "dj-cat"
		sleepingHead = "(= - ω - =)"
	}

	hpStyle := lipgloss.NewStyle().Foreground(th.Muted)
	headStyle := lipgloss.NewStyle().Foreground(th.Muted)
	deckStyle := lipgloss.NewStyle().Foreground(th.Muted)
	stopStyle := lipgloss.NewStyle().Foreground(th.Secondary)

	// Full stopped booth (exact 35 chars): 🎧 (= - ω - =)..zzZ [ 💿 ] ⏹ STOPPED
	full := hpStyle.Render("🎧 ") + headStyle.Render(sleepingHead+"..zzZ") + " " + deckStyle.Render("[ 💿 ]") + " " + stopStyle.Render("⏹ STOPPED")
	if lipgloss.Width(full) <= width {
		return full
	}

	// Mid stopped booth (exact 24 chars): 🎧 (= - ω - =)..zzZ [ 💿 ]
	mid := hpStyle.Render("🎧 ") + headStyle.Render(sleepingHead+"..zzZ") + " " + deckStyle.Render("[ 💿 ]")
	if lipgloss.Width(mid) <= width {
		return mid
	}

	// Compact stopped booth (exact 12 chars): 🎧 (= - ω - =)
	compact := hpStyle.Render("🎧 ") + headStyle.Render(sleepingHead)
	if lipgloss.Width(compact) <= width {
		return compact
	}

	return lipgloss.NewStyle().Foreground(th.Muted).Render("⏹ STOPPED")
}

type djPose struct {
	head string // exactly 9 visual columns
	arm  string // exactly 2 visual columns
}

func getAnimalPoses(animalMode string) []djPose {
	switch animalMode {
	case "dj-dog":
		return []djPose{
			{head: " (∪･ω･∪) ", arm: "ﾉ "},
			{head: " (∪･ｪ･∪) ", arm: "/ "},
			{head: " (∪-ω-∪) ", arm: "ﾉ-"},
			{head: " (∪･o･∪) ", arm: "ฅ "},
		}
	case "dj-bear":
		return []djPose{
			{head: " ʕ •ᴥ•ʔ  ", arm: "ﾉ "},
			{head: " ʕ ᵔᴥᵔʔ  ", arm: "/ "},
			{head: " ʕ -ᴥ•ʔ  ", arm: "ﾉ-"},
			{head: " ʕ oᴥoʔ  ", arm: "ฅ "},
		}
	case "dj-frog":
		return []djPose{
			{head: " ( •⊖• ) ", arm: "ﾉ "},
			{head: " ( ˘⊖˘ ) ", arm: "/ "},
			{head: " ( -⊖- ) ", arm: "ﾉ-"},
			{head: " ( o⊖o ) ", arm: "ฅ "},
		}
	case "dj-bunny":
		return []djPose{
			{head: "( •ㅅ• ) ", arm: "ﾉ "},
			{head: "( ˘ㅅ˘ ) ", arm: "/ "},
			{head: "( -ㅅ• ) ", arm: "ﾉ-"},
			{head: "( oㅅo ) ", arm: "ฅ "},
		}
	default: // "dj-cat"
		return []djPose{
			{head: "(=^･ω･^=)", arm: "ﾉ "},
			{head: "(=^･ｪ･^=)", arm: "/ "},
			{head: "(=^･ω-^=)", arm: "ﾉ-"},
			{head: "(=^･o･^=)", arm: "ฅ "},
		}
	}
}

func (v *Visualizer) renderAnimalDJ(animalMode string, width int, th theme.Theme) string {
	f := float64(v.frame)

	// Smooth pose cycle: updates every 2 ticks (~300ms) for steady rhythmic head-bobbing
	poses := getAnimalPoses(animalMode)
	poseIdx := (v.frame / 2) % len(poses)
	pose := poses[poseIdx]

	// Turntable vinyl spinning disc (rotates smoothly every tick)
	spinGlyphs := []string{"◓", "◑", "◒", "◐"}
	spin := spinGlyphs[v.frame%len(spinGlyphs)]

	// Floating musical notes (updates calmly every 4 ticks)
	notes := []string{"♪ ", "♫ ", "♬ ", "♩ "}
	note := notes[(v.frame/4)%len(notes)]

	// Headphone pulses gently on downbeats
	beatPulse := math.Pow(math.Sin(0.18*f), 4.0) > 0.6
	var hpStyle lipgloss.Style
	if beatPulse {
		hpStyle = lipgloss.NewStyle().Foreground(th.Playing).Bold(true)
	} else {
		hpStyle = lipgloss.NewStyle().Foreground(th.Highlight)
	}

	headStyle := lipgloss.NewStyle().Foreground(th.Primary).Bold(true)
	armStyle := lipgloss.NewStyle().Foreground(th.Secondary).Bold(true)
	bracketStyle := lipgloss.NewStyle().Foreground(th.Muted)
	discStyle := lipgloss.NewStyle().Foreground(th.Favorite)
	spinStyle := lipgloss.NewStyle().Foreground(th.Playing).Bold(true)
	noteStyle := lipgloss.NewStyle().Foreground(th.Secondary)

	// Fixed-width components:
	// hpStr: 3 cols ("🎧 ")
	// headStr: 9 cols
	// armStr: 2 cols
	// deckStr: 6 cols ("[💿 ◓]")
	// noteStr: 2 cols ("♫ ")
	hpStr := hpStyle.Render("🎧 ")
	headStr := headStyle.Render(pose.head)
	armStr := armStyle.Render(pose.arm)
	deckStr := bracketStyle.Render("[") + discStyle.Render("💿") + " " + spinStyle.Render(spin) + bracketStyle.Render("]")
	noteStr := noteStyle.Render(note)

	// Ultra-compact (width < 16): 🎧 (=^･ω･^=) (12 cols)
	if width < 16 {
		compact := hpStr + headStr
		if lipgloss.Width(compact) <= width {
			return compact
		}
		if lipgloss.Width(headStr) <= width {
			return headStr
		}
		return hpStr
	}

	// Compact (16 <= width < 24): 🎧 (=^･ω･^=)ﾉ ◓ (17 cols)
	if width < 24 {
		compact := hpStr + headStr + armStr + " " + spinStyle.Render(spin)
		if lipgloss.Width(compact) <= width {
			return compact
		}
		return hpStr + headStr
	}

	// Standard DJ Booth (exact 24 cols):
	// 🎧 (=^･ω･^=)ﾉ [💿 ◓] ♫
	boothStr := hpStr + headStr + armStr + " " + deckStr + " " + noteStr

	// If width allows (>= 31 cols), render the solid 6-bar mini equalizer rack:
	// 🎧 (=^･ω･^=)ﾉ [💿 ◓] ♫   ▂▃▄▅▆ (exact 31 cols)
	if width >= 31 {
		const numBars = 6
		v.updateBarPhysics(numBars)

		barChars := []rune{' ', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
		bassStyle := lipgloss.NewStyle().Foreground(th.Primary)
		midStyle := lipgloss.NewStyle().Foreground(th.Secondary)
		trebStyle := lipgloss.NewStyle().Foreground(th.Highlight)
		peakStyle := lipgloss.NewStyle().Foreground(th.Playing).Bold(true)

		var eqSb strings.Builder
		eqSb.WriteString(" ")
		for i := 0; i < numBars; i++ {
			h := v.heights[i]
			bIdx := clampRange(int(math.Floor(h*float64(len(barChars)-1))), 0, len(barChars)-1)
			ch := barChars[bIdx]

			var st lipgloss.Style
			if v.peaks[i] > 0.85 && h > 0.75 {
				st = peakStyle
			} else if i < 2 {
				st = bassStyle
			} else if i < 4 {
				st = midStyle
			} else {
				st = trebStyle
			}
			eqSb.WriteString(st.Render(string(ch)))
		}

		full := boothStr + eqSb.String()
		if lipgloss.Width(full) <= width {
			return full
		}
	}

	return boothStr
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

		// Musical multi-frequency harmonic model
		subBass := math.Pow(math.Sin(0.12*f), 4.0) * math.Max(0, 0.6*(1.0-1.8*x))
		bass := 0.45 + 0.35*math.Sin(0.12*f+0.8*x)
		mids := 0.35*math.Sin(0.2*f+2.5*x) + 0.2*math.Cos(0.14*f-3.0*x)
		highs := 0.25 * math.Sin(0.32*f+5.0*x)

		envelope := 0.95 - 0.25*x
		raw := (subBass + bass + mids + highs) * envelope
		rawTargets[i] = clampFloat(raw, 0.05, 1.0)
	}

	// Smooth spatial averaging & attack/decay physics
	for i := 0; i < numBins; i++ {
		left := rawTargets[i]
		if i > 0 {
			left = rawTargets[i-1]
		}
		right := rawTargets[i]
		if i < numBins-1 {
			right = rawTargets[i+1]
		}
		target := 0.2*left + 0.6*rawTargets[i] + 0.2*right

		// Smooth attack and gentle exponential decay
		if target > v.heights[i] {
			v.heights[i] += 0.5 * (target - v.heights[i])
		} else {
			v.heights[i] -= 0.12 * (v.heights[i] - target)
		}
		v.heights[i] = clampFloat(v.heights[i], 0.02, 1.0)

		// Peak hold dynamics
		if v.heights[i] >= v.peaks[i] {
			v.peaks[i] = v.heights[i]
			v.peakHold[i] = 4
		} else {
			if v.peakHold[i] > 0 {
				v.peakHold[i]--
			} else {
				v.peaks[i] = math.Max(0.0, v.peaks[i]-0.08)
			}
		}
	}
}

func (v *Visualizer) renderBars(width int, th theme.Theme) string {
	prefix := "♫ "
	suffix := " ♬"
	if width < 10 {
		prefix = ""
		suffix = ""
	}
	numBars := width - lipgloss.Width(prefix) - lipgloss.Width(suffix)
	if numBars < 1 {
		numBars = 1
	}

	v.updateBarPhysics(numBars)

	barChars := []rune{' ', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	bassStyle := lipgloss.NewStyle().Foreground(th.Primary)
	midStyle := lipgloss.NewStyle().Foreground(th.Secondary)
	trebStyle := lipgloss.NewStyle().Foreground(th.Highlight)
	peakStyle := lipgloss.NewStyle().Foreground(th.Playing).Bold(true)

	var sb strings.Builder
	if prefix != "" {
		sb.WriteString(lipgloss.NewStyle().Foreground(th.Primary).Render(prefix))
	}

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

	if suffix != "" {
		sb.WriteString(lipgloss.NewStyle().Foreground(th.Secondary).Render(suffix))
	}
	return sb.String()
}

func (v *Visualizer) renderWave(width int, th theme.Theme) string {
	prefix := "∿ "
	suffix := " ∿"
	if width < 10 {
		prefix = ""
		suffix = ""
	}
	numChars := width - lipgloss.Width(prefix) - lipgloss.Width(suffix)
	if numChars < 1 {
		numChars = 1
	}

	f := float64(v.frame)
	amplitude := 0.5 + 0.4*math.Sin(0.08*f)

	waveGlyphs := []string{"_", "⎽", "⎼", "─", "⎻", "⎺", "▔"}

	troughStyle := lipgloss.NewStyle().Foreground(th.Secondary)
	zeroStyle := lipgloss.NewStyle().Foreground(th.Muted)
	crestStyle := lipgloss.NewStyle().Foreground(th.Playing).Bold(true)
	peakStyle := lipgloss.NewStyle().Foreground(th.Highlight).Bold(true)

	var sb strings.Builder
	if prefix != "" {
		sb.WriteString(lipgloss.NewStyle().Foreground(th.Primary).Render(prefix))
	}

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

	if suffix != "" {
		sb.WriteString(lipgloss.NewStyle().Foreground(th.Primary).Render(suffix))
	}
	return sb.String()
}

func (v *Visualizer) renderSpectrum(width int, th theme.Theme) string {
	f := float64(v.frame)

	bassVal := clampFloat(0.4+0.45*math.Sin(0.14*f)+0.15*math.Sin(0.28*f), 0.1, 1.0)
	midVal := clampFloat(0.4+0.4*math.Sin(0.2*f+1.0)+0.2*math.Cos(0.12*f), 0.1, 1.0)
	trebVal := clampFloat(0.35+0.35*math.Sin(0.3*f+2.0)+0.15*math.Sin(0.45*f), 0.1, 1.0)

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
	if lipgloss.Width(fullStr) <= width {
		return fullStr
	}

	compactStr := fmt.Sprintf("🔊 B:%s M:%s T:%s", getBar(bassVal), getBar(midVal), getBar(trebVal))
	if lipgloss.Width(compactStr) <= width {
		return lipgloss.NewStyle().Foreground(th.Playing).Render(compactStr)
	}

	tinyStr := fmt.Sprintf("B:%s M:%s", getBar(bassVal), getBar(midVal))
	if lipgloss.Width(tinyStr) <= width {
		return lipgloss.NewStyle().Foreground(th.Playing).Render(tinyStr)
	}

	return lipgloss.NewStyle().Foreground(th.Playing).Render(getBar(bassVal) + getBar(midVal) + getBar(trebVal))
}

func (v *Visualizer) renderMinimal(width int, th theme.Theme) string {
	meterWidth := (width - 6) / 2
	if meterWidth < 1 {
		meterWidth = 1
	}

	filledL := clampRange(int(v.vuLeft*float64(meterWidth)), 0, meterWidth)
	emptyL := meterWidth - filledL
	barL := strings.Repeat("█", filledL) + strings.Repeat("░", emptyL)

	filledR := clampRange(int(v.vuRight*float64(meterWidth)), 0, meterWidth)
	emptyR := meterWidth - filledR
	barR := strings.Repeat("█", filledR) + strings.Repeat("░", emptyR)

	styleL := lipgloss.NewStyle().Foreground(th.Playing)
	styleR := lipgloss.NewStyle().Foreground(th.Highlight)
	labelStyle := lipgloss.NewStyle().Foreground(th.Muted).Bold(true)

	if width < 12 {
		return styleL.Render(barL) + labelStyle.Render("|") + styleR.Render(barR)
	}

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
