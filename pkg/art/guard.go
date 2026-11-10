package art

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"net/url"
	"time"

	"github.com/halpworld/halpradio/pkg/radio/netguard"
	"github.com/halpworld/halpradio/pkg/util"
)

// Limits on what a cover-art provider can make halpradio decode, keep and
// show.
const (
	// maxImageSide and maxImagePixels bound an image's declared size. They
	// are checked with image.DecodeConfig, which reads only the header,
	// before any full decode: a 100 KB PNG can declare 12000x12000 pixels
	// and would otherwise allocate hundreds of MB on the UI goroutine.
	// Artwork is shown in a few dozen terminal cells, so 2048 px per side
	// and 4 MP (at most ~32 MB as 16-bit RGBA) is ample.
	maxImageSide   = 2048
	maxImagePixels = 4_000_000

	// maxMetaRunes caps the provider-supplied artist, title, album and
	// source strings; maxURLLen caps the stored origin URL.
	maxMetaRunes = 200
	maxURLLen    = 2048

	// Provider throttle: limiterBurst lookups at once, then one every
	// limiterInterval per provider.
	limiterBurst    = 4
	limiterInterval = 15 * time.Second

	// diskMaxBytes bounds the on-disk artwork cache; pruneEvery paces the
	// pruning pass.
	diskMaxBytes = 200 << 20
	pruneEvery   = 16
)

// errImageTooLarge is returned for images whose declared dimensions exceed
// maxImageSide or maxImagePixels.
var errImageTooLarge = errors.New("art: image dimensions exceed limit")

// errThrottled is returned when a provider was skipped by the throttle.
var errThrottled = errors.New("art: provider throttled")

// checkDimensions reads only the image header and refuses images that are
// not a supported format or that declare more than maxImageSide pixels on a
// side or more than maxImagePixels in total.
func checkDimensions(data []byte) error {
	if len(data) == 0 {
		return fmt.Errorf("art: empty image data")
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("art: decode image header: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return fmt.Errorf("art: image has no pixels")
	}
	if cfg.Width > maxImageSide || cfg.Height > maxImageSide ||
		int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return fmt.Errorf("%w: %dx%d", errImageTooLarge, cfg.Width, cfg.Height)
	}
	return nil
}

// sanitizeCover strips terminal control sequences, C1 controls, bidi and
// zero-width characters from provider metadata and drops an origin URL that
// is not a plain http/https URL. It runs on fresh results and on every disk
// cache read.
func sanitizeCover(c *Cover) *Cover {
	if c == nil {
		return nil
	}
	c.Source = util.SanitizeDisplay(c.Source, maxMetaRunes)
	c.Artist = util.SanitizeDisplay(c.Artist, maxMetaRunes)
	c.Title = util.SanitizeDisplay(c.Title, maxMetaRunes)
	c.Album = util.SanitizeDisplay(c.Album, maxMetaRunes)
	if len(c.URL) > maxURLLen || !util.IsDisplaySafe(c.URL) {
		c.URL = ""
	} else if _, err := netguard.CheckURL(c.URL); err != nil {
		c.URL = ""
	}
	return c
}

// stripURL removes the URL from a transport error so a provider-chosen URL
// (which may carry escape sequences) never reaches an error message.
func stripURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return ue.Err
	}
	return err
}
