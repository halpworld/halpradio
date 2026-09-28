package dsp

import "math"

// biquad is a normalised second-order IIR section (a0 == 1) in transposed
// direct form II, which keeps state small and is numerically well behaved in
// float64.
type biquad struct {
	b0, b1, b2, a1, a2 float64
	z1, z2             float64
}

func (f *biquad) process(x float64) float64 {
	y := f.b0*x + f.z1
	f.z1 = f.b1*x - f.a1*y + f.z2
	f.z2 = f.b2*x - f.a2*y
	return y
}

func (f *biquad) reset() {
	f.z1, f.z2 = 0, 0
}

// setCoefficients installs new coefficients while keeping the filter state, so
// a live change does not click.
func (f *biquad) setCoefficients(b0, b1, b2, a0, a1, a2 float64) {
	f.b0, f.b1, f.b2 = b0/a0, b1/a0, b2/a0
	f.a1, f.a2 = a1/a0, a2/a0
}

// peaking configures an RBJ Audio EQ Cookbook peaking filter.
func (f *biquad) peaking(sampleRate, freq, q, gainDB float64) {
	a := math.Pow(10, gainDB/40)
	w0 := 2 * math.Pi * freq / sampleRate
	alpha := math.Sin(w0) / (2 * q)
	cosw := math.Cos(w0)
	f.setCoefficients(
		1+alpha*a, -2*cosw, 1-alpha*a,
		1+alpha/a, -2*cosw, 1-alpha/a,
	)
}

// highpass configures an RBJ second-order high-pass filter.
func (f *biquad) highpass(sampleRate, freq, q float64) {
	w0 := 2 * math.Pi * freq / sampleRate
	alpha := math.Sin(w0) / (2 * q)
	cosw := math.Cos(w0)
	f.setCoefficients(
		(1+cosw)/2, -(1 + cosw), (1+cosw)/2,
		1+alpha, -2*cosw, 1-alpha,
	)
}

// lowpass configures an RBJ second-order low-pass filter.
func (f *biquad) lowpass(sampleRate, freq, q float64) {
	w0 := 2 * math.Pi * freq / sampleRate
	alpha := math.Sin(w0) / (2 * q)
	cosw := math.Cos(w0)
	f.setCoefficients(
		(1-cosw)/2, 1-cosw, (1-cosw)/2,
		1+alpha, -2*cosw, 1-alpha,
	)
}

// magnitudeAt returns the filter's linear gain at freq, used by tests to check
// the designed response.
func (f *biquad) magnitudeAt(sampleRate, freq float64) float64 {
	w := 2 * math.Pi * freq / sampleRate
	// H(e^jw) = (b0 + b1 e^-jw + b2 e^-2jw) / (1 + a1 e^-jw + a2 e^-2jw)
	numRe := f.b0 + f.b1*math.Cos(w) + f.b2*math.Cos(2*w)
	numIm := -f.b1*math.Sin(w) - f.b2*math.Sin(2*w)
	denRe := 1 + f.a1*math.Cos(w) + f.a2*math.Cos(2*w)
	denIm := -f.a1*math.Sin(w) - f.a2*math.Sin(2*w)
	return math.Hypot(numRe, numIm) / math.Hypot(denRe, denIm)
}
