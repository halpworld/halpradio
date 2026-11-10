package components

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/theme"
)

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// A peer nickname containing a 16-symbol run (any case, with spaces or
// dashes, or letters that upper-case to ASCII) cannot show it.
func TestPartyNickFingerprintRunStripped(t *testing.T) {
	fp := party.ExampleFingerprint
	flat := strings.ReplaceAll(fp, "-", "")
	for _, nick := range []string{
		"kenth " + fp,
		"kenth_" + strings.ToLower(flat),
		"x!" + strings.Join(strings.Split(flat, ""), " ") + "!y",
		"a_" + strings.Repeat("ſ", party.FingerprintLen), // ſ upper-cases to S
		fp + "_" + fp,
	} {
		got := CleanPartyNick(nick)
		if party.ContainsFingerprint(got) {
			t.Errorf("nick %q cleaned to %q, still a fingerprint", nick, got)
		}
		if !strings.Contains(got, "…") {
			t.Errorf("nick %q cleaned to %q without a marker", nick, got)
		}
		if label := PartyHostLabel(nick, fp); strings.Count(label, fp) != 1 || strings.Contains(label[len(fp)+2:], strings.ToLower(flat[:8])) {
			t.Errorf("label %q", label)
		}
	}
	// Short runs and ordinary names are kept.
	for _, nick := range []string{"kenth", "DJ K4QZ-7HXM", "party-animal-2024"} {
		if got := CleanPartyNick(nick); got != nick {
			t.Errorf("nick %q changed to %q", nick, got)
		}
	}
}

// The 19-character fingerprint is never cut at 40 columns (or down to the
// 30-column minimum), for guests and the host, verified or not; the
// nickname gives way first.
func TestPartyBarKeepsFingerprintAtNarrowWidth(t *testing.T) {
	fp := party.ExampleFingerprint
	nick := strings.Repeat("longnickname", 4)
	invite := party.FormatInvite(party.ExampleRoomCode, fp)
	for _, width := range []int{30, 40, 50} {
		for _, isHost := range []bool{false, true} {
			for _, label := range []string{PartyHostLabel(nick, fp), PartyHostLabel(nick, fp) + PartyVerifiedSuffix} {
				bar := RenderPartyBar(invite, "room", label, isHost, party.DJPassHostOnly, 3, nil, "",
					player.StatusPlaying, 80, false, nil, nil, nil, width, theme.GetTheme("tokyonight"), false, "")
				plain := ansiSeq.ReplaceAllString(bar, "")
				if !strings.Contains(plain, "["+fp+"]") {
					t.Fatalf("width %d host=%v: fingerprint cut:\n%s", width, isHost, plain)
				}
				for _, line := range strings.Split(bar, "\n") {
					if w := lipgloss.Width(line); w > width {
						t.Fatalf("width %d: line is %d wide:\n%s", width, w, plain)
					}
				}
				if width >= 40 && strings.HasSuffix(label, PartyVerifiedSuffix) && !strings.Contains(plain, "✓") {
					t.Fatalf("width %d: verified mark dropped:\n%s", width, plain)
				}
			}
		}
	}
	// Wide enough: the whole invite is in the bar.
	wide := ansiSeq.ReplaceAllString(RenderPartyBar(invite, "room", PartyHostLabel("kenth", fp), false, party.DJPassHostOnly, 3, nil, "",
		player.StatusPlaying, 80, false, nil, nil, nil, 120, theme.GetTheme("tokyonight"), false, ""), "")
	if !strings.Contains(wide, "Invite: "+invite) {
		t.Fatalf("wide bar lacks the invite:\n%s", wide)
	}
}

func TestFitPartyHostLabel(t *testing.T) {
	fp := party.ExampleFingerprint
	label := PartyHostLabel("abcdefghijklmnopqrstuvwx", fp) + PartyVerifiedSuffix
	for w := 1; w <= lipgloss.Width(label)+1; w++ {
		got := FitPartyHostLabel(label, w)
		if !strings.HasPrefix(got, "["+fp+"]") {
			t.Fatalf("w=%d: %q", w, got)
		}
		if w >= lipgloss.Width("["+fp+"] @a... ✓") && lipgloss.Width(got) > w {
			t.Fatalf("w=%d: %q is %d wide", w, got, lipgloss.Width(got))
		}
	}
	if got := FitPartyHostLabel(label, 34); !strings.Contains(got, "✓") || !strings.Contains(got, "@") {
		t.Fatalf("34 cells: %q", got)
	}
}
