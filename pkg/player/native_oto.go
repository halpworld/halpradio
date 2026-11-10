//go:build darwin || windows || cgo

package player

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/halpworld/halpradio/pkg/player/dsp"
	"github.com/halpworld/halpradio/pkg/player/mp3safe"
	"github.com/halpworld/halpradio/pkg/player/streamnet"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/util"
)

func (m *Manager) playNative(ctx context.Context, st radio.Station, vol int, allowPrivate bool) {
	// Untrusted audio must never take the app down (CLAUDE.md rule 5): any
	// panic on this goroutine becomes StatusError. Decoder reads on oto's own
	// goroutine are covered by mp3safe.Decoder.
	holdingLock := false
	defer func() {
		if r := recover(); r != nil {
			if holdingLock {
				m.mu.Unlock()
			}
			if ctx.Err() == nil {
				m.setError(fmt.Sprintf("Native playback failed: %v", r))
			}
		}
	}()

	if !IsValidStreamURL(st.URL) {
		m.setError(fmt.Sprintf("Invalid or unsupported stream URL '%s'", util.SanitizeDisplay(st.URL, maxErrURLRunes)))
		return
	}

	req, err := http.NewRequestWithContext(ctx, "GET", st.URL, nil)
	if err != nil {
		if ctx.Err() == nil {
			m.setError(fmt.Sprintf("Invalid station URL: %v", err))
		}
		return
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko)")

	// Shared stream policy: http/https only, capped redirects; restricted
	// addresses only for stations the private-host policy allows, and even
	// then never after a public first hop.
	client := streamnet.NewClient(streamnet.Options{AllowPrivate: allowPrivate})
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			m.setError(fmt.Sprintf("HTTP connection failed: %v", err))
		}
		return
	}

	if resp.StatusCode >= 400 {
		resp.Body.Close()
		if ctx.Err() == nil {
			m.setError(fmt.Sprintf("Station HTTP error %d: %s", resp.StatusCode, resp.Status))
		}
		return
	}

	// mp3safe skips ID3v2 tags with a size cap and turns go-mp3 panics into
	// errors, both here and on oto's reader goroutine.
	decoder, err := mp3safe.NewDecoder(resp.Body)
	if err != nil {
		resp.Body.Close()
		if ctx.Err() == nil {
			m.setError(fmt.Sprintf("Native MP3 decode failed: %v (install mpv or ffmpeg for AAC/other formats)", err))
		}
		return
	}

	m.mu.Lock()
	holdingLock = true
	otoCtx, ok := m.otoCtx.(*oto.Context)
	if !ok || otoCtx == nil || m.otoSampleRate != decoder.SampleRate() {
		op := &oto.NewContextOptions{
			SampleRate:   decoder.SampleRate(),
			ChannelCount: 2,
			Format:       oto.FormatSignedInt16LE,
		}
		newOtoCtx, readyChan, err := oto.NewContext(op)
		if err != nil {
			holdingLock = false
			m.mu.Unlock()
			resp.Body.Close()
			if ctx.Err() == nil {
				m.setError(fmt.Sprintf("Audio device init failed: %v", err))
			}
			return
		}
		<-readyChan
		otoCtx = newOtoCtx
		m.otoCtx = newOtoCtx
		m.otoSampleRate = decoder.SampleRate()
	}

	// Decoded PCM passes through the DSP rack on its way to the device.
	chain := dsp.NewChain(decoder.SampleRate(), m.dspSettings)
	player := otoCtx.NewPlayer(dsp.NewPCMReader(decoder, chain))
	player.SetVolume(float64(vol) / 100.0)
	player.Play()

	m.nativePlayer = player
	m.nativeStream = resp.Body
	m.dspChain = chain
	m.status = StatusPlaying
	holdingLock = false
	m.mu.Unlock()

	// Keep goroutine alive while playing until context cancelled or EOF
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if !player.IsPlaying() {
				if derr := decoder.Err(); derr != nil && ctx.Err() == nil {
					m.setError(fmt.Sprintf("Stream decode failed: %v", derr))
					return
				}
				m.mu.Lock()
				if ctx.Err() == nil && m.status == StatusPlaying {
					m.status = StatusStopped
				}
				m.mu.Unlock()
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
}
