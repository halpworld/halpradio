package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/art"
	"github.com/halpworld/halpradio/pkg/theme"
)

// AlbumArtModalInput bundles the state the album art modal renders.
type AlbumArtModalInput struct {
	Cover      *art.Cover
	Lines      []string
	Status     string
	Fetching   bool
	Protocol   art.Protocol
	TrackLabel string
	Width      int
	Height     int
}

// RenderAlbumArtModal draws the floating cover art viewer opened with A. The
// artwork lines are rasterised in the update loop, so this stays a pure view.
func RenderAlbumArtModal(in AlbumArtModalInput, th theme.Theme) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Primary).Align(lipgloss.Center)
	trackStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Foreground)
	metaStyle := lipgloss.NewStyle().Foreground(th.Secondary)
	protoStyle := lipgloss.NewStyle().Foreground(th.BadgeText).Background(th.Badge).Bold(true).Padding(0, 1)
	hintStyle := lipgloss.NewStyle().Foreground(th.Muted)
	infoStyle := lipgloss.NewStyle().Foreground(th.Highlight).Italic(true)

	artWidth := 0
	for _, line := range in.Lines {
		if w := lipgloss.Width(line); w > artWidth {
			artWidth = w
		}
	}

	boxWidth := artWidth + 6
	if boxWidth < 44 {
		boxWidth = 44
	}
	if boxWidth > in.Width-4 {
		boxWidth = in.Width - 4
	}
	if boxWidth < 24 {
		boxWidth = 24
	}
	innerW := boxWidth - 4

	parts := []string{titleStyle.Width(innerW).Render("🖼  ALBUM ART"), ""}

	if len(in.Lines) > 0 {
		for _, line := range in.Lines {
			parts = append(parts, centerLine(line, innerW))
		}
	} else {
		msg := in.Status
		if msg == "" {
			if in.Fetching {
				msg = "Fetching cover art…"
			} else {
				msg = "No cover art available for this track"
			}
		}
		for _, row := range wrapPlain(msg, innerW) {
			parts = append(parts, infoStyle.Render(row))
		}
	}

	parts = append(parts, "")
	if in.TrackLabel != "" {
		parts = append(parts, trackStyle.Render(truncate(in.TrackLabel, innerW)))
	}
	if in.Cover != nil {
		meta := in.Cover.Source
		if in.Cover.Album != "" {
			meta = fmt.Sprintf("%s • %s", in.Cover.Album, in.Cover.Source)
		}
		parts = append(parts, metaStyle.Render(truncate(meta, innerW)))
	}

	badge := protoStyle.Render(in.Protocol.Label())
	parts = append(parts, "", badge)
	hint := "[ A ] / [ Esc ] close · [ L ] lyrics"
	if innerW < lipgloss.Width(hint) {
		hint = "[ Esc ] close"
	}
	parts = append(parts, hintStyle.Render(hint))

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.Border).
		Padding(1, 1).
		Width(boxWidth)

	return lipgloss.Place(
		in.Width,
		in.Height,
		lipgloss.Center,
		lipgloss.Center,
		boxStyle.Render(strings.Join(parts, "\n")),
	)
}
