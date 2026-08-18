package ui

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/timer"
)

func TestTimerModalOpenAndClose(t *testing.T) {
	m := createTestModel()

	if m.ShowTimerModal {
		t.Errorf("Expected timer modal to be closed initially")
	}

	// Press 'z' to open timer modal
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = updatedModel.(Model)

	if !m.ShowTimerModal {
		t.Fatalf("Expected timer modal to open on 'z'")
	}
	if m.TimerModalScreen != 0 {
		t.Errorf("Expected TimerModalScreen 0, got %d", m.TimerModalScreen)
	}

	// Press 'Esc' to close timer modal
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEscape})
	m = updatedModel.(Model)

	if m.ShowTimerModal {
		t.Errorf("Expected timer modal to close on 'Esc'")
	}
}

func TestStartPomodoroFromMenu(t *testing.T) {
	m := createTestModel()

	// Open timer modal
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = updatedModel.(Model)

	// Press '1' to start Pomodoro mode
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m = updatedModel.(Model)

	if m.ShowTimerModal {
		t.Errorf("Expected modal to close after starting Pomodoro")
	}
	if !m.Timer.IsActive() {
		t.Fatalf("Expected timer to be active")
	}
	if m.Timer.Type != timer.TimerPomodoro {
		t.Errorf("Expected type TimerPomodoro, got %v", m.Timer.Type)
	}
	if m.Timer.PomodoroPhase != timer.PhaseFocus {
		t.Errorf("Expected PhaseFocus, got %s", m.Timer.PomodoroPhase)
	}
	if !strings.Contains(m.WindowTitle(), "[🍅") {
		t.Errorf("Expected WindowTitle to have Pomodoro badge, got %q", m.WindowTitle())
	}
	if !strings.Contains(m.Timer.BadgeText(), "🍅") {
		t.Errorf("Expected BadgeText to contain 🍅, got %q", m.Timer.BadgeText())
	}
}

func TestStartSleepTimerFromMenu(t *testing.T) {
	m := createTestModel()

	// Open timer modal
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = updatedModel.(Model)

	// Press '2' to start 15 min sleep timer
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = updatedModel.(Model)

	if !m.Timer.IsActive() {
		t.Fatalf("Expected timer to be active after starting Sleep timer")
	}
	if m.Timer.Type != timer.TimerSleep {
		t.Errorf("Expected TimerSleep, got %v", m.Timer.Type)
	}
	if m.Timer.TotalDuration != 15*time.Minute {
		t.Errorf("Expected 15m duration, got %v", m.Timer.TotalDuration)
	}
	if !strings.Contains(m.WindowTitle(), "[⏳") {
		t.Errorf("Expected WindowTitle to contain [⏳, got %q", m.WindowTitle())
	}
}

func TestActiveTimerDashboardControls(t *testing.T) {
	m := createTestModel()

	// Start Pomodoro
	m.Timer.StartPomodoro(m.Timer.PomodoroCfg)

	// Open active dashboard via 'z'
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = updatedModel.(Model)

	if !m.ShowTimerModal {
		t.Fatalf("Expected timer modal to open")
	}

	// Press 'p' to pause
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updatedModel.(Model)

	if m.Timer.State != timer.StatePaused {
		t.Errorf("Expected timer to be paused, got %v", m.Timer.State)
	}
	if !strings.Contains(m.Timer.BadgeText(), "Paused") {
		t.Errorf("Expected BadgeText to reflect paused state, got %q", m.Timer.BadgeText())
	}

	// Press 'p' to resume
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m = updatedModel.(Model)

	if m.Timer.State != timer.StateRunning {
		t.Errorf("Expected timer to be running after resume, got %v", m.Timer.State)
	}

	// Press '+' to add 5 minutes
	remBefore := m.Timer.TimeRemaining
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	m = updatedModel.(Model)

	if m.Timer.TimeRemaining != remBefore+5*time.Minute {
		t.Errorf("Expected %v remaining, got %v", remBefore+5*time.Minute, m.Timer.TimeRemaining)
	}

	// Press 's' to skip to break phase
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	m = updatedModel.(Model)

	if m.Timer.PomodoroPhase != timer.PhaseShortBreak {
		t.Errorf("Expected PhaseShortBreak after skip, got %s", m.Timer.PomodoroPhase)
	}

	// Press 'c' to cancel timer
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = updatedModel.(Model)

	if m.Timer.IsActive() {
		t.Errorf("Expected timer to be stopped after 'c'")
	}
	if m.ShowTimerModal {
		t.Errorf("Expected modal to close after cancel")
	}
}

func TestCustomSleepTimerInputFlow(t *testing.T) {
	m := createTestModel()

	// Open modal
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'z'}})
	m = updatedModel.(Model)

	// Press '7' for custom duration
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'7'}})
	m = updatedModel.(Model)

	if m.TimerModalScreen != 1 {
		t.Fatalf("Expected TimerModalScreen 1, got %d", m.TimerModalScreen)
	}

	// Type backspace twice then '5', '0' (50 min)
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updatedModel.(Model)
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = updatedModel.(Model)
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	m = updatedModel.(Model)
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m = updatedModel.(Model)

	if m.TimerCustomSleepInput != "50" {
		t.Errorf("Expected TimerCustomSleepInput '50', got %q", m.TimerCustomSleepInput)
	}

	// Press Enter to start
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updatedModel.(Model)

	if !m.Timer.IsActive() {
		t.Fatalf("Expected timer to be active")
	}
	if m.Timer.TotalDuration != 50*time.Minute {
		t.Errorf("Expected 50m duration, got %v", m.Timer.TotalDuration)
	}
}

func TestPomodoroAutoStationSwitching(t *testing.T) {
	cleanupNotif := timer.SetNotificationRunnerForTesting(func(title, msg string) {})
	defer cleanupNotif()
	cleanupBell := timer.SetBellWriterForTesting(io.Discard)
	defer cleanupBell()

	m := createTestModel()

	// Configure focus station and break station
	m.Timer.PomodoroCfg.FocusStationID = "ambient-1"
	m.Timer.PomodoroCfg.BreakStationID = "rock-1"
	m.Timer.PomodoroCfg.FocusDuration = 10 * time.Minute
	m.Timer.PomodoroCfg.ShortBreakDuration = 5 * time.Minute

	// Start Pomodoro
	ev := m.Timer.StartPomodoro(m.Timer.PomodoroCfg)
	m.handleTimerEvent(ev)

	if m.PlayingID != "ambient-1" {
		t.Errorf("Expected PlayingID to be 'ambient-1' on focus start, got %q", m.PlayingID)
	}

	// Advance timer by 10 minutes (finish focus sprint -> start short break)
	events := m.Timer.Tick(10 * time.Minute)
	for _, ev := range events {
		m.handleTimerEvent(ev)
	}

	if m.Timer.PomodoroPhase != timer.PhaseShortBreak {
		t.Errorf("Expected PhaseShortBreak, got %s", m.Timer.PomodoroPhase)
	}
	if m.PlayingID != "rock-1" {
		t.Errorf("Expected auto-switched PlayingID 'rock-1' during break, got %q", m.PlayingID)
	}
}
