package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/halpworld/halpradio/pkg/util"
)

type pluginEntry struct {
	manifest Manifest
	state    PluginState
	dir      string
	wasmPath string
	storage  *Storage
	sandbox  *Sandbox
	lastErr  string
}

// Manager orchestrates plugin discovery, lifecycle, sandboxing, and event dispatching.
type Manager struct {
	mu         sync.RWMutex
	pluginsDir string
	dataDir    string
	stateFile  string
	entries    map[string]*pluginEntry
	registry   *RegistryClient
	onNotify   func(title, msg string)
	onFlash    func(msg string)
	logHandler func(level int, msg string)
	isClosing  bool
}

// NewManager initializes the plugin manager with configured paths.
func NewManager(registryURL string) *Manager {
	pluginsDir := util.GetPluginsDir()
	dataDir := util.GetPluginsDataDir()
	stateFile := util.GetPluginsConfigFile()

	return &Manager{
		pluginsDir: pluginsDir,
		dataDir:    dataDir,
		stateFile:  stateFile,
		entries:    make(map[string]*pluginEntry),
		registry:   NewRegistryClient(registryURL),
	}
}

// SetNotifyHandler sets the callback for UI/desktop notifications emitted by plugins.
func (m *Manager) SetNotifyHandler(fn func(title, msg string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onNotify = fn
}

// SetFlashHandler sets the callback for status bar flash messages emitted by plugins.
func (m *Manager) SetFlashHandler(fn func(msg string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onFlash = fn
}

// SetLogHandler sets the callback for plugin debug/info/error logs.
func (m *Manager) SetLogHandler(fn func(level int, msg string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logHandler = fn
}

func (m *Manager) log(level int, msg string) {
	m.mu.RLock()
	fn := m.logHandler
	m.mu.RUnlock()
	if fn != nil {
		fn(level, msg)
	}
}

// Init loads states and initializes installed plugins.
func (m *Manager) Init() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_ = os.MkdirAll(m.pluginsDir, 0700)
	_ = os.MkdirAll(m.dataDir, 0700)

	savedStates := m.loadSavedStates()

	// Scan plugins directory
	entries, err := os.ReadDir(m.pluginsDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read plugins dir: %w", err)
	}

	for _, d := range entries {
		if !d.IsDir() {
			continue
		}
		pluginID := d.Name()
		pluginDirPath := filepath.Join(m.pluginsDir, pluginID)

		// Check manifest
		manifestPath := filepath.Join(pluginDirPath, "manifest.yaml")
		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			manifestPath = filepath.Join(pluginDirPath, "manifest.json")
		}

		manifest, err := LoadManifest(manifestPath)
		if err != nil {
			m.log(3, fmt.Sprintf("Failed loading manifest for plugin %s: %v", pluginID, err))
			continue
		}

		wasmPath := filepath.Join(pluginDirPath, manifest.WasmFile)
		_, wasmErr := os.Stat(wasmPath)

		state, hasState := savedStates[manifest.ID]
		if !hasState {
			state = PluginState{
				Enabled:             true,
				PermissionsApproved: false, // Default: requires user approval
				InstalledAt:         time.Now().Format(time.RFC3339),
			}
		}

		entry := &pluginEntry{
			manifest: *manifest,
			state:    state,
			dir:      pluginDirPath,
			wasmPath: wasmPath,
		}

		if wasmErr == nil && state.Enabled {
			m.startPluginLocked(entry)
		}

		m.entries[manifest.ID] = entry
	}

	return m.saveStatesLocked()
}

func (m *Manager) startPluginLocked(entry *pluginEntry) {
	storage, err := NewStorage(m.dataDir, entry.manifest.ID)
	if err != nil {
		entry.lastErr = fmt.Sprintf("Storage init error: %v", err)
		return
	}
	entry.storage = storage

	wasmBytes, err := os.ReadFile(entry.wasmPath)
	if err != nil {
		entry.lastErr = fmt.Sprintf("Wasm read error: %v", err)
		return
	}

	sb, err := NewSandbox(
		context.Background(),
		entry.manifest,
		entry.state,
		wasmBytes,
		storage,
		m.onNotify,
		m.onFlash,
		m.logHandler,
	)
	if err != nil {
		entry.lastErr = fmt.Sprintf("Sandbox compilation error: %v", err)
		return
	}

	if err := sb.Start(entry.state.Config); err != nil {
		entry.lastErr = fmt.Sprintf("Startup error: %v", err)
		_ = sb.Close()
		return
	}

	entry.sandbox = sb
	entry.lastErr = ""
}

func (m *Manager) stopPluginLocked(entry *pluginEntry) {
	if entry.sandbox != nil {
		_ = entry.sandbox.Close()
		entry.sandbox = nil
	}
}

// loadSavedStates reads plugins.json.
func (m *Manager) loadSavedStates() map[string]PluginState {
	data, err := os.ReadFile(m.stateFile)
	if err != nil {
		return make(map[string]PluginState)
	}
	var cfg PluginsConfigFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return make(map[string]PluginState)
	}
	if cfg.Plugins == nil {
		cfg.Plugins = make(map[string]PluginState)
	}
	return cfg.Plugins
}

// saveStatesLocked writes plugins.json.
func (m *Manager) saveStatesLocked() error {
	states := make(map[string]PluginState, len(m.entries))
	for id, entry := range m.entries {
		states[id] = entry.state
	}
	cfg := PluginsConfigFile{Plugins: states}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.stateFile, data, 0644)
}

// GetPlugins returns a snapshot list of all installed plugins.
func (m *Manager) GetPlugins() []PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make([]PluginInfo, 0, len(m.entries))
	for _, e := range m.entries {
		_, wasmErr := os.Stat(e.wasmPath)
		res = append(res, PluginInfo{
			Manifest:       e.manifest,
			State:          e.state,
			Dir:            e.dir,
			WasmPath:       e.wasmPath,
			HasValidBinary: wasmErr == nil,
			IsLoaded:       e.sandbox != nil,
			LastError:      e.lastErr,
		})
	}
	return res
}

// GetPlugin returns a single installed plugin's info.
func (m *Manager) GetPlugin(id string) (PluginInfo, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	e, found := m.entries[id]
	if !found {
		return PluginInfo{}, false
	}
	_, wasmErr := os.Stat(e.wasmPath)
	return PluginInfo{
		Manifest:       e.manifest,
		State:          e.state,
		Dir:            e.dir,
		WasmPath:       e.wasmPath,
		HasValidBinary: wasmErr == nil,
		IsLoaded:       e.sandbox != nil,
		LastError:      e.lastErr,
	}, true
}

// EnablePlugin enables and starts a plugin.
func (m *Manager) EnablePlugin(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, found := m.entries[id]
	if !found {
		return fmt.Errorf("plugin %s not found", id)
	}

	entry.state.Enabled = true
	if entry.sandbox == nil {
		m.startPluginLocked(entry)
	}
	return m.saveStatesLocked()
}

// DisablePlugin stops and disables a plugin.
func (m *Manager) DisablePlugin(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, found := m.entries[id]
	if !found {
		return fmt.Errorf("plugin %s not found", id)
	}

	entry.state.Enabled = false
	m.stopPluginLocked(entry)
	return m.saveStatesLocked()
}

// ApprovePermissions updates permission approval state for a plugin.
func (m *Manager) ApprovePermissions(id string, approve bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, found := m.entries[id]
	if !found {
		return fmt.Errorf("plugin %s not found", id)
	}

	entry.state.PermissionsApproved = approve
	if entry.sandbox != nil {
		entry.sandbox.UpdateState(entry.state)
	} else if entry.state.Enabled && approve {
		m.startPluginLocked(entry)
	}

	return m.saveStatesLocked()
}

// RegistryClient returns the active registry client.
func (m *Manager) RegistryClient() *RegistryClient {
	return m.registry
}

// InstallFromRegistry downloads and installs a plugin from registry metadata.
func (m *Manager) InstallFromRegistry(ctx context.Context, reg PluginOrRegistry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	regPlugin := reg.ToRegistryPlugin()
	if err := m.registry.DownloadAndInstall(ctx, regPlugin, m.pluginsDir); err != nil {
		return err
	}

	// Reload the plugin into manager
	pluginDirPath := filepath.Join(m.pluginsDir, regPlugin.ID)
	manifestPath := filepath.Join(pluginDirPath, "manifest.yaml")
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("failed loading installed manifest: %w", err)
	}

	wasmPath := filepath.Join(pluginDirPath, manifest.WasmFile)

	entry := &pluginEntry{
		manifest: *manifest,
		state: PluginState{
			Enabled:             true,
			PermissionsApproved: false, // Prompt required after install
			InstalledAt:         time.Now().Format(time.RFC3339),
			UpdatedAt:           time.Now().Format(time.RFC3339),
		},
		dir:      pluginDirPath,
		wasmPath: wasmPath,
	}

	// If old sandbox existed, close it
	if old, exists := m.entries[regPlugin.ID]; exists {
		m.stopPluginLocked(old)
		entry.state.PermissionsApproved = old.state.PermissionsApproved
	}

	m.entries[regPlugin.ID] = entry

	if entry.state.Enabled && entry.state.PermissionsApproved {
		m.startPluginLocked(entry)
	}

	return m.saveStatesLocked()
}

// UninstallPlugin stops, deletes, and removes a plugin.
func (m *Manager) UninstallPlugin(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, found := m.entries[id]
	if !found {
		return fmt.Errorf("plugin %s not found", id)
	}

	m.stopPluginLocked(entry)
	_ = os.RemoveAll(entry.dir)
	delete(m.entries, id)

	return m.saveStatesLocked()
}

// Close shuts down all running sandboxes and plugins.
func (m *Manager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.isClosing = true
	for _, entry := range m.entries {
		m.stopPluginLocked(entry)
	}
	return nil
}

// Asynchronous Event Dispatching

// DispatchTrackChange sends track change payload to all approved enabled plugins.
func (m *Manager) DispatchTrackChange(payload TrackChangePayload) {
	m.mu.RLock()
	if m.isClosing || len(m.entries) == 0 {
		m.mu.RUnlock()
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		m.mu.RUnlock()
		return
	}

	// Dispatch to each plugin in parallel goroutines so Bubble Tea TUI rendering is never blocked
	for _, entry := range m.entries {
		if entry.state.Enabled && entry.state.PermissionsApproved && entry.sandbox != nil && entry.manifest.Permissions.HasEvent("on_track_change") {
			sb := entry.sandbox
			go func(s *Sandbox, d []byte) {
				_ = s.InvokeHook("on_track_change", d)
			}(sb, data)
		}
	}
	m.mu.RUnlock()
}

// DispatchPlaybackChange sends playback state changes to all approved enabled plugins.
func (m *Manager) DispatchPlaybackChange(payload PlaybackChangePayload) {
	m.mu.RLock()
	if m.isClosing || len(m.entries) == 0 {
		m.mu.RUnlock()
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		m.mu.RUnlock()
		return
	}

	for _, entry := range m.entries {
		if entry.state.Enabled && entry.state.PermissionsApproved && entry.sandbox != nil && entry.manifest.Permissions.HasEvent("on_playback_change") {
			sb := entry.sandbox
			go func(s *Sandbox, d []byte) {
				_ = s.InvokeHook("on_playback_change", d)
			}(sb, data)
		}
	}
	m.mu.RUnlock()
}

// DispatchTimerTick sends periodic timer ticks to plugins.
func (m *Manager) DispatchTimerTick(payload TimerTickPayload) {
	m.mu.RLock()
	if m.isClosing || len(m.entries) == 0 {
		m.mu.RUnlock()
		return
	}

	data, err := json.Marshal(payload)
	if err != nil {
		m.mu.RUnlock()
		return
	}

	for _, entry := range m.entries {
		if entry.state.Enabled && entry.state.PermissionsApproved && entry.sandbox != nil && entry.manifest.Permissions.HasEvent("on_timer_tick") {
			sb := entry.sandbox
			go func(s *Sandbox, d []byte) {
				_ = s.InvokeHook("on_timer_tick", d)
			}(sb, data)
		}
	}
	m.mu.RUnlock()
}

// PluginOrRegistry interface for versatile installer
type PluginOrRegistry interface {
	ToRegistryPlugin() RegistryPlugin
}

func (r RegistryPlugin) ToRegistryPlugin() RegistryPlugin {
	return r
}
