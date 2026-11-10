package ui

import (
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/party"
)

const testHostPeerID = "a1b2c3d4e5f60718293a4b5c6d7e8f90"

// The first sync from a verified host names it with its fingerprint; later
// syncs from the same host do not repeat the notice.
func TestPartySyncNamesFollowedHostWithFingerprint(t *testing.T) {
	m, _ := newPartyTestModel(t)
	sp := party.SyncPayload{StationName: "S", Status: "paused", FromHost: true,
		FromPeerID: testHostPeerID, FromNick: "dj\x1b]0;pwn\x07"}
	// The notice is shown while the new host's sync is held.
	nm, _ := m.Update(syncMsg(sp))
	m = nm.(Model)
	if !strings.Contains(m.StatusMessage, "Following host ["+party.PeerFingerprint(testHostPeerID)+"] @dj") {
		t.Fatalf("status %q", m.StatusMessage)
	}
	if strings.ContainsAny(m.StatusMessage, "\x1b\x07") {
		t.Fatalf("nick not sanitized: %q", m.StatusMessage)
	}
	nm, _ = deliverSync(t, m, syncMsg(sp))
	m = nm.(Model)
	if strings.Count(m.StatusMessage, "Following host") > 1 || m.partyHoldUntil.After(m.now()) {
		t.Fatalf("notice repeated: %q", m.StatusMessage)
	}
}
