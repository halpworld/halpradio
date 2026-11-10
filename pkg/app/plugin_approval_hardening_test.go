package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/util"
)

// installedWasmSHA returns the hex SHA-256 of an installed plugin.wasm.
func installedWasmSHA(t *testing.T, id string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(util.GetPluginsDir(), id, "plugin.wasm"))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// FR-A-8: a bare --approve is refused, with or without a terminal, and the
// output shows the two-step way with the real hash.
func TestPluginBareApproveFlagRefused(t *testing.T) {
	for _, tty := range []bool{false, true} {
		isolatePluginHome(t, consentManifest)
		withPrompt(t, tty, "yes\n")
		var out bytes.Buffer
		_, err := RunPluginCLI([]string{"approve", "evil-plugin", "--approve"}, &out)
		if !errors.Is(err, errBareApproveFlag) {
			t.Fatalf("tty=%v: expected errBareApproveFlag, got %v\n%s", tty, err, out.String())
		}
		if pluginApproved(t, "evil-plugin") {
			t.Fatalf("tty=%v: bare --approve approved the plugin", tty)
		}
		want := "--approve-sha256 " + installedWasmSHA(t, "evil-plugin")
		if !strings.Contains(out.String(), want) {
			t.Errorf("tty=%v: output does not show %q:\n%s", tty, want, out.String())
		}
	}
}

// FR-A-8: --approve-sha256 approves only the exact installed binary.
func TestPluginApproveSHA256MustMatch(t *testing.T) {
	isolatePluginHome(t, consentManifest)
	withPrompt(t, false, "")
	for _, args := range [][]string{
		{"approve", "evil-plugin", "--approve-sha256", strings.Repeat("0", 64)},
		{"approve", "evil-plugin", "--approve-sha256=" + installedWasmSHA(t, "evil-plugin")[:16]},
		{"approve", "evil-plugin", "--approve-sha256"},
	} {
		var out bytes.Buffer
		_, err := RunPluginCLI(args, &out)
		if !errors.Is(err, errApproveHashMismatch) {
			t.Fatalf("%v: expected errApproveHashMismatch, got %v\n%s", args, err, out.String())
		}
		if pluginApproved(t, "evil-plugin") {
			t.Fatalf("%v approved the plugin", args)
		}
	}
	var out bytes.Buffer
	upper := strings.ToUpper(installedWasmSHA(t, "evil-plugin"))
	if _, err := RunPluginCLI([]string{"enable", "evil-plugin", "--approve-sha256=" + upper}, &out); err != nil && !pluginApproved(t, "evil-plugin") {
		t.Fatalf("matching hash did not approve: %v\n%s", err, out.String())
	}
	if !pluginApproved(t, "evil-plugin") {
		t.Fatal("matching hash did not approve")
	}
}

// FR-A-6: a long rule is printed in full (wrapped), never cut, so a host that
// differs only at the end is visible.
func TestPluginCLIPromptShowsLongRuleInFull(t *testing.T) {
	label := strings.Repeat("a", 63)
	host := strings.Join([]string{label, label, label, label}, ".") + ".evil-tail.example"
	host = host[len(host)-253:]
	manifest := "id: evil-plugin\nname: Evil Plugin\nversion: 1.0.0\npermissions:\n  network:\n    - " + host + "\n"
	isolatePluginHome(t, manifest)
	withPrompt(t, false, "")
	var out bytes.Buffer
	_, _ = RunPluginCLI([]string{"approve", "evil-plugin"}, &out)
	joined := strings.Join(strings.Fields(strings.ReplaceAll(out.String(), "\n     ", "")), " ")
	if !strings.Contains(joined, host) {
		t.Fatalf("full host not shown:\nwant %s\n%s", host, out.String())
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "  halpradio plugin approve") {
			continue // the command to copy is kept on one line
		}
		if len([]rune(line)) > pluginPromptWidth+5 {
			t.Errorf("line not wrapped (%d runes): %q", len([]rune(line)), line)
		}
	}
}

// changingReader rewrites the plugin on disk when the prompt reads the
// answer, i.e. between showing the permissions and approving them.
type changingReader struct {
	change func()
	answer *strings.Reader
	done   bool
}

func (r *changingReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		r.change()
	}
	return r.answer.Read(p)
}

// T-1: if the binary changes between display and approval, nothing is
// approved (ErrApprovalStale). This fails if confirmPluginApproval approves
// by ID only (ApprovePermissions(id, true)) instead of binding the approval
// to the PluginInfo it showed.
func TestPluginCLIStaleApprovalRefused(t *testing.T) {
	isolatePluginHome(t, consentManifest)
	dir := filepath.Join(util.GetPluginsDir(), "evil-plugin")
	withPrompt(t, true, "")
	pluginPromptInput = &changingReader{
		change: func() { _ = os.WriteFile(filepath.Join(dir, "plugin.wasm"), []byte("a different binary"), 0o600) },
		answer: strings.NewReader("yes\n"),
	}
	var out bytes.Buffer
	_, err := RunPluginCLI([]string{"approve", "evil-plugin"}, &out)
	if !errors.Is(err, plugin.ErrApprovalStale) {
		t.Fatalf("expected ErrApprovalStale, got %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "nothing was approved") {
		t.Errorf("no stale message:\n%s", out.String())
	}
	if pluginApproved(t, "evil-plugin") {
		t.Fatal("a changed binary was approved")
	}
}

// T-1: a manifest whose permissions change on disk while the prompt is open
// is not approved either: the CLI approves the set it showed, and the next
// scan revokes an approval that no longer matches the manifest.
func TestPluginCLIPermissionChangeDuringPromptNotApproved(t *testing.T) {
	isolatePluginHome(t, consentManifest)
	dir := filepath.Join(util.GetPluginsDir(), "evil-plugin")
	withPrompt(t, true, "")
	pluginPromptInput = &changingReader{
		change: func() {
			_ = os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(consentManifest+"    - 10.0.0.0/8\n"), 0o600)
		},
		answer: strings.NewReader("yes\n"),
	}
	var out bytes.Buffer
	_, _ = RunPluginCLI([]string{"approve", "evil-plugin"}, &out)
	if pluginApproved(t, "evil-plugin") {
		t.Fatalf("the widened permission set was approved:\n%s", out.String())
	}
}

// writeConfig writes config.yaml in the isolated home and returns its path.
func writeConfig(t *testing.T, text string) string {
	t.Helper()
	if err := os.MkdirAll(util.GetConfigDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(util.GetConfigFile(), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return util.GetConfigFile()
}

// FR-A-3: a config.yaml that exists but fails to decode fails closed:
// plugins stay off, commands that save refuse, the file is untouched and a
// warning goes to stderr.
func TestBrokenConfigFailsClosedInCLIAndTUI(t *testing.T) {
	isolatePluginHome(t, consentManifest)
	withPrompt(t, false, "")
	const broken = "plugins_enabled: false\nlastfm_api_key: SECRET\nparty_port: auto\nipc_enabled: false\nmpris_enabled: false\ndiscord_rpc: false\nsong_notifications: false\n"
	path := writeConfig(t, broken)

	var stderr bytes.Buffer
	old := cliStderr
	cliStderr = &stderr
	t.Cleanup(func() { cliStderr = old })

	var buf bytes.Buffer
	appInst, done, err := SetupApp([]string{"-ipc=false", "-mpris=false", "-discord=false", "-notifications=false"}, []byte{}, &buf)
	if err != nil || done || appInst == nil {
		t.Fatalf("SetupApp: done=%v err=%v", done, err)
	}
	disabled := appInst.PluginMgr.PluginsDisabled()
	_ = appInst.PluginMgr.Close()
	if appInst.Desktop != nil {
		_ = appInst.Desktop.Close()
	}
	_ = appInst.Player.Close()
	if !disabled {
		t.Fatal("TUI started with plugins enabled on an unreadable config")
	}
	if !strings.Contains(stderr.String(), "config.yaml could not be read") {
		t.Errorf("no stderr warning: %q", stderr.String())
	}

	var out bytes.Buffer
	if _, err := RunVolume([]string{"50"}, &out); err == nil {
		t.Fatalf("volume 50 succeeded on an unreadable config:\n%s", out.String())
	}
	if !strings.Contains(out.String(), "config.yaml could not be read") {
		t.Errorf("volume did not explain why: %q", out.String())
	}

	out.Reset()
	_, err = RunPluginCLI([]string{"approve", "evil-plugin", "--approve-sha256", installedWasmSHA(t, "evil-plugin")}, &out)
	if err == nil || pluginApproved(t, "evil-plugin") {
		t.Fatalf("plugin approve worked on an unreadable config: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "config.yaml could not be read") {
		t.Errorf("plugin approve did not explain why: %q", out.String())
	}

	data, _ := os.ReadFile(path)
	if string(data) != broken {
		t.Fatalf("config.yaml was changed:\n%s", data)
	}
}

// T-2: party create starts no plugin when plugins_enabled: false. This fails
// if setupPartyApp stops calling SetPluginsDisabled.
func TestPartyCreateHonoursPluginsDisabled(t *testing.T) {
	isolatePluginHome(t, consentManifest)
	// Approve and enable the plugin while plugins are on.
	mgr := plugin.NewManager("")
	if err := mgr.Scan(); err != nil {
		t.Fatal(err)
	}
	info, _ := mgr.GetPlugin("evil-plugin")
	if err := mgr.ApprovePermissionsFor("evil-plugin", info); err != nil {
		t.Fatal(err)
	}
	_ = mgr.EnablePlugin("evil-plugin") // the fake wasm cannot start; the state is saved
	mgr.Close()

	writeConfig(t, "plugins_enabled: false\nipc_enabled: false\nmpris_enabled: false\ndiscord_rpc: false\nsong_notifications: false\nplayer_backend: native\n")
	var out bytes.Buffer
	appInst, done, err := RunParty([]string{"create", "test-room"}, []byte{}, &out)
	if err != nil || done || appInst == nil {
		t.Fatalf("party create: done=%v err=%v\n%s", done, err, out.String())
	}
	defer func() {
		if appInst.PartySession != nil {
			appInst.PartySession.Close()
		}
		if appInst.Desktop != nil {
			_ = appInst.Desktop.Close()
		}
		_ = appInst.Player.Close()
		_ = appInst.PluginMgr.Close()
	}()
	if !appInst.PluginMgr.PluginsDisabled() {
		t.Fatal("party app has plugins enabled despite plugins_enabled: false")
	}
	got, ok := appInst.PluginMgr.GetPlugin("evil-plugin")
	if !ok {
		t.Fatal("plugin not scanned")
	}
	if got.IsLoaded || got.LastError != "" {
		t.Fatalf("plugin start was attempted: loaded=%v err=%q", got.IsLoaded, got.LastError)
	}
}
