package util

import (
	"os"
	"strings"
	"testing"
)

func TestConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Volume != 80 {
		t.Errorf("Expected Volume 80, got %d", cfg.Volume)
	}
	if cfg.PlayerBackend != "auto" {
		t.Errorf("Expected PlayerBackend auto, got %s", cfg.PlayerBackend)
	}
	if cfg.Theme != "tokyonight" {
		t.Errorf("Expected Theme tokyonight, got %s", cfg.Theme)
	}
	if cfg.VisualizerMode != "dj-cat" {
		t.Errorf("Expected VisualizerMode dj-cat, got %s", cfg.VisualizerMode)
	}
	if cfg.PomodoroFocusMin != 25 {
		t.Errorf("Expected PomodoroFocusMin 25, got %d", cfg.PomodoroFocusMin)
	}
	if cfg.PomodoroShortBreak != 5 {
		t.Errorf("Expected PomodoroShortBreak 5, got %d", cfg.PomodoroShortBreak)
	}
	if cfg.PomodoroLongBreak != 15 {
		t.Errorf("Expected PomodoroLongBreak 15, got %d", cfg.PomodoroLongBreak)
	}
	if cfg.PomodoroCycles != 4 {
		t.Errorf("Expected PomodoroCycles 4, got %d", cfg.PomodoroCycles)
	}
	if cfg.SleepFadeSeconds != 10 {
		t.Errorf("Expected SleepFadeSeconds 10, got %d", cfg.SleepFadeSeconds)
	}
	if !cfg.EventNotifyDesktop {
		t.Errorf("Expected EventNotifyDesktop true by default")
	}
	if !cfg.EventTerminalBell {
		t.Errorf("Expected EventTerminalBell true by default")
	}
	if !cfg.SongNotifications {
		t.Errorf("Expected SongNotifications true by default")
	}
	if !cfg.MPRISEnabled {
		t.Errorf("Expected MPRISEnabled true by default")
	}
	if !cfg.IPCEnabled {
		t.Errorf("Expected IPCEnabled true by default")
	}
	if !cfg.CatalogAutoUpdate {
		t.Errorf("Expected CatalogAutoUpdate true by default")
	}
	if cfg.CatalogCacheTTLHours != 24 {
		t.Errorf("Expected CatalogCacheTTLHours 24, got %d", cfg.CatalogCacheTTLHours)
	}
}

func TestConfigPathsAndLifecycle(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	configDir := GetConfigDir()
	if !strings.Contains(configDir, "halpradio") {
		t.Errorf("Expected GetConfigDir to contain halpradio, got %s", configDir)
	}

	if err := EnsureConfigDir(); err != nil {
		t.Fatalf("EnsureConfigDir failed: %v", err)
	}

	if _, err := os.Stat(configDir); os.IsNotExist(err) {
		t.Fatalf("Expected configDir %s to exist", configDir)
	}

	stationsFile := GetLocalStationsFile()
	if !strings.HasSuffix(stationsFile, "stations.yaml") {
		t.Errorf("Expected stations.yaml suffix, got %s", stationsFile)
	}

	cacheFile := GetCatalogCacheFile()
	if !strings.HasSuffix(cacheFile, "catalog_cache.yaml") {
		t.Errorf("Expected catalog_cache.yaml suffix, got %s", cacheFile)
	}

	metadataFile := GetCatalogMetadataFile()
	if !strings.HasSuffix(metadataFile, "catalog_metadata.json") {
		t.Errorf("Expected catalog_metadata.json suffix, got %s", metadataFile)
	}

	favFile := GetFavoritesFile()
	if !strings.HasSuffix(favFile, "favorites.json") {
		t.Errorf("Expected favorites.json suffix, got %s", favFile)
	}

	savedTracksFile := GetSavedTracksFile()
	if !strings.HasSuffix(savedTracksFile, "saved_tracks.txt") {
		t.Errorf("Expected saved_tracks.txt suffix, got %s", savedTracksFile)
	}

	configFile := GetConfigFile()
	if !strings.HasSuffix(configFile, "config.yaml") {
		t.Errorf("Expected config.yaml suffix, got %s", configFile)
	}

	// 1. Load when config file does not exist
	loadedCfg, err := LoadConfig()
	if err == nil {
		t.Errorf("Expected error loading non-existent config, got nil")
	}
	if loadedCfg.Theme != "tokyonight" {
		t.Errorf("Expected default config on missing file, got theme %s", loadedCfg.Theme)
	}

	// 2. Save custom config and reload
	customCfg := Config{
		Volume:               65,
		PlayerBackend:        "mpv",
		Theme:                "gruvbox",
		VisualizerMode:       "wave",
		LastStationID:        "lofi-girl",
		SearchProvider:       "youtube",
		PomodoroFocusMin:     50,
		PomodoroShortBreak:   10,
		PomodoroLongBreak:    30,
		PomodoroCycles:       6,
		PomodoroFocusStation: "somafm-groovesalad",
		PomodoroBreakStation: "lofi-girl",
		SleepFadeSeconds:     15,
		EventNotifyDesktop:   false,
		EventTerminalBell:    false,
		EventCommandHook:     "echo 'timer event'",
	}

	if err := SaveConfig(customCfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	reloadedCfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed after save: %v", err)
	}
	if reloadedCfg.Volume != 65 || reloadedCfg.Theme != "gruvbox" || reloadedCfg.VisualizerMode != "wave" {
		t.Errorf("Reloaded config mismatch: %+v", reloadedCfg)
	}
	if reloadedCfg.PomodoroFocusMin != 50 || reloadedCfg.PomodoroCycles != 6 {
		t.Errorf("Reloaded pomodoro config mismatch: %+v", reloadedCfg)
	}
	if reloadedCfg.EventNotifyDesktop || reloadedCfg.EventTerminalBell {
		t.Errorf("Reloaded event flags mismatch: %+v", reloadedCfg)
	}

	// 3. Test fallback defaults for zero/negative values
	zeroCfgData := []byte(`
volume: -5
pomodoro_focus_min: 0
pomodoro_short_break_min: -1
pomodoro_long_break_min: 0
pomodoro_cycles: 0
sleep_fade_seconds: -10
`)
	if err := os.WriteFile(configFile, zeroCfgData, 0644); err != nil {
		t.Fatalf("Failed writing zeroCfgData: %v", err)
	}

	fallbackCfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed on zeroCfg: %v", err)
	}
	if fallbackCfg.Volume != 80 {
		t.Errorf("Expected Volume fallback 80, got %d", fallbackCfg.Volume)
	}
	if fallbackCfg.PomodoroFocusMin != 25 {
		t.Errorf("Expected PomodoroFocusMin fallback 25, got %d", fallbackCfg.PomodoroFocusMin)
	}
	if fallbackCfg.PomodoroShortBreak != 5 {
		t.Errorf("Expected PomodoroShortBreak fallback 5, got %d", fallbackCfg.PomodoroShortBreak)
	}
	if fallbackCfg.PomodoroLongBreak != 15 {
		t.Errorf("Expected PomodoroLongBreak fallback 15, got %d", fallbackCfg.PomodoroLongBreak)
	}
	if fallbackCfg.PomodoroCycles != 4 {
		t.Errorf("Expected PomodoroCycles fallback 4, got %d", fallbackCfg.PomodoroCycles)
	}
	if fallbackCfg.SleepFadeSeconds != 10 {
		t.Errorf("Expected SleepFadeSeconds fallback 10, got %d", fallbackCfg.SleepFadeSeconds)
	}

	// 5. Test Plugin directory and config paths
	pluginsDir := GetPluginsDir()
	if !strings.HasSuffix(pluginsDir, "plugins") {
		t.Errorf("Expected plugins directory suffix, got %s", pluginsDir)
	}

	pluginsDataDir := GetPluginsDataDir()
	if !strings.HasSuffix(pluginsDataDir, "plugins_data") {
		t.Errorf("Expected plugins_data directory suffix, got %s", pluginsDataDir)
	}

	pluginsConfigFile := GetPluginsConfigFile()
	if !strings.HasSuffix(pluginsConfigFile, "plugins.json") {
		t.Errorf("Expected plugins.json file suffix, got %s", pluginsConfigFile)
	}

	// 6. Test DiscordRPC config options
	discordCfg := DefaultConfig()
	if !discordCfg.DiscordRPC {
		t.Errorf("Expected DiscordRPC true by default")
	}
	discordCfg.DiscordRPC = false
	discordCfg.DiscordClientID = "999888777666555444"
	if err := SaveConfig(discordCfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}
	reloadedDiscordCfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if reloadedDiscordCfg.DiscordRPC || reloadedDiscordCfg.DiscordClientID != "999888777666555444" {
		t.Errorf("Expected DiscordRPC false and custom client ID, got %+v", reloadedDiscordCfg)
	}
}
