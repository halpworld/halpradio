package app

import (
	"bytes"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/ui"
)

// FR-C-3: the IPC join takes exactly what party.ParseInvite accepts, and
// forwards the normalised invite so the running instance pins the host.
func TestDispatchPartyIPCJoinInvite(t *testing.T) {
	var msgs []ui.PartyJoinRoomMsg
	sender := func(m tea.Msg) {
		if j, ok := m.(ui.PartyJoinRoomMsg); ok {
			msgs = append(msgs, j)
		}
	}
	code, fp := party.ExampleRoomCode, party.ExampleFingerprint
	for _, bad := range []string{
		code + "/",
		code + "/K4QZ-7HXM",
		code + "/" + fp + "/" + fp,
		code + "/" + fp + " 10.0.0.1:4000",
		code + "/" + strings.Replace(fp, "K", "0", 1), // 0 is not in the alphabet
		"/" + fp,
		code + "\x1b[2J/" + fp,
	} {
		if _, _, err := party.ParseInvite(bad); err == nil {
			t.Fatalf("test case %q is accepted by ParseInvite", bad)
		}
		if err := dispatchPartyIPC(sender, desktop.ActionPartyJoin, bad); err == nil {
			t.Errorf("malformed invite %q accepted", bad)
		}
	}
	if len(msgs) != 0 {
		t.Fatalf("malformed invites produced join messages: %+v", msgs)
	}
	want := party.FormatInvite(code, fp)
	for _, good := range []string{want, strings.ToLower(want), strings.ReplaceAll(want, "-", "")} {
		msgs = nil
		if err := dispatchPartyIPC(sender, desktop.ActionPartyJoin, good); err != nil {
			t.Fatalf("invite %q refused: %v", good, err)
		}
		if len(msgs) != 1 || msgs[0].RoomCode != want {
			t.Fatalf("invite %q gave %+v, want %q", good, msgs, want)
		}
	}
}

// FR-C-8: a hand-typed code for a new room must be hard to guess.
func TestPartyCreateRefusesPredictableCode(t *testing.T) {
	const code = "ABCD-EFGA-BCDE-FGAB" // 7 symbols: valid to join, too few to create
	if party.ValidateRoomCode(code) != nil || party.ValidateNewRoomCode(code) == nil {
		t.Fatal("test code no longer sits between the two limits")
	}
	var out bytes.Buffer
	_, _, err := runPartyCreate([]string{"--code", code, "room"}, nil, &out)
	if err == nil {
		t.Fatal("predictable room code accepted for a new room")
	}
	if !strings.Contains(out.String(), "cannot be used for a new room") || !strings.Contains(out.String(), "too predictable") {
		t.Fatalf("unclear error: %q", out.String())
	}
}

func TestHeadlessPinnedLines(t *testing.T) {
	fp := party.PeerFingerprint(testHostPeerID)
	if line := headlessFingerprintLine(false, "guest", "", fp); !strings.Contains(line, "["+fp+"]") || !strings.Contains(line, "invite") {
		t.Fatalf("pinned guest line %q", line)
	}
	label := ui.PartySyncNick(party.SyncPayload{FromPeerID: testHostPeerID, FromNick: "host"})
	if txt := ui.PartyFollowingHostText(label, true); !strings.Contains(txt, "verified by invite") || strings.Contains(txt, "check it matches") {
		t.Fatalf("pinned follow notice %q", txt)
	}
	if txt := ui.PartyFollowingHostText(label, false); strings.Contains(txt, "verified") || !strings.Contains(txt, "check it matches") {
		t.Fatalf("unpinned follow notice %q", txt)
	}
	if got := partyCLINick("dj_"+party.ExampleFingerprint, "listener"); got != "dj_…" {
		t.Fatalf("CLI nickname %q", got)
	}
}
