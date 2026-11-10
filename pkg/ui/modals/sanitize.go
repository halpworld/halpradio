package modals

import (
	"github.com/halpworld/halpradio/pkg/ai"
	"github.com/halpworld/halpradio/pkg/util"
)

// SanitizeLinerNotes returns a copy of n with every field safe to print:
// control, bidi and escape characters removed and lengths capped (TTY-4).
// The input is not modified.
func SanitizeLinerNotes(n *ai.LinerNotes) *ai.LinerNotes {
	if n == nil {
		return nil
	}
	out := n.Clone()
	sd := util.SanitizeDisplay
	out.Artist = sd(n.Artist, 200)
	out.Title = sd(n.Title, 200)
	out.Album = sd(n.Album, 200)
	out.ReleaseYear = sd(n.ReleaseYear, 16)
	out.Country = sd(n.Country, 64)
	out.OriginCity = sd(n.OriginCity, 100)
	out.Genre = sd(n.Genre, 100)
	out.Label = sd(n.Label, 100)
	out.MBID = sd(n.MBID, 64)
	out.Source = sd(n.Source, 64)
	out.Biography = util.SanitizeMultiline(n.Biography, 2000, 60)
	out.AlbumNotes = util.SanitizeMultiline(n.AlbumNotes, 2000, 60)
	if len(out.Discography) > 100 {
		out.Discography = out.Discography[:100]
	}
	for i := range out.Discography {
		out.Discography[i].Title = sd(out.Discography[i].Title, 200)
		out.Discography[i].Year = sd(out.Discography[i].Year, 16)
		out.Discography[i].Type = sd(out.Discography[i].Type, 32)
	}
	if len(out.TrackTrivia) > 50 {
		out.TrackTrivia = out.TrackTrivia[:50]
	}
	for i := range out.TrackTrivia {
		out.TrackTrivia[i] = sd(out.TrackTrivia[i], 500)
	}
	return out
}
