package plugin

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ---- hand-assembled wasm fixtures (see the PLG PoCs) ----

var hdr = []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00}

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// (memory (export "memory") 1) (func (export "on_init") (param i32 i32) (result i32) i32.const 0)
var wasmBenign = cat(hdr,
	[]byte{0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f},
	[]byte{0x03, 0x02, 0x01, 0x00},
	[]byte{0x05, 0x03, 0x01, 0x00, 0x01},
	[]byte{0x07, 0x14, 0x02, 0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00, 0x07, 'o', 'n', '_', 'i', 'n', 'i', 't', 0x00, 0x00},
	[]byte{0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x00, 0x0b},
)

// (memory (export "memory") 1) (func (export "_start") (loop br 0))
var wasmLoopStart = cat(hdr,
	[]byte{0x01, 0x04, 0x01, 0x60, 0x00, 0x00},
	[]byte{0x03, 0x02, 0x01, 0x00},
	[]byte{0x05, 0x03, 0x01, 0x00, 0x01},
	[]byte{0x07, 0x13, 0x02, 0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00, 0x06, '_', 's', 't', 'a', 'r', 't', 0x00, 0x00},
	[]byte{0x0a, 0x09, 0x01, 0x07, 0x00, 0x03, 0x40, 0x0c, 0x00, 0x0b, 0x0b},
)

// (memory (export "memory") 1) (func (export "on_track_change") (param i32 i32) (result i32) (loop br 0) unreachable)
var wasmLoopHook = cat(hdr,
	[]byte{0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f},
	[]byte{0x03, 0x02, 0x01, 0x00},
	[]byte{0x05, 0x03, 0x01, 0x00, 0x01},
	append([]byte{0x07, 0x1c, 0x02, 0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00, 0x0f}, append([]byte("on_track_change"), 0x00, 0x00)...),
	[]byte{0x0a, 0x0a, 0x01, 0x08, 0x00, 0x03, 0x40, 0x0c, 0x00, 0x0b, 0x00, 0x0b},
)

// no memory; exports alloc(i32)->i32 and on_init(i32,i32)->i32
var wasmNoMemAlloc = cat(hdr,
	[]byte{0x01, 0x0c, 0x02, 0x60, 0x01, 0x7f, 0x01, 0x7f, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f},
	[]byte{0x03, 0x03, 0x02, 0x00, 0x01},
	[]byte{0x07, 0x13, 0x02, 0x05, 'a', 'l', 'l', 'o', 'c', 0x00, 0x00, 0x07, 'o', 'n', '_', 'i', 'n', 'i', 't', 0x00, 0x01},
	[]byte{0x0a, 0x0b, 0x02, 0x04, 0x00, 0x41, 0x00, 0x0b, 0x04, 0x00, 0x41, 0x00, 0x0b},
)

// no memory, no alloc; exports on_init(i32,i32)->i32
var wasmNoMemNoAlloc = cat(hdr,
	[]byte{0x01, 0x07, 0x01, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f},
	[]byte{0x03, 0x02, 0x01, 0x00},
	[]byte{0x07, 0x0b, 0x01, 0x07, 'o', 'n', '_', 'i', 'n', 'i', 't', 0x00, 0x00},
	[]byte{0x0a, 0x06, 0x01, 0x04, 0x00, 0x41, 0x00, 0x0b},
)

// no memory; imports halpradio.ui_flash; _start calls ui_flash(0,5)
var wasmNoMemHostCall = cat(hdr,
	[]byte{0x01, 0x09, 0x02, 0x60, 0x02, 0x7f, 0x7f, 0x00, 0x60, 0x00, 0x00},
	cat([]byte{0x02, 0x16, 0x01, 0x09}, []byte("halpradio"), []byte{0x08}, []byte("ui_flash"), []byte{0x00, 0x00}),
	[]byte{0x03, 0x02, 0x01, 0x01},
	[]byte{0x07, 0x0a, 0x01, 0x06, '_', 's', 't', 'a', 'r', 't', 0x00, 0x01},
	[]byte{0x0a, 0x0a, 0x01, 0x08, 0x00, 0x41, 0x00, 0x41, 0x05, 0x10, 0x00, 0x0b},
)

// imports halpradio.ui_flash; memory with "PWNED" at 16; on_init and
// on_track_change both call ui_flash(16,5).
var wasmFlash = cat(hdr,
	[]byte{0x01, 0x0c, 0x02, 0x60, 0x02, 0x7f, 0x7f, 0x00, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f},
	append([]byte{0x02, 0x16, 0x01, 0x09}, append([]byte("halpradio"), append([]byte{0x08}, append([]byte("ui_flash"), 0x00, 0x00)...)...)...),
	[]byte{0x03, 0x02, 0x01, 0x01},
	[]byte{0x05, 0x03, 0x01, 0x00, 0x01},
	cat([]byte{0x07, 0x26, 0x03,
		0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00,
		0x07, 'o', 'n', '_', 'i', 'n', 'i', 't', 0x00, 0x01,
		0x0f}, []byte("on_track_change"), []byte{0x00, 0x01}),
	[]byte{0x0a, 0x0c, 0x01, 0x0a, 0x00, 0x41, 0x10, 0x41, 0x05, 0x10, 0x00, 0x41, 0x00, 0x0b},
	[]byte{0x0b, 0x0b, 0x01, 0x00, 0x41, 0x10, 0x0b, 0x05, 'P', 'W', 'N', 'E', 'D'},
)

// on_init: http_fetch(url) into memory, then ui_flash(result).
func wasmFetch(url string) []byte {
	n := byte(len(url))
	return cat(hdr,
		[]byte{0x01, 0x18, 0x03, 0x60, 0x08, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x01, 0x7f, 0x60, 0x02, 0x7f, 0x7f, 0x01, 0x7f, 0x60, 0x02, 0x7f, 0x7f, 0x00},
		cat([]byte{0x02, 0x2d, 0x02, 0x09}, []byte("halpradio"), []byte{0x0a}, []byte("http_fetch"), []byte{0x00, 0x00, 0x09}, []byte("halpradio"), []byte{0x08}, []byte("ui_flash"), []byte{0x00, 0x02}),
		[]byte{0x03, 0x02, 0x01, 0x01},
		[]byte{0x05, 0x03, 0x01, 0x00, 0x01},
		[]byte{0x07, 0x14, 0x02, 0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00, 0x07, 'o', 'n', '_', 'i', 'n', 'i', 't', 0x00, 0x02},
		[]byte{0x0a, 0x25, 0x01, 0x23, 0x01, 0x01, 0x7f,
			0x41, 0x10, 0x41, n, 0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0x41, 0x00, 0x41, 0x80, 0x08, 0x41, 0x80, 0x20, 0x10, 0x00,
			0x21, 0x02,
			0x41, 0x80, 0x08, 0x20, 0x02, 0x10, 0x01,
			0x41, 0x00, 0x0b},
		cat([]byte{0x0b, 6 + n, 0x01, 0x00, 0x41, 0x10, 0x0b, n}, []byte(url)),
	)
}

// ---- helpers ----

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	home := t.TempDir()
	return &Manager{
		pluginsDir: filepath.Join(home, "plugins"),
		dataDir:    filepath.Join(home, "plugins_data"),
		stateFile:  filepath.Join(home, "plugins.json"),
		entries:    make(map[string]*pluginEntry),
		registry:   NewRegistryClient("https://registry.invalid/registry.json"),
	}
}

func dropPlugin(t *testing.T, m *Manager, dir, manifestYAML string, wasm []byte) {
	t.Helper()
	d := filepath.Join(m.pluginsDir, dir)
	if err := os.MkdirAll(d, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "manifest.yaml"), []byte(manifestYAML), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "plugin.wasm"), wasm, 0600); err != nil {
		t.Fatal(err)
	}
}

func writeStates(t *testing.T, m *Manager, states map[string]PluginState) {
	t.Helper()
	for id, st := range states {
		m.entries[id] = &pluginEntry{state: st}
	}
	if err := m.saveStatesLocked(); err != nil {
		t.Fatal(err)
	}
	m.entries = make(map[string]*pluginEntry)
}

type flashRecorder struct {
	mu   sync.Mutex
	msgs []string
}

func (f *flashRecorder) add(msg string) {
	f.mu.Lock()
	f.msgs = append(f.msgs, msg)
	f.mu.Unlock()
}

func (f *flashRecorder) all() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.msgs...)
}

func approvedState() PluginState {
	return PluginState{Enabled: true, PermissionsApproved: true}
}

func testManifest(id string, network []string) Manifest {
	return Manifest{ID: id, Name: "Test " + id, Version: "1.0.0", WasmFile: "plugin.wasm",
		Permissions: PermissionsConfig{Network: network, Storage: []string{"local"}}}
}

// ---- PLG-4: unapproved plugins never run ----

func TestUnapprovedPluginDoesNotRun(t *testing.T) {
	m := newTestManager(t)
	var flashes flashRecorder
	m.SetFlashHandler(flashes.add)
	dropPlugin(t, m, "dropped", "id: dropped\nname: Dropped\nversion: \"1\"\n", wasmFlash)
	// Even a saved "enabled" state must not let it run without approval.
	dropPlugin(t, m, "enabled-only", "id: enabled-only\nname: Enabled\nversion: \"1\"\n", wasmFlash)
	writeStates(t, m, map[string]PluginState{"enabled-only": {Enabled: true}})

	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	for _, id := range []string{"dropped", "enabled-only"} {
		info, ok := m.GetPlugin(id)
		if !ok {
			t.Fatalf("%s not listed", id)
		}
		if info.IsLoaded || info.State.PermissionsApproved {
			t.Errorf("%s: unapproved plugin loaded=%v approved=%v", id, info.IsLoaded, info.State.PermissionsApproved)
		}
		if info.ApprovalNote == "" {
			t.Errorf("%s: expected an approval note", id)
		}
	}
	if info, _ := m.GetPlugin("dropped"); info.State.Enabled {
		t.Errorf("a newly found plugin must start disabled")
	}
	if got := flashes.all(); len(got) != 0 {
		t.Fatalf("unapproved plugin code ran: %v", got)
	}

	if err := m.EnablePlugin("dropped"); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("EnablePlugin on unapproved plugin: got %v, want ErrNotApproved", err)
	}
	if info, _ := m.GetPlugin("dropped"); info.State.Enabled || info.IsLoaded {
		t.Errorf("EnablePlugin changed state of an unapproved plugin")
	}
}

func TestSandboxRefusesUnapprovedStart(t *testing.T) {
	var flashes flashRecorder
	sb, err := NewSandbox(context.Background(), testManifest("unapproved", nil), PluginState{Enabled: true}, wasmFlash, nil, nil, flashes.add, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sb.Close()
	if err := sb.Start(nil); err == nil {
		t.Fatal("Start of an unapproved sandbox succeeded")
	}
	if len(flashes.all()) != 0 || sb.Running() {
		t.Fatal("guest code ran without approval")
	}
}

func TestRevokingApprovalGatesHostCalls(t *testing.T) {
	var flashes flashRecorder
	sb, err := NewSandbox(context.Background(), testManifest("revoked", nil), approvedState(), wasmFlash, nil, nil, flashes.add, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sb.Close()
	if err := sb.Start(nil); err != nil {
		t.Fatal(err)
	}
	before := len(flashes.all())
	sb.UpdateState(PluginState{Enabled: true})
	if err := sb.InvokeHook("on_track_change", []byte("{}")); err == nil {
		t.Error("InvokeHook ran after approval was revoked")
	}
	if len(flashes.all()) != before {
		t.Error("ui_flash went through after approval was revoked")
	}
}

// ---- PLG-7: a folder may not claim another plugin's ID ----

func TestIDSquattingRefused(t *testing.T) {
	m := newTestManager(t)
	var flashes flashRecorder
	m.SetFlashHandler(flashes.add)
	writeStates(t, m, map[string]PluginState{"webhook-broadcaster": {Enabled: true, PermissionsApproved: true}})
	dropPlugin(t, m, "zz-cool-visualizer",
		"id: webhook-broadcaster\nname: Cool Visualizer\nversion: \"1\"\npermissions:\n  network: [\"0.0.0.0/0\"]\n  storage: [\"local\"]\n", wasmFlash)

	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if _, ok := m.GetPlugin("webhook-broadcaster"); ok {
		t.Fatal("plugin from a folder named differently from its ID was accepted")
	}
	if len(flashes.all()) != 0 {
		t.Fatal("squatting plugin ran")
	}
}

// ---- PLG-1: approval is bound to the binary hash and the permission set ----

func TestApprovalBoundToBinaryAndPermissions(t *testing.T) {
	m := newTestManager(t)
	var flashes flashRecorder
	m.SetFlashHandler(flashes.add)
	const mf = "id: bound\nname: Bound\nversion: \"1\"\npermissions:\n  storage: [\"local\"]\n"
	dropPlugin(t, m, "bound", mf, wasmBenign)
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	if err := m.ApprovePermissions("bound", true); err != nil {
		t.Fatal(err)
	}
	if err := m.EnablePlugin("bound"); err != nil {
		t.Fatal(err)
	}
	info, _ := m.GetPlugin("bound")
	if !info.IsLoaded || !info.State.PermissionsApproved || info.State.ApprovedSHA256 != CalculateSHA256(wasmBenign) {
		t.Fatalf("approved plugin not running as expected: %+v", info)
	}
	_ = m.Close()

	// Swap the binary on disk: the approval must not carry over.
	if err := os.WriteFile(filepath.Join(m.pluginsDir, "bound", "plugin.wasm"), wasmFlash, 0600); err != nil {
		t.Fatal(err)
	}
	m2 := newTestManager(t)
	m2.pluginsDir, m2.dataDir, m2.stateFile = m.pluginsDir, m.dataDir, m.stateFile
	m2.SetFlashHandler(flashes.add)
	if err := m2.Init(); err != nil {
		t.Fatal(err)
	}
	info, _ = m2.GetPlugin("bound")
	if info.IsLoaded || info.State.PermissionsApproved || info.ApprovalNote != noteBinary {
		t.Fatalf("tampered binary kept approval: loaded=%v approved=%v note=%q", info.IsLoaded, info.State.PermissionsApproved, info.ApprovalNote)
	}
	if len(flashes.all()) != 0 {
		t.Fatal("tampered binary ran")
	}
	if err := m2.EnablePlugin("bound"); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("EnablePlugin after tamper: %v", err)
	}
	_ = m2.Close()

	// Approve again, then widen the permissions in the manifest.
	m3 := newTestManager(t)
	m3.pluginsDir, m3.dataDir, m3.stateFile = m.pluginsDir, m.dataDir, m.stateFile
	if err := m3.Init(); err != nil {
		t.Fatal(err)
	}
	if err := m3.ApprovePermissions("bound", true); err != nil {
		t.Fatal(err)
	}
	_ = m3.DisablePlugin("bound")
	_ = m3.Close()
	wider := mf + "  network: [\"api.example.com\"]\n"
	if err := os.WriteFile(filepath.Join(m.pluginsDir, "bound", "manifest.yaml"), []byte(wider), 0600); err != nil {
		t.Fatal(err)
	}
	m4 := newTestManager(t)
	m4.pluginsDir, m4.dataDir, m4.stateFile = m.pluginsDir, m.dataDir, m.stateFile
	if err := m4.Init(); err != nil {
		t.Fatal(err)
	}
	defer m4.Close()
	info, _ = m4.GetPlugin("bound")
	if info.State.PermissionsApproved || info.ApprovalNote != notePermissions {
		t.Fatalf("widened permissions kept approval: approved=%v note=%q", info.State.PermissionsApproved, info.ApprovalNote)
	}
	if ch := PermissionChanges(info.State.ApprovedPermissions, info.Manifest.Permissions); len(ch) != 1 || ch[0] != "network: api.example.com" {
		t.Errorf("PermissionChanges = %v", ch)
	}
}

func TestLegacyApprovalWithoutHashIsReset(t *testing.T) {
	m := newTestManager(t)
	dropPlugin(t, m, "legacy", "id: legacy\nname: Legacy\nversion: \"1\"\n", wasmFlash)
	writeStates(t, m, map[string]PluginState{"legacy": {Enabled: true, PermissionsApproved: true}})
	var flashes flashRecorder
	m.SetFlashHandler(flashes.add)
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	info, _ := m.GetPlugin("legacy")
	if info.IsLoaded || info.State.PermissionsApproved || info.ApprovalNote != noteLegacy {
		t.Fatalf("legacy approval honoured: %+v", info)
	}
	if len(flashes.all()) != 0 {
		t.Fatal("legacy-approved plugin ran")
	}
}

func TestBinaryVerifiedAtLoadTime(t *testing.T) {
	m := newTestManager(t)
	var flashes flashRecorder
	m.SetFlashHandler(flashes.add)
	dropPlugin(t, m, "swap", "id: swap\nname: Swap\nversion: \"1\"\n", wasmBenign)
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	if err := m.ApprovePermissions("swap", true); err != nil {
		t.Fatal(err)
	}
	// Swapped after approval but before start (same process, no rescan).
	if err := os.WriteFile(filepath.Join(m.pluginsDir, "swap", "plugin.wasm"), wasmFlash, 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.EnablePlugin("swap"); !errors.Is(err, ErrNotApproved) {
		t.Fatalf("EnablePlugin with swapped binary: %v", err)
	}
	if len(flashes.all()) != 0 {
		t.Fatal("swapped binary ran")
	}
	if info, _ := m.GetPlugin("swap"); info.State.PermissionsApproved {
		t.Fatal("approval survived a binary swap")
	}
}

func TestApprovePermissionsForRejectsStale(t *testing.T) {
	m := newTestManager(t)
	dropPlugin(t, m, "stale", "id: stale\nname: Stale\nversion: \"1\"\n", wasmBenign)
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	shown, _ := m.GetPlugin("stale")
	if err := os.WriteFile(filepath.Join(m.pluginsDir, "stale", "plugin.wasm"), wasmFlash, 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.ApprovePermissionsFor("stale", shown); !errors.Is(err, ErrApprovalStale) {
		t.Fatalf("got %v, want ErrApprovalStale", err)
	}
	if info, _ := m.GetPlugin("stale"); info.State.PermissionsApproved {
		t.Fatal("stale approval was recorded")
	}
	fresh, _ := m.GetPlugin("stale")
	if err := m.ApprovePermissionsFor("stale", fresh); err != nil {
		t.Fatalf("approving the shown version failed: %v", err)
	}
}

func TestRevokeStopsPlugin(t *testing.T) {
	m := newTestManager(t)
	dropPlugin(t, m, "rev", "id: rev\nname: Rev\nversion: \"1\"\n", wasmBenign)
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	_ = m.ApprovePermissions("rev", true)
	if err := m.EnablePlugin("rev"); err != nil {
		t.Fatal(err)
	}
	if info, _ := m.GetPlugin("rev"); !info.IsLoaded {
		t.Fatal("plugin did not start")
	}
	if err := m.ApprovePermissions("rev", false); err != nil {
		t.Fatal(err)
	}
	if info, _ := m.GetPlugin("rev"); info.IsLoaded || info.State.PermissionsApproved {
		t.Fatal("revoking approval left the plugin running")
	}
}

// ---- PLG-2 / PLG-10 API: plugins can be switched off ----

func TestPluginsDisabled(t *testing.T) {
	m := newTestManager(t)
	dropPlugin(t, m, "off", "id: off\nname: Off\nversion: \"1\"\n", wasmBenign)
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	_ = m.ApprovePermissions("off", true)
	_ = m.EnablePlugin("off")
	_ = m.Close()

	m2 := newTestManager(t)
	m2.pluginsDir, m2.dataDir, m2.stateFile = m.pluginsDir, m.dataDir, m.stateFile
	m2.SetPluginsDisabled(true)
	if err := m2.Init(); err != nil {
		t.Fatal(err)
	}
	defer m2.Close()
	if info, _ := m2.GetPlugin("off"); info.IsLoaded {
		t.Fatal("plugin started while plugins are disabled")
	}
	if err := m2.EnablePlugin("off"); !errors.Is(err, ErrPluginsDisabled) {
		t.Fatalf("EnablePlugin while disabled: %v", err)
	}
	if err := m2.ApprovePermissions("off", true); !errors.Is(err, ErrPluginsDisabled) {
		t.Fatalf("ApprovePermissions while disabled: %v", err)
	}
}

// ---- PLG-5: guest calls are bounded; Close never hangs ----

func TestStartTimeoutEnforced(t *testing.T) {
	sb, err := NewSandbox(context.Background(), testManifest("loop-start", nil), approvedState(), wasmLoopStart, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sb.Close()
	done := make(chan error, 1)
	go func() { done <- sb.Start(nil) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Start of a looping _start succeeded")
		}
	case <-time.After(InitTimeout + 5*time.Second):
		t.Fatal("Start not bounded by InitTimeout")
	}
	if sb.Running() {
		t.Fatal("timed-out module still marked running")
	}
}

func TestHookTimeoutEnforcedAndCloseReturns(t *testing.T) {
	sb, err := NewSandbox(context.Background(), testManifest("loop-hook", nil), approvedState(), wasmLoopHook, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sb.Start(nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- sb.InvokeHook("on_track_change", []byte(`{}`)) }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("looping hook returned nil")
		}
	case <-time.After(HookTimeout + 5*time.Second):
		t.Fatal("hook not bounded by HookTimeout")
	}
	if sb.Running() || sb.LastError() == "" {
		t.Errorf("after timeout: running=%v lastErr=%q", sb.Running(), sb.LastError())
	}
	_ = sb.Close()

	// Close while a hook is spinning must return promptly.
	sb2, err := NewSandbox(context.Background(), testManifest("loop-hook2", nil), approvedState(), wasmLoopHook, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := sb2.Start(nil); err != nil {
		t.Fatal(err)
	}
	go func() { _ = sb2.InvokeHook("on_track_change", []byte(`{}`)) }()
	time.Sleep(200 * time.Millisecond)
	closed := make(chan struct{})
	go func() { _ = sb2.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(HookTimeout):
		t.Fatal("Close blocked behind a spinning hook")
	}
}

func TestDispatchDoesNotBlockOrPileUp(t *testing.T) {
	sb, err := NewSandbox(context.Background(), testManifest("loop-q", nil), approvedState(), wasmLoopHook, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sb.Close()
	if err := sb.Start(nil); err != nil {
		t.Fatal(err)
	}
	accepted := 0
	start := time.Now()
	for i := 0; i < 1000; i++ {
		if sb.Dispatch("on_track_change", []byte("{}")) {
			accepted++
		}
	}
	if time.Since(start) > time.Second {
		t.Fatal("Dispatch blocked")
	}
	if accepted > eventQueueSize+1 {
		t.Fatalf("queue accepted %d events, bound is %d", accepted, eventQueueSize)
	}
}

const gcChildEnv = "HALPRADIO_PLUGIN_GC_CHILD"

// A spinning guest used to block Go's stop-the-world, freezing the whole
// process at the next GC. The check runs in a child process so a regression
// cannot freeze the test binary itself.
func TestGCNotFrozenBySpinningGuest(t *testing.T) {
	if os.Getenv(gcChildEnv) == "1" {
		sb, err := NewSandbox(context.Background(), testManifest("loop-gc", nil), approvedState(), wasmLoopHook, nil, nil, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := sb.Start(nil); err != nil {
			t.Fatal(err)
		}
		go func() { _ = sb.InvokeHook("on_track_change", []byte(`{}`)) }()
		time.Sleep(300 * time.Millisecond)
		runtime.GC()
		_ = sb.Close()
		return
	}
	if testing.Short() {
		t.Skip("spawns a child process")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGCNotFrozenBySpinningGuest$", "-test.count=1")
	cmd.Env = append(os.Environ(), gcChildEnv+"=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("child froze (GC blocked by spinning guest): %s", out)
	}
	if err != nil {
		t.Fatalf("child failed: %v\n%s", err, out)
	}
}

// ---- PLG-6: modules without memory are rejected, never panic ----

func TestNoMemoryModulesRejected(t *testing.T) {
	for name, w := range map[string][]byte{"alloc": wasmNoMemAlloc, "noalloc": wasmNoMemNoAlloc, "hostcall": wasmNoMemHostCall} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("host panic: %v", r)
				}
			}()
			sb, err := NewSandbox(context.Background(), testManifest("nomem-"+name, nil), approvedState(), w, nil, nil, func(string) {}, nil)
			if err == nil {
				_ = sb.Close()
				t.Fatal("module without linear memory accepted")
			}
		})
	}
}

// ---- PLG-9 / PLG-13: broad CIDR rules never reach local addresses ----

func TestBroadCIDRDeniesLocalTargets(t *testing.T) {
	broad := PermissionsConfig{Network: []string{"0.0.0.0/0", "::/0", "0.0.0.0/1", "100.0.0.0/6"}}
	for _, u := range []string{"http://127.0.0.1:11434/", "http://169.254.169.254/latest/meta-data/", "http://192.168.1.1/", "http://10.0.0.1/", "http://[::1]:8080/", "http://[fe80::1]/"} {
		if broad.CanAccessNetwork(u) {
			t.Errorf("broad CIDR allowed %s", u)
		}
	}
	for _, ip := range []string{"127.0.0.1", "::1", "::ffff:127.0.0.1", "169.254.169.254", "172.16.0.1"} {
		if broad.IsIPAllowed(net.ParseIP(ip)) {
			t.Errorf("broad CIDR IsIPAllowed(%s) = true", ip)
		}
	}
	if !broad.IsIPAllowed(net.ParseIP("93.184.216.34")) {
		t.Error("0.0.0.0/0 should still allow public addresses")
	}
	if got := broad.LocalNetworkRules(); len(got) != 0 {
		t.Errorf("broad rules reported as local: %v", got)
	}

	narrow := PermissionsConfig{Network: []string{"192.168.1.0/24", "127.0.0.1"}}
	if !narrow.IsIPAllowed(net.ParseIP("192.168.1.50")) || !narrow.IsIPAllowed(net.ParseIP("127.0.0.1")) {
		t.Error("explicit narrow local rules must still work")
	}
	if got := narrow.LocalNetworkRules(); len(got) != 2 {
		t.Errorf("LocalNetworkRules = %v, want both rules", got)
	}
	for _, l := range narrow.Summary() {
		if l.Category == "network" && !l.Warning {
			t.Errorf("local rule %q not flagged: %q", l.Rule, l.Text)
		}
	}
}

func TestLoopbackSSRFEndToEndBlocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "LOCAL-ONLY-SECRET")
	}))
	defer srv.Close()
	url := srv.URL + "/admin"
	for _, rules := range [][]string{{"0.0.0.0/0"}, {"*"}, {"0.0.0.0/1", "::/0"}} {
		var flashes flashRecorder
		sb, err := NewSandbox(context.Background(), testManifest("ssrf", rules), approvedState(), wasmFetch(url), nil, nil, flashes.add, nil)
		if err != nil {
			t.Fatal(err)
		}
		_ = sb.Start(nil)
		_ = sb.Close()
		for _, f := range flashes.all() {
			if strings.Contains(f, "LOCAL-ONLY-SECRET") {
				t.Fatalf("network=%v read a loopback service: %q", rules, f)
			}
		}
	}
}

func TestSandboxHTTPClientIgnoresProxy(t *testing.T) {
	sb, err := NewSandbox(context.Background(), testManifest("proxy", []string{"*"}), approvedState(), wasmBenign, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sb.Close()
	tr, ok := sb.httpClient.Transport.(*http.Transport)
	if !ok || tr.Proxy != nil {
		t.Fatal("plugin HTTP client must not use a proxy (it would move the IP check off the destination)")
	}
	rc := NewRegistryClient("")
	if tr, ok := rc.httpClient.Transport.(*http.Transport); !ok || tr.Proxy != nil {
		t.Fatal("registry HTTP client must not use a proxy")
	}
}

// ---- PLG-8 / TTY-5: unsafe manifest text is rejected ----

func TestManifestRejectsUnsafeText(t *testing.T) {
	base := "id: evil-plugin\nversion: \"1\"\n"
	cases := map[string]string{
		"esc in author":       base + "name: Nice\nauthor: \"a\\e]0;pwned\\a\"\n",
		"esc in name":         base + "name: \"Nice\\e[8m\"\n",
		"c1 csi in desc":      base + "name: Nice\ndescription: \"x\\u009b31m\"\n",
		"bidi in name":        base + "name: \"abc\\u202edef\"\n",
		"esc in network":      base + "name: Nice\npermissions:\n  network: [\"api.discord.com\\e[8m\"]\n",
		"junk network rule":   base + "name: Nice\npermissions:\n  network: [\"api.discord.com and everything\"]\n",
		"unknown event":       base + "name: Nice\npermissions:\n  events: [\"on_keypress\"]\n",
		"bad storage":         base + "name: Nice\npermissions:\n  storage: [\"/etc\"]\n",
		"overlong name":       base + "name: \"" + strings.Repeat("n", MaxNameRunes+1) + "\"\n",
		"bad version":         "id: evil-plugin\nname: Nice\nversion: \"1\\e[2J\"\n",
		"javascript homepage": base + "name: Nice\nhomepage: \"javascript:alert(1)\"\n",
	}
	for name, y := range cases {
		if _, err := ParseManifest([]byte(y), ".yaml"); err == nil {
			t.Errorf("%s: manifest accepted", name)
		}
	}
	ok := base + "name: Nice\nauthor: Ana\npermissions:\n  network: [\"api.discord.com\", \"*.example.org\", \"192.168.1.0/24\"]\n  events: [\"on_track_change\"]\n"
	if _, err := ParseManifest([]byte(ok), ".yaml"); err != nil {
		t.Errorf("valid manifest rejected: %v", err)
	}
}

func TestSanitizeStringStripsC1AndBidi(t *testing.T) {
	got := sanitizeString("a\u009b31mb‮c\x1b[2Jd", 100)
	if got != "a31mbc[2Jd" {
		t.Errorf("sanitizeString = %q", got)
	}
	if got := sanitizeString(strings.Repeat("é", 10), 4); got != "éééé" {
		t.Errorf("rune cap: %q", got)
	}
}

// ---- PLG-14: ui_flash is labelled and throttled ----

func TestFlashLabelledAndRateLimited(t *testing.T) {
	var flashes flashRecorder
	mf := testManifest("flasher", nil)
	mf.Name = "Flasher"
	sb, err := NewSandbox(context.Background(), mf, approvedState(), wasmFlash, nil, nil, flashes.add, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sb.Close()
	if err := sb.Start(nil); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		_ = sb.InvokeHook("on_track_change", []byte("{}"))
	}
	got := flashes.all()
	if len(got) == 0 || got[0] != "[Flasher] PWNED" {
		t.Fatalf("flash not labelled: %v", got)
	}
	if len(got) > 2*flashPerSecond {
		t.Fatalf("%d flashes from 21 calls; limit is %d per second", len(got), flashPerSecond)
	}
}

// ---- PLG-3 / PLG-11: registry and install hardening ----

func tlsRegistry(t *testing.T, h http.HandlerFunc) (*httptest.Server, *RegistryClient) {
	t.Helper()
	ts := httptest.NewTLSServer(h)
	t.Cleanup(ts.Close)
	c := NewRegistryClient(ts.URL + "/registry.json")
	c.httpClient = ts.Client()
	return ts, c
}

func TestRegistryRequiresHTTPS(t *testing.T) {
	c := NewRegistryClient("http://registry.invalid/registry.json")
	if _, err := c.FetchRegistry(context.Background()); err == nil || !strings.Contains(err.Error(), "non-https") {
		t.Fatalf("http registry accepted: %v", err)
	}
	if _, err := c.resolveDownloadURL("http://example.org/p.wasm"); err == nil {
		t.Fatal("http download URL accepted")
	}
}

func TestProductionRegistryClientRefusesLoopback(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"plugins":[]}`)
	}))
	defer ts.Close()
	c := NewRegistryClient(ts.URL) // default client: dialer refuses local addresses
	if _, err := c.FetchRegistry(context.Background()); err == nil {
		t.Fatal("registry client connected to a loopback address")
	}
}

func TestFetchRegistryDropsBadEntriesAndBoundsSize(t *testing.T) {
	sum := strings.Repeat("a", 64)
	body := `{"version":"1","plugins":[
	 {"id":"good","name":"Good","version":"1.0.0","author":"a","description":"d","download_url":"https://e.org/g.wasm","checksum":"` + sum + `"},
	 {"id":"good","name":"Dup","version":"1.0.0","download_url":"https://e.org/g.wasm","checksum":"` + sum + `"},
	 {"id":"osc","name":"n","version":"1","description":"d\u001b]52;c;ZWNobyBwd25lZA==\u0007","download_url":"https://e.org/o.wasm","checksum":"` + sum + `"},
	 {"id":"nosum","name":"n","version":"1","download_url":"https://e.org/n.wasm","checksum":"abc"},
	 {"id":"wide","name":"n","version":"1","download_url":"https://e.org/w.wasm","checksum":"` + sum + `","permissions":{"network":["x y"]}}
	]}`
	_, c := tlsRegistry(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) })
	idx, err := c.FetchRegistry(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(idx.Plugins) != 1 || idx.Plugins[0].ID != "good" || idx.Skipped != 4 {
		t.Fatalf("got %d plugins (skipped %d): %+v", len(idx.Plugins), idx.Skipped, idx.Plugins)
	}

	_, big := tlsRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"plugins":[],"pad":"`+strings.Repeat("x", maxRegistryBytes)+`"}`)
	})
	if _, err := big.FetchRegistry(context.Background()); err == nil {
		t.Fatal("oversized registry accepted")
	}
}

func TestInvalidRegistryEntryWritesNothing(t *testing.T) {
	var hits atomic.Int32
	ts, c := tlsRegistry(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write(wasmBenign)
	})
	base := t.TempDir()
	p := RegistryPlugin{ID: "evil", Name: "Nice\x1b[8m", Version: "1.0.0", DownloadURL: ts.URL + "/p.wasm", Checksum: CalculateSHA256(wasmBenign)}
	if err := c.DownloadAndInstall(context.Background(), p, base); err == nil {
		t.Fatal("unsafe registry entry installed")
	}
	if hits.Load() != 0 {
		t.Error("binary was downloaded before the entry was validated")
	}
	if entries, _ := os.ReadDir(base); len(entries) != 0 {
		t.Errorf("files written for a rejected entry: %v", entries)
	}
}

func TestPublisherSignaturePolicy(t *testing.T) {
	ts, c := tlsRegistry(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(wasmBenign) })
	p := RegistryPlugin{ID: "signed", Name: "Signed", Version: "1.0.0", DownloadURL: ts.URL + "/p.wasm",
		Checksum: CalculateSHA256(wasmBenign), Permissions: PermissionsConfig{Storage: []string{"local"}}}

	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signed := p
	signed.Signature = hex.EncodeToString(ed25519.Sign(priv, p.SigningPayload()))

	// No trusted key built in: a declared signature cannot be verified -> refused.
	c.trustedKeys = nil
	if _, _, err := c.FetchVerified(context.Background(), signed); err == nil {
		t.Fatal("declared signature accepted without a trusted key")
	}
	// Unsigned entries fall back to the checksum while no key is built in.
	if _, _, err := c.FetchVerified(context.Background(), p); err != nil {
		t.Fatalf("unsigned entry without trusted keys: %v", err)
	}

	c.trustedKeys = []string{hex.EncodeToString(pub)}
	if _, _, err := c.FetchVerified(context.Background(), signed); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if _, _, err := c.FetchVerified(context.Background(), p); err == nil {
		t.Fatal("unsigned entry accepted although a trusted key is configured")
	}
	widened := signed
	widened.Permissions = PermissionsConfig{Network: []string{"*"}, Storage: []string{"local"}}
	if _, _, err := c.FetchVerified(context.Background(), widened); err == nil {
		t.Fatal("signature accepted after the permissions were widened")
	}
}

func installTestManager(t *testing.T) (*Manager, *atomic.Value, string) {
	t.Helper()
	var cur atomic.Value
	cur.Store(wasmBenign)
	ts, c := tlsRegistry(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(cur.Load().([]byte)) })
	m := newTestManager(t)
	m.registry = c
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	return m, &cur, ts.URL
}

func TestUpdateResetsApproval(t *testing.T) {
	m, cur, url := installTestManager(t)
	var flashes flashRecorder
	m.SetFlashHandler(flashes.add)

	v1 := RegistryPlugin{ID: "scrobble-logger", Name: "Scrobble Logger", Version: "1.0.0", DownloadURL: url + "/p.wasm",
		Checksum: CalculateSHA256(wasmBenign), Permissions: PermissionsConfig{Storage: []string{"local"}}}
	if err := m.InstallFromRegistry(context.Background(), v1); err != nil {
		t.Fatal(err)
	}
	info, _ := m.GetPlugin("scrobble-logger")
	if info.IsLoaded || info.State.Enabled || info.State.PermissionsApproved {
		t.Fatalf("fresh install should be idle until approved: %+v", info.State)
	}
	if err := m.ApprovePermissionsFor("scrobble-logger", info); err != nil {
		t.Fatal(err)
	}
	if err := m.EnablePlugin("scrobble-logger"); err != nil {
		t.Fatal(err)
	}

	cur.Store(wasmFlash)
	v2 := v1
	v2.Version = "1.0.1"
	v2.Checksum = CalculateSHA256(wasmFlash)
	v2.Permissions = PermissionsConfig{Network: []string{"0.0.0.0/0"}, Storage: []string{"local"}}
	if err := m.InstallFromRegistry(context.Background(), v2); err != nil {
		t.Fatal(err)
	}
	info, _ = m.GetPlugin("scrobble-logger")
	if info.State.PermissionsApproved || info.IsLoaded {
		t.Fatalf("update kept approval: approved=%v loaded=%v", info.State.PermissionsApproved, info.IsLoaded)
	}
	if !info.State.Enabled || info.ApprovalNote != noteUpdated {
		t.Errorf("update should keep Enabled and explain re-approval: %+v note=%q", info.State, info.ApprovalNote)
	}
	if len(flashes.all()) != 0 {
		t.Fatal("updated binary ran before re-approval")
	}
	if ch := PermissionChanges(info.State.ApprovedPermissions, info.Manifest.Permissions); len(ch) != 1 {
		t.Errorf("PermissionChanges = %v", ch)
	}
}

func TestDowngradeRefused(t *testing.T) {
	m, cur, url := installTestManager(t)
	_ = cur
	p := RegistryPlugin{ID: "ver", Name: "Ver", Version: "1.2.0", DownloadURL: url + "/p.wasm", Checksum: CalculateSHA256(wasmBenign)}
	if err := m.InstallFromRegistry(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	old := p
	old.Version = "1.1.9"
	if err := m.InstallFromRegistry(context.Background(), old); err == nil {
		t.Fatal("downgrade accepted")
	}
	if info, _ := m.GetPlugin("ver"); info.Manifest.Version != "1.2.0" {
		t.Fatalf("version changed to %s", info.Manifest.Version)
	}
	if err := m.InstallFromRegistryOpts(context.Background(), old, InstallOptions{AllowDowngrade: true}); err != nil {
		t.Fatalf("explicit downgrade: %v", err)
	}
}

func TestUpdateWithSameBinaryKeepsApproval(t *testing.T) {
	m, _, url := installTestManager(t)
	p := RegistryPlugin{ID: "same", Name: "Same", Version: "1.0.0", DownloadURL: url + "/p.wasm", Checksum: CalculateSHA256(wasmBenign)}
	if err := m.InstallFromRegistry(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	info, _ := m.GetPlugin("same")
	if err := m.ApprovePermissionsFor("same", info); err != nil {
		t.Fatal(err)
	}
	if err := m.InstallFromRegistry(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	if info, _ := m.GetPlugin("same"); !info.State.PermissionsApproved {
		t.Fatal("re-running update with an identical entry dropped the approval")
	}
}

// ---- PLG-12 / state file ----

func TestUninstallRemovesStorage(t *testing.T) {
	m := newTestManager(t)
	dropPlugin(t, m, "keeper", "id: keeper\nname: Keeper\nversion: \"1\"\npermissions:\n  storage: [\"local\"]\n", wasmBenign)
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	st, err := NewStorage(m.dataDir, "keeper")
	if err != nil {
		t.Fatal(err)
	}
	_ = st.Set("token", "secret")
	if err := m.UninstallPlugin("keeper"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.dataDir, "keeper")); !os.IsNotExist(err) {
		t.Fatalf("plugin storage left behind after uninstall: %v", err)
	}
}

func TestStateFileIsPrivate(t *testing.T) {
	m := newTestManager(t)
	dropPlugin(t, m, "perm", "id: perm\nname: Perm\nversion: \"1\"\n", wasmBenign)
	if err := os.MkdirAll(filepath.Dir(m.stateFile), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(m.stateFile, []byte(`{"plugins":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if err := m.Init(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	fi, err := os.Stat(m.stateFile)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0600 {
		t.Fatalf("plugins.json mode = %v, want 0600", fi.Mode().Perm())
	}
}
