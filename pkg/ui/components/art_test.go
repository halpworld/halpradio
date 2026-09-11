package components

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/art"
	"github.com/halpworld/halpradio/pkg/theme"
)

func TestRenderAlbumArtModal_WithArtwork(t *testing.T) {
	lines := []string{
		strings.Repeat("▀", 24),
		strings.Repeat("▀", 24),
		strings.Repeat("▀", 24),
	}
	in := AlbumArtModalInput{
		Cover:      &art.Cover{Source: "iTunes", Album: "Dive", Artist: "Tycho", Title: "A Walk"},
		Lines:      lines,
		Protocol:   art.ProtocolHalfBlock,
		TrackLabel: "Tycho - A Walk",
		Width:      100,
		Height:     30,
	}
	out := RenderAlbumArtModal(in, theme.GetTheme("tokyonight"))

	if !strings.Contains(out, "ALBUM ART") {
		t.Errorf("expected the modal title, got:\n%s", out)
	}
	if !strings.Contains(out, "Tycho - A Walk") {
		t.Errorf("expected the track label, got:\n%s", out)
	}
	if !strings.Contains(out, "Dive") || !strings.Contains(out, "iTunes") {
		t.Errorf("expected album and provider metadata, got:\n%s", out)
	}
	if !strings.Contains(out, art.ProtocolHalfBlock.Label()) {
		t.Errorf("expected the protocol badge, got:\n%s", out)
	}
	if got := lipgloss.Width(out); got != 100 {
		t.Errorf("modal placed at %d columns, want 100", got)
	}
	if got := lipgloss.Height(out); got != 30 {
		t.Errorf("modal placed at %d rows, want 30", got)
	}
}

func TestRenderAlbumArtModal_EmptyStates(t *testing.T) {
	th := theme.GetTheme("nord")

	fetching := RenderAlbumArtModal(AlbumArtModalInput{
		Fetching: true,
		Protocol: art.ProtocolKitty,
		Width:    90,
		Height:   28,
	}, th)
	if !strings.Contains(fetching, "Fetching cover art") {
		t.Errorf("expected a fetching hint, got:\n%s", fetching)
	}

	missing := RenderAlbumArtModal(AlbumArtModalInput{
		Status:   "No cover art found for this track",
		Protocol: art.ProtocolBraille,
		Width:    90,
		Height:   28,
	}, th)
	if !strings.Contains(missing, "No cover art found") {
		t.Errorf("expected the status message, got:\n%s", missing)
	}
}

func TestRenderAlbumArtModal_NarrowTerminal(t *testing.T) {
	out := RenderAlbumArtModal(AlbumArtModalInput{
		Protocol: art.ProtocolHalfBlock,
		Width:    40,
		Height:   12,
	}, theme.GetTheme("catppuccin"))
	if got := lipgloss.Width(out); got != 40 {
		t.Errorf("modal placed at %d columns, want 40", got)
	}
}
