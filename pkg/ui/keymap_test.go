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
