package app

import (
	"github.com/halpworld/halpradio/pkg/debuglog"
	"github.com/halpworld/halpradio/pkg/player/dsp"
	"github.com/halpworld/halpradio/pkg/util"
)

// loadDSPSettings reads the persisted equalizer & DSP rack (dsp.yaml) and
// applies the loudness target from config.yaml. A missing or unreadable file
// leaves the rack bypassed; playback never depends on it.
func loadDSPSettings(cfg util.Config) dsp.Settings {
	path := util.GetDSPFile()
	s, err := dsp.Load(path)
	if err != nil {
		debuglog.Logf("dsp", "ignoring unreadable %s: %v", path, err)
	}
	s.TargetLUFS = cfg.LoudnessTargetLUFS
	return s.Normalize()
}
