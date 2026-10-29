package modals

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/ai"
	"github.com/halpworld/halpradio/pkg/theme"
)

// LinerNotesDrawerInput bundles everything the Interactive Liner Notes & Artist
// Biography Drawer (`i`) renders.
type LinerNotesDrawerInput struct {
	Notes       *ai.LinerNotes
	Status      string
	Fetching    bool
	TrackLabel  string
	StationName string
	Genre       string
	Country     string
	ArtLines    []string
	ArtCols     int
	ArtSource   string
	Scroll      int
	Focused     bool
	Width       int
	Height      int
}

// DJBannerInput bundles the live state of the Local AI Radio DJ broadcast box.
type DJBannerInput struct {
	Persona     string
	Engine      string
	Script      string
	DuckPercent int
	Fading      bool
	Width       int
}

// DJSettingsModalInput bundles the state of the AI DJ Studio & ONNX Voice Manager modal (`D`).
type DJSettingsModalInput struct {
	Enabled           bool
	PersonaID         string
	TTSEngine         string
	ActiveEngineLabel string
	SelectedVoiceID   string
	OllamaURL         string
	OllamaModel       string
	DuckLevel         int
	VoiceModels       []ai.VoiceModelSpec
	InstalledModels   map[string]bool
	Cursor            int
	Downloading       bool
	Status            string
	Width             int
	Height            int
}

// RenderLinerNotesDrawer renders the sliding right-hand terminal drawer (or
// full-width overlay on narrow terminals) displaying artist biography, album
// context, discography highlights, track trivia, and AI DJ quick controls.
func RenderLinerNotesDrawer(in LinerNotesDrawerInput, th theme.Theme) string {
	width := in.Width
	if width < 20 {
		width = 20
	}
	height := in.Height
	if height < 6 {
		height = 6
	}

	borderColor := th.Border
	if in.Focused {
		borderColor = th.Primary
	}
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(width - 2).
		Height(height - 2)

	innerW := width - 4
	if innerW < 10 {
		innerW = 10
	}
	maxInnerH := height - 2
	if maxInnerH < 1 {
		maxInnerH = 1
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Primary)
	badgeStyle := lipgloss.NewStyle().Background(th.Badge).Foreground(th.BadgeText).Bold(true).Padding(0, 1)
	mutedStyle := lipgloss.NewStyle().Foreground(th.Muted)
	borderStyle := lipgloss.NewStyle().Foreground(th.Border)

	var head []string
	head = append(head, centerLine(titleStyle.Render(truncate("📖 LINER NOTES & ARTIST BIO", innerW)), innerW))

	sourceLabel := "Offline Station Metadata"
	if in.Notes != nil && strings.TrimSpace(in.Notes.Source) != "" {
		sourceLabel = in.Notes.Source
	} else if in.Fetching {
		sourceLabel = "MusicBrainz + Wikipedia…"
	}
	if maxInnerH >= 8 {
		head = append(head, centerLine(badgeStyle.Render(truncate(sourceLabel, innerW-2)), innerW))
	}

	if len(in.ArtLines) > 0 && maxInnerH >= 16 {
		head = append(head, "")
		for _, line := range in.ArtLines {
			head = append(head, centerLine(line, innerW))
		}
		if in.ArtSource != "" {
			head = append(head, centerLine(mutedStyle.Render(truncate("🖼 "+in.ArtSource, innerW)), innerW))
		}
	}

	if maxInnerH >= 10 {
		head = append(head, centerLine(borderStyle.Render(strings.Repeat("─", innerW)), innerW))
	}

	foot := renderLinerNotesFooter(innerW, th)

	bodyHeight := maxInnerH - len(head) - len(foot)
	var body []string
	if bodyHeight > 0 {
		body = renderLinerNotesBody(in, innerW, bodyHeight, th)
	}

	all := append([]string{}, head...)
	all = append(all, body...)
	all = append(all, foot...)
	if len(all) > maxInnerH {
		all = all[:maxInnerH]
	}
	return boxStyle.Render(strings.Join(all, "\n"))
}

func renderLinerNotesBody(in LinerNotesDrawerInput, width, height int, th theme.Theme) []string {
	secStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Secondary)
	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Highlight)
	textStyle := lipgloss.NewStyle().Foreground(th.Foreground)
	mutedStyle := lipgloss.NewStyle().Foreground(th.Muted)
	accentStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Playing)

	if in.Notes == nil {
		msg := in.Status
		if msg == "" {
			if in.Fetching {
				msg = "Fetching artist biography & recording context from MusicBrainz + Wikipedia…"
			} else {
				msg = "Press i to load liner notes for the current broadcast."
			}
		}
		wrapped := wrapPlain(msg, width)
		out := make([]string, 0, height)
		pad := (height - len(wrapped)) / 2
		for i := 0; i < pad && len(out) < height; i++ {
			out = append(out, "")
		}
		for _, line := range wrapped {
			if len(out) >= height {
				break
			}
			out = append(out, mutedStyle.Italic(true).Render(line))
		}
		for len(out) < height {
			out = append(out, "")
		}
		return out
	}

	n := in.Notes
	var lines []string

	// Metadata summary block
	artist := fallback(n.Artist, in.StationName, "Unknown Artist")
	title := fallback(n.Title, in.TrackLabel, "Live Broadcast")
	lines = append(lines, keyStyle.Render("Track:  ")+accentStyle.Render(truncate(title, maxInt(8, width-8))))
	lines = append(lines, keyStyle.Render("Artist: ")+textStyle.Bold(true).Render(truncate(artist, maxInt(8, width-8))))

	if n.Album != "" || n.ReleaseYear != "" {
		albumStr := n.Album
		if albumStr != "" && n.ReleaseYear != "" {
			albumStr = fmt.Sprintf("%s (%s)", n.Album, n.ReleaseYear)
		} else if n.ReleaseYear != "" {
			albumStr = n.ReleaseYear
		}
		lines = append(lines, keyStyle.Render("Album:  ")+textStyle.Render(truncate(albumStr, maxInt(8, width-8))))
	}

	origin := n.OriginCity
	if origin != "" && n.Country != "" && !strings.EqualFold(origin, n.Country) {
		origin = fmt.Sprintf("%s, %s", origin, n.Country)
	} else if origin == "" {
		origin = fallback(n.Country, in.Country, "International")
	}
	genre := fallback(n.Genre, in.Genre, "Eclectic")
	lines = append(lines, keyStyle.Render("Origin: ")+textStyle.Render(truncate(origin+" • "+genre, maxInt(8, width-8))))

	if n.Label != "" {
		lines = append(lines, keyStyle.Render("Label:  ")+textStyle.Render(truncate(n.Label, maxInt(8, width-8))))
	}

	// Section 1: Artist Biography
	lines = append(lines, "")
	lines = append(lines, secStyle.Render(truncate("🎤 ARTIST BIOGRAPHY", width)))
	bio := strings.TrimSpace(n.Biography)
	if bio == "" {
		bio = fmt.Sprintf("%s is featured on %s.", artist, fallback(in.StationName, "halpradio", "halpradio"))
	}
	for _, wl := range wrapPlain(bio, width) {
		lines = append(lines, textStyle.Render(wl))
	}

	// Section 2: Album & Recording Context
	if strings.TrimSpace(n.AlbumNotes) != "" {
		lines = append(lines, "")
		lines = append(lines, secStyle.Render(truncate("💿 ALBUM & RECORDING CONTEXT", width)))
		for _, wl := range wrapPlain(n.AlbumNotes, width) {
			lines = append(lines, textStyle.Render(wl))
		}
	}

	// Section 3: Discography Highlights
	if len(n.Discography) > 0 {
		lines = append(lines, "")
		lines = append(lines, secStyle.Render(truncate("📀 DISCOGRAPHY HIGHLIGHTS", width)))
		for _, d := range n.Discography {
			yearTag := ""
			if d.Year != "" {
				yearTag = " (" + d.Year + ")"
			}
			typeTag := ""
			if d.Type != "" {
				typeTag = " [" + d.Type + "]"
			}
			entry := fmt.Sprintf("• %s%s%s", d.Title, yearTag, typeTag)
			lines = append(lines, textStyle.Render(truncate(entry, width)))
		}
	}

	// Section 4: Track Trivia & Broadcast Notes
	if len(n.TrackTrivia) > 0 {
		lines = append(lines, "")
		lines = append(lines, secStyle.Render(truncate("✨ TRACK TRIVIA & BROADCAST NOTES", width)))
		for _, triv := range n.TrackTrivia {
			wrapped := wrapPlain(triv, maxInt(8, width-2))
			for idx, wl := range wrapped {
				prefix := "• "
				if idx > 0 {
					prefix = "  "
				}
				lines = append(lines, mutedStyle.Render(prefix)+textStyle.Render(wl))
			}
		}
	}

	// Apply vertical scrolling
	scroll := in.Scroll
	maxScroll := len(lines) - height
	if maxScroll < 0 {
		maxScroll = 0
	}
	if scroll > maxScroll {
		scroll = maxScroll
	}
	if scroll < 0 {
		scroll = 0
	}

	out := make([]string, 0, height)
	for i := 0; i < height; i++ {
		idx := scroll + i
		if idx >= 0 && idx < len(lines) {
			out = append(out, lines[idx])
		} else {
			out = append(out, "")
		}
	}
	return out
}

func renderLinerNotesFooter(width int, th theme.Theme) []string {
	djStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Highlight)
	hintStyle := lipgloss.NewStyle().Foreground(th.Muted)

	return []string{
		"",
		djStyle.Render(truncate("🎙️ LOCAL AI DJ: [d] Intro • [D] Studio", width)),
		hintStyle.Render(truncate("j/k scroll • i/Esc close", width)),
	}
}

// RenderDJBanner renders the live broadcast banner above the player bar when the
// Local AI Radio DJ is announcing a track or smoothly fading volume back to 100%.
func RenderDJBanner(in DJBannerInput, th theme.Theme) string {
	width := in.Width
	if width <= 0 {
		width = 80
	}
	if width < 28 {
		width = 28
	}

	innerW := width - 4
	if innerW < 20 {
		innerW = 20
	}

	persona := strings.TrimSpace(in.Persona)
	if persona == "" {
		persona = "Late Night Chill"
	}
	engine := strings.TrimSpace(in.Engine)
	if engine == "" {
		engine = "Visual Broadcast (0ms Cloud Lag)"
	}
	script := strings.TrimSpace(in.Script)
	if script == "" {
		script = "You're locked into halpradio. Sit back and enjoy the flow."
	}
	duck := in.DuckPercent
	if duck <= 0 || duck > 100 {
		duck = 20
	}

	borderStyle := lipgloss.NewStyle().Foreground(th.Primary)
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Primary)
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Secondary)
	valStyle := lipgloss.NewStyle().Foreground(th.Foreground)
	quoteStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Highlight)
	duckStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Playing)

	// Top border: ┌─ 🎙️ LOCAL AI RADIO DJ ──────┐
	headerTitle := " 🎙️ LOCAL AI RADIO DJ "
	titleW := lipgloss.Width(headerTitle)
	dashes := width - 3 - titleW
	if dashes < 1 {
		dashes = 1
	}
	topBorder := borderStyle.Render("┌─") + titleStyle.Render(headerTitle) + borderStyle.Render(strings.Repeat("─", dashes)+"┐")
	if lipgloss.Width(topBorder) > width {
		topBorder = borderStyle.Render("┌" + strings.Repeat("─", maxInt(1, width-2)) + "┐")
	}

	metaRaw := fmt.Sprintf("Persona: %q  |  Engine: %s", persona, engine)
	metaTrimmed := truncate(metaRaw, innerW)
	line1 := labelStyle.Render("Persona: ") +
		valStyle.Render(truncate(fmt.Sprintf("%q", persona), innerW))
	if lipgloss.Width(metaRaw) <= innerW {
		line1 = labelStyle.Render("Persona: ") +
			valStyle.Render(fmt.Sprintf("%q", persona)) +
			borderStyle.Render("  |  ") +
			labelStyle.Render("Engine: ") +
			valStyle.Render(engine)
	} else {
		line1 = valStyle.Render(metaTrimmed)
	}

	speakPrefix := "Speaking: "
	maxQuoteW := innerW - len(speakPrefix) - 2
	if maxQuoteW < 10 {
		maxQuoteW = 10
	}
	line2 := labelStyle.Render(speakPrefix) + quoteStyle.Render("\""+truncate(script, maxQuoteW)+"\"")

	duckText := fmt.Sprintf("[Music Ducked: %d%% ──► Fading to 100%%]", duck)
	if !in.Fading && innerW >= 62 {
		duckText = fmt.Sprintf("[Music Ducked: %d%% ──► Fading to 100%% • Voice Overlay Active]", duck)
	}
	line3 := duckStyle.Render(truncate(duckText, innerW))

	wrapRow := func(content string) string {
		cw := lipgloss.Width(content)
		pad := innerW - cw
		if pad < 0 {
			pad = 0
		}
		return borderStyle.Render("│ ") + content + strings.Repeat(" ", pad) + borderStyle.Render(" │")
	}

	botBorder := borderStyle.Render("└" + strings.Repeat("─", maxInt(1, width-2)) + "┘")

	return strings.Join([]string{
		topBorder,
		wrapRow(line1),
		wrapRow(line2),
		wrapRow(line3),
		botBorder,
	}, "\n")
}

// RenderDJSettingsModal renders the interactive AI DJ Studio & ONNX Voice Manager modal (`D`).
func RenderDJSettingsModal(in DJSettingsModalInput, th theme.Theme) string {
	width := in.Width
	height := in.Height
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}

	boxWidth := 74
	if width < 78 {
		boxWidth = width - 4
	}
	if boxWidth < 40 {
		boxWidth = 40
	}
	innerW := boxWidth - 6
	if innerW < 30 {
		innerW = 30
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Primary)
	secStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Secondary)
	fgStyle := lipgloss.NewStyle().Foreground(th.Foreground)
	mutedStyle := lipgloss.NewStyle().Foreground(th.Muted)
	hiStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Highlight)
	onStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Playing)
	offStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Favorite)

	persona := ai.GetPersona(in.PersonaID)
	enginePref := strings.TrimSpace(in.TTSEngine)
	if enginePref == "" {
		enginePref = "auto"
	}
	activeEngine := strings.TrimSpace(in.ActiveEngineLabel)
	if activeEngine == "" {
		switch strings.ToLower(enginePref) {
		case "piper":
			activeEngine = "Piper (Local ONNX, 0ms Cloud Lag)"
		case "sherpa-onnx", "kokoro":
			activeEngine = "Sherpa-ONNX (Kokoro-82M Local)"
		case "native", "say", "espeak":
			activeEngine = "Native OS Speech (0ms Cloud Lag)"
		default:
			activeEngine = "Visual Broadcast (0ms Cloud Lag)"
		}
	}

	models := in.VoiceModels
	if len(models) == 0 {
		models = ai.DefaultVoiceModels()
	}

	cursor := in.Cursor
	totalItems := 3 + len(models)
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= totalItems {
		cursor = totalItems - 1
	}

	rowPrefix := func(idx int) string {
		if idx == cursor {
			return hiStyle.Render("❯ ")
		}
		return "  "
	}

	enabledBadge := offStyle.Render("[OFF]") + mutedStyle.Render(" (On-demand via [d])")
	if in.Enabled {
		enabledBadge = onStyle.Render("[ON ✓]") + fgStyle.Render(fmt.Sprintf(" (Auto-announce tracks • Duck to %d%%)", fallbackInt(in.DuckLevel, 20)))
	}
	row0 := rowPrefix(0) + secStyle.Render("Auto AI Radio DJ: ") + enabledBadge

	row1 := rowPrefix(1) + secStyle.Render("DJ Persona:       ") +
		hiStyle.Render(persona.Name) + mutedStyle.Render(" — "+truncate(persona.Tagline, maxInt(12, innerW-36)))

	row2 := rowPrefix(2) + secStyle.Render("TTS Engine Mode:  ") +
		hiStyle.Render(strings.ToUpper(enginePref)) + mutedStyle.Render(" ──► ") + onStyle.Render(truncate(activeEngine, maxInt(14, innerW-28)))

	var voiceRows []string
	maxVoiceVisible := len(models)
	if height <= 24 && maxVoiceVisible > 3 {
		maxVoiceVisible = 3
	}
	startIdx := 0
	if cursor >= 3 {
		modelCursor := cursor - 3
		if modelCursor >= maxVoiceVisible {
			startIdx = modelCursor - maxVoiceVisible + 1
		}
	}
	endIdx := startIdx + maxVoiceVisible
	if endIdx > len(models) {
		endIdx = len(models)
	}

	for i := startIdx; i < endIdx; i++ {
		m := models[i]
		itemIdx := 3 + i
		installed := false
		if in.InstalledModels != nil {
			installed = in.InstalledModels[m.ID]
		}
		isSelected := strings.EqualFold(in.SelectedVoiceID, m.ID)

		statusTag := mutedStyle.Render(fmt.Sprintf("[Download ~%s from HuggingFace]", m.SizeLabel))
		if installed {
			statusTag = onStyle.Render("[Installed ✓]")
		}
		activeMarker := ""
		if isSelected {
			activeMarker = hiStyle.Render(" ●")
		}
		line := fmt.Sprintf("%s%s %s%s",
			rowPrefix(itemIdx),
			fgStyle.Bold(itemIdx == cursor).Render(truncate(m.Name, maxInt(16, innerW-34))),
			statusTag,
			activeMarker,
		)
		voiceRows = append(voiceRows, truncateRendered(line, innerW))
	}

	var lines []string
	lines = append(lines, lipgloss.NewStyle().Width(innerW).Align(lipgloss.Center).Render(titleStyle.Render("🎙️ LOCAL AI RADIO DJ STUDIO & ONNX VOICES")))
	if height > 23 {
		lines = append(lines, "")
	}
	lines = append(lines, truncateRendered(row0, innerW))
	lines = append(lines, truncateRendered(row1, innerW))
	lines = append(lines, truncateRendered(row2, innerW))

	if height > 22 {
		lines = append(lines, "")
	}
	lines = append(lines, secStyle.Render(truncate("📦 OPT-IN LOCAL ONNX VOICE MODELS (HuggingFace):", innerW)))
	lines = append(lines, voiceRows...)

	if in.Status != "" {
		if height > 24 {
			lines = append(lines, "")
		}
		lines = append(lines, onStyle.Render(truncate(in.Status, innerW)))
	}

	if height > 23 {
		lines = append(lines, "")
	}
	helpLine := "[j/k] Move  [Enter] Toggle/Select  [d] Download ONNX  [t] Test DJ  [Esc] Close"
	if innerW < 68 {
		helpLine = "[j/k] Move  [Enter] Select  [d] Download  [t] Test  [Esc] Close"
	}
	lines = append(lines, mutedStyle.Render(truncate(helpLine, innerW)))

	padY := 1
	if height <= 25 {
		padY = 0
	}

	modalBox := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(th.Primary).
		Padding(padY, 1).
		Width(boxWidth).
		Render(strings.Join(lines, "\n"))

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, modalBox)
}

func truncate(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxW {
		return s
	}
	runes := []rune(s)
	if maxW <= 1 {
		return string(runes[:1])
	}
	var b strings.Builder
	for _, r := range runes {
		if lipgloss.Width(b.String()+string(r))+1 > maxW {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
}

func truncateRendered(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	return lipgloss.NewStyle().MaxWidth(maxW).Render(s)
}

func centerLine(line string, width int) string {
	w := lipgloss.Width(line)
	if w >= width {
		return line
	}
	left := (width - w) / 2
	return strings.Repeat(" ", left) + line
}

func wrapPlain(s string, width int) []string {
	if width < 4 {
		width = 4
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	curr := ""
	for _, w := range words {
		if curr == "" {
			curr = w
		} else if lipgloss.Width(curr)+1+lipgloss.Width(w) <= width {
			curr += " " + w
		} else {
			out = append(out, curr)
			curr = w
		}
	}
	if curr != "" {
		out = append(out, curr)
	}
	return out
}

func fallback(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func fallbackInt(val, def int) int {
	if val <= 0 {
		return def
	}
	return val
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
