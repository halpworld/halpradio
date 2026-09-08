package components

import (
	"strings"
	"testing"
	"time"

	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
)

func TestRenderPartyBar(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	viz := NewVisualizer("dj-cat")
	viz.Tick()

	st := &radio.Station{
		ID:   "somafm-groove",
		Name: "SomaFM Groove Salad",
	}

	reactions := []party.FloatingReaction{
		{ID: "1", Emoji: "🔥", Sender: "alice", CreatedAt: time.Now()},
		{ID: "2", Emoji: "❤️", Sender: "bob", CreatedAt: time.Now()},
		{ID: "3", Emoji: "☕", Sender: "kenth", CreatedAt: time.Now()},
	}

	out := RenderPartyBar(
		"8X2K9P",
		"team-focus",
		"arkalon76",
		true,
		party.DJPassHostOnly,
		4,
		st,
		"Groove Salad Chill",
		player.StatusPlaying,
		75,
		false,
		viz,
		reactions,
		nil,
		80,
		th,
		false,
		"",
	)

	if !strings.Contains(out, "8X2K9P") {
		t.Errorf("expected party bar to contain room code 8X2K9P, got:\n%s", out)
	}
	if !strings.Contains(out, "4 Listeners") {
		t.Errorf("expected party bar to show '4 Listeners'")
	}
	if !strings.Contains(out, "arkalon76") {
		t.Errorf("expected party bar to show host 'arkalon76'")
	}
	if !strings.Contains(out, "🔥") || !strings.Contains(out, "alice") {
		t.Errorf("expected party bar to contain reaction 🔥 (alice)")
	}
	if !strings.Contains(out, "Ctrl+p") {
		t.Errorf("expected party bar to contain keymap hints")
	}
}

func TestRenderPartyBarChatMode(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	viz := NewVisualizer("dj-cat")

	out := RenderPartyBar(
		"8X2K9P",
		"team-focus",
		"arkalon76",
		false,
		party.DJPassOpenDemocracy,
		2,
		nil,
		"",
		player.StatusStopped,
		50,
		false,
		viz,
		nil,
		nil,
		80,
		th,
		true,
		"Hello teammates!",
	)

	if !strings.Contains(out, "Chat Ping:") || !strings.Contains(out, "Hello teammates!") {
		t.Errorf("expected chat prompt to be rendered, got:\n%s", out)
	}
}
