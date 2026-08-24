//go:build !darwin || !cgo

package player

// startAutoPause is a no-op on platforms without the CoreAudio default output
// device listener (non-macOS builds or builds with CGO disabled).
func startAutoPause(onEvent func()) func() {
	return nil
}
