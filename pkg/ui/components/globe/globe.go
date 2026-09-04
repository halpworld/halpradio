package globe

import (
	"fmt"
	"math"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
)

// BrailleCanvas provides a 2x4 subpixel dot grid for terminal Braille graphics.
type BrailleCanvas struct {
	WidthChars  int
	HeightChars int
	WidthDots   int
	HeightDots  int
	grid        []byte
	hotspots    []bool
	crosshairs  []bool
}

// NewBrailleCanvas creates a new Braille drawing buffer of width x height character cells.
func NewBrailleCanvas(wChars, hChars int) *BrailleCanvas {
	if wChars < 4 {
		wChars = 4
	}
	if hChars < 4 {
		hChars = 4
	}
	wDots := wChars * 2
	hDots := hChars * 4
	cellCount := wChars * hChars

	return &BrailleCanvas{
		WidthChars:  wChars,
		HeightChars: hChars,
		WidthDots:   wDots,
		HeightDots:  hDots,
		grid:        make([]byte, cellCount),
		hotspots:    make([]bool, cellCount),
		crosshairs:  make([]bool, cellCount),
	}
}

// SetDot turns on the dot at pixel coordinate (x, y).
func (c *BrailleCanvas) SetDot(x, y int) {
	if x < 0 || x >= c.WidthDots || y < 0 || y >= c.HeightDots {
		return
	}
	charX := x / 2
	charY := y / 4
	cellIdx := charY*c.WidthChars + charX

	subX := x % 2
	subY := y % 4

	var mask byte
	if subX == 0 {
		switch subY {
		case 0:
			mask = 0x01 // dot 1
		case 1:
			mask = 0x02 // dot 2
		case 2:
			mask = 0x04 // dot 3
		case 3:
			mask = 0x40 // dot 7
		}
	} else {
		switch subY {
		case 0:
			mask = 0x08 // dot 4
		case 1:
			mask = 0x10 // dot 5
		case 2:
			mask = 0x20 // dot 6
		case 3:
			mask = 0x80 // dot 8
		}
	}

	c.grid[cellIdx] |= mask
}

// MarkHotspot flags a character cell as containing an active broadcast station cluster.
func (c *BrailleCanvas) MarkHotspot(x, y int) {
	if x < 0 || x >= c.WidthDots || y < 0 || y >= c.HeightDots {
		return
	}
	charX := x / 2
	charY := y / 4
	cellIdx := charY*c.WidthChars + charX
	c.hotspots[cellIdx] = true

	// Set surrounding dots to form a distinct glowing cluster marker
	c.SetDot(x, y)
	c.SetDot(x+1, y)
	c.SetDot(x, y+1)
	c.SetDot(x+1, y+1)
}

// RenderGlobeView draws the interactive 3D Braille World Globe alongside the station cluster explorer.
func RenderGlobeView(
	clusters []radio.StationCluster,
	currentLat, currentLon float64,
	zoom float64,
	selectedStationIdx int,
	playingStationID string,
	width, height int,
	th theme.Theme,
) string {
	if width < 30 || height < 10 {
		return lipgloss.NewStyle().Foreground(th.Muted).Render("Terminal window too small for 3D Globe Explorer")
	}

	// Calculate dimensions
	// Globe aspect ratio: character width vs height
	splitLayout := width >= 76 && height >= 12
	var globeCharW, globeCharH int
	var infoWidth int

	if splitLayout {
		infoWidth = 32
		if width >= 110 {
			infoWidth = 40
		}
		globeCharW = width - infoWidth - 9
		globeCharH = height - 2
	} else {
		globeCharW = width - 6
		globeCharH = height - 3
		if globeCharH < 3 {
			globeCharH = 3
		}
		infoWidth = width - 6
	}

	if globeCharW < 12 {
		globeCharW = 12
	}
	if globeCharH < 3 {
		globeCharH = 3
	}

	// Globe radius in character cells
	// In braille, 1 char = 2 dots wide x 4 dots tall.
	// Since 1 char is ~2:1 vertical-to-horizontal in terminal fonts,
	// (2 dots width) : (4 dots height) yields ~1:1 pixel aspect ratio!
	canvas := NewBrailleCanvas(globeCharW, globeCharH)

	cx := float64(canvas.WidthDots) / 2.0
	cy := float64(canvas.HeightDots) / 2.0

	// Base radius fits the smallest canvas axis
	baseRadius := math.Min(cx, cy) - 2.0
	if baseRadius < 4.0 {
		baseRadius = 4.0
	}
	if zoom <= 0.2 {
		zoom = 1.0
	}
	radius := baseRadius * zoom

	phi0 := currentLat * (math.Pi / 180.0) // pitch (latitude)
	lam0 := currentLon * (math.Pi / 180.0) // yaw (longitude)

	cosPhi0 := math.Cos(phi0)
	sinPhi0 := math.Sin(phi0)
	cosLam0 := math.Cos(lam0)
	sinLam0 := math.Sin(lam0)

	// Step 1: Render Earth Landmass on Sphere
	// Sample sphere surface pixels
	for py := 0; py < canvas.HeightDots; py++ {
		dy := (float64(py) - cy) / radius
		for px := 0; px < canvas.WidthDots; px++ {
			dx := (float64(px) - cx) / radius
			r2 := dx*dx + dy*dy

			if r2 > 1.0 {
				continue // Outside sphere
			}

			// Point on front hemisphere of sphere (camera coordinates)
			xc := dx
			yc := -dy // Screen y points down, world y points up (North)
			zc := math.Sqrt(1.0 - r2)

			// Rotate camera space point back to world coordinates
			// 1. Pitch rotation around X axis
			x1 := xc
			y1 := yc*cosPhi0 + zc*sinPhi0
			z1 := -yc*sinPhi0 + zc*cosPhi0

			// 2. Yaw rotation around Y axis
			x2 := x1*cosLam0 + z1*sinLam0
			y2 := y1
			z2 := -x1*sinLam0 + z1*cosLam0

			// Convert unit sphere point (x2, y2, z2) to Latitude & Longitude
			lat := math.Asin(math.Max(-1.0, math.Min(1.0, y2))) * (180.0 / math.Pi)
			lon := math.Atan2(x2, z2) * (180.0 / math.Pi)

			// Land sampling
			if IsLand(lat, lon) {
				canvas.SetDot(px, py)
			} else if r2 >= 0.97 && r2 <= 1.0 && (px+py)%2 == 0 {
				// Clean subtle outer spherical perimeter silhouette
				canvas.SetDot(px, py)
			}
		}
	}

	// Step 2: Project Broadcast Hotspots onto Sphere
	for _, cluster := range clusters {
		phiS := cluster.Lat * (math.Pi / 180.0)
		lamS := cluster.Lon * (math.Pi / 180.0)

		// Spherical point
		xe := math.Cos(phiS) * math.Sin(lamS)
		ye := math.Sin(phiS)
		ze := math.Cos(phiS) * math.Cos(lamS)

		// Inverse yaw
		x1 := xe*cosLam0 - ze*sinLam0
		y1 := ye
		z1 := xe*sinLam0 + ze*cosLam0

		// Inverse pitch
		xc := x1
		yc := y1*cosPhi0 - z1*sinPhi0
		zc := y1*sinPhi0 + z1*cosPhi0

		// If visible on front hemisphere
		if zc > 0.05 {
			px := int(math.Round(cx + xc*radius))
			py := int(math.Round(cy - yc*radius))
			canvas.MarkHotspot(px, py)
		}
	}

	// Step 3: Draw Center Crosshair Target on Center Character Cells
	centerCharX := canvas.WidthChars / 2
	centerCharY := canvas.HeightChars / 2

	// Render Braille Grid to Strings with Theme Styling
	landStyle := lipgloss.NewStyle().Foreground(th.Playing)
	hotspotStyle := lipgloss.NewStyle().Foreground(th.Highlight).Bold(true)
	crosshairStyle := lipgloss.NewStyle().Foreground(th.Favorite).Bold(true)

	var globeLines []string
	for cyIdx := 0; cyIdx < canvas.HeightChars; cyIdx++ {
		var lineBuilder strings.Builder
		for cxIdx := 0; cxIdx < canvas.WidthChars; cxIdx++ {
			// Center crosshair target overlay
			if cxIdx == centerCharX && cyIdx == centerCharY {
				lineBuilder.WriteString(crosshairStyle.Render("┼"))
				continue
			} else if (cxIdx == centerCharX-1 && cyIdx == centerCharY) || (cxIdx == centerCharX+1 && cyIdx == centerCharY) {
				if cxIdx == centerCharX-1 {
					lineBuilder.WriteString(crosshairStyle.Render("["))
				} else {
					lineBuilder.WriteString(crosshairStyle.Render("]"))
				}
				continue
			}

			cellIdx := cyIdx*canvas.WidthChars + cxIdx
			mask := canvas.grid[cellIdx]
			isHotspot := canvas.hotspots[cellIdx]

			if mask == 0 {
				lineBuilder.WriteString(" ")
			} else {
				brailleRune := rune(0x2800 + int(mask))
				rStr := string(brailleRune)
				if isHotspot {
					lineBuilder.WriteString(hotspotStyle.Render(rStr))
				} else {
					lineBuilder.WriteString(landStyle.Render(rStr))
				}
			}
		}
		globeLines = append(globeLines, lineBuilder.String())
	}

	// Find nearest station cluster to current coordinates and location info
	nearestCluster, _, distKm := radio.FindNearestCluster(clusters, currentLat, currentLon)
	_, locCountry, locCity, locFlag, locDist := radio.LookupNearestLocation(currentLat, currentLon)

	if !splitLayout {
		summary := fmt.Sprintf("📍 %s %s", locFlag, locCity)
		if nearestCluster != nil && distKm <= 1200 {
			summary = fmt.Sprintf("📍 %s %s (%d stns) [Enter: Tune]", nearestCluster.Flag, nearestCluster.City, len(nearestCluster.Stations))
		}
		summaryStyle := lipgloss.NewStyle().Foreground(th.Primary).Bold(true).Width(globeCharW).Align(lipgloss.Center)
		globeLines = append(globeLines, summaryStyle.Render(summary))

		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(th.Border).
			Padding(0, 1).
			Width(globeCharW).
			Render(strings.Join(globeLines, "\n"))
	}

	globeBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.Border).
		Padding(0, 1).
		Width(globeCharW).
		Render(strings.Join(globeLines, "\n"))

	// Step 4: Nearest Station Cluster / Country Information Card
	var infoContent string
	coordStr := fmt.Sprintf("%.1f°%s, %.1f°%s",
		math.Abs(currentLat), latDirection(currentLat),
		math.Abs(currentLon), lonDirection(currentLon),
	)
	metaStyle := lipgloss.NewStyle().Foreground(th.Muted)

	if nearestCluster != nil && distKm <= 1200 {
		cardTitle := lipgloss.NewStyle().
			Bold(true).
			Foreground(th.Primary).
			Render(fmt.Sprintf("%s %s, %s", nearestCluster.Flag, nearestCluster.City, nearestCluster.CountryName))

		metaLine := metaStyle.Render(fmt.Sprintf("📍 %s  📡 %d Stations (dist: %.0fkm)", coordStr, len(nearestCluster.Stations), distKm))

		// Station list within this cluster
		var stationItems []string
		for i, st := range nearestCluster.Stations {
			cursor := "  "
			if i == selectedStationIdx {
				cursor = "▶ "
			}

			stStyle := lipgloss.NewStyle().Foreground(th.Foreground)
			if st.ID == playingStationID {
				stStyle = lipgloss.NewStyle().Foreground(th.Playing).Bold(true)
			} else if i == selectedStationIdx {
				stStyle = lipgloss.NewStyle().Foreground(th.Highlight).Bold(true)
			}

			freqBadge := st.BroadcastBadge()
			if st.Frequency != "" {
				freqBadge = st.Frequency
			}

			name := st.Name
			maxNameLen := infoWidth - 14
			if maxNameLen > 5 && len(name) > maxNameLen {
				name = name[:maxNameLen-1] + "…"
			}

			row := fmt.Sprintf("%s%s (%s)", cursor, stStyle.Render(name), metaStyle.Render(freqBadge))
			stationItems = append(stationItems, row)
		}

		maxDisplayStations := globeCharH - 8
		if maxDisplayStations < 3 {
			maxDisplayStations = 3
		}
		if len(stationItems) > maxDisplayStations {
			stationItems = stationItems[:maxDisplayStations]
			stationItems = append(stationItems, metaStyle.Render("  ...and more"))
		}

		stationsBlock := strings.Join(stationItems, "\n")

		instructionsStyle := lipgloss.NewStyle().Foreground(th.Secondary).Italic(true)
		instructions := instructionsStyle.Render("[Enter] Tune In  [n/p] Next/Prev")

		infoContent = lipgloss.JoinVertical(
			lipgloss.Left,
			cardTitle,
			metaLine,
			"",
			lipgloss.NewStyle().Bold(true).Foreground(th.Secondary).Render("BROADCASTS AT TARGET:"),
			stationsBlock,
			"",
			instructions,
		)
	} else {
		cardTitle := lipgloss.NewStyle().
			Bold(true).
			Foreground(th.Primary).
			Render(fmt.Sprintf("%s %s (%s)", locFlag, locCity, locCountry))

		metaLine := metaStyle.Render(fmt.Sprintf("📍 %s (nearest hub: %.0fkm)", coordStr, locDist))
		hint := lipgloss.NewStyle().Foreground(th.Muted).Render("No direct broadcast stations here.\nRotate the globe [h/j/k/l] to explore!")

		infoContent = lipgloss.JoinVertical(
			lipgloss.Left,
			cardTitle,
			metaLine,
			"",
			hint,
		)
	}

	infoBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.Border).
		Width(infoWidth).
		Height(globeCharH).
		Padding(0, 1).
		Render(infoContent)

	return lipgloss.JoinHorizontal(lipgloss.Top, globeBox, " ", infoBox)
}

func latDirection(lat float64) string {
	if lat >= 0 {
		return "N"
	}
	return "S"
}

func lonDirection(lon float64) string {
	if lon >= 0 {
		return "E"
	}
	return "W"
}
