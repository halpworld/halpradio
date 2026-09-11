package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultLRCLibBaseURL is the public LRCLIB endpoint.
	DefaultLRCLibBaseURL = "https://lrclib.net"
	// DefaultNetEaseBaseURL is the public NetEase Cloud Music endpoint.
	DefaultNetEaseBaseURL = "https://music.163.com"

	// SourceLRCLib marks sheets resolved through LRCLIB.
	SourceLRCLib = "LRCLIB"
	// SourceNetEase marks sheets resolved through NetEase Cloud Music.
	SourceNetEase = "NetEase"

	// InstrumentalMarker is the single line carried by instrumental sheets.
	InstrumentalMarker = "♪ Instrumental ♪"

	// DefaultTimeout bounds a single provider request.
	DefaultTimeout = 8 * time.Second

	lrclibUserAgent  = "halpradio/0.5 (https://github.com/halpworld/halpradio)"
	netEaseUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/122.0.0.0 Safari/537.36"
)

// ErrNotFound is returned when no provider has lyrics for the track.
var ErrNotFound = errors.New("lyrics: no lyrics found")

// errNoMatch signals that a provider answered correctly but has no lyrics.
var errNoMatch = errors.New("lyrics: provider has no match")

// errBadShape signals that a provider answered with an unexpected payload.
var errBadShape = errors.New("lyrics: unexpected provider response")

// Client fetches lyrics from LRCLIB with a NetEase fallback, memoised in RAM
// and on disk.
type Client struct {
	LRCLibBaseURL  string // default "https://lrclib.net"
	NetEaseBaseURL string // default "https://music.163.com"
	HTTPClient     *http.Client

	cacheDir string
	mem      *memCache
	memOnce  sync.Once
	diskMu   sync.Mutex
}

// NewClient returns a Client caching sheets under cacheDir (created lazily).
// An empty cacheDir disables disk caching.
func NewClient(cacheDir string) *Client {
	return &Client{
		LRCLibBaseURL:  DefaultLRCLibBaseURL,
		NetEaseBaseURL: DefaultNetEaseBaseURL,
		HTTPClient:     &http.Client{Timeout: DefaultTimeout},
		cacheDir:       strings.TrimSpace(cacheDir),
		mem:            newMemCache(memCapacity, memTTL, negativeTTL),
	}
}

// CacheDir reports the directory used for the on-disk sheet cache, or an empty
// string when disk caching is disabled.
func (c *Client) CacheDir() string {
	if c == nil {
		return ""
	}
	return c.cacheDir
}

// Fetch resolves lyrics for a track. It tries the in-memory cache, the disk
// cache, LRCLIB's /api/get exact lookup, LRCLIB's /api/search, then NetEase.
// duration may be zero when unknown. It returns a nil Sheet and ErrNotFound
// when no provider has lyrics.
//
// When every provider failed to answer at all (network or server errors) the
// returned error wraps those failures instead of ErrNotFound, and the outcome
// is not negatively cached.
func (c *Client) Fetch(ctx context.Context, artist, title, album string, duration time.Duration) (*Sheet, error) {
	if c == nil {
		return nil, ErrNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}

	queryArtist := normalizeQuery(artist)
	queryTitle := normalizeQuery(title)
	queryAlbum := normalizeQuery(album)
	if queryTitle == "" {
		return nil, ErrNotFound
	}

	key := cacheKey(artist, title, album, duration)

	if res, ok := c.memCacheRef().get(key); ok {
		if res.negative {
			return nil, ErrNotFound
		}
		return res.sheet.clone(), nil
	}

	if sheet := c.readDisk(key); sheet != nil {
		c.memCacheRef().putSheet(key, sheet)
		return sheet.clone(), nil
	}

	var failures []error

	providers := []struct {
		name string
		run  func() (*Sheet, error)
	}{
		{"lrclib.get", func() (*Sheet, error) {
			return c.lrclibGet(ctx, queryArtist, queryTitle, queryAlbum, duration)
		}},
		{"lrclib.search", func() (*Sheet, error) {
			return c.lrclibSearch(ctx, queryArtist, queryTitle, duration)
		}},
		{"netease", func() (*Sheet, error) {
			return c.netEase(ctx, queryArtist, queryTitle, duration)
		}},
	}

	for _, p := range providers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		sheet, err := p.run()
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", p.name, err))
			continue
		}
		if sheet == nil || sheet.IsEmpty() {
			continue
		}

		applyRequestMetadata(sheet, artist, title, album, duration)
		c.memCacheRef().putSheet(key, sheet)
		c.writeDisk(key, sheet)
		return sheet.clone(), nil
	}

	if len(failures) > 0 {
		// Do not remember a negative result that was caused by a transport or
		// server failure rather than by a genuine miss.
		return nil, fmt.Errorf("lyrics: no provider answered: %w", errors.Join(failures...))
	}

	c.memCacheRef().putNegative(key)
	return nil, ErrNotFound
}

// applyRequestMetadata fills in any metadata the provider did not report,
// using the caller's original (un-normalised) strings.
func applyRequestMetadata(sheet *Sheet, artist, title, album string, duration time.Duration) {
	if strings.TrimSpace(sheet.Artist) == "" {
		sheet.Artist = strings.TrimSpace(artist)
	}
	if strings.TrimSpace(sheet.Title) == "" {
		sheet.Title = strings.TrimSpace(title)
	}
	if strings.TrimSpace(sheet.Album) == "" {
		sheet.Album = strings.TrimSpace(album)
	}
	if sheet.Duration <= 0 && duration > 0 {
		sheet.Duration = duration
	}
}

// memCacheRef returns the in-memory cache, initialising it once for Clients
// that were built as a zero value rather than through NewClient.
func (c *Client) memCacheRef() *memCache {
	c.memOnce.Do(func() {
		if c.mem == nil {
			c.mem = newMemCache(memCapacity, memTTL, negativeTTL)
		}
	})
	return c.mem
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: DefaultTimeout}
}

func (c *Client) lrclibBase() string {
	if base := strings.TrimRight(strings.TrimSpace(c.LRCLibBaseURL), "/"); base != "" {
		return base
	}
	return DefaultLRCLibBaseURL
}

func (c *Client) netEaseBase() string {
	if base := strings.TrimRight(strings.TrimSpace(c.NetEaseBaseURL), "/"); base != "" {
		return base
	}
	return DefaultNetEaseBaseURL
}

// getJSON performs a GET request and decodes the JSON body into out. A 404 or
// 204 response yields errNoMatch; an undecodable body yields errBadShape.
func (c *Client) getJSON(ctx context.Context, rawURL string, headers map[string]string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound, resp.StatusCode == http.StatusNoContent:
		return errNoMatch
	case resp.StatusCode >= 400:
		return fmt.Errorf("lyrics: %s returned HTTP %d", req.URL.Host, resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("%w: %v", errBadShape, err)
	}
	return nil
}

// lrcLibTrack mirrors one track object returned by the LRCLIB API.
type lrcLibTrack struct {
	ID           int64   `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  string  `json:"plainLyrics"`
	SyncedLyrics string  `json:"syncedLyrics"`
}

// hasLyrics reports whether the track carries anything renderable.
func (t lrcLibTrack) hasLyrics() bool {
	return t.Instrumental ||
		strings.TrimSpace(t.SyncedLyrics) != "" ||
		strings.TrimSpace(t.PlainLyrics) != ""
}

func (t lrcLibTrack) isSynced() bool {
	return strings.TrimSpace(t.SyncedLyrics) != ""
}

// toSheet converts an LRCLIB track into a Sheet, or nil when it has no lyrics.
func (t lrcLibTrack) toSheet() *Sheet {
	sheet := &Sheet{
		Artist: strings.TrimSpace(t.ArtistName),
		Title:  strings.TrimSpace(t.TrackName),
		Album:  strings.TrimSpace(t.AlbumName),
		Source: SourceLRCLib,
	}
	if t.Duration > 0 {
		sheet.Duration = time.Duration(t.Duration * float64(time.Second))
	}

	if t.Instrumental {
		sheet.Instrumental = true
		sheet.Lines = []Line{{Text: InstrumentalMarker}}
		return sheet
	}
	if lines := ParseLRC(t.SyncedLyrics); len(lines) > 0 {
		sheet.Lines = lines
		sheet.Synced = true
		return sheet
	}
	if lines := plainToLines(t.PlainLyrics); len(lines) > 0 {
		sheet.Lines = lines
		return sheet
	}
	return nil
}

// lrclibGet performs LRCLIB's exact /api/get lookup.
func (c *Client) lrclibGet(ctx context.Context, artist, title, album string, duration time.Duration) (*Sheet, error) {
	if artist == "" || title == "" {
		return nil, nil
	}

	q := url.Values{}
	q.Set("artist_name", artist)
	q.Set("track_name", title)
	if album != "" {
		q.Set("album_name", album)
	}
	if duration > 0 {
		q.Set("duration", strconv.FormatInt(int64(duration.Round(time.Second)/time.Second), 10))
	}

	var track lrcLibTrack
	err := c.getJSON(ctx, c.lrclibBase()+"/api/get?"+q.Encode(),
		map[string]string{"User-Agent": lrclibUserAgent}, &track)
	switch {
	case errors.Is(err, errNoMatch), errors.Is(err, errBadShape):
		return nil, nil
	case err != nil:
		return nil, err
	}

	return track.toSheet(), nil
}

// lrclibSearch performs LRCLIB's fuzzy /api/search lookup and picks the best
// candidate: synced lyrics first, then the closest duration, then the first.
func (c *Client) lrclibSearch(ctx context.Context, artist, title string, duration time.Duration) (*Sheet, error) {
	if title == "" {
		return nil, nil
	}

	q := url.Values{}
	if artist != "" {
		q.Set("artist_name", artist)
	}
	q.Set("track_name", title)

	var candidates []lrcLibTrack
	err := c.getJSON(ctx, c.lrclibBase()+"/api/search?"+q.Encode(),
		map[string]string{"User-Agent": lrclibUserAgent}, &candidates)
	switch {
	case errors.Is(err, errNoMatch), errors.Is(err, errBadShape):
		return nil, nil
	case err != nil:
		return nil, err
	}

	best := pickLRCLibCandidate(candidates, duration)
	if best == nil {
		return nil, nil
	}
	return best.toSheet(), nil
}

// pickLRCLibCandidate returns the most suitable search result, or nil.
func pickLRCLibCandidate(candidates []lrcLibTrack, duration time.Duration) *lrcLibTrack {
	best := -1
	for i := range candidates {
		if !candidates[i].hasLyrics() {
			continue
		}
		if best < 0 || betterLRCLibCandidate(candidates[i], candidates[best], duration) {
			best = i
		}
	}
	if best < 0 {
		return nil
	}
	return &candidates[best]
}

// betterLRCLibCandidate reports whether a should beat b.
func betterLRCLibCandidate(a, b lrcLibTrack, duration time.Duration) bool {
	if a.isSynced() != b.isSynced() {
		return a.isSynced()
	}
	if duration > 0 {
		want := duration.Seconds()
		return absFloat(a.Duration-want) < absFloat(b.Duration-want)
	}
	return false
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// netEaseSong mirrors one song object from the NetEase search endpoint.
type netEaseSong struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Artists []struct {
		Name string `json:"name"`
	} `json:"artists"`
	Album struct {
		Name string `json:"name"`
	} `json:"album"`
	Duration int64 `json:"duration"` // milliseconds
}

func (s netEaseSong) artistName() string {
	names := make([]string, 0, len(s.Artists))
	for _, a := range s.Artists {
		if n := strings.TrimSpace(a.Name); n != "" {
			names = append(names, n)
		}
	}
	return strings.Join(names, " & ")
}

type netEaseSearchResponse struct {
	Result struct {
		Songs []netEaseSong `json:"songs"`
	} `json:"result"`
}

type netEaseLyricResponse struct {
	LRC struct {
		Lyric string `json:"lyric"`
	} `json:"lrc"`
}

// netEase resolves lyrics through NetEase Cloud Music. Unexpected payload
// shapes are treated as "no lyrics" rather than as errors.
func (c *Client) netEase(ctx context.Context, artist, title string, duration time.Duration) (*Sheet, error) {
	if title == "" {
		return nil, nil
	}

	headers := map[string]string{
		"User-Agent": netEaseUserAgent,
		"Referer":    DefaultNetEaseBaseURL,
	}

	terms := strings.TrimSpace(artist + " " + title)
	q := url.Values{}
	q.Set("s", terms)
	q.Set("type", "1")
	q.Set("limit", "5")

	var search netEaseSearchResponse
	err := c.getJSON(ctx, c.netEaseBase()+"/api/search/get?"+q.Encode(), headers, &search)
	switch {
	case errors.Is(err, errNoMatch), errors.Is(err, errBadShape):
		return nil, nil
	case err != nil:
		return nil, err
	}

	song := pickNetEaseSong(search.Result.Songs, artist, title, duration)
	if song == nil {
		return nil, nil
	}

	lq := url.Values{}
	lq.Set("id", strconv.FormatInt(song.ID, 10))
	lq.Set("lv", "1")
	lq.Set("kv", "1")
	lq.Set("tv", "-1")

	var lyric netEaseLyricResponse
	err = c.getJSON(ctx, c.netEaseBase()+"/api/song/lyric?"+lq.Encode(), headers, &lyric)
	switch {
	case errors.Is(err, errNoMatch), errors.Is(err, errBadShape):
		return nil, nil
	case err != nil:
		return nil, err
	}

	raw := lyric.LRC.Lyric
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	sheet := &Sheet{
		Artist: song.artistName(),
		Title:  strings.TrimSpace(song.Name),
		Album:  strings.TrimSpace(song.Album.Name),
		Source: SourceNetEase,
	}
	if song.Duration > 0 {
		sheet.Duration = time.Duration(song.Duration) * time.Millisecond
	}

	if lines := ParseLRC(raw); len(lines) > 0 && lines[len(lines)-1].At > 0 {
		sheet.Lines = lines
		sheet.Synced = true
		return sheet, nil
	}
	if lines := plainToLines(raw); len(lines) > 0 {
		sheet.Lines = lines
		return sheet, nil
	}
	return nil, nil
}

// pickNetEaseSong scores search hits by title and artist agreement, breaking
// ties with the closest duration.
func pickNetEaseSong(songs []netEaseSong, artist, title string, duration time.Duration) *netEaseSong {
	wantTitle := normalizeKey(title)
	wantArtist := normalizeKey(artist)

	best := -1
	bestScore := -1
	var bestDelta time.Duration

	for i := range songs {
		if songs[i].ID == 0 {
			continue
		}

		score := 0
		gotTitle := normalizeKey(songs[i].Name)
		if gotTitle != "" && wantTitle != "" {
			switch {
			case gotTitle == wantTitle:
				score += 2
			case strings.Contains(gotTitle, wantTitle), strings.Contains(wantTitle, gotTitle):
				score++
			}
		}
		gotArtist := normalizeKey(songs[i].artistName())
		if gotArtist != "" && wantArtist != "" &&
			(gotArtist == wantArtist ||
				strings.Contains(gotArtist, wantArtist) ||
				strings.Contains(wantArtist, gotArtist)) {
			score += 2
		}

		delta := time.Duration(0)
		if duration > 0 && songs[i].Duration > 0 {
			delta = absDuration(time.Duration(songs[i].Duration)*time.Millisecond - duration)
		}

		if best < 0 || score > bestScore || (score == bestScore && duration > 0 && delta < bestDelta) {
			best, bestScore, bestDelta = i, score, delta
		}
	}

	if best < 0 {
		return nil
	}
	return &songs[best]
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
