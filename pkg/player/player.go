package player

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/halpworld/halpradio/pkg/radio"
)

type PlayStatus string

const (
	StatusStopped    PlayStatus = "STOPPED"
	StatusConnecting PlayStatus = "CONNECTING"
	StatusPlaying    PlayStatus = "PLAYING"
	StatusPaused     PlayStatus = "PAUSED"
	StatusError      PlayStatus = "ERROR"
)

type NativeAudioPlayer interface {
	Close() error
	Pause()
	Play()
	SetVolume(volume float64)
	IsPlaying() bool
}

type Player interface {
	Play(st radio.Station) error
	Stop() error
	Pause() error
	Resume() error
	SetVolume(vol int) int
	Volume() int
	ToggleMute() bool
	IsMuted() bool
	Status() PlayStatus
	CurrentStation() *radio.Station
	CurrentTrack() string
	ActiveBackend() string
	Error() string
}

type TrackInfo struct {
	StationID   string
	StationName string
	TrackTitle  string
}

type Manager struct {
	mu             sync.Mutex
	status         PlayStatus
	currentStation *radio.Station
	currentTrack   string
	activeBackend  string
	volume         int
	isMuted        bool
	lastError      string

	cmd        *exec.Cmd
	cancelFn   context.CancelFunc
	icyCancel  context.CancelFunc
	onTrackUpd func(TrackInfo)

	otoCtx        any
	otoSampleRate int
	nativePlayer  NativeAudioPlayer
	nativeStream  io.Closer
}

func NewManager(preferredBackend string, initialVolume int, onTrackUpd func(TrackInfo)) *Manager {
	if initialVolume <= 0 || initialVolume > 100 {
		initialVolume = 80
	}
	m := &Manager{
		status:        StatusStopped,
		volume:        initialVolume,
		onTrackUpd:    onTrackUpd,
		activeBackend: detectBackend(preferredBackend),
	}
	return m
}

func detectBackend(preferred string) string {
	if preferred != "" && preferred != "auto" {
		if preferred == "native" || preferred == "go" {
			return "native"
		}
		if _, err := exec.LookPath(preferred); err == nil {
			return preferred
		}
	}
	// Detect external CLI players in order of preference
	candidates := []string{"mpv", "vlc", "cvlc", "ffplay", "mplayer", "mpg123"}
	for _, c := range candidates {
		if _, err := exec.LookPath(c); err == nil {
			return c
		}
	}
	// Built-in native Go audio backend (no external dependencies required)
	return "native"
}

func (m *Manager) ActiveBackend() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.activeBackend
}

func (m *Manager) Status() PlayStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

func (m *Manager) CurrentStation() *radio.Station {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentStation
}

func (m *Manager) CurrentTrack() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentTrack
}

func (m *Manager) Error() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastError
}

func (m *Manager) Volume() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.isMuted {
		return 0
	}
	return m.volume
}

func (m *Manager) IsMuted() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isMuted
}

func (m *Manager) SetVolume(vol int) int {
	m.mu.Lock()
	if vol < 0 {
		vol = 0
	}
	if vol > 100 {
		vol = 100
	}
	m.volume = vol
	m.isMuted = false
	currVol := m.volume
	np := m.nativePlayer
	m.mu.Unlock()

	if np != nil {
		np.SetVolume(float64(currVol) / 100.0)
	}

	return currVol
}

func (m *Manager) ToggleMute() bool {
	m.mu.Lock()
	m.isMuted = !m.isMuted
	muted := m.isMuted
	vol := m.volume
	np := m.nativePlayer
	m.mu.Unlock()

	if np != nil {
		if muted {
			np.SetVolume(0.0)
		} else {
			np.SetVolume(float64(vol) / 100.0)
		}
	}
	return muted
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	if m.cancelFn != nil {
		m.cancelFn()
		m.cancelFn = nil
	}
	if m.icyCancel != nil {
		m.icyCancel()
		m.icyCancel = nil
	}
	if m.cmd != nil && m.cmd.Process != nil {
		_ = m.cmd.Process.Kill()
		m.cmd = nil
	}
	if m.nativePlayer != nil {
		_ = m.nativePlayer.Close()
		m.nativePlayer = nil
	}
	if m.nativeStream != nil {
		_ = m.nativeStream.Close()
		m.nativeStream = nil
	}
	m.status = StatusStopped
	m.currentTrack = ""
	m.mu.Unlock()
	return nil
}

func (m *Manager) Pause() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status == StatusPlaying {
		if m.nativePlayer != nil {
			m.nativePlayer.Pause()
		}
		if m.cmd != nil && m.cmd.Process != nil {
			_ = m.cmd.Process.Kill()
		}
		m.status = StatusPaused
	}
	return nil
}

func (m *Manager) Resume() error {
	m.mu.Lock()
	st := m.currentStation
	np := m.nativePlayer
	m.mu.Unlock()

	if np != nil {
		np.Play()
		m.mu.Lock()
		m.status = StatusPlaying
		m.mu.Unlock()
		return nil
	}

	if st != nil {
		return m.Play(*st)
	}
	return nil
}

// IsValidStreamURL validates that a URL has a valid syntax, non-empty host, and uses http/https scheme.
func IsValidStreamURL(rawURL string) bool {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return false
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "http" || u.Scheme == "https"
}

// sanitizeTrackTitle removes ASCII control characters and non-printable runes from remote stream titles.
func sanitizeTrackTitle(title string) string {
	var b strings.Builder
	for _, r := range title {
		if r >= 32 && r != 127 && unicode.IsPrint(r) {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

func (m *Manager) Play(st radio.Station) error {
	_ = m.Stop()

	if !IsValidStreamURL(st.URL) {
		m.setError(fmt.Sprintf("Invalid stream URL '%s' (only http/https supported)", st.URL))
		return nil
	}

	m.mu.Lock()
	m.currentStation = &st
	m.status = StatusConnecting
	m.currentTrack = st.Name
	m.lastError = ""
	backend := m.activeBackend
	vol := m.volume
	if m.isMuted {
		vol = 0
	}
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancelFn = cancel
	m.mu.Unlock()

	if backend == "native" {
		go m.playNative(ctx, st, vol)
	} else {
		go m.playExternal(ctx, backend, st, vol)
	}

	// Start ICY Stream Title reader in background
	icyCtx, icyCancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.icyCancel = icyCancel
	m.mu.Unlock()
	go m.startICYListener(icyCtx, st)

	return nil
}

func (m *Manager) playExternal(ctx context.Context, backend string, st radio.Station, vol int) {
	if !IsValidStreamURL(st.URL) {
		m.setError(fmt.Sprintf("Invalid or unsupported stream URL '%s'", st.URL))
		return
	}

	var cmd *exec.Cmd
	switch backend {
	case "mpv":
		volArg := fmt.Sprintf("--volume=%d", vol)
		cmd = exec.CommandContext(ctx, "mpv", "--no-video", "--quiet", volArg, "--", st.URL)

	case "vlc", "cvlc":
		gainArg := fmt.Sprintf("--gain=%.2f", float64(vol)/100.0)
		cmd = exec.CommandContext(ctx, backend, "-I", "dummy", "--quiet", gainArg, "--", st.URL)

	case "ffplay":
		volArg := fmt.Sprintf("%d", vol)
		cmd = exec.CommandContext(ctx, "ffplay", "-nodisp", "-loglevel", "quiet", "-volume", volArg, "--", st.URL)

	case "mplayer":
		volArg := fmt.Sprintf("%d", vol)
		cmd = exec.CommandContext(ctx, "mplayer", "-quiet", "-volume", volArg, "--", st.URL)

	case "mpg123":
		volArg := fmt.Sprintf("%d", vol)
		cmd = exec.CommandContext(ctx, "mpg123", "-q", "-g", volArg, "--", st.URL)

	default:
		m.setError(fmt.Sprintf("Unknown backend '%s'", backend))
		return
	}

	m.mu.Lock()
	m.cmd = cmd
	m.mu.Unlock()

	err := cmd.Start()
	if err != nil {
		if ctx.Err() == nil {
			m.setError(fmt.Sprintf("%s start failed: %v", backend, err))
		}
		return
	}

	m.mu.Lock()
	m.status = StatusPlaying
	m.mu.Unlock()

	err = cmd.Wait()

	m.mu.Lock()
	if ctx.Err() == nil {
		if err != nil {
			m.status = StatusError
			m.lastError = fmt.Sprintf("%s playback error: %v", backend, err)
		} else if m.status == StatusPlaying {
			m.status = StatusStopped
		}
	}
	m.mu.Unlock()
}

func (m *Manager) setError(errMsg string) {
	m.mu.Lock()
	m.status = StatusError
	m.lastError = errMsg
	m.mu.Unlock()
}

// maxIcyMetaInt defines a safe upper bound (256 KB) for ICY metadata intervals to prevent OOM denial-of-service.
const maxIcyMetaInt = 262144

// startICYListener connects to the stream with ICY header to extract real-time song title
func (m *Manager) startICYListener(ctx context.Context, st radio.Station) {
	if !IsValidStreamURL(st.URL) {
		return
	}

	req, err := http.NewRequestWithContext(ctx, "GET", st.URL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Icy-MetaData", "1")
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
		return
	}
	defer resp.Body.Close()

	icyMetaInt := resp.Header.Get("Icy-Metaint")
	if icyMetaInt == "" {
		return
	}

	metaInt, err := strconv.Atoi(icyMetaInt)
	if err != nil || metaInt <= 0 || metaInt > maxIcyMetaInt {
		return
	}

	reader := bufio.NewReader(resp.Body)
	buffer := make([]byte, metaInt)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		_, err := ioReadFull(reader, buffer)
		if err != nil {
			return
		}

		lenByte, err := reader.ReadByte()
		if err != nil {
			return
		}

		metaLen := int(lenByte) * 16
		if metaLen > 0 {
			metaBuf := make([]byte, metaLen)
			_, err := ioReadFull(reader, metaBuf)
			if err != nil {
				return
			}

			str := string(metaBuf)
			if idx := strings.Index(str, "StreamTitle='"); idx != -1 {
				str = str[idx+len("StreamTitle='"):]
				if endIdx := strings.Index(str, "';"); endIdx != -1 {
					title := sanitizeTrackTitle(str[:endIdx])
					if title != "" {
						m.mu.Lock()
						m.currentTrack = title
						m.mu.Unlock()

						if m.onTrackUpd != nil {
							m.onTrackUpd(TrackInfo{
								StationID:   st.ID,
								StationName: st.Name,
								TrackTitle:  title,
							})
						}
					}
				}
			}
		}
	}
}

func ioReadFull(r *bufio.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		nn, err := r.Read(buf[n:])
		n += nn
		if err != nil {
			return n, err
		}
	}
	return n, nil
}
