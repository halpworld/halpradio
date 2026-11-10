package streamproxy

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/halpworld/halpradio/pkg/player/streamnet"
)

// fakePublic routes the made-up public names to loopback test servers and
// sends everything else through the real strict dialer, which refuses
// loopback, LAN and metadata addresses.
func fakePublic(t *testing.T, names map[string]string) streamnet.DialFunc {
	strict := streamnet.NewDialer(streamnet.Options{})
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, _, _ := net.SplitHostPort(addr)
		if real, ok := names[host]; ok {
			var d net.Dialer
			return d.DialContext(ctx, network, real)
		}
		return strict(ctx, network, addr)
	}
}

type counter struct{ n atomic.Int32 }

func (c *counter) handler(body string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		c.n.Add(1)
		_, _ = io.WriteString(w, body)
	})
}

func startProxy(t *testing.T, o Options) *Proxy {
	t.Helper()
	p, err := Start(o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func clientVia(t *testing.T, proxyURL string, follow bool) *http.Client {
	t.Helper()
	u, err := url.Parse(proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	c := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(u)}, Timeout: 5 * time.Second}
	if !follow {
		c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	return c
}

// internal is the "LAN service" every attack tries to reach.
func internal(t *testing.T) (*counter, string) {
	var c counter
	srv := httptest.NewServer(c.handler("secret"))
	t.Cleanup(srv.Close)
	return &c, srv.Listener.Addr().String()
}

func TestRedirectToPrivateIsRefused(t *testing.T) {
	hits, inAddr := internal(t)
	pub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redir":
			http.Redirect(w, r, "http://"+inAddr+"/secret", http.StatusFound)
		case "/redir-meta":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
		case "/redir-localhost":
			http.Redirect(w, r, "http://localhost:"+strings.Split(inAddr, ":")[1]+"/", http.StatusFound)
		}
	}))
	defer pub.Close()
	p := startProxy(t, Options{Dial: fakePublic(t, map[string]string{"radio.example": pub.Listener.Addr().String()})})

	// The proxy itself never follows: the 3xx reaches the player.
	resp, err := clientVia(t, p.URL(), false).Get("http://radio.example/redir")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusFound || hits.n.Load() != 0 {
		t.Fatalf("status %d hits %d: proxy must pass the redirect through untouched", resp.StatusCode, hits.n.Load())
	}
	// The player's follow-up request comes back through the proxy and is refused.
	for _, path := range []string{"/redir", "/redir-meta", "/redir-localhost"} {
		resp, err := clientVia(t, p.URL(), true).Get("http://radio.example" + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: final status %d, want 403", path, resp.StatusCode)
		}
	}
	if hits.n.Load() != 0 {
		t.Fatalf("internal service was reached %d times", hits.n.Load())
	}
}

// Playlist entries and HLS segments are separate requests from the player;
// every one of them is dialled by the proxy.
func TestPlaylistAndHLSEntriesToPrivateAreRefused(t *testing.T) {
	hits, inAddr := internal(t)
	pub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/pl.m3u":
			fmt.Fprintf(w, "#EXTM3U\nhttp://%s/secret-from-playlist\n", inAddr)
		case "/live.m3u8":
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXTINF:10,\nhttp://%s/seg0.ts\n#EXT-X-ENDLIST\n", inAddr)
		}
	}))
	defer pub.Close()
	p := startProxy(t, Options{Dial: fakePublic(t, map[string]string{"radio.example": pub.Listener.Addr().String()})})
	c := clientVia(t, p.URL(), true)
	for _, list := range []string{"/pl.m3u", "/live.m3u8"} {
		resp, err := c.Get("http://radio.example" + list)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		for _, line := range strings.Split(string(body), "\n") {
			if !strings.HasPrefix(line, "http://") {
				continue
			}
			r2, err := c.Get(line)
			if err != nil {
				t.Fatal(err)
			}
			r2.Body.Close()
			if r2.StatusCode != http.StatusForbidden {
				t.Errorf("%s entry %s: status %d, want 403", list, line, r2.StatusCode)
			}
		}
	}
	if hits.n.Load() != 0 {
		t.Fatalf("internal service was reached %d times", hits.n.Load())
	}
}

func rawRequest(t *testing.T, addr, req string) string {
	t.Helper()
	c, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(c, req); err != nil {
		t.Fatal(err)
	}
	line, _ := bufio.NewReader(c).ReadString('\n')
	return strings.TrimSpace(line)
}

func auth(p *Proxy) string {
	u, _ := url.Parse(p.URL())
	pw, _ := u.User.Password()
	r, _ := http.NewRequest("GET", "http://x/", nil)
	r.SetBasicAuth(u.User.Username(), pw)
	return "Proxy-Authorization: " + r.Header.Get("Authorization") + "\r\n"
}

func TestConnectToPrivateIsRefused(t *testing.T) {
	hits, inAddr := internal(t)
	p := startProxy(t, Options{})
	for _, target := range []string{inAddr, "169.254.169.254:80", "[::1]:443", "localhost:" + strings.Split(inAddr, ":")[1], "10.0.0.1:443"} {
		line := rawRequest(t, p.Addr(), "CONNECT "+target+" HTTP/1.1\r\nHost: "+target+"\r\n"+auth(p)+"\r\n")
		if !strings.Contains(line, "403") {
			t.Errorf("CONNECT %s: %q, want 403", target, line)
		}
	}
	if hits.n.Load() != 0 {
		t.Fatalf("internal service was reached %d times", hits.n.Load())
	}
}

func TestConnectToPublicTunnels(t *testing.T) {
	var hits counter
	pub := httptest.NewServer(hits.handler("hello"))
	defer pub.Close()
	p := startProxy(t, Options{Dial: fakePublic(t, map[string]string{"radio.example": pub.Listener.Addr().String()})})

	c, err := net.Dial("tcp", p.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	fmt.Fprintf(c, "CONNECT radio.example:443 HTTP/1.1\r\nHost: radio.example:443\r\n%s\r\n", auth(p))
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, &http.Request{Method: "CONNECT"})
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("CONNECT: %v %v", resp, err)
	}
	fmt.Fprintf(c, "GET / HTTP/1.1\r\nHost: radio.example\r\nConnection: close\r\n\r\n")
	inner, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(inner.Body)
	if string(b) != "hello" {
		t.Fatalf("tunnel body %q", b)
	}
}

func TestCredentialRequired(t *testing.T) {
	var hits counter
	pub := httptest.NewServer(hits.handler("x"))
	defer pub.Close()
	p := startProxy(t, Options{Dial: fakePublic(t, map[string]string{"radio.example": pub.Listener.Addr().String()})})

	bad := []string{
		"",
		"Proxy-Authorization: Basic " + "aGFscHJhZGlvOndyb25n" + "\r\n", // halpradio:wrong
		"Proxy-Authorization: Bearer x\r\n",
		strings.Replace(auth(p), "Basic ", "Basic x", 1),
	}
	for _, h := range bad {
		for _, req := range []string{
			"GET http://radio.example/ HTTP/1.1\r\nHost: radio.example\r\n" + h + "\r\n",
			"CONNECT radio.example:443 HTTP/1.1\r\nHost: radio.example:443\r\n" + h + "\r\n",
		} {
			if line := rawRequest(t, p.Addr(), req); !strings.Contains(line, "407") {
				t.Errorf("header %q: %q, want 407", h, line)
			}
		}
	}
	if hits.n.Load() != 0 {
		t.Fatalf("upstream reached without a valid credential")
	}
	// Two proxies never share a credential.
	q := startProxy(t, Options{})
	if q.URL() == p.URL() || strings.Split(q.URL(), "@")[0] == strings.Split(p.URL(), "@")[0] {
		t.Fatal("credential is not per session")
	}
	// With the right credential the same request passes.
	if line := rawRequest(t, p.Addr(), "GET http://radio.example/ HTTP/1.1\r\nHost: radio.example\r\n"+auth(p)+"\r\n"); !strings.Contains(line, "200") {
		t.Fatalf("authorised GET: %q", line)
	}
}

func TestOnlyGetHeadConnect(t *testing.T) {
	p := startProxy(t, Options{})
	for _, req := range []string{
		"POST http://radio.example/ HTTP/1.1\r\nHost: radio.example\r\nContent-Length: 0\r\n",
		"PUT http://radio.example/ HTTP/1.1\r\nHost: radio.example\r\nContent-Length: 0\r\n",
		"OPTIONS http://radio.example/ HTTP/1.1\r\nHost: radio.example\r\n",
		"DELETE http://radio.example/ HTTP/1.1\r\nHost: radio.example\r\n",
	} {
		if line := rawRequest(t, p.Addr(), req+auth(p)+"\r\n"); !strings.Contains(line, "405") {
			t.Errorf("%q -> %q, want 405", strings.SplitN(req, " ", 2)[0], line)
		}
	}
	// Origin-form and https absolute URLs are not proxy requests.
	for _, req := range []string{
		"GET /local HTTP/1.1\r\nHost: radio.example\r\n",
		"GET https://radio.example/ HTTP/1.1\r\nHost: radio.example\r\n",
	} {
		if line := rawRequest(t, p.Addr(), req+auth(p)+"\r\n"); !strings.Contains(line, "400") {
			t.Errorf("%q -> %q, want 400", req, line)
		}
	}
}

// The proxy can reach neither itself nor anything else on loopback.
func TestProxyCannotReachItselfOrLoopback(t *testing.T) {
	p := startProxy(t, Options{})
	c := clientVia(t, p.URL(), false)
	for _, u := range []string{"http://" + p.Addr() + "/", "http://127.0.0.1:1/", "http://[::1]/", "http://0.0.0.0/"} {
		resp, err := c.Get(u)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("%s: status %d, want 403", u, resp.StatusCode)
		}
	}
	line := rawRequest(t, p.Addr(), "CONNECT "+p.Addr()+" HTTP/1.1\r\nHost: "+p.Addr()+"\r\n"+auth(p)+"\r\n")
	if !strings.Contains(line, "403") {
		t.Errorf("CONNECT to itself: %q", line)
	}
}

func TestHeadersStripped(t *testing.T) {
	got := make(chan http.Header, 1)
	pub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got <- r.Header.Clone()
		w.Header().Set("Proxy-Agent", "evil")
		_, _ = io.WriteString(w, "ok")
	}))
	defer pub.Close()
	p := startProxy(t, Options{Dial: fakePublic(t, map[string]string{"radio.example": pub.Listener.Addr().String()})})
	req, _ := http.NewRequest("GET", "http://radio.example/", nil)
	req.Header.Set("Proxy-Connection", "keep-alive")
	req.Header.Set("Proxy-Foo", "bar")
	req.Header.Set("Icy-MetaData", "1")
	resp, err := clientVia(t, p.URL(), false).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	h := <-got
	for k := range h {
		if strings.HasPrefix(strings.ToLower(k), "proxy-") {
			t.Errorf("upstream saw %s", k)
		}
	}
	if h.Get("Icy-Metadata") != "1" {
		t.Error("ordinary headers must pass")
	}
	if resp.Header.Get("Proxy-Agent") != "" {
		t.Error("Proxy-* response header passed back")
	}
}

// SHOUTcast v1 answers "ICY 200 OK"; the player must still get the stream.
func TestICYStatusLineIsAccepted(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = bufio.NewReader(c).ReadString('\n')
		_, _ = io.WriteString(c, "ICY 200 OK\r\nicy-name: Test\r\ncontent-type: audio/mpeg\r\n\r\nMP3DATA")
	}()
	p := startProxy(t, Options{Dial: fakePublic(t, map[string]string{"shout.example": ln.Addr().String()})})
	resp, err := clientVia(t, p.URL(), false).Get("http://shout.example/;")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Icy-Name") != "Test" || string(b) != "MP3DATA" {
		t.Fatalf("status %d icy-name %q body %q", resp.StatusCode, resp.Header.Get("Icy-Name"), b)
	}
}

func TestConnectionLimit(t *testing.T) {
	p := startProxy(t, Options{MaxConns: 2})
	var held []net.Conn
	for i := 0; i < 2; i++ {
		c, err := net.Dial("tcp", p.Addr())
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
	}
	defer func() {
		for _, c := range held {
			c.Close()
		}
	}()
	time.Sleep(50 * time.Millisecond)
	c, err := net.Dial("tcp", p.Addr())
	if err != nil {
		return
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Read(make([]byte, 1)); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("third connection was served")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("third connection was queued, not refused")
	}
}

func TestHeaderSizeLimit(t *testing.T) {
	p := startProxy(t, Options{MaxHeaderBytes: 1024})
	line := rawRequest(t, p.Addr(), "GET http://radio.example/ HTTP/1.1\r\nHost: radio.example\r\nX-Big: "+strings.Repeat("a", 8192)+"\r\n"+auth(p)+"\r\n")
	if !strings.Contains(line, "431") {
		t.Fatalf("oversized header: %q, want 431", line)
	}
}

// Close ends streaming responses and tunnels and leaves no goroutines.
func TestCloseLeavesNoGoroutines(t *testing.T) {
	stream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f := w.(http.Flusher)
		for {
			if _, err := io.WriteString(w, "frame"); err != nil {
				return
			}
			f.Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
	}))
	defer stream.Close()
	names := map[string]string{"radio.example": stream.Listener.Addr().String()}

	runtime.GC()
	before := runtime.NumGoroutine()
	for i := 0; i < 20; i++ {
		p, err := Start(Options{Dial: fakePublic(t, names)})
		if err != nil {
			t.Fatal(err)
		}
		resp, err := clientVia(t, p.URL(), false).Get("http://radio.example/live")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = resp.Body.Read(make([]byte, 5))
		tc, err := net.Dial("tcp", p.Addr())
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(tc, "CONNECT radio.example:443 HTTP/1.1\r\nHost: radio.example:443\r\n%s\r\n", auth(p))
		_, _ = bufio.NewReader(tc).ReadString('\n')

		_ = p.Close()
		_ = p.Close() // idempotent
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		tc.Close()
		if c, err := net.DialTimeout("tcp", p.Addr(), time.Second); err == nil {
			c.Close()
			t.Fatal("listener still accepting after Close")
		}
	}
	http.DefaultTransport.(*http.Transport).CloseIdleConnections()
	deadline := time.Now().Add(5 * time.Second)
	for {
		runtime.GC()
		n := runtime.NumGoroutine()
		if n <= before+3 {
			return
		}
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("goroutines %d -> %d after 20 start/close cycles\n%s", before, n, buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(20 * time.Millisecond)
	}
}
