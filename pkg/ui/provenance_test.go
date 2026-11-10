package ui

import (
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/player/fingerprint"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/util"
)

// A station built from a party sync is marked Source "party", so the player
// itself refuses a private address even if CheckPartyStreamHost were skipped
// (or the name later rebinds to a LAN address).
func TestPartySyncStationIsStrictInPlayer(t *testing.T) {
	st, host, err := PartySyncStation(radio.NewStore(), party.SyncPayload{
		StationID: "p1", StationName: "P", StreamURL: "http://stream.example.com/live", FromHost: true,
	})
	if err != nil || host == "" {
		t.Fatalf("PartySyncStation: host %q err %v", host, err)
	}
	if st.Source != "party" {
		t.Fatalf("Source = %q, want party", st.Source)
	}
	st.URL = "http://192.168.1.20:8000/live" // the host check is skipped
	m := player.NewManager("mpv", 50, nil)
	_ = m.Play(st)
	if m.Status() != player.StatusError || !strings.Contains(m.Error(), "private") {
		t.Fatalf("player did not refuse the party LAN stream: status %v err %q", m.Status(), m.Error())
	}
}

func TestPartySyncStationKnownURLUsesStoredStation(t *testing.T) {
	store := radio.NewStore()
	lan := radio.Station{ID: "mine", Name: "Mine", URL: "http://192.168.1.10/s", Source: "local"}
	store.Local = append(store.Local, lan)
	st, host, err := PartySyncStation(store, party.SyncPayload{StationID: "x", StreamURL: lan.URL, FromHost: true})
	if err != nil || host != "" {
		t.Fatalf("host %q err %v", host, err)
	}
	if st.ID != "mine" || st.Source != "local" {
		t.Fatalf("got %+v, want the stored station", st)
	}
}

// Identify follows the station's provenance: a station the user added may be
// sampled on the LAN, a directory station stays strict.
func TestIdentifyClientFollowsStationSource(t *testing.T) {
	base := fingerprint.NewClient("k")
	lanURL := "http://192.168.1.10:8000/s"

	user := identifyClientFor(base, radio.Station{URL: lanURL, Source: "local"})
	if user.AllowPrivate == nil || !user.AllowPrivate(lanURL) {
		t.Fatal("user LAN station refused")
	}
	if user.AllowPrivate("http://10.0.0.1/other") {
		t.Fatal("policy must apply only to the captured station URL")
	}
	dir := identifyClientFor(base, radio.Station{URL: lanURL, Source: "radiobrowser"})
	if dir.AllowPrivate(lanURL) {
		t.Fatal("radiobrowser station allowed on a private address")
	}
	if base.AllowPrivate != nil {
		t.Fatal("shared client was mutated")
	}
	if user.Cache != base.Cache {
		t.Fatal("per-request client should share the cache")
	}
}

func TestIdentifyTrackCmdUsesCurrentStationSource(t *testing.T) {
	mp := player.NewMockPlayer(80, nil)
	m := NewModel(radio.NewStore(), mp, util.DefaultConfig())
	_ = mp.Play(radio.Station{ID: "s", URL: "http://192.168.1.10/s", Source: "radiobrowser"})
	if cmd := m.identifyTrackCmd(); cmd == nil {
		t.Fatal("no identify command")
	}
	if m.FingerprintClient.AllowPrivate != nil {
		t.Fatal("identifyTrackCmd changed the shared client's policy")
	}
}
