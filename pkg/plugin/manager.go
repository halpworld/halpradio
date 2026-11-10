package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/halpworld/halpradio/pkg/util"
)

var (
	// ErrNotApproved is returned when an action needs the user's approval of
	// the plugin's current binary and permission set, and there is none.
	ErrNotApproved = errors.New("plugin permissions are not approved")
	// ErrPluginsDisabled is returned while the manager has plugins switched
	// off (SetPluginsDisabled(true)).
	ErrPluginsDisabled = errors.New("plugins are disabled")
	// ErrApprovalStale is returned by ApprovePermissionsFor when the plugin's
	// binary or permissions differ from what was shown to the user.
	ErrApprovalStale = errors.New("plugin changed since its permissions were shown; review them again")
)

// Notes shown to the user when a plugin needs (re-)approval.
const (
	noteNew         = "New plugin: review and approve its permissions to use it."
	noteBinary      = "The plugin binary changed since you approved it: review its permissions again."
	notePermissions = "The plugin's permissions changed since you approved it: review them again."
	noteLegacy      = "Approval now applies to one exact plugin binary: review its permissions again."
	noteUpdated     = "Plugin updated: review and approve its permissions again."
)

type pluginEntry struct {
	manifest Manifest
	state    PluginState
	dir      string
	wasmPath string
	wasmSHA  string // SHA-256 of the wasm as last read from disk
	note     string
	storage  *Storage
	sandbox  *Sandbox
	starting bool
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
	disabled   bool
}

// InstallOptions tunes InstallFromRegistryOpts.
type InstallOptions struct {
	// AllowDowngrade permits installing a version older than (or not
	// comparable with) the installed one. Off by default.
	AllowDowngrade bool
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

// SetPluginsDisabled switches all plugins off (true) or back on (false).
// While disabled no plugin is started, running plugins are stopped, and
// EnablePlugin, ApprovePermissions(id, true) and installs return
// ErrPluginsDisabled. Disabling and uninstalling stay possible. Call it
// before Init to honour a "plugins off" setting.
func (m *Manager) SetPluginsDisabled(disabled bool) {
	m.mu.Lock()
	m.disabled = disabled
	var toClose []*Sandbox
	if disabled {
		for _, e := range m.entries {
			if e.sandbox != nil {
				toClose = append(toClose, e.sandbox)
				e.sandbox = nil
			}
		}
	}
	m.mu.Unlock()
	for _, sb := range toClose {
		_ = sb.Close()
	}
}

// PluginsDisabled reports whether plugins are switched off.
func (m *Manager) PluginsDisabled() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.disabled
}

func (m *Manager) log(level int, msg string) {
	m.mu.RLock()
	fn := m.logHandler
	m.mu.RUnlock()
	if fn != nil {
		fn(level, msg)
	}
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func hashFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return hashBytes(b), nil
}

// approvalStatus reports whether the entry's approval covers its current
// binary and permission set, and if not, why.
func approvalStatus(e *pluginEntry) (bool, string) {
	st := e.state
	if !st.PermissionsApproved {
		if e.note != "" {
			return false, e.note
		}
		return false, noteNew
	}
	if st.ApprovedSHA256 == "" || st.ApprovedPermissions == nil {
		return false, noteLegacy
	}
	if e.wasmSHA == "" || !strings.EqualFold(st.ApprovedSHA256, e.wasmSHA) {
		return false, noteBinary
	}
	if !st.ApprovedPermissions.Equal(e.manifest.Permissions) {
		return false, notePermissions
	}
	return true, ""
}

// revokeLocked withdraws approval. The previously approved permission set is
// kept (not trusted) so a UI can show what changed via PermissionChanges.
func revokeLocked(e *pluginEntry, note string) {
	e.state.PermissionsApproved = false
	e.state.ApprovedSHA256 = ""
	e.note = note
}

// Init scans installed plugins and starts the enabled, approved ones.
func (m *Manager) Init() error {
	err := m.Scan()
	m.StartApproved()
	return err
}

// Scan loads plugin manifests and saved state without running any plugin
// code. A plugin is only accepted from a directory named exactly like its
// manifest ID. Approvals that no longer match the binary on disk or the
// manifest's permissions are revoked here.
func (m *Manager) Scan() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	_ = os.MkdirAll(m.pluginsDir, 0700)
	_ = os.MkdirAll(m.dataDir, 0700)

	savedStates := m.loadSavedStates()

	dirEntries, err := os.ReadDir(m.pluginsDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to read plugins dir: %w", err)
	}

	for _, d := range dirEntries {
		if !d.IsDir() {
			continue
		}
		dirName := d.Name()
		if !validIDRegex.MatchString(dirName) {
			continue // staging dirs (.tmp-*, .old-*) and junk
		}
		pluginDirPath := filepath.Join(m.pluginsDir, dirName)

		manifestPath := filepath.Join(pluginDirPath, "manifest.yaml")
		if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
			manifestPath = filepath.Join(pluginDirPath, "manifest.json")
		}

		manifest, err := LoadManifest(manifestPath)
		if err != nil {
			m.logLocked(3, fmt.Sprintf("Failed loading manifest for plugin %s: %v", dirName, err))
			continue
		}
		if manifest.ID != dirName {
			// A folder may not claim another plugin's ID (and inherit
			// its approval, storage or state).
			m.logLocked(3, fmt.Sprintf("Plugin in folder %q declares ID %q; the folder name must equal the ID, skipping", dirName, manifest.ID))
			continue
		}

		wasmPath := filepath.Join(pluginDirPath, manifest.WasmFile)
		rel, err := filepath.Rel(pluginDirPath, wasmPath)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			m.logLocked(3, fmt.Sprintf("Plugin %s has invalid wasm path: %s", dirName, wasmPath))
			continue
		}
		sha, _ := hashFile(wasmPath)

		state, hasState := savedStates[manifest.ID]
		if !hasState {
			state = PluginState{
				Enabled:             false, // nothing runs before the user approves and enables it
				PermissionsApproved: false,
				InstalledAt:         time.Now().Format(time.RFC3339),
			}
		}

		entry := &pluginEntry{
			manifest: *manifest,
			state:    state,
			dir:      pluginDirPath,
			wasmPath: wasmPath,
			wasmSHA:  sha,
		}
		if state.PermissionsApproved {
			if ok, note := approvalStatus(entry); !ok {
				revokeLocked(entry, note)
			}
		}

		if old, exists := m.entries[manifest.ID]; exists && old.sandbox != nil {
			_ = old.sandbox.Close()
		}
		m.entries[manifest.ID] = entry
	}

	return m.saveStatesLocked()
}

func (m *Manager) logLocked(level int, msg string) {
	if m.logHandler != nil {
		m.logHandler(level, msg)
	}
}

// StartApproved starts every enabled plugin whose approval matches its
// current binary and permissions. Each start runs outside the manager lock.
func (m *Manager) StartApproved() {
	m.mu.RLock()
	var ids []string
	if !m.disabled && !m.isClosing {
		for id, e := range m.entries {
			if ok, _ := approvalStatus(e); ok && e.state.Enabled && e.sandbox == nil {
				ids = append(ids, id)
			}
		}
	}
	m.mu.RUnlock()
	for _, id := range ids {
		_ = m.startEntry(id)
	}
}

// startEntry compiles and starts a plugin. The manager lock is not held
// while guest code runs. The bytes that are actually loaded are hashed and
// must equal the approved hash; the sandbox gets the approved permission
// snapshot, never a wider one.
func (m *Manager) startEntry(id string) error {
	m.mu.Lock()
	e, found := m.entries[id]
	if !found {
		m.mu.Unlock()
		return fmt.Errorf("plugin %s not found", id)
	}
	if m.isClosing || m.disabled {
		m.mu.Unlock()
		return ErrPluginsDisabled
	}
	if e.starting || e.sandbox != nil || !e.state.Enabled {
		m.mu.Unlock()
		return nil
	}
	if ok, _ := approvalStatus(e); !ok {
		m.mu.Unlock()
		return fmt.Errorf("plugin %s: %w", id, ErrNotApproved)
	}
	e.starting = true
	manifest := e.manifest
	manifest.Permissions = e.state.ApprovedPermissions.Normalized()
	state := e.state
	if e.state.Config != nil {
		state.Config = make(map[string]string, len(e.state.Config))
		for k, v := range e.state.Config {
			state.Config[k] = v
		}
	}
	wasmPath := e.wasmPath
	onNotify, onFlash, logFn := m.onNotify, m.onFlash, m.logHandler
	m.mu.Unlock()

	fail := func(msg string, sb *Sandbox, err error) error {
		if sb != nil {
			_ = sb.Close()
		}
		m.mu.Lock()
		e.starting = false
		e.lastErr = msg
		m.mu.Unlock()
		return err
	}

	wasmBytes, err := os.ReadFile(wasmPath)
	if err != nil {
		return fail(fmt.Sprintf("Wasm read error: %v", err), nil, err)
	}
	sha := hashBytes(wasmBytes)
	if !strings.EqualFold(sha, state.ApprovedSHA256) {
		m.mu.Lock()
		e.starting = false
		e.wasmSHA = sha
		revokeLocked(e, noteBinary)
		e.lastErr = noteBinary
		_ = m.saveStatesLocked()
		m.mu.Unlock()
		return fmt.Errorf("plugin %s: binary changed since approval: %w", id, ErrNotApproved)
	}

	storage, err := NewStorage(m.dataDir, id)
	if err != nil {
		return fail(fmt.Sprintf("Storage init error: %v", err), nil, err)
	}

	sb, err := NewSandbox(context.Background(), manifest, state, wasmBytes, storage, onNotify, onFlash, logFn)
	if err != nil {
		return fail(fmt.Sprintf("Sandbox compilation error: %v", err), nil, err)
	}
	if err := sb.Start(state.Config); err != nil {
		return fail(fmt.Sprintf("Startup error: %v", err), sb, err)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	e.starting = false
	if m.isClosing || m.disabled || m.entries[id] != e || !e.state.Enabled ||
		!e.state.PermissionsApproved || !strings.EqualFold(e.state.ApprovedSHA256, sha) {
		// State changed while starting (closed, disabled, revoked, replaced).
		_ = sb.Close()
		return fmt.Errorf("plugin %s: state changed during start", id)
	}
	e.storage = storage
	e.sandbox = sb
	e.lastErr = ""
	return nil
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

// saveStatesLocked writes plugins.json atomically with mode 0600.
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
	dir := filepath.Dir(m.stateFile)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".plugins-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, m.stateFile)
}

func (m *Manager) infoLocked(e *pluginEntry) PluginInfo {
	_, wasmErr := os.Stat(e.wasmPath)
	info := PluginInfo{
		Manifest:       e.manifest,
		State:          e.state,
		Dir:            e.dir,
		WasmPath:       e.wasmPath,
		HasValidBinary: wasmErr == nil,
		IsLoaded:       e.sandbox != nil && e.sandbox.Running(),
		LastError:      e.lastErr,
		WasmSHA256:     e.wasmSHA,
	}
	if e.sandbox != nil && info.LastError == "" {
		info.LastError = e.sandbox.LastError()
	}
	if ok, note := approvalStatus(e); !ok {
		info.State.PermissionsApproved = false
		info.ApprovalNote = note
	}
	if e.state.ApprovedPermissions != nil {
		cp := *e.state.ApprovedPermissions
		info.State.ApprovedPermissions = &cp
	}
	return info
}

// GetPlugins returns a snapshot list of all installed plugins.
func (m *Manager) GetPlugins() []PluginInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make([]PluginInfo, 0, len(m.entries))
	for _, e := range m.entries {
		res = append(res, m.infoLocked(e))
	}
	// Sorted by ID so a list drawn from one call lines up with a key handled
	// against the next call.
	sort.Slice(res, func(i, j int) bool { return res[i].Manifest.ID < res[j].Manifest.ID })
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
	return m.infoLocked(e), true
}

// EnablePlugin enables and starts a plugin. It returns ErrNotApproved, and
// changes nothing, when the plugin's current binary and permissions are not
// approved.
func (m *Manager) EnablePlugin(id string) error {
	m.mu.Lock()
	if m.disabled {
		m.mu.Unlock()
		return ErrPluginsDisabled
	}
	entry, found := m.entries[id]
	if !found {
		m.mu.Unlock()
		return fmt.Errorf("plugin %s not found", id)
	}
	if ok, _ := approvalStatus(entry); !ok {
		m.mu.Unlock()
		return fmt.Errorf("plugin %s: %w", id, ErrNotApproved)
	}
	entry.state.Enabled = true
	err := m.saveStatesLocked()
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if startErr := m.startEntry(id); errors.Is(startErr, ErrNotApproved) || errors.Is(startErr, ErrPluginsDisabled) {
		return startErr
	}
	return nil // other start failures are reported via PluginInfo.LastError
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

// ApprovePermissions grants or revokes approval for a plugin. Granting binds
// the approval to the SHA-256 of the plugin binary currently on disk and to
// its current permission set; any later change voids it. Revoking also stops
// the plugin. Prefer ApprovePermissionsFor, which also checks that the
// plugin did not change after its permissions were shown to the user.
func (m *Manager) ApprovePermissions(id string, approve bool) error {
	m.mu.Lock()
	entry, found := m.entries[id]
	if !found {
		m.mu.Unlock()
		return fmt.Errorf("plugin %s not found", id)
	}
	if !approve {
		entry.state.PermissionsApproved = false
		entry.state.ApprovedSHA256 = ""
		entry.state.ApprovedPermissions = nil
		entry.note = ""
		m.stopPluginLocked(entry)
		err := m.saveStatesLocked()
		m.mu.Unlock()
		return err
	}
	if m.disabled {
		m.mu.Unlock()
		return ErrPluginsDisabled
	}
	sha, err := hashFile(entry.wasmPath)
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("cannot approve plugin %s: %w", id, err)
	}
	return m.approveAndUnlock(entry, id, sha)
}

// ApprovePermissionsFor approves the plugin exactly as it was shown: shown
// must be the PluginInfo the user reviewed. If the binary on disk or the
// permission set no longer matches it, nothing is approved and
// ErrApprovalStale is returned.
func (m *Manager) ApprovePermissionsFor(id string, shown PluginInfo) error {
	m.mu.Lock()
	entry, found := m.entries[id]
	if !found {
		m.mu.Unlock()
		return fmt.Errorf("plugin %s not found", id)
	}
	if m.disabled {
		m.mu.Unlock()
		return ErrPluginsDisabled
	}
	sha, err := hashFile(entry.wasmPath)
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("cannot approve plugin %s: %w", id, err)
	}
	if shown.Manifest.ID != id || shown.WasmSHA256 == "" || !strings.EqualFold(shown.WasmSHA256, sha) ||
		!shown.Manifest.Permissions.Equal(entry.manifest.Permissions) {
		entry.wasmSHA = sha
		m.mu.Unlock()
		return fmt.Errorf("plugin %s: %w", id, ErrApprovalStale)
	}
	return m.approveAndUnlock(entry, id, sha)
}

// approveAndUnlock records the approval, saves, releases m.mu and starts the
// plugin if it is enabled.
func (m *Manager) approveAndUnlock(entry *pluginEntry, id, sha string) error {
	perms := entry.manifest.Permissions.Normalized()
	entry.state.PermissionsApproved = true
	entry.state.ApprovedSHA256 = sha
	entry.state.ApprovedPermissions = &perms
	entry.wasmSHA = sha
	entry.note = ""
	entry.lastErr = ""
	if entry.sandbox != nil {
		entry.sandbox.UpdateState(entry.state)
	}
	err := m.saveStatesLocked()
	start := entry.state.Enabled && entry.sandbox == nil
	m.mu.Unlock()
	if err != nil {
		return err
	}
	if start {
		_ = m.startEntry(id)
	}
	return nil
}

// RegistryClient returns the active registry client.
func (m *Manager) RegistryClient() *RegistryClient {
	return m.registry
}

// InstallFromRegistry downloads and installs a plugin from registry metadata.
// It refuses downgrades. The installed plugin is not started: its
// permissions must be approved (again, for an update) first.
func (m *Manager) InstallFromRegistry(ctx context.Context, reg PluginOrRegistry) error {
	return m.InstallFromRegistryOpts(ctx, reg, InstallOptions{})
}

// InstallFromRegistryOpts is InstallFromRegistry with options.
func (m *Manager) InstallFromRegistryOpts(ctx context.Context, reg PluginOrRegistry, opts InstallOptions) error {
	regPlugin := reg.ToRegistryPlugin()
	if !validIDRegex.MatchString(regPlugin.ID) {
		return fmt.Errorf("invalid plugin ID %q", regPlugin.ID)
	}

	m.mu.RLock()
	disabled := m.disabled || m.isClosing
	var installedVersion string
	upToDate := false
	if old, ok := m.entries[regPlugin.ID]; ok {
		installedVersion = old.manifest.Version
		// Same version, same binary, same permissions: nothing to do, and
		// the existing approval stays valid.
		upToDate = old.manifest.Version == regPlugin.Version && old.wasmSHA != "" &&
			strings.EqualFold(old.wasmSHA, strings.TrimSpace(regPlugin.Checksum)) &&
			old.manifest.Permissions.Equal(regPlugin.Permissions)
	}
	m.mu.RUnlock()
	if disabled {
		return ErrPluginsDisabled
	}
	if upToDate {
		return nil
	}
	if err := checkDowngrade(regPlugin.ID, installedVersion, regPlugin.Version, opts); err != nil {
		return err
	}

	// Network and verification happen without holding the manager lock.
	wasm, manifest, err := m.registry.FetchVerified(ctx, regPlugin)
	if err != nil {
		return err
	}
	manifest.WasmFile = "plugin.wasm"

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.disabled || m.isClosing {
		return ErrPluginsDisabled
	}
	old, exists := m.entries[regPlugin.ID]
	if exists {
		// Re-check: another install may have happened meanwhile.
		if err := checkDowngrade(regPlugin.ID, old.manifest.Version, manifest.Version, opts); err != nil {
			return err
		}
		m.stopPluginLocked(old)
	}
	if err := writePluginDir(m.pluginsDir, manifest, wasm); err != nil {
		return err
	}

	pluginDirPath := filepath.Join(m.pluginsDir, manifest.ID)
	now := time.Now().Format(time.RFC3339)
	entry := &pluginEntry{
		manifest: manifest,
		state: PluginState{
			Enabled:     false,
			InstalledAt: now,
			UpdatedAt:   now,
		},
		dir:      pluginDirPath,
		wasmPath: filepath.Join(pluginDirPath, manifest.WasmFile),
		wasmSHA:  hashBytes(wasm),
		note:     noteNew,
	}
	if exists {
		// Keep user preferences, never the approval: the new binary and
		// permissions must be approved again before anything runs.
		entry.state.Enabled = old.state.Enabled
		entry.state.Config = old.state.Config
		if old.state.InstalledAt != "" {
			entry.state.InstalledAt = old.state.InstalledAt
		}
		entry.state.ApprovedPermissions = old.state.ApprovedPermissions // for PermissionChanges only
		entry.note = noteUpdated
	}
	m.entries[manifest.ID] = entry
	return m.saveStatesLocked()
}

func checkDowngrade(id, installed, candidate string, opts InstallOptions) error {
	if installed == "" || opts.AllowDowngrade {
		return nil
	}
	cmp, err := CompareVersions(candidate, installed)
	if err != nil {
		return fmt.Errorf("refusing to install %s %q over %q: versions cannot be compared: %w", id, candidate, installed, err)
	}
	if cmp < 0 {
		return fmt.Errorf("refusing to downgrade %s from %s to %s", id, installed, candidate)
	}
	return nil
}

// UninstallPlugin stops a plugin and deletes its files and its storage.
func (m *Manager) UninstallPlugin(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry, found := m.entries[id]
	if !found {
		return fmt.Errorf("plugin %s not found", id)
	}

	m.stopPluginLocked(entry)
	if err := os.RemoveAll(entry.dir); err != nil {
		return fmt.Errorf("failed to remove plugin %s: %w", id, err)
	}
	if validIDRegex.MatchString(id) {
		if err := os.RemoveAll(filepath.Join(m.dataDir, id)); err != nil {
			return fmt.Errorf("failed to remove data of plugin %s: %w", id, err)
		}
	}
	delete(m.entries, id)

	return m.saveStatesLocked()
}

// Close shuts down all running sandboxes and plugins. Running guest calls
// are interrupted, so this does not wait on a looping plugin.
func (m *Manager) Close() error {
	m.mu.Lock()
	m.isClosing = true
	var toClose []*Sandbox
	for _, entry := range m.entries {
		if entry.sandbox != nil {
			toClose = append(toClose, entry.sandbox)
			entry.sandbox = nil
		}
	}
	m.mu.Unlock()
	for _, sb := range toClose {
		_ = sb.Close()
	}
	return nil
}

// Asynchronous Event Dispatching

// dispatch queues an event on every enabled, approved, running plugin that
// was approved for it. It never blocks: each plugin has a bounded queue and
// events are dropped while a plugin is busy.
func (m *Manager) dispatch(event string, payload any) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.isClosing || m.disabled || len(m.entries) == 0 {
		return
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	for _, entry := range m.entries {
		if entry.sandbox == nil || !entry.state.Enabled || !entry.state.PermissionsApproved ||
			entry.state.ApprovedPermissions == nil || !entry.state.ApprovedPermissions.HasEvent(event) {
			continue
		}
		entry.sandbox.Dispatch(event, data)
	}
}

// DispatchTrackChange sends track change payload to all approved enabled plugins.
func (m *Manager) DispatchTrackChange(payload TrackChangePayload) {
	m.dispatch("on_track_change", payload)
}

// DispatchPlaybackChange sends playback state changes to all approved enabled plugins.
func (m *Manager) DispatchPlaybackChange(payload PlaybackChangePayload) {
	m.dispatch("on_playback_change", payload)
}

// DispatchTimerTick sends periodic timer ticks to plugins.
func (m *Manager) DispatchTimerTick(payload TimerTickPayload) {
	m.dispatch("on_timer_tick", payload)
}

// PluginOrRegistry interface for versatile installer
type PluginOrRegistry interface {
	ToRegistryPlugin() RegistryPlugin
}

func (r RegistryPlugin) ToRegistryPlugin() RegistryPlugin {
	return r
}
