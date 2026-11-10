package app

import (
	"testing"

	"github.com/halpworld/halpradio/pkg/radio"
)

// A URL typed on the command line is the user's own, so it is marked
// "custom" and the player allows LAN hosts for it.
func TestResolveStationDirectURLIsCustom(t *testing.T) {
	st, err := resolveStation("http://192.168.1.10:8000/live", false, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Source != "custom" || !radio.UserSource(st.Source) {
		t.Fatalf("Source = %q, want custom", st.Source)
	}
}
