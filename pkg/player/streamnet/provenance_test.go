package streamnet

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

// RR-E-1: without AllowPrivate even the first hop may not be restricted.
func TestNewClient_DefaultRefusesPrivateFirstHop(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = io.WriteString(w, "internal")
	}))
	defer srv.Close()
	for _, u := range []string{srv.URL + "/admin?reboot=1", strings.Replace(srv.URL, "127.0.0.1", "localhost", 1) + "/x"} {
		resp, err := NewClient(Options{}).Get(u)
		if err == nil {
			resp.Body.Close()
			t.Errorf("GET %s succeeded", u)
		} else if !errors.Is(err, ErrBlockedDestination) {
			t.Errorf("GET %s: err = %v, want ErrBlockedDestination", u, err)
		}
	}
	if hits != 0 {
		t.Fatalf("loopback server reached %d times", hits)
	}
	// The same server is reachable for a user-added station.
	resp, err := NewClient(Options{AllowPrivate: true}).Get(srv.URL)
	if err != nil {
		t.Fatalf("AllowPrivate: %v", err)
	}
	resp.Body.Close()
}

func TestIsPrivateAddr_Table(t *testing.T) {
	cases := map[string]bool{
		// IPv4 restricted
		"0.0.0.0": true, "0.1.2.3": true, "10.0.0.1": true, "100.64.0.1": true, "100.127.255.254": true,
		"127.0.0.1": true, "169.254.169.254": true, "172.16.0.1": true, "172.31.255.255": true,
		"192.0.0.8": true, "192.0.2.1": true, "192.168.1.1": true, "198.18.0.1": true, "198.19.255.255": true,
		"198.51.100.1": true, "203.0.113.1": true, "224.0.0.1": true, "239.255.255.250": true,
		"240.0.0.1": true, "255.255.255.255": true,
		// IPv6 restricted
		"::": true, "::1": true, "fe80::1": true, "fe80::1%en0": true, "fc00::1": true, "fd00:ec2::254": true,
		"fec0::1": true, "ff02::1": true, "2001::1": true, "2001:db8::1": true, "100::1": true,
		"::ffff:127.0.0.1": true, "::ffff:10.0.0.1": true, "::127.0.0.1": true, "::169.254.169.254": true, "::8.8.8.8": true,
		"64:ff9b::7f00:1": true, "64:ff9b::a9fe:a9fe": true, "64:ff9b:1::7f00:1": true, "64:ff9b:1::808:808": true,
		"2002:7f00:1::1": true, "2002:c0a8:101::1": true, "2002:a9fe:a9fe::": true,
		// public
		"1.1.1.1": false, "8.8.8.8": false, "93.184.216.34": false, "100.63.255.255": false, "100.128.0.1": false,
		"172.32.0.1": false, "198.20.0.1": false, "223.255.255.254": false,
		"2606:4700:4700::1111": false, "::ffff:8.8.8.8": false, "64:ff9b::808:808": false, "2002:808:808::1": false,
		"2a00:1450:4001::200e": false,
	}
	for s, want := range cases {
		if got := IsPrivateAddr(netip.MustParseAddr(s)); got != want {
			t.Errorf("IsPrivateAddr(%s) = %v, want %v", s, got, want)
		}
	}
	if !IsPrivateAddr(netip.Addr{}) {
		t.Error("invalid address must be restricted")
	}
}

func TestIsRestrictedHostName(t *testing.T) {
	cases := map[string]bool{
		"localhost": true, "LOCALHOST.": true, "foo.localhost": true, "printer.local": true, "metadata.google.internal": true,
		"router.home.arpa": true, "nas.lan": true, "router": true, "2130706433": true, "0x7f.1": true, "127.1": true,
		"010.0.0.1": true, "127.0.0.1": true, "[::1]": true, "169.254.169.254": true, "": true,
		"ice1.somafm.com": false, "stream.example.org": false, "8.8.8.8": false, "[2606:4700:4700::1111]": false,
		"radio.1live.de": false,
	}
	for h, want := range cases {
		if got := IsRestrictedHostName(h); got != want {
			t.Errorf("IsRestrictedHostName(%q) = %v, want %v", h, got, want)
		}
	}
}

func TestCheckHostPublic(t *testing.T) {
	fake := func(m map[string][]string) LookupFunc {
		return func(_ context.Context, host string) ([]netip.Addr, error) {
			v, ok := m[host]
			if !ok {
				return nil, errors.New("nxdomain")
			}
			var out []netip.Addr
			for _, s := range v {
				out = append(out, netip.MustParseAddr(s))
			}
			return out, nil
		}
	}
	lookup := fake(map[string][]string{
		"public.example":  {"93.184.216.34"},
		"rebind.example":  {"93.184.216.34", "127.0.0.1"},
		"meta.example":    {"169.254.169.254"},
		"nat64.example":   {"64:ff9b::a00:1"},
		"public6.example": {"2606:4700:4700::1111"},
	})
	ok := []string{"http://public.example/s", "https://public6.example:8443/x", "http://8.8.8.8/s"}
	bad := []string{"http://rebind.example/", "http://meta.example/", "http://nat64.example/", "http://missing.example/",
		"http://127.0.0.1:8080/", "http://localhost/", "http://[::1]/", "file:///etc/passwd", "http:///x", "http://router/"}
	for _, u := range ok {
		if err := CheckHostPublic(context.Background(), u, lookup); err != nil {
			t.Errorf("%s: %v", u, err)
		}
	}
	for _, u := range bad {
		if err := CheckHostPublic(context.Background(), u, lookup); !errors.Is(err, ErrBlockedDestination) {
			t.Errorf("%s: err = %v, want refusal", u, err)
		}
	}
}
