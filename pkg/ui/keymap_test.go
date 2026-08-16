package ui

import (
	"testing"
)

func TestDefaultKeyMap(t *testing.T) {
	km := DefaultKeyMap()

	if len(km.Up.Keys()) == 0 {
		t.Errorf("Expected Up keys defined")
	}
	if len(km.Down.Keys()) == 0 {
		t.Errorf("Expected Down keys defined")
	}
	if len(km.PlayPause.Keys()) == 0 {
		t.Errorf("Expected PlayPause keys defined")
	}
	if len(km.Stop.Keys()) == 0 {
		t.Errorf("Expected Stop keys defined")
	}
	if len(km.NextStation.Keys()) == 0 {
		t.Errorf("Expected NextStation keys defined")
	}
	if len(km.PrevStation.Keys()) == 0 {
		t.Errorf("Expected PrevStation keys defined")
	}
	if len(km.VolUp.Keys()) == 0 {
		t.Errorf("Expected VolUp keys defined")
	}
	if len(km.VolDown.Keys()) == 0 {
		t.Errorf("Expected VolDown keys defined")
	}
	if len(km.Mute.Keys()) == 0 {
		t.Errorf("Expected Mute keys defined")
	}
	if len(km.Quit.Keys()) == 0 {
		t.Errorf("Expected Quit keys defined")
	}
	if len(km.Theme.Keys()) == 0 {
		t.Errorf("Expected Theme keys defined")
	}
	if len(km.Timer.Keys()) == 0 {
		t.Errorf("Expected Timer keys defined")
	}
	if len(km.Help.Keys()) == 0 {
		t.Errorf("Expected Help keys defined")
	}
	if len(km.Search.Keys()) == 0 {
		t.Errorf("Expected Search keys defined")
	}
}

func TestKeyMapMediaKeyVariations(t *testing.T) {
	km := DefaultKeyMap()

	hasKey := func(keys []string, target string) bool {
		for _, k := range keys {
			if k == target {
				return true
			}
		}
		return false
	}

	// Verify next station bindings include keyboard layout variants & media keys
	nextKeys := km.NextStation.Keys()
	for _, expected := range []string{"n", "]", "next", "media_next"} {
		if !hasKey(nextKeys, expected) {
			t.Errorf("NextStation missing binding %q", expected)
		}
	}

	// Verify prev station bindings include keyboard layout variants & media keys
	prevKeys := km.PrevStation.Keys()
	for _, expected := range []string{"N", "[", "prev", "media_prev"} {
		if !hasKey(prevKeys, expected) {
			t.Errorf("PrevStation missing binding %q", expected)
		}
	}

	// Verify play/pause bindings include space, enter, and hardware media keys
	playKeys := km.PlayPause.Keys()
	for _, expected := range []string{"space", "enter", "play", "pause", "playpause", "media_play_pause"} {
		if !hasKey(playKeys, expected) {
			t.Errorf("PlayPause missing binding %q", expected)
		}
	}

	// Verify stop bindings include s, x (standard media stop), and hardware stop
	stopKeys := km.Stop.Keys()
	for _, expected := range []string{"s", "x", "stop", "media_stop"} {
		if !hasKey(stopKeys, expected) {
			t.Errorf("Stop missing binding %q", expected)
		}
	}

	// Verify volume keys support AZERTY, ISO, QWERTZ, and compact layouts (+, =, >, -, _, <)
	volUpKeys := km.VolUp.Keys()
	for _, expected := range []string{"+", "=", ">", "volume_up"} {
		if !hasKey(volUpKeys, expected) {
			t.Errorf("VolUp missing binding %q", expected)
		}
	}

	volDownKeys := km.VolDown.Keys()
	for _, expected := range []string{"-", "_", "<", "volume_down"} {
		if !hasKey(volDownKeys, expected) {
			t.Errorf("VolDown missing binding %q", expected)
		}
	}

	// Verify mute bindings include m, M, 0, and volume_mute
	muteKeys := km.Mute.Keys()
	for _, expected := range []string{"m", "M", "0", "mute", "volume_mute"} {
		if !hasKey(muteKeys, expected) {
			t.Errorf("Mute missing binding %q", expected)
		}
	}
}
