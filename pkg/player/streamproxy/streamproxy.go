// Package streamproxy is a small in-process HTTP forward proxy that external
// players (mpv, ffplay, ffmpeg, mpg123, vlc, mplayer) are started with when
// they play a station that may not reach private addresses.
//
// External players resolve host names, follow redirects, read playlists and
// fetch HLS segments on their own, so halpradio's Go-side address checks do
// not see those requests. Routed through this proxy, every connection the
// player makes (first request, each redirect hop, each playlist entry and
// segment, each https CONNECT) is dialled here with the strict
// streamnet dialer, which checks the resolved address at connect time. DNS
// rebinding is covered for the same reason.
//
// The proxy listens on 127.0.0.1 on a random port, requires a random
// per-session Basic credential, forwards only GET and HEAD for absolute
// http URLs and CONNECT, never follows redirects itself, strips hop-by-hop
// and Proxy-* headers, bounds concurrent connections, header size and idle
// time, and cannot reach loopback (including itself).
package streamproxy

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/halpworld/halpradio/pkg/debuglog"
	"github.com/halpworld/halpradio/pkg/player/streamnet"
)

// Defaults for Options fields left zero.
const (
	DefaultMaxConns       = 16
	DefaultHeaderTimeout  = 10 * time.Second
	DefaultIdleTimeout    = 60 * time.Second
	DefaultUpstreamIdle   = 30 * time.Second
	DefaultMaxHeaderBytes = 16 << 10
	dialTimeout           = 10 * time.Second
	proxyUser             = "halpradio"
)

// Options configures Start. Zero values select the defaults.
type Options struct {
	// Dial makes every outbound connection. nil selects the strict
	// streamnet dialer (no restricted address, checked after resolution).
	// Tests inject a dialer to simulate public hosts on loopback.
	Dial streamnet.DialFunc
	// MaxConns bounds concurrently accepted client connections.
	MaxConns int
	// HeaderTimeout bounds reading a request's line and headers.
	HeaderTimeout time.Duration
	// IdleTimeout closes an idle keep-alive client connection.
	IdleTimeout time.Duration
	// UpstreamIdle closes an upstream connection or tunnel that moved no
	// bytes for this long.
	UpstreamIdle time.Duration
	// MaxHeaderBytes bounds the request line plus headers.
	MaxHeaderBytes int
}

// quiet discards net/http's own error logging, which would otherwise write
// to stderr underneath the TUI.
var quiet = log.New(io.Discard, "", 0)

// Proxy is one running proxy. It is safe for concurrent use.
type Proxy struct {
	ln        net.Listener
	srv       *http.Server
	transport *http.Transport
	rp        *httputil.ReverseProxy
	dial      streamnet.DialFunc
	wantAuth  []byte
	pass      string
	upIdle    time.Duration
	baseCtx   context.Context
	cancel    context.CancelFunc
	served    chan struct{}

	mu      sync.Mutex
	closed  bool
	tunnels map[net.Conn]struct{}
	wg      sync.WaitGroup
}

// Start listens on 127.0.0.1:0 and serves until Close.
func Start(o Options) (*Proxy, error) {
	if o.Dial == nil {
		o.Dial = streamnet.NewDialer(streamnet.Options{DialTimeout: dialTimeout})
	}
	if o.MaxConns <= 0 {
		o.MaxConns = DefaultMaxConns
	}
	if o.HeaderTimeout <= 0 {
		o.HeaderTimeout = DefaultHeaderTimeout
	}
	if o.IdleTimeout <= 0 {
		o.IdleTimeout = DefaultIdleTimeout
	}
	if o.UpstreamIdle <= 0 {
		o.UpstreamIdle = DefaultUpstreamIdle
	}
	if o.MaxHeaderBytes <= 0 {
		o.MaxHeaderBytes = DefaultMaxHeaderBytes
	}

	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, fmt.Errorf("streamproxy: credential: %w", err)
	}
	pass := hex.EncodeToString(raw[:])

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("streamproxy: listen: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	p := &Proxy{
		ln:       &limitListener{Listener: ln, sem: make(chan struct{}, o.MaxConns)},
		dial:     o.Dial,
		wantAuth: []byte("Basic " + base64.StdEncoding.EncodeToString([]byte(proxyUser+":"+pass))),
		pass:     pass,
		upIdle:   o.UpstreamIdle,
		baseCtx:  ctx,
		cancel:   cancel,
		served:   make(chan struct{}),
		tunnels:  map[net.Conn]struct{}{},
	}
	p.transport = &http.Transport{
		Proxy:                 nil,
		DialContext:           p.dialUpstream,
		ResponseHeaderTimeout: 15 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
		DisableCompression:    true,
		ForceAttemptHTTP2:     false,
	}
	p.rp = &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			u := *pr.In.URL
			u.User = nil
			pr.Out.URL = &u
			pr.Out.Host = u.Host
			stripProxyHeaders(pr.Out.Header)
		},
		Transport:     p.transport,
		FlushInterval: -1,
		ModifyResponse: func(r *http.Response) error {
			stripProxyHeaders(r.Header)
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			debuglog.Logf("streamproxy", "GET %s refused: %s", debuglog.RedactURL(r.URL.String()), debuglog.RedactText(err.Error()))
			status := http.StatusBadGateway
			if errors.Is(err, streamnet.ErrBlockedDestination) {
				status = http.StatusForbidden
			}
			w.WriteHeader(status)
		},
		ErrorLog: quiet,
	}
	p.srv = &http.Server{
		Handler:           p,
		ReadHeaderTimeout: o.HeaderTimeout,
		IdleTimeout:       o.IdleTimeout,
		MaxHeaderBytes:    o.MaxHeaderBytes,
		BaseContext:       func(net.Listener) context.Context { return ctx },
		ErrorLog:          quiet,
	}
	go func() {
		defer close(p.served)
		_ = p.srv.Serve(p.ln)
	}()
	return p, nil
}

// Addr is the proxy's host:port.
func (p *Proxy) Addr() string { return p.ln.Addr().String() }

// URL is the proxy URL with the session credential, for player options and
// the http_proxy environment variable.
func (p *Proxy) URL() string {
	return "http://" + proxyUser + ":" + p.pass + "@" + p.Addr()
}

// Close stops the listener, every client connection and tunnel, and waits
// for all handlers to return. It is idempotent.
func (p *Proxy) Close() error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil
	}
	p.closed = true
	for c := range p.tunnels {
		_ = c.Close()
	}
	p.mu.Unlock()

	p.cancel()
	err := p.srv.Close()
	<-p.served
	p.wg.Wait()
	p.transport.CloseIdleConnections()
	return err
}

func (p *Proxy) begin() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	p.wg.Add(1)
	return true
}

// ServeHTTP implements the proxy.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !p.begin() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	defer p.wg.Done()

	got := []byte(r.Header.Get("Proxy-Authorization"))
	if subtle.ConstantTimeCompare(got, p.wantAuth) != 1 {
		w.Header().Set("Proxy-Authenticate", `Basic realm="halpradio"`)
		w.WriteHeader(http.StatusProxyAuthRequired)
		return
	}

	switch r.Method {
	case http.MethodConnect:
		p.serveConnect(w, r)
	case http.MethodGet, http.MethodHead:
		if r.URL == nil || !r.URL.IsAbs() || r.URL.Scheme != "http" || r.URL.Host == "" {
			http.Error(w, "absolute http URL required", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Upgrade") != "" {
			http.Error(w, "upgrade not supported", http.StatusBadRequest)
			return
		}
		debuglog.Logf("streamproxy", "%s %s", r.Method, debuglog.RedactURL(r.URL.String()))
		p.rp.ServeHTTP(w, r)
	default:
		w.Header().Set("Allow", "GET, HEAD, CONNECT")
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (p *Proxy) serveConnect(w http.ResponseWriter, r *http.Request) {
	target := r.Host
	host, port, err := net.SplitHostPort(target)
	if err != nil || host == "" {
		http.Error(w, "CONNECT needs host:port", http.StatusBadRequest)
		return
	}
	if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
		http.Error(w, "bad port", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), dialTimeout)
	up, err := p.dialUpstream(ctx, "tcp", target)
	cancel()
	if err != nil {
		debuglog.Logf("streamproxy", "CONNECT %s refused: %s", host, debuglog.RedactText(err.Error()))
		status := http.StatusBadGateway
		if errors.Is(err, streamnet.ErrBlockedDestination) {
			status = http.StatusForbidden
		}
		w.WriteHeader(status)
		return
	}
	client, brw, err := http.NewResponseController(w).Hijack()
	if err != nil {
		_ = up.Close()
		return
	}
	debuglog.Logf("streamproxy", "CONNECT %s", host)
	if !p.track(client, up) {
		_ = client.Close()
		_ = up.Close()
		return
	}
	defer p.untrack(client, up)

	_ = client.SetDeadline(time.Time{})
	if _, err := io.WriteString(client, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}
	var fromClient io.Reader = client
	if brw != nil && brw.Reader.Buffered() > 0 {
		fromClient = io.MultiReader(brw.Reader, client)
	}
	idleClient := &idleConn{Conn: client, idle: p.upIdle}
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(up, readerFunc(func(b []byte) (int, error) {
			_ = client.SetReadDeadline(time.Now().Add(p.upIdle))
			return fromClient.Read(b)
		}))
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(idleClient, up)
		done <- struct{}{}
	}()
	<-done
	_ = client.Close()
	_ = up.Close()
	<-done
}

func (p *Proxy) track(cs ...net.Conn) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return false
	}
	for _, c := range cs {
		p.tunnels[c] = struct{}{}
	}
	return true
}

func (p *Proxy) untrack(cs ...net.Conn) {
	p.mu.Lock()
	for _, c := range cs {
		delete(p.tunnels, c)
		_ = c.Close()
	}
	p.mu.Unlock()
}

// dialUpstream dials through the policy dialer and wraps the connection
// with an idle deadline and the ICY status-line fix.
func (p *Proxy) dialUpstream(ctx context.Context, network, addr string) (net.Conn, error) {
	c, err := p.dial(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return &icyConn{idleConn: idleConn{Conn: c, idle: p.upIdle}}, nil
}

// hopHeaders are removed in both directions in addition to the ones
// httputil.ReverseProxy already drops and any header named in Connection.
func stripProxyHeaders(h http.Header) {
	for k := range h {
		if strings.HasPrefix(strings.ToLower(k), "proxy-") {
			delete(h, k)
		}
	}
	for _, k := range []string{"Connection", "Keep-Alive", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
		h.Del(k)
	}
}

type readerFunc func([]byte) (int, error)

func (f readerFunc) Read(b []byte) (int, error) { return f(b) }

// idleConn refreshes the deadline before every read and write, so a
// connection that moves no bytes for idle is closed.
type idleConn struct {
	net.Conn
	idle time.Duration
}

func (c *idleConn) Read(b []byte) (int, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(c.idle))
	return c.Conn.Read(b)
}

func (c *idleConn) Write(b []byte) (int, error) {
	_ = c.Conn.SetWriteDeadline(time.Now().Add(c.idle))
	return c.Conn.Write(b)
}

// icyConn rewrites a SHOUTcast v1 "ICY 200 OK" status line to "HTTP/1.0 200
// OK" so Go's HTTP client accepts it; the player then sees an ordinary
// HTTP/1.0 response with the icy-* headers intact.
type icyConn struct {
	idleConn
	once    bool
	pending []byte
}

func (c *icyConn) Read(b []byte) (int, error) {
	if !c.once {
		c.once = true
		br := bufio.NewReaderSize(&c.idleConn, 16)
		head := make([]byte, 0, 4)
		for len(head) < 4 {
			ch, err := br.ReadByte()
			if err != nil {
				c.pending = head
				break
			}
			head = append(head, ch)
		}
		if string(head) == "ICY " {
			head = []byte("HTTP/1.0 ")
		}
		rest := make([]byte, br.Buffered())
		_, _ = io.ReadFull(br, rest)
		c.pending = append(head, rest...)
	}
	if len(c.pending) > 0 {
		n := copy(b, c.pending)
		c.pending = c.pending[n:]
		return n, nil
	}
	return c.idleConn.Read(b)
}

// limitListener bounds concurrently open accepted connections.
type limitListener struct {
	net.Listener
	sem chan struct{}
}

func (l *limitListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		select {
		case l.sem <- struct{}{}:
			return &limitConn{Conn: c, release: func() { <-l.sem }}, nil
		default:
			_ = c.Close() // over the limit: refuse rather than queue
		}
	}
}

type limitConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *limitConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}
