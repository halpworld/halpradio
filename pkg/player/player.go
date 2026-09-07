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

	"github.com/halpworld/halpradio/pkg/debuglog"
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
	SetTunerMode(enabled bool, signalStrength float64, freq float64, band string)
	UpdateTunerSignal(signalStrength float64, freq float64, band string)
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
	extCtrl    extControl
	cancelFn   context.CancelFunc
	icyCancel  context.CancelFunc
	icyStream  io.Closer
	onTrackUpd func(TrackInfo)

	otoCtx        any
	otoSampleRate int
	nativePlayer  NativeAudioPlayer
	nativeStream  io.Closer

	staticSynth   *StaticSynthesizer
	staticPlayer  NativeAudioPlayer
	tunerMode     bool
	lastTunerRSSI float64

	onAutoPause   func()
	autoPauseStop func()
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
		staticSynth:   NewStaticSynthesizer(44100),
	}
	debuglog.Logf("player", "backend %q selected (preferred %q), volume %d", m.activeBackend, preferredBackend, initialVolume)
	return m
}

// SetOnAutoPause registers a callback that is invoked after playback is
// automatically paused because the audio output device left Bluetooth.
func (m *Manager) SetOnAutoPause(cb func()) {
	m.mu.Lock()
	m.onAutoPause = cb
	m.mu.Unlock()
}

// SetAutoPause enables or disables automatic pausing when the default audio
// output device leaves Bluetooth (e.g. AirPods taken out of the ears).
func (m *Manager) SetAutoPause(enabled bool) {
	m.mu.Lock()
	if m.autoPauseStop != nil {
		stop := m.autoPauseStop
		m.autoPauseStop = nil
		m.mu.Unlock()
		stop()
		m.mu.Lock()
	}
	if enabled {
		m.autoPauseStop = startAutoPause(func() { m.autoPauseOnDeviceLoss() })
	}
	m.mu.Unlock()
}

func (m *Manager) autoPauseOnDeviceLoss() {
	m.mu.Lock()
	status := m.status
	m.mu.Unlock()
	if status != StatusPlaying && status != StatusConnecting {
		return
	}
	_ = m.Pause()
	m.mu.Lock()
	cb := m.onAutoPause
	m.mu.Unlock()
	if cb != nil {
		cb()
	}
}

// Close releases all player resources, including system audio device listeners.
func (m *Manager) Close() error {
	m.SetAutoPause(false)
	m.SetTunerMode(false, 1.0, 0, "")
	m.mu.Lock()
	if m.staticPlayer != nil {
		_ = m.staticPlayer.Close()
		m.staticPlayer = nil
	}
	m.mu.Unlock()
	return m.Stop()
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
	synth := m.staticSynth
	ctrl := m.extCtrl
	inTuner := m.tunerMode
	rssi := m.lastTunerRSSI
	m.mu.Unlock()

	if np != nil {
		if inTuner {
			np.SetVolume((float64(currVol) / 100.0) * rssi)
		} else {
			np.SetVolume(float64(currVol) / 100.0)
		}
	}
	if synth != nil {
		synth.SetVolume(float64(currVol)/100.0, false)
	}
	if ctrl != nil {
		effVol := currVol
		if inTuner {
			effVol = int(float64(currVol) * rssi)
		}
		ctrl.SetVolume(clampVolume(effVol))
	}

	return currVol
}

func (m *Manager) ToggleMute() bool {
	m.mu.Lock()
	m.isMuted = !m.isMuted
	muted := m.isMuted
	vol := m.volume
	np := m.nativePlayer
	synth := m.staticSynth
	ctrl := m.extCtrl
	inTuner := m.tunerMode
	rssi := m.lastTunerRSSI
	m.mu.Unlock()

	if np != nil {
		if muted {
			np.SetVolume(0.0)
		} else if inTuner {
			np.SetVolume((float64(vol) / 100.0) * rssi)
		} else {
			np.SetVolume(float64(vol) / 100.0)
		}
	}
	if synth != nil {
		synth.SetVolume(float64(vol)/100.0, muted)
	}
	if ctrl != nil {
		ctrl.SetMute(muted)
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
	if m.icyStream != nil {
		_ = m.icyStream.Close()
		m.icyStream = nil
	}
	if m.extCtrl != nil {
		_ = m.extCtrl.Close()
		m.extCtrl = nil
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
	m.currentStation = nil
	m.currentTrack = ""
	m.mu.Unlock()
	return nil
}

func (m *Manager) Pause() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status == StatusPlaying || m.status == StatusConnecting {
		if m.cancelFn != nil {
			m.cancelFn()
			m.cancelFn = nil
		}
		if m.icyCancel != nil {
			m.icyCancel()
			m.icyCancel = nil
		}
		if m.icyStream != nil {
			_ = m.icyStream.Close()
			m.icyStream = nil
		}
		if m.nativePlayer != nil {
			m.nativePlayer.Pause()
		}
		if m.extCtrl != nil {
			_ = m.extCtrl.Close()
			m.extCtrl = nil
		}
		if m.cmd != nil && m.cmd.Process != nil {
			_ = m.cmd.Process.Kill()
			m.cmd = nil
		}
		m.status = StatusPaused
	}
	return nil
}

func (m *Manager) Resume() error {
	m.mu.Lock()
	st := m.currentStation
	np := m.nativePlayer
	if np != nil && st != nil {
		np.Play()
		m.status = StatusPlaying
		icyCtx, icyCancel := context.WithCancel(context.Background())
		m.icyCancel = icyCancel
		currentSt := *st
		m.mu.Unlock()
		go m.startICYListener(icyCtx, currentSt)
		return nil
	}
	m.mu.Unlock()

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
	} else if m.tunerMode && m.lastTunerRSSI >= 0 {
		vol = int(float64(vol) * m.lastTunerRSSI)
		if vol < 0 {
			vol = 0
		} else if vol > 100 {
			vol = 100
		}
	}
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancelFn = cancel
	m.mu.Unlock()

	debuglog.Logf("player", "play station=%q backend=%s vol=%d url=%s", st.Name, backend, vol, st.URL)

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

// clampVolume constrains a volume level to mpv/mplayer's 0-100 percentage range.
func clampVolume(vol int) int {
	if vol < 0 {
		return 0
	}
	if vol > 100 {
		return 100
	}
	return vol
}

// buildExternalArgs returns the argv used to launch backend on streamURL at the
// given volume. ipcAddr, when non-empty, is the address mpv should expose its
// JSON IPC channel on for runtime volume/mute control.
func buildExternalArgs(backend string, streamURL string, vol int, ipcAddr string) ([]string, error) {
	switch backend {
	case "mpv":
		// --no-terminal detaches mpv from stdin/stdout entirely, so it can never
		// steal keystrokes from the TUI nor scribble over it. Runtime control
		// arrives over JSON IPC instead, since mpv ignores stdin commands.
		args := []string{"mpv", "--no-video", "--no-terminal", fmt.Sprintf("--volume=%d", vol)}
		if ipcAddr != "" {
			args = append(args, "--input-ipc-server="+ipcAddr)
		}
		return append(args, "--", streamURL), nil

	case "vlc", "cvlc":
		return []string{backend, "-I", "dummy", "--quiet", fmt.Sprintf("--gain=%.2f", float64(vol)/100.0), "--", streamURL}, nil

	case "ffplay":
		return []string{"ffplay", "-nodisp", "-loglevel", "quiet", "-volume", strconv.Itoa(vol), "--", streamURL}, nil

	case "mplayer":
		// -slave turns mplayer's stdin into a real command channel and stops it
		// interpreting terminal keypresses; without it the volume and mute
		// commands written to stdin are ignored.
		return []string{"mplayer", "-quiet", "-slave", "-volume", strconv.Itoa(vol), "--", streamURL}, nil

	case "mpg123":
		return []string{"mpg123", "-q", "-g", strconv.Itoa(vol), "--", streamURL}, nil
	}
	return nil, fmt.Errorf("unknown backend '%s'", backend)
}

func (m *Manager) playExternal(ctx context.Context, backend string, st radio.Station, vol int) {
	if !IsValidStreamURL(st.URL) {
		m.setError(fmt.Sprintf("Invalid or unsupported stream URL '%s'", st.URL))
		return
	}

	// mpv is controlled over JSON IPC; if the endpoint cannot be created we still
	// play, only losing live volume/mute (--volume still applies the level at launch).
	var ipc *mpvIPCEndpoint
	ipcAddr := ""
	if backend == "mpv" {
		if endpoint, err := newMPVIPCEndpoint(); err == nil {
			ipc = endpoint
			ipcAddr = endpoint.Addr
		}
	}

	argv, err := buildExternalArgs(backend, st.URL, vol, ipcAddr)
	if err != nil {
		ipc.Cleanup()
		m.setError(err.Error())
		return
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)

	var ctrl extControl
	switch backend {
	case "mpv":
		if ipc != nil {
			ctrl = newMPVControl(ipc)
		}
	case "mplayer":
		if stdinPipe, err := cmd.StdinPipe(); err == nil {
			ctrl = &mplayerControl{stdin: stdinPipe}
		}
	}

	m.mu.Lock()
	m.cmd = cmd
	m.extCtrl = ctrl
	m.mu.Unlock()

	debuglog.Logf("player", "exec %v", cmd.Args)

	if err := cmd.Start(); err != nil {
		m.mu.Lock()
		if m.extCtrl == ctrl {
			m.extCtrl = nil
		}
		if m.cmd == cmd {
			m.cmd = nil
		}
		m.mu.Unlock()
		if ctrl != nil {
			_ = ctrl.Close()
		} else {
			ipc.Cleanup()
		}
		if ctx.Err() == nil {
			m.setError(fmt.Sprintf("%s start failed: %v", backend, err))
		}
		return
	}

	m.mu.Lock()
	m.status = StatusPlaying
	m.mu.Unlock()

	err = cmd.Wait()
	debuglog.Logf("player", "%s exited: err=%v ctxErr=%v", backend, err, ctx.Err())

	m.mu.Lock()
	if m.cmd == cmd {
		if m.extCtrl == ctrl {
			m.extCtrl = nil
		}
		m.cmd = nil
	}
	if ctx.Err() == nil {
		if err != nil {
			m.status = StatusError
			m.lastError = fmt.Sprintf("%s playback error: %v", backend, err)
		} else if m.status == StatusPlaying {
			m.status = StatusStopped
		}
	}
	m.mu.Unlock()

	if ctrl != nil {
		_ = ctrl.Close()
	}
}

func (m *Manager) setError(errMsg string) {
	debuglog.Logf("player", "error: %s", errMsg)
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

	m.mu.Lock()
	if ctx.Err() != nil || (m.status != StatusPlaying && m.status != StatusConnecting) || m.currentStation == nil || m.currentStation.ID != st.ID {
		m.mu.Unlock()
		_ = resp.Body.Close()
		return
	}
	m.icyStream = resp.Body
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		if m.icyStream == resp.Body {
			m.icyStream = nil
		}
		m.mu.Unlock()
		_ = resp.Body.Close()
	}()

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

		m.mu.Lock()
		if m.status != StatusPlaying && m.status != StatusConnecting || m.currentStation == nil || m.currentStation.ID != st.ID {
			m.mu.Unlock()
			return
		}
		m.mu.Unlock()

		_, err := ioReadFull(reader, buffer)
		if err != nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
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

			select {
			case <-ctx.Done():
				return
			default:
			}

			str := string(metaBuf)
			if idx := strings.Index(str, "StreamTitle='"); idx != -1 {
				str = str[idx+len("StreamTitle='"):]
				if endIdx := strings.Index(str, "';"); endIdx != -1 {
					title := sanitizeTrackTitle(str[:endIdx])
					if title != "" {
						m.mu.Lock()
						if ctx.Err() != nil || (m.status != StatusPlaying && m.status != StatusConnecting) || m.currentStation == nil || m.currentStation.ID != st.ID {
							m.mu.Unlock()
							return
						}
						m.currentTrack = title
						cb := m.onTrackUpd
						m.mu.Unlock()

						if cb != nil {
							cb(TrackInfo{
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

// SetTunerMode enables or disables analog tuner static noise synthesis.
func (m *Manager) SetTunerMode(enabled bool, signalStrength float64, freq float64, band string) {
	m.mu.Lock()
	m.tunerMode = enabled
	m.lastTunerRSSI = signalStrength
	vol := m.volume
	isMuted := m.isMuted
	synth := m.staticSynth
	np := m.nativePlayer
	ctrl := m.extCtrl
	status := m.status
	m.mu.Unlock()

	if synth != nil {
		synth.SetParams(signalStrength, freq, band, float64(vol)/100.0, isMuted)
		synth.SetEnabled(enabled)
	}
	m.startOrStopStaticPlayer(enabled)

	// When leaving tuner mode, restore full master volume on station
	if !enabled && status == StatusPlaying && !isMuted {
		if np != nil {
			np.SetVolume(float64(vol) / 100.0)
		}
		if ctrl != nil {
			ctrl.SetVolume(clampVolume(vol))
		}
	}
}

// UpdateTunerSignal updates the current tuner reception parameters and adjusts static volume.
func (m *Manager) UpdateTunerSignal(signalStrength float64, freq float64, band string) {
	m.mu.Lock()
	m.lastTunerRSSI = signalStrength
	vol := m.volume
	isMuted := m.isMuted
	synth := m.staticSynth
	np := m.nativePlayer
	ctrl := m.extCtrl
	status := m.status
	m.mu.Unlock()

	if synth != nil {
		synth.SetParams(signalStrength, freq, band, float64(vol)/100.0, isMuted)
	}

	// Dynamic volume crossfade: station volume scales with signal strength on both native and external players
	if status == StatusPlaying && !isMuted {
		stationVol := (float64(vol) / 100.0) * signalStrength
		if np != nil {
			np.SetVolume(stationVol)
		}
		if ctrl != nil {
			ctrl.SetVolume(clampVolume(int(stationVol * 100.0)))
		}
	}
}

// IsTunerActive reports whether tuner mode is currently active.
func (m *Manager) IsTunerActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tunerMode
}
