//go:build darwin || windows || cgo

package player

import (
	"github.com/ebitengine/oto/v3"
)

func (m *Manager) startOrStopStaticPlayer(enabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !enabled {
		if m.staticPlayer != nil {
			m.staticPlayer.Pause()
		}
		return
	}

	otoCtx, ok := m.otoCtx.(*oto.Context)
	if !ok || otoCtx == nil {
		op := &oto.NewContextOptions{
			SampleRate:   44100,
			ChannelCount: 2,
			Format:       oto.FormatSignedInt16LE,
		}
		newOtoCtx, readyChan, err := oto.NewContext(op)
		if err != nil {
			// Audio device unavailable, safely continue without audio static
			return
		}
		<-readyChan
		otoCtx = newOtoCtx
		m.otoCtx = newOtoCtx
		m.otoSampleRate = 44100
	}

	if m.staticPlayer == nil && m.staticSynth != nil {
		m.staticPlayer = otoCtx.NewPlayer(m.staticSynth)
	}

	if m.staticPlayer != nil {
		m.staticPlayer.Play()
	}
}
