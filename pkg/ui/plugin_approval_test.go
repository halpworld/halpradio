package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
	"github.com/halpworld/halpradio/pkg/ui/components"
	"github.com/halpworld/halpradio/pkg/util"
)

const approvalManifest = `id: test-plugin
name: Test Plugin
version: 1.0.0
author: Someone
permissions:
  network:
    - api.example.com
    - 127.0.0.1
`

// newApprovalModel installs a fake plugin in an isolated config dir and
// returns a model showing its permission modal.
func newApprovalModel(t *testing.T) (Model, *plugin.Manager, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	dir := filepath.Join(util.GetPluginsDir(), "test-plugin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(approvalManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	wasm := filepath.Join(dir, "plugin.wasm")
	if err := os.WriteFile(wasm, []byte("not really wasm"), 0o600); err != nil {
		t.Fatal(err)
	}
	mgr := plugin.NewManager("")
	if err := mgr.Scan(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	info, ok := mgr.GetPlugin("test-plugin")
	if !ok {
		t.Fatal("plugin not found")
	}
	m := NewModel(radio.NewStore(), player.NewMockPlayer(80, nil), util.DefaultConfig())
	m.SetPluginManager(mgr)
	clock := &fakeClock{t: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}
	m.Now = clock.Now
	m.openApproval(info)
	// Past the key guard.
	clock.Advance(2 * time.Second)
	return m, mgr, wasm
}

// fakeClock is an injectable clock for Model.Now.
type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) Advance(d time.Duration) { c.t = c.t.Add(d) }

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

// press sends one key and returns the model and command.
func press(t *testing.T, m Model, k tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()
	nm, cmd := m.Update(k)
	return nm.(Model), cmd
}

// scrollToEnd presses j until the end of the rules has been on screen.
func scrollToEnd(t *testing.T, m Model) Model {
	t.Helper()
	for i := 0; i < 500 && !m.ApprovalSeenEnd; i++ {
		m, _ = press(t, m, runeKey('j'))
	}
	if !m.ApprovalSeenEnd {
		t.Fatal("never reached the end of the rules")
	}
	return m
}

// approveYEnter presses y then Enter and runs the resulting command.
func approveYEnter(t *testing.T, m Model) (Model, bool) {
	t.Helper()
	m = scrollToEnd(t, m)
	m, _ = press(t, m, runeKey('y'))
	m, cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		return m, false
	}
	nm, _ := m.Update(cmd())
	return nm.(Model), true
}

func approved(t *testing.T, mgr *plugin.Manager) bool {
	t.Helper()
	info, _ := mgr.GetPlugin("test-plugin")
	return info.State.PermissionsApproved
}

// TTY-5: Enter alone never approves; it needs "y" first.
func TestPermissionModalEnterDoesNotApprove(t *testing.T) {
	m, mgr, _ := newApprovalModel(t)
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			nm, _ = m.Update(msg)
			m = nm.(Model)
		}
	}
	if approved(t, mgr) {
		t.Fatal("Enter approved the plugin")
	}
	if !m.ShowPermissionApproval {
		t.Error("Enter should leave the modal open")
	}
}

func TestPermissionModalYApprovesShownBinary(t *testing.T) {
	m, mgr, _ := newApprovalModel(t)
	m, ok := approveYEnter(t, m)
	if !ok {
		t.Fatal("expected an approval command")
	}
	if !approved(t, mgr) {
		t.Fatalf("y, Enter did not approve: %q", m.StatusMessage)
	}
}

// The binary changed after the modal was shown: nothing is approved and
// the user is told why.
func TestPermissionModalStaleBinaryRefused(t *testing.T) {
	m, mgr, wasm := newApprovalModel(t)
	if err := os.WriteFile(wasm, []byte("swapped binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, ok := approveYEnter(t, m)
	if !ok {
		t.Fatal("expected an approval command")
	}
	if approved(t, mgr) {
		t.Fatal("a swapped binary was approved")
	}
	if !strings.Contains(m.StatusMessage, "changed after you reviewed it") {
		t.Errorf("status %q", m.StatusMessage)
	}
}

func TestPermissionModalRendersSanitizedSummary(t *testing.T) {
	m, _, _ := newApprovalModel(t)
	info := m.ApprovalPlugin
	info.Manifest.Name = "Nice\x1b]0;pwned\x07Plugin"
	info.Manifest.Author = "‮evil"
	info.ApprovalNote = "binary changed since approval"
	out := components.RenderPermissionApprovalModal(info, 100, 50, theme.GetTheme("tokyonight"))
	if strings.Contains(out, "\x07") || strings.Contains(out, "\x1b]") || strings.Contains(out, "‮") {
		t.Fatalf("unsanitized manifest text in modal: %q", out)
	}
	for _, want := range []string{"!!", "127.0.0.1", "binary changed since approval", info.WasmSHA256[:12], "[ y ]"} {
		if !strings.Contains(out, want) {
			t.Errorf("modal missing %q", want)
		}
	}
	if !strings.Contains(out, "[ y ] then [ Enter ]") {
		t.Error("modal does not say approval takes y then Enter")
	}
}

// TTY-2: the window title embeds stream metadata and must not carry
// control characters (an OSC title ends at BEL/ESC).
func TestWindowTitleSanitized(t *testing.T) {
	mp := player.NewMockPlayer(80, nil)
	m := NewModel(radio.NewStore(), mp, util.DefaultConfig())
	_ = mp.Play(radio.Station{ID: "s", Name: "Evil\x1b]0;owned\x07Station", URL: "http://example.com/"})
	mp.SetTrack("Track\x1b[2J‮")
	mp.SetStatus(player.StatusPlaying)
	title := m.WindowTitle()
	if strings.ContainsAny(title, "\x1b\x07‮") {
		t.Fatalf("control characters in window title: %q", title)
	}
	if !strings.Contains(title, "Station") {
		t.Errorf("title lost the station name: %q", title)
	}
}
