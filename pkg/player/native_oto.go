//go:build darwin || windows || cgo

package player

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/ebitengine/oto/v3"
	"github.com/hajimehoshi/go-mp3"
	"github.com/halpworld/halpradio/pkg/radio"
)

func (m *Manager) playNative(ctx context.Context, st radio.Station, vol int) {
	if !IsValidStreamURL(st.URL) {
		m.setError(fmt.Sprintf("Invalid or unsupported stream URL '%s'", st.URL))
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

	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 10 * time.Second,
	}
	client := &http.Client{Transport: transport}
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

	decoder, err := mp3.NewDecoder(resp.Body)
	if err != nil {
		resp.Body.Close()
		if ctx.Err() == nil {
			m.setError(fmt.Sprintf("Native MP3 decode failed: %v (install mpv or ffmpeg for AAC/other formats)", err))
		}
		return
	}

	m.mu.Lock()
	otoCtx, ok := m.otoCtx.(*oto.Context)
	if !ok || otoCtx == nil || m.otoSampleRate != decoder.SampleRate() {
		op := &oto.NewContextOptions{
			SampleRate:   decoder.SampleRate(),
			ChannelCount: 2,
			Format:       oto.FormatSignedInt16LE,
		}
		newOtoCtx, readyChan, err := oto.NewContext(op)
		if err != nil {
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

	player := otoCtx.NewPlayer(decoder)
	player.SetVolume(float64(vol) / 100.0)
	player.Play()

	m.nativePlayer = player
	m.nativeStream = resp.Body
	m.status = StatusPlaying
	m.mu.Unlock()

	// Keep goroutine alive while playing until context cancelled or EOF
	for {
		select {
		case <-ctx.Done():
			return
		default:
			if !player.IsPlaying() {
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
