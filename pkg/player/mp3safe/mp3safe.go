// Package mp3safe wraps github.com/hajimehoshi/go-mp3 so that untrusted
// stream bytes can never take the process down: decoder panics become errors,
// and ID3v2 tags are skipped here with a size cap instead of letting go-mp3
// allocate whatever size the tag header claims.
package mp3safe

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/hajimehoshi/go-mp3"
)

// MaxID3v2Size bounds the total size of leading ID3v2 tags that are skipped.
// Real tags with cover art stay well below this; a hostile header claiming
// hundreds of megabytes is rejected before anything is allocated.
const MaxID3v2Size = 1 << 20

// maxID3v2Tags bounds how many consecutive ID3v2 tags are skipped.
const maxID3v2Tags = 8

// ErrID3TooLarge is returned when leading ID3v2 tags exceed MaxID3v2Size.
var ErrID3TooLarge = errors.New("mp3: ID3v2 tag too large")

// ErrTooManyID3 is returned when more than maxID3v2Tags consecutive ID3v2
// tags lead the stream. go-mp3 would otherwise parse the next one itself,
// without a size cap.
var ErrTooManyID3 = errors.New("mp3: too many ID3v2 tags")

// ErrMalformedID3 is returned for an ID3v2 header whose size is not syncsafe.
var ErrMalformedID3 = errors.New("mp3: malformed ID3v2 tag header")

// Decoder is a panic-safe MP3 decoder producing 16-bit stereo PCM.
// Read runs on the audio backend's goroutine while Err is polled from
// another, so the failure is guarded by mu.
type Decoder struct {
	d *mp3.Decoder

	mu     sync.Mutex
	failed error
}

// NewDecoder skips leading ID3v2 tags (bounded), then parses the first MP3
// frame. A panic inside go-mp3 is returned as an error.
func NewDecoder(r io.Reader) (dec *Decoder, err error) {
	br := bufio.NewReaderSize(r, 4096)
	if err := skipID3v2(br); err != nil {
		return nil, err
	}
	defer func() {
		if rec := recover(); rec != nil {
			dec, err = nil, fmt.Errorf("mp3 decode panic: %v", rec)
		}
	}()
	d, err := mp3.NewDecoder(br)
	if err != nil {
		return nil, err
	}
	return &Decoder{d: d}, nil
}

// Read decodes PCM. After a decoder panic every call returns the same error.
func (s *Decoder) Read(p []byte) (n int, err error) {
	if failed := s.Err(); failed != nil {
		return 0, failed
	}
	defer func() {
		if rec := recover(); rec != nil {
			failed := fmt.Errorf("mp3 decode panic: %v", rec)
			s.mu.Lock()
			s.failed = failed
			s.mu.Unlock()
			n, err = 0, failed
		}
	}()
	return s.d.Read(p)
}

// SampleRate returns the stream's sample rate.
func (s *Decoder) SampleRate() int { return s.d.SampleRate() }

// Err reports the decoder panic converted to an error, if one happened.
func (s *Decoder) Err() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failed
}

// skipID3v2 discards leading ID3v2 tags, refusing tags whose declared sizes
// add up to more than MaxID3v2Size and more than maxID3v2Tags tags. On a nil
// return the reader is never positioned at an ID3v2 header, so go-mp3's own
// (uncapped) tag skipper finds nothing to allocate for.
func skipID3v2(br *bufio.Reader) error {
	total := 0
	for i := 0; i < maxID3v2Tags; i++ {
		hdr, err := br.Peek(10)
		if err != nil || string(hdr[:3]) != "ID3" {
			return nil // short stream or no tag: let the decoder report it
		}
		for _, b := range hdr[6:10] {
			if b&0x80 != 0 {
				return ErrMalformedID3
			}
		}
		size := int(hdr[6])<<21 | int(hdr[7])<<14 | int(hdr[8])<<7 | int(hdr[9])
		size += 10 // header
		if hdr[5]&0x10 != 0 {
			size += 10 // ID3v2.4 footer
		}
		total += size
		if total > MaxID3v2Size {
			return ErrID3TooLarge
		}
		if _, err := br.Discard(size); err != nil {
			return err
		}
	}
	if hdr, err := br.Peek(3); err == nil && string(hdr) == "ID3" {
		return ErrTooManyID3
	}
	return nil
}
