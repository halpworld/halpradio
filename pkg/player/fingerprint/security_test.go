package fingerprint

import (
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Minimised go-mp3 crash reproducer (same bytes as pkg/player/mp3safe tests).
const crashMP3Hex = "" +
	"49443304000000000023545353450000000f0000034c61766635372e37312e3130300000000000000000000000e874bc" +
	"8890ba27e1fd3a17ee08513f15b78a574ae65d55680477f572573ccc11550e3305acb0881e4c9fcbfff36288ff2b1c16" +
	"1800d24cdc02f9c160e619421d364e16d043198ced4197ea226ab0e832c8a27ea5a530b04ba5c1d6baed849bf3e98126" +
	"4a6b08807306c5d3249093746430200c420718da1399fc6319b2329b6eaca98c3a63b0200d034d2c4c4c9b14cb04f9a0" +
	"f21f391a0634b819b481d390d2f9b8e4062814a0019706f701e04f209a8b8665b19800e0e16602cf8120f6d03cb2fff3" +
	"62c4eb22b3"

// PLY-2: capture-fallback decoding of hostile bytes must not crash.
func TestDecodeMP3ToMono_HostileInputIsAnError(t *testing.T) {
	crash, err := hex.DecodeString(crashMP3Hex)
	if err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{
		"go-mp3 panic reproducer": crash,
		"huge ID3v2 size":         {'I', 'D', '3', 4, 0, 0, 0x7f, 0x7f, 0x7f, 0x7f, 0, 0},
	}
	for name, in := range inputs {
		t.Run(name, func(t *testing.T) {
			// A panic here fails the test run; an error or short output is fine.
			pcm, _, _ := decodeMP3ToMono(in)
			if len(pcm) > 128*1024 {
				t.Fatalf("decoded more than the buffer: %d", len(pcm))
			}
		})
	}
}

// Capture must refuse anything but http/https before ffmpeg or the HTTP client
// sees it (file:, other ffmpeg protocols, option-looking strings).
func TestCaptureAudioSample_RejectsNonHTTP(t *testing.T) {
	for _, u := range []string{
		"file:///etc/passwd",
		"-i/etc/passwd",
		"concat:/etc/passwd",
		"pipe:0",
		"ftp://example.invalid/x.mp3",
		"http://",
	} {
		_, cleanup, err := CaptureAudioSample(context.Background(), u, time.Second)
		cleanup()
		if err == nil {
			t.Errorf("CaptureAudioSample(%q) succeeded, want refusal", u)
		}
	}
}

// remote-data: the AcoustID body is read with a cap.
func TestQueryAcoustID_OversizedBodyRefused(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","pad":"`))
		chunk := []byte(strings.Repeat("A", 64*1024))
		for i := 0; i < (maxAcoustIDBody/len(chunk))+4; i++ {
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
		_, _ = w.Write([]byte(`","results":[]}`))
	}))
	defer ts.Close()

	c := NewClient("test-key")
	c.AcoustIDURL = ts.URL
	_, err := c.queryAcoustID(context.Background(), "fp", 10, "s", "S")
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("err = %v, want size refusal", err)
	}
}

// TTY-4: identification strings from the network are display-sanitized.
func TestQueryAcoustID_ResultIsSanitized(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","results":[{"id":"r","score":7.5,"recordings":[{"id":"x",
			"title":"Song\u001b]8;;https://evil.example\u0007click\u001b]8;;\u0007‮",
			"artists":[{"name":"A\u009b2J​B"}],
			"releasegroups":[{"id":"g","title":"Al\nbum\u0000"}]}]}]}`))
	}))
	defer ts.Close()

	c := NewClient("test-key")
	c.AcoustIDURL = ts.URL
	res, err := c.queryAcoustID(context.Background(), "fp", 10, "s", "S")
	if err != nil {
		t.Fatal(err)
	}
	for field, v := range map[string]string{"artist": res.Artist, "title": res.Title, "album": res.Album} {
		for _, r := range v {
			if r < 0x20 || (r >= 0x7f && r <= 0x9f) || r == 0x202e || r == 0x200b {
				t.Fatalf("%s keeps unsafe rune %U: %q", field, r, v)
			}
		}
	}
	if res.Confidence != 1 {
		t.Errorf("confidence = %v, want clamp to 1", res.Confidence)
	}
}

// RR-E-1: sampling a stream never reaches loopback unless the caller
// explicitly allows private hosts (ffmpeg and the Go fallback alike).
func TestCaptureRefusesPrivateHostByDefault(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
	}))
	defer srv.Close()

	_, cleanup, err := CaptureAudioSample(context.Background(), srv.URL, time.Second)
	cleanup()
	if err == nil {
		t.Fatal("capture from loopback succeeded")
	}
	c := NewClient("k")
	if _, err := c.Identify(context.Background(), srv.URL, "id", "name", ""); err == nil {
		t.Fatal("Identify with nil AllowPrivate reached loopback")
	}
	if hits.Load() != 0 {
		t.Fatalf("loopback server was contacted %d times", hits.Load())
	}
}

func TestCaptureAllowsPrivateWhenPermitted(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = w.Write(make([]byte, 4096))
	}))
	defer srv.Close()

	_, cleanup, _ := captureAudioSample(context.Background(), srv.URL, time.Second, true, nil)
	cleanup()
	if hits.Load() == 0 {
		t.Fatal("allowed capture never contacted the LAN server")
	}
}
