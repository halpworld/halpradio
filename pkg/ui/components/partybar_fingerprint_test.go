package components

import (
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/theme"
)

func TestPartyHostLabel(t *testing.T) {
	if got := PartyHostLabel("dj\x1b]0;x\x07", "a1b2-c3d4"); strings.ContainsAny(got, "\x1b\x07") || !strings.HasPrefix(got, "[a1b2-c3d4] @dj") {
		t.Fatalf("label %q", got)
	}
	if got := PartyHostLabel("", ""); got != "@a peer" {
		t.Fatalf("empty label %q", got)
	}
}

func TestPartyBarShowsHostFingerprint(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	render := func(isHost bool) string {
		return RenderPartyBar("7K9P2XQMHF4DWNR8", "room", PartyHostLabel("kenth", "a1b2-c3d4"), isHost,
			party.DJPassHostOnly, 2, nil, "", player.StatusStopped, 50, false, NewVisualizer("dj-cat"),
			nil, nil, 160, th, false, "")
	}
	guest := render(false)
	if !strings.Contains(guest, "[a1b2-c3d4] @kenth") {
		t.Fatalf("guest bar lacks host fingerprint:\n%s", guest)
	}
	if strings.Contains(guest, PartyHostCheckHint) {
		t.Fatal("guest bar shows the host hint")
	}
	if host := render(true); !strings.Contains(host, PartyHostCheckHint) {
		t.Fatalf("host bar lacks the check hint:\n%s", host)
	}
}

// FR-A-5 (PoC F): a nickname cannot imitate a fingerprint, and on a narrow
// bar the real fingerprint stays visible and comes first; the nick is cut.
func TestPartyHostLabelNickCannotImitateFingerprint(t *testing.T) {
	const real, fake = "N8TE-9P97-YS5K-JABC", "A1B2-C3D4-E5F6-G7H8"
	for _, nick := range []string{
		"kenth [" + fake + "]",
		"kenth ［" + fake + "］",
		"[" + fake + "] @kenth",
		"kenth [" + fake + "] " + strings.Repeat("·", 40),
	} {
		label := PartyHostLabel(nick, real)
		if !strings.HasPrefix(label, "["+real+"] @") {
			t.Fatalf("nick %q: label %q does not start with the real fingerprint", nick, label)
		}
		if strings.Count(label, "[") != 1 || strings.Count(label, "]") != 1 || strings.Contains(label, "［") {
			t.Fatalf("nick %q: brackets kept in %q", nick, label)
		}
		bar := RenderPartyBar("ABCDEFGHJKLMNPQR", "room", label, false, party.DJPassHostOnly, 3, nil, "",
			player.StatusPlaying, 80, false, nil, nil, nil, 50, theme.GetTheme("tokyonight"), false, "")
		if !strings.Contains(bar, "["+real+"]") {
			t.Fatalf("nick %q: real fingerprint cut from the 50-col bar:\n%s", nick, bar)
		}
		if strings.Contains(bar, "["+fake+"]") {
			t.Fatalf("nick %q: fake fingerprint shown in brackets:\n%s", nick, bar)
		}
		if i, j := strings.Index(bar, real), strings.Index(bar, fake); j >= 0 && j < i {
			t.Fatalf("nick %q: fake fingerprint shown before the real one", nick)
		}
	}
}
