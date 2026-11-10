package streamnet

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func TestIsPrivateAddr(t *testing.T) {
	private := []string{"127.0.0.1", "::1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254",
		"fe80::1", "fd00:ec2::254", "100.100.100.200", "0.0.0.0", "::", "::ffff:127.0.0.1", "64:ff9b::a9fe:a9fe", "224.0.0.1"}
	public := []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111", "93.184.216.34"}
	for _, s := range private {
		if !IsPrivateAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s should be private", s)
		}
	}
	for _, s := range public {
		if IsPrivateAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s should be public", s)
		}
	}
}

func TestGuard_PublicThenPrivateBlocked(t *testing.T) {
	g := &guard{}
	if err := g.check(netip.MustParseAddr("1.1.1.1")); err != nil {
		t.Fatal(err)
	}
	if err := g.check(netip.MustParseAddr("8.8.8.8")); err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"127.0.0.1", "192.168.0.1", "169.254.169.254"} {
		if err := g.check(netip.MustParseAddr(ip)); !errors.Is(err, ErrBlockedDestination) {
			t.Errorf("%s: err = %v, want blocked", ip, err)
		}
	}
}

func TestGuard_PrivateFirstHopAllowed(t *testing.T) {
	g := &guard{}
	for _, ip := range []string{"192.168.1.20", "127.0.0.1", "1.1.1.1", "10.0.0.1"} {
		if err := g.check(netip.MustParseAddr(ip)); err != nil {
			t.Errorf("%s: %v", ip, err)
		}
	}
}

func TestClient_LANStationPlaysAndRedirectsLocally(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "audio")
	}))
	defer final.Close()
	start := httptest.NewServer(http.RedirectHandler(final.URL+"/s", http.StatusFound))
	defer start.Close()

	resp, err := NewClient(Options{AllowPrivate: true}).Get(start.URL)
	if err != nil {
		t.Fatalf("LAN station must still play: %v", err)
	}
	resp.Body.Close()
}

func TestClient_PublicSessionCannotReachLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	defer srv.Close()
	// Simulate a session whose first hop was a public address (e.g. a public
	// station that redirected here).
	c := newClient(Options{}, &guard{decided: true, allowPrivate: false})
	_, err := c.Get(srv.URL)
	if !errors.Is(err, ErrBlockedDestination) {
		t.Fatalf("err = %v, want ErrBlockedDestination", err)
	}
}

func TestCheckRedirect(t *testing.T) {
	mk := func(u string) *http.Request {
		r, _ := http.NewRequest("GET", u, nil)
		return r
	}
	if err := CheckRedirect(mk("file:///etc/passwd"), nil); err == nil {
		t.Error("file:// redirect allowed")
	}
	if err := CheckRedirect(mk("gopher://x/"), nil); err == nil {
		t.Error("gopher redirect allowed")
	}
	via := make([]*http.Request, MaxRedirects)
	if err := CheckRedirect(mk("http://example.com/"), via); err == nil {
		t.Error("redirect cap not enforced")
	}
	if err := CheckRedirect(mk("https://example.com/"), via[:1]); err != nil {
		t.Errorf("normal redirect refused: %v", err)
	}
}
