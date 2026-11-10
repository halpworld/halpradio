package app

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/ui"
)

func newHeadless(t *testing.T) (*headlessSync, *player.MockPlayer, *bytes.Buffer) {
	t.Helper()
	pm := player.NewMockPlayer(50, nil)
	var out bytes.Buffer
	// The clock jumps forward whenever a held sync is scheduled, so a sync
	// that would wait is applied at once.
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	h := &headlessSync{store: radio.NewStore(), pm: pm, out: &out}
	h.now = func() time.Time { return now }
	h.after = func(d time.Duration, f func()) { now = now.Add(d); f() }
	return h, pm, &out
}

// PTY-1 (headless path): a non-host sync never switches audio.
func TestHeadlessSyncNonHostDoesNotPlay(t *testing.T) {
	h, pm, out := newHeadless(t)
	h.handle(party.SyncPayload{StationID: "x", StationName: "Evil\x1b]0;pwn\x07", StreamURL: "http://example.com/s", Status: "playing", FromNick: "mallory"})
	h.wg.Wait()
	if pm.CurrentStation() != nil {
		t.Fatal("non-host sync played")
	}
	if !strings.Contains(out.String(), "@mallory tuned to") {
		t.Errorf("output %q", out.String())
	}
	if strings.ContainsAny(out.String(), "\x1b\x07") {
		t.Errorf("control characters printed: %q", out.String())
	}
}

func TestHeadlessSyncHostPrivateRefused(t *testing.T) {
	h, pm, out := newHeadless(t)
	h.handle(party.SyncPayload{StationID: "x", StreamURL: "http://169.254.169.254/latest", Status: "playing", FromHost: true, FromNick: "host"})
	h.wg.Wait()
	if pm.CurrentStation() != nil {
		t.Fatal("host sync to a link-local address played")
	}
	if !strings.Contains(out.String(), "Refused") {
		t.Errorf("output %q", out.String())
	}
}

func TestHeadlessSyncHostDNSPrivateRefused(t *testing.T) {
	old := ui.PartyLookupHost
	ui.PartyLookupHost = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	t.Cleanup(func() { ui.PartyLookupHost = old })

	h, pm, _ := newHeadless(t)
	h.handle(party.SyncPayload{StationID: "x", StreamURL: "http://loopback.example.com/s", Status: "playing", FromHost: true})
	h.wg.Wait()
	if pm.CurrentStation() != nil {
		t.Fatal("host sync to a name resolving to loopback played")
	}
}

func TestHeadlessSyncHostPublicPlays(t *testing.T) {
	old := ui.PartyLookupHost
	ui.PartyLookupHost = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	t.Cleanup(func() { ui.PartyLookupHost = old })

	h, pm, _ := newHeadless(t)
	h.handle(party.SyncPayload{StationID: "pub", StationName: "Pub", StreamURL: "http://stream.example.com/s", Status: "playing", FromHost: true})
	h.wg.Wait()
	if st := pm.CurrentStation(); st == nil || st.ID != "pub" {
		t.Fatalf("public host stream did not play: %+v", st)
	}
}

// LOC-7: an IPC join carries only the code; a "CODE host:port" payload or a
// legacy code is refused instead of being turned into a direct connection.
func TestDispatchPartyIPCJoinCodeOnly(t *testing.T) {
	var msgs []ui.PartyJoinRoomMsg
	sender := func(m tea.Msg) {
		if j, ok := m.(ui.PartyJoinRoomMsg); ok {
			msgs = append(msgs, j)
		}
	}
	for _, bad := range []string{party.ExampleRoomCode + " 10.0.0.1:4000", "8X2K9P", ""} {
		if err := dispatchPartyIPC(sender, desktop.ActionPartyJoin, bad); err == nil {
			t.Errorf("payload %q accepted", bad)
		}
	}
	if len(msgs) != 0 {
		t.Fatalf("invalid payloads produced join messages: %+v", msgs)
	}
	if err := dispatchPartyIPC(sender, desktop.ActionPartyJoin, strings.ToLower(party.ExampleRoomCode)); err != nil {
		t.Fatalf("valid code refused: %v", err)
	}
	if len(msgs) != 1 || msgs[0].RoomCode != party.NormalizeRoomCode(party.ExampleRoomCode) {
		t.Fatalf("unexpected join messages: %+v", msgs)
	}
}

// countingPartyPlayer counts Play calls.
type countingPartyPlayer struct{ plays []string }

func (c *countingPartyPlayer) Play(st radio.Station) error {
	c.plays = append(c.plays, st.ID)
	return nil
}
func (c *countingPartyPlayer) Pause() error { return nil }
func (c *countingPartyPlayer) Stop() error  { return nil }

// FR-A-7 / FR-A-10 (headless): a burst of 100 syncs from a new host gives
// one Play, after the 5 s hold, of the latest station; repeats do not
// restart it; later changes are at most one per 2 s.
func TestHeadlessSyncBurstIsCoalesced(t *testing.T) {
	store := radio.NewStore()
	for i := 0; i < 100; i++ {
		store.Bundled = append(store.Bundled, radio.Station{ID: fmt.Sprintf("st-%d", i), Name: "S", URL: fmt.Sprintf("http://example.com/%d", i)})
	}
	pm := &countingPartyPlayer{}
	var out bytes.Buffer
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start
	type timer struct {
		at time.Time
		f  func()
	}
	var timers []timer
	h := &headlessSync{store: store, pm: pm, out: &out}
	h.now = func() time.Time { return now }
	h.after = func(d time.Duration, f func()) { timers = append(timers, timer{now.Add(d), f}) }
	fire := func() {
		for len(timers) > 0 && !timers[0].at.After(now) {
			tm := timers[0]
			timers = timers[1:]
			tm.f()
		}
	}

	for i := 0; i < 100; i++ {
		h.handle(party.SyncPayload{StationID: fmt.Sprintf("st-%d", i), Status: "playing", FromHost: true, FromPeerID: testHostPeerID, FromNick: "host"})
		now = now.Add(10 * time.Millisecond)
	}
	if len(pm.plays) != 0 || len(timers) != 1 {
		t.Fatalf("during the hold: plays=%v timers=%d", pm.plays, len(timers))
	}
	if !strings.Contains(out.String(), "Following host ["+party.PeerFingerprint(testHostPeerID)+"] @host") {
		t.Errorf("no host notice:\n%s", out.String())
	}
	now = start.Add(ui.PartyNewHostHold)
	fire()
	if len(pm.plays) != 1 || pm.plays[0] != "st-99" {
		t.Fatalf("after the hold: plays=%v, want [st-99]", pm.plays)
	}
	for i := 0; i < 10; i++ {
		h.handle(party.SyncPayload{StationID: "st-99", Status: "playing", FromHost: true, FromPeerID: testHostPeerID})
		now = now.Add(ui.PartySyncMinInterval)
		fire()
	}
	if len(pm.plays) != 1 {
		t.Fatalf("repeats restarted the stream: %v", pm.plays)
	}
	h.handle(party.SyncPayload{StationID: "st-5", Status: "playing", FromHost: true, FromPeerID: testHostPeerID})
	h.handle(party.SyncPayload{StationID: "st-6", Status: "playing", FromHost: true, FromPeerID: testHostPeerID})
	fire()
	if len(pm.plays) != 2 || pm.plays[1] != "st-5" {
		t.Fatalf("plays=%v, want st-5 applied (2 s since the last change)", pm.plays)
	}
	if len(timers) != 1 {
		t.Fatalf("st-6 not scheduled: %d timers", len(timers))
	}
	now = now.Add(ui.PartySyncMinInterval)
	fire()
	if len(pm.plays) != 3 || pm.plays[2] != "st-6" {
		t.Fatalf("plays=%v, want st-6 after 2 s", pm.plays)
	}
}
