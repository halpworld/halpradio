package dsp

import "math"

// Loudness measurement follows ITU-R BS.1770-4 / EBU R128: K-weighting, 400 ms
// blocks overlapping by 75 % (a new block every 100 ms), an absolute gate at
// -70 LUFS and a relative gate 10 LU below the ungated level. Instead of
// integrating over the whole programme — a radio stream never ends — the
// gated integration runs over a sliding window, giving a "sliding integrated
// loudness" that follows a station's level without reacting to individual
// beats or phrases.
const (
	loudnessOffset     = -0.691
	absoluteGateLUFS   = -70.0
	relativeGateLU     = -10.0
	subBlockSeconds    = 0.1
	subBlocksPerBlock  = 4    // 400 ms momentary block
	integrationBlocks  = 100  // 10 s sliding integration window
	warmupBlocks       = 30   // first 3 s converge quickly
	maxBoostDB         = 12.0 // never lift a quiet station by more than this
	maxCutDB           = -24.0
	warmupSlewDBPerSec = 30.0
	cutSlewDBPerSec    = 3.0
	boostSlewDBPerSec  = 1.5
)

// normalizer is a slow automatic gain control steering the sliding
// integrated loudness of its input towards a LUFS target. It measures the
// input before its own gain, so the loop has no feedback and cannot pump.
type normalizer struct {
	target float64

	kWeight [2][2]biquad // per channel: stage 1 high shelf, stage 2 RLB high-pass

	subLen   int
	subPos   int
	subSum   float64
	subPower [subBlocksPerBlock]float64
	subCount int

	blockPower [integrationBlocks]float64
	blockHead  int
	blockCount int

	measured  float64 // last sliding integrated loudness, LUFS (-Inf if none)
	desiredDB float64
	gainDB    float64
	gainLin   float64

	warmupStep float64 // dB per sample
	cutStep    float64
	boostStep  float64
	blocksSeen int
}

func newNormalizer(sampleRate int, targetLUFS float64) *normalizer {
	sr := float64(sampleRate)
	n := &normalizer{
		target:     ClampTargetLUFS(targetLUFS),
		subLen:     int(math.Round(sr * subBlockSeconds)),
		measured:   math.Inf(-1),
		gainLin:    1,
		warmupStep: warmupSlewDBPerSec / sr,
		cutStep:    cutSlewDBPerSec / sr,
		boostStep:  boostSlewDBPerSec / sr,
	}
	if n.subLen < 1 {
		n.subLen = 1
	}
	for ch := 0; ch < 2; ch++ {
		kWeightingShelf(&n.kWeight[ch][0], sr)
		kWeightingHighpass(&n.kWeight[ch][1], sr)
	}
	return n
}

// kWeightingShelf designs BS.1770's stage-1 "head" high-shelf for any sample
// rate, using the analogue prototype parameters from libebur128.
func kWeightingShelf(f *biquad, sampleRate float64) {
	const (
		f0 = 1681.974450955533
		g  = 3.999843853973347
		q  = 0.7071752369554196
	)
	k := math.Tan(math.Pi * f0 / sampleRate)
	vh := math.Pow(10, g/20)
	vb := math.Pow(vh, 0.4996667741545416)
	f.setCoefficients(
		vh+vb*k/q+k*k, 2*(k*k-vh), vh-vb*k/q+k*k,
		1+k/q+k*k, 2*(k*k-1), 1-k/q+k*k,
	)
}

// kWeightingHighpass designs BS.1770's stage-2 RLB high-pass.
func kWeightingHighpass(f *biquad, sampleRate float64) {
	const (
		f0 = 38.13547087602444
		q  = 0.5003270373238773
	)
	k := math.Tan(math.Pi * f0 / sampleRate)
	f.setCoefficients(
		1, -2, 1,
		1+k/q+k*k, 2*(k*k-1), 1-k/q+k*k,
	)
}

func (n *normalizer) setTarget(t float64) {
	n.target = ClampTargetLUFS(t)
	n.updateDesired()
}

// process measures one stereo frame and returns it with the current gain.
func (n *normalizer) process(l, r float64) (float64, float64) {
	kl := n.kWeight[0][1].process(n.kWeight[0][0].process(l))
	kr := n.kWeight[1][1].process(n.kWeight[1][0].process(r))
	n.subSum += kl*kl + kr*kr
	n.subPos++
	if n.subPos >= n.subLen {
		n.finishSubBlock()
	}

	if n.gainDB != n.desiredDB {
		step := n.boostStep
		if n.blocksSeen < warmupBlocks {
			step = n.warmupStep
		} else if n.desiredDB < n.gainDB {
			step = n.cutStep
		}
		if n.desiredDB > n.gainDB {
			n.gainDB = math.Min(n.desiredDB, n.gainDB+step)
		} else {
			n.gainDB = math.Max(n.desiredDB, n.gainDB-step)
		}
		n.gainLin = math.Pow(10, n.gainDB/20)
	}
	return l * n.gainLin, r * n.gainLin
}

func (n *normalizer) finishSubBlock() {
	power := n.subSum / float64(n.subLen)
	n.subSum, n.subPos = 0, 0

	copy(n.subPower[:], n.subPower[1:])
	n.subPower[subBlocksPerBlock-1] = power
	if n.subCount < subBlocksPerBlock {
		n.subCount++
		if n.subCount < subBlocksPerBlock {
			return
		}
	}

	var block float64
	for _, p := range n.subPower {
		block += p
	}
	block /= subBlocksPerBlock

	n.blockPower[n.blockHead] = block
	n.blockHead = (n.blockHead + 1) % integrationBlocks
	if n.blockCount < integrationBlocks {
		n.blockCount++
	}
	n.blocksSeen++

	n.measured = n.gatedLoudness()
	n.updateDesired()
}

// gatedLoudness applies the BS.1770 two-stage gate to the blocks in the
// sliding window and returns their loudness, or -Inf if everything is silence.
func (n *normalizer) gatedLoudness() float64 {
	absGate := lufsToPower(absoluteGateLUFS)
	var sum float64
	var count int
	for i := 0; i < n.blockCount; i++ {
		if p := n.blockPower[i]; p > absGate {
			sum += p
			count++
		}
	}
	if count == 0 {
		return math.Inf(-1)
	}
	relGate := lufsToPower(powerToLUFS(sum/float64(count)) + relativeGateLU)
	sum, count = 0, 0
	for i := 0; i < n.blockCount; i++ {
		if p := n.blockPower[i]; p > absGate && p > relGate {
			sum += p
			count++
		}
	}
	if count == 0 {
		return math.Inf(-1)
	}
	return powerToLUFS(sum / float64(count))
}

// updateDesired recomputes the target gain. During silence it holds the
// previous gain rather than boosting the noise floor.
func (n *normalizer) updateDesired() {
	if math.IsInf(n.measured, -1) {
		return
	}
	n.desiredDB = math.Max(maxCutDB, math.Min(maxBoostDB, n.target-n.measured))
}

func powerToLUFS(p float64) float64 {
	if p <= 0 {
		return math.Inf(-1)
	}
	return loudnessOffset + 10*math.Log10(p)
}

func lufsToPower(lufs float64) float64 {
	return math.Pow(10, (lufs-loudnessOffset)/10)
}
