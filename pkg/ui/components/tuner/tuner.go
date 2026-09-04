package tuner

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
)

type BandConfig struct {
	Name      string
	MinFreq   float64
	MaxFreq   float64
	Step      float64
	Unit      string
	Tolerance float64
	Markers   []float64
}

var Bands = map[string]BandConfig{
	"FM": {
		Name:      "FM Broadcast",
		MinFreq:   87.5,
		MaxFreq:   108.0,
		Step:      0.1,
		Unit:      "MHz",
		Tolerance: 0.35,
		Markers:   []float64{88.0, 92.0, 96.0, 100.0, 104.0, 108.0},
	},
	"AM": {
		Name:      "Medium Wave (AM)",
		MinFreq:   530.0,
		MaxFreq:   1710.0,
		Step:      10.0,
		Unit:      "kHz",
		Tolerance: 25.0,
		Markers:   []float64{540.0, 700.0, 900.0, 1100.0, 1300.0, 1500.0, 1700.0},
	},
	"SW": {
		Name:      "Shortwave (SW)",
		MinFreq:   3.20,
		MaxFreq:   22.00,
		Step:      0.05,
		Unit:      "MHz",
		Tolerance: 0.15,
		Markers:   []float64{3.5, 7.0, 10.0, 14.0, 18.0, 21.0},
	},
}

// NextBand cycles to the next frequency band: FM -> AM -> SW -> FM.
func NextBand(current string) string {
	switch strings.ToUpper(current) {
	case "FM":
		return "AM"
	case "AM":
		return "SW"
	default:
		return "FM"
	}
}

// CalculateSignalRSSI calculates signal reception strength (0.0 to 1.0) and finds closest station.
func CalculateSignalRSSI(stations []radio.Station, band string, currentFreq float64) (float64, *radio.Station) {
	cfg, ok := Bands[strings.ToUpper(band)]
	if !ok {
		cfg = Bands["FM"]
	}

	var closestStation *radio.Station
	minDist := math.MaxFloat64

	for i := range stations {
		st := &stations[i]
		stFreq := radio.ExtractOrAssignFrequency(*st, band)
		dist := math.Abs(currentFreq - stFreq)
		if dist < minDist {
			minDist = dist
			closestStation = st
		}
	}

	if closestStation == nil || minDist > cfg.Tolerance {
		return 0.05, nil
	}

	// Gaussian / Quadratic resonance curve
	ratio := minDist / cfg.Tolerance
	rssi := math.Max(0.05, 1.0-(ratio*ratio))
	return math.Min(1.0, rssi), closestStation
}

// FindNearestStation returns the closest station in the catalog for the band and the frequency distance (stFreq - currentFreq).
func FindNearestStation(stations []radio.Station, band string, currentFreq float64) (*radio.Station, float64) {
	var closest *radio.Station
	minDist := math.MaxFloat64
	delta := 0.0

	for i := range stations {
		st := &stations[i]
		stFreq := radio.ExtractOrAssignFrequency(*st, band)
		dist := math.Abs(currentFreq - stFreq)
		if dist < minDist {
			minDist = dist
			closest = st
			delta = stFreq - currentFreq
		}
	}
	return closest, delta
}

// FormatSMeter formats an authentic ham radio S-meter readout.
func FormatSMeter(rssi float64, th theme.Theme) string {
	signalColor := th.Muted
	label := "S1"
	if rssi >= 0.95 {
		signalColor = th.Playing
		label = "S9+20"
	} else if rssi >= 0.80 {
		signalColor = th.Playing
		label = "S9"
	} else if rssi >= 0.60 {
		signalColor = th.Highlight
		label = "S7"
	} else if rssi >= 0.40 {
		signalColor = th.Highlight
		label = "S5"
	} else if rssi >= 0.20 {
		signalColor = th.Muted
		label = "S3"
	}

	blocks := int(math.Round(rssi * 8))
	if blocks > 8 {
		blocks = 8
	}
	meterStr := strings.Repeat("■", blocks) + strings.Repeat("·", 8-blocks)
	return lipgloss.NewStyle().Foreground(signalColor).Bold(true).Render(fmt.Sprintf("[%s %s]", label, meterStr))
}

// RenderAnalogTunerView renders the vintage retro radio dial interface.
func RenderAnalogTunerView(
	stations []radio.Station,
	currentFreq float64,
	band string,
	playingStationID string,
	width, height int,
	th theme.Theme,
) string {
	bandUpper := strings.ToUpper(strings.TrimSpace(band))
	cfg, ok := Bands[bandUpper]
	if !ok {
		bandUpper = "FM"
		cfg = Bands["FM"]
	}

	// Clamp current frequency within band limits
	if currentFreq < cfg.MinFreq {
		currentFreq = cfg.MinFreq
	}
	if currentFreq > cfg.MaxFreq {
		currentFreq = cfg.MaxFreq
	}

	if width < 30 || height < 9 {
		return lipgloss.NewStyle().Foreground(th.Muted).Render("Terminal window too small for Analog Tuner")
	}

	contentW := width - 6
	if contentW > 80 {
		contentW = 80
	}
	if contentW < 24 {
		contentW = 24
	}

	innerBoxW := contentW - 4
	if innerBoxW < 18 {
		innerBoxW = 18
	}

	// Calculate Signal RSSI
	rssi, lockedStation := CalculateSignalRSSI(stations, bandUpper, currentFreq)
	isLocked := rssi >= 0.55 && lockedStation != nil

	// Dial Title & Frequency Readout
	headerTitle := lipgloss.NewStyle().
		Bold(true).
		Foreground(th.Primary).
		Width(contentW).
		Align(lipgloss.Center).
		Render(fmt.Sprintf("📻 VINTAGE ANALOG HAM TUNER • %s", cfg.Name))

	freqDisplay := lipgloss.NewStyle().
		Bold(true).
		Background(th.Primary).
		Foreground(th.BadgeText).
		Padding(0, 1).
		Render(fmt.Sprintf("[ %.1f %s ]", currentFreq, cfg.Unit))
	if bandUpper == "AM" {
		freqDisplay = lipgloss.NewStyle().
			Bold(true).
			Background(th.Primary).
			Foreground(th.BadgeText).
			Padding(0, 1).
			Render(fmt.Sprintf("[ %.0f %s ]", currentFreq, cfg.Unit))
	} else if bandUpper == "SW" {
		freqDisplay = lipgloss.NewStyle().
			Bold(true).
			Background(th.Primary).
			Foreground(th.BadgeText).
			Padding(0, 1).
			Render(fmt.Sprintf("[ %.2f %s ]", currentFreq, cfg.Unit))
	}

	// Signal Strength S-Meter (RSSI)
	sMeter := FormatSMeter(rssi, th)
	lockBadge := ""
	if isLocked {
		lockBadge = " " + lipgloss.NewStyle().Background(th.Playing).Foreground(th.BadgeText).Bold(true).Padding(0, 1).Render("LOCKED")
	} else if rssi >= 0.25 {
		lockBadge = " " + lipgloss.NewStyle().Foreground(th.Highlight).Bold(true).Render("RECEIVING")
	} else {
		lockBadge = " " + lipgloss.NewStyle().Foreground(th.Muted).Render("STATIC")
	}

	topRow := lipgloss.NewStyle().
		Width(contentW).
		Align(lipgloss.Center).
		Render(lipgloss.JoinHorizontal(lipgloss.Center, freqDisplay, "  ", sMeter, lockBadge))

	availInterior := height - 2
	borderedScale := availInterior >= 9

	// Horizontal Dial Scale Rendering
	scaleInnerW := contentW
	if borderedScale {
		scaleInnerW = innerBoxW - 2
	}
	if scaleInnerW < 18 {
		scaleInnerW = 18
	}

	// Calculate needle position: 0 to scaleInnerW-1
	freqRatio := (currentFreq - cfg.MinFreq) / (cfg.MaxFreq - cfg.MinFreq)
	needlePos := int(math.Round(freqRatio * float64(scaleInnerW-1)))
	if needlePos < 0 {
		needlePos = 0
	}
	if needlePos >= scaleInnerW {
		needlePos = scaleInnerW - 1
	}

	// Top Scale Frequency Numeric Labels
	numRowChars := make([]rune, scaleInnerW)
	for i := range numRowChars {
		numRowChars[i] = ' '
	}

	for _, marker := range cfg.Markers {
		mRatio := (marker - cfg.MinFreq) / (cfg.MaxFreq - cfg.MinFreq)
		mPos := int(math.Round(mRatio * float64(scaleInnerW-1)))
		label := fmt.Sprintf("%.0f", marker)
		if bandUpper == "SW" {
			label = fmt.Sprintf("%.1f", marker)
		}
		if mPos+len(label) <= scaleInnerW {
			for idx, r := range label {
				if mPos+idx < scaleInnerW {
					numRowChars[mPos+idx] = r
				}
			}
		}
	}
	numRow := lipgloss.NewStyle().Foreground(th.Muted).Render(string(numRowChars))

	// Needle indicator row: ' ' ... '▼' ... ' '
	needleRowChars := make([]rune, scaleInnerW)
	for i := range needleRowChars {
		needleRowChars[i] = ' '
	}
	needleRowChars[needlePos] = '▼'
	needleRow := lipgloss.NewStyle().Foreground(th.Favorite).Bold(true).Render(string(needleRowChars))

	// Scale Track with Ticks and Station Positions
	trackChars := make([]rune, scaleInnerW)
	for i := range trackChars {
		if i%4 == 0 {
			trackChars[i] = '┼'
		} else {
			trackChars[i] = '─'
		}
	}
	trackChars[0] = '├'
	trackChars[scaleInnerW-1] = '┤'

	// Render station markers on track
	for _, st := range stations {
		stF := radio.ExtractOrAssignFrequency(st, bandUpper)
		stRatio := (stF - cfg.MinFreq) / (cfg.MaxFreq - cfg.MinFreq)
		stPos := int(math.Round(stRatio * float64(scaleInnerW-1)))
		if stPos > 0 && stPos < scaleInnerW-1 && stPos != needlePos {
			trackChars[stPos] = '▲'
		}
	}

	// Highlight needle intersection on track
	trackRow := lipgloss.NewStyle().Foreground(th.Border).Render(string(trackChars[:needlePos])) +
		lipgloss.NewStyle().Foreground(th.Favorite).Bold(true).Render("│") +
		lipgloss.NewStyle().Foreground(th.Border).Render(string(trackChars[needlePos+1:]))

	var scaleBox string
	if !borderedScale {
		scaleBox = lipgloss.JoinVertical(lipgloss.Left, numRow, needleRow, trackRow)
	} else {
		scaleBox = lipgloss.NewStyle().
			Background(th.Background).
			Border(lipgloss.NormalBorder()).
			BorderForeground(th.Border).
			Padding(0, 1).
			Width(innerBoxW).
			Render(lipgloss.JoinVertical(lipgloss.Left, numRow, needleRow, trackRow))
	}

	// Dynamic Layout sizing based on height:
	var cabinetElements []string
	cabinetElements = append(cabinetElements, headerTitle)
	if availInterior >= 17 {
		cabinetElements = append(cabinetElements, "")
	}
	cabinetElements = append(cabinetElements, topRow)
	if availInterior >= 19 {
		cabinetElements = append(cabinetElements, "")
	}
	cabinetElements = append(cabinetElements, scaleBox)

	if availInterior < 13 {
		// Compact single-line status for constrained terminals
		if availInterior >= 10 {
			cabinetElements = append(cabinetElements, "")
		}
		var singleStatus string
		if isLocked && lockedStation != nil {
			singleStatus = fmt.Sprintf("📡 LOCKED: %s (%s)  [Enter: Tune]", lockedStation.Name, lockedStation.LocationString())
			maxLen := contentW - 2
			if maxLen > 5 && len(singleStatus) > maxLen {
				singleStatus = singleStatus[:maxLen-1] + "…"
			}
		} else {
			staticWave := generateStaticPattern(int(currentFreq * 10))
			singleStatus = fmt.Sprintf("⚡ STATIC: %s  [Sweep h/l]", staticWave)
		}
		statusLine := lipgloss.NewStyle().Foreground(th.Highlight).Bold(true).Width(contentW).Align(lipgloss.Center).Render(singleStatus)
		cabinetElements = append(cabinetElements, statusLine)
	} else {
		// Multi-line status card
		if availInterior >= 15 {
			cabinetElements = append(cabinetElements, "")
		}
		var statusSection string
		if isLocked && lockedStation != nil {
			isPlaying := lockedStation.ID == playingStationID
			statusIcon := "📡"
			if isPlaying {
				statusIcon = "▶"
			}

			name := lockedStation.Name
			if len(name) > innerBoxW-16 && innerBoxW > 20 {
				name = name[:innerBoxW-17] + "…"
			}

			stTitle := lipgloss.NewStyle().
				Bold(true).
				Foreground(th.Highlight).
				Render(fmt.Sprintf("%s STATION: %s", statusIcon, name))

			locInfo := lockedStation.LocationString()
			genreInfo := lockedStation.Genre
			if genreInfo == "" {
				genreInfo = "General Broadcast"
			}
			detailsText := fmt.Sprintf("   Location: %s  •  Genre: %s (%d kbps)", locInfo, genreInfo, lockedStation.Bitrate)
			if len(detailsText) > innerBoxW-4 && innerBoxW > 10 {
				detailsText = detailsText[:innerBoxW-5] + "…"
			}

			details := lipgloss.NewStyle().
				Foreground(th.Foreground).
				Render(detailsText)

			tuneActionText := "   ● Live Stream Receiving [Space: Mute]"
			if len(tuneActionText) > innerBoxW-2 && innerBoxW > 15 {
				tuneActionText = tuneActionText[:innerBoxW-3] + "…"
			}
			tuneAction := lipgloss.NewStyle().
				Foreground(th.Playing).
				Bold(true).
				Render(tuneActionText)

			stationPct := int(math.Round(rssi * 100))
			staticPct := 100 - stationPct
			audioBalance := fmt.Sprintf("   🔊 Audio Output: %d%% Broadcast • %d%% Atmospheric Static", stationPct, staticPct)
			if len(audioBalance) > innerBoxW-2 && innerBoxW > 15 {
				audioBalance = fmt.Sprintf("   🔊 %d%% Stn • %d%% Static", stationPct, staticPct)
			}
			audioBalStyle := lipgloss.NewStyle().Foreground(th.Secondary).Render(audioBalance)

			statusSection = lipgloss.JoinVertical(lipgloss.Left, stTitle, details, audioBalStyle, tuneAction)
		} else {
			staticWave := generateStaticPattern(int(currentFreq * 10))
			nearestSt, delta := FindNearestStation(stations, bandUpper, currentFreq)

			staticTitleText := fmt.Sprintf("⚡ ATMOSPHERIC RF STATIC NOISE:  %s", staticWave)
			if len(staticTitleText) > innerBoxW-2 && innerBoxW > 15 {
				staticTitleText = fmt.Sprintf("⚡ STATIC: %s", staticWave)
			}
			staticTitle := lipgloss.NewStyle().
				Foreground(th.Highlight).
				Bold(true).
				Render(staticTitleText)

			var nearestInfo string
			if nearestSt != nil {
				deltaSign := "+"
				if delta < 0 {
					deltaSign = ""
				}
				nearestInfo = fmt.Sprintf("   Nearest Carrier: %s (%s%.1f %s)", nearestSt.Name, deltaSign, delta, cfg.Unit)
				if len(nearestInfo) > innerBoxW-2 && innerBoxW > 15 {
					nearestInfo = fmt.Sprintf("   Nearest: %s (%s%.1f)", nearestSt.Name, deltaSign, delta)
				}
			} else {
				nearestInfo = "   Audio Output: Continuous RF Atmospheric Static"
			}
			nearest := lipgloss.NewStyle().
				Foreground(th.Foreground).
				Render(nearestInfo)

			hintText := "   Sweep dial with [h/l] or [H/L] to tune into carrier signals"
			if len(hintText) > innerBoxW-2 && innerBoxW > 10 {
				hintText = "   Sweep dial with [h/l] or [H/L]"
			}
			hint := lipgloss.NewStyle().
				Foreground(th.Muted).
				Render(hintText)
			statusSection = lipgloss.JoinVertical(lipgloss.Left, staticTitle, nearest, hint)
		}

		statusPaddingY := 0
		if availInterior >= 22 {
			statusPaddingY = 1
		}
		statusBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(th.Border).
			Padding(statusPaddingY, 1).
			Width(innerBoxW).
			Render(statusSection)
		cabinetElements = append(cabinetElements, statusBox)
	}

	if availInterior >= 7 {
		instructionText := "[h/l: Sweep | H/L: Fast | b: Band | n/N: Seek | Space: Mute | 9: Globe]"
		if len(instructionText) > contentW {
			instructionText = "[h/l: Sweep | H/L: Fast | b: Band | Space: Mute]"
		}
		if len(instructionText) > contentW {
			instructionText = "[h/l: Sweep | Space: Mute]"
		}
		instructions := lipgloss.NewStyle().
			Foreground(th.Secondary).
			Italic(true).
			Width(contentW).
			Align(lipgloss.Center).
			Render(instructionText)
		cabinetElements = append(cabinetElements, instructions)
	}

	cabinet := lipgloss.JoinVertical(
		lipgloss.Center,
		cabinetElements...,
	)

	rendered := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.Primary).
		Padding(0, 1).
		Width(contentW).
		Render(cabinet)

	lines := strings.Split(rendered, "\n")
	if len(lines) > height && height > 0 {
		rendered = strings.Join(lines[:height], "\n")
	}
	return rendered
}

func generateStaticPattern(seed int) string {
	tick := int(time.Now().UnixMilli() / 150)
	patterns := []string{
		"~ · ∿ · - ≋ · ∿ · ~",
		"· ∿ · ~ ≋ - · ∿ ~ ·",
		"- ~ · ≋ · - ∿ · ~ ∿",
		"≋ · ∿ ~ - · ∿ ≋ · ~",
		"∿ ~ · ≋ - ∿ · ~ · ≋",
	}
	return patterns[(seed+tick)%len(patterns)]
}
