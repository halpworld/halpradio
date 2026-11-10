package player

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/halpworld/halpradio/pkg/player/dsp"
	"github.com/halpworld/halpradio/pkg/radio"
)

// RR-E-1: a directory station pointing at loopback is refused by Play
// before any backend, socket or ICY listener starts.
func TestPlay_RemoteStationOnPrivateHostRefused(t *testing.T) {
	for _, u := range []string{
		"http://127.0.0.1:8000/live",
		"http://localhost:8000/",
		"http://169.254.169.254/latest/meta-data/",
		"http://router.lan/stream",
		"http://[::1]/",
	} {
		m := NewManager("mpv", 50, nil)
		_ = m.Play(radio.Station{ID: "rb", URL: u, Source: "radiobrowser"})
		if m.Status() != StatusError || !strings.Contains(m.Error(), "private") {
			t.Errorf("%s: status %v err %q, want private-host refusal", u, m.Status(), m.Error())
		}
		m.mu.Lock()
		started := m.cmd != nil || m.cancelFn != nil
		m.mu.Unlock()
		if started {
			t.Errorf("%s: a backend was started", u)
		}
	}
}

func TestPrivateHostPolicy_DefaultAndOverride(t *testing.T) {
	m := &Manager{}
	cases := map[string]bool{"local": true, "custom": true, "": true,
		"radiobrowser": false, "bundled": false, "somafm": false, "party": false, "catalog": false}
	for src, want := range cases {
		if got := m.privateAllowed(radio.Station{Source: src}); got != want {
			t.Errorf("default policy source %q = %v, want %v", src, got, want)
		}
	}
	m.SetPrivateHostPolicy(func(radio.Station) bool { return false })
	if m.privateAllowed(radio.Station{Source: "local"}) {
		t.Error("override not applied")
	}
	m.SetPrivateHostPolicy(nil)
	if !m.privateAllowed(radio.Station{Source: "local"}) {
		t.Error("nil policy must restore the default")
	}
}

// The native backend's dial guard refuses loopback when private is not
// allowed (covers names that resolve to private addresses and rebinding).
func TestPlayNative_StrictRefusesLoopback(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	m := &Manager{status: StatusConnecting, dspSettings: dsp.DefaultSettings()}
	m.playNative(context.Background(), radio.Station{URL: srv.URL}, 50, false)
	if m.Status() != StatusError {
		t.Fatalf("status = %v, want StatusError", m.Status())
	}
	if hits.Load() != 0 {
		t.Fatalf("strict client reached the loopback server %d times", hits.Load())
	}
}

// External players resolve on their own, so the host is resolved first and
// any restricted answer refuses the spawn.
func TestPlayExternal_PreCheckRefusesPrivateResolution(t *testing.T) {
	for _, addrs := range [][]string{{"10.0.0.7"}, {"93.184.216.34", "192.168.1.1"}, {"::ffff:127.0.0.1"}} {
		m := &Manager{status: StatusConnecting, dspSettings: dsp.DefaultSettings()}
		m.lookup = func(context.Context, string) ([]netip.Addr, error) {
			var out []netip.Addr
			for _, a := range addrs {
				out = append(out, netip.MustParseAddr(a))
			}
			return out, nil
		}
		m.playExternal(context.Background(), "mpv", radio.Station{URL: "http://radio.example/live"}, 50, false)
		if m.Status() != StatusError || !strings.Contains(m.Error(), "not a public address") {
			t.Errorf("%v: status %v err %q", addrs, m.Status(), m.Error())
		}
		if m.cmd != nil {
			t.Errorf("%v: player was spawned", addrs)
		}
	}
}

// RR-E-8: URLs quoted in status errors are display-sanitised.
func TestPlayErrorURLIsSanitised(t *testing.T) {
	m := NewManager("mpv", 50, nil)
	_ = m.Play(radio.Station{URL: "ftp://x\x1b]52;c;ZXZpbA==\x07/\n" + strings.Repeat("a", 1000)})
	e := m.Error()
	if strings.ContainsAny(e, "\x1b\x07\n") {
		t.Fatalf("control characters reached the status line: %q", e)
	}
	if len([]rune(e)) > 400 {
		t.Fatalf("error not capped: %d runes", len([]rune(e)))
	}
}
