package desktop

import (
	"testing"

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
