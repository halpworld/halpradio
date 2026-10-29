package dsp

import (
	"encoding/binary"
	"io"
	"math"
	"sync"
)

// Chain runs the rack over interleaved stereo audio in this order:
//
//	equalizer → lo-fi tape → crossfeed → loudness normalizer → peak limiter
//
// The EQ and tape colouration come first so the normalizer measures what the
// listener actually hears, and the limiter sits last to catch any peak the
// boosts produced. Settings may be changed from another goroutine while audio
// is flowing; the change takes effect on the next buffer.
type Chain struct {
	mu         sync.Mutex
	sampleRate int
	settings   Settings

	eq        *equalizer
	lofi      *lofi
	crossfeed *crossfeed
	norm      *normalizer
	limiter   *limiter
}

// NewChain builds a rack for the given sample rate and settings.
func NewChain(sampleRate int, s Settings) *Chain {
	if sampleRate <= 0 {
		sampleRate = 44100
	}
	c := &Chain{
		sampleRate: sampleRate,
		eq:         newEqualizer(sampleRate),
		limiter:    newLimiter(sampleRate),
	}
	c.apply(s.Normalize())
	return c
}

// SampleRate returns the rate the chain was designed for.
func (c *Chain) SampleRate() int { return c.sampleRate }

// Update swaps in new settings. Stages that stay enabled keep their state, so
// adjusting the EQ or target loudness mid-stream is seamless.
func (c *Chain) Update(s Settings) {
	c.mu.Lock()
	c.apply(s.Normalize())
	c.mu.Unlock()
}

// Settings returns a copy of the active settings.
func (c *Chain) Settings() Settings {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.settings.Clone()
}

// Loudness reports the normalizer's sliding integrated loudness in LUFS and
// the gain it currently applies in dB. ok is false while the normalizer is
// off or has not heard enough non-silent audio yet.
func (c *Chain) Loudness() (lufs float64, gainDB float64, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.norm == nil || math.IsInf(c.norm.measured, -1) {
		return 0, 0, false
	}
	return c.norm.measured, c.norm.gainDB, true
}

func (c *Chain) apply(s Settings) {
	c.settings = s
	c.eq.setGains(s.Bands)

	if s.LoFi && c.lofi == nil {
		c.lofi = newLoFi(c.sampleRate)
	} else if !s.LoFi {
		c.lofi = nil
	}
	if s.Crossfeed && c.crossfeed == nil {
		c.crossfeed = newCrossfeed(c.sampleRate)
	} else if !s.Crossfeed {
		c.crossfeed = nil
	}
	if s.Normalizer {
		if c.norm == nil {
			c.norm = newNormalizer(c.sampleRate, s.TargetLUFS)
		} else {
			c.norm.setTarget(s.TargetLUFS)
		}
	} else {
		c.norm = nil
	}
}

// Process runs the rack in place over interleaved stereo float32 samples in
// [-1, 1]. A trailing odd sample is left untouched. When every stage is
// bypassed the buffer is not modified at all.
func (c *Chain) Process(buf []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.settings.Active() {
		return
	}
	for i := 0; i+1 < len(buf); i += 2 {
		l, r := c.processFrame(float64(buf[i]), float64(buf[i+1]))
		buf[i], buf[i+1] = float32(l), float32(r)
	}
}

func (c *Chain) processFrame(l, r float64) (float64, float64) {
	l, r = c.eq.process(l, r)
	if c.lofi != nil {
		l, r = c.lofi.process(l, r)
	}
	if c.crossfeed != nil {
		l, r = c.crossfeed.process(l, r)
	}
	if c.norm != nil {
		l, r = c.norm.process(l, r)
	}
	return c.limiter.process(l, r)
}

// processInt16LE runs the rack over interleaved stereo signed 16-bit
// little-endian PCM in place. len(pcm) must be a multiple of 4.
func (c *Chain) processInt16LE(pcm []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.settings.Active() {
		return
	}
	for i := 0; i+3 < len(pcm); i += 4 {
		l := float64(int16(binary.LittleEndian.Uint16(pcm[i:]))) / 32768
		r := float64(int16(binary.LittleEndian.Uint16(pcm[i+2:]))) / 32768
		l, r = c.processFrame(l, r)
		binary.LittleEndian.PutUint16(pcm[i:], uint16(toInt16(l)))
		binary.LittleEndian.PutUint16(pcm[i+2:], uint16(toInt16(r)))
	}
}

func toInt16(x float64) int16 {
	v := math.Round(x * 32767)
	if v > 32767 {
		return 32767
	}
	if v < -32768 {
		return -32768
	}
	return int16(v)
}

// PCMReader applies a Chain to an io.Reader of interleaved stereo signed
// 16-bit little-endian PCM, such as a go-mp3 decoder feeding oto. Audio is
// processed in whole frames; a frame split across source reads is held back
// until it is complete.
type PCMReader struct {
	src   io.Reader
	chain *Chain

	buf  []byte // processing buffer
	out  []byte // processed bytes not yet handed to the caller
	err  error  // source error to report once out is drained
	raw  [4]byte
	nraw int // bytes of an incomplete frame carried to the next fill
}

// NewPCMReader wraps src so every frame read passes through chain.
func NewPCMReader(src io.Reader, chain *Chain) *PCMReader {
	return &PCMReader{src: src, chain: chain}
}

func (r *PCMReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.out) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		r.fill(len(p))
		if len(r.out) == 0 {
			return 0, r.err
		}
	}
	n := copy(p, r.out)
	r.out = r.out[n:]
	if len(r.out) == 0 && r.err != nil {
		return n, r.err
	}
	return n, nil
}

// fill reads at least one whole frame (or hits an error) and processes it.
func (r *PCMReader) fill(want int) {
	size := want + (4-want%4)%4
	if size < 4 {
		size = 4
	}
	if cap(r.buf) < size {
		r.buf = make([]byte, size)
	}
	buf := r.buf[:size]
	off := copy(buf, r.raw[:r.nraw])
	r.nraw = 0
	for {
		n, err := r.src.Read(buf[off:])
		off += n
		whole := off - off%4
		if whole > 0 || err != nil {
			if err == nil {
				r.nraw = copy(r.raw[:], buf[whole:off])
			}
			// On error a dangling partial frame is simply dropped.
			r.chain.processInt16LE(buf[:whole])
			r.out = buf[:whole]
			r.err = err
			return
		}
	}
}
