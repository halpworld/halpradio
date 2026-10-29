package dsp

import "math"

const (
	limiterLookaheadSeconds = 0.005
	limiterReleaseSeconds   = 0.12
	// limiterCeiling is -1 dBFS, the EBU R128 recommended maximum peak.
	limiterCeiling = 0.891
)

// limiter is a stereo-linked lookahead peak limiter. The audio is delayed by
// the lookahead while the required gain reduction is known in advance; that
// gain curve is released gradually and smoothed with a moving average as long
// as the lookahead, so reduction ramps in before a peak arrives instead of
// clicking on it.
type limiter struct {
	lookahead int
	release   float64 // gain recovery per sample

	delayL, delayR []float64
	gains          []float64
	pos            int
	gainSum        float64
	env            float64
}

func newLimiter(sampleRate int) *limiter {
	n := int(math.Round(float64(sampleRate) * limiterLookaheadSeconds))
	if n < 1 {
		n = 1
	}
	lm := &limiter{
		lookahead: n,
		release:   1 / (float64(sampleRate) * limiterReleaseSeconds),
		delayL:    make([]float64, n),
		delayR:    make([]float64, n),
		gains:     make([]float64, n),
		env:       1,
	}
	for i := range lm.gains {
		lm.gains[i] = 1
	}
	lm.gainSum = float64(n)
	return lm
}

func (lm *limiter) process(l, r float64) (float64, float64) {
	peak := math.Max(math.Abs(l), math.Abs(r))
	want := 1.0
	if peak > limiterCeiling {
		want = limiterCeiling / peak
	}
	lm.env = math.Min(want, lm.env+lm.release)

	// What falls out of the ring is the sample from one lookahead ago. The
	// gain ring still holds the envelope from that sample up to the previous
	// one, so the average already contains the reduction that sample needs.
	outL, outR := lm.delayL[lm.pos], lm.delayR[lm.pos]
	g := lm.gainSum / float64(lm.lookahead)

	lm.delayL[lm.pos], lm.delayR[lm.pos] = l, r
	lm.gainSum += lm.env - lm.gains[lm.pos]
	lm.gains[lm.pos] = lm.env
	lm.pos = (lm.pos + 1) % lm.lookahead
	if lm.pos == 0 {
		// Re-sum once per lap so rounding in the running sum cannot drift
		// over hours of playback.
		lm.gainSum = 0
		for _, v := range lm.gains {
			lm.gainSum += v
		}
	}

	return clampUnit(outL * g), clampUnit(outR * g)
}

func clampUnit(x float64) float64 {
	if x > 1 {
		return 1
	}
	if x < -1 {
		return -1
	}
	return x
}
