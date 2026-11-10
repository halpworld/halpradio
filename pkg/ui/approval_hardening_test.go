package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
	"github.com/halpworld/halpradio/pkg/ui/components"
	"github.com/halpworld/halpradio/pkg/util"
)

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b_[^\x1b]*\x1b\\`)

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

// typeKeys sends each rune and runs any command it returns once.
func typeKeys(t *testing.T, m Model, text string) Model {
	t.Helper()
	for _, r := range text {
		nm, cmd := m.Update(runeKey(r))
		m = nm.(Model)
		if cmd != nil {
			if msg := cmd(); msg != nil {
				if _, isBatch := msg.(tea.BatchMsg); !isBatch {
					nm, _ = m.Update(msg)
					m = nm.(Model)
				}
			}
		}
	}
	return m
}

// installedWhileClosed returns an approval model whose prompt is not open,
// as before the install result arrives.
func installedWhileClosed(t *testing.T) (Model, *plugin.Manager) {
	t.Helper()
	m, mgr, _ := newApprovalModel(t)
	m.closeApproval()
	m.ApprovalPlugin = plugin.PluginInfo{}
	m.Width, m.Height = 120, 40
	return m, mgr
}

// FR-A-1 (PoC A): an install that finishes while the party form is open does
// not open the prompt under it; typing into the form approves nothing.
func TestInstallDuringPartyFormDoesNotOpenPrompt(t *testing.T) {
	m, mgr := installedWhileClosed(t)
	m.openPartyModal()
	m.enterPartyFormScreen(1)
	m.PartyInputFocus = 0
	m.PartyInputs[0] = ""

	nm, _ := m.Update(PluginInstalledMsg{PluginID: "test-plugin"})
	m = nm.(Model)
	if m.ShowPermissionApproval {
		t.Fatal("approval prompt opened over the party form")
	}
	if m.PendingApprovalID != "test-plugin" {
		t.Errorf("plugin not kept pending: %q", m.PendingApprovalID)
	}
	if !strings.Contains(m.StatusMessage, "Open Plugins to review its permissions") {
		t.Errorf("status %q", m.StatusMessage)
	}
	m = typeKeys(t, m, "study")
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if approved(t, mgr) {
		t.Fatal("keys typed into the party form approved the plugin")
	}

	// The prompt is reached from the plugin manager, with the plugin selected.
	m.ShowPartyModal = false
	m.openPluginModal()
	if m.PluginSelectedID != "test-plugin" || !strings.Contains(m.PluginStatusMsg, "review its permissions") {
		t.Fatalf("pending plugin not selected: id=%q status=%q", m.PluginSelectedID, m.PluginStatusMsg)
	}
	m, _ = press(t, m, runeKey('p'))
	if !m.ShowPermissionApproval || m.ApprovalPlugin.Manifest.ID != "test-plugin" {
		t.Fatal("p did not open the pending plugin's prompt")
	}
	if m.PendingApprovalID != "" {
		t.Errorf("pending not cleared after opening the prompt")
	}
}

// FR-A-1 (PoC A2): an install that finishes during a search does not take the
// next "y" of the query.
func TestInstallDuringSearchDoesNotStealKeys(t *testing.T) {
	m, mgr := installedWhileClosed(t)
	m.IsSearching = true
	m = typeKeys(t, m, "jaz")
	nm, _ := m.Update(PluginInstalledMsg{PluginID: "test-plugin"})
	m = nm.(Model)
	if m.ShowPermissionApproval {
		t.Fatal("prompt opened during a search")
	}
	m = typeKeys(t, m, "zy")
	m, _ = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if approved(t, mgr) {
		t.Fatal("search keys approved the plugin")
	}
}

// FR-A-1: with nothing else active, the prompt opens directly, and keys in
// the first second after it appears are ignored.
func TestApprovalKeyGuard(t *testing.T) {
	m, mgr := installedWhileClosed(t)
	clock := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	m.Now = clock.Now
	nm, _ := m.Update(PluginInstalledMsg{PluginID: "test-plugin"})
	m = nm.(Model)
	if !m.ShowPermissionApproval {
		t.Fatal("prompt did not open with nothing else active")
	}
	if !m.ApprovalSeenEnd {
		t.Fatal("precondition: short rule list should be fully visible at 120x40")
	}
	clock.Advance(900 * time.Millisecond)
	m, _ = press(t, m, runeKey('y'))
	m, cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.ApprovalArmed || cmd != nil || !m.ShowPermissionApproval {
		t.Fatal("keys inside the 1 s guard were accepted")
	}
	clock.Advance(200 * time.Millisecond)
	m, ok := approveYEnter(t, m)
	if !ok || !approved(t, mgr) {
		t.Fatalf("y, Enter after the guard did not approve: %q", m.StatusMessage)
	}
}

// FR-A-1: n, q and Esc decline at once, even inside the guard.
func TestApprovalDeclineKeysIgnoreGuard(t *testing.T) {
	for _, k := range []tea.KeyMsg{runeKey('n'), runeKey('q'), {Type: tea.KeyEsc}} {
		m, mgr, _ := newApprovalModel(t)
		m.ApprovalShownAt = m.now() // guard active
		m, _ = press(t, m, k)
		if m.ShowPermissionApproval || approved(t, mgr) {
			t.Fatalf("%q did not decline at once", k.String())
		}
	}
}

// FR-A-1: approval needs y then Enter; any other key in between cancels.
func TestApprovalTwoKeys(t *testing.T) {
	m, mgr, _ := newApprovalModel(t)
	m = scrollToEnd(t, m)
	m, cmd := press(t, m, runeKey('y'))
	if cmd != nil || !m.ApprovalArmed || !m.ShowPermissionApproval {
		t.Fatal("y alone must only arm the prompt")
	}
	if !strings.Contains(stripANSI(m.View()), "Press Enter to approve, any other key cancels") {
		t.Error("armed prompt does not say what Enter does")
	}
	m, _ = press(t, m, runeKey('a'))
	if m.ApprovalArmed {
		t.Fatal("another key did not cancel")
	}
	m, cmd = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil || approved(t, mgr) {
		t.Fatal("Enter after a cancel approved")
	}
	m, cmd = press(t, m, runeKey('y'))
	m, _ = press(t, m, runeKey('j'))
	m, cmd = press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("scrolling did not cancel an armed approval")
	}
}

// manyRulesInfo is the test plugin with 32 network rules, the dangerous ones
// last in the manifest.
func manyRulesInfo(info plugin.PluginInfo) plugin.PluginInfo {
	var rules []string
	for i := 0; i < 30; i++ {
		rules = append(rules, fmt.Sprintf("cdn%02d.lyrics-provider.example.org", i))
	}
	info.Manifest.Permissions.Network = append(rules, "192.168.0.0/16", "*")
	return info
}

// FR-A-2: with 32 rules the prompt fits the terminal at every scroll
// position, always shows the plugin name and SHA prefix, and lists the
// wildcard and LAN rules first.
func TestApprovalPromptFitsAndPinsIdentity(t *testing.T) {
	th := theme.GetTheme("tokyonight")
	for _, size := range [][2]int{{80, 24}, {80, 40}, {120, 40}} {
		m, _, _ := newApprovalModel(t)
		info := manyRulesInfo(m.ApprovalPlugin)
		if err := info.Manifest.Permissions.Validate(); err != nil {
			t.Fatalf("manifest would be refused: %v", err)
		}
		m.Width, m.Height = size[0], size[1]
		m.openApproval(info)
		m.ApprovalShownAt = time.Time{}
		sha := info.WasmSHA256[:12]

		first := stripANSI(m.View())
		wild := strings.Index(first, "Any public internet host")
		lan := strings.Index(first, "192.168.0.0/16")
		cdn := strings.Index(first, "cdn00.")
		if wild < 0 || lan < 0 || (cdn >= 0 && (wild > cdn || lan > cdn)) {
			t.Errorf("%v: dangerous rules not first (wild=%d lan=%d cdn=%d)", size, wild, lan, cdn)
		}
		l := components.LayoutPermissionApproval(info, size[0], size[1], th)
		for s := 0; s <= l.MaxScroll+1; s++ {
			m.ApprovalScroll = s
			out := m.View()
			if h := lipgloss.Height(out); h > size[1] {
				t.Fatalf("%v scroll %d: %d lines on a %d-line terminal", size, s, h, size[1])
			}
			plain := stripANSI(out)
			for _, want := range []string{"Plugin: Test Plugin", sha, "[ y ] then [ Enter ]"} {
				if !strings.Contains(plain, want) {
					t.Fatalf("%v scroll %d: %q not on screen", size, s, want)
				}
			}
		}
	}
}

// FR-A-2: "y" is refused, with a hint, until the end of the rules has been
// on screen.
func TestApprovalYRefusedBeforeEnd(t *testing.T) {
	m, mgr, _ := newApprovalModel(t)
	m.Width, m.Height = 80, 24
	m.openApproval(manyRulesInfo(m.ApprovalPlugin))
	m.ApprovalShownAt = time.Time{}
	if m.ApprovalSeenEnd {
		t.Fatal("precondition: 32 rules should not fit at 80x24")
	}
	m, _ = press(t, m, runeKey('y'))
	m, cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.ApprovalArmed || cmd != nil || approved(t, mgr) {
		t.Fatal("y accepted before the end was shown")
	}
	if !strings.Contains(m.ApprovalHint, "Scroll to the end") {
		t.Errorf("hint %q", m.ApprovalHint)
	}
	if !strings.Contains(stripANSI(m.View()), "more below") {
		t.Error("no 'more below' marker")
	}
	m = scrollToEnd(t, m)
	m, _ = press(t, m, runeKey('y'))
	if !m.ApprovalArmed {
		t.Fatal("y refused after the end was shown")
	}
}

// FR-A-2: a window too small to show the identity and 3 rule lines accepts
// no approval and says why.
func TestApprovalWindowTooSmall(t *testing.T) {
	m, mgr, _ := newApprovalModel(t)
	m.Width, m.Height = 80, 12
	m.openApproval(m.ApprovalPlugin)
	m.ApprovalShownAt = time.Time{}
	out := m.View()
	if lipgloss.Height(out) > 12 {
		t.Fatalf("%d lines on a 12-line terminal", lipgloss.Height(out))
	}
	if !strings.Contains(stripANSI(out), components.ApprovalTooSmallText) {
		t.Fatalf("no too-small message:\n%s", stripANSI(out))
	}
	m, _ = press(t, m, runeKey('y'))
	m, cmd := press(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.ApprovalArmed || cmd != nil || approved(t, mgr) {
		t.Fatal("approval taken in a window too small to review it")
	}
	// At the boundary: the smallest height that is not "too small" shows
	// exactly ApprovalMinRuleLines rule lines, and one line less is too small.
	th := theme.GetTheme("tokyonight")
	h := 1
	for ; h < 60; h++ {
		if !components.LayoutPermissionApproval(m.ApprovalPlugin, 80, h, th).TooSmall {
			break
		}
	}
	if got := components.LayoutPermissionApproval(m.ApprovalPlugin, 80, h, th).Visible; got != components.ApprovalMinRuleLines {
		t.Fatalf("smallest reviewable height %d shows %d rule lines, want %d", h, got, components.ApprovalMinRuleLines)
	}
	// Growing the window makes it reviewable.
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = nm.(Model)
	if !m.ApprovalSeenEnd {
		t.Fatal("resize did not update the review state")
	}
}

// FR-A-6: a 253-character rule is shown in full (wrapped), so a host whose
// tail differs is visible.
func TestApprovalShowsLongRuleInFull(t *testing.T) {
	m, _, _ := newApprovalModel(t)
	label := strings.Repeat("a", 63)
	host := strings.Join([]string{label, label, label, label}, ".") + ".evil-tail.example"
	host = host[len(host)-253:]
	info := m.ApprovalPlugin
	info.Manifest.Permissions.Network = []string{host}
	if err := info.Manifest.Permissions.Validate(); err != nil {
		t.Fatalf("manifest would be refused: %v", err)
	}
	for _, w := range []int{80, 120} {
		l := components.LayoutPermissionApproval(info, w, 40, theme.GetTheme("tokyonight"))
		flat := strings.Join(strings.Fields(stripANSI(strings.Join(l.Lines, ""))), "")
		if !strings.Contains(flat, host) {
			t.Fatalf("width %d: full host not shown:\n%s", w, stripANSI(strings.Join(l.Lines, "\n")))
		}
	}
}

// modalFlags lists every overlay flag in the model.
func modalFlags(m *Model) map[string]*bool {
	return map[string]*bool{
		"approval": &m.ShowPermissionApproval,
		"whichkey": &m.ShowWhichKey,
		"prexport": &m.ShowPRExport,
		"art":      &m.ShowArtModal,
		"theme":    &m.ShowThemePicker,
		"add":      &m.ShowAddModal,
		"timer":    &m.ShowTimerModal,
		"eq":       &m.ShowEQModal,
		"party":    &m.ShowPartyModal,
		"plugin":   &m.ShowPluginModal,
		"dj":       &m.ShowDJModal,
	}
}

// FR-A-1: for every pair of overlays (and every text input under them), the
// overlay that is drawn is the one that receives keys.
func TestDrawnModalIsTheOneThatGetsKeys(t *testing.T) {
	base, _, _ := newApprovalModel(t)
	base.closeApproval()
	base.ExportStation = radio.Station{Name: "S", URL: "http://example.com/"}
	names := []string{"approval", "whichkey", "prexport", "art", "theme", "add", "timer", "eq", "party", "plugin", "dj"}
	inputs := []string{"", "search", "chat", "themesearch"}
	only := func(name string) Model {
		m := base
		*modalFlags(&m)[name] = true
		if name == "approval" {
			m.openApproval(base.ApprovalPluginOr())
		}
		return m
	}
	for _, a := range names {
		for _, b := range names {
			for _, in := range inputs {
				m := base
				switch in {
				case "search":
					m.IsSearching = true
				case "chat":
					m.IsChatting = true
				case "themesearch":
					m.ThemeIsSearching = true
				}
				for _, n := range []string{a, b} {
					*modalFlags(&m)[n] = true
					if n == "approval" {
						m.openApproval(base.ApprovalPluginOr())
					}
				}
				kind := m.activeModal()
				drawn := ""
				for _, n := range names {
					ref := only(n)
					ref.IsSearching, ref.IsChatting, ref.ThemeIsSearching = m.IsSearching, m.IsChatting, m.ThemeIsSearching
					if ref.activeModal() == kind && ref.View() == m.View() {
						drawn = n
					}
				}
				if drawn == "" {
					t.Fatalf("%s+%s (%s): drawn overlay does not match activeModal %d", a, b, in, kind)
				}
				// Esc goes to the drawn overlay: its flag is the one that
				// changes, and no other overlay flag does.
				before := map[string]bool{}
				for n, p := range modalFlags(&m) {
					before[n] = *p
				}
				nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
				after := nm.(Model)
				for n, p := range modalFlags(&after) {
					if n != drawn && *p != before[n] {
						t.Fatalf("%s+%s (%s): Esc changed %s while %s was drawn", a, b, in, n, drawn)
					}
				}
			}
		}
	}
}

// ApprovalPluginOr returns the test plugin for re-opening the prompt.
func (m Model) ApprovalPluginOr() plugin.PluginInfo {
	if m.PluginMgr != nil {
		if info, ok := m.PluginMgr.GetPlugin("test-plugin"); ok {
			return info
		}
	}
	return plugin.PluginInfo{Manifest: plugin.Manifest{ID: "test-plugin", Name: "Test Plugin"}}
}

// installMany installs n fake plugins plug-0..plug-n-1 in an isolated home.
func installMany(t *testing.T, n int) *plugin.Manager {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData"))
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("plug-%d", i)
		dir := filepath.Join(util.GetPluginsDir(), id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		man := fmt.Sprintf("id: %s\nname: P%d\nversion: 1.0.0\nauthor: a\n", id, i)
		_ = os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(man), 0o600)
		_ = os.WriteFile(filepath.Join(dir, "plugin.wasm"), []byte("x"), 0o600)
	}
	mgr := plugin.NewManager("")
	if err := mgr.Scan(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return mgr
}

// FR-A-4 (PoC B): plugin manager keys act on the selected plugin ID. When
// the list changes under the cursor, x removes the plugin the user selected,
// and a selection that is gone drops the key.
func TestPluginManagerActsOnSelectedID(t *testing.T) {
	mgr := installMany(t, 6)
	m := NewModel(radio.NewStore(), player.NewMockPlayer(80, nil), util.DefaultConfig())
	m.SetPluginManager(mgr)
	m.Width, m.Height = 120, 40
	m.openPluginModal()
	for i := 0; i < 3; i++ {
		m, _ = press(t, m, runeKey('j'))
	}
	if m.PluginSelectedID != "plug-3" {
		t.Fatalf("selected %q, want plug-3", m.PluginSelectedID)
	}
	// Another plugin above the cursor disappears: the row index now points
	// at plug-4, but x must remove plug-3.
	if err := mgr.UninstallPlugin("plug-0"); err != nil {
		t.Fatal(err)
	}
	m, _ = press(t, m, runeKey('x'))
	if _, ok := mgr.GetPlugin("plug-3"); ok {
		t.Fatal("x did not remove the selected plugin")
	}
	if _, ok := mgr.GetPlugin("plug-4"); !ok {
		t.Fatal("x removed a plugin the user did not select")
	}

	// The selected plugin vanishes: p and Enter do nothing.
	sel := m.PluginSelectedID
	if sel == "" {
		t.Fatal("no selection after uninstall")
	}
	if err := mgr.UninstallPlugin(sel); err != nil {
		t.Fatal(err)
	}
	for _, k := range []tea.KeyMsg{runeKey('p'), {Type: tea.KeyEnter}, runeKey('x')} {
		before := len(mgr.GetPlugins())
		m2, _ := press(t, m, k)
		if m2.ShowPermissionApproval || len(mgr.GetPlugins()) != before {
			t.Fatalf("%q acted on another plugin after the selection vanished", k.String())
		}
		if !strings.Contains(m2.PluginStatusMsg, "no longer installed") {
			t.Errorf("%q: status %q", k.String(), m2.PluginStatusMsg)
		}
		if i := pluginIndexByID(mgr.GetPlugins(), m2.PluginSelectedID); i < 0 {
			t.Errorf("%q: cursor not re-clamped onto an installed plugin", k.String())
		}
	}
}

// FR-A-9: plugin status text is labelled as coming from a plugin.
func TestPluginTextIsLabelled(t *testing.T) {
	m := NewModel(radio.NewStore(), player.NewMockPlayer(80, nil), util.DefaultConfig())
	nm, _ := m.Update(PluginFlashMsg("[halpradio] Update required: run curl … | sh"))
	m = nm.(Model)
	if !strings.HasPrefix(m.StatusMessage, "[plugin] ") {
		t.Fatalf("flash not labelled: %q", m.StatusMessage)
	}
	nm, _ = m.Update(PluginNotificationMsg{Title: "halpradio", Message: "Session expired"})
	m = nm.(Model)
	if !strings.HasPrefix(m.StatusMessage, "[plugin] ") {
		t.Fatalf("notification not labelled: %q", m.StatusMessage)
	}
}
