package util

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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
	if !cfg.AutoPause {
		t.Errorf("Expected AutoPause true by default")
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
	if cfg.AIDJEnabled {
		t.Errorf("Expected AIDJEnabled false by default (opt-in)")
	}
	if cfg.AIDJPersona != "late-night-chill" {
		t.Errorf("Expected AIDJPersona 'late-night-chill', got %q", cfg.AIDJPersona)
	}
	if cfg.AIDJTTSEngine != "auto" {
		t.Errorf("Expected AIDJTTSEngine 'auto', got %q", cfg.AIDJTTSEngine)
	}
	if cfg.AIDJVoiceModel != "en_US-lessac-medium" {
		t.Errorf("Expected AIDJVoiceModel 'en_US-lessac-medium', got %q", cfg.AIDJVoiceModel)
	}
	if cfg.AIDJOllamaURL != "http://127.0.0.1:11434" {
		t.Errorf("Expected AIDJOllamaURL 'http://127.0.0.1:11434', got %q", cfg.AIDJOllamaURL)
	}
	if cfg.AIDJOllamaModel != "" {
		t.Errorf("Expected AIDJOllamaModel '', got %q", cfg.AIDJOllamaModel)
	}
	if cfg.AIDJDuckLevel != 20 {
		t.Errorf("Expected AIDJDuckLevel 20, got %d", cfg.AIDJDuckLevel)
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

	themesDir := GetThemesDir()
	if !strings.HasSuffix(themesDir, "themes") {
		t.Errorf("Expected themes directory suffix, got %s", themesDir)
	}
	if _, err := os.Stat(themesDir); os.IsNotExist(err) {
		t.Fatalf("Expected themesDir %s to exist", themesDir)
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

	// 7. Test AI DJ & Cache directories
	t.Setenv("XDG_CACHE_HOME", tempDir)
	if !strings.HasSuffix(GetAICacheDir(), "ai") {
		t.Errorf("Expected GetAICacheDir suffix 'ai', got %s", GetAICacheDir())
	}
	if !strings.HasSuffix(GetTTSModelsDir(), "models") {
		t.Errorf("Expected GetTTSModelsDir suffix 'models', got %s", GetTTSModelsDir())
	}
	if !strings.HasSuffix(GetLinerNotesCacheDir(), "linernotes") {
		t.Errorf("Expected GetLinerNotesCacheDir suffix 'linernotes', got %s", GetLinerNotesCacheDir())
	}
	if err := EnsureCacheDir(); err != nil {
		t.Fatalf("EnsureCacheDir failed: %v", err)
	}
	if _, err := os.Stat(GetLinerNotesCacheDir()); os.IsNotExist(err) {
		t.Errorf("Expected GetLinerNotesCacheDir to be created by EnsureCacheDir")
	}
	if _, err := os.Stat(GetTTSModelsDir()); !os.IsNotExist(err) {
		t.Errorf("Expected GetTTSModelsDir NOT to be created until on-demand download")
	}
}

func TestLoudnessTargetAndDSPFile(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("HOME", tempDir)
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	if got := DefaultConfig().LoudnessTargetLUFS; got != -14 {
		t.Errorf("default loudness target = %v, want -14", got)
	}
	if got := GetDSPFile(); got != filepath.Join(GetConfigDir(), "dsp.yaml") {
		t.Errorf("GetDSPFile() = %q", got)
	}

	// A config.yaml written before the field existed falls back to -14.
	if err := EnsureConfigDir(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GetConfigFile(), []byte("volume: 50\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LoudnessTargetLUFS != -14 {
		t.Errorf("missing loudness target should default to -14, got %v", cfg.LoudnessTargetLUFS)
	}

	cfg.LoudnessTargetLUFS = -23
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg, _ = LoadConfig()
	if cfg.LoudnessTargetLUFS != -23 {
		t.Errorf("loudness target did not round-trip: %v", cfg.LoudnessTargetLUFS)
	}
}

// resetFallbackDir clears the cached private fallback directory so a test
// observes the fallback logic for its own TMPDIR.
func resetFallbackDir(t *testing.T) {
	t.Helper()
	fallbackDirMu.Lock()
	fallbackDir = ""
	fallbackDirMu.Unlock()
	t.Cleanup(func() {
		fallbackDirMu.Lock()
		fallbackDir = ""
		fallbackDirMu.Unlock()
	})
}

func unsetHomeEnv(t *testing.T, tmp string) {
	t.Helper()
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("XDG_CACHE_HOME", "")
	t.Setenv("TMPDIR", tmp)
	resetFallbackDir(t)
}

// LOC-3 / AI-6: with HOME unset the config, cache and TTS models directories
// used to resolve to "./.halpradio", so a planted ./.halpradio/config.yaml
// (event_command_hook) or ./.halpradio/cache/ai/models/_runtime binary in the
// working directory was used. They must now always be absolute and outside
// the working directory.
func TestConfigDirsNeverRelativeWhenHomeUnset(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("HOME is not how Windows resolves the profile directory")
	}
	tmp := t.TempDir()
	unsetHomeEnv(t, tmp)

	work := t.TempDir()
	planted := filepath.Join(work, ".halpradio")
	if err := os.MkdirAll(planted, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(planted, "config.yaml"), []byte("event_command_hook: \"touch pwned\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)

	for name, dir := range map[string]string{
		"config": GetConfigDir(),
		"cache":  GetCacheDir(),
		"models": GetTTSModelsDir(),
	} {
		if !filepath.IsAbs(dir) {
			t.Errorf("%s dir %q is relative", name, dir)
		}
		if strings.HasPrefix(dir, work) {
			t.Errorf("%s dir %q is inside the working directory", name, dir)
		}
	}

	cfg, _ := LoadConfig()
	if cfg.EventCommandHook != "" {
		t.Fatalf("event_command_hook loaded from the working directory: %q", cfg.EventCommandHook)
	}

	root := filepath.Dir(GetConfigDir())
	info, err := os.Lstat(root)
	if err != nil {
		t.Fatalf("fallback root missing: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Errorf("fallback root mode = %v, want 0700", info.Mode().Perm())
	}
	if !strings.HasPrefix(root, tmp) {
		t.Errorf("fallback root %q not under TMPDIR %q", root, tmp)
	}
}

// A pre-existing, attacker-created halpradio-<uid> directory (too open, or a
// symlink) must not be adopted as the fallback.
func TestPrivateFallbackDirRejectsPlantedDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uid-based fallback is Unix only")
	}
	for _, tc := range []string{"world-writable", "symlink"} {
		t.Run(tc, func(t *testing.T) {
			tmp := t.TempDir()
			unsetHomeEnv(t, tmp)
			stable := filepath.Join(tmp, fmt.Sprintf("halpradio-%d", os.Getuid()))
			switch tc {
			case "world-writable":
				if err := os.Mkdir(stable, 0o777); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(stable, 0o777); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := t.TempDir()
				if err := os.Symlink(target, stable); err != nil {
					t.Fatal(err)
				}
			}
			got := filepath.Dir(GetConfigDir())
			if got == stable {
				t.Fatalf("planted %s directory %q was adopted", tc, stable)
			}
			if !isPrivateDir(got) {
				t.Fatalf("fallback %q is not a private directory", got)
			}
		})
	}
}

// LOC-6: config.yaml must be written atomically with 0600 inside a 0700
// directory, and looser permissions left by older versions are tightened.
func TestSaveConfigPermissionsAndTightening(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	dir := GetConfigDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GetConfigFile(), []byte("volume: 33\nlastfm_api_key: secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Volume != 33 {
		t.Fatalf("volume = %d", cfg.Volume)
	}
	if fi, _ := os.Stat(GetConfigFile()); fi.Mode().Perm() != 0o600 {
		t.Errorf("LoadConfig left config.yaml at %v, want 0600", fi.Mode().Perm())
	}
	if di, _ := os.Stat(dir); di.Mode().Perm() != 0o700 {
		t.Errorf("LoadConfig left config dir at %v, want 0700", di.Mode().Perm())
	}

	// Loosen again and check SaveConfig fixes both.
	_ = os.Chmod(dir, 0o755)
	_ = os.Chmod(GetConfigFile(), 0o644)
	cfg.Volume = 44
	if err := SaveConfig(cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if fi, _ := os.Stat(GetConfigFile()); fi.Mode().Perm() != 0o600 {
		t.Errorf("SaveConfig wrote config.yaml with %v, want 0600", fi.Mode().Perm())
	}
	if di, _ := os.Stat(dir); di.Mode().Perm() != 0o700 {
		t.Errorf("SaveConfig left config dir at %v, want 0700", di.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Errorf("temporary file left behind: %s", e.Name())
		}
	}
	again, err := LoadConfig()
	if err != nil || again.Volume != 44 {
		t.Fatalf("round trip: %v %d", err, again.Volume)
	}
}

func TestFileOwnerUIDMatchesCurrentUser(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no uid on Windows")
	}
	info, err := os.Stat(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	uid, ok := fileOwnerUID(info)
	if !ok || uid != os.Getuid() {
		t.Fatalf("fileOwnerUID = %d, %v; want %d, true", uid, ok, os.Getuid())
	}
}

// Provider opt-outs default to enabled, stay enabled when the key is absent
// from an older config.yaml, and honour an explicit false.
func TestProviderSwitchesDefaultEnabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))

	d := DefaultConfig()
	if !d.LyricsNetEaseEnabled || !d.ArtITunesEnabled || !d.ArtDeezerEnabled || !d.ArtMusicBrainzEnabled {
		t.Fatalf("provider switches must default to enabled: %+v", d)
	}
	if err := os.MkdirAll(GetConfigDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(GetConfigFile(), []byte("volume: 40\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.LyricsNetEaseEnabled || !cfg.ArtITunesEnabled || !cfg.ArtDeezerEnabled || !cfg.ArtMusicBrainzEnabled {
		t.Fatalf("missing keys must keep providers enabled: %+v", cfg)
	}
	off := "lyrics_netease_enabled: false\nalbum_art_itunes_enabled: false\nalbum_art_deezer_enabled: false\nalbum_art_musicbrainz_enabled: false\n"
	if err := os.WriteFile(GetConfigFile(), []byte(off), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LyricsNetEaseEnabled || cfg.ArtITunesEnabled || cfg.ArtDeezerEnabled || cfg.ArtMusicBrainzEnabled {
		t.Fatalf("explicit false must disable providers: %+v", cfg)
	}
	if err := SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	cfg, _ = LoadConfig()
	if cfg.ArtDeezerEnabled {
		t.Fatal("false did not survive a save/load round trip")
	}
}
