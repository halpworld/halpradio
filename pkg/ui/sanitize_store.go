package ui

import (
	"github.com/halpworld/halpradio/pkg/ai"
	"github.com/halpworld/halpradio/pkg/art"
	"github.com/halpworld/halpradio/pkg/player/fingerprint"
	"github.com/halpworld/halpradio/pkg/ui/modals"
	"github.com/halpworld/halpradio/pkg/util"
)

// TTY-4: song identification results, liner notes, cover metadata and DJ
// scripts come from third-party databases or a language model. They are
// sanitized once, when Update stores them in the model, so every sink
// (window title, player bar, drawers, history, clipboard) gets clean text.

func sanitizeIdentified(r *fingerprint.Result) *fingerprint.Result {
	if r == nil {
		return nil
	}
	out := *r
	out.Artist = util.SanitizeDisplay(r.Artist, 200)
	out.Title = util.SanitizeDisplay(r.Title, 200)
	out.Album = util.SanitizeDisplay(r.Album, 200)
	out.Source = util.SanitizeDisplay(r.Source, 64)
	out.StationID = util.SanitizeDisplay(r.StationID, 128)
	out.StationName = util.SanitizeDisplay(r.StationName, 200)
	return &out
}

func sanitizeLinerNotes(n *ai.LinerNotes) *ai.LinerNotes {
	return modals.SanitizeLinerNotes(n)
}

func sanitizeCover(c *art.Cover) *art.Cover {
	if c == nil {
		return nil
	}
	out := *c
	out.Source = util.SanitizeDisplay(c.Source, 64)
	out.Artist = util.SanitizeDisplay(c.Artist, 200)
	out.Title = util.SanitizeDisplay(c.Title, 200)
	out.Album = util.SanitizeDisplay(c.Album, 200)
	out.URL = util.SanitizeDisplay(c.URL, 2048)
	return &out
}

func sanitizeDJScript(s string) string {
	return util.SanitizeMultiline(s, 2000, 40)
}
