package components

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
	"github.com/halpworld/halpradio/pkg/util"
)

// nickBrackets removes square brackets, and look-alikes, from nicknames.
var nickBrackets = strings.NewReplacer("[", "", "]", "", "［", "", "］", "", "⟦", "", "⟧", "", "〚", "", "〛", "", "⁅", "", "⁆", "")

// partyNickMax is the longest nickname shown in a peer label.
const partyNickMax = 24

// fingerprintSymbol reports whether r counts as a fingerprint symbol for
// party.ContainsFingerprint (it matches case-insensitively, so a few
// non-ASCII letters that upper-case to ASCII count too).
func fingerprintSymbol(r rune) bool {
	return party.ContainsFingerprint(strings.Repeat(string(r), party.FingerprintLen))
}

// fingerprintSeparator is skipped by party.ContainsFingerprint inside a run.
func fingerprintSeparator(r rune) bool { return r == '-' || unicode.IsSpace(r) }

// StripFingerprintRuns replaces every part of s that party.ContainsFingerprint
// would read as a fingerprint (16 or more alphabet symbols, ignoring case,
// spaces and dashes) with "…", so a nickname cannot carry a fingerprint of
// its own.
func StripFingerprintRuns(s string) string {
	if !party.ContainsFingerprint(s) {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); {
		if !fingerprintSymbol(rs[i]) {
			b.WriteRune(rs[i])
			i++
			continue
		}
		// A run starts at a symbol and goes on through symbols and
		// separators; trailing separators are not part of it.
		j, last := i, i
		for j < len(rs) && (fingerprintSymbol(rs[j]) || fingerprintSeparator(rs[j])) {
			if fingerprintSymbol(rs[j]) {
				last = j
			}
			j++
		}
		run := string(rs[i : last+1])
		if party.ContainsFingerprint(run) {
			b.WriteString("…")
		} else {
			b.WriteString(run)
		}
		i = last + 1
	}
	return b.String()
}

// CleanPartyNick is a peer nickname as it may be shown: printable, without
// square brackets (or look-alikes) or fingerprint-like runs, at most
// partyNickMax characters, never empty.
func CleanPartyNick(nick string) string {
	n := nickBrackets.Replace(util.SanitizeDisplay(nick, 256))
	n = StripFingerprintRuns(n)
	n = strings.TrimSpace(util.SanitizeDisplay(n, partyNickMax))
	if n == "" {
		n = "a peer"
	}
	return n
}

// PartyVerifiedSuffix follows the host label when the host was verified by
// the fingerprint in the invite the user joined with.
const PartyVerifiedSuffix = " ✓ verified by invite"

// PartyHostLabel is the sanitized "[XXXX-XXXX-XXXX-XXXX] @nick" label for a
// party peer. The fingerprint identifies the peer's key, so guests can check
// that the host they follow is the one on the host's own screen. It comes
// first and is never cut; the nickname is cleaned by CleanPartyNick so it
// cannot imitate a fingerprint, and it is the part that gets shortened. An
// empty fingerprint gives "@nick" alone.
func PartyHostLabel(nick, fingerprint string) string {
	label := "@" + CleanPartyNick(nick)
	// The fingerprint is never shortened.
	if fp := util.SanitizeDisplay(fingerprint, 64); fp != "" {
		label = "[" + fp + "] " + label
	}
	return label
}

// SessionHostLabel is the label of the host sess follows: with
// PartyVerifiedSuffix when the invite's pin verified it. On the host it is
// the user's own label.
func SessionHostLabel(sess *party.PartySession) string {
	if sess == nil {
		return PartyHostLabel("", "")
	}
	if sess.IsHost() {
		return PartyHostLabel(sess.Nickname(), sess.Fingerprint())
	}
	label := PartyHostLabel(sess.HostNickname(), sess.HostFingerprint())
	if sess.HostPinned() {
		label += PartyVerifiedSuffix
	}
	return label
}

// FitPartyHostLabel shortens a PartyHostLabel (optionally ending in
// PartyVerifiedSuffix) to maxW cells. The nickname is shortened first, then
// the verified text becomes " ✓", then the nickname is dropped; the
// fingerprint is never cut.
func FitPartyHostLabel(label string, maxW int) string {
	if lipgloss.Width(label) <= maxW {
		return label
	}
	fp, rest := "", label
	if strings.HasPrefix(label, "[") {
		if i := strings.Index(label, "] @"); i > 0 {
			fp, rest = label[:i+2], label[i+2:]
		}
	}
	suffix := ""
	if strings.HasSuffix(rest, PartyVerifiedSuffix) {
		suffix, rest = PartyVerifiedSuffix, strings.TrimSuffix(rest, PartyVerifiedSuffix)
	}
	if fp == "" {
		return truncate(rest, max(maxW-lipgloss.Width(suffix), 1)) + suffix
	}
	for _, sfx := range []string{suffix, strings.TrimSuffix(suffix, " verified by invite")} {
		room := maxW - lipgloss.Width(fp) - lipgloss.Width(sfx)
		if lipgloss.Width(rest) <= room {
			return fp + rest + sfx
		}
		if room >= 4 { // "@" + one rune + "..."
			return fp + truncate(rest, room) + sfx
		}
	}
	return strings.TrimSpace(fp) + strings.TrimSuffix(suffix, " verified by invite")
}

// isPartyHostLabel reports whether s is already a PartyHostLabel.
func isPartyHostLabel(s string) bool {
	return strings.HasPrefix(s, "@") || (strings.HasPrefix(s, "[") && strings.Contains(s, "] @"))
}

// PartyHostCheckHint is shown next to the host's own fingerprint.
const PartyHostCheckHint = "tell guests to check this"

// PartyManualCheckHint follows a host label that no invite verified.
const PartyManualCheckHint = "check it matches the fingerprint on the host's screen"

// PartyInviteNote is said once wherever the invite is shared.
const PartyInviteNote = "Share the invite: it also pins the host, so guests need no fingerprint check."

// RenderPartyBar renders the synchronized terminal party room player bar with
// animal DJ visualizer, floating ASCII reactions, and mini-chat. invite is
// what guests join with (PartySession.Invite: the room code and the host's
// fingerprint).
func RenderPartyBar(
	invite string,
	roomName string,
	hostLabel string, // from PartyHostLabel
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

	// 1. Top Header Line: Invite & Listeners & Volume
	invite = util.SanitizeDisplay(invite, 64)
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

	renderedVol := volStyle.Render(volText)

	// The invite is never cut: the volume, then the wording, give way first.
	var lineTop string
	for _, badge := range []string{
		fmt.Sprintf("👥 TERMINAL PARTY ROOM · Invite: %s (%d Listeners)", invite, listenerCount),
		fmt.Sprintf("👥 Invite: %s (%d)", invite, listenerCount),
		"👥 Invite: " + invite,
		invite,
	} {
		renderedRoom := roomStyle.Render(badge)
		if space := innerW - lipgloss.Width(renderedRoom) - lipgloss.Width(renderedVol); space >= 1 {
			lineTop = renderedRoom + strings.Repeat(" ", space) + renderedVol
			break
		}
		if lipgloss.Width(renderedRoom) <= innerW {
			lineTop = renderedRoom
			break
		}
	}
	if lineTop == "" {
		// Too narrow for the whole invite: show the room code; the party
		// manager (Ctrl+p) shows the invite.
		code, _, _ := strings.Cut(invite, "/")
		lineTop = roomStyle.Render(truncate(code, innerW))
	}

	// 2. Host & Station Info
	hostDisplay := hostLabel
	if !isPartyHostLabel(hostDisplay) {
		hostDisplay = PartyHostLabel(hostLabel, "")
	}
	hostPrefix := "Host: "
	if lipgloss.Width(hostPrefix)+lipgloss.Width(hostDisplay) > innerW && innerW < 30 {
		hostPrefix = ""
	}
	hostRoom := innerW - lipgloss.Width(hostPrefix)
	if isHost {
		if own := hostDisplay + " (You 👑, " + PartyHostCheckHint + ")"; lipgloss.Width(own) <= hostRoom {
			hostDisplay = own
		} else {
			hostDisplay = FitPartyHostLabel(hostDisplay, hostRoom-lipgloss.Width(" (You 👑)")) + " (You 👑)"
		}
	}
	hostDisplay = FitPartyHostLabel(hostDisplay, hostRoom)
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

	// The host label is fitted above; only the station part is cut.
	lineInfo := hostPrefix + hostStyle.Render(hostDisplay)
	if rest := innerW - lipgloss.Width(lineInfo); rest > 0 {
		tail := fmt.Sprintf("  |  Station: %s  %s", stStyle.Render(stName), djPassStyle.Render(djPassBadge))
		if lipgloss.Width(tail) > rest {
			tail = truncate(tail, rest)
			if rest < 8 {
				tail = ""
			}
		}
		lineInfo += tail
	}

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

		tok := fmt.Sprintf("%s %s", emojiStyle.Render(r.Emoji), senderStyle.Render("("+CleanPartyNick(r.Sender)+")"))
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
		chatBubble := fmt.Sprintf("💬 @%s: %s", CleanPartyNick(lastMsg.Sender), lastMsg.Message)
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
