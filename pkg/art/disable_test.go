package art

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

// The album_art_*_enabled switches stop halpradio from sending track titles
// to that provider at all.
func TestDisabledProvidersAreNotContacted(t *testing.T) {
	s := newStubProviders(t)
	s.itunesArtwork = s.srv.URL + "/img/a/100x100bb.jpg"
	s.deezerCover = s.srv.URL + "/img/deezer.png"
	s.mbid = "b1a9c0e2-5b3c-4b4e-9d6e-0e5c1d2f3a4b"
	c := s.client(filepath.Join(t.TempDir(), "covers"), "")
	c.DisableITunes = true
	c.DisableDeezer = true
	c.DisableMusicBrainz = true

	_, err := c.Fetch(context.Background(), "Daft Punk", "One More Time", "Discovery")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	for _, p := range []string{"itunes", "deezer", "musicbrainz", "image"} {
		if n := s.count(p); n != 0 {
			t.Errorf("%s contacted %d times although disabled", p, n)
		}
	}
}

func TestDisableITunesFallsThroughToDeezer(t *testing.T) {
	s := newStubProviders(t)
	s.itunesArtwork = s.srv.URL + "/img/a/100x100bb.jpg"
	s.deezerCover = s.srv.URL + "/img/deezer.png"
	c := s.client(filepath.Join(t.TempDir(), "covers"), "")
	c.DisableITunes = true

	cover, err := c.Fetch(context.Background(), "Daft Punk", "One More Time", "Discovery")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if cover.Source != "Deezer" {
		t.Errorf("source %q, want Deezer", cover.Source)
	}
	if n := s.count("itunes"); n != 0 {
		t.Errorf("iTunes contacted %d times although disabled", n)
	}
}
