package dsp

import "math"

const (
	lofiLowCutHz  = 90.0
	lofiHighCutHz = 6500.0
	// lofiDrive sets how hard the tape saturator is pushed; tanh(drive·x) is
	// rescaled so a full-scale sample still maps to full scale.
	lofiDrive = 1.8
	// lofiHissLevel is the peak level of the tape/vinyl surface noise floor
	// (≈ -56 dBFS): audible in quiet passages, buried under music.
	lofiHissLevel = 0.0016
)

// lofi is the vintage cassette effect: band-limiting like a worn tape head, a
// soft tanh saturator for warm odd harmonics, and a faint low-passed surface
// hiss.
type lofi struct {
	hp, lp  [2]biquad
	hissLP  biquad
	norm    float64
	rngSeed uint32
}

func newLoFi(sampleRate int) *lofi {
	sr := float64(sampleRate)
	l := &lofi{norm: 1 / math.Tanh(lofiDrive), rngSeed: 0x9e3779b9}
	highCut := math.Min(lofiHighCutHz, 0.45*sr)
	for ch := 0; ch < 2; ch++ {
		l.hp[ch].highpass(sr, lofiLowCutHz, math.Sqrt2/2)
		l.lp[ch].lowpass(sr, highCut, math.Sqrt2/2)
	}
	l.hissLP.lowpass(sr, math.Min(3000, 0.45*sr), math.Sqrt2/2)
	return l
}

// noise returns a deterministic uniform sample in [-1, 1) from a xorshift32
// generator, cheap enough to run per sample without allocation or locking.
func (l *lofi) noise() float64 {
	x := l.rngSeed
	x ^= x << 13
	x ^= x >> 17
	x ^= x << 5
	l.rngSeed = x
	return float64(x)/float64(1<<31) - 1
}

func (l *lofi) process(left, right float64) (float64, float64) {
	hiss := l.hissLP.process(l.noise()) * lofiHissLevel
	left = l.lp[0].process(l.hp[0].process(left))
	right = l.lp[1].process(l.hp[1].process(right))
	left = math.Tanh(lofiDrive*left)*l.norm + hiss
	right = math.Tanh(lofiDrive*right)*l.norm + hiss
	return left, right
}
