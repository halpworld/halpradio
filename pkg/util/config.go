package util

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Volume               int     `yaml:"volume"`
	PlayerBackend        string  `yaml:"player_backend"`
	LoudnessTargetLUFS   float64 `yaml:"loudness_target_lufs"`
	Theme                string  `yaml:"theme"`
	VisualizerMode       string  `yaml:"visualizer_mode"`
	LastStationID        string  `yaml:"last_station_id"`
	SearchProvider       string  `yaml:"search_provider,omitempty"`
	PomodoroFocusMin     int     `yaml:"pomodoro_focus_min,omitempty"`
	PomodoroShortBreak   int     `yaml:"pomodoro_short_break_min,omitempty"`
	PomodoroLongBreak    int     `yaml:"pomodoro_long_break_min,omitempty"`
	PomodoroCycles       int     `yaml:"pomodoro_cycles,omitempty"`
	PomodoroFocusStation string  `yaml:"pomodoro_focus_station,omitempty"`
	PomodoroBreakStation string  `yaml:"pomodoro_break_station,omitempty"`
	SleepFadeSeconds     int     `yaml:"sleep_fade_seconds,omitempty"`
	EventNotifyDesktop   bool    `yaml:"event_notify_desktop"`
	EventTerminalBell    bool    `yaml:"event_terminal_bell"`
	EventCommandHook     string  `yaml:"event_command_hook,omitempty"`
	SongNotifications    bool    `yaml:"song_notifications"`
	AutoPause            bool    `yaml:"autopause"`
	MPRISEnabled         bool    `yaml:"mpris_enabled"`
	IPCEnabled           bool    `yaml:"ipc_enabled"`
	DiscordRPC           bool    `yaml:"discord_rpc"`
	DiscordClientID      string  `yaml:"discord_client_id,omitempty"`
	PluginsEnabled       bool    `yaml:"plugins_enabled"`
	PluginRegistryURL    string  `yaml:"plugin_registry_url,omitempty"`
	ThemeRegistryURL     string  `yaml:"theme_registry_url,omitempty"`
	CatalogAutoUpdate    bool    `yaml:"catalog_auto_update"`
	CatalogUpdateURL     string  `yaml:"catalog_update_url,omitempty"`
	CatalogCacheTTLHours int     `yaml:"catalog_cache_ttl_hours,omitempty"`
	ExperimentalTuner    bool    `yaml:"experimental_tuner,omitempty"`
	FingerprintEnabled   bool    `yaml:"fingerprint_enabled"`
	AcoustidAPIKey       string  `yaml:"acoustid_api_key,omitempty"`
	AutoIdentify         bool    `yaml:"auto_identify"`
	LyricsEnabled        bool    `yaml:"lyrics_enabled"`
	LyricsAutoOpen       bool    `yaml:"lyrics_auto_open"`
	LyricsOffsetMs       int     `yaml:"lyrics_offset_ms,omitempty"`
	AlbumArtEnabled      bool    `yaml:"album_art_enabled"`
	AlbumArtProtocol     string  `yaml:"album_art_protocol,omitempty"`
	// Per-provider opt-outs for third-party lookups. They default to
	// enabled; a missing key keeps the default because LoadConfig starts
	// from DefaultConfig before decoding.
	LyricsNetEaseEnabled  bool   `yaml:"lyrics_netease_enabled"`
	ArtITunesEnabled      bool   `yaml:"album_art_itunes_enabled"`
	ArtDeezerEnabled      bool   `yaml:"album_art_deezer_enabled"`
	ArtMusicBrainzEnabled bool   `yaml:"album_art_musicbrainz_enabled"`
	LastFMAPIKey          string `yaml:"lastfm_api_key,omitempty"`
	PartyNickname         string `yaml:"party_nickname,omitempty"`
	// Deprecated: unused since party protocol v2 (no relay).
	PartyRelayURL   string `yaml:"party_relay_url,omitempty"`
	PartyPort       int    `yaml:"party_port,omitempty"`
	AIDJEnabled     bool   `yaml:"ai_dj"`
	AIDJPersona     string `yaml:"ai_dj_persona,omitempty"`
	AIDJTTSEngine   string `yaml:"ai_dj_tts_engine,omitempty"`
	AIDJVoiceModel  string `yaml:"ai_dj_voice_model,omitempty"`
	AIDJOllamaURL   string `yaml:"ai_dj_ollama_url,omitempty"`
	AIDJOllamaModel string `yaml:"ai_dj_ollama_model,omitempty"`
	AIDJDuckLevel   int    `yaml:"ai_dj_duck_level,omitempty"`

	// loadErr records why an existing config.yaml could not be read. It is
	// never serialised; SaveConfig refuses a config that carries it.
	loadErr error
}

// ErrConfigUnreadable is returned by SaveConfig when config.yaml exists but
// could not be read, so saving would overwrite the user's settings.
var ErrConfigUnreadable = errors.New("config.yaml could not be read")

// LoadError reports why the existing config.yaml could not be read, or nil
// (also for a missing file, which is the normal first-run default).
func (c Config) LoadError() error { return c.loadErr }

// LoadWarning is the user-facing warning for a config that could not be
// read, or "" when it loaded normally.
func (c Config) LoadWarning() string {
	if c.loadErr == nil {
		return ""
	}
	return "config.yaml could not be read: " + SanitizeDisplay(c.loadErr.Error(), 200) +
		". Plugins and network extras are off until it is fixed."
}

// failClosedConfig is the config used when config.yaml exists but cannot be
// read: the defaults, with every safety switch the user may have turned off
// kept off, so a typo never silently re-enables plugins or third parties.
func failClosedConfig(err error) Config {
	cfg := DefaultConfig()
	cfg.PluginsEnabled = false
	cfg.CatalogAutoUpdate = false
	cfg.LyricsNetEaseEnabled = false
	cfg.ArtITunesEnabled = false
	cfg.ArtDeezerEnabled = false
	cfg.ArtMusicBrainzEnabled = false
	cfg.EventCommandHook = ""
	cfg.loadErr = err
	return cfg
}

func DefaultConfig() Config {
	return Config{
		Volume:                80,
		PlayerBackend:         "auto",
		LoudnessTargetLUFS:    -14,
		Theme:                 "tokyonight",
		VisualizerMode:        "dj-cat",
		LastStationID:         "",
		SearchProvider:        "spotify",
		PomodoroFocusMin:      25,
		PomodoroShortBreak:    5,
		PomodoroLongBreak:     15,
		PomodoroCycles:        4,
		PomodoroFocusStation:  "",
		PomodoroBreakStation:  "",
		SleepFadeSeconds:      10,
		EventNotifyDesktop:    true,
		EventTerminalBell:     true,
		EventCommandHook:      "",
		SongNotifications:     true,
		AutoPause:             true,
		MPRISEnabled:          true,
		IPCEnabled:            true,
		DiscordRPC:            true,
		DiscordClientID:       "1340000000000000000",
		PluginsEnabled:        true,
		PluginRegistryURL:     "https://raw.githubusercontent.com/halpworld/halpradio-plugins/main/registry.json",
		ThemeRegistryURL:      "https://raw.githubusercontent.com/halpworld/halpradio-themes/main/themes.json",
		CatalogAutoUpdate:     true,
		CatalogUpdateURL:      "https://raw.githubusercontent.com/halpworld/halpradio/main/stations.yaml",
		CatalogCacheTTLHours:  24,
		ExperimentalTuner:     false,
		FingerprintEnabled:    true,
		AcoustidAPIKey:        "v8pQ6oyB",
		AutoIdentify:          true,
		LyricsEnabled:         true,
		LyricsAutoOpen:        false,
		LyricsOffsetMs:        0,
		AlbumArtEnabled:       true,
		AlbumArtProtocol:      "auto",
		LyricsNetEaseEnabled:  true,
		ArtITunesEnabled:      true,
		ArtDeezerEnabled:      true,
		ArtMusicBrainzEnabled: true,
		LastFMAPIKey:          "",
		PartyNickname:         "",
		PartyPort:             0,
		AIDJEnabled:           false,
		AIDJPersona:           "late-night-chill",
		AIDJTTSEngine:         "auto",
		AIDJVoiceModel:        "en_US-lessac-medium",
		AIDJOllamaURL:         "http://127.0.0.1:11434",
		AIDJOllamaModel:       "",
		AIDJDuckLevel:         20,
	}
}

// GetConfigDir returns the directory path where user settings & local stations live.
//
// The result is always an absolute path. When neither the OS config directory
// nor the home directory can be determined (for example HOME is unset), it
// falls back to a private per-user directory under the system temp directory
// (see privateFallbackDir) and never to a path relative to the working
// directory: a relative fallback would let whoever controls the current
// directory supply config.yaml, including event_command_hook.
func GetConfigDir() string {
	if configDir, err := os.UserConfigDir(); err == nil && filepath.IsAbs(configDir) {
		return filepath.Join(configDir, "halpradio")
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.IsAbs(home) {
		return filepath.Join(home, ".config", "halpradio")
	}
	return filepath.Join(privateFallbackDir(), "config")
}

var (
	fallbackDirMu sync.Mutex
	fallbackDir   string
)

// privateFallbackDir returns an absolute directory that only the current user
// can write, for use when no home directory is known. It prefers the stable
// "<tmp>/halpradio-<uid>" so settings survive restarts, but only after
// checking that it is a real directory (not a symlink) owned by this user with
// mode 0700. If that check fails (another user planted it) or cannot be made
// (no uid concept, as on Windows), a fresh randomly named 0700 directory is
// created for this process instead. The result is cached for the process.
func privateFallbackDir() string {
	fallbackDirMu.Lock()
	defer fallbackDirMu.Unlock()
	if fallbackDir != "" {
		return fallbackDir
	}

	tmp := os.TempDir()
	if !filepath.IsAbs(tmp) {
		if runtime.GOOS == "windows" {
			tmp = filepath.Join(os.Getenv("SystemRoot"), "Temp")
		} else {
			tmp = "/tmp"
		}
	}

	if uid := os.Getuid(); uid >= 0 {
		stable := filepath.Join(tmp, fmt.Sprintf("halpradio-%d", uid))
		if err := os.Mkdir(stable, 0o700); err == nil || os.IsExist(err) {
			if isPrivateDir(stable) {
				fallbackDir = stable
				return fallbackDir
			}
		}
	}

	if d, err := os.MkdirTemp(tmp, "halpradio-"); err == nil && filepath.IsAbs(d) {
		fallbackDir = d
		return fallbackDir
	}

	// Nothing writable: return an unpredictable path that does not exist, so
	// reads find nothing and writes fail instead of landing somewhere shared.
	var rnd [12]byte
	_, _ = io.ReadFull(rand.Reader, rnd[:])
	fallbackDir = filepath.Join(tmp, "halpradio-unavailable-"+hex.EncodeToString(rnd[:]))
	return fallbackDir
}

// isPrivateDir reports whether path is a directory (not a symlink) owned by
// the current user with no group/other permission bits. It returns false when
// ownership cannot be determined.
func isPrivateDir(path string) bool {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	owner, ok := fileOwnerUID(info)
	if !ok || owner != os.Getuid() {
		return false
	}
	return info.Mode().Perm()&0o077 == 0
}

// fileOwnerUID returns the owning uid recorded in info.Sys() on platforms
// whose stat structure has a Uid field (all Unix variants). It reports false
// elsewhere (Windows), so callers fail closed.
func fileOwnerUID(info os.FileInfo) (int, bool) {
	if info == nil || info.Sys() == nil {
		return 0, false
	}
	v := reflect.ValueOf(info.Sys())
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return 0, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, false
	}
	f := v.FieldByName("Uid")
	if !f.IsValid() {
		return 0, false
	}
	switch f.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return int(f.Uint()), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(f.Int()), true
	}
	return 0, false
}

// tightenPerms removes group/other permission bits from path when it is owned
// by the current user (config.yaml holds API keys and a command hook). Paths
// owned by someone else, and platforms without uids, are left alone.
func tightenPerms(path string, perm os.FileMode) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	if owner, ok := fileOwnerUID(info); !ok || owner != os.Getuid() {
		return
	}
	if info.Mode().Perm()&0o077 != 0 {
		_ = os.Chmod(path, perm)
	}
}

// EnsureConfigDir creates the configuration directory if it doesn't exist and
// tightens an existing one to 0700.
func EnsureConfigDir() error {
	dir := GetConfigDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tightenPerms(dir, 0o700)
	return os.MkdirAll(GetThemesDir(), 0700)
}

// WriteFileAtomic writes data to path through a temporary file in the same
// directory that is created with perm, synced and then renamed over path, so a
// crash never leaves a truncated file and the content is never readable with
// looser permissions, even briefly.
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	fail := func(err error) error {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Chmod(perm); err != nil && runtime.GOOS != "windows" {
		return fail(err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Sync(); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return nil
}

func GetThemesDir() string {
	return filepath.Join(GetConfigDir(), "themes")
}

func GetLocalStationsFile() string {
	return filepath.Join(GetConfigDir(), "stations.yaml")
}

func GetCatalogCacheFile() string {
	return filepath.Join(GetConfigDir(), "catalog_cache.yaml")
}

func GetCatalogMetadataFile() string {
	return filepath.Join(GetConfigDir(), "catalog_metadata.json")
}

func GetFavoritesFile() string {
	return filepath.Join(GetConfigDir(), "favorites.json")
}

// GetDSPFile returns the path the equalizer & DSP rack settings persist to.
func GetDSPFile() string {
	return filepath.Join(GetConfigDir(), "dsp.yaml")
}

func GetSavedTracksFile() string {
	return filepath.Join(GetConfigDir(), "saved_tracks.txt")
}

// GetDebugLogFile returns the path diagnostic logs are written to when
// halpradio is started with --debug (or HALPRADIO_DEBUG set).
func GetDebugLogFile() string {
	return filepath.Join(GetConfigDir(), "debug.log")
}

func GetConfigFile() string {
	return filepath.Join(GetConfigDir(), "config.yaml")
}

// GetCacheDir returns the directory used for large regenerable downloads such
// as album artwork and lyric sheets. It is deliberately separate from the
// config directory so users can delete it without losing their settings.
//
// Like GetConfigDir it always returns an absolute path: the TTS runtime that
// is executed from GetTTSModelsDir must never resolve relative to the working
// directory.
func GetCacheDir() string {
	if cacheDir, err := os.UserCacheDir(); err == nil && filepath.IsAbs(cacheDir) {
		return filepath.Join(cacheDir, "halpradio")
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.IsAbs(home) {
		return filepath.Join(home, ".cache", "halpradio")
	}
	return filepath.Join(privateFallbackDir(), "cache")
}

// GetLyricsCacheDir returns the on-disk cache location for lyric sheets.
func GetLyricsCacheDir() string {
	return filepath.Join(GetCacheDir(), "lyrics")
}

// GetAlbumArtCacheDir returns the on-disk cache location for cover artwork.
func GetAlbumArtCacheDir() string {
	return filepath.Join(GetCacheDir(), "art")
}

// GetAICacheDir returns the base on-disk cache location for AI DJ and liner notes data.
func GetAICacheDir() string {
	return filepath.Join(GetCacheDir(), "ai")
}

// GetTTSModelsDir returns the on-disk cache location for opt-in ONNX voice models.
func GetTTSModelsDir() string {
	return filepath.Join(GetAICacheDir(), "models")
}

// GetLinerNotesCacheDir returns the on-disk cache location for artist/track liner notes.
func GetLinerNotesCacheDir() string {
	return filepath.Join(GetAICacheDir(), "linernotes")
}

// EnsureCacheDir creates the lyrics, artwork, and liner notes cache directories.
// A failure here is never fatal: the callers fall back to network-only operation.
func EnsureCacheDir() error {
	if err := os.MkdirAll(GetLyricsCacheDir(), 0700); err != nil {
		return err
	}
	if err := os.MkdirAll(GetAlbumArtCacheDir(), 0700); err != nil {
		return err
	}
	return os.MkdirAll(GetLinerNotesCacheDir(), 0700)
}

func GetPluginsDir() string {
	return filepath.Join(GetConfigDir(), "plugins")
}

func GetPluginsDataDir() string {
	return filepath.Join(GetConfigDir(), "plugins_data")
}

func GetPluginsConfigFile() string {
	return filepath.Join(GetConfigDir(), "plugins.json")
}

// LoadConfig reads config.yaml if present, or returns DefaultConfig.
func LoadConfig() (Config, error) {
	cfg := DefaultConfig()
	filePath := GetConfigFile()

	f, err := os.Open(filePath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return cfg, err // first run: plain defaults
		}
		return failClosedConfig(err), err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return failClosedConfig(err), err
	}
	if !info.Mode().IsRegular() {
		err := fmt.Errorf("config %s is not a regular file", filePath)
		return failClosedConfig(err), err
	}
	data, err := io.ReadAll(io.LimitReader(f, maxConfigFileBytes+1))
	if err != nil {
		return failClosedConfig(err), err
	}
	if len(data) > maxConfigFileBytes {
		err := fmt.Errorf("config %s exceeds %d bytes", filePath, maxConfigFileBytes)
		return failClosedConfig(err), err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return failClosedConfig(err), err
	}

	// event_command_hook runs a shell command. Only honour it from a file the
	// current user owns; on platforms where ownership cannot be read (Windows)
	// the file lives in the per-user profile directory.
	owner, ownerKnown := fileOwnerUID(info)
	if ownerKnown && owner != os.Getuid() {
		cfg.EventCommandHook = ""
	} else if ownerKnown {
		// config.yaml holds API keys and the hook: tighten files written by
		// older versions (0644) and a 0755 config directory.
		if info.Mode().Perm()&0o077 != 0 {
			_ = f.Chmod(0o600)
		}
		tightenPerms(filepath.Dir(filePath), 0o700)
	}

	// Apply default fallbacks for unset numeric fields
	if cfg.Volume <= 0 {
		cfg.Volume = 80
	}
	if cfg.PomodoroFocusMin <= 0 {
		cfg.PomodoroFocusMin = 25
	}
	if cfg.PomodoroShortBreak <= 0 {
		cfg.PomodoroShortBreak = 5
	}
	if cfg.PomodoroLongBreak <= 0 {
		cfg.PomodoroLongBreak = 15
	}
	if cfg.PomodoroCycles <= 0 {
		cfg.PomodoroCycles = 4
	}
	if cfg.SleepFadeSeconds < 0 {
		cfg.SleepFadeSeconds = 10
	}
	if cfg.LoudnessTargetLUFS == 0 {
		cfg.LoudnessTargetLUFS = -14
	}
	if cfg.AlbumArtProtocol == "" {
		cfg.AlbumArtProtocol = "auto"
	}
	if cfg.AIDJPersona == "" {
		cfg.AIDJPersona = "late-night-chill"
	}
	if cfg.AIDJTTSEngine == "" {
		cfg.AIDJTTSEngine = "auto"
	}
	if cfg.AIDJVoiceModel == "" {
		cfg.AIDJVoiceModel = "en_US-lessac-medium"
	}
	if cfg.AIDJOllamaURL == "" {
		cfg.AIDJOllamaURL = "http://127.0.0.1:11434"
	}
	if cfg.AIDJDuckLevel <= 0 || cfg.AIDJDuckLevel > 100 {
		cfg.AIDJDuckLevel = 20
	}

	return cfg, nil
}

// SaveConfig persists the current configuration to config.yaml.
//
// It writes nothing and returns ErrConfigUnreadable when cfg came from a
// config.yaml that could not be read, or when the file on disk does not
// parse now, so a broken file is never replaced by defaults.
func SaveConfig(cfg Config) error {
	if cfg.loadErr != nil {
		return fmt.Errorf("%w: %v; fix or remove it first", ErrConfigUnreadable, cfg.loadErr)
	}
	if _, err := LoadConfig(); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w: %v; fix or remove it first", ErrConfigUnreadable, err)
	}
	if err := EnsureConfigDir(); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return WriteFileAtomic(GetConfigFile(), data, 0o600)
}

// maxConfigFileBytes bounds config.yaml reads; a real config is a few KB.
const maxConfigFileBytes = 1 << 20
