//go:build !darwin && !windows && !cgo

package player

func (m *Manager) startOrStopStaticPlayer(enabled bool) {
	// Headless Linux without cgo: audio static safely disabled
}
