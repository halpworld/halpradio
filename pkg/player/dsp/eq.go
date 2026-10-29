package dsp

// eqBandQ gives each peaking band roughly one octave of bandwidth, so adjacent
// ISO octave bands overlap smoothly instead of leaving notches between them.
const eqBandQ = 1.41

// equalizer is the 10-band graphic equalizer: one peaking biquad per band and
// channel. Bands at 0 dB, or above the Nyquist-safe limit for the current
// sample rate, are skipped entirely.
type equalizer struct {
	sampleRate float64
	gains      [NumBands]float64
	active     [NumBands]bool
	filters    [2][NumBands]biquad
}

func newEqualizer(sampleRate int) *equalizer {
	return &equalizer{sampleRate: float64(sampleRate)}
}

// setGains redesigns the bands whose gain changed. Unchanged bands keep their
// state, and a band that was already running keeps its state across a gain
// change so dragging a slider does not click.
func (e *equalizer) setGains(bands []float64) {
	for i := 0; i < NumBands; i++ {
		var g float64
		if i < len(bands) {
			g = ClampBandGain(bands[i])
		}
		wasActive := e.active[i]
		e.active[i] = g != 0 && BandFrequencies[i] < 0.45*e.sampleRate
		if !e.active[i] {
			e.gains[i] = g
			continue
		}
		if wasActive && e.gains[i] == g {
			continue
		}
		e.gains[i] = g
		for ch := 0; ch < 2; ch++ {
			if !wasActive {
				e.filters[ch][i].reset()
			}
			e.filters[ch][i].peaking(e.sampleRate, BandFrequencies[i], eqBandQ, g)
		}
	}
}

func (e *equalizer) process(l, r float64) (float64, float64) {
	for i := 0; i < NumBands; i++ {
		if !e.active[i] {
			continue
		}
		l = e.filters[0][i].process(l)
		r = e.filters[1][i].process(r)
	}
	return l, r
}
