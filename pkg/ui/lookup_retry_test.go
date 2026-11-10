package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/util"
)

// NET-7: a lyrics lookup that failed because the providers were unreachable
// used to clear LyricsTrackKey, so every metadata tick sent the same lookup
// again. Now ticks leave it alone and only reopening the drawer retries.
func TestLyricsUnreachableDoesNotRetryEveryTick(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", home+"/.cache")
	cfg := util.DefaultConfig()
	cfg.LyricsEnabled = true
	mp := player.NewMockPlayer(80, nil)
	m := NewModel(radio.NewStore(), mp, cfg)
	if m.LyricsClient == nil {
		t.Skip("no lyrics client in this configuration")
	}
	_ = mp.Play(radio.Station{ID: "s1", Name: "S", URL: "http://example.com/"})
	mp.SetStatus(player.StatusPlaying)
	mp.SetTrack("Tycho - A Walk")
	m.PlayingID = "s1"

	if cmds := m.syncNowPlaying(); len(cmds) == 0 || !m.IsFetchingLyrics {
		a, ti, _ := m.nowPlayingParts()
		t.Fatalf("first sync did not start a lookup: status=%v track=%q parts=%q/%q enabled=%v", mp.Status(), mp.CurrentTrack(), a, ti, m.Config.LyricsEnabled)
	}
	key := m.LyricsTrackKey
	nm, _ := m.Update(LyricsFetchedMsg{TrackKey: key, Err: errors.New("dial tcp: connection refused")})
	m = nm.(Model)

	for i := 0; i < 3; i++ {
		m.syncNowPlaying()
		if m.IsFetchingLyrics {
			t.Fatalf("tick %d resent the failed lyrics lookup", i)
		}
	}

	m.ShowLyrics = false
	m.toggleLyricsDrawer()
	if !m.IsFetchingLyrics {
		t.Fatal("reopening the drawer did not retry the failed lookup")
	}
}

// E: a refused Ollama URL (remote plain http) is reported instead of the DJ
// silently using offline templates.
func TestDJShowsRefusedOllamaURL(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := util.DefaultConfig()
	cfg.AIDJEnabled = true
	cfg.AIDJOllamaURL = "http://10.1.2.3:11434"
	mp := player.NewMockPlayer(80, nil)
	m := NewModel(radio.NewStore(), mp, cfg)
	if !strings.Contains(m.StatusMessage, "Ollama URL not used") {
		t.Errorf("no startup warning: %q", m.StatusMessage)
	}
	m.StatusMessage = ""
	_ = mp.Play(radio.Station{ID: "s1", Name: "S", URL: "http://example.com/"})
	m.PlayingID = "s1"
	m.startDJAnnouncement(true)
	m.cancelDJAnnouncement()
	if !strings.Contains(m.StatusMessage, "Ollama URL not used") {
		t.Fatalf("status %q", m.StatusMessage)
	}

	cfg.AIDJOllamaURL = "http://127.0.0.1:11434"
	m2 := NewModel(radio.NewStore(), mp, cfg)
	if w := m2.djOllamaWarning(); w != "" {
		t.Errorf("loopback Ollama URL warned: %q", w)
	}
}

// The privacy switches in config.yaml reach the lyrics and art clients.
func TestProviderSwitchesWired(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := util.DefaultConfig()
	cfg.LyricsEnabled = true
	cfg.AlbumArtEnabled = true
	cfg.AlbumArtProtocol = "halfblock"
	cfg.LyricsNetEaseEnabled = false
	cfg.ArtITunesEnabled = false
	cfg.ArtDeezerEnabled = false
	cfg.ArtMusicBrainzEnabled = false
	m := NewModel(radio.NewStore(), player.NewMockPlayer(80, nil), cfg)
	if m.LyricsClient == nil || !m.LyricsClient.DisableNetEase {
		t.Error("lyrics_netease_enabled: false not applied")
	}
	if m.ArtClient == nil || !m.ArtClient.DisableITunes || !m.ArtClient.DisableDeezer || !m.ArtClient.DisableMusicBrainz {
		t.Errorf("album_art_*_enabled: false not applied: %+v", m.ArtClient)
	}
	on := NewModel(radio.NewStore(), player.NewMockPlayer(80, nil), util.DefaultConfig())
	if on.LyricsClient != nil && on.LyricsClient.DisableNetEase {
		t.Error("NetEase disabled by default")
	}
}
