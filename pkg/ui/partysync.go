package ui

import (
	"context"
	"errors"
	"fmt"
	"github.com/halpworld/halpradio/pkg/ui/components"
	"net"
	"net/netip"
	neturl "net/url"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/player/streamnet"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/util"
)

// Party forced-playback policy (PTY-1). A sync payload is a request from a
// remote peer to play something on this machine, so:
//
//   - only a sync from the verified room host plays automatically; a sync
//     from any other peer (Open Democracy) is shown as a status line only;
//   - a station ID the local store knows plays that local station (the host
//     picks among the user's own stations);
//   - otherwise the payload's stream URL must be a plain http(s) URL, and its
//     host must be public: loopback, private, link-local, CGNAT and similar
//     addresses are refused unless that exact URL is already one of the
//     user's stations. Host names are resolved and every address checked.

// ErrPartyStreamRefused is wrapped by the errors PartySyncStation and
// CheckPartyStreamHost return for a URL that may not be played.
var ErrPartyStreamRefused = errors.New("party stream refused")

// PartyLookupHost resolves a stream host for CheckPartyStreamHost. Tests
// replace it.
var PartyLookupHost = func(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// PartySyncNick is the sanitized "[xxxx-xxxx] @nick" label for a sync's
// sender; the fingerprint is that of the sender's verified identity.
func PartySyncNick(sp party.SyncPayload) string {
	return components.PartyHostLabel(sp.FromNick, party.PeerFingerprint(sp.FromPeerID))
}

// PartyFollowingHostText is the one-time notice shown when the host this
// session follows becomes known (or changes by hand-over). pinned is set
// when the invite's fingerprint verified the host; otherwise the user is
// asked to compare the fingerprint by hand.
func PartyFollowingHostText(label string, pinned bool) string {
	if pinned {
		return "Following host " + label + components.PartyVerifiedSuffix
	}
	return "Following host " + label + " (" + components.PartyManualCheckHint + ")"
}

// PartySyncPinned reports whether sp comes from the host the invite pinned
// (or the one it handed over to).
func PartySyncPinned(sess *party.PartySession, sp party.SyncPayload) bool {
	return sess != nil && sess.HostPinned() && sess.HostPeerID() == sp.FromPeerID
}

// PartySyncStation returns the station a sync from the verified host may
// play. hostToResolve is non-empty when the stream host is a DNS name that
// must pass CheckPartyStreamHost before playing.
func PartySyncStation(store *radio.Store, sp party.SyncPayload) (st radio.Station, hostToResolve string, err error) {
	if store != nil {
		if local := store.FindStationByID(sp.StationID); local != nil {
			return *local, "", nil
		}
	}
	raw := strings.TrimSpace(sp.StreamURL)
	if !radio.ValidStreamURL(raw) {
		return radio.Station{}, "", fmt.Errorf("%w: not a plain http(s) stream URL", ErrPartyStreamRefused)
	}
	st = radio.Station{
		ID:      util.SanitizeDisplay(sp.StationID, radio.MaxStationIDLen),
		Name:    util.SanitizeDisplay(sp.StationName, 120),
		URL:     raw,
		Genre:   util.SanitizeDisplay(sp.Genre, 80),
		Country: util.SanitizeDisplay(sp.Country, 60),
		Codec:   util.SanitizeDisplay(sp.Codec, 60),
		Bitrate: sp.Bitrate,
		// The player and stream readers are strict for this source, so a
		// private address is refused even if the host check were skipped.
		Source: "party",
	}
	if st.Name == "" {
		st.Name = "Party station"
	}
	if store != nil {
		for _, known := range store.GetAllStations() {
			if known.URL == raw {
				// The user already has this exact stream: play their own
				// entry, with its own Source.
				return known, "", nil
			}
		}
	}
	host := strings.TrimSuffix(strings.ToLower(hostnameOf(raw)), ".")
	if host == "" {
		return radio.Station{}, "", fmt.Errorf("%w: URL has no host", ErrPartyStreamRefused)
	}
	if ip, perr := netip.ParseAddr(strings.Trim(host, "[]")); perr == nil {
		if streamnet.IsPrivateAddr(ip) {
			return radio.Station{}, "", fmt.Errorf("%w: %s is a local or private address", ErrPartyStreamRefused, ip)
		}
		return st, "", nil
	}
	if !plausiblePublicHostname(host) {
		return radio.Station{}, "", fmt.Errorf("%w: %s is not a public host name", ErrPartyStreamRefused, util.SanitizeDisplay(host, 80))
	}
	return st, host, nil
}

// CheckPartyStreamHost resolves host and refuses it when any address is not
// public. It fails closed when the name cannot be resolved.
func CheckPartyStreamHost(ctx context.Context, host string) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	addrs, err := PartyLookupHost(ctx, host)
	if err != nil || len(addrs) == 0 {
		return fmt.Errorf("%w: cannot resolve %s", ErrPartyStreamRefused, util.SanitizeDisplay(host, 80))
	}
	for _, a := range addrs {
		if streamnet.IsPrivateAddr(a) {
			return fmt.Errorf("%w: %s resolves to a local or private address", ErrPartyStreamRefused, util.SanitizeDisplay(host, 80))
		}
	}
	return nil
}

func hostnameOf(raw string) string {
	u, err := neturl.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// plausiblePublicHostname rejects names that resolvers may treat as local or
// as numeric addresses ("localhost", "*.local", "2130706433", "0x7f.1").
func plausiblePublicHostname(h string) bool {
	if h == "localhost" || !strings.Contains(h, ".") {
		return false
	}
	for _, suf := range []string{".localhost", ".local", ".internal", ".lan", ".home", ".arpa", ".home.arpa"} {
		if strings.HasSuffix(h, suf) {
			return false
		}
	}
	labels := strings.Split(h, ".")
	tld := labels[len(labels)-1]
	if tld == "" {
		return false
	}
	for _, r := range tld {
		if (r < 'a' || r > 'z') && r != '-' && (r < '0' || r > '9') {
			return false
		}
	}
	// A top-level label made only of digits or hex-looking tokens is an
	// address in disguise.
	hasLetter := false
	for _, r := range tld {
		if r >= 'a' && r <= 'z' {
			hasLetter = true
		}
	}
	return hasLetter && !strings.HasPrefix(tld, "0x")
}

// partySyncCheckedMsg carries the result of resolving a host-pushed stream
// URL. seq ties it to the sync it belongs to.
type partySyncCheckedMsg struct {
	seq     uint64
	station radio.Station
	err     error
}

// Party sync pacing (FR-A-7, FR-A-10). A host can send syncs as fast as the
// network allows, and a new host (after join or a hand-over) could switch
// the audio before its notice can be read. So:
//
//   - syncs from a newly followed host are held for PartyNewHostHold after
//     the "Following host" notice;
//   - at most one playback change is applied per PartySyncMinInterval; syncs
//     arriving in between replace each other and the latest is applied;
//   - a sync that repeats what is already playing does not restart it.
const (
	PartyNewHostHold     = 5 * time.Second
	PartySyncMinInterval = 2 * time.Second
)

// partySyncFlushMsg applies the held host sync once its time has come.
type partySyncFlushMsg struct{}

// handlePartySync applies the forced-playback policy above to one sync and,
// the first time a host is followed, names it with its fingerprint.
func (m Model) handlePartySync(sp party.SyncPayload) (tea.Model, tea.Cmd) {
	if !sp.FromHost {
		return m.applyPartySync(sp)
	}
	fp := party.PeerFingerprint(sp.FromPeerID)
	key := fmt.Sprintf("%p|%s", m.PartySession, fp)
	if fp != "" && key != m.partyHostAnnounced {
		m.partyHostAnnounced = key
		m.partyHoldUntil = m.now().Add(PartyNewHostHold)
		announce := PartyFollowingHostText(PartySyncNick(sp), PartySyncPinned(m.PartySession, sp))
		if m.StatusMessage != "" {
			m.StatusMessage = announce + " • " + m.StatusMessage
		} else {
			m.StatusMessage = announce
		}
	}
	cp := sp
	m.partyPendingSync = &cp
	return m.flushPartySync()
}

// partySyncDue is when the held sync may be applied.
func (m Model) partySyncDue() time.Time {
	due := m.partyHoldUntil
	if next := m.partyLastApply.Add(PartySyncMinInterval); !m.partyLastApply.IsZero() && next.After(due) {
		due = next
	}
	return due
}

// flushPartySync applies the held sync if it is due, or schedules a
// partySyncFlushMsg for when it is.
func (m Model) flushPartySync() (tea.Model, tea.Cmd) {
	if m.partyPendingSync == nil {
		return m, nil
	}
	wait := m.partySyncDue().Sub(m.now())
	if wait > 0 {
		if m.partyFlushPending {
			return m, nil
		}
		m.partyFlushPending = true
		return m, tea.Tick(wait, func(time.Time) tea.Msg { return partySyncFlushMsg{} })
	}
	sp := *m.partyPendingSync
	m.partyPendingSync = nil
	if m.partySyncIsRepeat(sp) {
		// Nothing to change, but a stream check still in flight for an
		// older sync must not play after this one.
		m.partySyncSeq++
		return m, nil
	}
	m.partyLastApply = m.now()
	return m.applyPartySync(sp)
}

// partySyncIsRepeat reports whether sp asks for what the player already
// does, so applying it would only restart the stream.
func (m Model) partySyncIsRepeat(sp party.SyncPayload) bool {
	if m.Player == nil {
		return false
	}
	status := m.Player.Status()
	switch sp.Status {
	case "playing":
		cur := m.Player.CurrentStation()
		if cur == nil || (status != player.StatusPlaying && status != player.StatusConnecting) {
			return false
		}
		if sp.StationID != "" && cur.ID == sp.StationID && m.Store != nil && m.Store.FindStationByID(sp.StationID) != nil {
			return true
		}
		return strings.TrimSpace(sp.StreamURL) != "" && cur.URL == strings.TrimSpace(sp.StreamURL)
	case "paused":
		return status == player.StatusPaused
	case "stopped":
		return status == player.StatusStopped
	}
	return false
}

func (m Model) applyPartySync(sp party.SyncPayload) (tea.Model, tea.Cmd) {
	m.partySyncSeq++
	name := util.SanitizeDisplay(sp.StationName, 120)
	if !sp.FromHost {
		// Open Democracy: another listener tuned; tell the user, never
		// switch their audio.
		switch sp.Status {
		case "playing":
			m.StatusMessage = fmt.Sprintf("📻 %s tuned to %s", PartySyncNick(sp), name)
		case "paused":
			m.StatusMessage = fmt.Sprintf("⏸️ %s paused %s", PartySyncNick(sp), name)
		case "stopped":
			m.StatusMessage = fmt.Sprintf("⏹️ %s stopped the party audio", PartySyncNick(sp))
		}
		return m, nil
	}

	switch sp.Status {
	case "playing":
		st, host, err := PartySyncStation(m.Store, sp)
		if err != nil {
			m.StatusMessage = "Party sync refused: " + err.Error()
			return m, nil
		}
		if host == "" {
			m.playPartyStation(st)
			return m, nil
		}
		seq := m.partySyncSeq
		m.StatusMessage = fmt.Sprintf("📻 Party sync: checking %s…", st.Name)
		return m, func() tea.Msg {
			return partySyncCheckedMsg{seq: seq, station: st, err: CheckPartyStreamHost(context.Background(), host)}
		}
	case "paused":
		m.cancelDJAnnouncement()
		_ = m.Player.Pause()
		m.PlayingID = ""
		m.StatusMessage = fmt.Sprintf("⏸️ Party paused: %s", name)
		m.SyncDesktop()
	case "stopped":
		m.cancelDJAnnouncement()
		_ = m.Player.Stop()
		m.PlayingID = ""
		m.StatusMessage = "⏹️ Party audio stopped"
		m.SyncDesktop()
	}
	return m, nil
}

// playPartyStation starts a station the host's sync was allowed to play.
func (m *Model) playPartyStation(st radio.Station) {
	m.cancelDJAnnouncement()
	if err := m.Player.Play(st); err != nil {
		m.StatusMessage = "Party sync error: " + err.Error()
		return
	}
	m.PlayingID = st.ID
	m.StatusMessage = fmt.Sprintf("📻 Party sync: %s", st.Name)
	m.SyncDesktop()
}

// partyFlashText is PartyFlashText for the model's session.
func (m Model) partyFlashText(msg string) string { return PartyFlashText(m.PartySession, msg) }

// PartyFlashText rewrites party notices that name a peer, so the nickname
// is shown the same way as everywhere else ("[xxxx-xxxx] @nick", brackets
// removed from the nick) and a host change always names the new host by
// its fingerprint (FR-A-5, FR-A-10). pkg/party builds these texts from raw
// nicknames.
func PartyFlashText(sess *party.PartySession, msg string) string {
	hostLabel := func() string {
		if sess == nil {
			return "a new host"
		}
		return components.SessionHostLabel(sess)
	}
	check := func() string {
		if sess != nil && sess.HostPinned() {
			return ""
		}
		return " (check this fingerprint with the new host)"
	}
	switch {
	case strings.HasPrefix(msg, "👑 Host migrated to @"):
		if sess != nil && sess.IsHost() {
			return "👑 Host handed over: you are now the room host"
		}
		return "👑 Host migrated to " + hostLabel() + check()
	case strings.HasPrefix(msg, "👑 Host @") && strings.HasSuffix(msg, "You are now the room host!"):
		return "👑 The host disconnected. You are now the room host!"
	case strings.HasPrefix(msg, "👑 Host @") && strings.HasSuffix(msg, "is now the room host!"):
		return "👑 The host disconnected. " + hostLabel() + " is now the room host" + check()
	case strings.HasPrefix(msg, "👋 Host @") && strings.HasSuffix(msg, " left the room. Waiting for the host from your invite."):
		nick := strings.TrimSuffix(strings.TrimPrefix(msg, "👋 Host @"), " left the room. Waiting for the host from your invite.")
		return "👋 Host " + components.PartyHostLabel(nick, "") + " left the room. Waiting for the host from your invite."
	case strings.HasPrefix(msg, "👋 @") && strings.HasSuffix(msg, " left the room"):
		nick := strings.TrimSuffix(strings.TrimPrefix(msg, "👋 @"), " left the room")
		return "👋 " + components.PartyHostLabel(nick, "") + " left the room"
	}
	return util.SanitizeDisplay(msg, 300)
}
