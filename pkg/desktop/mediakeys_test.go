package desktop

import (
	"testing"
)

func TestNormalizeKey(t *testing.T) {
	tests := []struct {
		input    string
		expected MediaAction
		valid    bool
	}{
		{"play", ActionPlayPause, true},
		{"pause", ActionPlayPause, true},
		{"playpause", ActionPlayPause, true},
		{"media_play_pause", ActionPlayPause, true},
		{"xf86audioplay", ActionPlayPause, true},
		{"xf86audiopause", ActionPlayPause, true},
		{"stop", ActionStop, true},
		{"media_stop", ActionStop, true},
		{"xf86audiostop", ActionStop, true},
		{"next", ActionNextStation, true},
		{"nexttrack", ActionNextStation, true},
		{"media_next", ActionNextStation, true},
		{"xf86audionext", ActionNextStation, true},
		{"prev", ActionPrevStation, true},
		{"previous", ActionPrevStation, true},
		{"media_prev", ActionPrevStation, true},
		{"xf86audioprev", ActionPrevStation, true},
		{"volume_up", ActionVolumeUp, true},
		{"volup", ActionVolumeUp, true},
		{"xf86audioraisevolume", ActionVolumeUp, true},
		{"volume_down", ActionVolumeDown, true},
		{"voldown", ActionVolumeDown, true},
		{"xf86audiolowervolume", ActionVolumeDown, true},
		{"mute", ActionMute, true},
		{"volume_mute", ActionMute, true},
		{"xf86audiomute", ActionMute, true},
		{"unknown_key", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			action, ok := NormalizeKey(tt.input)
			if ok != tt.valid {
				t.Errorf("NormalizeKey(%q) valid = %v; want %v", tt.input, ok, tt.valid)
			}
			if action != tt.expected {
				t.Errorf("NormalizeKey(%q) action = %v; want %v", tt.input, action, tt.expected)
			}
		})
	}
}

func TestParseAction(t *testing.T) {
	tests := []struct {
		input    string
		expected MediaAction
		valid    bool
	}{
		{"play-pause", ActionPlayPause, true},
		{"playpause", ActionPlayPause, true},
		{"toggle", ActionPlayPause, true},
		{"play", ActionPlay, true},
		{"pause", ActionPause, true},
		{"stop", ActionStop, true},
		{"next", ActionNextStation, true},
		{"next-station", ActionNextStation, true},
		{"nexttrack", ActionNextStation, true},
		{"prev", ActionPrevStation, true},
		{"previous", ActionPrevStation, true},
		{"volup", ActionVolumeUp, true},
		{"vol-up", ActionVolumeUp, true},
		{"+", ActionVolumeUp, true},
		{"voldown", ActionVolumeDown, true},
		{"vol-down", ActionVolumeDown, true},
		{"-", ActionVolumeDown, true},
		{"mute", ActionMute, true},
		{"toggle-mute", ActionMute, true},
		{"random", ActionRandom, true},
		{"shuffle", ActionRandom, true},
		{"quit", ActionQuit, true},
		{"exit", ActionQuit, true},
		{"status", ActionStatus, true},
		{"info", ActionStatus, true},
		{"invalid", "", false},
		{"", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			action, ok := ParseAction(tt.input)
			if ok != tt.valid {
				t.Errorf("ParseAction(%q) valid = %v; want %v", tt.input, ok, tt.valid)
			}
			if action != tt.expected {
				t.Errorf("ParseAction(%q) action = %v; want %v", tt.input, action, tt.expected)
			}
		})
	}
}
