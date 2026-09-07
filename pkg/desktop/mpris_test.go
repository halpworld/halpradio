package desktop

import (
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

func TestMapStatusToMPRIS(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"PLAYING", "Playing"},
		{"playing", "Playing"},
		{"PAUSED", "Paused"},
		{"paused", "Paused"},
		{"STOPPED", "Stopped"},
		{"stopped", "Stopped"},
		{"ERROR", "Stopped"},
		{"", "Stopped"},
		{"unknown", "Stopped"},
	}

	for _, tt := range tests {
		got := MapStatusToMPRIS(tt.input)
		if got != tt.want {
			t.Errorf("MapStatusToMPRIS(%q) = %q; want %q", tt.input, got, tt.want)
		}
	}
}

func TestBuildMPRISMetadata(t *testing.T) {
	t.Run("Artist and title formatted", func(t *testing.T) {
		meta := BuildMPRISMetadata("ChillHop Radio", "Lofi / Beats", "Kavv - Coffee Break", "http://stream.example.com/audio")

		if meta["xesam:title"] != "Coffee Break" {
			t.Errorf("expected title 'Coffee Break', got %v", meta["xesam:title"])
		}

		artists, ok := meta["xesam:artist"].([]string)
		if !ok || len(artists) != 1 || artists[0] != "Kavv" {
			t.Errorf("expected artist ['Kavv'], got %v", meta["xesam:artist"])
		}

		if meta["xesam:album"] != "ChillHop Radio" {
			t.Errorf("expected album 'ChillHop Radio', got %v", meta["xesam:album"])
		}

		genres, ok := meta["xesam:genre"].([]string)
		if !ok || len(genres) != 1 || genres[0] != "Lofi / Beats" {
			t.Errorf("expected genre ['Lofi / Beats'], got %v", meta["xesam:genre"])
		}

		if meta["xesam:url"] != "http://stream.example.com/audio" {
			t.Errorf("expected url http://stream.example.com/audio, got %v", meta["xesam:url"])
		}
	})

	t.Run("Title only, no artist dash", func(t *testing.T) {
		meta := BuildMPRISMetadata("Radio Paradise", "Eclectic", "Solo Piano Symphony", "")

		if meta["xesam:title"] != "Solo Piano Symphony" {
			t.Errorf("expected title 'Solo Piano Symphony', got %v", meta["xesam:title"])
		}

		artists, ok := meta["xesam:artist"].([]string)
		if !ok || len(artists) != 1 || artists[0] != "Radio Paradise" {
			t.Errorf("expected fallback artist 'Radio Paradise', got %v", meta["xesam:artist"])
		}
	})

	t.Run("Empty track title defaults to station name", func(t *testing.T) {
		meta := BuildMPRISMetadata("BBC Radio 1", "Pop", "", "https://bbc.co.uk/stream")

		if meta["xesam:title"] != "BBC Radio 1" {
			t.Errorf("expected title 'BBC Radio 1', got %v", meta["xesam:title"])
		}
	})
}

func TestMPRISMethodsAndCallbacks(t *testing.T) {
	var calledNext, calledPrev, calledPause, calledPlayPause, calledStop, calledPlay, calledQuit bool
	var setVol float64

	handler := MPRISHandler{
		OnNext:      func() { calledNext = true },
		OnPrev:      func() { calledPrev = true },
		OnPause:     func() { calledPause = true },
		OnPlayPause: func() { calledPlayPause = true },
		OnStop:      func() { calledStop = true },
		OnPlay:      func() { calledPlay = true },
		OnVolume:    func(v float64) { setVol = v },
		OnQuit:      func() { calledQuit = true },
	}

	server := &MPRISServer{
		handler: handler,
	}

	root := &mprisRoot{server: server}
	player := &mprisPlayer{server: server}

	_ = root.Raise()
	_ = root.Quit()
	if !calledQuit {
		t.Errorf("expected OnQuit called")
	}

	_ = player.Next()
	if !calledNext {
		t.Errorf("expected OnNext called")
	}

	_ = player.Previous()
	if !calledPrev {
		t.Errorf("expected OnPrev called")
	}

	_ = player.Pause()
	if !calledPause {
		t.Errorf("expected OnPause called")
	}

	_ = player.PlayPause()
	if !calledPlayPause {
		t.Errorf("expected OnPlayPause called")
	}

	_ = player.Stop()
	if !calledStop {
		t.Errorf("expected OnStop called")
	}

	_ = player.Play()
	if !calledPlay {
		t.Errorf("expected OnPlay called")
	}

	_ = player.SetPosition(dbus.ObjectPath("/"), 100)
	_ = player.OpenUri("http://example.com")

	if handler.OnVolume != nil {
		handler.OnVolume(0.75)
		if setVol != 0.75 {
			t.Errorf("expected setVol 0.75, got %f", setVol)
		}
	}
}

func TestMPRISServerNilSafe(t *testing.T) {
	var s *MPRISServer
	s.UpdatePlaybackState("PLAYING", "Station", "Genre", "Track", "URL", 0.8)
	_ = s.Close()
}

// fakeProps mimics the parts of *prop.Properties that MPRISServer relies on.
// onSet reproduces godbus' behaviour of invoking a property Callback
// synchronously, from inside the setter, while its own lock is held.
type fakeProps struct {
	mu    sync.Mutex
	set   map[string]any
	onSet func(iface, property string, v any)
}

func newFakeProps() *fakeProps {
	return &fakeProps{set: map[string]any{}}
}

func (f *fakeProps) SetMust(iface, property string, v any) {
	f.mu.Lock()
	f.set[iface+"."+property] = v
	cb := f.onSet
	f.mu.Unlock()
	if cb != nil {
		cb(iface, property, v)
	}
}

func (f *fakeProps) value(key string) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.set[key]
}

// TestUpdatePlaybackStateDoesNotDeadlockOnPropertyCallback is a regression test
// for issue #26: publishing playback state while holding MPRISServer.mu let the
// D-Bus Volume callback re-enter the same non-reentrant mutex, wedging the
// Bubble Tea update loop and freezing the whole TUI on Linux.
func TestUpdatePlaybackStateDoesNotDeadlockOnPropertyCallback(t *testing.T) {
	props := newFakeProps()
	var gotVolume float64
	server := &MPRISServer{
		props:   props,
		handler: MPRISHandler{OnVolume: func(v float64) { gotVolume = v }},
	}
	props.onSet = func(_, property string, v any) {
		if property == "Volume" {
			// godbus runs the property Callback inline; the callback in turn
			// touches MPRISServer state.
			server.onRemoteVolume(v.(float64))
		}
	}

	done := make(chan struct{})
	go func() {
		server.UpdatePlaybackState("PLAYING", "Radio Paradise", "Eclectic", "Some Song", "http://example.com/stream", 0.75)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("UpdatePlaybackState deadlocked while publishing D-Bus properties")
	}

	if gotVolume != 0.75 {
		t.Errorf("expected volume callback to receive 0.75, got %v", gotVolume)
	}
}

// TestUpdatePlaybackStatePublishesReadOnlyProperties guards the second half of
// issue #26: PlaybackStatus and Metadata are exported read-only, so routing
// them through prop.Properties.Set silently dropped every update.
func TestUpdatePlaybackStatePublishesReadOnlyProperties(t *testing.T) {
	props := newFakeProps()
	server := &MPRISServer{props: props}

	server.UpdatePlaybackState("PAUSED", "Radio Paradise", "Eclectic", "Some Song", "http://example.com/stream", 0.4)

	if got := props.value(playerInterface + ".PlaybackStatus"); got != "Paused" {
		t.Errorf("expected PlaybackStatus %q, got %v", "Paused", got)
	}
	meta, ok := props.value(playerInterface + ".Metadata").(map[string]any)
	if !ok {
		t.Fatalf("expected Metadata to be published as map[string]any, got %T", props.value(playerInterface+".Metadata"))
	}
	if meta["xesam:title"] != "Some Song" {
		t.Errorf("expected xesam:title %q, got %v", "Some Song", meta["xesam:title"])
	}
	if got := props.value(playerInterface + ".Volume"); got != 0.4 {
		t.Errorf("expected Volume 0.4, got %v", got)
	}
}

// TestSetPropSurvivesPanickingWriter ensures a misbehaving D-Bus stack cannot
// take down the TUI: prop.Properties.SetMust panics on a type mismatch.
func TestSetPropSurvivesPanickingWriter(t *testing.T) {
	setProp(panicProps{}, playerInterface, "Volume", 0.5)
	setProp(nil, playerInterface, "Volume", 0.5)
}

type panicProps struct{}

func (panicProps) SetMust(string, string, any) { panic("boom") }

// TestMPRISMetadataIsStorable verifies the metadata map round-trips through
// dbus.Store, which is what prop.Properties.SetMust uses internally.
func TestMPRISMetadataIsStorable(t *testing.T) {
	meta := BuildMPRISMetadata("Radio Paradise", "Eclectic", "Some Song", "http://example.com/stream")
	dest := map[string]any{}
	if err := dbus.Store([]any{meta}, &dest); err != nil {
		t.Fatalf("metadata is not storable into the exported property: %v", err)
	}
	if dest["xesam:title"] != "Some Song" {
		t.Errorf("expected xesam:title to survive the round-trip, got %v", dest["xesam:title"])
	}
}
