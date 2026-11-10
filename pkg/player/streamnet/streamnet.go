// Package streamnet builds HTTP clients for fetching untrusted stream URLs
// with one shared destination policy:
//
//   - only http and https, on the first request and on every redirect;
//   - at most MaxRedirects redirects;
//   - with Options.AllowPrivate false (the default) every dial to a
//     restricted address (IsPrivateAddr: loopback, RFC 1918, unique-local,
//     link-local including 169.254.169.254, CGNAT, multicast, reserved,
//     NAT64/6to4/IPv4-mapped forms of those) is refused, including the
//     first. The check runs on the resolved IP at connect time, so DNS names
//     that resolve to such addresses and DNS rebinding are covered.
//   - with AllowPrivate true (stations the user added) the class of the
//     first dialled address decides: a LAN station keeps working, but a
//     public first hop can still never reach a restricted address.
//
// The client policy applies to Go HTTP clients. External player processes
// for stations that may not reach private hosts are routed through
// pkg/player/streamproxy, which dials with NewDialer; CheckHostPublic is a
// fast pre-spawn check in front of that.
package streamnet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"
)

// MaxRedirects is the redirect cap for stream fetches.
const MaxRedirects = 5

// ErrBlockedDestination is returned (wrapped) when a connection or redirect
// is refused by the policy.
var ErrBlockedDestination = errors.New("blocked stream destination")

// Options configures NewClient. Zero values select the defaults used by the
// player (10 s dial, TLS and response-header timeouts; no overall timeout,
// since streams are endless).
type Options struct {
	DialTimeout           time.Duration
	KeepAlive             time.Duration
	TLSHandshakeTimeout   time.Duration
	ResponseHeaderTimeout time.Duration
	// Timeout is http.Client.Timeout (0 = none).
	Timeout time.Duration
	// AllowPrivate permits a session whose first dialled address is
	// restricted (a station the user added on their LAN). When false every
	// restricted address is refused.
	AllowPrivate bool
}

func mustPrefixes(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(ss))
	for i, x := range ss {
		out[i] = netip.MustParsePrefix(x)
	}
	return out
}

// restricted4 lists IPv4 ranges that are never a public stream server.
var restricted4 = mustPrefixes(
	"0.0.0.0/8",       // "this network"
	"10.0.0.0/8",      // RFC 1918
	"100.64.0.0/10",   // CGNAT (incl. 100.100.100.200 metadata)
	"127.0.0.0/8",     // loopback
	"169.254.0.0/16",  // link-local (incl. 169.254.169.254 metadata)
	"172.16.0.0/12",   // RFC 1918
	"192.0.0.0/24",    // IETF protocol assignments
	"192.0.2.0/24",    // TEST-NET-1
	"192.168.0.0/16",  // RFC 1918
	"198.18.0.0/15",   // benchmarking
	"198.51.100.0/24", // TEST-NET-2
	"203.0.113.0/24",  // TEST-NET-3
	"224.0.0.0/4",     // multicast
	"240.0.0.0/4",     // reserved, incl. 255.255.255.255 broadcast
)

// restricted6 lists IPv6 ranges that are restricted as a whole.
var restricted6 = mustPrefixes(
	"::/128",         // unspecified
	"::1/128",        // loopback
	"100::/64",       // discard-only
	"2001::/32",      // Teredo (embedded client address is obfuscated)
	"2001:db8::/32",  // documentation
	"64:ff9b:1::/48", // local-use NAT64 (RFC 8215); embedding varies
	"fc00::/7",       // unique-local
	"fe80::/10",      // link-local
	"fec0::/10",      // deprecated site-local
	"ff00::/8",       // multicast
)

var (
	compat4 = netip.MustParsePrefix("::/96")        // IPv4-compatible
	nat64   = netip.MustParsePrefix("64:ff9b::/96") // well-known NAT64
	sixTo4  = netip.MustParsePrefix("2002::/16")    // 6to4
)

// IsPrivateAddr reports whether ip must not be reached by a stream from an
// untrusted source: anything that is not public unicast. IPv4-mapped,
// NAT64 (64:ff9b::/96) and 6to4 (2002::/16) addresses are decoded and their
// embedded IPv4 address is checked too; deprecated IPv4-compatible (::/96)
// addresses are always restricted. An invalid address is restricted.
func IsPrivateAddr(ip netip.Addr) bool {
	ip = ip.WithZone("").Unmap()
	if !ip.IsValid() {
		return true
	}
	if ip.Is4() {
		for _, p := range restricted4 {
			if p.Contains(ip) {
				return true
			}
		}
		return false
	}
	for _, p := range restricted6 {
		if p.Contains(ip) {
			return true
		}
	}
	b := ip.As16()
	switch {
	case compat4.Contains(ip):
		// Deprecated (RFC 4291) and never routed publicly: always refused.
		return true
	case nat64.Contains(ip):
		return IsPrivateAddr(netip.AddrFrom4([4]byte{b[12], b[13], b[14], b[15]}))
	case sixTo4.Contains(ip):
		return IsPrivateAddr(netip.AddrFrom4([4]byte{b[2], b[3], b[4], b[5]}))
	}
	return false
}

// IsRestrictedHostName reports whether host (a URL host without port,
// brackets allowed) names a local destination by its form alone: an IP
// literal that IsPrivateAddr rejects; localhost and *.localhost; the
// local-only suffixes .local, .internal, .home.arpa, .lan, .intranet;
// single-label names (resolved through the local search domain); and
// numeric forms that are not a canonical IP literal (2130706433, 0x7f.1,
// 127.1), which some resolvers read as loopback.
func IsRestrictedHostName(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if h == "" {
		return true
	}
	if ip, err := netip.ParseAddr(h); err == nil {
		return IsPrivateAddr(ip)
	}
	if h == "localhost" || !strings.Contains(h, ".") {
		return true
	}
	for _, suf := range []string{".localhost", ".local", ".internal", ".home.arpa", ".lan", ".intranet"} {
		if strings.HasSuffix(h, suf) {
			return true
		}
	}
	last := h[strings.LastIndexByte(h, '.')+1:]
	if isNumericLabel(last) {
		return true // not a canonical IP literal, but numeric: ambiguous
	}
	return false
}

func isNumericLabel(l string) bool {
	if l == "" {
		return false
	}
	if strings.HasPrefix(l, "0x") {
		l = l[2:]
		for _, r := range l {
			if !strings.ContainsRune("0123456789abcdef", r) {
				return false
			}
		}
		return true
	}
	for _, r := range l {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// LookupFunc resolves a host name; tests replace it.
type LookupFunc func(ctx context.Context, host string) ([]netip.Addr, error)

// defaultLookup uses the system resolver.
func defaultLookup(ctx context.Context, host string) ([]netip.Addr, error) {
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

// CheckHostPublic is the pre-spawn check for external players: it refuses
// rawURL unless it is http/https, its host is not restricted by form, and
// every address the host resolves to is public. A lookup failure refuses.
// It cannot stop the player's own later lookup from getting a different
// answer (DNS rebinding) or the player from following a redirect.
func CheckHostPublic(ctx context.Context, rawURL string, lookup LookupFunc) error {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return fmt.Errorf("%w: not an http(s) URL with a host", ErrBlockedDestination)
	}
	host := u.Hostname()
	if IsRestrictedHostName(host) {
		return fmt.Errorf("%w: %s is a local or private host", ErrBlockedDestination, host)
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return nil // public literal
	}
	if lookup == nil {
		lookup = defaultLookup
	}
	addrs, err := lookup(ctx, host)
	if err != nil {
		return fmt.Errorf("%w: cannot resolve %s: %v", ErrBlockedDestination, host, err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("%w: %s has no addresses", ErrBlockedDestination, host)
	}
	for _, a := range addrs {
		if IsPrivateAddr(a) {
			return fmt.Errorf("%w: %s resolves to private address %s", ErrBlockedDestination, host, a)
		}
	}
	return nil
}

// guard enforces the address policy for one client. With strict set every
// restricted address is refused; otherwise the class of the first dialled
// address decides.
type guard struct {
	mu           sync.Mutex
	strict       bool
	decided      bool
	allowPrivate bool
}

func (g *guard) check(ip netip.Addr) error {
	priv := IsPrivateAddr(ip)
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.strict {
		if priv {
			return fmt.Errorf("%w: %s is a private address", ErrBlockedDestination, ip)
		}
		return nil
	}
	if !g.decided {
		g.decided = true
		g.allowPrivate = priv
		return nil
	}
	if priv && !g.allowPrivate {
		return fmt.Errorf("%w: %s is a private address reached from a public stream", ErrBlockedDestination, ip)
	}
	return nil
}

func (g *guard) control(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBlockedDestination, err)
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("%w: unparseable address %q", ErrBlockedDestination, host)
	}
	return g.check(ip)
}

// CheckRedirect enforces the scheme allow-list and the redirect cap. The
// address class of the redirect target is enforced at dial time.
func CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= MaxRedirects {
		return fmt.Errorf("%w: more than %d redirects", ErrBlockedDestination, MaxRedirects)
	}
	if req.URL == nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
		return fmt.Errorf("%w: redirect to non-http(s) URL", ErrBlockedDestination)
	}
	if req.URL.Host == "" {
		return fmt.Errorf("%w: redirect without host", ErrBlockedDestination)
	}
	return nil
}

// NewClient returns an HTTP client for one stream session (one station
// play, one ICY listener, one fingerprint capture). Do not share a client
// across stations: with AllowPrivate the first dialled address fixes the
// policy for it.
func NewClient(o Options) *http.Client {
	if o.DialTimeout == 0 {
		o.DialTimeout = 10 * time.Second
	}
	if o.KeepAlive == 0 {
		o.KeepAlive = 30 * time.Second
	}
	if o.TLSHandshakeTimeout == 0 {
		o.TLSHandshakeTimeout = 10 * time.Second
	}
	if o.ResponseHeaderTimeout == 0 {
		o.ResponseHeaderTimeout = 10 * time.Second
	}
	return newClient(o, &guard{strict: !o.AllowPrivate})
}

// DialFunc is the signature of net.Dialer.DialContext.
type DialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// NewDialer returns a dial function with the same address policy as
// NewClient: with AllowPrivate false every connection to a restricted
// address is refused at connect time, after DNS resolution. It is used by
// the in-process proxy that external players are routed through.
func NewDialer(o Options) DialFunc {
	if o.DialTimeout == 0 {
		o.DialTimeout = 10 * time.Second
	}
	if o.KeepAlive == 0 {
		o.KeepAlive = 30 * time.Second
	}
	g := &guard{strict: !o.AllowPrivate}
	return (&net.Dialer{Timeout: o.DialTimeout, KeepAlive: o.KeepAlive, Control: g.control}).DialContext
}

func newClient(o Options, g *guard) *http.Client {
	transport := &http.Transport{
		// No proxy: the dial check must see the real destination.
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout:   o.DialTimeout,
			KeepAlive: o.KeepAlive,
			Control:   g.control,
		}).DialContext,
		TLSHandshakeTimeout:   o.TLSHandshakeTimeout,
		ResponseHeaderTimeout: o.ResponseHeaderTimeout,
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: CheckRedirect,
		Timeout:       o.Timeout,
	}
}
