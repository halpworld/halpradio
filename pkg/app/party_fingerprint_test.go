package app

import (
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/party"
)

const testHostPeerID = "a1b2c3d4e5f60718293a4b5c6d7e8f90"

// Headless guests print the followed host with its fingerprint, once per host.
func TestHeadlessPrintsFollowedHostOnce(t *testing.T) {
	h, _, out := newHeadless(t)
	sp := party.SyncPayload{StationID: "x", StationName: "S", Status: "paused",
		FromHost: true, FromPeerID: testHostPeerID, FromNick: "dj\x1b]0;pwn\x07"}
	h.handle(sp)
	h.handle(sp)
	h.wg.Wait()
	got := out.String()
	if strings.Count(got, "Following host") != 1 {
		t.Fatalf("want one Following host line, got:\n%s", got)
	}
	if !strings.Contains(got, "Following host ["+party.PeerFingerprint(testHostPeerID)+"] @dj") {
		t.Fatalf("host label missing: %q", got)
	}
	if strings.ContainsAny(got, "\x1b\x07") {
		t.Fatalf("nick not sanitized: %q", got)
	}

	// A hand-over to another host is announced again.
	sp.FromPeerID = "ffff0000" + testHostPeerID[8:]
	h.handle(sp)
	h.wg.Wait()
	if !strings.Contains(out.String(), "["+party.PeerFingerprint(sp.FromPeerID)+"]") {
		t.Fatalf("new host not announced: %q", out.String())
	}
}

func TestHeadlessFingerprintLine(t *testing.T) {
	host := headlessFingerprintLine(true, "kenth", party.PeerFingerprint(testHostPeerID), "")
	if !strings.Contains(host, "["+party.PeerFingerprint(testHostPeerID)+"] @kenth") || !strings.Contains(host, "tell guests to check this") {
		t.Fatalf("host line %q", host)
	}
	guest := headlessFingerprintLine(false, "guest", party.PeerFingerprint(testHostPeerID), "")
	if strings.Contains(guest, "tell guests") {
		t.Fatalf("guest line %q", guest)
	}
}
