package app

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/ui"
	"github.com/halpworld/halpradio/pkg/ui/components"
)

// partyPlayer is the part of the player the headless party loop drives.
type partyPlayer interface {
	Play(radio.Station) error
	Pause() error
	Stop() error
}

// headlessSync applies the same forced-playback policy as the TUI
// (ui.PartySyncStation / ui.CheckPartyStreamHost, PTY-1) to the headless
// `halpradio party create|join --headless` loop: only the verified host
// switches audio, and host-pushed URLs must point at public hosts unless the
// user already has that station.
type headlessSync struct {
	// sess is the party session, used to tell whether a host was verified
	// by the invite; nil in tests that do not need it.
	sess   *party.PartySession
	store  *radio.Store
	pm     partyPlayer
	out    io.Writer
	isJSON bool

	// now and after are the clock and timer; tests replace them. nil means
	// time.Now and time.AfterFunc.
	now   func() time.Time
	after func(time.Duration, func())

	mu  sync.Mutex // guards everything below and out
	seq uint64
	wg  sync.WaitGroup

	// followed is the fingerprint of the host last announced with
	// "Following host", so the line is printed once per host.
	followed string
	hostSeen bool

	// Pacing, as in the TUI (ui.PartyNewHostHold, ui.PartySyncMinInterval):
	// pending is the latest host sync not applied yet.
	pending    *party.SyncPayload
	holdUntil  time.Time
	lastApply  time.Time
	timerSet   bool
	appliedKey string
}

func (h *headlessSync) clock() time.Time {
	if h.now != nil {
		return h.now()
	}
	return time.Now()
}

func (h *headlessSync) schedule(d time.Duration, f func()) {
	if h.after != nil {
		h.after(d, f)
		return
	}
	time.AfterFunc(d, f)
}

// announceHost prints "Following host [xxxx-xxxx] @nick" the first time a
// sync arrives from a (new) verified host, and holds that host's syncs for
// ui.PartyNewHostHold so the notice can be read before the audio changes.
func (h *headlessSync) announceHost(sp party.SyncPayload) {
	if !sp.FromHost {
		return
	}
	fp := party.PeerFingerprint(sp.FromPeerID)
	h.mu.Lock()
	isNew := !h.hostSeen || fp != h.followed
	h.hostSeen = true
	h.followed = fp
	if isNew {
		h.holdUntil = h.clock().Add(ui.PartyNewHostHold)
	}
	h.mu.Unlock()
	if isNew && fp != "" {
		h.printf("🔑 %s\n", ui.PartyFollowingHostText(ui.PartySyncNick(sp), ui.PartySyncPinned(h.sess, sp)))
	}
}

func (h *headlessSync) printf(format string, args ...any) {
	if h.isJSON {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	fmt.Fprintf(h.out, format, args...)
}

func (h *headlessSync) handle(sp party.SyncPayload) {
	name := safe(sp.StationName)
	nick := ui.PartySyncNick(sp)
	if !sp.FromHost {
		switch sp.Status {
		case "playing":
			h.printf("📻 [Sync] %s tuned to %s (not switching: only the host changes your audio)\n", nick, name)
		case "paused":
			h.printf("⏸️ [Sync] %s paused %s\n", nick, name)
		case "stopped":
			h.printf("⏹️ [Sync] %s stopped\n", nick)
		}
		return
	}
	h.announceHost(sp)
	h.mu.Lock()
	cp := sp
	h.pending = &cp
	h.mu.Unlock()
	h.flush()
}

// syncKey identifies what a host sync asks for, so a repeat is not applied.
func syncKey(sp party.SyncPayload) string {
	return sp.Status + "|" + sp.StationID + "|" + strings.TrimSpace(sp.StreamURL)
}

// flush applies the pending host sync if it is due (new-host hold over and
// at least ui.PartySyncMinInterval since the last change), or schedules
// itself for when it is. Later syncs replace a pending one.
func (h *headlessSync) flush() {
	h.mu.Lock()
	if h.pending == nil {
		h.mu.Unlock()
		return
	}
	now := h.clock()
	due := h.holdUntil
	if !h.lastApply.IsZero() {
		if next := h.lastApply.Add(ui.PartySyncMinInterval); next.After(due) {
			due = next
		}
	}
	if wait := due.Sub(now); wait > 0 {
		if !h.timerSet {
			h.timerSet = true
			h.mu.Unlock()
			h.schedule(wait, func() {
				h.mu.Lock()
				h.timerSet = false
				h.mu.Unlock()
				h.flush()
			})
			return
		}
		h.mu.Unlock()
		return
	}
	sp := *h.pending
	h.pending = nil
	if syncKey(sp) == h.appliedKey {
		h.mu.Unlock()
		return
	}
	h.lastApply = now
	h.appliedKey = syncKey(sp)
	h.seq++
	seq := h.seq
	h.mu.Unlock()
	h.apply(seq, sp)
}

// apply switches playback for a host sync that is due.
func (h *headlessSync) apply(seq uint64, sp party.SyncPayload) {
	name := safe(sp.StationName)
	nick := ui.PartySyncNick(sp)
	switch sp.Status {
	case "playing":
		st, host, err := ui.PartySyncStation(h.store, sp)
		if err != nil {
			h.printf("⚠ [Sync] Refused %s's station: %s\n", nick, safe(err.Error()))
			return
		}
		if host == "" {
			h.play(seq, st, nick)
			return
		}
		h.wg.Add(1)
		go func() {
			defer h.wg.Done()
			if err := ui.CheckPartyStreamHost(context.Background(), host); err != nil {
				h.printf("⚠ [Sync] Refused %s's station: %s\n", nick, safe(err.Error()))
				return
			}
			h.play(seq, st, nick)
		}()
	case "paused":
		_ = h.pm.Pause()
		h.printf("⏸️ [Sync] Paused: %s\n", name)
	case "stopped":
		_ = h.pm.Stop()
		h.printf("⏹️ [Sync] Stopped\n")
	}
}

// play starts st unless a newer sync arrived while its host was checked.
func (h *headlessSync) play(seq uint64, st radio.Station, nick string) {
	h.mu.Lock()
	stale := seq != h.seq
	h.mu.Unlock()
	if stale {
		return
	}
	if err := h.pm.Play(st); err != nil {
		h.mu.Lock()
		h.appliedKey = "" // let the host's next sync retry
		h.mu.Unlock()
		h.printf("⚠ [Sync] Could not play %s: %s\n", safe(st.Name), safe(err.Error()))
		return
	}
	h.printf("📻 [Sync] Playing: %s from %s (host)\n", safe(st.Name), nick)
}

// headlessRoster formats a roster line with sanitized nicknames.
func headlessRoster(peers []*party.PeerInfo) string {
	names := make([]string, 0, len(peers))
	for _, p := range peers {
		if p == nil {
			continue
		}
		names = append(names, safe(components.CleanPartyNick(p.Nickname)))
	}
	return fmt.Sprintf("👥 [Roster] %d listeners: %s\n", len(peers), strings.Join(names, ", "))
}

// headlessFingerprintLine is the startup line naming this peer's own
// fingerprint; on the host it asks the user to have guests check it.
// hostPin is the host fingerprint from the guest's invite, or "".
func headlessFingerprintLine(isHost bool, nick, fingerprint, hostPin string) string {
	label := components.PartyHostLabel(nick, fingerprint)
	if isHost {
		return fmt.Sprintf("   Host:      %s (%s)\n", label, components.PartyHostCheckHint)
	}
	if hostPin != "" {
		return fmt.Sprintf("   You:       %s (following only host [%s] from your invite)\n", label, safe(hostPin))
	}
	return fmt.Sprintf("   You:       %s (the host's fingerprint is shown once it is known)\n", label)
}
