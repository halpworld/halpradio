package ui

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/util"
)

func newPartyTestModel(t *testing.T) (Model, *radio.Store) {
	t.Helper()
	store := radio.NewStore()
	cfg := util.DefaultConfig()
	cfg.PartyNickname = "tester"
	return NewModel(store, player.NewMockPlayer(80, nil), cfg), store
}

func sendKeys(m Model, keys ...tea.KeyMsg) Model {
	for _, k := range keys {
		nm, _ := m.Update(k)
		m = nm.(Model)
	}
	return m
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		m = sendKeys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func openJoinForm(m Model) Model {
	return sendKeys(m, tea.KeyMsg{Type: tea.KeyCtrlP}, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
}

// The join form used to require exactly 6 characters, so the new
// 16-character codes could not be entered at all.
func TestPartyJoinFormAcceptsSixteenCharacterCode(t *testing.T) {
	m, _ := newPartyTestModel(t)
	m = openJoinForm(m)
	if m.PartyModalScreen != 2 {
		t.Fatalf("expected join form, got screen %d", m.PartyModalScreen)
	}
	m = typeText(m, party.ExampleRoomCode)
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.PartySession == nil {
		t.Fatalf("16-character code was rejected: %q", m.PartyStatusMsg)
	}
	defer m.PartySession.Close()
	if m.ShowPartyModal {
		t.Error("modal should close after joining")
	}
}

func TestPartyJoinFormLegacyCodeShowsError(t *testing.T) {
	m, _ := newPartyTestModel(t)
	m = openJoinForm(m)
	m = typeText(m, "8X2K9P")
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.PartySession != nil {
		_ = m.PartySession.Close()
		t.Fatal("legacy 6-character code should not join")
	}
	if !strings.Contains(m.PartyStatusMsg, party.ErrLegacyRoomCode.Error()) {
		t.Errorf("expected legacy-code error, got %q", m.PartyStatusMsg)
	}
	if !m.ShowPartyModal || m.PartyModalScreen != 2 {
		t.Error("form should stay open after a rejected code")
	}
}

func TestPartyJoinFormBadAddressKeepsForm(t *testing.T) {
	m, _ := newPartyTestModel(t)
	m = openJoinForm(m)
	m = typeText(m, party.ExampleRoomCode)
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyTab}, tea.KeyMsg{Type: tea.KeyTab})
	m = typeText(m, "127.0.0.1:1") // nothing listens on port 1
	m = sendKeys(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.PartySession != nil {
		_ = m.PartySession.Close()
		t.Fatal("a failed direct connect should not leave a session")
	}
	if !strings.HasPrefix(m.PartyStatusMsg, "Could not connect:") {
		t.Errorf("expected a connect error, got %q", m.PartyStatusMsg)
	}
}

// deliverSync sends a sync and, if it is held (new host, or too soon after
// the last change), moves the model's clock to when it is due and flushes it.
func deliverSync(t *testing.T, m Model, msg PartyPlaybackSyncMsg) (tea.Model, tea.Cmd) {
	t.Helper()
	nm, cmd := m.Update(msg)
	m = nm.(Model)
	if m.partyPendingSync != nil {
		due := m.partySyncDue()
		m.Now = func() time.Time { return due }
		return m.Update(partySyncFlushMsg{})
	}
	return m, cmd
}

func syncMsg(sp party.SyncPayload) PartyPlaybackSyncMsg {
	if sp.Status == "" {
		sp.Status = "playing"
	}
	return PartyPlaybackSyncMsg(sp)
}

// PTY-1: a sync from a peer that is not the host only shows a status line.
func TestPartySyncFromNonHostDoesNotPlay(t *testing.T) {
	m, store := newPartyTestModel(t)
	st := radio.Station{ID: "local-1", Name: "Local One", URL: "http://example.com/one"}
	store.Bundled = append(store.Bundled, st)

	nm, cmd := deliverSync(t, m, syncMsg(party.SyncPayload{
		StationID: st.ID, StationName: "Local One", StreamURL: st.URL,
		FromHost: false, FromNick: "bob\x1b[31m",
	}))
	m = nm.(Model)
	if cmd != nil {
		t.Error("non-host sync should not start any work")
	}
	if m.PlayingID != "" {
		t.Fatalf("non-host sync played %q", m.PlayingID)
	}
	if !strings.Contains(m.StatusMessage, "@bob") || !strings.Contains(m.StatusMessage, "tuned to Local One") {
		t.Errorf("unexpected status %q", m.StatusMessage)
	}
	if strings.Contains(m.StatusMessage, "\x1b") {
		t.Errorf("escape sequence reached status: %q", m.StatusMessage)
	}
}

func TestPartySyncFromHostRefusesPrivateAddresses(t *testing.T) {
	for _, u := range []string{
		"http://127.0.0.1:8080/admin",
		"http://192.168.1.1/",
		"http://169.254.169.254/latest/meta-data/",
		"http://[::1]:9000/",
		"http://localhost:8000/",
		"http://router.local/",
		"file:///etc/passwd",
	} {
		m, _ := newPartyTestModel(t)
		nm, cmd := deliverSync(t, m, syncMsg(party.SyncPayload{
			StationID: "remote-x", StationName: "X", StreamURL: u, FromHost: true, FromNick: "host",
		}))
		m = nm.(Model)
		if cmd != nil {
			t.Errorf("%s: expected an immediate refusal, got a command", u)
		}
		if m.PlayingID != "" {
			t.Errorf("%s: played %q", u, m.PlayingID)
		}
		if !strings.HasPrefix(m.StatusMessage, "Party sync refused") {
			t.Errorf("%s: status %q", u, m.StatusMessage)
		}
	}
}

func withLookup(t *testing.T, addrs ...string) {
	t.Helper()
	old := PartyLookupHost
	PartyLookupHost = func(ctx context.Context, host string) ([]netip.Addr, error) {
		if len(addrs) == 0 {
			return nil, errors.New("no such host")
		}
		var out []netip.Addr
		for _, a := range addrs {
			out = append(out, netip.MustParseAddr(a))
		}
		return out, nil
	}
	t.Cleanup(func() { PartyLookupHost = old })
}

func runSyncWithLookup(t *testing.T, m Model, u string) Model {
	t.Helper()
	nm, cmd := deliverSync(t, m, syncMsg(party.SyncPayload{
		StationID: "remote-dns", StationName: "DNS Station", StreamURL: u, FromHost: true, FromNick: "host",
	}))
	m = nm.(Model)
	if cmd == nil {
		t.Fatalf("%s: expected a resolve command", u)
	}
	if m.PlayingID != "" {
		t.Fatalf("%s: played before the host was checked", u)
	}
	nm, _ = m.Update(cmd())
	return nm.(Model)
}

func TestPartySyncFromHostRefusesNameResolvingToPrivate(t *testing.T) {
	withLookup(t, "93.184.216.34", "10.0.0.5")
	m, _ := newPartyTestModel(t)
	m = runSyncWithLookup(t, m, "http://rebind.example.com/stream")
	if m.PlayingID != "" {
		t.Fatalf("played a host that resolves to a private address")
	}
	if !strings.HasPrefix(m.StatusMessage, "Party sync refused") {
		t.Errorf("status %q", m.StatusMessage)
	}
}

func TestPartySyncFromHostUnresolvableFailsClosed(t *testing.T) {
	withLookup(t)
	m, _ := newPartyTestModel(t)
	m = runSyncWithLookup(t, m, "http://nowhere.example.com/stream")
	if m.PlayingID != "" {
		t.Fatal("played an unresolvable host")
	}
}

func TestPartySyncFromHostPlaysPublicStream(t *testing.T) {
	withLookup(t, "93.184.216.34")
	m, _ := newPartyTestModel(t)
	m = runSyncWithLookup(t, m, "http://stream.example.com/live")
	if m.PlayingID != "remote-dns" {
		t.Fatalf("expected public stream to play, PlayingID=%q status=%q", m.PlayingID, m.StatusMessage)
	}
}

// A stale resolve result (another sync arrived meanwhile) is ignored.
func TestPartySyncStaleResolveIgnored(t *testing.T) {
	withLookup(t, "93.184.216.34")
	m, _ := newPartyTestModel(t)
	nm, cmd := deliverSync(t, m, syncMsg(party.SyncPayload{
		StationID: "first", StreamURL: "http://first.example.com/", FromHost: true,
	}))
	m = nm.(Model)
	nm, _ = deliverSync(t, m, syncMsg(party.SyncPayload{Status: "stopped", FromHost: true}))
	m = nm.(Model)
	nm, _ = m.Update(cmd())
	m = nm.(Model)
	if m.PlayingID != "" {
		t.Fatalf("stale sync played %q", m.PlayingID)
	}
}

// The user's own stations may point at a LAN address; those play.
func TestPartySyncFromHostAllowsLocalStationURL(t *testing.T) {
	m, store := newPartyTestModel(t)
	lan := radio.Station{ID: "my-lan", Name: "My LAN", URL: "http://192.168.1.10:8000/stream"}
	store.Local = append(store.Local, lan)

	// By station ID.
	nm, _ := deliverSync(t, m, syncMsg(party.SyncPayload{StationID: lan.ID, StreamURL: "http://ignored.example/", FromHost: true}))
	m = nm.(Model)
	if m.PlayingID != lan.ID {
		t.Fatalf("local station by ID did not play: %q (%s)", m.PlayingID, m.StatusMessage)
	}

	// By exact URL under another ID.
	m2, store2 := newPartyTestModel(t)
	store2.Local = append(store2.Local, lan)
	nm, _ = deliverSync(t, m2, syncMsg(party.SyncPayload{StationID: "other-id", StreamURL: lan.URL, FromHost: true}))
	m2 = nm.(Model)
	// The user's own entry is played, so its own Source applies.
	if m2.PlayingID != lan.ID {
		t.Fatalf("exact local URL did not play the stored station: %q (%s)", m2.PlayingID, m2.StatusMessage)
	}
}

// countingPlayer counts Play calls.
type countingPlayer struct {
	*player.MockPlayer
	plays int
}

func (c *countingPlayer) Play(st radio.Station) error {
	c.plays++
	return c.MockPlayer.Play(st)
}

// FR-A-7 / FR-A-10: a new host's syncs are held 5 s after the notice, then
// at most one change per 2 s is applied, latest wins. A burst of 100 syncs
// gives one Play, and a repeat of what is playing does not restart it.
func TestPartySyncBurstIsCoalesced(t *testing.T) {
	m, store := newPartyTestModel(t)
	cp := &countingPlayer{MockPlayer: player.NewMockPlayer(80, nil)}
	m.Player = cp
	for i := 0; i < 100; i++ {
		store.Bundled = append(store.Bundled, radio.Station{ID: fmt.Sprintf("st-%d", i), Name: fmt.Sprintf("S%d", i), URL: fmt.Sprintf("http://example.com/%d", i)})
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start
	m.Now = func() time.Time { return now }

	var ticks int
	for i := 0; i < 100; i++ {
		nm, cmd := m.Update(syncMsg(party.SyncPayload{StationID: fmt.Sprintf("st-%d", i), FromHost: true, FromPeerID: testHostPeerID, FromNick: "host"}))
		m = nm.(Model)
		if cmd != nil {
			ticks++
		}
		now = now.Add(10 * time.Millisecond)
	}
	if cp.plays != 0 {
		t.Fatalf("%d plays during the new-host hold", cp.plays)
	}
	if !strings.Contains(m.StatusMessage, "Following host ["+party.PeerFingerprint(testHostPeerID)+"] @host") {
		t.Errorf("no host notice: %q", m.StatusMessage)
	}
	if ticks != 1 {
		t.Errorf("%d flush timers scheduled, want 1", ticks)
	}
	// An early flush does nothing.
	nm, _ := m.Update(partySyncFlushMsg{})
	m = nm.(Model)
	if cp.plays != 0 {
		t.Fatal("played before the hold ended")
	}
	now = start.Add(PartyNewHostHold)
	nm, _ = m.Update(partySyncFlushMsg{})
	m = nm.(Model)
	if cp.plays != 1 || m.PlayingID != "st-99" {
		t.Fatalf("after the hold: plays=%d playing=%q, want 1 play of st-99", cp.plays, m.PlayingID)
	}

	// Within 2 s of the change, new syncs wait; the latest wins.
	for _, id := range []string{"st-1", "st-2", "st-3"} {
		nm, _ = m.Update(syncMsg(party.SyncPayload{StationID: id, FromHost: true, FromPeerID: testHostPeerID}))
		m = nm.(Model)
	}
	if cp.plays != 1 {
		t.Fatalf("change applied within %v of the last", PartySyncMinInterval)
	}
	now = now.Add(PartySyncMinInterval)
	nm, _ = m.Update(partySyncFlushMsg{})
	m = nm.(Model)
	if cp.plays != 2 || m.PlayingID != "st-3" {
		t.Fatalf("plays=%d playing=%q, want 2 plays, st-3", cp.plays, m.PlayingID)
	}

	// Repeats of the current station never restart it.
	cp.SetStatus(player.StatusPlaying)
	for i := 0; i < 10; i++ {
		now = now.Add(PartySyncMinInterval)
		nm, _ = m.Update(syncMsg(party.SyncPayload{StationID: "st-3", FromHost: true, FromPeerID: testHostPeerID}))
		m = nm.(Model)
	}
	if cp.plays != 2 {
		t.Fatalf("repeat syncs restarted the stream: %d plays", cp.plays)
	}

	// A hand-over to a new host is held again.
	other := "ffff0000" + testHostPeerID[8:]
	now = now.Add(PartySyncMinInterval)
	nm, _ = m.Update(syncMsg(party.SyncPayload{StationID: "st-50", FromHost: true, FromPeerID: other, FromNick: "new"}))
	m = nm.(Model)
	if cp.plays != 2 || !strings.Contains(m.StatusMessage, "["+party.PeerFingerprint(other)+"] @new") {
		t.Fatalf("new host applied at once or not named: plays=%d status=%q", cp.plays, m.StatusMessage)
	}
}

// FR-A-10: the host-migrated notice names the new host by fingerprint, and
// a nickname cannot add a fake one.
func TestPartyHostMigratedFlashNamesFingerprint(t *testing.T) {
	m, _ := newPartyTestModel(t)
	nm, _ := m.Update(PartyStatusFlashMsg("👑 Host migrated to @mallory [a1b2-c3d4]"))
	m = nm.(Model)
	if strings.Contains(m.StatusMessage, "[a1b2-c3d4]") || !strings.Contains(m.StatusMessage, "Host migrated to") {
		t.Fatalf("status %q", m.StatusMessage)
	}
	nm, _ = m.Update(PartyStatusFlashMsg("👋 @eve [9f3e-11aa] left the room"))
	m = nm.(Model)
	if strings.Contains(m.StatusMessage, "[9f3e-11aa]") || !strings.Contains(m.StatusMessage, "@eve") {
		t.Fatalf("status %q", m.StatusMessage)
	}
}
