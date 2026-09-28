package dsp

import "math"

// Crossfeed defaults follow Boris Mikhaylov's bs2b "default" profile, itself a
// digital take on Benjamin Bauer's 1961 stereophonic-to-binaural network and
// the Chu Moy headphone amplifier crossfeed: 700 Hz cut, 4.5 dB feed.
const (
	crossfeedCutHz  = 700.0
	crossfeedFeedDB = 4.5
)

// crossfeed blends a low-passed copy of each channel into the opposite ear,
// the way sound from a left loudspeaker also reaches the right ear. The
// one-pole low-pass supplies the head-shadow roll-off and, through its group
// delay (≈ 1/(2π·700 Hz) ≈ 230 µs), the interaural time difference, while a
// matching high-shelf on the direct path keeps overall tonal balance flat.
type crossfeed struct {
	// Cross (low-pass) path.
	aLo, bLo float64
	// Direct (high-boost) path.
	a0Hi, a1Hi, b1Hi float64

	loL, loR     float64 // low-pass state per channel
	hiL, hiR     float64 // high-shelf state per channel
	prevL, prevR float64 // previous input sample per channel
}

func newCrossfeed(sampleRate int) *crossfeed {
	sr := float64(sampleRate)
	gbLo := crossfeedFeedDB*-5/6 - 3
	gbHi := crossfeedFeedDB/6 - 3
	gLo := math.Pow(10, gbLo/20)
	gHi := 1 - math.Pow(10, gbHi/20)
	fcHi := crossfeedCutHz * math.Pow(2, (gbLo-20*math.Log10(gHi))/12)

	c := &crossfeed{}
	x := math.Exp(-2 * math.Pi * crossfeedCutHz / sr)
	c.bLo = x
	c.aLo = gLo * (1 - x)

	x = math.Exp(-2 * math.Pi * fcHi / sr)
	c.b1Hi = x
	c.a0Hi = 1 - gHi*(1-x)
	c.a1Hi = -x
	return c
}

func (c *crossfeed) process(l, r float64) (float64, float64) {
	c.loL = c.aLo*l + c.bLo*c.loL
	c.loR = c.aLo*r + c.bLo*c.loR
	c.hiL = c.a0Hi*l + c.a1Hi*c.prevL + c.b1Hi*c.hiL
	c.hiR = c.a0Hi*r + c.a1Hi*c.prevR + c.b1Hi*c.hiR
	c.prevL, c.prevR = l, r
	return c.hiL + c.loR, c.hiR + c.loL
}
