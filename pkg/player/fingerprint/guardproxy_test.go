package fingerprint

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/halpworld/halpradio/pkg/player/streamproxy"
)

// FR-C-1: ffmpeg is limited to streaming protocols (plus httpproxy for
// https through the guard proxy) and a strict capture's environment points
// every proxy variable at the guard proxy, with no_proxy removed.
func TestFFmpegCaptureArgsAndEnv(t *testing.T) {
	args := strings.Join(ffmpegCaptureArgs("http://radio.example/", time.Second, "/tmp/x.wav"), "\x00")
	if !strings.Contains(args, "-protocol_whitelist\x00http,https,tcp,tls,crypto,httpproxy\x00-i\x00http://radio.example/") {
		t.Fatalf("argv: %q", args)
	}
	env := captureEnv([]string{"A=1", "no_proxy=*", "NO_PROXY=*", "ALL_PROXY=x", "http_proxy=corp"}, "http://u:p@127.0.0.1:9")
	got := strings.Join(env, "\n")
	for _, bad := range []string{"no_proxy", "NO_PROXY", "ALL_PROXY", "corp"} {
		if strings.Contains(got, bad) {
			t.Errorf("env keeps %s:\n%s", bad, got)
		}
	}
	if !strings.Contains(got, "http_proxy=http://u:p@127.0.0.1:9") || !strings.Contains(got, "https_proxy=http://u:p@127.0.0.1:9") || !strings.Contains(got, "A=1") {
		t.Errorf("env: %s", got)
	}
}

// A strict capture starts the guard proxy and closes it when done; a
// permitted (user-station) capture does not start one.
func TestCaptureProxyLifetime(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // no ffmpeg: nothing is spawned
	var started []*streamproxy.Proxy
	orig := startCaptureProxy
	startCaptureProxy = func() (*streamproxy.Proxy, error) {
		p, err := streamproxy.Start(streamproxy.Options{})
		started = append(started, p)
		return p, err
	}
	defer func() { startCaptureProxy = orig }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // the Go fallback must not dial anything
	_, cleanup, _ := captureAudioSample(ctx, "http://93.184.216.34/live", time.Second, false, nil)
	cleanup()
	if len(started) != 1 {
		t.Fatalf("strict capture started %d proxies, want 1", len(started))
	}
	if c, err := net.DialTimeout("tcp", started[0].Addr(), time.Second); err == nil {
		c.Close()
		t.Fatal("proxy still listening after the capture")
	}
	_, cleanup, _ = captureAudioSample(ctx, "http://192.168.1.20/live", time.Second, true, nil)
	cleanup()
	if len(started) != 1 {
		t.Fatal("user-station capture started a proxy")
	}
}
