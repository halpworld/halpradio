package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
)

// RenderPartyBar renders the synchronized terminal party room player bar with
// animal DJ visualizer, floating ASCII reactions, and mini-chat.
func RenderPartyBar(
	roomCode string,
	roomName string,
	hostNick string,
	isHost bool,
	djPass party.DJPassMode,
	listenerCount int,
	currStation *radio.Station,
	currTrack string,
	status player.PlayStatus,
	volume int,
	isMuted bool,
	viz *Visualizer,
	reactions []party.FloatingReaction,
	recentChat []party.ChatMessage,
	width int,
	th theme.Theme,
	isChatting bool,
	chatInput string,
) string {
	if width < 30 {
		width = 30
	}

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.Border).
		Padding(0, 1).
		Width(width - 2)

	innerW := width - 6
	if innerW < 20 {
		innerW = 20
	}

	// 1. Top Header Line: Room Code & Listeners & Volume
	roomBadge := fmt.Sprintf("👥 TERMINAL PARTY ROOM: %s (%d Listeners)", party.FormatRoomCode(roomCode), listenerCount)
	roomStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Primary)

	var volText string
	if isMuted {
		volText = lipgloss.NewStyle().Foreground(th.Favorite).Bold(true).Render("[ 🔇 MUTED ]")
	} else {
		barLen := 8
		filled := (volume * barLen) / 100
		empty := barLen - filled
		gauge := strings.Repeat("█", filled) + strings.Repeat("░", empty)
		volText = fmt.Sprintf("🔊 [%s] %d%%", gauge, volume)
	}
	volStyle := lipgloss.NewStyle().Foreground(th.Highlight)

	renderedRoom := roomStyle.Render(roomBadge)
	renderedVol := volStyle.Render(volText)

	spaceTop := innerW - lipgloss.Width(renderedRoom) - lipgloss.Width(renderedVol)
	if spaceTop < 1 {
		spaceTop = 1
	}
	lineTop := renderedRoom + strings.Repeat(" ", spaceTop) + renderedVol

	// 2. Host & Station Info
	hostDisplay := "@" + hostNick
	if isHost {
		hostDisplay += " (You 👑)"
	}
	hostStyle := lipgloss.NewStyle().Foreground(th.Secondary).Bold(true)

	djPassBadge := "[DJ: Host Only]"
	if djPass == party.DJPassOpenDemocracy {
		djPassBadge = "[DJ: Open Democracy]"
	}
	djPassStyle := lipgloss.NewStyle().Foreground(th.Playing)

	stName := "No Station Playing"
	if currStation != nil {
		stName = currStation.Name
	}
	stStyle := lipgloss.NewStyle().Bold(true).Foreground(th.Foreground)

	lineInfoLeft := fmt.Sprintf("Host: %s  |  Station: %s  %s",
		hostStyle.Render(hostDisplay),
		stStyle.Render(stName),
		djPassStyle.Render(djPassBadge),
	)

	// Truncate info line if needed
	lineInfo := truncate(lineInfoLeft, innerW)

	// 3. Middle Line: Animal DJ Visualizer + Floating ASCII Reactions
	isPlaying := (status == player.PlayStatus(player.StatusPlaying))
	vizWidth := 34
	if width < 70 {
		vizWidth = 22
	}
	vizRendered := ""
	if viz != nil {
		vizRendered = viz.Render(isPlaying, vizWidth, th)
	}

	// Floating reactions string: 🔥 (alice)   ❤️ (bob)   ☕ (kenth)
	var reactionTokens []string
	now := time.Now()
	for _, r := range reactions {
		age := now.Sub(r.CreatedAt)
		var senderStyle lipgloss.Style
		var emojiStyle lipgloss.Style

		if age < 1500*time.Millisecond {
			// Fresh reaction: bold and vibrant
			emojiStyle = lipgloss.NewStyle().Bold(true)
			senderStyle = lipgloss.NewStyle().Foreground(th.Playing).Bold(true)
		} else if age < 2500*time.Millisecond {
			// Floating upward: secondary
			emojiStyle = lipgloss.NewStyle()
			senderStyle = lipgloss.NewStyle().Foreground(th.Secondary)
		} else {
			// Fading out
			emojiStyle = lipgloss.NewStyle().Foreground(th.Muted)
			senderStyle = lipgloss.NewStyle().Foreground(th.Muted)
		}

		tok := fmt.Sprintf("%s %s", emojiStyle.Render(r.Emoji), senderStyle.Render("("+r.Sender+")"))
		reactionTokens = append(reactionTokens, tok)
	}

	reactionsArea := strings.Join(reactionTokens, "   ")
	availForReactions := innerW - lipgloss.Width(vizRendered) - 2
	if availForReactions > 5 && reactionsArea != "" {
		reactionsArea = truncate(reactionsArea, availForReactions)
	}

	lineMiddle := vizRendered
	if reactionsArea != "" {
		lineMiddle = vizRendered + "   " + reactionsArea
	}

	// 4. Bottom Line: Chat preview or reaction legend
	var lineBottom string
	if isChatting {
		promptStyle := lipgloss.NewStyle().Foreground(th.Playing).Bold(true)
		inputStyle := lipgloss.NewStyle().Foreground(th.BadgeText).Background(th.Highlight)
		lineBottom = promptStyle.Render("💬 Chat Ping: ") + inputStyle.Render(chatInput+"█") +
			lipgloss.NewStyle().Foreground(th.Muted).Render("  (Enter to send, Esc to cancel)")
	} else if len(recentChat) > 0 && time.Since(recentChat[len(recentChat)-1].Timestamp) < 6*time.Second {
		// Show active chat bubble if message arrived within 6 seconds
		lastMsg := recentChat[len(recentChat)-1]
		chatBubble := fmt.Sprintf("💬 @%s: %s", lastMsg.Sender, lastMsg.Message)
		chatStyle := lipgloss.NewStyle().Foreground(th.Highlight).Bold(true)
		legendStyle := lipgloss.NewStyle().Foreground(th.Muted)
		lineBottom = chatStyle.Render(truncate(chatBubble, innerW/2)) + "  " +
			legendStyle.Render("[1:🔥 2:❤️ 3:☕ 4:🚀 5:👀 | t: Chat | Ctrl+p: Menu]")
	} else {
		legendStyle := lipgloss.NewStyle().Foreground(th.Muted)
		keyStyle := lipgloss.NewStyle().Foreground(th.Secondary).Bold(true)

		lineBottom = legendStyle.Render("[Reactions: ") +
			keyStyle.Render("1") + legendStyle.Render(":🔥 ") +
			keyStyle.Render("2") + legendStyle.Render(":❤️ ") +
			keyStyle.Render("3") + legendStyle.Render(":☕ ") +
			keyStyle.Render("4") + legendStyle.Render(":🚀 ") +
			keyStyle.Render("5") + legendStyle.Render(":👀 | ") +
			keyStyle.Render("t") + legendStyle.Render(": Chat ping | ") +
			keyStyle.Render("Ctrl+p") + legendStyle.Render(": Party Menu]")
	}

	return boxStyle.Render(lipgloss.JoinVertical(
		lipgloss.Left,
		lineTop,
		lineInfo,
		lineMiddle,
		lineBottom,
	))
}
