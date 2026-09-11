package lyrics

import (
	"strings"
	"testing"
	"time"
)

func TestSplitTrackTitle(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantArtist string
		wantTitle  string
	}{
		{name: "empty", raw: "", wantArtist: "", wantTitle: ""},
		{name: "whitespace only", raw: "   \t ", wantArtist: "", wantTitle: ""},
		{name: "plain hyphen", raw: "Daft Punk - Voyager", wantArtist: "Daft Punk", wantTitle: "Voyager"},
		{name: "extra whitespace", raw: "  Daft Punk   -   Voyager  ", wantArtist: "Daft Punk", wantTitle: "Voyager"},
		{name: "en dash", raw: "Daft Punk – Voyager", wantArtist: "Daft Punk", wantTitle: "Voyager"},
		{name: "em dash", raw: "Daft Punk — Voyager", wantArtist: "Daft Punk", wantTitle: "Voyager"},
		{name: "unspaced double hyphen", raw: "Daft Punk--Voyager", wantArtist: "Daft Punk", wantTitle: "Voyager"},
		{name: "unspaced hyphen", raw: "Daft Punk-Voyager", wantArtist: "Daft Punk", wantTitle: "Voyager"},
		{name: "colon separator", raw: "Kraftwerk : Autobahn", wantArtist: "Kraftwerk", wantTitle: "Autobahn"},
		{name: "title by artist", raw: "Voyager by Daft Punk", wantArtist: "Daft Punk", wantTitle: "Voyager"},
		{
			name: "title cased By is not a separator", raw: "Stand By Me",
			wantArtist: "", wantTitle: "",
		},
		{name: "station name only", raw: "Radio Paradise", wantArtist: "", wantTitle: ""},
		{
			name: "now playing banner", raw: "Now Playing: Daft Punk - Voyager",
			wantArtist: "Daft Punk", wantTitle: "Voyager",
		},
		{
			name: "decorated banner", raw: "*** NOW PLAYING *** Daft Punk - Voyager ***",
			wantArtist: "Daft Punk", wantTitle: "Voyager",
		},
		{
			name: "official video suffix", raw: "Daft Punk - Voyager (Official Video)",
			wantArtist: "Daft Punk", wantTitle: "Voyager",
		},
		{
			name: "bracketed suffix", raw: "Daft Punk - Voyager [HQ]",
			wantArtist: "Daft Punk", wantTitle: "Voyager",
		},
		{
			name: "bitrate bracket", raw: "DJ Shadow - Midnight In A Perfect World [128kbps]",
			wantArtist: "DJ Shadow", wantTitle: "Midnight In A Perfect World",
		},
		{name: "topic channel suffix", raw: "Daft Punk - Topic", wantArtist: "", wantTitle: ""},
		{
			name: "trailing station pipe", raw: "Daft Punk - Voyager | Radio X FM",
			wantArtist: "Daft Punk", wantTitle: "Voyager",
		},
		{
			name: "quoted title", raw: `Daft Punk - "Voyager"`,
			wantArtist: "Daft Punk", wantTitle: "Voyager",
		},
		{
			name: "wrapping quotes", raw: `"Daft Punk - Voyager"`,
			wantArtist: "Daft Punk", wantTitle: "Voyager",
		},
		{
			name: "curly quotes", raw: "“Daft Punk - Voyager”",
			wantArtist: "Daft Punk", wantTitle: "Voyager",
		},
		{
			name: "html entities", raw: "Simon &amp; Garfunkel - The Sound of Silence",
			wantArtist: "Simon & Garfunkel", wantTitle: "The Sound of Silence",
		},
		{
			name: "playlist track number", raw: "01. Daft Punk - Voyager",
			wantArtist: "Daft Punk", wantTitle: "Voyager",
		},
		{
			name: "playlist track number with dash", raw: "03 - Daft Punk - Voyager",
			wantArtist: "Daft Punk", wantTitle: "Voyager",
		},
		{name: "advert", raw: "Advertisement", wantArtist: "", wantTitle: ""},
		{name: "commercial break", raw: "Commercial Break", wantArtist: "", wantTitle: ""},
		{name: "station liner", raw: "You're listening to Radio X", wantArtist: "", wantTitle: ""},
		{name: "ad slug", raw: "buy ads at adsite.com - listen now", wantArtist: "", wantTitle: ""},
		{name: "jingle", raw: "Jingle - Radio X", wantArtist: "", wantTitle: ""},
		{name: "url only", raw: "http://radio.example.com", wantArtist: "", wantTitle: ""},
		{name: "unknown placeholders", raw: "Unknown - Unknown", wantArtist: "", wantTitle: ""},
		{name: "unknown artist track number", raw: "Unknown Artist - Track 01", wantArtist: "", wantTitle: ""},
		{name: "various artists", raw: "Various Artists - Compilation", wantArtist: "", wantTitle: ""},
		{name: "punctuation only", raw: "- - -", wantArtist: "", wantTitle: ""},
		{name: "slash in artist", raw: "AC/DC - Thunderstruck", wantArtist: "AC/DC", wantTitle: "Thunderstruck"},
		{name: "hyphenated artist", raw: "Jay-Z - 99 Problems", wantArtist: "Jay-Z", wantTitle: "99 Problems"},
		{
			name: "plus in artist", raw: "Florence + The Machine - Dog Days Are Over (Official)",
			wantArtist: "Florence + The Machine", wantTitle: "Dog Days Are Over",
		},
		{
			name: "trailing period kept", raw: "Bruce Springsteen - Born in the U.S.A.",
			wantArtist: "Bruce Springsteen", wantTitle: "Born in the U.S.A.",
		},
		{
			name: "feature kept in the raw title", raw: "Beyoncé - Halo (feat. Jay-Z)",
			wantArtist: "Beyoncé", wantTitle: "Halo (feat. Jay-Z)",
		},
		{
			name: "third segment stays with the title", raw: "Daft Punk - Voyager - Discovery",
			wantArtist: "Daft Punk", wantTitle: "Voyager - Discovery",
		},
		{
			name: "numeric title is not a placeholder", raw: "Prince - 1999",
			wantArtist: "Prince", wantTitle: "1999",
		},
		{
			name: "control characters", raw: "Daft Punk - Voyager\x00\x07",
			wantArtist: "Daft Punk", wantTitle: "Voyager",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			artist, title := SplitTrackTitle(tt.raw)
			if artist != tt.wantArtist || title != tt.wantTitle {
				t.Errorf("SplitTrackTitle(%q) = (%q, %q), want (%q, %q)",
					tt.raw, artist, title, tt.wantArtist, tt.wantTitle)
			}
		})
	}
}

func TestSplitTrackTitleNeverPartial(t *testing.T) {
	// Either both halves are populated or both are empty.
	inputs := []string{
		"", "x", "Radio", "- Title", "Artist -", "Now Playing:", "|||", "a - b",
		"Artist - ", " - Title", "feat. Someone", "??? - ???",
	}
	for _, in := range inputs {
		artist, title := SplitTrackTitle(in)
		if (artist == "") != (title == "") {
			t.Errorf("SplitTrackTitle(%q) returned a partial result (%q, %q)", in, artist, title)
		}
	}
}

func TestNormalizeQuery(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "empty", in: "", want: ""},
		{name: "whitespace collapsed", in: "  Daft   Punk\t ", want: "Daft Punk"},
		{name: "feat in parens", in: "Halo (feat. Jay-Z)", want: "Halo"},
		{name: "ft in brackets", in: "Halo [ft. Jay-Z]", want: "Halo"},
		{name: "bare feat tail", in: "Track feat. Someone", want: "Track"},
		{name: "bare ft tail", in: "Track ft. Someone", want: "Track"},
		{name: "official video", in: "Song Title (Official Music Video)", want: "Song Title"},
		{name: "explicit bracket", in: "Song [Explicit]", want: "Song"},
		{name: "remaster paren", in: "Thriller (2001 Remaster)", want: "Thriller"},
		{name: "remaster tail", in: "Nothing Else Matters - Remastered 2021", want: "Nothing Else Matters"},
		{name: "trailing year", in: "Blue Monday 1988", want: "Blue Monday"},
		{name: "bracketed trailing year", in: "Blue Monday (1988)", want: "Blue Monday"},
		{name: "title that is only a year is kept", in: "1999", want: "1999"},
		{name: "numeric title kept", in: "2112", want: "2112"},
		{name: "remix parenthetical kept", in: "Around the World (Remix)", want: "Around the World (Remix)"},
		{name: "live parenthetical kept", in: "Song (Live at Wembley)", want: "Song (Live at Wembley)"},
		{name: "html entity", in: "Simon &amp; Garfunkel", want: "Simon & Garfunkel"},
		{name: "already clean", in: "Discovery", want: "Discovery"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeQuery(tt.in); got != tt.want {
				t.Errorf("normalizeQuery(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestNormalizeKeyIsLowercase(t *testing.T) {
	got := normalizeKey("DAFT PUNK (Official Video)")
	if got != "daft punk" {
		t.Errorf("normalizeKey = %q, want %q", got, "daft punk")
	}
}

func TestCacheKey(t *testing.T) {
	base := cacheKey("Daft Punk", "Voyager", "Discovery", 227*time.Second)

	if !strings.Contains(base, "daft punk") || !strings.Contains(base, "voyager") {
		t.Fatalf("cache key %q does not contain normalised metadata", base)
	}
	if !strings.HasSuffix(base, "|227") {
		t.Errorf("cache key %q should end with the duration in seconds", base)
	}

	tests := []struct {
		name  string
		key   string
		equal bool
	}{
		{name: "identical", key: cacheKey("Daft Punk", "Voyager", "Discovery", 227*time.Second), equal: true},
		{name: "case insensitive", key: cacheKey("DAFT PUNK", "voyager", "DISCOVERY", 227*time.Second), equal: true},
		{name: "noise insensitive", key: cacheKey("Daft Punk", "Voyager (Official Video)", "Discovery", 227*time.Second), equal: true},
		{name: "rounds sub-second durations", key: cacheKey("Daft Punk", "Voyager", "Discovery", 227*time.Second+400*time.Millisecond), equal: true},
		{name: "different title", key: cacheKey("Daft Punk", "Aerodynamic", "Discovery", 227*time.Second), equal: false},
		{name: "different duration", key: cacheKey("Daft Punk", "Voyager", "Discovery", 300*time.Second), equal: false},
		{name: "different album", key: cacheKey("Daft Punk", "Voyager", "Homework", 227*time.Second), equal: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if (tt.key == base) != tt.equal {
				t.Errorf("cacheKey %q vs %q: equal=%v, want %v", tt.key, base, tt.key == base, tt.equal)
			}
		})
	}

	if zero := cacheKey("", "", "", 0); zero != "|||0" {
		t.Errorf("cacheKey of empty metadata = %q, want %q", zero, "|||0")
	}
}

func TestUnwrapQuotes(t *testing.T) {
	tests := []struct{ in, want string }{
		{`"quoted"`, "quoted"},
		{"'quoted'", "quoted"},
		{"“quoted”", "quoted"},
		{"«quoted»", "quoted"},
		{`"unbalanced`, `"unbalanced`},
		{`""`, `""`},
		{"plain", "plain"},
	}
	for _, tt := range tests {
		if got := unwrapQuotes(tt.in); got != tt.want {
			t.Errorf("unwrapQuotes(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
