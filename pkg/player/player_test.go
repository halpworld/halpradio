package player

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/halpworld/halpradio/pkg/radio"
)

func TestPlayerVolumeClamping(t *testing.T) {
	pm := NewManager("auto", 80, nil)

	if pm.Volume() != 80 {
		t.Errorf("Expected initial volume 80, got %d", pm.Volume())
	}

	vol := pm.SetVolume(120)
	if vol != 100 || pm.Volume() != 100 {
		t.Errorf("Expected volume clamped to 100, got %d", vol)
	}

	volMin := pm.SetVolume(-10)
	if volMin != 0 || pm.Volume() != 0 {
		t.Errorf("Expected volume clamped to 0, got %d", volMin)
	}
}

func TestPlayerToggleMute(t *testing.T) {
	pm := NewManager("auto", 75, nil)

	muted := pm.ToggleMute()
	if !muted || pm.Volume() != 0 {
		t.Errorf("Expected muted state with 0 effective volume")
	}

	unmuted := pm.ToggleMute()
	if unmuted || pm.Volume() != 75 {
		t.Errorf("Expected unmuted state with volume restored to 75")
	}
}

func TestAutoPauseLifecycle(t *testing.T) {
	pm := NewManager("auto", 80, nil)
	pm.SetOnAutoPause(func() {})
	pm.SetAutoPause(true)
	pm.SetAutoPause(false)
	pm.SetAutoPause(true)
	if err := pm.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if pm.Status() != StatusStopped {
		t.Errorf("Expected stopped status after Close, got %s", pm.Status())
	}
}

func TestAutoPauseOnDeviceLoss(t *testing.T) {
	pm := NewManager("auto", 80, nil)
	pm.mu.Lock()
	pm.status = StatusPlaying
	pm.mu.Unlock()

	called := false
	pm.SetOnAutoPause(func() { called = true })
	pm.autoPauseOnDeviceLoss()

	if pm.Status() != StatusPaused {
		t.Errorf("Expected paused after device loss, got %s", pm.Status())
	}
	if !called {
		t.Errorf("Expected onAutoPause callback to fire")
	}

	called = false
	pm.autoPauseOnDeviceLoss()
	if called {
		t.Errorf("Expected no callback when not playing")
	}
}

func TestBackendDetection(t *testing.T) {
	backend := detectBackend("native")
	if backend != "native" {
		t.Errorf("Expected native backend when preferred is 'native', got '%s'", backend)
	}

	goBackend := detectBackend("go")
	if goBackend != "native" {
		t.Errorf("Expected native backend when preferred is 'go', got '%s'", goBackend)
	}

	autoBackend := detectBackend("auto")
	if autoBackend == "" {
		t.Errorf("Expected auto backend detection to yield a valid backend name")
	}
}

func TestPlayerDefaultsAndState(t *testing.T) {
	pm := NewManager("", -5, nil)
	if pm.Volume() != 80 {
		t.Errorf("Expected default volume fallback 80, got %d", pm.Volume())
	}
	if pm.Status() != StatusStopped {
		t.Errorf("Expected initial status StatusStopped, got %s", pm.Status())
	}
	if pm.CurrentStation() != nil {
		t.Errorf("Expected nil station initially")
	}
	if pm.CurrentTrack() != "" {
		t.Errorf("Expected empty track initially")
	}
	if pm.Error() != "" {
		t.Errorf("Expected empty error initially")
	}
	if pm.IsMuted() {
		t.Errorf("Expected not muted initially")
	}
	if pm.ActiveBackend() == "" {
		t.Errorf("Expected non-empty backend")
	}

	// Test Stop on stopped manager
	if err := pm.Stop(); err != nil {
		t.Errorf("Stop failed: %v", err)
	}

	// Test Pause on stopped manager
	if err := pm.Pause(); err != nil {
		t.Errorf("Pause failed: %v", err)
	}

	// Test Resume when no current station
	if err := pm.Resume(); err != nil {
		t.Errorf("Resume failed: %v", err)
	}
}

func TestPlayerErrorHandling(t *testing.T) {
	pm := NewManager("native", 80, nil)

	// Attempt playing invalid stream URL
	invalidStation := radio.Station{
		ID:   "test-invalid",
		Name: "Invalid Station",
		URL:  "http://127.0.0.1:59999/nonexistent",
	}

	_ = pm.Play(invalidStation)
	time.Sleep(500 * time.Millisecond)

	if pm.Status() != StatusError {
		t.Errorf("Expected status ERROR for unreachable stream, got %s", pm.Status())
	}
	if pm.Error() == "" {
		t.Errorf("Expected non-empty lastError for unreachable stream")
	}
}

func TestIsValidStreamURL(t *testing.T) {
	validURLs := []string{
		"http://stream.example.com/live.mp3",
		"https://ice1.somafm.com/groovesalad-128-mp3",
		"http://192.168.1.100:8000/stream",
	}
	for _, u := range validURLs {
		if !IsValidStreamURL(u) {
			t.Errorf("Expected %q to be valid stream URL", u)
		}
	}

	invalidURLs := []string{
		"",
		"   ",
		"--script=/tmp/evil.lua",
		"-I dummy",
		"file:///etc/passwd",
		"ftp://evil.com/stream",
		"gopher://evil.com",
		"http://",
		"https://",
	}
	for _, u := range invalidURLs {
		if IsValidStreamURL(u) {
			t.Errorf("Expected %q to be invalid stream URL", u)
		}
	}
}

func TestSanitizeTrackTitle(t *testing.T) {
	input := "\x1b[31mRed Title\x1b[0m\x00\r\n - Artist"
	clean := sanitizeTrackTitle(input)
	if clean != "[31mRed Title[0m - Artist" {
		t.Errorf("Expected sanitized track title, got %q", clean)
	}
}

func TestPlayRejectsMaliciousURL(t *testing.T) {
	pm := NewManager("mpv", 80, nil)
	malicious := radio.Station{
		ID:   "exploit",
		Name: "Exploit Station",
		URL:  "--script=/tmp/pwn.lua",
	}
	_ = pm.Play(malicious)
	if pm.Status() != StatusError {
		t.Errorf("Expected StatusError when attempting to play malicious argument URL, got %s", pm.Status())
	}
}

func TestStartICYListener(t *testing.T) {
	metaInt := 16
	metaBlock := make([]byte, 48)
	copy(metaBlock, []byte("StreamTitle='Synth Artist - Neon Dreams';"))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Icy-Metaint", fmt.Sprintf("%d", metaInt))
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		audioChunk := make([]byte, metaInt)
		for i := 0; i < metaInt; i++ {
			audioChunk[i] = 0xAA
		}

		// Write 1 chunk of audio
		_, _ = w.Write(audioChunk)
		// Write metadata length (3 blocks of 16 bytes = 48 bytes)
		_, _ = w.Write([]byte{3})
		// Write metadata block
		_, _ = w.Write(metaBlock)
		if flusher != nil {
			flusher.Flush()
		}
		// Write another audio chunk
		_, _ = w.Write(audioChunk)
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer ts.Close()

	var receivedTrack TrackInfo
	trackCh := make(chan TrackInfo, 1)

	pm := NewManager("native", 80, func(info TrackInfo) {
		trackCh <- info
	})

	st := radio.Station{
		ID:   "test-icy",
		Name: "Test ICY Station",
		URL:  ts.URL,
	}

	pm.mu.Lock()
	pm.status = StatusPlaying
	pm.currentStation = &st
	pm.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go pm.startICYListener(ctx, st)

	select {
	case receivedTrack = <-trackCh:
		if receivedTrack.TrackTitle != "Synth Artist - Neon Dreams" {
			t.Errorf("Track title mismatch: got %q, want %q", receivedTrack.TrackTitle, "Synth Artist - Neon Dreams")
		}
		if pm.CurrentTrack() != "Synth Artist - Neon Dreams" {
			t.Errorf("CurrentTrack mismatch: got %q", pm.CurrentTrack())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timed out waiting for ICY metadata in test environment")
	}
}

func TestStopCancelsICYListenerAndClearsTrack(t *testing.T) {
	metaInt := 16
	metaBlock1 := make([]byte, 48)
	copy(metaBlock1, []byte("StreamTitle='Artist 1 - Track 1';"))
	metaBlock2 := make([]byte, 48)
	copy(metaBlock2, []byte("StreamTitle='Artist 2 - Track 2';"))

	serverProceed := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Icy-Metaint", fmt.Sprintf("%d", metaInt))
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		audioChunk := make([]byte, metaInt)
		// Write chunk 1 + metadata 1
		_, _ = w.Write(audioChunk)
		_, _ = w.Write([]byte{3})
		_, _ = w.Write(metaBlock1)
		if flusher != nil {
			flusher.Flush()
		}

		// Wait for signal from test to write second track
		<-serverProceed

		_, _ = w.Write(audioChunk)
		_, _ = w.Write([]byte{3})
		_, _ = w.Write(metaBlock2)
		_, _ = w.Write(audioChunk)
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer ts.Close()
	defer close(serverProceed)

	trackCh := make(chan TrackInfo, 10)
	pm := NewManager("native", 80, func(info TrackInfo) {
		trackCh <- info
	})

	st := radio.Station{
		ID:   "test-stop-station",
		Name: "Test Stop Station",
		URL:  ts.URL,
	}

	pm.mu.Lock()
	pm.status = StatusPlaying
	pm.currentStation = &st
	ctx, cancel := context.WithCancel(context.Background())
	pm.icyCancel = cancel
	pm.mu.Unlock()

	go pm.startICYListener(ctx, st)

	// Wait for first track to arrive
	select {
	case track := <-trackCh:
		if track.TrackTitle != "Artist 1 - Track 1" {
			t.Fatalf("Expected 'Artist 1 - Track 1', got %q", track.TrackTitle)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for first track")
	}

	// Stop player
	if err := pm.Stop(); err != nil {
		t.Fatalf("Stop error: %v", err)
	}

	if pm.Status() != StatusStopped {
		t.Errorf("Expected StatusStopped, got %s", pm.Status())
	}
	if pm.CurrentStation() != nil {
		t.Errorf("Expected CurrentStation to be nil after Stop(), got %+v", pm.CurrentStation())
	}
	if pm.CurrentTrack() != "" {
		t.Errorf("Expected CurrentTrack to be empty after Stop(), got %q", pm.CurrentTrack())
	}

	// Signal server to send track 2
	serverProceed <- struct{}{}

	// Ensure no second track is received
	select {
	case track := <-trackCh:
		t.Fatalf("Received unexpected track notification after Stop(): %+v", track)
	case <-time.After(300 * time.Millisecond):
		// Expected: no track received
	}
}

func TestPauseCancelsICYListener(t *testing.T) {
	metaInt := 16
	metaBlock1 := make([]byte, 48)
	copy(metaBlock1, []byte("StreamTitle='Artist 1 - Track 1';"))
	metaBlock2 := make([]byte, 48)
	copy(metaBlock2, []byte("StreamTitle='Artist 2 - Track 2';"))

	serverProceed := make(chan struct{})

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Icy-Metaint", fmt.Sprintf("%d", metaInt))
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)

		audioChunk := make([]byte, metaInt)
		// Write chunk 1 + metadata 1
		_, _ = w.Write(audioChunk)
		_, _ = w.Write([]byte{3})
		_, _ = w.Write(metaBlock1)
		if flusher != nil {
			flusher.Flush()
		}

		// Wait for signal from test to write second track
		<-serverProceed

		_, _ = w.Write(audioChunk)
		_, _ = w.Write([]byte{3})
		_, _ = w.Write(metaBlock2)
		_, _ = w.Write(audioChunk)
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer ts.Close()
	defer close(serverProceed)

	trackCh := make(chan TrackInfo, 10)
	pm := NewManager("native", 80, func(info TrackInfo) {
		trackCh <- info
	})

	st := radio.Station{
		ID:   "test-pause-station",
		Name: "Test Pause Station",
		URL:  ts.URL,
	}

	pm.mu.Lock()
	pm.status = StatusPlaying
	pm.currentStation = &st
	ctx, cancel := context.WithCancel(context.Background())
	pm.icyCancel = cancel
	pm.mu.Unlock()

	go pm.startICYListener(ctx, st)

	// Wait for first track to arrive
	select {
	case track := <-trackCh:
		if track.TrackTitle != "Artist 1 - Track 1" {
			t.Fatalf("Expected 'Artist 1 - Track 1', got %q", track.TrackTitle)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Timeout waiting for first track")
	}

	// Pause player
	if err := pm.Pause(); err != nil {
		t.Fatalf("Pause error: %v", err)
	}

	if pm.Status() != StatusPaused {
		t.Errorf("Expected StatusPaused, got %s", pm.Status())
	}
	// Station should still be remembered for Resume()
	if pm.CurrentStation() == nil || pm.CurrentStation().ID != "test-pause-station" {
		t.Errorf("Expected CurrentStation to be preserved on Pause()")
	}

	// Signal server to send track 2
	serverProceed <- struct{}{}

	// Ensure no second track is received
	select {
	case track := <-trackCh:
		t.Fatalf("Received unexpected track notification after Pause(): %+v", track)
	case <-time.After(300 * time.Millisecond):
		// Expected: no track received
	}
}

func TestICYListenerStationMismatchIgnored(t *testing.T) {
	metaInt := 16
	metaBlock := make([]byte, 48)
	copy(metaBlock, []byte("StreamTitle='Artist X - Song Y';"))

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Icy-Metaint", fmt.Sprintf("%d", metaInt))
		w.WriteHeader(http.StatusOK)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}

		audioChunk := make([]byte, metaInt)
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write(audioChunk)
		_, _ = w.Write([]byte{3})
		_, _ = w.Write(metaBlock)
		_, _ = w.Write(audioChunk)
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
	}))
	defer ts.Close()

	trackCh := make(chan TrackInfo, 10)
	pm := NewManager("native", 80, func(info TrackInfo) {
		trackCh <- info
	})

	stA := radio.Station{ID: "station-a", Name: "Station A", URL: ts.URL}
	stB := radio.Station{ID: "station-b", Name: "Station B", URL: "http://example.com/b"}

	pm.mu.Lock()
	pm.status = StatusPlaying
	pm.currentStation = &stB // Station changed to B
	pm.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	go pm.startICYListener(ctx, stA)

	select {
	case track := <-trackCh:
		t.Fatalf("Received unexpected track for mismatched station: %+v", track)
	case <-time.After(300 * time.Millisecond):
		// Expected: no track dispatched
	}
}

func TestPlayerManagerVolumeAndResumeEdgeCases(t *testing.T) {
	pm := NewManager("auto", 80, nil)

	// Test SetVolume clamping
	vHigh := pm.SetVolume(150)
	if vHigh != 100 || pm.Volume() != 100 {
		t.Errorf("expected volume clamped to 100, got %d", vHigh)
	}

	vLow := pm.SetVolume(-20)
	if vLow != 0 || pm.Volume() != 0 {
		t.Errorf("expected volume clamped to 0, got %d", vLow)
	}

	// Test ToggleMute while 0 volume
	pm.SetVolume(75)
	if !pm.ToggleMute() || !pm.IsMuted() {
		t.Errorf("expected isMuted true after ToggleMute")
	}
	if pm.ToggleMute() || pm.IsMuted() {
		t.Errorf("expected isMuted false after second ToggleMute")
	}

	// Test Resume when no station has been played (safe no-op)
	if err := pm.Resume(); err != nil {
		t.Errorf("expected no error resuming with no current station, got %v", err)
	}

	// Test Resume when status is StatusStopped
	st := radio.Station{ID: "test-st", Name: "Test Station", URL: "http://example.com/stream"}
	pm.mu.Lock()
	pm.currentStation = &st
	pm.status = StatusStopped
	pm.mu.Unlock()

	// Should not error
	_ = pm.Resume()

	// Test Pause and Resume when status is StatusPaused
	pm.mu.Lock()
	pm.status = StatusPaused
	pm.mu.Unlock()
	_ = pm.Resume()

	// Test Stop when playing vs stopped
	_ = pm.Stop()
	_ = pm.Stop() // repeat stop when already stopped
}

// recordingControl is a stand-in external backend command channel that records
// what the Manager sends to it.
type recordingControl struct {
	mu      sync.Mutex
	volumes []int
	mutes   []bool
	closed  bool
}

func (c *recordingControl) SetVolume(vol int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.volumes = append(c.volumes, vol)
}

func (c *recordingControl) SetMute(muted bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.mutes = append(c.mutes, muted)
}

func (c *recordingControl) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *recordingControl) snapshot() ([]int, []bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]int(nil), c.volumes...), append([]bool(nil), c.mutes...), c.closed
}

// TestMPVArgsUseIPCNotStdin guards the regression where mpv was launched with
// --input-terminal=no while volume/mute commands were written to its stdin.
// mpv discards stdin commands, so those changes silently did nothing.
func TestMPVArgsUseIPCNotStdin(t *testing.T) {
	args, err := buildExternalArgs("mpv", "http://stream.example.com/live.mp3", 55, "/tmp/halp/s")
	if err != nil {
		t.Fatalf("buildExternalArgs error: %v", err)
	}

	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--input-ipc-server=/tmp/halp/s") {
		t.Errorf("expected mpv to expose a JSON IPC channel, got %v", args)
	}
	// mpv must stay off the user's terminal so it cannot steal TUI keystrokes.
	if !strings.Contains(joined, "--no-terminal") {
		t.Errorf("expected mpv to be launched with --no-terminal, got %v", args)
	}
	if !strings.Contains(joined, "--volume=55") {
		t.Errorf("expected initial volume to be applied at launch, got %v", args)
	}
	if args[len(args)-2] != "--" || args[len(args)-1] != "http://stream.example.com/live.mp3" {
		t.Errorf("expected stream URL to be passed after the -- separator, got %v", args)
	}
}

// TestMPVArgsWithoutIPCEndpoint covers the degraded path where an IPC endpoint
// could not be created: playback must still start, just without live control.
func TestMPVArgsWithoutIPCEndpoint(t *testing.T) {
	args, err := buildExternalArgs("mpv", "http://stream.example.com/live.mp3", 40, "")
	if err != nil {
		t.Fatalf("buildExternalArgs error: %v", err)
	}
	for _, a := range args {
		if strings.HasPrefix(a, "--input-ipc-server") {
			t.Errorf("expected no IPC flag when endpoint is unavailable, got %v", args)
		}
	}
}

// TestMPlayerArgsUseSlaveMode guards the sibling regression: mplayer only reads
// commands from stdin when it is started in slave mode.
func TestMPlayerArgsUseSlaveMode(t *testing.T) {
	args, err := buildExternalArgs("mplayer", "http://stream.example.com/live.mp3", 55, "")
	if err != nil {
		t.Fatalf("buildExternalArgs error: %v", err)
	}
	found := false
	for _, a := range args {
		if a == "-slave" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected mplayer to be launched with -slave, got %v", args)
	}
}

func TestBuildExternalArgsUnknownBackend(t *testing.T) {
	if _, err := buildExternalArgs("nonexistent", "http://example.com/s", 50, ""); err == nil {
		t.Errorf("expected an error for an unknown backend")
	}
}

func TestBuildExternalArgsKnownBackends(t *testing.T) {
	for _, backend := range []string{"mpv", "vlc", "cvlc", "ffplay", "mplayer", "mpg123"} {
		args, err := buildExternalArgs(backend, "http://example.com/s", 50, "")
		if err != nil {
			t.Fatalf("backend %s: unexpected error %v", backend, err)
		}
		if len(args) == 0 || args[0] != backend {
			t.Errorf("backend %s: expected argv to start with the binary name, got %v", backend, args)
		}
		if args[len(args)-2] != "--" {
			t.Errorf("backend %s: expected -- before the stream URL, got %v", backend, args)
		}
	}
}

// TestMPVControlSendsJSONIPCCommands verifies the wire format mpv actually
// accepts: newline-delimited {"command":["set_property",...]} objects.
func TestMPVControlSendsJSONIPCCommands(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	c := &mpvControl{endpoint: &mpvIPCEndpoint{Addr: "test"}}
	c.attach(client)

	lines := make(chan string, 4)
	go func() {
		reader := bufio.NewReader(server)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				close(lines)
				return
			}
			lines <- strings.TrimSpace(line)
		}
	}()

	c.SetVolume(37)
	c.SetMute(true)

	want := []string{
		`{"command":["set_property","volume",37]}`,
		`{"command":["set_property","mute",true]}`,
	}
	for _, expected := range want {
		select {
		case got := <-lines:
			if got != expected {
				t.Errorf("IPC command mismatch:\n got %s\nwant %s", got, expected)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for IPC command %s", expected)
		}
	}

	if err := c.Close(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("Close error: %v", err)
	}
}

// TestMPVControlFlushesStateOnConnect covers volume changes made in the window
// between launching mpv and its IPC endpoint becoming connectable.
func TestMPVControlFlushesStateOnConnect(t *testing.T) {
	c := &mpvControl{endpoint: &mpvIPCEndpoint{Addr: "test"}}

	// Not connected yet: commands must be remembered, not lost.
	c.SetVolume(21)
	c.SetMute(true)

	client, server := net.Pipe()
	defer server.Close()

	received := make(chan string, 4)
	go func() {
		reader := bufio.NewReader(server)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			received <- strings.TrimSpace(line)
		}
	}()

	c.attach(client)

	want := []string{
		`{"command":["set_property","volume",21]}`,
		`{"command":["set_property","mute",true]}`,
	}
	for _, expected := range want {
		select {
		case got := <-received:
			if got != expected {
				t.Errorf("replayed command mismatch:\n got %s\nwant %s", got, expected)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for replayed command %s", expected)
		}
	}
	_ = c.Close()
}

func TestMPVControlAfterCloseIsNoOp(t *testing.T) {
	c := &mpvControl{endpoint: &mpvIPCEndpoint{Addr: "test"}}
	if err := c.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("expected Close to be idempotent, got %v", err)
	}
	// Must not panic or block once the process is gone.
	c.SetVolume(50)
	c.SetMute(true)

	if err := c.setProperty("volume", 10); !errors.Is(err, errMPVNotConnected) {
		t.Errorf("expected errMPVNotConnected after Close, got %v", err)
	}
}

func TestMPVIPCEndpointCleanup(t *testing.T) {
	endpoint, err := newMPVIPCEndpoint()
	if err != nil {
		t.Fatalf("newMPVIPCEndpoint error: %v", err)
	}
	if endpoint.Addr == "" {
		t.Fatalf("expected a non-empty IPC address")
	}
	endpoint.Cleanup()
	endpoint.Cleanup() // idempotent

	var nilEndpoint *mpvIPCEndpoint
	nilEndpoint.Cleanup() // must not panic
}

// TestManagerRoutesVolumeAndMuteToExternalControl is the Manager-level
// regression test: in-app volume and mute changes must actually reach the
// running external backend rather than being written into a void.
func TestManagerRoutesVolumeAndMuteToExternalControl(t *testing.T) {
	pm := NewManager("native", 80, nil)
	ctrl := &recordingControl{}
	pm.mu.Lock()
	pm.extCtrl = ctrl
	pm.status = StatusPlaying
	pm.mu.Unlock()

	pm.SetVolume(42)
	pm.ToggleMute()
	pm.ToggleMute()

	volumes, mutes, _ := ctrl.snapshot()
	if len(volumes) != 1 || volumes[0] != 42 {
		t.Errorf("expected SetVolume(42) to reach the backend, got %v", volumes)
	}
	if len(mutes) != 2 || !mutes[0] || mutes[1] {
		t.Errorf("expected mute then unmute to reach the backend, got %v", mutes)
	}

	// Volume changes are clamped to the 0-100 range the backends accept.
	pm.SetVolume(150)
	pm.SetVolume(-20)
	volumes, _, _ = ctrl.snapshot()
	if volumes[len(volumes)-2] != 100 || volumes[len(volumes)-1] != 0 {
		t.Errorf("expected clamped volumes 100 then 0, got %v", volumes)
	}

	if err := pm.Stop(); err != nil {
		t.Fatalf("Stop error: %v", err)
	}
	if _, _, closed := ctrl.snapshot(); !closed {
		t.Errorf("expected Stop to close the external control channel")
	}
	pm.mu.Lock()
	leaked := pm.extCtrl
	pm.mu.Unlock()
	if leaked != nil {
		t.Errorf("expected extCtrl to be cleared after Stop")
	}
}

func TestManagerTunerScalingReachesExternalControl(t *testing.T) {
	pm := NewManager("native", 80, nil)
	ctrl := &recordingControl{}
	pm.mu.Lock()
	pm.extCtrl = ctrl
	pm.status = StatusPlaying
	pm.mu.Unlock()

	// Weak reception attenuates the station on the external backend too.
	pm.UpdateTunerSignal(0.5, 98.5, "FM")
	volumes, _, _ := ctrl.snapshot()
	if len(volumes) != 1 || volumes[0] != 40 {
		t.Errorf("expected tuner signal to scale backend volume to 40, got %v", volumes)
	}

	// Leaving tuner mode restores the full master volume.
	pm.mu.Lock()
	pm.tunerMode = true
	pm.mu.Unlock()
	pm.SetTunerMode(false, 1.0, 0, "")
	volumes, _, _ = ctrl.snapshot()
	if volumes[len(volumes)-1] != 80 {
		t.Errorf("expected backend volume restored to 80 on leaving tuner mode, got %v", volumes)
	}

	if err := pm.Close(); err != nil {
		t.Fatalf("Close error: %v", err)
	}
}

func TestManagerPauseClosesExternalControl(t *testing.T) {
	pm := NewManager("native", 80, nil)
	ctrl := &recordingControl{}
	st := radio.Station{ID: "pause-ctrl", Name: "Pause Ctrl", URL: "http://example.com/s"}
	pm.mu.Lock()
	pm.extCtrl = ctrl
	pm.currentStation = &st
	pm.status = StatusPlaying
	pm.mu.Unlock()

	if err := pm.Pause(); err != nil {
		t.Fatalf("Pause error: %v", err)
	}
	if _, _, closed := ctrl.snapshot(); !closed {
		t.Errorf("expected Pause to close the external control channel")
	}
}

// TestMPVIPCVolumeControlIntegration drives a real mpv process end to end and
// reads the volume property back, proving control commands take effect. This is
// the check the old stdin channel fails.
func TestMPVIPCVolumeControlIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping mpv integration test in short mode")
	}
	mpvPath, err := exec.LookPath("mpv")
	if err != nil {
		t.Skip("mpv not installed; skipping IPC integration test")
	}

	endpoint, err := newMPVIPCEndpoint()
	if err != nil {
		t.Skipf("cannot create mpv IPC endpoint: %v", err)
	}

	// A synthetic tone with a null audio device keeps the test silent and offline.
	cmd := exec.Command(mpvPath, "--no-video", "--ao=null", "--no-terminal",
		"--volume=90", "--input-ipc-server="+endpoint.Addr, "--length=30",
		"--", "av://lavfi:sine=frequency=440")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start mpv: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	ctrl := newMPVControl(endpoint)
	defer ctrl.Close()

	// Wait for mpv to publish its IPC endpoint before asserting. mpv is kept
	// alive well past the assertions so a failure reports the wrong volume
	// rather than a broken pipe.
	var conn io.ReadWriteCloser
	connectDeadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(connectDeadline) {
		if c, err := dialMPVIPC(endpoint.Addr); err == nil {
			conn = c
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if conn == nil {
		t.Skip("mpv never exposed its IPC endpoint in this environment")
	}
	defer conn.Close()
	probe := newMPVProbe(conn)

	requestID := 0
	next := func() int { requestID++; return requestID }

	if got := probe.get(t, "volume", next()); got != float64(90) {
		t.Fatalf("expected mpv to start at volume 90, got %v", got)
	}

	ctrl.SetVolume(20)
	ctrl.SetMute(true)

	// The control channel connects asynchronously, so poll until it lands.
	var gotVol any
	pollDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(pollDeadline) {
		gotVol = probe.get(t, "volume", next())
		if gotVol == float64(20) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if gotVol != float64(20) {
		t.Errorf("expected mpv volume to change to 20 via IPC, got %v", gotVol)
	}
	if got := probe.get(t, "mute", next()); got != true {
		t.Errorf("expected mpv to be muted via IPC, got %v", got)
	}
}

// mpvProbe is an independent mpv IPC connection used by tests to read back the
// properties the player's own control channel has changed.
type mpvProbe struct {
	conn   io.ReadWriteCloser
	reader *bufio.Reader
}

func newMPVProbe(conn io.ReadWriteCloser) *mpvProbe {
	return &mpvProbe{conn: conn, reader: bufio.NewReader(conn)}
}

// get issues a get_property request and returns its value. mpv interleaves
// asynchronous event objects on the same socket, so replies are matched by
// request_id rather than by simply taking the next line.
func (p *mpvProbe) get(t *testing.T, name string, requestID int) any {
	t.Helper()
	if dl, ok := p.conn.(interface{ SetDeadline(time.Time) error }); ok {
		_ = dl.SetDeadline(time.Now().Add(5 * time.Second))
	}
	req := fmt.Sprintf(`{"command":["get_property","%s"],"request_id":%d}`+"\n", name, requestID)
	if _, err := io.WriteString(p.conn, req); err != nil {
		t.Fatalf("mpv IPC write failed: %v", err)
	}

	for {
		line, err := p.reader.ReadString('\n')
		if err != nil {
			t.Fatalf("mpv IPC read failed: %v", err)
		}
		var resp struct {
			Data      any    `json:"data"`
			Error     string `json:"error"`
			RequestID *int   `json:"request_id"`
			Event     string `json:"event"`
		}
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("mpv IPC decode failed for %q: %v", line, err)
		}
		if resp.Event != "" || resp.RequestID == nil || *resp.RequestID != requestID {
			continue // an asynchronous event or a stale reply
		}
		if resp.Error != "success" {
			t.Fatalf("mpv get_property %s returned %q", name, resp.Error)
		}
		return resp.Data
	}
}
