package player

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/debuglog"
	"github.com/halpworld/halpradio/pkg/player/dsp"
	"github.com/halpworld/halpradio/pkg/radio"
)

// PLY-3: a station URL must never make mpv run yt-dlp or fetch side files.
func TestMPVArgsDisableHelperPrograms(t *testing.T) {
	args, err := buildExternalArgs("mpv", "https://www.youtube.com/watch?v=x", 50, "", dsp.DefaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	sep := -1
	for i, a := range args {
		if a == "--" {
			sep = i
		}
	}
	if sep < 0 {
		t.Fatalf("no -- separator in %v", args)
	}
	opts := strings.Join(args[:sep], " ")
	for _, want := range []string{"--ytdl=no", "--load-unsafe-playlists=no", "--sub-auto=no", "--audio-file-auto=no", "--cover-art-auto=no"} {
		if !strings.Contains(opts, want) {
			t.Errorf("mpv argv lacks %s: %v", want, args)
		}
	}
}

// LOC-4: URLs with credentials or tokens must not reach the debug log.
func TestSetErrorAndExecLogsAreRedacted(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "debug.log")
	if _, err := debuglog.Init(logPath); err != nil {
		t.Fatal(err)
	}
	m := &Manager{}
	m.setError("HTTP connection failed: Get \"http://alice:hunter2@radio.example/live?token=s3cret\": EOF")
	debuglog.Close()

	b, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"hunter2", "s3cret", "alice"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("debug log leaked %q:\n%s", secret, b)
		}
	}
	if !strings.Contains(string(b), "radio.example") {
		t.Fatalf("expected the host to remain for debugging:\n%s", b)
	}
}

// PLY-2: a hostile stream with an oversized ID3v2 header must end in
// StatusError, without allocating the advertised size or crashing.
func TestPlayNativeHostileID3BecomesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// "ID3", v2.4, no flags, syncsafe size 0x0FFFFFFF (~256 MiB).
		_, _ = w.Write([]byte{'I', 'D', '3', 4, 0, 0, 0x7f, 0x7f, 0x7f, 0x7f})
	}))
	defer srv.Close()

	m := &Manager{status: StatusConnecting, dspSettings: dsp.DefaultSettings()}
	m.playNative(context.Background(), radio.Station{URL: srv.URL}, 50, true)

	if m.Status() != StatusError {
		t.Fatalf("status = %v, want StatusError (err %q)", m.Status(), m.Error())
	}
	if !strings.Contains(m.Error(), "decode") {
		t.Errorf("unexpected error text %q", m.Error())
	}
}
