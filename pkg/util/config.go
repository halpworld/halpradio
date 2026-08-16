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
	MPRISEnabled         bool   `yaml:"mpris_enabled"`
	IPCEnabled           bool   `yaml:"ipc_enabled"`
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
		MPRISEnabled:         true,
		IPCEnabled:           true,
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
	return os.MkdirAll(dir, 0700)
}

func GetLocalStationsFile() string {
	return filepath.Join(GetConfigDir(), "stations.yaml")
}

func GetFavoritesFile() string {
	return filepath.Join(GetConfigDir(), "favorites.json")
}

func GetSavedTracksFile() string {
	return filepath.Join(GetConfigDir(), "saved_tracks.txt")
}

func GetConfigFile() string {
	return filepath.Join(GetConfigDir(), "config.yaml")
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
