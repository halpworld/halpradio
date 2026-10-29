package ai

import (
	"container/list"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/halpworld/halpradio/pkg/lyrics"
	"github.com/halpworld/halpradio/pkg/util"
)

const (
	defaultMusicBrainzURL = "https://musicbrainz.org"
	defaultWikipediaURL   = "https://en.wikipedia.org"

	linerMemCapacity = 128
	linerMemTTL      = 6 * time.Hour
	linerDiskTTL     = 30 * 24 * time.Hour
)

// DiscographyItem represents a notable album, EP, or single in an artist's catalog.
type DiscographyItem struct {
	Title string `json:"title"`
	Year  string `json:"year,omitempty"`
	Type  string `json:"type,omitempty"`
}

// LinerNotes holds rich artist biography, recording context, discography, and
// trivia for the Interactive Liner Notes & Artist Biography Drawer (`i`).
type LinerNotes struct {
	Artist      string            `json:"artist"`
	Title       string            `json:"title"`
	Album       string            `json:"album,omitempty"`
	ReleaseYear string            `json:"release_year,omitempty"`
	Country     string            `json:"country,omitempty"`
	OriginCity  string            `json:"origin_city,omitempty"`
	Genre       string            `json:"genre,omitempty"`
	Label       string            `json:"label,omitempty"`
	MBID        string            `json:"mbid,omitempty"`
	Biography   string            `json:"biography"`
	AlbumNotes  string            `json:"album_notes"`
	Discography []DiscographyItem `json:"discography,omitempty"`
	TrackTrivia []string          `json:"track_trivia,omitempty"`
	Source      string            `json:"source"`
	FetchedAt   time.Time         `json:"fetched_at"`
}

// Clone returns a deep copy of LinerNotes.
func (ln *LinerNotes) Clone() *LinerNotes {
	if ln == nil {
		return nil
	}
	out := *ln
	if len(ln.Discography) > 0 {
		out.Discography = make([]DiscographyItem, len(ln.Discography))
		copy(out.Discography, ln.Discography)
	}
	if len(ln.TrackTrivia) > 0 {
		out.TrackTrivia = make([]string, len(ln.TrackTrivia))
		copy(out.TrackTrivia, ln.TrackTrivia)
	}
	return &out
}

// ToTrackContext converts LinerNotes and station metadata into a TrackContext for the AI DJ.
func (ln *LinerNotes) ToTrackContext(stationName, stationGenre, stationCountry string) TrackContext {
	if ln == nil {
		return TrackContext{
			StationName: stationName,
			Genre:       stationGenre,
			Country:     stationCountry,
		}
	}
	genre := ln.Genre
	if genre == "" {
		genre = stationGenre
	}
	country := ln.Country
	if country == "" {
		country = stationCountry
	}
	trivia := ""
	if len(ln.TrackTrivia) > 0 {
		trivia = ln.TrackTrivia[0]
	}
	return TrackContext{
		StationName: stationName,
		Genre:       genre,
		Country:     country,
		Artist:      ln.Artist,
		Title:       ln.Title,
		Album:       ln.Album,
		ReleaseYear: ln.ReleaseYear,
		OriginCity:  ln.OriginCity,
		Trivia:      trivia,
	}
}

// LinerNotesQuery specifies the current track and station context for a liner notes lookup.
type LinerNotesQuery struct {
	RawTrack    string
	Artist      string
	Title       string
	Album       string
	StationName string
	Genre       string
	Country     string
	Bitrate     int
	Codec       string
}

// LinerNotesOption configures a LinerNotesClient.
type LinerNotesOption func(*LinerNotesClient)

// WithLinerNotesCacheDir sets a custom on-disk cache directory.
func WithLinerNotesCacheDir(dir string) LinerNotesOption {
	return func(c *LinerNotesClient) {
		c.cacheDir = dir
	}
}

// WithLinerNotesHTTPClient sets a custom HTTP client.
func WithLinerNotesHTTPClient(hc *http.Client) LinerNotesOption {
	return func(c *LinerNotesClient) {
		if hc != nil {
			c.httpClient = hc
		}
	}
}

// WithMusicBrainzBaseURL overrides the MusicBrainz base URL (for testing).
func WithMusicBrainzBaseURL(u string) LinerNotesOption {
	return func(c *LinerNotesClient) {
		c.mbBaseURL = strings.TrimRight(u, "/")
	}
}

// WithWikipediaBaseURL overrides the Wikipedia REST base URL (for testing).
func WithWikipediaBaseURL(u string) LinerNotesOption {
	return func(c *LinerNotesClient) {
		c.wikiBaseURL = strings.TrimRight(u, "/")
	}
}

type linerMemEntry struct {
	key      string
	notes    *LinerNotes
	storedAt time.Time
}

// LinerNotesClient fetches and caches artist biographies, discographies, and recording trivia.
type LinerNotesClient struct {
	httpClient  *http.Client
	cacheDir    string
	mbBaseURL   string
	wikiBaseURL string

	memMu sync.Mutex
	items map[string]*list.Element
	order *list.List

	diskMu sync.Mutex
}

// NewLinerNotesClient creates a new LinerNotesClient with in-memory LRU and on-disk JSON caching.
func NewLinerNotesClient(opts ...LinerNotesOption) *LinerNotesClient {
	c := &LinerNotesClient{
		httpClient:  &http.Client{Timeout: 6 * time.Second},
		cacheDir:    util.GetLinerNotesCacheDir(),
		mbBaseURL:   defaultMusicBrainzURL,
		wikiBaseURL: defaultWikipediaURL,
		items:       make(map[string]*list.Element),
		order:       list.New(),
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Fetch resolves LinerNotes for the given query, checking memory cache, disk cache,
// MusicBrainz + Wikipedia APIs, and finally a 100% offline station-metadata synthesizer.
func (c *LinerNotesClient) Fetch(ctx context.Context, q LinerNotesQuery) (*LinerNotes, error) {
	artist := strings.TrimSpace(q.Artist)
	title := strings.TrimSpace(q.Title)
	if artist == "" && title == "" && strings.TrimSpace(q.RawTrack) != "" {
		a, t := lyrics.SplitTrackTitle(q.RawTrack)
		artist = a
		title = t
		if artist == "" && title == "" {
			title = strings.TrimSpace(q.RawTrack)
		}
	}
	q.Artist = artist
	q.Title = title

	key := linerCacheKey(artist, title, q.StationName)

	// 1. Check in-memory LRU cache
	if cached := c.getMem(key); cached != nil {
		return cached, nil
	}

	// 2. Check on-disk JSON cache
	if diskCached := c.readDisk(key); diskCached != nil {
		c.putMem(key, diskCached)
		return diskCached.Clone(), nil
	}

	// 3. Query MusicBrainz & Wikipedia when an artist name is available
	if artist != "" {
		mbNotes, mbOK := c.queryMusicBrainz(ctx, q)
		wikiBio, wikiOK := c.queryWikipediaBio(ctx, artist)

		if mbOK || wikiOK {
			notes := mergeOnlineNotes(q, mbNotes, mbOK, wikiBio, wikiOK)
			c.putMem(key, notes)
			c.writeDisk(key, notes)
			return notes.Clone(), nil
		}
	}

	// 4. 100% Offline fallback synthesized from station & stream metadata
	offline := synthesizeOfflineLinerNotes(q)
	c.putMem(key, offline)
	return offline.Clone(), nil
}

func linerCacheKey(artist, title, station string) string {
	a := strings.ToLower(strings.TrimSpace(artist))
	t := strings.ToLower(strings.TrimSpace(title))
	if a != "" || t != "" {
		return a + "|" + t
	}
	return "station|" + strings.ToLower(strings.TrimSpace(station))
}

func (c *LinerNotesClient) getMem(key string) *LinerNotes {
	if key == "" {
		return nil
	}
	c.memMu.Lock()
	defer c.memMu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return nil
	}
	entry := elem.Value.(*linerMemEntry)
	if time.Since(entry.storedAt) > linerMemTTL {
		c.order.Remove(elem)
		delete(c.items, key)
		return nil
	}
	c.order.MoveToFront(elem)
	return entry.notes.Clone()
}

func (c *LinerNotesClient) putMem(key string, notes *LinerNotes) {
	if key == "" || notes == nil {
		return
	}
	c.memMu.Lock()
	defer c.memMu.Unlock()

	if elem, ok := c.items[key]; ok {
		entry := elem.Value.(*linerMemEntry)
		entry.notes = notes.Clone()
		entry.storedAt = time.Now()
		c.order.MoveToFront(elem)
		return
	}
	for c.order.Len() >= linerMemCapacity {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*linerMemEntry).key)
	}
	c.items[key] = c.order.PushFront(&linerMemEntry{
		key:      key,
		notes:    notes.Clone(),
		storedAt: time.Now(),
	})
}

func (c *LinerNotesClient) diskPath(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.cacheDir, hex.EncodeToString(sum[:])+".json")
}

func (c *LinerNotesClient) readDisk(key string) *LinerNotes {
	if c.cacheDir == "" || key == "" {
		return nil
	}
	c.diskMu.Lock()
	defer c.diskMu.Unlock()

	raw, err := os.ReadFile(c.diskPath(key))
	if err != nil {
		return nil
	}
	var notes LinerNotes
	if err := json.Unmarshal(raw, &notes); err != nil {
		return nil
	}
	if notes.FetchedAt.IsZero() || time.Since(notes.FetchedAt) > linerDiskTTL {
		return nil
	}
	if notes.Biography == "" && len(notes.TrackTrivia) == 0 {
		return nil
	}
	return &notes
}

func (c *LinerNotesClient) writeDisk(key string, notes *LinerNotes) {
	if c.cacheDir == "" || key == "" || notes == nil {
		return
	}
	c.diskMu.Lock()
	defer c.diskMu.Unlock()

	if err := os.MkdirAll(c.cacheDir, 0o700); err != nil {
		return
	}
	payload, err := json.Marshal(notes)
	if err != nil {
		return
	}
	path := c.diskPath(key)
	tmp, err := os.CreateTemp(c.cacheDir, "liner-*.tmp")
	if err != nil {
		_ = os.WriteFile(path, payload, 0o600)
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(payload); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
	}
}

// MusicBrainz JSON structures
type mbRecordingSearchResp struct {
	Recordings []struct {
		ID               string `json:"id"`
		Title            string `json:"title"`
		FirstReleaseDate string `json:"first-release-date"`
		Disambiguation   string `json:"disambiguation"`
		ArtistCredit     []struct {
			Name   string `json:"name"`
			Artist struct {
				ID             string `json:"id"`
				Name           string `json:"name"`
				Disambiguation string `json:"disambiguation"`
			} `json:"artist"`
		} `json:"artist-credit"`
		Releases []struct {
			Title        string `json:"title"`
			Date         string `json:"date"`
			Country      string `json:"country"`
			ReleaseGroup struct {
				PrimaryType string `json:"primary-type"`
			} `json:"release-group"`
		} `json:"releases"`
		Tags []struct {
			Name  string `json:"name"`
			Count int    `json:"count"`
		} `json:"tags"`
	} `json:"recordings"`
}

type mbArtistLookupResp struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Type           string `json:"type"`
	Country        string `json:"country"`
	Disambiguation string `json:"disambiguation"`
	Area           struct {
		Name string `json:"name"`
	} `json:"area"`
	BeginArea struct {
		Name string `json:"name"`
	} `json:"begin-area"`
	LifeSpan struct {
		Begin string `json:"begin"`
		End   string `json:"end"`
		Ended bool   `json:"ended"`
	} `json:"life-span"`
	ReleaseGroups []struct {
		Title            string `json:"title"`
		PrimaryType      string `json:"primary-type"`
		FirstReleaseDate string `json:"first-release-date"`
	} `json:"release-groups"`
	Tags []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	} `json:"tags"`
}

func (c *LinerNotesClient) queryMusicBrainz(ctx context.Context, q LinerNotesQuery) (*LinerNotes, bool) {
	if c.mbBaseURL == "" || q.Artist == "" {
		return nil, false
	}
	reqCtx, cancel := context.WithTimeout(ctx, 4500*time.Millisecond)
	defer cancel()

	var queryStr string
	if q.Title != "" {
		queryStr = fmt.Sprintf(`recording:"%s" AND artist:"%s"`, escapeMBQuery(q.Title), escapeMBQuery(q.Artist))
	} else {
		queryStr = fmt.Sprintf(`artist:"%s"`, escapeMBQuery(q.Artist))
	}

	recURL := fmt.Sprintf("%s/ws/2/recording?query=%s&fmt=json&limit=3", c.mbBaseURL, url.QueryEscape(queryStr))
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, recURL, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("User-Agent", defaultUserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, false
	}

	var recData mbRecordingSearchResp
	if err := json.NewDecoder(resp.Body).Decode(&recData); err != nil || len(recData.Recordings) == 0 {
		return nil, false
	}

	best := recData.Recordings[0]
	notes := &LinerNotes{
		Artist:      q.Artist,
		Title:       fallbackStr(q.Title, best.Title),
		Album:       q.Album,
		ReleaseYear: extractYear(best.FirstReleaseDate),
		Country:     q.Country,
		Genre:       q.Genre,
	}

	if len(best.Releases) > 0 {
		if notes.Album == "" {
			notes.Album = best.Releases[0].Title
		}
		if notes.ReleaseYear == "" {
			notes.ReleaseYear = extractYear(best.Releases[0].Date)
		}
		if notes.Country == "" && best.Releases[0].Country != "" {
			notes.Country = best.Releases[0].Country
		}
	}

	var artistMBID string
	var artistDisambig string
	if len(best.ArtistCredit) > 0 {
		ac := best.ArtistCredit[0]
		if ac.Artist.Name != "" {
			notes.Artist = ac.Artist.Name
		}
		artistMBID = ac.Artist.ID
		artistDisambig = ac.Artist.Disambiguation
		notes.MBID = artistMBID
	}

	var tagNames []string
	for _, t := range best.Tags {
		if t.Name != "" && len(tagNames) < 4 {
			tagNames = append(tagNames, t.Name)
		}
	}

	// Enrich with artist lookup if MBID is present
	if artistMBID != "" {
		artistURL := fmt.Sprintf("%s/ws/2/artist/%s?inc=release-groups+tags&fmt=json", c.mbBaseURL, url.PathEscape(artistMBID))
		if aReq, err := http.NewRequestWithContext(reqCtx, http.MethodGet, artistURL, nil); err == nil {
			aReq.Header.Set("User-Agent", defaultUserAgent)
			aReq.Header.Set("Accept", "application/json")
			if aResp, err := c.httpClient.Do(aReq); err == nil {
				var aData mbArtistLookupResp
				if aResp.StatusCode == http.StatusOK && json.NewDecoder(aResp.Body).Decode(&aData) == nil {
					if aData.BeginArea.Name != "" {
						notes.OriginCity = aData.BeginArea.Name
					} else if aData.Area.Name != "" {
						notes.OriginCity = aData.Area.Name
					}
					if aData.Country != "" {
						notes.Country = aData.Country
					}
					if aData.Disambiguation != "" {
						artistDisambig = aData.Disambiguation
					}
					for _, t := range aData.Tags {
						if t.Name != "" && len(tagNames) < 5 && !containsIgnoreCase(tagNames, t.Name) {
							tagNames = append(tagNames, t.Name)
						}
					}
					notes.Discography = extractDiscography(aData.ReleaseGroups)
					if aData.LifeSpan.Begin != "" {
						originStr := notes.OriginCity
						if originStr == "" {
							originStr = notes.Country
						}
						if originStr != "" {
							notes.TrackTrivia = append(notes.TrackTrivia,
								fmt.Sprintf("%s originated in %s (active since %s).", notes.Artist, originStr, extractYear(aData.LifeSpan.Begin)))
						} else {
							notes.TrackTrivia = append(notes.TrackTrivia,
								fmt.Sprintf("%s has been active since %s.", notes.Artist, extractYear(aData.LifeSpan.Begin)))
						}
					}
				}
				_ = aResp.Body.Close()
			}
		}
	}

	if len(tagNames) > 0 && notes.Genre == "" {
		notes.Genre = strings.Join(tagNames[:minInt(3, len(tagNames))], ", ")
	}
	if len(tagNames) > 0 {
		notes.TrackTrivia = append(notes.TrackTrivia,
			fmt.Sprintf("Sonic profile & community tags: %s.", strings.Join(tagNames, ", ")))
	}
	if notes.Album != "" && notes.ReleaseYear != "" {
		notes.AlbumNotes = fmt.Sprintf("'%s' appears on the %s release \"%s\".", notes.Title, notes.ReleaseYear, notes.Album)
	} else if notes.Album != "" {
		notes.AlbumNotes = fmt.Sprintf("'%s' is featured on the release \"%s\".", notes.Title, notes.Album)
	}
	if artistDisambig != "" {
		notes.Biography = fmt.Sprintf("%s — %s.", notes.Artist, artistDisambig)
	}

	return notes, true
}

func extractDiscography(groups []struct {
	Title            string `json:"title"`
	PrimaryType      string `json:"primary-type"`
	FirstReleaseDate string `json:"first-release-date"`
}) []DiscographyItem {
	type SortableItem struct {
		DiscographyItem
	}
	seen := make(map[string]bool)
	var items []DiscographyItem
	for _, rg := range groups {
		title := strings.TrimSpace(rg.Title)
		if title == "" {
			continue
		}
		key := strings.ToLower(title)
		if seen[key] {
			continue
		}
		seen[key] = true
		pType := rg.PrimaryType
		if pType == "" {
			pType = "Album"
		}
		items = append(items, DiscographyItem{
			Title: title,
			Year:  extractYear(rg.FirstReleaseDate),
			Type:  pType,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Year != items[j].Year {
			if items[i].Year == "" {
				return false
			}
			if items[j].Year == "" {
				return true
			}
			return items[i].Year < items[j].Year
		}
		return items[i].Title < items[j].Title
	})
	if len(items) > 6 {
		items = items[:6]
	}
	return items
}

type wikipediaSummaryResp struct {
	Type        string `json:"type"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Extract     string `json:"extract"`
}

func (c *LinerNotesClient) queryWikipediaBio(ctx context.Context, artist string) (string, bool) {
	if c.wikiBaseURL == "" || strings.TrimSpace(artist) == "" {
		return "", false
	}
	reqCtx, cancel := context.WithTimeout(ctx, 3500*time.Millisecond)
	defer cancel()

	candidates := []string{
		strings.ReplaceAll(strings.TrimSpace(artist), " ", "_"),
		strings.ReplaceAll(strings.TrimSpace(artist)+" (musician)", " ", "_"),
		strings.ReplaceAll(strings.TrimSpace(artist)+" (band)", " ", "_"),
	}

	for _, slug := range candidates {
		u := fmt.Sprintf("%s/api/rest_v1/page/summary/%s", c.wikiBaseURL, url.PathEscape(slug))
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, u, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", defaultUserAgent)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			continue
		}
		var summary wikipediaSummaryResp
		err = json.NewDecoder(resp.Body).Decode(&summary)
		_ = resp.Body.Close()
		if err != nil {
			continue
		}
		if summary.Type == "disambiguation" || strings.TrimSpace(summary.Extract) == "" {
			continue
		}
		return trimBiographySentences(summary.Extract, 4), true
	}
	return "", false
}

func trimBiographySentences(text string, maxSentences int) string {
	text = strings.TrimSpace(text)
	if text == "" || maxSentences <= 0 {
		return text
	}
	parts := strings.SplitAfter(text, ". ")
	if len(parts) <= maxSentences {
		return text
	}
	return strings.TrimSpace(strings.Join(parts[:maxSentences], ""))
}

func mergeOnlineNotes(q LinerNotesQuery, mb *LinerNotes, mbOK bool, wikiBio string, wikiOK bool) *LinerNotes {
	out := &LinerNotes{
		Artist:    fallbackStr(q.Artist, "Featured Artist"),
		Title:     fallbackStr(q.Title, "Live Broadcast"),
		Album:     q.Album,
		Country:   q.Country,
		Genre:     q.Genre,
		FetchedAt: time.Now().UTC(),
	}

	if mbOK && mb != nil {
		out.Artist = fallbackStr(mb.Artist, out.Artist)
		out.Title = fallbackStr(mb.Title, out.Title)
		out.Album = fallbackStr(mb.Album, out.Album)
		out.ReleaseYear = mb.ReleaseYear
		out.Country = fallbackStr(mb.Country, out.Country)
		out.OriginCity = mb.OriginCity
		out.Genre = fallbackStr(mb.Genre, out.Genre)
		out.MBID = mb.MBID
		out.Biography = mb.Biography
		out.AlbumNotes = mb.AlbumNotes
		out.Discography = mb.Discography
		out.TrackTrivia = mb.TrackTrivia
	}

	if wikiOK && wikiBio != "" {
		out.Biography = wikiBio
	}
	if out.Biography == "" {
		origin := out.OriginCity
		if origin == "" {
			origin = out.Country
		}
		if origin != "" {
			out.Biography = fmt.Sprintf("%s is a %s recording artist out of %s, featured in rotation on %s.",
				out.Artist, fallbackStr(out.Genre, "contemporary"), origin, fallbackStr(q.StationName, "halpradio"))
		} else {
			out.Biography = fmt.Sprintf("%s is a %s recording artist featured in rotation on %s.",
				out.Artist, fallbackStr(out.Genre, "contemporary"), fallbackStr(q.StationName, "halpradio"))
		}
	}
	if out.AlbumNotes == "" {
		if out.Album != "" && out.ReleaseYear != "" {
			out.AlbumNotes = fmt.Sprintf("'%s' was released on the %s record \"%s\".", out.Title, out.ReleaseYear, out.Album)
		} else if out.Album != "" {
			out.AlbumNotes = fmt.Sprintf("'%s' appears on the release \"%s\".", out.Title, out.Album)
		} else {
			out.AlbumNotes = fmt.Sprintf("Currently spinning '%s' by %s on %s.", out.Title, out.Artist, fallbackStr(q.StationName, "halpradio"))
		}
	}
	if q.StationName != "" {
		out.TrackTrivia = append(out.TrackTrivia,
			fmt.Sprintf("Curated for %s (%s).", q.StationName, fallbackStr(out.Genre, "Live Stream")))
	}

	switch {
	case mbOK && wikiOK:
		out.Source = "MusicBrainz + Wikipedia"
	case mbOK:
		out.Source = "MusicBrainz"
	case wikiOK:
		out.Source = "Wikipedia"
	default:
		out.Source = "Offline Station Metadata"
	}
	return out
}

func synthesizeOfflineLinerNotes(q LinerNotesQuery) *LinerNotes {
	station := fallbackStr(q.StationName, "halpradio Live Stream")
	genre := fallbackStr(q.Genre, "Eclectic / Independent")
	country := fallbackStr(q.Country, "International")
	artist := fallbackStr(q.Artist, station)
	title := fallbackStr(q.Title, "Live Broadcast Stream")

	var bio string
	if q.Artist != "" {
		bio = fmt.Sprintf("%s is a featured %s artist currently broadcasting on %s (%s). Open the liner notes while online to automatically enrich with MusicBrainz discography and Wikipedia history.",
			q.Artist, strings.ToLower(genre), station, country)
	} else {
		bio = fmt.Sprintf("%s is broadcasting %s programming from %s.", station, genre, country)
	}

	var albumNotes string
	if q.Album != "" {
		albumNotes = fmt.Sprintf("Track '%s' from the release \"%s\", selected for the %s rotation.", title, q.Album, station)
	} else {
		albumNotes = fmt.Sprintf("Live rotation cut '%s' curated by %s.", title, station)
	}

	var trivia []string
	trivia = append(trivia, fmt.Sprintf("Station context: %s • Genre: %s • Region: %s.", station, genre, country))
	if q.Bitrate > 0 || q.Codec != "" {
		codec := fallbackStr(q.Codec, "Audio Stream")
		if q.Bitrate > 0 {
			trivia = append(trivia, fmt.Sprintf("Broadcast stream profile: %d kbps %s.", q.Bitrate, codec))
		} else {
			trivia = append(trivia, fmt.Sprintf("Broadcast stream profile: %s.", codec))
		}
	}
	trivia = append(trivia, "100% local offline liner sheet synthesized from live ICY & station catalog metadata.")

	return &LinerNotes{
		Artist:      artist,
		Title:       title,
		Album:       q.Album,
		Country:     country,
		Genre:       genre,
		Biography:   bio,
		AlbumNotes:  albumNotes,
		TrackTrivia: trivia,
		Source:      "Offline Station Metadata",
		FetchedAt:   time.Now().UTC(),
	}
}

func escapeMBQuery(s string) string {
	s = strings.ReplaceAll(s, `\`, ` `)
	s = strings.ReplaceAll(s, `"`, ` `)
	return strings.TrimSpace(s)
}

func extractYear(dateStr string) string {
	dateStr = strings.TrimSpace(dateStr)
	if len(dateStr) >= 4 {
		y := dateStr[:4]
		for _, r := range y {
			if r < '0' || r > '9' {
				return ""
			}
		}
		return y
	}
	return ""
}

func containsIgnoreCase(list []string, target string) bool {
	t := strings.ToLower(strings.TrimSpace(target))
	for _, item := range list {
		if strings.ToLower(strings.TrimSpace(item)) == t {
			return true
		}
	}
	return false
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
