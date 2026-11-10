package ui

// Wiring tests for FR-C-3 (invites that pin the host) in the TUI.

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/ui/components"
	"github.com/halpworld/halpradio/pkg/util"
)

func startTestHost(t *testing.T) *party.PartySession {
	t.Helper()
	host, err := party.NewPartySession(party.SessionConfig{RoomName: "r", Nickname: "host", IsHost: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	return host
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func connectToHost(t *testing.T, guest, host *party.PartySession) {
	t.Helper()
	if err := guest.ConnectDirect(fmt.Sprintf("127.0.0.1:%d", host.ListenPort())); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the guest to follow the host", func() bool { return guest.HostPeerID() == host.PeerID() })
}

func pasteText(m Model, s string) Model {
	return sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Paste: true})
}

// An invite typed or pasted into the join form pins the host: the session
// follows only that identity, and the UI says it was verified by the invite.
func TestPartyJoinFormInvitePinsHost(t *testing.T) {
	host := startTestHost(t)
	invite := host.Invite()
	if !strings.Contains(invite, "/"+host.Fingerprint()) {
		t.Fatalf("host invite %q lacks its fingerprint", invite)
	}
	for name, enter := range map[string]func(Model) Model{
		"typed":  func(m Model) Model { return typeText(m, strings.ToLower(invite)) },
		"pasted": func(m Model) Model { return pasteText(m, invite) },
	} {
		t.Run(name, func(t *testing.T) {
			m, _ := newPartyTestModel(t)
			m.Width, m.Height = 160, 40
			m = enter(openJoinForm(m))
			m = sendKeys(m, tea.KeyMsg{Type: tea.KeyEnter})
			if m.PartySession == nil {
				t.Fatalf("invite refused: %q", m.PartyStatusMsg)
			}
			sess := m.PartySession
			defer sess.Close()
			if sess.HostPin() != host.Fingerprint() {
				t.Fatalf("join did not pin the host: pin %q, want %q", sess.HostPin(), host.Fingerprint())
			}
			connectToHost(t, sess, host)
			if !sess.HostPinned() {
				t.Fatal("host from the invite is not marked as pinned")
			}
			label := components.SessionHostLabel(sess)
			if !strings.HasSuffix(label, components.PartyVerifiedSuffix) || !strings.HasPrefix(label, "["+host.Fingerprint()+"] @") {
				t.Fatalf("label %q", label)
			}
			view := stripANSI(m.View())
			if !strings.Contains(view, "verified by invite") {
				t.Fatalf("party bar does not say the host is verified:\n%s", view)
			}
			if strings.Contains(view, "check it matches") {
				t.Fatalf("pinned host still asks for a manual check:\n%s", view)
			}
		})
	}
}

// The IPC join message carries an invite as well.
func TestPartyJoinMsgInvitePinsHost(t *testing.T) {
	host := startTestHost(t)
	m, _ := newPartyTestModel(t)
	nm, _ := m.Update(PartyJoinRoomMsg{RoomCode: host.Invite()})
	m = nm.(Model)
	if m.PartySession == nil {
		t.Fatalf("invite refused: %q", m.StatusMessage)
	}
	defer m.PartySession.Close()
	if m.PartySession.HostPin() != host.Fingerprint() {
		t.Fatalf("pin %q, want %q", m.PartySession.HostPin(), host.Fingerprint())
	}
	nm, _ = m.Update(PartyJoinRoomMsg{RoomCode: party.NormalizeRoomCode(host.RoomCode()) + "/" + "TOO-SHORT"})
	m = nm.(Model)
	if !strings.HasPrefix(m.StatusMessage, "Party join error:") {
		t.Fatalf("malformed invite joined: %q", m.StatusMessage)
	}
}

// A bare code still joins, but the host is not verified: no "verified" mark,
// and the manual fingerprint check is asked for.
func TestPartyBareCodeHostNotVerified(t *testing.T) {
	host := startTestHost(t)
	m, _ := newPartyTestModel(t)
	m.Width, m.Height = 160, 40
	m = typeText(openJoinForm(m), host.RoomCode())
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.PartySession == nil {
		t.Fatalf("bare code refused: %q", m.PartyStatusMsg)
	}
	sess := m.PartySession
	defer sess.Close()
	if sess.HostPin() != "" {
		t.Fatalf("bare code pinned %q", sess.HostPin())
	}
	connectToHost(t, sess, host)
	if strings.Contains(components.SessionHostLabel(sess), "verified") {
		t.Fatalf("unpinned host labelled verified: %q", components.SessionHostLabel(sess))
	}
	if strings.Contains(stripANSI(m.View()), "verified by invite") {
		t.Fatal("bar says verified for an unpinned host")
	}
	mgr := stripANSI(components.RenderPartyManagerModal(sess, 3, 0, nil, 0, "", 160, 50, m.Theme))
	if strings.Contains(mgr, "verified by invite") || !strings.Contains(strings.Join(strings.Fields(mgr), " "), "check it matches") {
		t.Fatalf("manager host line for an unpinned host:\n%s", mgr)
	}
	sp := party.SyncPayload{FromHost: true, FromPeerID: host.PeerID(), FromNick: "host"}
	if txt := PartyFollowingHostText(PartySyncNick(sp), PartySyncPinned(sess, sp)); !strings.Contains(txt, "check it matches") {
		t.Fatalf("unpinned follow notice %q", txt)
	}
}

// The copy key copies the invite, not the bare room code.
func TestPartyCopyKeyCopiesInvite(t *testing.T) {
	var copied string
	defer util.SetClipboardWriterForTesting(func(s string) error { copied = s; return nil })()
	m, _ := newPartyTestModel(t)
	nm, _ := m.Update(PartyCreateRoomMsg{RoomName: "r", Nickname: "dj"})
	m = nm.(Model)
	if m.PartySession == nil {
		t.Fatalf("create failed: %q", m.StatusMessage)
	}
	defer m.PartySession.Close()
	if !strings.Contains(m.StatusMessage, "Invite: "+m.PartySession.Invite()) {
		t.Fatalf("created message lacks the invite: %q", m.StatusMessage)
	}
	m.ShowPartyModal, m.PartyModalScreen = true, 3
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	want := party.FormatInvite(m.PartySession.RoomCode(), m.PartySession.Fingerprint())
	if copied != want || !strings.Contains(copied, "/") {
		t.Fatalf("copied %q, want the invite %q", copied, want)
	}
}

// The user's own nickname cannot carry a fingerprint.
func TestPartyOwnNickFingerprintStripped(t *testing.T) {
	nick := "dj_" + strings.ToLower(party.ExampleFingerprint)
	m, _ := newPartyTestModel(t)
	nm, _ := m.Update(PartyCreateRoomMsg{RoomName: "r", Nickname: nick})
	m = nm.(Model)
	if m.PartySession == nil {
		t.Fatalf("create failed: %q", m.StatusMessage)
	}
	defer m.PartySession.Close()
	if got := m.PartySession.Nickname(); party.ContainsFingerprint(got) || got != "dj_…" {
		t.Fatalf("own nickname %q still carries a fingerprint", got)
	}
	if got := partyOwnNick(party.ExampleFingerprint, "", "host"); got != "…" {
		t.Fatalf("partyOwnNick = %q", got)
	}
}
