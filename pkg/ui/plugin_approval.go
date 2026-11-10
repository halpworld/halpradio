package ui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/ui/components"
	"github.com/halpworld/halpradio/pkg/util"
)

// approvalKeyGuard is how long keys are ignored after the permission prompt
// appears, so keys typed for the screen behind it cannot answer it.
const approvalKeyGuard = time.Second

// openApproval shows the permission prompt for info from the top of its
// rule list, disarmed, with the key guard started.
func (m *Model) openApproval(info plugin.PluginInfo) {
	m.ApprovalPlugin = info
	m.ShowPermissionApproval = true
	m.ApprovalScroll = 0
	m.ApprovalSeenEnd = false
	m.ApprovalArmed = false
	m.ApprovalHint = ""
	m.ApprovalShownAt = m.now()
	if m.PendingApprovalID == info.Manifest.ID {
		m.PendingApprovalID = ""
	}
	m.updateApprovalSeen()
}

// closeApproval hides the prompt and clears its state.
func (m *Model) closeApproval() {
	m.ShowPermissionApproval = false
	m.ApprovalArmed = false
	m.ApprovalHint = ""
	m.ApprovalScroll = 0
	m.ApprovalSeenEnd = false
}

// approvalSize is the terminal size the prompt is laid out for; View uses
// the same fallback.
func (m Model) approvalSize() (int, int) {
	if m.Width == 0 || m.Height == 0 {
		return 80, 24
	}
	return m.Width, m.Height
}

// approvalLayout is the prompt's scroll layout at the current size.
func (m Model) approvalLayout() components.PermissionApprovalLayout {
	w, h := m.approvalSize()
	return components.LayoutPermissionApproval(m.ApprovalPlugin, w, h, m.Theme)
}

// updateApprovalSeen clamps the scroll position and records whether the end
// of the rule list has been on screen.
func (m *Model) updateApprovalSeen() {
	if !m.ShowPermissionApproval {
		return
	}
	l := m.approvalLayout()
	m.ApprovalScroll = l.ClampScroll(m.ApprovalScroll)
	if l.EndVisible(m.ApprovalScroll) {
		m.ApprovalSeenEnd = true
	}
}

// approvalInput is what View draws for the prompt.
func (m Model) approvalInput() components.PermissionApprovalInput {
	w, h := m.approvalSize()
	return components.PermissionApprovalInput{
		Info:   m.ApprovalPlugin,
		Scroll: m.ApprovalScroll,
		Armed:  m.ApprovalArmed,
		Hint:   m.ApprovalHint,
		Width:  w,
		Height: h,
	}
}

// handlePermissionApprovalKey handles keys on the permission prompt.
//
//   - n, q and Esc decline at once.
//   - Other keys are ignored for approvalKeyGuard after the prompt appears.
//   - j/k, arrows, PgUp/PgDn scroll the rules.
//   - "y" arms the approval, but only once the end of the rules has been on
//     screen and the window is large enough to review them.
//   - Enter while armed approves; any other key disarms.
//
// The approval covers exactly the PluginInfo the prompt showed; if the
// binary or permissions changed since, the manager refuses it.
func (m Model) handlePermissionApprovalKey(msg tea.KeyMsg) (Model, tea.Cmd) {
	k := msg.String()
	switch k {
	case "n", "N", "esc", "q":
		if m.PluginMgr != nil && m.ApprovalPlugin.Manifest.ID != "" {
			_ = m.PluginMgr.ApprovePermissions(m.ApprovalPlugin.Manifest.ID, false)
			_ = m.PluginMgr.DisablePlugin(m.ApprovalPlugin.Manifest.ID)
			m.StatusMessage = fmt.Sprintf("Plugin %s kept disabled", util.SanitizeDisplay(m.ApprovalPlugin.Manifest.Name, 60))
		}
		m.closeApproval()
		return m, nil
	}

	if m.now().Sub(m.ApprovalShownAt) < approvalKeyGuard {
		return m, nil
	}

	l := m.approvalLayout()
	scroll := func(delta int) {
		m.ApprovalArmed = false
		m.ApprovalHint = ""
		m.ApprovalScroll += delta
		m.updateApprovalSeen()
	}
	page := max(l.Visible-1, 1)
	switch k {
	case "j", "down":
		scroll(1)
		return m, nil
	case "k", "up":
		scroll(-1)
		return m, nil
	case "pgdown", " ", "space", "ctrl+d":
		scroll(page)
		return m, nil
	case "pgup", "ctrl+u":
		scroll(-page)
		return m, nil
	case "end", "G":
		scroll(len(l.Lines))
		return m, nil
	case "home", "g":
		scroll(-len(l.Lines))
		return m, nil
	case "y", "Y":
		switch {
		case l.TooSmall:
			m.ApprovalArmed = false
			m.ApprovalHint = "Enlarge the window to review every permission before approving"
		case !m.ApprovalSeenEnd:
			m.ApprovalArmed = false
			m.ApprovalHint = "Scroll to the end (j / PgDn) to review every permission first"
		default:
			m.ApprovalArmed = true
			m.ApprovalHint = ""
		}
		return m, nil
	case "enter":
		if !m.ApprovalArmed || l.TooSmall || !m.ApprovalSeenEnd {
			m.ApprovalArmed = false
			return m, nil
		}
		shown := m.ApprovalPlugin
		m.closeApproval()
		if m.PluginMgr == nil || shown.Manifest.ID == "" {
			return m, nil
		}
		mgr := m.PluginMgr
		id := shown.Manifest.ID
		name := util.SanitizeDisplay(shown.Manifest.Name, 60)
		m.StatusMessage = fmt.Sprintf("⏳ Approving %s…", name)
		return m, func() tea.Msg {
			err := mgr.ApprovePermissionsFor(id, shown)
			if err == nil {
				err = mgr.EnablePlugin(id)
			}
			return pluginApprovedMsg{id: id, name: name, err: err}
		}
	}
	// Any other key cancels an armed approval.
	if m.ApprovalArmed {
		m.ApprovalArmed = false
		m.ApprovalHint = "Approval cancelled. Press y, then Enter, to approve"
	}
	return m, nil
}

// handlePluginInstalled opens the permission prompt for a freshly installed
// plugin only when nothing else is in use; otherwise it leaves the plugin
// pending and says where to review it (FR-A-1).
func (m Model) handlePluginInstalled(msg PluginInstalledMsg) Model {
	if msg.Err != nil {
		m.PluginStatusMsg = "Install error: " + util.SanitizeDisplay(msg.Err.Error(), 200)
		return m
	}
	if m.PluginMgr == nil {
		return m
	}
	info, ok := m.PluginMgr.GetPlugin(msg.PluginID)
	if !ok {
		m.PluginStatusMsg = fmt.Sprintf("✓ Plugin %s installed.", util.SanitizeDisplay(msg.PluginID, 64))
		return m
	}
	name := util.SanitizeDisplay(info.Manifest.Name, 60)
	if name == "" {
		name = util.SanitizeDisplay(info.Manifest.ID, 64)
	}
	if m.busyWithOtherInput() {
		m.PendingApprovalID = info.Manifest.ID
		text := fmt.Sprintf("Plugin %s installed. Open Plugins to review its permissions.", name)
		m.StatusMessage = text
		m.PluginStatusMsg = text
		return m
	}
	m.PluginStatusMsg = fmt.Sprintf("✓ Plugin %s installed. Review its permissions to enable it.", name)
	m.PluginSelectedID = info.Manifest.ID
	m.openApproval(info)
	return m
}

// pluginIndexByID returns the row of id in installed, or -1.
func pluginIndexByID(installed []plugin.PluginInfo, id string) int {
	for i, p := range installed {
		if p.Manifest.ID == id {
			return i
		}
	}
	return -1
}

// pluginCursorFor is the row View highlights on the installed tab: the
// selected plugin's row, or the clamped cursor when nothing is selected.
func (m Model) pluginCursorFor(installed []plugin.PluginInfo) int {
	if m.PluginModalTab != 0 {
		return m.PluginCursor
	}
	if i := pluginIndexByID(installed, m.PluginSelectedID); i >= 0 {
		return i
	}
	return clampIndex(m.PluginCursor, len(installed))
}

func clampIndex(i, n int) int {
	if i >= n {
		i = n - 1
	}
	if i < 0 {
		i = 0
	}
	return i
}

// selectPluginRow moves the installed-tab cursor to row i and records the
// plugin there as the selection.
func (m *Model) selectPluginRow(installed []plugin.PluginInfo, i int) {
	i = clampIndex(i, len(installed))
	m.PluginCursor = i
	if len(installed) == 0 {
		m.PluginSelectedID = ""
		return
	}
	m.PluginSelectedID = installed[i].Manifest.ID
}

// selectedPlugin returns the plugin the user selected. If the selected
// plugin is gone, it returns false and re-clamps the cursor, so the key is
// dropped rather than applied to whatever row is now under the cursor.
func (m *Model) selectedPlugin(installed []plugin.PluginInfo) (plugin.PluginInfo, bool) {
	if m.PluginSelectedID == "" {
		m.selectPluginRow(installed, m.PluginCursor)
		return plugin.PluginInfo{}, false
	}
	i := pluginIndexByID(installed, m.PluginSelectedID)
	if i < 0 {
		m.selectPluginRow(installed, m.PluginCursor)
		m.PluginStatusMsg = "The selected plugin is no longer installed; nothing was done."
		return plugin.PluginInfo{}, false
	}
	m.PluginCursor = i
	return installed[i], true
}

// openPluginModal opens the plugin manager. A plugin installed while the
// user was busy is selected, with a hint to review it.
func (m *Model) openPluginModal() {
	m.ShowPluginModal = true
	m.PluginModalTab = 0
	m.PluginStatusMsg = ""
	var installed []plugin.PluginInfo
	if m.PluginMgr != nil {
		installed = m.PluginMgr.GetPlugins()
	}
	if i := pluginIndexByID(installed, m.PendingApprovalID); i >= 0 {
		m.selectPluginRow(installed, i)
		m.PluginStatusMsg = fmt.Sprintf("Plugin %s was installed. Press p or Enter to review its permissions.",
			util.SanitizeDisplay(installed[i].Manifest.Name, 60))
		return
	}
	m.PendingApprovalID = ""
	m.selectPluginRow(installed, 0)
}

// pluginFlashLabel is the prefix for text a plugin shows in the status
// bar, so it cannot pass as halpradio's own (FR-A-9).
//
// The sandbox prefixes flashes with "[<name>] ". When exactly one loaded
// plugin has that name (or ID) the label names its ID; otherwise it is the
// generic "[plugin] ", since the message carries no plugin ID.
func (m Model) pluginFlashLabel(text string) string {
	name := ""
	if strings.HasPrefix(text, "[") {
		if end := strings.Index(text, "] "); end > 0 {
			name = text[1:end]
		}
	}
	return "[plugin" + m.pluginIDFor(name) + "] "
}

// pluginIDFor returns ":<id>" for the one loaded plugin whose display name or
// ID is name, or "" when there is none or more than one.
func (m Model) pluginIDFor(name string) string {
	if name == "" || m.PluginMgr == nil {
		return ""
	}
	match := ""
	for _, p := range m.PluginMgr.GetPlugins() {
		if !p.IsLoaded {
			continue
		}
		if util.SanitizeDisplay(p.Manifest.Name, 40) == name || p.Manifest.ID == name {
			if match != "" && match != p.Manifest.ID {
				return ""
			}
			match = p.Manifest.ID
		}
	}
	if match == "" {
		return ""
	}
	return ":" + util.SanitizeDisplay(match, 64)
}

// pluginNotificationLabel is the "[plugin:<id>] " label for a plugin
// notification. The sandbox sends the title as "<name>" or
// "<name>: <title>".
func (m Model) pluginNotificationLabel(title string) string {
	id := m.pluginIDFor(title)
	if id == "" {
		if i := strings.Index(title, ": "); i > 0 {
			id = m.pluginIDFor(title[:i])
		}
	}
	return "[plugin" + id + "] "
}
