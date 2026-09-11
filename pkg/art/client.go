package art

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Default provider endpoints. Each is overridable on Client so tests can point
// at an httptest server.
const (
	DefaultMusicBrainzBaseURL = "https://musicbrainz.org"
	DefaultCoverArtBaseURL    = "https://coverartarchive.org"
	DefaultITunesBaseURL      = "https://itunes.apple.com"
	DefaultDeezerBaseURL      = "https://api.deezer.com"
	DefaultLastFMBaseURL      = "https://ws.audioscrobbler.com"
)

const (
	// userAgent identifies halpradio to the providers. MusicBrainz rejects
	// requests without a descriptive User-Agent.
	userAgent = "halpradio/0.5 (https://github.com/halpworld/halpradio)"
	// maxImageBytes caps a downloaded cover so a hostile or misconfigured
	// host cannot exhaust memory.
	maxImageBytes = 8 << 20
	// maxJSONBytes caps a provider metadata response.
	maxJSONBytes = 2 << 20
)

// ErrNotFound is returned when no provider has artwork for the track.
var ErrNotFound = errors.New("art: no cover art found")

// Cover is fetched album artwork.
type Cover struct {
	Data   []byte // raw encoded image bytes (PNG or JPEG)
	Source string // "Cover Art Archive", "iTunes", "Deezer", "Last.fm"
	Artist string
	Title  string
	Album  string
	URL    string // origin URL of the image
}

// clone returns a shallow copy so cached entries cannot be mutated by callers.
// The Data slice is shared and must be treated as read-only.
func (c *Cover) clone() *Cover {
	if c == nil {
		return nil
	}
	dup := *c
	return &dup
}

// Client fetches cover art from multiple providers with RAM and disk caches.
//
// The zero value is not usable; construct one with NewClient. Base URL fields
// may be overridden afterwards (for example to point at a test server); an
// empty field falls back to the matching Default…BaseURL constant.
type Client struct {
	MusicBrainzBaseURL string // default "https://musicbrainz.org"
	CoverArtBaseURL    string // default "https://coverartarchive.org"
	ITunesBaseURL      string // default "https://itunes.apple.com"
	DeezerBaseURL      string // default "https://api.deezer.com"
	LastFMBaseURL      string // default "https://ws.audioscrobbler.com"
	HTTPClient         *http.Client

	lastFMKey string
	cacheDir  string

	mu  sync.Mutex
	mem map[string]*memEntry
}

// NewClient returns a Client caching artwork under cacheDir (created lazily).
// An empty cacheDir disables disk caching. lastFMKey may be empty, which skips
// the Last.fm provider.
func NewClient(cacheDir, lastFMKey string) *Client {
	return &Client{
		MusicBrainzBaseURL: DefaultMusicBrainzBaseURL,
		CoverArtBaseURL:    DefaultCoverArtBaseURL,
		ITunesBaseURL:      DefaultITunesBaseURL,
		DeezerBaseURL:      DefaultDeezerBaseURL,
		LastFMBaseURL:      DefaultLastFMBaseURL,
		HTTPClient:         &http.Client{Timeout: 10 * time.Second},
		lastFMKey:          strings.TrimSpace(lastFMKey),
		cacheDir:           strings.TrimSpace(cacheDir),
		mem:                make(map[string]*memEntry, memCapacity),
	}
}

// CacheDir reports the directory used for the disk cache, or "" when disk
// caching is disabled.
func (c *Client) CacheDir() string {
	if c == nil {
		return ""
	}
	return c.cacheDir
}

// Fetch resolves cover art for a track, trying each provider in turn and
// returning ErrNotFound when none has artwork.
//
// Results (including negative lookups) are cached in RAM and, when a cache
// directory is configured, on disk. The returned Cover's Data must be treated
// as read-only because it may be shared with the cache.
func (c *Client) Fetch(ctx context.Context, artist, title, album string) (*Cover, error) {
	if c == nil {
		return nil, ErrNotFound
	}
	if ctx == nil {
		ctx = context.Background()
	}

	cleanArtist := normalizeText(artist)
	cleanTitle := normalizeText(title)
	cleanAlbum := normalizeText(album)
	if cleanArtist == "" && cleanTitle == "" {
		return nil, ErrNotFound
	}

	key := cacheKey(artist, title, album)
	if cover, ok := c.memGet(key); ok {
		if cover == nil {
			return nil, ErrNotFound
		}
		return cover, nil
	}
	if cover, ok := c.diskGet(key); ok {
		c.memPut(key, cover)
		return cover.clone(), nil
	}

	providers := []struct {
		name string
		fn   func(context.Context, string, string) (*Cover, error)
	}{
		{"iTunes", c.fetchITunes},
		{"Deezer", c.fetchDeezer},
		{"Cover Art Archive", c.fetchCoverArtArchive},
		{"Last.fm", c.fetchLastFM},
	}

	for _, p := range providers {
		cover, err := p.fn(ctx, cleanArtist, cleanTitle)
		if err != nil {
			if ctx.Err() != nil {
				// The caller gave up; do not poison the cache with a
				// negative result for a cancelled lookup.
				return nil, ctx.Err()
			}
			continue
		}
		if cover == nil || len(cover.Data) == 0 {
			continue
		}
		cover.Artist = firstNonEmpty(cover.Artist, artist)
		cover.Title = firstNonEmpty(cover.Title, title)
		cover.Album = firstNonEmpty(album, cover.Album, cleanAlbum)
		c.memPut(key, cover)
		_ = c.diskPut(key, cover)
		return cover.clone(), nil
	}

	c.memPut(key, nil)
	return nil, ErrNotFound
}

// fetchITunes queries the iTunes Search API and upgrades the 100px artwork URL
// it returns to 600px.
func (c *Client) fetchITunes(ctx context.Context, artist, title string) (*Cover, error) {
	var payload struct {
		Results []struct {
			ArtistName     string `json:"artistName"`
			TrackName      string `json:"trackName"`
			CollectionName string `json:"collectionName"`
			ArtworkURL100  string `json:"artworkUrl100"`
		} `json:"results"`
	}

	q := url.Values{}
	q.Set("term", strings.TrimSpace(artist+" "+title))
	q.Set("media", "music")
	q.Set("entity", "song")
	q.Set("limit", "1")

	endpoint := baseOr(c.ITunesBaseURL, DefaultITunesBaseURL) + "/search?" + q.Encode()
	if err := c.getJSON(ctx, endpoint, &payload); err != nil {
		return nil, err
	}
	if len(payload.Results) == 0 || payload.Results[0].ArtworkURL100 == "" {
		return nil, ErrNotFound
	}

	hit := payload.Results[0]
	imgURL := strings.Replace(hit.ArtworkURL100, "100x100bb", "600x600bb", 1)
	data, err := c.download(ctx, imgURL)
	if err != nil {
		return nil, err
	}
	return &Cover{
		Data:   data,
		Source: "iTunes",
		Artist: hit.ArtistName,
		Title:  hit.TrackName,
		Album:  hit.CollectionName,
		URL:    imgURL,
	}, nil
}

// fetchDeezer queries the Deezer search API for the album cover of the best
// matching track.
func (c *Client) fetchDeezer(ctx context.Context, artist, title string) (*Cover, error) {
	var payload struct {
		Data []struct {
			Title  string `json:"title"`
			Artist struct {
				Name string `json:"name"`
			} `json:"artist"`
			Album struct {
				Title    string `json:"title"`
				CoverXL  string `json:"cover_xl"`
				CoverBig string `json:"cover_big"`
			} `json:"album"`
		} `json:"data"`
	}

	q := url.Values{}
	q.Set("q", strings.TrimSpace(artist+" "+title))
	q.Set("limit", "1")

	endpoint := baseOr(c.DeezerBaseURL, DefaultDeezerBaseURL) + "/search?" + q.Encode()
	if err := c.getJSON(ctx, endpoint, &payload); err != nil {
		return nil, err
	}
	if len(payload.Data) == 0 {
		return nil, ErrNotFound
	}

	hit := payload.Data[0]
	imgURL := firstNonEmpty(hit.Album.CoverXL, hit.Album.CoverBig)
	if imgURL == "" {
		return nil, ErrNotFound
	}
	data, err := c.download(ctx, imgURL)
	if err != nil {
		return nil, err
	}
	return &Cover{
		Data:   data,
		Source: "Deezer",
		Artist: hit.Artist.Name,
		Title:  hit.Title,
		Album:  hit.Album.Title,
		URL:    imgURL,
	}, nil
}

// fetchCoverArtArchive resolves a release MBID through MusicBrainz and then
// pulls the 500px front cover from the Cover Art Archive.
func (c *Client) fetchCoverArtArchive(ctx context.Context, artist, title string) (*Cover, error) {
	var payload struct {
		Recordings []struct {
			Title        string `json:"title"`
			ArtistCredit []struct {
				Name string `json:"name"`
			} `json:"artist-credit"`
			Releases []struct {
				ID    string `json:"id"`
				Title string `json:"title"`
			} `json:"releases"`
		} `json:"recordings"`
	}

	q := url.Values{}
	q.Set("query", fmt.Sprintf("artist:%q AND recording:%q", artist, title))
	q.Set("fmt", "json")
	q.Set("limit", "1")

	endpoint := baseOr(c.MusicBrainzBaseURL, DefaultMusicBrainzBaseURL) + "/ws/2/recording?" + q.Encode()
	if err := c.getJSON(ctx, endpoint, &payload); err != nil {
		return nil, err
	}
	if len(payload.Recordings) == 0 || len(payload.Recordings[0].Releases) == 0 {
		return nil, ErrNotFound
	}

	rec := payload.Recordings[0]
	mbid := rec.Releases[0].ID
	if !isMBID(mbid) {
		return nil, ErrNotFound
	}

	credited := ""
	if len(rec.ArtistCredit) > 0 {
		credited = rec.ArtistCredit[0].Name
	}

	imgURL := baseOr(c.CoverArtBaseURL, DefaultCoverArtBaseURL) + "/release/" + mbid + "/front-500"
	data, err := c.download(ctx, imgURL)
	if err != nil {
		return nil, err
	}
	return &Cover{
		Data:   data,
		Source: "Cover Art Archive",
		Artist: credited,
		Title:  rec.Title,
		Album:  rec.Releases[0].Title,
		URL:    imgURL,
	}, nil
}

// fetchLastFM queries track.getInfo and takes the largest album image. It is
// skipped entirely when no API key was configured.
func (c *Client) fetchLastFM(ctx context.Context, artist, title string) (*Cover, error) {
	if c.lastFMKey == "" {
		return nil, ErrNotFound
	}

	var payload struct {
		Track struct {
			Name  string `json:"name"`
			Album struct {
				Artist string `json:"artist"`
				Title  string `json:"title"`
				Image  []struct {
					Text string `json:"#text"`
					Size string `json:"size"`
				} `json:"image"`
			} `json:"album"`
		} `json:"track"`
	}

	q := url.Values{}
	q.Set("method", "track.getInfo")
	q.Set("api_key", c.lastFMKey)
	q.Set("artist", artist)
	q.Set("track", title)
	q.Set("format", "json")

	endpoint := baseOr(c.LastFMBaseURL, DefaultLastFMBaseURL) + "/2.0/?" + q.Encode()
	if err := c.getJSON(ctx, endpoint, &payload); err != nil {
		return nil, err
	}

	imgURL, best := "", -1
	for _, im := range payload.Track.Album.Image {
		if strings.TrimSpace(im.Text) == "" {
			continue
		}
		if rank := lastFMSizeRank(im.Size); rank > best {
			imgURL, best = im.Text, rank
		}
	}
	if imgURL == "" {
		return nil, ErrNotFound
	}

	data, err := c.download(ctx, imgURL)
	if err != nil {
		return nil, err
	}
	return &Cover{
		Data:   data,
		Source: "Last.fm",
		Artist: payload.Track.Album.Artist,
		Title:  payload.Track.Name,
		Album:  payload.Track.Album.Title,
		URL:    imgURL,
	}, nil
}

// lastFMSizeRank orders the Last.fm image size labels from smallest to
// largest. Unknown labels rank lowest.
func lastFMSizeRank(size string) int {
	switch strings.ToLower(strings.TrimSpace(size)) {
	case "small":
		return 1
	case "medium":
		return 2
	case "large":
		return 3
	case "extralarge":
		return 4
	case "mega":
		return 5
	default:
		return 0
	}
}

// getJSON performs a GET against rawURL and decodes the (size-capped) JSON
// body into out.
func (c *Client) getJSON(ctx context.Context, rawURL string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("art: build request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("art: request %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("art: provider returned status %d", resp.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxJSONBytes)).Decode(out); err != nil {
		return fmt.Errorf("art: decode response: %w", err)
	}
	return nil
}

// download retrieves an image, capping the body at 8 MB, rejecting non-image
// content types and verifying that the bytes actually decode as an image.
func (c *Client) download(ctx context.Context, rawURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("art: build image request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "image/*")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("art: download %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("art: image download returned status %d", resp.StatusCode)
	}
	if ct := contentType(resp.Header.Get("Content-Type")); ct != "" && !isImageContentType(ct) {
		return nil, fmt.Errorf("art: unexpected content type %q for cover art", ct)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
	if err != nil {
		return nil, fmt.Errorf("art: read image body: %w", err)
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("art: empty image body")
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("art: downloaded bytes are not a usable image: %w", err)
	}
	return data, nil
}

// httpClient returns the configured client or a sane default.
func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// contentType strips parameters from a Content-Type header value.
func contentType(v string) string {
	v = strings.TrimSpace(strings.ToLower(v))
	if i := strings.IndexByte(v, ';'); i >= 0 {
		v = strings.TrimSpace(v[:i])
	}
	return v
}

// isImageContentType reports whether a media type may carry image bytes.
func isImageContentType(ct string) bool {
	return strings.HasPrefix(ct, "image/") ||
		ct == "application/octet-stream" ||
		ct == "binary/octet-stream"
}

// isMBID reports whether s looks like a MusicBrainz UUID, so it can safely be
// interpolated into a Cover Art Archive path.
func isMBID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch i {
		case 8, 13, 18, 23:
			if ch != '-' {
				return false
			}
		default:
			isHex := (ch >= '0' && ch <= '9') ||
				(ch >= 'a' && ch <= 'f') ||
				(ch >= 'A' && ch <= 'F')
			if !isHex {
				return false
			}
		}
	}
	return true
}

// baseOr trims a configured base URL, falling back to def when it is empty.
func baseOr(configured, def string) string {
	v := strings.TrimRight(strings.TrimSpace(configured), "/")
	if v == "" {
		return strings.TrimRight(def, "/")
	}
	return v
}

// firstNonEmpty returns the first argument that is not blank.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
