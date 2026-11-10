package art

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/halpworld/halpradio/pkg/radio/netguard"
	"github.com/halpworld/halpradio/pkg/util"
)

func withPublicOrigin(ctx context.Context) context.Context {
	return netguard.WithOrigin(ctx, "https://itunes.apple.com/")
}

func pngChunk(typ string, data []byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, uint32(len(data)))
	b.WriteString(typ)
	b.Write(data)
	crc := crc32.NewIEEE()
	crc.Write([]byte(typ))
	crc.Write(data)
	_ = binary.Write(&b, binary.BigEndian, crc.Sum32())
	return b.Bytes()
}

// makeBombPNG builds a small PNG that declares w x h 1-bit pixels.
func makeBombPNG(w, h int) []byte {
	var out bytes.Buffer
	out.Write([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], uint32(w))
	binary.BigEndian.PutUint32(ihdr[4:], uint32(h))
	ihdr[8] = 1
	out.Write(pngChunk("IHDR", ihdr))
	var z bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&z, zlib.BestCompression)
	row := make([]byte, 1+(w+7)/8)
	for y := 0; y < h; y++ {
		_, _ = zw.Write(row)
	}
	_ = zw.Close()
	out.Write(pngChunk("IDAT", z.Bytes()))
	out.Write(pngChunk("IEND", nil))
	return out.Bytes()
}

func smallPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{255, 0, 0, 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

const (
	artOSC52 = "\x1b]52;c;ZXZpbA==\x07"
	artC1    = "\u009b2J"
)

// iTunesServer serves one iTunes search hit whose artwork URL is artURL
// (with %s replaced by the server URL) and serves img at /img.
func iTunesServer(t *testing.T, artURL string, meta string, img []byte) *httptest.Server {
	t.Helper()
	var srvURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/search"):
			u := artURL
			if strings.Contains(u, "%s") {
				u = fmt.Sprintf(u, srvURL)
			}
			b, _ := json.Marshal(map[string]any{"resultCount": 1, "results": []map[string]any{{
				"artistName": "Artist" + meta, "trackName": "Song" + meta, "collectionName": "Album" + meta, "artworkUrl100": u,
			}}})
			_, _ = w.Write(b)
		case r.URL.Path == "/img":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(img)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	srvURL = srv.URL
	return srv
}

func onlyITunes(c *Client, srv *httptest.Server) {
	c.ITunesBaseURL = srv.URL
	c.DeezerBaseURL = srv.URL + "/none"
	c.MusicBrainzBaseURL = srv.URL + "/none"
	c.CoverArtBaseURL = srv.URL + "/none"
}

func TestPNGBombRefused(t *testing.T) {
	bomb := makeBombPNG(12000, 12000)
	srv := iTunesServer(t, "%s/img", "", bomb)
	c := NewClient(t.TempDir(), "")
	onlyITunes(c, srv)
	if cover, err := c.Fetch(context.Background(), "Artist", "Song", ""); err == nil {
		t.Fatalf("bomb accepted: %d bytes from %s", len(cover.Data), cover.Source)
	}
	// Every decode path refuses it before allocating pixels.
	if err := checkDimensions(bomb); !errors.Is(err, errImageTooLarge) {
		t.Errorf("checkDimensions = %v", err)
	}
	if _, err := decodeImage(bomb); err == nil {
		t.Errorf("decodeImage decoded the bomb")
	}
	for _, p := range []Protocol{ProtocolHalfBlock, ProtocolKitty, ProtocolBraille, ProtocolSixel} {
		if _, err := NewRenderer(p).Render(bomb, 40, 20); err == nil {
			t.Errorf("Render(%v) accepted the bomb", p)
		}
	}
	// Within the side limit but over the pixel limit.
	if err := checkDimensions(makeBombPNG(2048, 2048)); !errors.Is(err, errImageTooLarge) {
		t.Errorf("2048x2048 (4.2 MP) accepted")
	}
	// Over the side limit but well under the pixel limit.
	if err := checkDimensions(makeBombPNG(2049, 8)); !errors.Is(err, errImageTooLarge) {
		t.Errorf("2049x8 accepted")
	}
	// RR-E-6: the previous 4000x4000 (16 MP) ceiling is now refused.
	if err := checkDimensions(makeBombPNG(4000, 4000)); !errors.Is(err, errImageTooLarge) {
		t.Errorf("4000x4000 (16 MP) accepted")
	}
	if err := checkDimensions(makeBombPNG(2000, 2000)); err != nil {
		t.Errorf("2000x2000 (4 MP) refused: %v", err)
	}
	if err := checkDimensions(makeBombPNG(64, 64)); err != nil {
		t.Errorf("small image refused: %v", err)
	}
}

func TestDiskCachedBombDropped(t *testing.T) {
	dir := t.TempDir()
	c := NewClient(dir, "")
	key := cacheKey("Artist", "Song", "")
	imgPath, metaPath, _ := c.diskPaths(key)
	_ = os.MkdirAll(dir, 0o700)
	_ = os.WriteFile(imgPath, makeBombPNG(12000, 12000), 0o600)
	meta, _ := json.Marshal(diskMeta{Source: "iTunes", CachedAt: time.Now()})
	_ = os.WriteFile(metaPath, meta, 0o600)
	if _, ok := c.diskGet(key); ok {
		t.Fatal("cached bomb served")
	}
	if _, err := os.Stat(imgPath); err == nil {
		t.Error("cached bomb not deleted")
	}
}

func TestCoverMetadataSanitized(t *testing.T) {
	srv := iTunesServer(t, "%s/img", artOSC52+artC1+"‮", smallPNG(t))
	dir := t.TempDir()
	c := NewClient(dir, "")
	onlyITunes(c, srv)
	cover, err := c.Fetch(context.Background(), "Artist", "Song", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{cover.Source, cover.Artist, cover.Title, cover.Album, cover.URL} {
		if !util.IsDisplaySafe(v) {
			t.Errorf("unsafe cover field %q", v)
		}
	}

	// A poisoned metadata sidecar on disk is cleaned on read.
	key := cacheKey("x", "y", "")
	imgPath, metaPath, _ := c.diskPaths(key)
	_ = os.WriteFile(imgPath, smallPNG(t), 0o600)
	meta, _ := json.Marshal(diskMeta{Source: "S" + artOSC52, URL: "javascript:alert(1)", Artist: "A" + artC1, Title: "T", CachedAt: time.Now()})
	_ = os.WriteFile(metaPath, meta, 0o600)
	got, ok := c.diskGet(key)
	if !ok {
		t.Fatal("valid cache entry not served")
	}
	for _, v := range []string{got.Source, got.Artist, got.Title, got.URL} {
		if !util.IsDisplaySafe(v) {
			t.Errorf("unsafe cached field %q", v)
		}
	}
	if got.URL != "" {
		t.Errorf("non-http cached URL kept: %q", got.URL)
	}
}

func TestArtNonHTTPImageURLRefused(t *testing.T) {
	for _, u := range []string{"file:///etc/passwd", "ftp://e.example/x.png", "gopher://e.example/", "http://user:pw@e.example/x.png"} {
		srv := iTunesServer(t, u, "", nil)
		c := NewClient("", "")
		onlyITunes(c, srv)
		if _, err := c.Fetch(context.Background(), "Artist", "Song", ""); err == nil {
			t.Errorf("image URL %q fetched", u)
		}
	}
}

func TestArtRedirectPolicy(t *testing.T) {
	// The default client refuses a redirect that downgrades https to http.
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(smallPNG(t))
	}))
	defer plain.Close()
	var hits atomic.Int64
	tlsSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, plain.URL+"/img", http.StatusFound)
	}))
	defer tlsSrv.Close()

	c := NewClient("", "")
	tr := c.HTTPClient.Transport.(*http.Transport)
	tr.TLSClientConfig = tlsSrv.Client().Transport.(*http.Transport).TLSClientConfig
	if _, err := c.download(netguard.WithOrigin(context.Background(), tlsSrv.URL), tlsSrv.URL+"/img"); err == nil {
		t.Errorf("https to http redirect followed")
	}
	if hits.Load() == 0 {
		t.Fatal("test server not reached")
	}

	// A provider on a public origin cannot send the client to loopback.
	c2 := NewClient("", "")
	ctx := withPublicOrigin(context.Background())
	if _, err := c2.download(ctx, plain.URL+"/img"); err == nil {
		t.Errorf("loopback image fetched from a public origin")
	}
}

func TestArtThrottle(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := NewClient("", "")
	onlyITunes(c, srv)
	now := time.Unix(1000, 0)
	c.limiter.SetClock(func() time.Time { return now })
	for i := 0; i < 20; i++ {
		_, _ = c.Fetch(context.Background(), "Artist", fmt.Sprintf("Song %d", i), "")
	}
	// Four providers, each with limiterBurst lookups.
	if got := hits.Load(); got > 4*limiterBurst {
		t.Errorf("providers hit %d times for 20 rapid titles", got)
	}
	// A throttled miss is not negatively cached.
	if _, ok := c.memGet(cacheKey("Artist", "Song 19", "")); ok {
		t.Errorf("throttled lookup was negatively cached")
	}
}

func TestArtImageBodyOverLimitRefused(t *testing.T) {
	big := append(smallPNG(t), make([]byte, maxImageBytes)...)
	srv := iTunesServer(t, "%s/img", "", big)
	c := NewClient("", "")
	onlyITunes(c, srv)
	if _, err := c.Fetch(context.Background(), "Artist", "Song", ""); err == nil {
		t.Errorf("oversized image body accepted")
	}
}
