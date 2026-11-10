package app

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/util"
)

// isolatePluginHome points the config dir at a temp dir and installs one
// fake plugin there (manifest + arbitrary bytes as plugin.wasm). Nothing is
// started: approval checks only hash the file.
func isolatePluginHome(t *testing.T, manifest string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	dir := filepath.Join(util.GetPluginsDir(), "evil-plugin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plugin.wasm"), []byte("not really wasm"), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

const consentManifest = `id: evil-plugin
name: Evil Plugin
version: 1.0.0
author: Someone
permissions:
  network:
    - api.example.com
    - 127.0.0.1
`

func withPrompt(t *testing.T, tty bool, answer string) {
	t.Helper()
	oldTTY, oldIn := stdinIsTerminal, pluginPromptInput
	stdinIsTerminal = func() bool { return tty }
	pluginPromptInput = strings.NewReader(answer)
	t.Cleanup(func() { stdinIsTerminal, pluginPromptInput = oldTTY, oldIn })
}

func pluginApproved(t *testing.T, id string) bool {
	t.Helper()
	mgr := plugin.NewManager("")
	if err := mgr.Scan(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	defer mgr.Close()
	info, ok := mgr.GetPlugin(id)
	if !ok {
		t.Fatalf("plugin %s not found after scan", id)
	}
	return info.State.PermissionsApproved
}

// PLG-2: enable used to approve silently. Without a terminal and without
// --approve-sha256 it must refuse and leave the plugin unapproved.
func TestPluginEnableNonTTYRequiresApproveFlag(t *testing.T) {
	isolatePluginHome(t, consentManifest)
	withPrompt(t, false, "")

	var out bytes.Buffer
	ok, err := RunPluginCLI([]string{"enable", "evil-plugin"}, &out)
	if ok || err == nil {
		t.Fatalf("enable without --approve-sha256 on non-TTY should fail, got ok=%v err=%v\n%s", ok, err, out.String())
	}
	if pluginApproved(t, "evil-plugin") {
		t.Fatal("plugin was approved without consent")
	}
	s := out.String()
	for _, want := range []string{"api.example.com", "WARNING", "!!", "--approve-sha256"} {
		if !strings.Contains(s, want) {
			t.Errorf("prompt output missing %q:\n%s", want, s)
		}
	}
}

func TestPluginApproveTTYDefaultsToNo(t *testing.T) {
	isolatePluginHome(t, consentManifest)
	// FR-A-8: only the full word "yes" approves; "y" no longer does.
	for _, answer := range []string{"\n", "n\n", "", "y\n", "yy\n", "yes please\n"} {
		withPrompt(t, true, answer)
		var out bytes.Buffer
		_, err := RunPluginCLI([]string{"approve", "evil-plugin"}, &out)
		if !errors.Is(err, errPluginApprovalDeclined) {
			t.Fatalf("answer %q: expected decline, got %v", answer, err)
		}
		if !strings.Contains(out.String(), "Type yes") {
			t.Errorf("expected a type-yes prompt, got:\n%s", out.String())
		}
		if pluginApproved(t, "evil-plugin") {
			t.Fatalf("answer %q approved the plugin", answer)
		}
	}
}

func TestPluginApproveTTYYes(t *testing.T) {
	isolatePluginHome(t, consentManifest)
	withPrompt(t, true, "yes\n")
	var out bytes.Buffer
	if _, err := RunPluginCLI([]string{"approve", "evil-plugin"}, &out); err != nil {
		t.Fatalf("approve: %v\n%s", err, out.String())
	}
	if !pluginApproved(t, "evil-plugin") {
		t.Fatal("expected plugin to be approved after answering yes")
	}
}

func TestPluginApproveFlagNonTTY(t *testing.T) {
	isolatePluginHome(t, consentManifest)
	withPrompt(t, false, "")
	var out bytes.Buffer
	sha := installedWasmSHA(t, "evil-plugin")
	if _, err := RunPluginCLI([]string{"approve", "evil-plugin", "--approve-sha256", sha}, &out); err != nil {
		t.Fatalf("approve --approve-sha256: %v\n%s", err, out.String())
	}
	if !pluginApproved(t, "evil-plugin") {
		t.Fatal("expected --approve-sha256 with the right hash to approve")
	}
	// The permissions are still printed so a log shows what was granted.
	if !strings.Contains(out.String(), "api.example.com") {
		t.Errorf("permissions not printed:\n%s", out.String())
	}
}

// TTY-8: manifest text is printed through the display sanitizer.
func TestPluginCLISanitizesManifestText(t *testing.T) {
	isolatePluginHome(t, consentManifest)
	withPrompt(t, false, "")
	var out bytes.Buffer
	_, _ = RunPluginCLI([]string{"approve", "evil-plugin"}, &out)
	if strings.ContainsAny(out.String(), "\x1b\x07\u009b\u202e") {
		t.Fatalf("control characters reached CLI output: %q", out.String())
	}
}

// PLG-10: plugins_enabled: false blocks approving and enabling.
func TestPluginCLIRespectsPluginsDisabled(t *testing.T) {
	isolatePluginHome(t, consentManifest)
	withPrompt(t, false, "")
	if err := os.MkdirAll(util.GetConfigDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(util.GetConfigFile(), []byte("plugins_enabled: false\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	_, err := RunPluginCLI([]string{"approve", "evil-plugin", "--approve-sha256", installedWasmSHA(t, "evil-plugin")}, &out)
	if !errors.Is(err, plugin.ErrPluginsDisabled) {
		t.Fatalf("expected ErrPluginsDisabled, got %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "Plugins are disabled") {
		t.Errorf("expected a disabled note, got:\n%s", out.String())
	}
	if pluginApproved(t, "evil-plugin") {
		t.Fatal("plugin approved while plugins are disabled")
	}
}

// PLG-10: the TUI startup path honours plugins_enabled: false, and an absent
// key keeps plugins enabled.
func TestSetupAppHonoursPluginsEnabled(t *testing.T) {
	for _, tc := range []struct {
		config       string
		wantDisabled bool
	}{
		{"plugins_enabled: false\n", true},
		{"theme: nord\n", false},
	} {
		isolatePluginHome(t, consentManifest)
		if err := os.MkdirAll(util.GetConfigDir(), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(util.GetConfigFile(), []byte(tc.config), 0o600); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		appInst, done, err := SetupApp([]string{"-ipc=false", "-mpris=false", "-discord=false", "-notifications=false"}, []byte{}, &buf)
		if err != nil || done || appInst == nil {
			t.Fatalf("SetupApp: done=%v err=%v", done, err)
		}
		got := appInst.PluginMgr.PluginsDisabled()
		_ = appInst.PluginMgr.Close()
		if appInst.Desktop != nil {
			_ = appInst.Desktop.Close()
		}
		_ = appInst.Player.Close()
		if got != tc.wantDisabled {
			t.Errorf("config %q: PluginsDisabled()=%v, want %v", tc.config, got, tc.wantDisabled)
		}
	}
}
