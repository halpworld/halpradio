package dsp

import (
	"fmt"
	"strconv"
	"strings"
)

// LavfiGraph renders s as an FFmpeg libavfilter graph equivalent to the
// native Chain, in the same stage order, for backends built on FFmpeg (mpv's
// --af, ffplay's -af). It returns "" when the rack is bypassed.
//
//   - EQ: one octave-wide "equalizer" peaking filter per non-zero band.
//   - Lo-fi: "highpass" + "lowpass" band-limiting and an "asoftclip" tanh
//     saturator.
//   - Crossfeed: FFmpeg's built-in "crossfeed" (a Bauer-style shelf network).
//   - Normalizer: "loudnorm", FFmpeg's EBU R128 dynamic normalizer with a
//     true-peak limiter at -1 dBTP.
func LavfiGraph(s Settings) string {
	s = s.Normalize()
	var filters []string
	for i, g := range s.Bands {
		if g == 0 {
			continue
		}
		filters = append(filters, fmt.Sprintf("equalizer=f=%s:t=o:w=1:g=%s",
			formatNum(BandFrequencies[i]), formatNum(g)))
	}
	if s.LoFi {
		filters = append(filters,
			"highpass=f="+formatNum(lofiLowCutHz),
			"lowpass=f="+formatNum(lofiHighCutHz),
			"asoftclip=type=tanh",
		)
	}
	if s.Crossfeed {
		filters = append(filters, "crossfeed=strength=0.3:range=0.5")
	}
	if s.Normalizer {
		filters = append(filters, fmt.Sprintf("loudnorm=I=%s:TP=-1:LRA=11", formatNum(s.TargetLUFS)))
	}
	return strings.Join(filters, ",")
}

// MPVAudioFilter returns the value for mpv's --af option / "af" property: the
// graph wrapped in a labelled lavfi filter, or "" to clear all filters.
func MPVAudioFilter(s Settings) string {
	graph := LavfiGraph(s)
	if graph == "" {
		return ""
	}
	return "@halpradio:lavfi=[" + graph + "]"
}

func formatNum(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
