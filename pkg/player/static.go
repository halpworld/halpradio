package player

import (
	"math"
	"strings"
	"sync"
	"time"
)

// StaticSynthesizer generates realistic, authentic analog radio static,
// atmospheric RF noise, crackles (QRN), and carrier heterodyne whistles.
// It implements io.Reader, outputting 16-bit signed integer little-endian stereo PCM.
type StaticSynthesizer struct {
	mu             sync.RWMutex
	sampleRate     int
	enabled        bool
	signalStrength float64 // 0.0 (pure static) to 1.0 (perfect lock)
	masterVolume   float64 // 0.0 to 1.0
	isMuted        bool
	freq           float64
	stationFreq    float64
	band           string // "FM", "AM", "SW"

	// Pink noise filter state (Paul Kellet 6-pole IIR)
	b0, b1, b2, b3, b4, b5, b6 float64

	// Low-pass / band-pass filter state
	lpState float64

	// Atmospheric crackle / pop impulse state
	popEnv  float64
	popSign float64

	// Heterodyne whistle phase
	whistlePhase float64

	// Ionospheric fading LFO phase for SW
	lfoPhase float64

	// Fast 64-bit Xorshift PRNG state
	rngState uint64
}

// NewStaticSynthesizer creates an analog radio noise synthesizer at the specified sample rate.
func NewStaticSynthesizer(sampleRate int) *StaticSynthesizer {
	if sampleRate <= 0 {
		sampleRate = 44100
	}
	seed := uint64(time.Now().UnixNano())
	if seed == 0 {
		seed = 0x853c49e6748fea9b
	}
	return &StaticSynthesizer{
		sampleRate:   sampleRate,
		masterVolume: 0.8,
		band:         "FM",
		rngState:     seed,
	}
}

// SetEnabled toggles static sound generation on or off.
func (s *StaticSynthesizer) SetEnabled(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.enabled = enabled
}

// IsEnabled reports whether the static generator is active.
func (s *StaticSynthesizer) IsEnabled() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.enabled
}

// SetParams updates the tuner reception parameters: signal strength (0..1), frequency, band, master volume, and mute.
func (s *StaticSynthesizer) SetParams(signalStrength float64, freq float64, band string, masterVolume float64, isMuted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if signalStrength < 0 {
		signalStrength = 0
	} else if signalStrength > 1.0 {
		signalStrength = 1.0
	}
	s.signalStrength = signalStrength
	s.freq = freq
	if band != "" {
		s.band = strings.ToUpper(band)
	}
	if masterVolume < 0 {
		masterVolume = 0
	} else if masterVolume > 1.0 {
		masterVolume = 1.0
	}
	s.masterVolume = masterVolume
	s.isMuted = isMuted
}

// SetNearestStationCarrier sets the nearest station frequency for carrier heterodyne whistle emulation.
func (s *StaticSynthesizer) SetNearestStationCarrier(stationFreq float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stationFreq = stationFreq
}

// SetVolume updates the master volume and mute state.
func (s *StaticSynthesizer) SetVolume(volume float64, isMuted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if volume < 0 {
		volume = 0
	} else if volume > 1.0 {
		volume = 1.0
	}
	s.masterVolume = volume
	s.isMuted = isMuted
}

// nextFloat generates a pseudo-random float in [-1.0, 1.0] using Xorshift64*.
func (s *StaticSynthesizer) nextFloat() float64 {
	x := s.rngState
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	s.rngState = x
	// Multiply by 64-bit constant and extract high bits
	return float64(int32(x*0x2545F4914F6CDD1D>>32)) / 2147483648.0
}

// Read implements io.Reader, filling p with 16-bit stereo PCM signed little-endian audio.
func (s *StaticSynthesizer) Read(p []byte) (int, error) {
	s.mu.Lock()
	enabled := s.enabled
	isMuted := s.isMuted
	masterVol := s.masterVolume
	signal := s.signalStrength
	band := s.band
	freq := s.freq
	stationFreq := s.stationFreq
	sampleRate := s.sampleRate
	s.mu.Unlock()

	// Align to 4-byte frames (2 channels * 2 bytes/sample)
	frameBytes := 4
	usableLen := len(p) - (len(p) % frameBytes)
	if usableLen <= 0 {
		return 0, nil
	}

	// If disabled or muted, output silence to keep oto audio pump healthy
	if !enabled || isMuted || masterVol <= 0.001 {
		for i := 0; i < usableLen; i++ {
			p[i] = 0
		}
		return usableLen, nil
	}

	// Calculate static noise gain from signal strength (RSSI)
	// Higher RSSI -> substantially quieter static
	staticGain := math.Pow(math.Max(0.0, 1.0-signal), 1.35)
	if signal >= 0.96 {
		// Subtle 0.8% analog baseline noise floor at perfect lock
		staticGain = 0.008
	}

	// Overall output scalar (comfortable listening level, no clipping)
	effectiveGain := staticGain * masterVol * 0.26

	// Filter and synthesis parameters depending on band
	var lpCoeff float64
	var popChance float64
	switch band {
	case "AM":
		// Warm, mid-focused, atmospheric band (lowpass ~3.5 kHz)
		lpCoeff = 0.38
		popChance = 0.00040
	case "SW":
		// Ionospheric shortwave band with fading / flutter
		lpCoeff = 0.28
		popChance = 0.00055
	default: // "FM"
		// Wideband with high-frequency hiss
		lpCoeff = 0.72
		popChance = 0.00020
	}

	whistleFreq := 0.0
	whistleAmp := 0.0
	if stationFreq > 0 {
		diff := math.Abs(freq - stationFreq)
		if band == "AM" && diff < 8.0 && diff > 0.02 {
			whistleFreq = diff * 220.0
			whistleAmp = (1.0 - diff/8.0) * 0.14
		} else if band == "SW" && diff < 0.15 && diff > 0.001 {
			whistleFreq = (diff / 0.15) * 1800.0
			whistleAmp = (1.0 - diff/0.15) * 0.16
		}
	}

	for i := 0; i < usableLen; i += frameBytes {
		white := s.nextFloat()

		// Pink noise filter (Paul Kellet algorithm)
		s.b0 = 0.99886*s.b0 + white*0.0555179
		s.b1 = 0.99332*s.b1 + white*0.0750759
		s.b2 = 0.96900*s.b2 + white*0.1538520
		s.b3 = 0.86650*s.b3 + white*0.3104856
		s.b4 = 0.55000*s.b4 + white*0.5329522
		s.b5 = -0.7616*s.b5 - white*0.0168980
		pink := (s.b0 + s.b1 + s.b2 + s.b3 + s.b4 + s.b5 + s.b6 + white*0.5362) * 0.11
		s.b6 = white * 0.115926

		// Lowpass / bandpass filter
		if band == "SW" {
			// SW Ionospheric fading LFO (~0.2 Hz)
			s.lfoPhase += 2.0 * math.Pi * 0.2 / float64(sampleRate)
			if s.lfoPhase > 2.0*math.Pi {
				s.lfoPhase -= 2.0 * math.Pi
			}
			fading := 0.72 + 0.28*math.Sin(s.lfoPhase)
			s.lpState += (lpCoeff * fading) * (pink - s.lpState)
			pink = s.lpState * fading
		} else {
			s.lpState += lpCoeff * (pink - s.lpState)
			pink = s.lpState
		}

		// Atmospheric static crackles / pops (QRN)
		if s.nextFloat() > (1.0 - popChance*2.0) {
			s.popEnv = 0.35 + 0.45*math.Abs(s.nextFloat())
			if s.nextFloat() > 0 {
				s.popSign = 1.0
			} else {
				s.popSign = -1.0
			}
		}
		if s.popEnv > 0.001 {
			pink += s.popEnv * s.popSign
			s.popEnv *= 0.993 // exponential decay
		}

		// Carrier heterodyne beat note whistle
		if whistleAmp > 0.001 && whistleFreq > 30.0 {
			s.whistlePhase += 2.0 * math.Pi * whistleFreq / float64(sampleRate)
			if s.whistlePhase > 2.0*math.Pi {
				s.whistlePhase -= 2.0 * math.Pi
			}
			pink += math.Sin(s.whistlePhase) * whistleAmp
		}

		// Scale sample by master volume & static attenuation
		outSample := pink * effectiveGain

		// Soft limiter / clipping protection
		if outSample > 0.95 {
			outSample = 0.95
		} else if outSample < -0.95 {
			outSample = -0.95
		}

		val := int16(outSample * 32767.0)

		// Left channel
		p[i] = byte(val)
		p[i+1] = byte(val >> 8)
		// Right channel
		p[i+2] = byte(val)
		p[i+3] = byte(val >> 8)
	}

	return usableLen, nil
}
