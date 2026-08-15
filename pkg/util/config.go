package util

import (
	"os"
	"path/filepath"
)

type Config struct {
	Volume         int    `yaml:"volume"`
	PlayerBackend  string `yaml:"player_backend"`
	Theme          string `yaml:"theme"`
	VisualizerMode string `yaml:"visualizer_mode"`
	LastStationID  string `yaml:"last_station_id"`
	SearchProvider string `yaml:"search_provider,omitempty"`
}

func DefaultConfig() Config {
	return Config{
		Volume:         80,
		PlayerBackend:  "auto",
		Theme:          "tokyonight",
		VisualizerMode: "dj-cat",
		LastStationID:  "",
		SearchProvider: "spotify",
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
