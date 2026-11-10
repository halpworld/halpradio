package player

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/halpworld/halpradio/pkg/player/dsp"
	"github.com/halpworld/halpradio/pkg/player/streamproxy"
	"github.com/halpworld/halpradio/pkg/radio"
)

var allBackends = []string{"mpv", "vlc", "cvlc", "ffplay", "mplayer", "mpg123"}

func envValue(env []string, name string) (string, bool) {
	for _, kv := range env {
		if k, v, _ := strings.Cut(kv, "="); k == name {
			return v, true
		}
	}
	return "", false
}

// FR-C-1: for strict stations every backend gets the guard proxy, inherited
// proxy variables (no_proxy above all) are removed, and the credential is
// not on the command line (except VLC, see buildExternalCommand).
func TestExternalCommand_StrictStationGetsProxy(t *testing.T) {
	const proxy = "http://halpradio:secretpass@127.0.0.1:4242"
	base := []string{"PATH=/usr/bin", "HOME=/h", "no_proxy=*", "NO_PROXY=*", "all_proxy=socks5://x", "http_proxy=http://corp:3128", "HTTPS_PROXY=http://corp:3128"}
	for _, b := range allBackends {
		args, env, err := buildExternalCommand(b, "http://radio.example/live", 50, "", dsp.DefaultSettings(), proxy, base)
		if err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"http_proxy", "HTTP_PROXY", "https_proxy", "HTTPS_PROXY"} {
			if v, ok := envValue(env, name); !ok || v != proxy {
				t.Errorf("%s: %s = %q, want the guard proxy", b, name, v)
			}
		}
		for _, name := range []string{"no_proxy", "NO_PROXY", "all_proxy", "ALL_PROXY"} {
			if _, ok := envValue(env, name); ok {
				t.Errorf("%s: inherited %s survived", b, name)
			}
		}
		if v, _ := envValue(env, "PATH"); v != "/usr/bin" {
			t.Errorf("%s: unrelated environment lost", b)
		}
		joined := strings.Join(args, " ")
		if b != "vlc" && b != "cvlc" && strings.Contains(joined, "secretpass") {
			t.Errorf("%s: proxy credential on the command line: %v", b, args)
		}
		if (b == "vlc" || b == "cvlc") && !strings.Contains(joined, "--http-proxy="+proxy) {
			t.Errorf("%s: missing --http-proxy: %v", b, args)
		}
		// mpg123's internal HTTP client never sends the proxy credential;
		// the curl helper does.
		if b == "mpg123" && !strings.Contains(joined, "--network curl --") {
			t.Errorf("mpg123: proxied station must use the curl helper: %v", args)
		}
		if b == "mpg123" && args[len(args)-2] != "--" {
			t.Errorf("mpg123: URL not after --: %v", args)
		}
	}
}

func TestExternalCommand_UserStationHasNoProxy(t *testing.T) {
	for _, b := range allBackends {
		args, env, err := buildExternalCommand(b, "http://192.168.1.20:8000/live", 50, "", dsp.DefaultSettings(), "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if env != nil {
			t.Errorf("%s: user station must inherit the environment, got %v", b, env)
		}
		if j := strings.Join(args, " "); strings.Contains(j, "-proxy") || strings.Contains(j, "127.0.0.1") || strings.Contains(j, "--network") {
			t.Errorf("%s: user station argv mentions a proxy: %v", b, args)
		}
	}
}

// The protocol whitelist applies to every station on the libavformat
// backends.
func TestExternalCommand_ProtocolWhitelist(t *testing.T) {
	for _, proxy := range []string{"", "http://u:p@127.0.0.1:1"} {
		args, _, _ := buildExternalCommand("mpv", "http://radio.example/", 50, "", dsp.DefaultSettings(), proxy, nil)
		for _, want := range []string{
			"--stream-lavf-o=protocol_whitelist=[" + lavfProtocols + "]",
			"--demuxer-lavf-o=protocol_whitelist=[" + lavfProtocols + "]",
			"--load-unsafe-playlists=no",
		} {
			if !containsArg(args, want) {
				t.Errorf("mpv (proxy %q) lacks %s: %v", proxy, want, args)
			}
		}
		args, _, _ = buildExternalCommand("ffplay", "http://radio.example/", 50, "", dsp.DefaultSettings(), proxy, nil)
		if !strings.Contains(strings.Join(args, "\x00"), "-protocol_whitelist\x00"+lavfProtocols+"\x00") {
			t.Errorf("ffplay (proxy %q) lacks the whitelist: %v", proxy, args)
		}
	}
	for _, bad := range []string{"file", "gopher", "pipe", "data", "rtmp", "udp", "unix", "subfile", "concat"} {
		for _, p := range strings.Split(lavfProtocols, ",") {
			if p == bad {
				t.Errorf("whitelist contains %s", bad)
			}
		}
	}
}

func publicLookup(context.Context, string) ([]netip.Addr, error) {
	return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
}

// The proxy lives exactly as long as the external player: it is closed when
// playExternal returns (here the binary cannot be found, so nothing forks).
func TestPlayExternal_StrictStationStartsAndStopsProxy(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for i := 0; i < 10; i++ {
		var mu sync.Mutex
		var started []*streamproxy.Proxy
		m := &Manager{status: StatusConnecting, dspSettings: dsp.DefaultSettings(), lookup: publicLookup}
		m.startProxy = func() (*streamproxy.Proxy, error) {
			p, err := streamproxy.Start(streamproxy.Options{})
			mu.Lock()
			started = append(started, p)
			mu.Unlock()
			return p, err
		}
		m.playExternal(context.Background(), "mpg123", radio.Station{URL: "http://radio.example/live", Source: "radiobrowser"}, 50, false)
		if len(started) != 1 {
			t.Fatalf("strict station started %d proxies, want 1", len(started))
		}
		if c, err := net.DialTimeout("tcp", started[0].Addr(), time.Second); err == nil {
			c.Close()
			t.Fatal("proxy still listening after the player exited")
		}
	}
}

func TestPlayExternal_UserStationGetsNoProxy(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	m := &Manager{status: StatusConnecting, dspSettings: dsp.DefaultSettings()}
	m.startProxy = func() (*streamproxy.Proxy, error) {
		t.Error("proxy started for a user station")
		return nil, errors.New("unexpected")
	}
	m.playExternal(context.Background(), "mpg123", radio.Station{URL: "http://192.168.1.20:8000/live", Source: "local"}, 50, true)
	if !strings.Contains(m.Error(), "start failed") {
		t.Fatalf("expected the (missing) player to be tried directly, got %q", m.Error())
	}
}

// If the proxy cannot start, a strict station does not play on an external
// backend at all.
func TestPlayExternal_ProxyFailureFailsClosed(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	m := &Manager{status: StatusConnecting, dspSettings: dsp.DefaultSettings(), lookup: publicLookup}
	m.startProxy = func() (*streamproxy.Proxy, error) { return nil, errors.New("no listener") }
	m.playExternal(context.Background(), "mpv", radio.Station{URL: "http://radio.example/live", Source: "bundled"}, 50, false)
	if m.Status() != StatusError || !strings.Contains(m.Error(), "guard proxy") {
		t.Fatalf("status %v err %q, want a guard-proxy refusal", m.Status(), m.Error())
	}
	if m.cmd != nil {
		t.Fatal("player started without the proxy")
	}
}
