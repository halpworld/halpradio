package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestDescribeUpdateMsg(t *testing.T) {
	tests := []struct {
		name      string
		msg       tea.Msg
		wantLabel string
		wantQuiet bool
	}{
		{"tick", TickMsg{}, "TickMsg", true},
		{"mouse", tea.MouseMsg{}, "MouseMsg", true},
		{"key", tea.KeyMsg{Type: tea.KeyEnter}, `KeyMsg enter`, false},
		// LOC-4: typed text and pastes are logged as a length only (a paste
		// is bracketed by bubbletea, hence 15).
		{"typed", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")}, "KeyMsg text runes=1 paste=false", false},
		{"paste", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("sk-secret-key"), Paste: true}, "KeyMsg text runes=15 paste=true", false},
		{"resize", tea.WindowSizeMsg{Width: 120, Height: 40}, "WindowSizeMsg 120x40", false},
		{"other", AutoPauseMsg{}, "ui.AutoPauseMsg", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			label, quiet := describeUpdateMsg(tc.msg)
			if label != tc.wantLabel {
				t.Errorf("expected label %q, got %q", tc.wantLabel, label)
			}
			if quiet != tc.wantQuiet {
				t.Errorf("expected quiet=%t, got %t", tc.wantQuiet, quiet)
			}
		})
	}
}
