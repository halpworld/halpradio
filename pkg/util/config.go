package util

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Volume               int    `yaml:"volume"`
	PlayerBackend        string `yaml:"player_backend"`
	Theme                string `yaml:"theme"`
	VisualizerMode       string `yaml:"visualizer_mode"`
	LastStationID        string `yaml:"last_station_id"`
	SearchProvider       string `yaml:"search_provider,omitempty"`
	PomodoroFocusMin     int    `yaml:"pomodoro_focus_min,omitempty"`
	PomodoroShortBreak   int    `yaml:"pomodoro_short_break_min,omitempty"`
	PomodoroLongBreak    int    `yaml:"pomodoro_long_break_min,omitempty"`
	PomodoroCycles       int    `yaml:"pomodoro_cycles,omitempty"`
	PomodoroFocusStation string `yaml:"pomodoro_focus_station,omitempty"`
	PomodoroBreakStation string `yaml:"pomodoro_break_station,omitempty"`
	SleepFadeSeconds     int    `yaml:"sleep_fade_seconds,omitempty"`
	EventNotifyDesktop   bool   `yaml:"event_notify_desktop"`
	EventTerminalBell    bool   `yaml:"event_terminal_bell"`
	EventCommandHook     string `yaml:"event_command_hook,omitempty"`
	SongNotifications    bool   `yaml:"song_notifications"`
	AutoPause            bool   `yaml:"autopause"`
	MPRISEnabled         bool   `yaml:"mpris_enabled"`
	IPCEnabled           bool   `yaml:"ipc_enabled"`
	DiscordRPC           bool   `yaml:"discord_rpc"`
	DiscordClientID      string `yaml:"discord_client_id,omitempty"`
	PluginsEnabled       bool   `yaml:"plugins_enabled"`
	PluginRegistryURL    string `yaml:"plugin_registry_url,omitempty"`
	ThemeRegistryURL     string `yaml:"theme_registry_url,omitempty"`
	CatalogAutoUpdate    bool   `yaml:"catalog_auto_update"`
	CatalogUpdateURL     string `yaml:"catalog_update_url,omitempty"`
	CatalogCacheTTLHours int    `yaml:"catalog_cache_ttl_hours,omitempty"`
	ExperimentalTuner    bool   `yaml:"experimental_tuner,omitempty"`
	FingerprintEnabled   bool   `yaml:"fingerprint_enabled"`
	AcoustidAPIKey       string `yaml:"acoustid_api_key,omitempty"`
	AutoIdentify         bool   `yaml:"auto_identify"`
	LyricsEnabled        bool   `yaml:"lyrics_enabled"`
	LyricsAutoOpen       bool   `yaml:"lyrics_auto_open"`
	LyricsOffsetMs       int    `yaml:"lyrics_offset_ms,omitempty"`
	AlbumArtEnabled      bool   `yaml:"album_art_enabled"`
	AlbumArtProtocol     string `yaml:"album_art_protocol,omitempty"`
	LastFMAPIKey         string `yaml:"lastfm_api_key,omitempty"`
	PartyNickname        string `yaml:"party_nickname,omitempty"`
	PartyRelayURL        string `yaml:"party_relay_url,omitempty"`
	PartyPort            int    `yaml:"party_port,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		Volume:               80,
		PlayerBackend:        "auto",
		Theme:                "tokyonight",
		VisualizerMode:       "dj-cat",
		LastStationID:        "",
		SearchProvider:       "spotify",
		PomodoroFocusMin:     25,
		PomodoroShortBreak:   5,
		PomodoroLongBreak:    15,
		PomodoroCycles:       4,
		PomodoroFocusStation: "",
		PomodoroBreakStation: "",
		SleepFadeSeconds:     10,
		EventNotifyDesktop:   true,
		EventTerminalBell:    true,
		EventCommandHook:     "",
		SongNotifications:    true,
		AutoPause:            true,
		MPRISEnabled:         true,
		IPCEnabled:           true,
		DiscordRPC:           true,
		DiscordClientID:      "1340000000000000000",
		PluginsEnabled:       true,
		PluginRegistryURL:    "https://raw.githubusercontent.com/halpworld/halpradio-plugins/main/registry.json",
		ThemeRegistryURL:     "https://raw.githubusercontent.com/halpworld/halpradio-themes/main/themes.json",
		CatalogAutoUpdate:    true,
		CatalogUpdateURL:     "https://raw.githubusercontent.com/halpworld/halpradio/main/stations.yaml",
		CatalogCacheTTLHours: 24,
		ExperimentalTuner:    false,
		FingerprintEnabled:   true,
		AcoustidAPIKey:       "v8pQ6oyB",
		AutoIdentify:         true,
		LyricsEnabled:        true,
		LyricsAutoOpen:       false,
		LyricsOffsetMs:       0,
		AlbumArtEnabled:      true,
		AlbumArtProtocol:     "auto",
		LastFMAPIKey:         "",
		PartyNickname:        "",
		PartyPort:            0,
	}
}

// GetConfigDir returns the directory path where user settings & local stations live.
func GetConfigDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		home, err := os.UserHomeDir()
		if err != nil {
			return ".halpradio"
		}
		return filepath.Join(home, ".config", "halpradio")
	}
	return filepath.Join(configDir, "halpradio")
}

// EnsureConfigDir creates the configuration directory if it doesn't exist.
func EnsureConfigDir() error {
	dir := GetConfigDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.MkdirAll(GetThemesDir(), 0700)
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
func GetCacheDir() string {
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		home, err := os.UserHomeDir()
		if err != nil {
			return filepath.Join(".halpradio", "cache")
		}
		return filepath.Join(home, ".cache", "halpradio")
	}
	return filepath.Join(cacheDir, "halpradio")
}

// GetLyricsCacheDir returns the on-disk cache location for lyric sheets.
func GetLyricsCacheDir() string {
	return filepath.Join(GetCacheDir(), "lyrics")
}

// GetAlbumArtCacheDir returns the on-disk cache location for cover artwork.
func GetAlbumArtCacheDir() string {
	return filepath.Join(GetCacheDir(), "art")
}

// EnsureCacheDir creates the lyrics and artwork cache directories. A failure
// here is never fatal: the callers fall back to network-only operation.
func EnsureCacheDir() error {
	if err := os.MkdirAll(GetLyricsCacheDir(), 0700); err != nil {
		return err
	}
	return os.MkdirAll(GetAlbumArtCacheDir(), 0700)
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

	data, err := os.ReadFile(filePath)
	if err != nil {
		return cfg, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig(), err
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
	if cfg.AlbumArtProtocol == "" {
		cfg.AlbumArtProtocol = "auto"
	}

	return cfg, nil
}

// SaveConfig persists the current configuration to config.yaml.
func SaveConfig(cfg Config) error {
	if err := EnsureConfigDir(); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return os.WriteFile(GetConfigFile(), data, 0644)
}
