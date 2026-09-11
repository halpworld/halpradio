package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/lyrics"
	"github.com/halpworld/halpradio/pkg/theme"
)

// LyricsDrawerInput bundles everything the lyrics drawer renders. It is passed
// by value so the view stays a pure function of model state.
type LyricsDrawerInput struct {
	Sheet      *lyrics.Sheet
	Status     string
	Fetching   bool
	TrackLabel string
	ArtLines   []string
	ArtCols    int
	ArtSource  string
	Elapsed    time.Duration
	Offset     time.Duration
	Scroll     int
	Focused    bool
	Width      int
	Height     int
}

// displayRow is one wrapped screen row of a lyric sheet, tracking which source
// line it came from so the active line can still be located after wrapping.
type displayRow struct {
	srcIdx int
	text   string
	cont   bool
	last   bool
}

// RenderLyricsDrawer draws the side drawer holding album art and the live
// lyric sheet. Synced sheets auto-scroll around the active line; unsynced
// sheets scroll manually from in.Scroll.
func RenderLyricsDrawer(in LyricsDrawerInput, th theme.Theme) string {
	borderColor := th.Border
	if in.Focused {
		borderColor = th.Primary
	}
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Padding(0, 1).
		Width(in.Width - 2).
		Height(in.Height - 2)

	innerW := in.Width - 4
	if innerW < 10 {
		innerW = 10
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Primary)
	mutedStyle := lipgloss.NewStyle().Foreground(th.Muted)
	trackStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Secondary)

	var head []string
	head = append(head, titleStyle.Render(truncate("📜 LIVE LYRICS", innerW)))

	if len(in.ArtLines) > 0 {
		head = append(head, "")
		for _, line := range in.ArtLines {
			head = append(head, centerLine(line, innerW))
		}
		if in.ArtSource != "" {
			head = append(head, centerLine(mutedStyle.Render(truncate("🖼 "+in.ArtSource, innerW)), innerW))
		}
	}

	if in.TrackLabel != "" {
		head = append(head, "")
		head = append(head, trackStyle.Render(truncate(in.TrackLabel, innerW)))
	}

	foot := lyricsFooter(in, innerW, th)

	// Everything left over after the header and footer belongs to the sheet.
	bodyHeight := (in.Height - 2) - len(head) - len(foot)
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	body := renderLyricsBody(in, innerW, bodyHeight, th)

	all := append([]string{}, head...)
	all = append(all, body...)
	all = append(all, foot...)
	return boxStyle.Render(strings.Join(all, "\n"))
}

// renderLyricsBody produces exactly height rows of lyric text.
func renderLyricsBody(in LyricsDrawerInput, width, height int, th theme.Theme) []string {
	activeStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Playing)
	nearStyle := lipgloss.NewStyle().Foreground(th.Foreground)
	farStyle := lipgloss.NewStyle().Foreground(th.Muted)
	infoStyle := lipgloss.NewStyle().Foreground(th.Highlight).Italic(true)

	if in.Sheet == nil || in.Sheet.IsEmpty() {
		msg := in.Status
		if msg == "" {
			if in.Fetching {
				msg = "Searching for lyrics…"
			} else {
				msg = "No lyrics loaded"
			}
		}
		rows := wrapPlain(msg, width)
		out := make([]string, 0, height)
		pad := (height - len(rows)) / 2
		for i := 0; i < pad && len(out) < height; i++ {
			out = append(out, "")
		}
		for _, r := range rows {
			if len(out) >= height {
				break
			}
			out = append(out, infoStyle.Render(r))
		}
		for len(out) < height {
			out = append(out, "")
		}
		return out
	}

	// Reserve the last row for the line progress gauge on synced sheets.
	gaugeRows := 0
	if in.Sheet.Synced && height >= 4 {
		gaugeRows = 2
	}
	textHeight := height - gaugeRows
	if textHeight < 1 {
		textHeight = 1
		gaugeRows = height - 1
		if gaugeRows < 0 {
			gaugeRows = 0
		}
	}

	// The active-line marker costs two columns on each side.
	rows := wrapSheet(in.Sheet, width-4)
	if len(rows) == 0 {
		return make([]string, height)
	}

	activeIdx := -1
	if in.Sheet.Synced {
		activeIdx = in.Sheet.ActiveIndex(in.Elapsed)
	}

	anchor := 0
	if activeIdx >= 0 {
		anchor = firstRowFor(rows, activeIdx)
	} else {
		anchor = firstRowFor(rows, in.Scroll)
	}

	start := anchor - textHeight/2
	if in.Sheet.Synced {
		// Keep the active line a third of the way down so upcoming lines
		// stay visible.
		start = anchor - textHeight/3
	} else {
		start = anchor
	}
	if start > len(rows)-textHeight {
		start = len(rows) - textHeight
	}
	if start < 0 {
		start = 0
	}

	out := make([]string, 0, height)
	for i := 0; i < textHeight; i++ {
		idx := start + i
		if idx < 0 || idx >= len(rows) {
			out = append(out, "")
			continue
		}
		row := rows[idx]
		switch {
		case activeIdx >= 0 && row.srcIdx == activeIdx:
			marker := "► "
			if row.cont {
				marker = "  "
			}
			tail := ""
			if row.last {
				tail = " ◄"
			}
			out = append(out, activeStyle.Render(marker+row.text+tail))
		case activeIdx >= 0 && absInt(row.srcIdx-activeIdx) == 1:
			out = append(out, "  "+nearStyle.Render(row.text))
		default:
			out = append(out, "  "+farStyle.Render(row.text))
		}
	}

	if gaugeRows > 0 {
		out = append(out, "")
		progress := 0.0
		if activeIdx >= 0 {
			progress = in.Sheet.Progress(activeIdx, in.Elapsed)
		}
		out = append(out, lyricGauge(progress, width, th))
		for len(out) < height {
			out = append(out, "")
		}
	}
	for len(out) < height {
		out = append(out, "")
	}
	return out[:height]
}

// lyricGauge draws the elapsed share of the active lyric line.
func lyricGauge(progress float64, width int, th theme.Theme) string {
	if width < 4 {
		return ""
	}
	if progress < 0 {
		progress = 0
	}
	if progress > 1 {
		progress = 1
	}
	filled := int(progress * float64(width))
	if filled > width {
		filled = width
	}
	on := lipgloss.NewStyle().Foreground(th.Playing)
	off := lipgloss.NewStyle().Foreground(th.Border)
	return on.Render(strings.Repeat("━", filled)) + off.Render(strings.Repeat("─", width-filled))
}

// lyricsFooter renders the provenance and key hints at the base of the drawer.
func lyricsFooter(in LyricsDrawerInput, width int, th theme.Theme) []string {
	sourceStyle := lipgloss.NewStyle().Foreground(th.Highlight)
	hintStyle := lipgloss.NewStyle().Foreground(th.Muted)

	var provenance string
	switch {
	case in.Sheet != nil && in.Sheet.Instrumental:
		provenance = fmt.Sprintf("🎼 Instrumental • %s", in.Sheet.Source)
	case in.Sheet != nil && in.Sheet.Synced:
		provenance = fmt.Sprintf("⏱ Synced via %s", in.Sheet.Source)
		if in.Offset != 0 {
			provenance += fmt.Sprintf(" (%s)", signedSeconds(in.Offset))
		}
	case in.Sheet != nil:
		provenance = fmt.Sprintf("📄 Unsynced via %s", in.Sheet.Source)
	case in.Fetching:
		provenance = "⟳ Querying LRCLIB…"
	default:
		provenance = "— no sheet —"
	}

	hint := "L close · , . sync"
	if in.Sheet != nil && !in.Sheet.Synced {
		hint = "j/k scroll · L close"
	}

	return []string{
		"",
		sourceStyle.Render(truncate(provenance, width)),
		hintStyle.Render(truncate(hint, width)),
	}
}

// signedSeconds formats a sync offset with an explicit sign.
func signedSeconds(d time.Duration) string {
	secs := d.Round(100 * time.Millisecond).Seconds()
	if secs >= 0 {
		return fmt.Sprintf("+%.1fs", secs)
	}
	return fmt.Sprintf("%.1fs", secs)
}

// wrapSheet expands every lyric line into the display rows it needs at the
// given width, preserving the source index of each row.
func wrapSheet(sheet *lyrics.Sheet, width int) []displayRow {
	if width < 4 {
		width = 4
	}
	var rows []displayRow
	for i, line := range sheet.Lines {
		text := strings.TrimSpace(line.Text)
		if text == "" {
			rows = append(rows, displayRow{srcIdx: i, text: "", last: true})
			continue
		}
		chunks := wrapPlain(text, width)
		for j, chunk := range chunks {
			rows = append(rows, displayRow{
				srcIdx: i,
				text:   chunk,
				cont:   j > 0,
				last:   j == len(chunks)-1,
			})
		}
	}
	return rows
}

// firstRowFor returns the index of the first display row belonging to the
// given source line, clamped into range.
func firstRowFor(rows []displayRow, srcIdx int) int {
	if srcIdx <= 0 {
		return 0
	}
	for i, r := range rows {
		if r.srcIdx >= srcIdx {
			return i
		}
	}
	if len(rows) == 0 {
		return 0
	}
	return len(rows) - 1
}

// wrapPlain word-wraps s to width columns, breaking overlong words.
func wrapPlain(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	var out []string
	current := ""
	for _, w := range words {
		for lipgloss.Width(w) > width {
			// Break a word that cannot fit on any line.
			if current != "" {
				out = append(out, current)
				current = ""
			}
			head, tail := splitAtWidth(w, width)
			out = append(out, head)
			w = tail
		}
		switch {
		case current == "":
			current = w
		case lipgloss.Width(current)+1+lipgloss.Width(w) <= width:
			current += " " + w
		default:
			out = append(out, current)
			current = w
		}
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}

// splitAtWidth cuts s after width display columns.
func splitAtWidth(s string, width int) (string, string) {
	runes := []rune(s)
	for i := range runes {
		if lipgloss.Width(string(runes[:i+1])) > width {
			if i == 0 {
				return string(runes[:1]), string(runes[1:])
			}
			return string(runes[:i]), string(runes[i:])
		}
	}
	return s, ""
}

// centerLine pads a pre-rendered line so it sits centred in width columns.
// It measures with lipgloss so terminal image escape sequences, which have no
// display width, keep the padding the renderer already baked in.
func centerLine(line string, width int) string {
	w := lipgloss.Width(line)
	if w >= width {
		return line
	}
	left := (width - w) / 2
	return strings.Repeat(" ", left) + line
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
