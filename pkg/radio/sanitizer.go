package radio

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

// SanitizedResult represents the outcome of cleaning a raw stream title.
type SanitizedResult struct {
	CleanTitle      string `json:"clean_title"`
	OriginalTitle   string `json:"original_title"`
	Artist          string `json:"artist,omitempty"`
	Title           string `json:"title,omitempty"`
	IsClean         bool   `json:"is_clean"`
	IsGenericOrSpam bool   `json:"is_generic_or_spam"`
	Reason          string `json:"reason,omitempty"`
}

var (
	// Ad & commercial keywords
	adRegex = regexp.MustCompile(`(?i)\b(buy|order)\s+ads?\s+at\b|\b(advertise|advertising)\s+(with\s+us|at|on)\b|\b(sponsor(ed)?(\s+by)?|commercial\s+break|ad\s+break)\b`)

	// DJ & radio station slogans / jingles
	sloganRegex = regexp.MustCompile(`(?i)\bdj\s+[\w\s.-]+\s+in\s+the\s+mix\b|\b(live\s+on\s+air|on\s+the\s+air)\b|\b(best\s+of\s+the|greatest\s+hits|non[-\s]?stop\s+hits)\b|\b(now\s+playing\s+on|you('re|\s+are)\s+listening\s+to|tune\s+in\s+to)\b|\b(we('ll|\s+will)\s+be\s+right\s+back|stay\s+tuned)\b`)

	// Technical stream noise
	techNoiseRegex = regexp.MustCompile(`(?i)\b(stream[-\s]?\d+k(bps)?|\d+\s*kbps)\b|\b(station\s+id|server\s+restarted|buffering|connecting\.{3})\b|\b(stream\s+url:?)\b`)

	// URLs, domains, handles, phones
	urlRegex   = regexp.MustCompile(`(?i)(https?://\S+|www\.\S+|\b[a-zA-Z0-9.-]+\.(com|fm|org|net|io|co|de|uk|fr|ru|app|live)\b(/\S*)?)`)
	phoneRegex = regexp.MustCompile(`(?i)\b(call\s+\+?\d[\d\s\-]{6,})\b`)
	emailRegex = regexp.MustCompile(`(?i)[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)

	// Generic unknown metadata
	unknownRegex = regexp.MustCompile(`(?i)^(unknown(\s+artist|\s+track)?|no\s+artist|various\s+artists|unnamed|track\s*\d+|audio\s*track|http:?)$`)

	// Promotional banner prefixes (e.g. "*** NOW PLAYING ***", "[LIVE]")
	bannerPrefixRegex = regexp.MustCompile(`(?i)^[\s*#~_\-♫♪▶\[\](){}|•]*\b(now\s+playing|currently\s+playing|current\s+track)\b[\s*#~_\-♫♪▶\[\](){}|•]*[:-]?\s*`)

	// Decorative wrapper delimiters
	decorPrefixRegex = regexp.MustCompile(`^[\s*#~_\-♫♪▶\[\](){}|•]+`)
	decorSuffixRegex = regexp.MustCompile(`[\s*#~_\-♫♪▶\[\](){}|•]+$`)
)

// SanitizeTrackTitle cleans promotional noise, URLs, jingles, and spam from remote stream titles.
func SanitizeTrackTitle(rawTitle string, stationName string) SanitizedResult {
	orig := strings.TrimSpace(rawTitle)
	if orig == "" {
		return SanitizedResult{
			CleanTitle:      "",
			OriginalTitle:   "",
			IsClean:         false,
			IsGenericOrSpam: true,
			Reason:          "empty title",
		}
	}

	// 1. Unescape HTML entities (e.g. &amp; -> &, &quot; -> ")
	decoded := html.UnescapeString(orig)

	// Strip non-printable characters
	var b strings.Builder
	for _, r := range decoded {
		if r >= 32 && r != 127 && unicode.IsPrint(r) {
			b.WriteRune(r)
		}
	}
	s := strings.TrimSpace(b.String())

	// Early check for pure slogans, pure URLs, or pure technical noise
	if isPureSlogan(s) {
		return SanitizedResult{
			CleanTitle:      "",
			OriginalTitle:   orig,
			IsClean:         false,
			IsGenericOrSpam: true,
			Reason:          "station slogan/jingle detected",
		}
	}
	if isPureNoiseOrURL(s) {
		return SanitizedResult{
			CleanTitle:      "",
			OriginalTitle:   orig,
			IsClean:         false,
			IsGenericOrSpam: true,
			Reason:          "pure URL/promo noise",
		}
	}
	if isStationSelfReference(s, stationName) {
		return SanitizedResult{
			CleanTitle:      "",
			OriginalTitle:   orig,
			IsClean:         false,
			IsGenericOrSpam: true,
			Reason:          "station name self-reference",
		}
	}

	// 2. Strip leading banner prefixes (e.g. "*** NOW PLAYING *** Daft Punk - Voyager")
	s = bannerPrefixRegex.ReplaceAllString(s, "")
	s = decorPrefixRegex.ReplaceAllString(s, "")

	// 3. Strip composite noise (parentheticals, trailing delimiter promos like "-- buy ads at...")
	s = cleanCompositeNoise(s)

	// 4. Check if the string is purely spam / advertisement
	if adRegex.MatchString(s) {
		cleaned := stripAdSegments(s)
		if cleaned == "" || adRegex.MatchString(cleaned) || !strings.Contains(cleaned, " - ") {
			return SanitizedResult{
				CleanTitle:      "",
				OriginalTitle:   orig,
				IsClean:         false,
				IsGenericOrSpam: true,
				Reason:          "advertisement detected",
			}
		}
		s = cleaned
	}

	// 5. Check if the string is purely a DJ slogan / jingle
	if isPureSlogan(s) {
		return SanitizedResult{
			CleanTitle:      "",
			OriginalTitle:   orig,
			IsClean:         false,
			IsGenericOrSpam: true,
			Reason:          "station slogan/jingle detected",
		}
	}

	// 6. Check if title matches Station Name verbatim or is station frequency/slogan
	if isStationSelfReference(s, stationName) {
		return SanitizedResult{
			CleanTitle:      "",
			OriginalTitle:   orig,
			IsClean:         false,
			IsGenericOrSpam: true,
			Reason:          "station name self-reference",
		}
	}

	// 7. Check if title is purely an URL / email / phone
	if isPureNoiseOrURL(s) {
		return SanitizedResult{
			CleanTitle:      "",
			OriginalTitle:   orig,
			IsClean:         false,
			IsGenericOrSpam: true,
			Reason:          "pure URL/promo noise",
		}
	}

	// 8. Strip technical stream noise (e.g. "STREAM-128K")
	s = techNoiseRegex.ReplaceAllString(s, "")

	// 9. Clean extraneous decorative leading/trailing symbols
	s = decorPrefixRegex.ReplaceAllString(s, "")
	s = decorSuffixRegex.ReplaceAllString(s, "")
	s = strings.TrimSpace(s)

	// Check again if anything left
	if s == "" || unknownRegex.MatchString(s) {
		return SanitizedResult{
			CleanTitle:      "",
			OriginalTitle:   orig,
			IsClean:         false,
			IsGenericOrSpam: true,
			Reason:          "unknown or stripped to empty",
		}
	}

	// If after cleaning it is still a station self reference
	if isStationSelfReference(s, stationName) {
		return SanitizedResult{
			CleanTitle:      "",
			OriginalTitle:   orig,
			IsClean:         false,
			IsGenericOrSpam: true,
			Reason:          "station self-reference after clean",
		}
	}

	// 10. Parse Artist and Title
	artist, title := ParseArtistAndTitle(s)

	// If parsed title has no delimiter and matches slogan / noise
	if artist == "" && (sloganRegex.MatchString(title) || isPureSlogan(title)) {
		return SanitizedResult{
			CleanTitle:      "",
			OriginalTitle:   orig,
			IsClean:         false,
			IsGenericOrSpam: true,
			Reason:          "slogan without artist",
		}
	}

	// Clean duplicate artist/title (e.g. "Daft Punk - Daft Punk - Voyager")
	if artist != "" && title != "" {
		if strings.HasPrefix(strings.ToLower(title), strings.ToLower(artist)+" - ") {
			title = strings.TrimSpace(title[len(artist)+3:])
			s = fmt.Sprintf("%s - %s", artist, title)
		}
	}

	return SanitizedResult{
		CleanTitle:      s,
		OriginalTitle:   orig,
		Artist:          artist,
		Title:           title,
		IsClean:         true,
		IsGenericOrSpam: false,
	}
}

// IsDirtyOrGeneric reports whether a title is empty, advertising, a generic slogan, or station self-reference.
func IsDirtyOrGeneric(title string, stationName string) bool {
	res := SanitizeTrackTitle(title, stationName)
	return res.IsGenericOrSpam || !res.IsClean || res.CleanTitle == ""
}

func stripAdSegments(s string) string {
	delims := []string{" | ", " -- ", " // ", " - "}
	for _, delim := range delims {
		if strings.Contains(s, delim) {
			parts := strings.Split(s, delim)
			var valid []string
			for _, p := range parts {
				trimmed := strings.TrimSpace(p)
				if trimmed != "" && !adRegex.MatchString(trimmed) && !urlRegex.MatchString(trimmed) && !techNoiseRegex.MatchString(trimmed) {
					valid = append(valid, trimmed)
				}
			}
			if len(valid) >= 2 {
				return strings.Join(valid, " - ")
			}
		}
	}
	return ""
}

func isPureSlogan(s string) bool {
	lower := strings.ToLower(strings.TrimSpace(s))
	if sloganRegex.MatchString(lower) && !strings.Contains(s, " - ") && !strings.Contains(s, " : ") {
		return true
	}
	slogans := []string{
		"live on air", "on air", "best 80s hits", "best 90s hits", "greatest hits",
		"continuous music", "non stop music", "commercial free", "hot hits",
		"top 40", "music non stop", "all hits", "radio in the mix",
		"we'll be right back", "stay tuned", "back soon",
	}
	for _, slogan := range slogans {
		if lower == slogan {
			return true
		}
	}
	return false
}

func isStationSelfReference(title string, stationName string) bool {
	if stationName == "" {
		return false
	}
	cleanT := strings.ToLower(stripPunctuation(title))
	cleanS := strings.ToLower(stripPunctuation(stationName))

	if cleanT == cleanS {
		return true
	}

	// Title contains station name with generic suffix like "live", "stream", "online", "80.0 fm"
	if strings.HasPrefix(cleanT, cleanS) {
		remainder := strings.TrimSpace(strings.TrimPrefix(cleanT, cleanS))
		if remainder == "" || remainder == "live" || remainder == "stream" || remainder == "online" || remainder == "radio" || remainder == "fm" {
			return true
		}
	}
	return false
}

func isPureNoiseOrURL(s string) bool {
	trimmed := strings.TrimSpace(s)
	if urlRegex.MatchString(trimmed) && !strings.Contains(trimmed, " - ") {
		return true
	}
	if phoneRegex.MatchString(trimmed) || emailRegex.MatchString(trimmed) {
		return true
	}
	if techNoiseRegex.MatchString(trimmed) && !strings.Contains(trimmed, " - ") {
		return true
	}
	return false
}

func cleanCompositeNoise(s string) string {
	// Strip parentheticals containing URLs or "visit" or "stream url" or "buy ads"
	parenRegex := regexp.MustCompile(`(?i)\s*[\(\[][^\)\]]*(https?://|www\.|\.com|\.fm|buy ads|visit|listen live|stream url)[^\)\]]*[\)\]]`)
	s = parenRegex.ReplaceAllString(s, "")

	// Strip trailing pipe / delimiter promotional phrases (requiring spaces around // or -- to avoid breaking http://)
	pipePromoRegex := regexp.MustCompile(`(?i)\s*(\|\s*|\s+\/\/\s*|\s+--\s*)(https?://|www\.|buy ads|listen live|visit).*$`)
	s = pipePromoRegex.ReplaceAllString(s, "")

	// Strip inline URLs
	s = urlRegex.ReplaceAllString(s, "")

	return strings.TrimSpace(s)
}

func stripPunctuation(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// MusicBrainzRecordingMatch holds the result of a MusicBrainz search validation.
type MusicBrainzRecordingMatch struct {
	ID         string  `json:"id"`
	Score      int     `json:"score"`
	Title      string  `json:"title"`
	Artist     string  `json:"artist"`
	Album      string  `json:"album,omitempty"`
	Year       int     `json:"year,omitempty"`
	Confidence float64 `json:"confidence"`
}

// ValidateCandidateWithMusicBrainz verifies a candidate song title against the public MusicBrainz API.
func ValidateCandidateWithMusicBrainz(ctx context.Context, artist, title string) (*MusicBrainzRecordingMatch, error) {
	artist = strings.TrimSpace(artist)
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, fmt.Errorf("empty title")
	}

	var query string
	if artist != "" {
		query = fmt.Sprintf("recording:%q AND artist:%q", title, artist)
	} else {
		query = fmt.Sprintf("recording:%q", title)
	}

	endpoint := fmt.Sprintf("https://musicbrainz.org/ws/2/recording/?query=%s&fmt=json", url.QueryEscape(query))
	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "halpradio/1.0 (https://github.com/halpworld/halpradio)")

	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MusicBrainz HTTP %d: %s", resp.StatusCode, resp.Status)
	}

	var payload struct {
		Recordings []struct {
			ID           string `json:"id"`
			Score        int    `json:"score"`
			Title        string `json:"title"`
			ArtistCredit []struct {
				Name string `json:"name"`
			} `json:"artist-credit"`
			Releases []struct {
				Title string `json:"title"`
				Date  string `json:"date"`
			} `json:"releases"`
		} `json:"recordings"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	if len(payload.Recordings) == 0 {
		return nil, fmt.Errorf("no MusicBrainz match found")
	}

	top := payload.Recordings[0]
	matchedArtist := ""
	if len(top.ArtistCredit) > 0 {
		matchedArtist = top.ArtistCredit[0].Name
	}
	album := ""
	year := 0
	if len(top.Releases) > 0 {
		album = top.Releases[0].Title
		if len(top.Releases[0].Date) >= 4 {
			fmt.Sscanf(top.Releases[0].Date[:4], "%d", &year)
		}
	}

	return &MusicBrainzRecordingMatch{
		ID:         top.ID,
		Score:      top.Score,
		Title:      top.Title,
		Artist:     matchedArtist,
		Album:      album,
		Year:       year,
		Confidence: float64(top.Score) / 100.0,
	}, nil
}
