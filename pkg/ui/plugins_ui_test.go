package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/util"
)

func TestPluginUIFlow(t *testing.T) {
	store := radio.NewStore()
	pm := player.NewManager("auto", 80, nil)
	cfg := util.DefaultConfig()

	m := NewModel(store, pm, cfg)
	pluginMgr := plugin.NewManager("")
	_ = pluginMgr.Init()
	m.SetPluginManager(pluginMgr)

	// Open plugins modal
	m.ShowPluginModal = true
	m.PluginModalTab = 0
	m.PluginCursor = 0

	// Switch tab to registry
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	m = m2.(Model)
	if m.PluginModalTab != 1 {
		t.Errorf("expected PluginModalTab 1, got %d", m.PluginModalTab)
	}

	// Switch back to installed tab
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	m = m2.(Model)
	if m.PluginModalTab != 0 {
		t.Errorf("expected PluginModalTab 0, got %d", m.PluginModalTab)
	}

	// Close modal with esc
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = m2.(Model)
	if m.ShowPluginModal {
		t.Errorf("expected ShowPluginModal to be false after escape")
	}

	// Test Permission Approval Dialog
	m.ShowPermissionApproval = true
	m.ApprovalPlugin = plugin.PluginInfo{
		Manifest: plugin.Manifest{
			ID:      "test-plugin",
			Name:    "Test Plugin",
			Version: "1.0.0",
		},
	}

	// Deny with 'n'
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	m = m2.(Model)
	if m.ShowPermissionApproval {
		t.Errorf("expected ShowPermissionApproval to be closed")
	}
}
