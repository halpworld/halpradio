// Package netguard holds the outbound HTTP policy shared by halpradio's remote
// data clients (station directories, the station catalog, lyrics and cover
// art): which URLs may be fetched, which redirects may be followed, which
// addresses may be dialled, how much of a response body is read, and how
// often a provider may be asked.
//
// The policy is deliberately local to the packages that fetch metadata; the
// audio player keeps its own policy for stream URLs.
package netguard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/halpworld/halpradio/pkg/player/streamnet"
)

// MaxRedirects is the number of redirect hops a guarded client follows.
const MaxRedirects = 3

// ErrRestricted is returned when a request would reach a loopback, private,
// link-local, multicast or otherwise non-public address from a public origin.
var ErrRestricted = errors.New("netguard: destination address is not public")

// ErrTooLarge is returned by ReadLimited when a body exceeds its limit.
var ErrTooLarge = errors.New("netguard: response body exceeds size limit")

type originKey struct{}

// WithOrigin records on ctx where a request chain starts. When origin is
// itself a loopback or private address (a local test server, a developer's
// mirror on localhost) the chain may stay on such addresses; otherwise every
// dial, including every redirect hop, must reach a public address.
func WithOrigin(ctx context.Context, origin string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	allow := false
	if u, err := url.Parse(origin); err == nil {
		allow = isRestrictedHostLiteral(u.Hostname())
	}
	return context.WithValue(ctx, originKey{}, allow)
}

func privateAllowed(ctx context.Context) bool {
	v, _ := ctx.Value(originKey{}).(bool)
	return v
}

// CheckURL parses raw and accepts only absolute http/https URLs with a host,
// without embedded credentials, control characters or whitespace.
func CheckURL(raw string) (*url.URL, error) {
	if raw == "" || len(raw) > 4096 {
		return nil, fmt.Errorf("netguard: empty or oversized URL")
	}
	for _, r := range raw {
		if r <= 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return nil, fmt.Errorf("netguard: URL contains control or space characters")
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("netguard: invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("netguard: scheme %q not allowed", u.Scheme)
	}
	if u.Hostname() == "" {
		return nil, fmt.Errorf("netguard: URL has no host")
	}
	if u.User != nil {
		return nil, fmt.Errorf("netguard: URL carries credentials")
	}
	return u, nil
}

// CheckRedirect is an http.Client.CheckRedirect policy: at most MaxRedirects
// hops, http/https only, no https to http downgrade, and no hop from a public
// origin to an address literal that is loopback, private or link-local.
// Host names are checked again at dial time by the guarded transport.
func CheckRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= MaxRedirects {
		return fmt.Errorf("netguard: stopped after %d redirects", len(via))
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return fmt.Errorf("netguard: redirect to scheme %q refused", req.URL.Scheme)
	}
	if req.URL.User != nil {
		return fmt.Errorf("netguard: redirect carrying credentials refused")
	}
	if len(via) > 0 {
		prev := via[len(via)-1].URL
		if prev.Scheme == "https" && req.URL.Scheme != "https" {
			return fmt.Errorf("netguard: https to http redirect refused")
		}
		origin := via[0].URL
		if !isRestrictedHostLiteral(origin.Hostname()) && isRestrictedHostLiteral(req.URL.Hostname()) {
			return ErrRestricted
		}
	}
	return nil
}

// NewClient returns an http.Client with the given timeout, the redirect
// policy above, and a dialer that resolves host names itself and refuses
// non-public addresses unless the request context was marked by WithOrigin
// with a non-public origin. A configured HTTP(S)_PROXY is still honoured.
func NewClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	proxies := proxyAddrs()
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.Proxy = http.ProxyFromEnvironment
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if proxies[addr] || privateAllowed(ctx) {
			return dialer.DialContext(ctx, network, addr)
		}
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if isRestrictedHostLiteral(host) {
			return nil, ErrRestricted
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		var lastErr error = ErrRestricted
		for _, ip := range ips {
			if IsRestrictedIP(ip.IP) {
				continue
			}
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			lastErr = err
		}
		return nil, lastErr
	}
	return &http.Client{Timeout: timeout, Transport: tr, CheckRedirect: CheckRedirect}
}

// proxyAddrs lists the host:port of any proxy configured in the environment,
// so the dial guard does not refuse a proxy that runs on localhost.
func proxyAddrs() map[string]bool {
	out := map[string]bool{}
	for _, target := range []string{"http://example.com/", "https://example.com/"} {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			continue
		}
		p, err := http.ProxyFromEnvironment(req)
		if err != nil || p == nil {
			continue
		}
		port := p.Port()
		if port == "" {
			switch p.Scheme {
			case "https":
				port = "443"
			case "socks5", "socks5h":
				port = "1080"
			default:
				port = "80"
			}
		}
		out[net.JoinHostPort(p.Hostname(), port)] = true
	}
	return out
}

// IsRestrictedIP reports whether ip is not a public unicast address. It
// applies the same table as the stream player (streamnet.IsPrivateAddr):
// loopback, RFC 1918 / unique-local, link-local (including the cloud
// metadata address 169.254.169.254), unspecified, multicast, CGNAT, 0/8,
// 192.0.0.0/24, 198.18/15, 240/4 and broadcast, documentation ranges,
// site-local, Teredo, local-use NAT64, and the IPv4 address embedded in
// IPv4-mapped, IPv4-compatible, NAT64 and 6to4 addresses.
func IsRestrictedIP(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	return streamnet.IsPrivateAddr(a)
}

// isRestrictedHostLiteral reports whether host is "localhost" (or a name
// under .localhost) or an IP literal that IsRestrictedIP rejects.
func isRestrictedHostLiteral(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	if i := strings.IndexByte(h, '%'); i >= 0 { // IPv6 zone
		h = h[:i]
	}
	if ip := net.ParseIP(h); ip != nil {
		return IsRestrictedIP(ip)
	}
	return false
}

// ReadLimited reads all of r but fails with ErrTooLarge instead of silently
// truncating when more than limit bytes arrive.
func ReadLimited(r io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}
	return data, nil
}

// Limiter is a per-provider token bucket: each provider may make Burst
// lookups at once and then one more every Interval. It stops a station that
// rotates its track title every second from turning halpradio into a request
// flood against lyrics and artwork providers.
type Limiter struct {
	Burst    int
	Interval time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewLimiter returns a Limiter allowing burst lookups, refilled at one per
// interval.
func NewLimiter(burst int, interval time.Duration) *Limiter {
	return &Limiter{Burst: burst, Interval: interval}
}

// Allow reports whether provider may make a lookup now, consuming a token
// when it may. A nil Limiter, or one with Interval <= 0, always allows.
func (l *Limiter) Allow(provider string) bool {
	if l == nil || l.Interval <= 0 {
		return true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if l.now != nil {
		now = l.now()
	}
	burst := float64(max(l.Burst, 1))
	if l.buckets == nil {
		l.buckets = map[string]*bucket{}
	}
	b, ok := l.buckets[provider]
	if !ok {
		b = &bucket{tokens: burst, last: now}
		l.buckets[provider] = b
	}
	b.tokens = min(burst, b.tokens+float64(now.Sub(b.last))/float64(l.Interval))
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// SetClock replaces the limiter's clock; tests use it to avoid sleeping.
func (l *Limiter) SetClock(now func() time.Time) {
	l.mu.Lock()
	l.now = now
	l.mu.Unlock()
}
