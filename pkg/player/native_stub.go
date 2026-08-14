//go:build !darwin && !windows && !cgo

package player

import (
	"context"

	"github.com/halpworld/halpradio/pkg/radio"
)

func (m *Manager) playNative(ctx context.Context, st radio.Station, vol int) {
	m.setError("Native Go audio backend requires CGO and ALSA on Linux. Please use an external backend (mpv, vlc, ffplay) or compile with CGO_ENABLED=1.")
}
