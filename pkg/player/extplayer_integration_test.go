package player

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/halpworld/halpradio/pkg/player/dsp"
	"github.com/halpworld/halpradio/pkg/player/streamnet"
	"github.com/halpworld/halpradio/pkg/player/streamproxy"
)

// TestExternalPlayersThroughGuardProxy runs the real external players
// against loopback servers. It is opt-in because it spawns processes (and
// the macOS -race runtime can hang in fork):
//
//	HALPRADIO_EXT_PLAYER_TEST=1 go test -run TestExternalPlayersThroughGuardProxy -v ./pkg/player/
//
// "radio.example" stands for a public host: only the proxy's injected
// dialer can reach it, so any request that reaches it went through the
// proxy. The internal HTTP service and the raw TCP port stand for LAN or
// metadata services and must never be reached on a strict station.
func TestExternalPlayersThroughGuardProxy(t *testing.T) {
	if os.Getenv("HALPRADIO_EXT_PLAYER_TEST") == "" {
		t.Skip("set HALPRADIO_EXT_PLAYER_TEST=1 to run the real external players")
	}

	var internalHits, rawHits atomic.Int32
	internal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		internalHits.Add(1)
		_, _ = w.Write([]byte("secret"))
	}))
	defer internal.Close()
	raw, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	go func() {
		for {
			c, err := raw.Accept()
			if err != nil {
				return
			}
			rawHits.Add(1)
			_ = c.Close()
		}
	}()
	in, rawAddr := internal.Listener.Addr().String(), raw.Addr().String()

	frame := make([]byte, 417) // MPEG-1 Layer III, 128 kbit/s, 44.1 kHz, silent-ish
	copy(frame, []byte{0xff, 0xfb, 0x90, 0x00})
	var mu sync.Mutex
	pubHits := map[string]int{}
	pub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		pubHits[r.URL.Path]++
		mu.Unlock()
		switch r.URL.Path {
		case "/stream.mp3", "/seg0.mp3":
			w.Header().Set("Content-Type", "audio/mpeg")
			for i := 0; i < 120; i++ {
				if _, err := w.Write(frame); err != nil {
					return
				}
				if i%10 == 9 {
					w.(http.Flusher).Flush()
					time.Sleep(20 * time.Millisecond)
				}
			}
		case "/redir":
			http.Redirect(w, r, "http://"+in+"/from-redirect", http.StatusFound)
		case "/redir-meta":
			http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
		case "/pl.m3u":
			w.Header().Set("Content-Type", "audio/x-mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXTINF:-1,x\nhttp://%s/from-playlist\n", in)
		case "/pl-tcp.m3u":
			w.Header().Set("Content-Type", "audio/x-mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXTINF:-1,x\ntcp://%s\n", rawAddr)
		case "/pl-gopher.m3u":
			w.Header().Set("Content-Type", "audio/x-mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXTINF:-1,x\ngopher://%s/_PING\n", rawAddr)
		case "/hls-private.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n#EXTINF:10,\nhttp://%s/seg.ts\n#EXT-X-ENDLIST\n", in)
		case "/hls-tcp.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprintf(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:10\n#EXTINF:10,\ntcp://%s\n#EXT-X-ENDLIST\n", rawAddr)
		case "/hls-ok.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-TARGETDURATION:3\n#EXTINF:3,\nhttp://radio.example/seg0.mp3\n#EXT-X-ENDLIST\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer pub.Close()

	var tlsDials atomic.Int32
	strict := streamnet.NewDialer(streamnet.Options{})
	dial := func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, _ := net.SplitHostPort(addr)
		if host == "radio.example" {
			if port == "443" {
				tlsDials.Add(1)
			}
			var d net.Dialer
			return d.DialContext(ctx, network, pub.Listener.Addr().String())
		}
		return strict(ctx, network, addr)
	}

	run := func(t *testing.T, backend, url, proxyURL string) {
		t.Helper()
		var env []string
		if proxyURL != "" {
			env = os.Environ()
		}
		argv, env, err := buildExternalCommand(backend, url, 0, "", dsp.DefaultSettings(), proxyURL, env)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Env = env
		cmd.WaitDelay = 2 * time.Second
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		_ = cmd.Wait() // always reaped; CommandContext kills on timeout
	}

	for _, backend := range []string{"mpv", "ffplay", "mpg123"} {
		t.Run(backend, func(t *testing.T) {
			if _, err := exec.LookPath(backend); err != nil {
				t.Skipf("%s not installed", backend)
			}
			// Control: without the proxy the redirect reaches the internal
			// service, so the checks below are meaningful.
			internalHits.Store(0)
			run(t, backend, pub.URL+"/redir", "")
			if internalHits.Load() == 0 {
				t.Logf("%s: control redirect did not reach the internal service directly", backend)
			}

			px, err := streamproxy.Start(streamproxy.Options{Dial: dial})
			if err != nil {
				t.Fatal(err)
			}
			defer px.Close()
			internalHits.Store(0)
			rawHits.Store(0)
			for _, p := range []string{"/redir", "/redir-meta", "/pl.m3u", "/pl-tcp.m3u", "/pl-gopher.m3u", "/hls-private.m3u8", "/hls-tcp.m3u8"} {
				run(t, backend, "http://radio.example"+p, px.URL())
			}
			if n := internalHits.Load(); n != 0 {
				t.Errorf("%s reached the internal service %d times through the proxy", backend, n)
			}
			if n := rawHits.Load(); n != 0 {
				t.Errorf("%s opened %d raw TCP connections to the internal port", backend, n)
			}

			mu.Lock()
			pubHits = map[string]int{}
			mu.Unlock()
			run(t, backend, "http://radio.example/stream.mp3", px.URL())
			if backend != "mpg123" {
				run(t, backend, "http://radio.example/hls-ok.m3u8", px.URL())
			}
			mu.Lock()
			got := fmt.Sprint(pubHits)
			streamed, seg := pubHits["/stream.mp3"], pubHits["/seg0.mp3"]
			mu.Unlock()
			if streamed == 0 {
				t.Errorf("%s did not fetch a public stream through the proxy (%s)", backend, got)
			}
			if backend != "mpg123" && seg == 0 {
				t.Errorf("%s did not fetch the public HLS segment through the proxy (%s)", backend, got)
			}

			{ // https goes through CONNECT on every backend (mpg123 via curl)
				before := tlsDials.Load()
				run(t, backend, "https://radio.example/stream.mp3", px.URL())
				if tlsDials.Load() == before {
					t.Errorf("%s did not send https through the proxy (CONNECT)", backend)
				}
			}

			// A user station gets no proxy and still reaches the LAN.
			mu.Lock()
			pubHits = map[string]int{}
			mu.Unlock()
			run(t, backend, pub.URL+"/stream.mp3", "")
			mu.Lock()
			direct := pubHits["/stream.mp3"]
			mu.Unlock()
			if direct == 0 {
				t.Errorf("%s: user station on a LAN address did not play", backend)
			}
			t.Logf("%s: public hits %s", backend, strings.TrimSpace(got))
		})
	}
}
