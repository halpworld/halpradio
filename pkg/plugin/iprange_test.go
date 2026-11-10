package plugin

import (
	"net"
	"strings"
	"testing"
)

// RR-C-5: IPv6 transition and legacy forms that reach internal IPv4, and
// the IPv4 special-purpose ranges, are treated as non-public.
func TestRestrictedAddressTable(t *testing.T) {
	cases := []struct {
		ip         string
		restricted bool
	}{
		// IPv4 special-purpose
		{"0.0.0.0", true},
		{"0.1.2.3", true},
		{"127.0.0.1", true},
		{"10.1.2.3", true},
		{"100.64.0.1", true},
		{"100.127.255.254", true},
		{"169.254.169.254", true},
		{"172.16.0.1", true},
		{"192.168.1.1", true},
		{"198.18.0.1", true},
		{"198.19.255.255", true},
		{"192.88.99.1", true},
		{"224.0.0.1", true},
		{"239.255.255.250", true},
		{"255.255.255.255", true},
		{"240.0.0.1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"100.128.0.1", false},
		{"198.20.0.1", false},
		// IPv4-mapped
		{"::ffff:127.0.0.1", true},
		{"::ffff:10.0.0.1", true},
		{"::ffff:8.8.8.8", false},
		// IPv4-compatible (deprecated): refused whatever it embeds
		{"::127.0.0.1", true},
		{"::8.8.8.8", true},
		// NAT64 well-known prefix: judged by the embedded IPv4
		{"64:ff9b::127.0.0.1", true},
		{"64:ff9b::7f00:1", true},
		{"64:ff9b::a9fe:a9fe", true}, // 169.254.169.254
		{"64:ff9b::c0a8:101", true},  // 192.168.1.1
		{"64:ff9b::808:808", false},  // 8.8.8.8 via DNS64
		// Local-use NAT64: refused
		{"64:ff9b:1::808:808", true},
		// 6to4: judged by the embedded IPv4
		{"2002:7f00:1::1", true},
		{"2002:a00:1::1", true},
		{"2002:a9fe:a9fe::1", true},
		{"2002:808:808::1", false},
		// Teredo, site-local, other IPv6 special-purpose
		{"2001:0:4136:e378:8000:63bf:3fff:fdd2", true},
		{"fec0::1", true},
		{"fe80::1", true},
		{"fd00::1", true},
		{"ff02::1", true},
		{"::1", true},
		{"::", true},
		{"100::1", true},
		{"2001:db8::1", true},
		{"2606:4700:4700::1111", false},
		{"2001:4860:4860::8888", false},
	}
	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", tc.ip)
		}
		if got := IsPrivateOrLoopbackIP(ip); got != tc.restricted {
			t.Errorf("IsPrivateOrLoopbackIP(%s) = %v, want %v", tc.ip, got, tc.restricted)
		}
		// Under "*" a restricted address must never be allowed, at dial
		// time (IsIPAllowed) or as a URL literal (CanAccessNetwork).
		star := PermissionsConfig{Network: []string{"*"}}
		if got := star.IsIPAllowed(ip); got == tc.restricted {
			t.Errorf(`"*" IsIPAllowed(%s) = %v`, tc.ip, got)
		}
		host := tc.ip
		if strings.Contains(tc.ip, ":") {
			host = "[" + tc.ip + "]"
		}
		if got := star.CanAccessNetwork("https://" + host + "/x"); got == tc.restricted {
			t.Errorf(`"*" CanAccessNetwork(%s) = %v`, host, got)
		}
	}
}

// A broad rule over a transition prefix does not unlock the internal IPv4
// addresses it embeds.
func TestTransitionPrefixRulesStayPublic(t *testing.T) {
	for _, rule := range []string{"64:ff9b::/96", "2002::/16", "::/0"} {
		p := PermissionsConfig{Network: []string{rule}}
		for _, target := range []string{"64:ff9b::7f00:1", "2002:7f00:1::1", "2002:a9fe:a9fe::1"} {
			if p.IsIPAllowed(net.ParseIP(target)) {
				t.Errorf("rule %s allowed internal target %s", rule, target)
			}
		}
	}
}
