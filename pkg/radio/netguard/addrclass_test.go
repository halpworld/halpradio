package netguard

import (
	"net"
	"testing"
)

// RR-E-1: the metadata clients use the same restricted-address table as the
// stream player.
func TestIsRestrictedIP_Table(t *testing.T) {
	cases := map[string]bool{
		"0.0.0.0": true, "10.1.2.3": true, "100.64.0.1": true, "127.0.0.1": true, "169.254.169.254": true,
		"172.16.0.1": true, "192.0.0.8": true, "192.168.1.1": true, "198.18.0.1": true, "224.0.0.1": true,
		"240.0.0.1": true, "255.255.255.255": true,
		"::": true, "::1": true, "fe80::1": true, "fd00::1": true, "fec0::1": true, "ff02::1": true, "2001::1": true,
		"::ffff:127.0.0.1": true, "::127.0.0.1": true, "64:ff9b::7f00:1": true, "64:ff9b:1::7f00:1": true,
		"2002:7f00:1::1": true, "2002:a9fe:a9fe::": true,
		"1.1.1.1": false, "8.8.8.8": false, "2606:4700:4700::1111": false, "::ffff:8.8.8.8": false,
		"64:ff9b::808:808": false, "2002:808:808::1": false,
	}
	for s, want := range cases {
		if got := IsRestrictedIP(net.ParseIP(s)); got != want {
			t.Errorf("IsRestrictedIP(%s) = %v, want %v", s, got, want)
		}
	}
	if !IsRestrictedIP(nil) {
		t.Error("nil must be restricted")
	}
}
