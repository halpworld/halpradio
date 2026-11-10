package player

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/halpworld/halpradio/pkg/debuglog"
	"github.com/halpworld/halpradio/pkg/player/dsp"
	"github.com/halpworld/halpradio/pkg/player/streamnet"
	"github.com/halpworld/halpradio/pkg/player/streamproxy"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/util"
)

type PlayStatus string

const (
	StatusStopped    PlayStatus = "STOPPED"
	StatusConnecting PlayStatus = "CONNECTING"
	StatusPlaying    PlayStatus = "PLAYING"
	StatusPaused     PlayStatus = "PAUSED"
	StatusError      PlayStatus = "ERROR"
)

// DSPSupport describes how the active backend applies the DSP audio rack.
type DSPSupport string

const (
	// DSPLive applies every change to the playing stream immediately.
	DSPLive DSPSupport = "live"
	// DSPNextStation applies changes when the next stream starts.
	DSPNextStation DSPSupport = "next-station"
	// DSPUnavailable means the backend cannot run the rack at all.
	DSPUnavailable DSPSupport = "unavailable"
)

// DSPSupportFor reports how backend handles the DSP rack. The native backend
// runs the pure-Go chain; FFmpeg-based players get an equivalent lavfi graph.
func DSPSupportFor(backend string) DSPSupport {
	switch backend {
	case "native", "mpv":
		return DSPLive
	case "ffplay":
		return DSPNextStation
	}
	return DSPUnavailable
}

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
	Duck(targetRatio float64, fadeDur time.Duration)
	Unduck(fadeDur time.Duration)
	DuckFactor() float64
	SetDSP(s dsp.Settings)
	DSPSettings() dsp.Settings
	DSPSupport() DSPSupport
	DSPMeter() (lufs float64, gainDB float64, ok bool)
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

	duckFactor float64
	duckCancel context.CancelFunc

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

	dspSettings dsp.Settings
	dspChain    *dsp.Chain // native backend's live rack, nil when not playing natively

	// privatePolicy decides per play request whether the stream may reach
	// loopback, LAN or link-local addresses (nil = DefaultPrivateHostPolicy).
	// allowPrivate is that decision for currentStation.
	privatePolicy PrivateHostPolicy
	allowPrivate  bool
	// lookup resolves stream hosts for the external-player pre-check
	// (nil = system resolver); tests replace it.
	lookup streamnet.LookupFunc
	// startProxy starts the stream guard proxy for an external player on a
	// strict station (nil = streamproxy.Start with the strict dialer);
	// tests replace it.
	startProxy func() (*streamproxy.Proxy, error)
}

// maxErrURLRunes caps a URL quoted in a status-line error message.
const maxErrURLRunes = 200

// PrivateHostPolicy reports whether a play request for st may reach
// restricted addresses (loopback, RFC 1918, link-local, metadata services,
// see streamnet.IsPrivateAddr). It should return true only for stations the
// user created or typed themselves, never for directory, catalog or party
// stations.
type PrivateHostPolicy func(st radio.Station) bool

// DefaultPrivateHostPolicy is used until SetPrivateHostPolicy is called. It
// decides from the station's provenance stamp: stations from the user's
// local catalog ("local"/"custom") and unstamped stations (CLI URLs, the
// add-station form before saving) may reach the LAN; bundled, catalog,
// RadioBrowser, SomaFM, party and any other source may not. Remote ingestion
// in pkg/radio always stamps its source, so nothing remote is unstamped.
func DefaultPrivateHostPolicy(st radio.Station) bool {
	return radio.UserSource(st.Source)
}

// privateAllowed applies the current policy to st. The hook is called
// without m.mu held so a slow or re-entrant policy cannot deadlock Play.
func (m *Manager) privateAllowed(st radio.Station) bool {
	m.mu.Lock()
	policy := m.privatePolicy
	m.mu.Unlock()
	if policy == nil {
		policy = DefaultPrivateHostPolicy
	}
	return policy(st)
}

// SetPrivateHostPolicy replaces the private-host policy consulted by Play.
// A nil policy restores DefaultPrivateHostPolicy.
func (m *Manager) SetPrivateHostPolicy(p PrivateHostPolicy) {
	m.mu.Lock()
	m.privatePolicy = p
	m.mu.Unlock()
}

func NewManager(preferredBackend string, initialVolume int, onTrackUpd func(TrackInfo)) *Manager {
	if initialVolume <= 0 || initialVolume > 100 {
		initialVolume = 80
	}
	m := &Manager{
		status:        StatusStopped,
		volume:        initialVolume,
		duckFactor:    1.0,
		onTrackUpd:    onTrackUpd,
		activeBackend: detectBackend(preferredBackend),
		staticSynth:   NewStaticSynthesizer(44100),
		dspSettings:   dsp.DefaultSettings(),
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

// effectiveGainLocked returns the live station gain in [0.0, 1.0] combining
// master volume, duckFactor, and (when in tuner mode) lastTunerRSSI.
// Caller must hold m.mu.
func (m *Manager) effectiveGainLocked() float64 {
	gain := (float64(m.volume) / 100.0) * m.duckFactor
	if m.tunerMode && m.lastTunerRSSI >= 0 {
		gain *= m.lastTunerRSSI
	}
	if gain < 0 {
		gain = 0
	}
	if gain > 1.0 {
		gain = 1.0
	}
	return gain
}

// applyLiveVolume pushes the current effective volume (including duckFactor and
// tuner RSSI) to active native and external player backends.
func (m *Manager) applyLiveVolume() {
	m.mu.Lock()
	isMuted := m.isMuted
	gain := m.effectiveGainLocked()
	np := m.nativePlayer
	ctrl := m.extCtrl
	m.mu.Unlock()

	if np != nil {
		if isMuted {
			np.SetVolume(0.0)
		} else {
			np.SetVolume(gain)
		}
	}
	if ctrl != nil && !isMuted {
		ctrl.SetVolume(clampVolume(int(math.Round(gain * 100.0))))
	}
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
	gain := m.effectiveGainLocked()
	np := m.nativePlayer
	synth := m.staticSynth
	ctrl := m.extCtrl
	m.mu.Unlock()

	if np != nil {
		np.SetVolume(gain)
	}
	if synth != nil {
		synth.SetVolume(float64(currVol)/100.0, false)
	}
	if ctrl != nil {
		ctrl.SetVolume(clampVolume(int(math.Round(gain * 100.0))))
	}

	return currVol
}

func (m *Manager) ToggleMute() bool {
	m.mu.Lock()
	m.isMuted = !m.isMuted
	muted := m.isMuted
	vol := m.volume
	gain := m.effectiveGainLocked()
	np := m.nativePlayer
	synth := m.staticSynth
	ctrl := m.extCtrl
	m.mu.Unlock()

	if np != nil {
		if muted {
			np.SetVolume(0.0)
		} else {
			np.SetVolume(gain)
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

// Duck smoothly interpolates duckFactor from its current value to targetRatio
// over fadeDur without altering the user's master volume.
func (m *Manager) Duck(targetRatio float64, fadeDur time.Duration) {
	if targetRatio < 0 {
		targetRatio = 0
	}
	if targetRatio > 1.0 {
		targetRatio = 1.0
	}

	m.mu.Lock()
	if m.duckCancel != nil {
		m.duckCancel()
		m.duckCancel = nil
	}
	startFactor := m.duckFactor
	if fadeDur <= 0 || math.Abs(startFactor-targetRatio) < 0.001 {
		m.duckFactor = targetRatio
		m.mu.Unlock()
		m.applyLiveVolume()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.duckCancel = cancel
	m.mu.Unlock()

	tickInterval := 25 * time.Millisecond
	if fadeDur < tickInterval*2 {
		tickInterval = fadeDur / 4
		if tickInterval < 2*time.Millisecond {
			tickInterval = 2 * time.Millisecond
		}
	}

	go func() {
		startTime := time.Now()
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				elapsed := now.Sub(startTime)
				if elapsed >= fadeDur {
					m.mu.Lock()
					if ctx.Err() == nil {
						m.duckFactor = targetRatio
					}
					m.mu.Unlock()
					if ctx.Err() == nil {
						m.applyLiveVolume()
					}
					return
				}
				progress := float64(elapsed) / float64(fadeDur)
				curr := startFactor + (targetRatio-startFactor)*progress
				m.mu.Lock()
				if ctx.Err() == nil {
					m.duckFactor = curr
				}
				m.mu.Unlock()
				if ctx.Err() == nil {
					m.applyLiveVolume()
				}
			}
		}
	}()
}

// Unduck smoothly interpolates duckFactor back to 1.0 (full volume) over fadeDur.
func (m *Manager) Unduck(fadeDur time.Duration) {
	m.Duck(1.0, fadeDur)
}

// DuckFactor returns the current live audio ducking multiplier (1.0 = unducked).
func (m *Manager) DuckFactor() float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.duckFactor
}

func (m *Manager) Stop() error {
	m.mu.Lock()
	if m.duckCancel != nil {
		m.duckCancel()
		m.duckCancel = nil
	}
	m.duckFactor = 1.0
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
	m.dspChain = nil
	m.status = StatusStopped
	m.currentStation = nil
	m.currentTrack = ""
	m.mu.Unlock()
	return nil
}

func (m *Manager) Pause() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.duckCancel != nil {
		m.duckCancel()
		m.duckCancel = nil
	}
	m.duckFactor = 1.0
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
		allow := m.allowPrivate
		m.mu.Unlock()
		go m.startICYListener(icyCtx, currentSt, allow)
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

	// Validate and use the same string: IsValidStreamURL trims, so the URL
	// handed to the backends must be trimmed too.
	st.URL = strings.TrimSpace(st.URL)

	if !IsValidStreamURL(st.URL) {
		m.setError(fmt.Sprintf("Invalid stream URL '%s' (only http/https supported)", util.SanitizeDisplay(debuglog.RedactURL(st.URL), maxErrURLRunes)))
		return nil
	}

	allowPrivate := m.privateAllowed(st)
	if !allowPrivate {
		if u, err := url.Parse(st.URL); err != nil || streamnet.IsRestrictedHostName(u.Hostname()) {
			m.setError("Station refused: its stream host is a local or private address")
			return nil
		}
	}

	m.mu.Lock()
	m.allowPrivate = allowPrivate
	m.currentStation = &st
	m.status = StatusConnecting
	m.currentTrack = st.Name
	m.lastError = ""
	backend := m.activeBackend
	vol := 0
	if !m.isMuted {
		vol = clampVolume(int(math.Round(m.effectiveGainLocked() * 100.0)))
	}
	m.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.cancelFn = cancel
	m.mu.Unlock()

	debuglog.Logf("player", "play station=%q backend=%s vol=%d url=%s", st.Name, backend, vol, debuglog.RedactURL(st.URL))

	if backend == "native" {
		go m.playNative(ctx, st, vol, allowPrivate)
	} else {
		go m.playExternal(ctx, backend, st, vol, allowPrivate)
	}

	// Start ICY Stream Title reader in background
	icyCtx, icyCancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.icyCancel = icyCancel
	m.mu.Unlock()
	go m.startICYListener(icyCtx, st, allowPrivate)

	return nil
}

// SetDSP replaces the DSP rack settings. The native backend and mpv apply the
// change to the playing stream at once; ffplay picks it up on the next stream.
func (m *Manager) SetDSP(s dsp.Settings) {
	s = s.Normalize()
	m.mu.Lock()
	m.dspSettings = s.Clone()
	chain := m.dspChain
	ctrl := m.extCtrl
	m.mu.Unlock()

	if chain != nil {
		chain.Update(s)
	}
	if ctrl != nil {
		ctrl.SetAudioFilter(dsp.MPVAudioFilter(s))
	}
	debuglog.Logf("player", "dsp settings: %s", s.Summary())
}

// DSPSettings returns a copy of the current DSP rack settings.
func (m *Manager) DSPSettings() dsp.Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.dspSettings.Clone()
}

// DSPSupport reports how the active backend applies the DSP rack.
func (m *Manager) DSPSupport() DSPSupport {
	m.mu.Lock()
	defer m.mu.Unlock()
	return DSPSupportFor(m.activeBackend)
}

// DSPMeter reports the native normalizer's measured loudness and applied gain.
// ok is false for external backends, which do their own metering.
func (m *Manager) DSPMeter() (lufs float64, gainDB float64, ok bool) {
	m.mu.Lock()
	chain := m.dspChain
	m.mu.Unlock()
	if chain == nil {
		return 0, 0, false
	}
	return chain.Loudness()
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

// lavfProtocols is the libavformat protocol whitelist for mpv, ffplay and
// ffmpeg: network streaming only (no file:, pipe:, gopher:, rtmp:, data:
// ...). tcp and tls carry http and https; httpproxy is how https goes
// through the stream guard proxy. Bare tcp:// playlist entries are not
// opened by mpv because of --load-unsafe-playlists=no, and HLS segments
// must be http(s).
const lavfProtocols = "http,https,tcp,tls,crypto,httpproxy"

// proxyEnvNames are the proxy variables cleared from a guarded child's
// environment before http_proxy is pointed at the stream guard proxy, so an
// inherited no_proxy (honoured by libavformat even with an explicit proxy)
// or all_proxy cannot route around it.
var proxyEnvNames = []string{
	"http_proxy", "HTTP_PROXY", "https_proxy", "HTTPS_PROXY",
	"all_proxy", "ALL_PROXY", "no_proxy", "NO_PROXY", "ftp_proxy", "FTP_PROXY",
}

// guardedEnv returns base without any proxy variable, plus http_proxy and
// https_proxy (both spellings) set to proxyURL.
func guardedEnv(base []string, proxyURL string) []string {
	out := make([]string, 0, len(base)+4)
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		drop := false
		for _, p := range proxyEnvNames {
			if name == p {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return append(out,
		"http_proxy="+proxyURL, "HTTP_PROXY="+proxyURL,
		"https_proxy="+proxyURL, "HTTPS_PROXY="+proxyURL)
}

// buildExternalArgs returns the argv used to launch backend on streamURL at the
// given volume, without the stream guard proxy (see buildExternalCommand).
func buildExternalArgs(backend string, streamURL string, vol int, ipcAddr string, fx dsp.Settings) ([]string, error) {
	args, _, err := buildExternalCommand(backend, streamURL, vol, ipcAddr, fx, "", nil)
	return args, err
}

// buildExternalCommand returns the argv and environment used to launch
// backend on streamURL at the given volume. ipcAddr, when non-empty, is the
// address mpv should expose its JSON IPC channel on for runtime
// volume/mute/filter control. fx is rendered as an FFmpeg filter graph for
// the backends that accept one.
//
// proxyURL, when non-empty, is the stream guard proxy (with its session
// credential) that every request of the player must go through; env is then
// baseEnv with the proxy variables replaced (see guardedEnv). The proxy is
// handed over in the environment rather than argv so the credential is not
// visible to other users in the process list; VLC also gets it as an option
// because its environment handling is unverified. With proxyURL empty env
// is nil and the child inherits halpradio's environment.
func buildExternalCommand(backend, streamURL string, vol int, ipcAddr string, fx dsp.Settings, proxyURL string, baseEnv []string) (args []string, env []string, err error) {
	if proxyURL != "" {
		env = guardedEnv(baseEnv, proxyURL)
	}
	switch backend {
	case "mpv":
		// --no-terminal detaches mpv from stdin/stdout entirely, so it can never
		// steal keystrokes from the TUI nor scribble over it. Runtime control
		// arrives over JSON IPC instead, since mpv ignores stdin commands.
		//
		// Station URLs are untrusted, so mpv must only play them: no youtube-dl /
		// yt-dlp hook (which would run a helper program on any page URL), no
		// unsafe playlist entries, no sidecar files fetched next to the URL,
		// and libavformat limited to network streaming protocols. mpv reads
		// the proxy from http_proxy (libavformat), for https too (CONNECT).
		args = []string{"mpv", "--no-video", "--no-terminal",
			"--ytdl=no", "--load-unsafe-playlists=no",
			"--sub-auto=no", "--audio-file-auto=no", "--cover-art-auto=no",
			"--stream-lavf-o=protocol_whitelist=[" + lavfProtocols + "]",
			"--demuxer-lavf-o=protocol_whitelist=[" + lavfProtocols + "]",
			fmt.Sprintf("--volume=%d", vol)}
		if ipcAddr != "" {
			args = append(args, "--input-ipc-server="+ipcAddr)
		}
		if af := dsp.MPVAudioFilter(fx); af != "" {
			args = append(args, "--af="+af)
		}
		return append(args, "--", streamURL), env, nil

	case "vlc", "cvlc":
		args = []string{backend, "-I", "dummy", "--quiet", fmt.Sprintf("--gain=%.2f", float64(vol)/100.0)}
		if proxyURL != "" {
			// Unverified: VLC is not available on the development machine.
			args = append(args, "--http-proxy="+proxyURL)
		}
		return append(args, "--", streamURL), env, nil

	case "ffplay":
		args = []string{"ffplay", "-nodisp", "-loglevel", "quiet",
			"-protocol_whitelist", lavfProtocols,
			"-volume", strconv.Itoa(vol)}
		if graph := dsp.LavfiGraph(fx); graph != "" {
			args = append(args, "-af", graph)
		}
		return append(args, "--", streamURL), env, nil

	case "mplayer":
		// -slave turns mplayer's stdin into a real command channel and stops it
		// interpreting terminal keypresses; without it the volume and mute
		// commands written to stdin are ignored. Proxy via http_proxy:
		// unverified (mplayer is not available on the development machine).
		return []string{"mplayer", "-quiet", "-slave", "-volume", strconv.Itoa(vol), "--", streamURL}, env, nil

	case "mpg123":
		// mpg123 reads http_proxy, but its internal HTTP client never sends
		// the proxy credential (verified with 1.33.7), so the guard proxy
		// would answer 407. Behind the proxy it hands every request to the
		// curl helper instead (--network curl), which sends the credential
		// from http_proxy/https_proxy. mpg123 still handles redirects and
		// playlist entries itself (curl runs without -L), so each hop is a
		// new request through the proxy. If curl is missing, playback fails
		// rather than going direct.
		args = []string{"mpg123", "-q", "-g", strconv.Itoa(vol)}
		if proxyURL != "" {
			args = append(args, "--network", "curl")
		}
		return append(args, "--", streamURL), env, nil
	}
	return nil, nil, fmt.Errorf("unknown backend '%s'", backend)
}

func (m *Manager) playExternal(ctx context.Context, backend string, st radio.Station, vol int, allowPrivate bool) {
	if !IsValidStreamURL(st.URL) {
		m.setError(fmt.Sprintf("Invalid or unsupported stream URL '%s'", util.SanitizeDisplay(debuglog.RedactURL(st.URL), maxErrURLRunes)))
		return
	}

	// External players resolve and follow redirects on their own. For
	// stations that may not reach private addresses, refuse early if the
	// host resolves to a restricted address (a clear message), then route
	// every request of the player through the stream guard proxy, which
	// dials with the strict streamnet dialer: redirects, playlist entries,
	// HLS segments, https CONNECTs and later re-resolutions are all checked.
	proxyURL := ""
	if !allowPrivate {
		m.mu.Lock()
		lookup := m.lookup
		m.mu.Unlock()
		checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := streamnet.CheckHostPublic(checkCtx, st.URL, lookup)
		cancel()
		if err != nil {
			if ctx.Err() == nil {
				m.setError("Station refused: its stream host is not a public address")
			}
			debuglog.Logf("player", "external pre-check refused: %s", debuglog.RedactText(err.Error()))
			return
		}
		if ctx.Err() != nil {
			return
		}
		m.mu.Lock()
		start := m.startProxy
		m.mu.Unlock()
		if start == nil {
			start = func() (*streamproxy.Proxy, error) { return streamproxy.Start(streamproxy.Options{}) }
		}
		px, err := start()
		if err != nil {
			// Fail closed: never fall back to a direct connection.
			m.setError("Station refused: the stream guard proxy could not start")
			debuglog.Logf("player", "stream guard proxy: %s", debuglog.RedactText(err.Error()))
			return
		}
		defer px.Close() // after the player exits (Stop cancels ctx, which kills it)
		proxyURL = px.URL()
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

	m.mu.Lock()
	fx := m.dspSettings.Clone()
	m.mu.Unlock()

	var baseEnv []string
	if proxyURL != "" {
		baseEnv = os.Environ()
	}
	argv, env, err := buildExternalCommand(backend, st.URL, vol, ipcAddr, fx, proxyURL, baseEnv)
	if err != nil {
		ipc.Cleanup()
		m.setError(err.Error())
		return
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = env // nil: inherit (user stations only)

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

	debuglog.Logf("player", "exec %v", debuglog.RedactArgs(cmd.Args))

	if err := cmd.Start(); err != nil {
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
	if ctx.Err() != nil {
		m.mu.Unlock()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		if ctrl != nil {
			_ = ctrl.Close()
		} else {
			ipc.Cleanup()
		}
		return
	}
	m.cmd = cmd
	m.extCtrl = ctrl
	m.mu.Unlock()

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
	debuglog.Logf("player", "error: %s", debuglog.RedactText(errMsg))
	m.mu.Lock()
	m.status = StatusError
	m.lastError = errMsg
	m.mu.Unlock()
}

// maxIcyMetaInt defines a safe upper bound (256 KB) for ICY metadata intervals to prevent OOM denial-of-service.
const maxIcyMetaInt = 262144

// startICYListener connects to the stream with ICY header to extract real-time song title
func (m *Manager) startICYListener(ctx context.Context, st radio.Station, allowPrivate bool) {
	// ICY metadata is untrusted input; a parsing bug must not crash the app.
	defer func() {
		if r := recover(); r != nil {
			debuglog.Logf("player", "icy listener recovered: %v", r)
		}
	}()
	if !IsValidStreamURL(st.URL) {
		return
	}

	req, err := http.NewRequestWithContext(ctx, "GET", st.URL, nil)
	if err != nil {
		return
	}
	req.Header.Set("Icy-MetaData", "1")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko)")

	// Same redirect/destination policy as the native player.
	client := streamnet.NewClient(streamnet.Options{AllowPrivate: allowPrivate})
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
					rawCleaned := sanitizeTrackTitle(str[:endIdx])
					san := radio.SanitizeTrackTitle(rawCleaned, st.Name)
					if san.IsClean && san.CleanTitle != "" {
						title := san.CleanTitle
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
	gain := m.effectiveGainLocked()
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

	// Restore or apply live effective volume on station
	if !enabled && status == StatusPlaying && !isMuted {
		if np != nil {
			np.SetVolume(gain)
		}
		if ctrl != nil {
			ctrl.SetVolume(clampVolume(int(math.Round(gain * 100.0))))
		}
	}
}

// UpdateTunerSignal updates the current tuner reception parameters and adjusts static volume.
func (m *Manager) UpdateTunerSignal(signalStrength float64, freq float64, band string) {
	m.mu.Lock()
	m.lastTunerRSSI = signalStrength
	vol := m.volume
	duck := m.duckFactor
	isMuted := m.isMuted
	synth := m.staticSynth
	np := m.nativePlayer
	ctrl := m.extCtrl
	status := m.status
	m.mu.Unlock()

	if synth != nil {
		synth.SetParams(signalStrength, freq, band, float64(vol)/100.0, isMuted)
	}

	// Dynamic volume crossfade: station volume scales with signal strength and duckFactor on both native and external players
	if status == StatusPlaying && !isMuted {
		stationVol := (float64(vol) / 100.0) * duck * signalStrength
		if np != nil {
			np.SetVolume(stationVol)
		}
		if ctrl != nil {
			ctrl.SetVolume(clampVolume(int(math.Round(stationVol * 100.0))))
		}
	}
}

// IsTunerActive reports whether tuner mode is currently active.
func (m *Manager) IsTunerActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.tunerMode
}
