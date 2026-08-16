package timer

import (
	"testing"
	"time"
)

func TestSleepTimerLifecycle(t *testing.T) {
	tm := NewTimer()
	if tm.IsActive() {
		t.Errorf("Expected timer to be inactive initially")
	}

	ev := tm.StartSleep(10*time.Minute, 10*time.Second, true, true, "", 80)
	if !tm.IsActive() {
		t.Errorf("Expected timer to be active after StartSleep")
	}
	if tm.Type != TimerSleep {
		t.Errorf("Expected type TimerSleep, got %v", tm.Type)
	}
	if tm.State != StateRunning {
		t.Errorf("Expected state StateRunning, got %v", tm.State)
	}
	if ev.Title != "Sleep Timer Started" {
		t.Errorf("Unexpected start event title: %s", ev.Title)
	}

	// Step forward by 9 minutes and 55 seconds (5 seconds before completion, inside 10s fade window)
	events := tm.Tick(9*time.Minute + 55*time.Second)
	if !tm.IsFading {
		t.Errorf("Expected timer to be in fading state")
	}
	if len(events) == 0 || events[0].Type != EventSleepFadeStart {
		t.Fatalf("Expected EventSleepFadeStart event, got %v", events)
	}

	// Step forward remaining 6 seconds
	events = tm.Tick(6 * time.Second)
	if tm.IsActive() {
		t.Errorf("Expected timer to be inactive after completion")
	}
	var completeEv *Event
	for _, e := range events {
		if e.Type == EventSleepComplete {
			completeEv = &e
			break
		}
	}
	if completeEv == nil {
		t.Fatalf("Expected EventSleepComplete event upon expiration")
	}
	if !completeEv.ShouldStopAudio {
		t.Errorf("Expected ShouldStopAudio to be true on sleep complete")
	}
}

func TestPomodoroCycleProgression(t *testing.T) {
	tm := NewTimer()
	cfg := PomodoroConfig{
		FocusDuration:         25 * time.Minute,
		ShortBreakDuration:    5 * time.Minute,
		LongBreakDuration:     15 * time.Minute,
		CyclesBeforeLongBreak: 3,
		FocusStationID:        "lofi-focus",
		BreakStationID:        "cafe-break",
		AutoStartBreaks:       true,
		AutoStartFocus:        true,
	}

	startEv := tm.StartPomodoro(cfg)
	if startEv.Type != EventFocusStart {
		t.Fatalf("Expected EventFocusStart, got %v", startEv.Type)
	}
	if tm.PomodoroCycle != 1 {
		t.Errorf("Expected PomodoroCycle 1, got %d", tm.PomodoroCycle)
	}
	if tm.PomodoroPhase != PhaseFocus {
		t.Errorf("Expected PhaseFocus, got %s", tm.PomodoroPhase)
	}

	// 1. Tick through sprint #1
	events := tm.Tick(25 * time.Minute)
	if tm.PomodoroPhase != PhaseShortBreak {
		t.Fatalf("Expected PhaseShortBreak after sprint 1, got %s", tm.PomodoroPhase)
	}
	if len(events) < 2 {
		t.Fatalf("Expected focus complete + short break start events, got %v", events)
	}
	if events[0].Type != EventFocusComplete || events[1].Type != EventShortBreakStart {
		t.Errorf("Unexpected event types: %v, %v", events[0].Type, events[1].Type)
	}
	if events[1].StationID != "cafe-break" {
		t.Errorf("Expected break station ID 'cafe-break', got %s", events[1].StationID)
	}

	// 2. Tick through short break #1 -> transitions to sprint #2
	events = tm.Tick(5 * time.Minute)
	if tm.PomodoroPhase != PhaseFocus || tm.PomodoroCycle != 2 {
		t.Fatalf("Expected PhaseFocus cycle 2, got %s cycle %d", tm.PomodoroPhase, tm.PomodoroCycle)
	}

	// 3. Skip sprint #2 -> transitions to short break #2
	events = tm.SkipPhase()
	if tm.PomodoroPhase != PhaseShortBreak || tm.PomodoroCycle != 2 {
		t.Fatalf("Expected PhaseShortBreak cycle 2 after skip, got %s cycle %d", tm.PomodoroPhase, tm.PomodoroCycle)
	}

	// 4. Tick through short break #2 -> transitions to sprint #3
	events = tm.Tick(5 * time.Minute)
	if tm.PomodoroPhase != PhaseFocus || tm.PomodoroCycle != 3 {
		t.Fatalf("Expected PhaseFocus cycle 3, got %s cycle %d", tm.PomodoroPhase, tm.PomodoroCycle)
	}

	// 5. Tick through sprint #3 (3rd and final cycle before long break)
	events = tm.Tick(25 * time.Minute)
	if tm.PomodoroPhase != PhaseLongBreak {
		t.Fatalf("Expected PhaseLongBreak after cycle 3, got %s", tm.PomodoroPhase)
	}
	var longBreakEv *Event
	for _, e := range events {
		if e.Type == EventLongBreakStart {
			longBreakEv = &e
			break
		}
	}
	if longBreakEv == nil {
		t.Fatalf("Expected EventLongBreakStart event")
	}

	// 6. Tick through long break -> resets back to cycle 1 sprint
	events = tm.Tick(15 * time.Minute)
	if tm.PomodoroPhase != PhaseFocus || tm.PomodoroCycle != 1 {
		t.Fatalf("Expected fresh round cycle 1 focus after long break, got %s cycle %d", tm.PomodoroPhase, tm.PomodoroCycle)
	}
}

func TestTimerPauseResumeAndReset(t *testing.T) {
	tm := NewTimer()
	tm.StartSleep(20*time.Minute, 10*time.Second, true, true, "", 80)

	tm.Tick(5 * time.Minute)
	if tm.TimeRemaining != 15*time.Minute {
		t.Errorf("Expected 15m remaining, got %v", tm.TimeRemaining)
	}

	pauseEv := tm.Pause()
	if tm.State != StatePaused || pauseEv.Type != EventTimerPaused {
		t.Errorf("Expected paused state, got %v", tm.State)
	}

	// Tick while paused should not advance timer
	tm.Tick(2 * time.Minute)
	if tm.TimeRemaining != 15*time.Minute {
		t.Errorf("Expected 15m remaining while paused, got %v", tm.TimeRemaining)
	}

	resumeEv := tm.Resume()
	if tm.State != StateRunning || resumeEv.Type != EventTimerResumed {
		t.Errorf("Expected running state, got %v", tm.State)
	}

	tm.ResetCurrentInterval()
	if tm.TimeRemaining != 20*time.Minute {
		t.Errorf("Expected 20m after reset, got %v", tm.TimeRemaining)
	}
}

func TestTimerAddMinutes(t *testing.T) {
	tm := NewTimer()
	tm.StartSleep(10*time.Minute, 10*time.Second, true, true, "", 80)

	tm.AddMinutes(5)
	if tm.TimeRemaining != 15*time.Minute {
		t.Errorf("Expected 15m remaining after AddMinutes(5), got %v", tm.TimeRemaining)
	}

	tm.AddMinutes(-10)
	if tm.TimeRemaining != 5*time.Minute {
		t.Errorf("Expected 5m remaining after AddMinutes(-10), got %v", tm.TimeRemaining)
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		d        time.Duration
		expected string
	}{
		{0, "00:00"},
		{59 * time.Second, "00:59"},
		{5 * time.Minute, "05:00"},
		{25*time.Minute + 15*time.Second, "25:15"},
		{1*time.Hour + 30*time.Minute, "01:30:00"},
	}

	for _, c := range cases {
		got := FormatDuration(c.d)
		if got != c.expected {
			t.Errorf("FormatDuration(%v) = %s; want %s", c.d, got, c.expected)
		}
	}
}

func TestBadgeTextAndProgress(t *testing.T) {
	tm := NewTimer()
	if tm.BadgeText() != "" {
		t.Errorf("Expected empty badge for inactive timer")
	}

	tm.StartPomodoro(PomodoroConfig{
		FocusDuration:         20 * time.Minute,
		ShortBreakDuration:    5 * time.Minute,
		LongBreakDuration:     15 * time.Minute,
		CyclesBeforeLongBreak: 4,
	})

	if tm.BadgeText() != "🍅 20:00 (#1/4)" {
		t.Errorf("Unexpected badge text: %s", tm.BadgeText())
	}
	if tm.Progress() != 0.0 {
		t.Errorf("Expected progress 0.0, got %f", tm.Progress())
	}

	tm.Tick(10 * time.Minute)
	if tm.Progress() != 0.5 {
		t.Errorf("Expected progress 0.5, got %f", tm.Progress())
	}
	if tm.BadgeText() != "🍅 10:00 (#1/4)" {
		t.Errorf("Unexpected badge text: %s", tm.BadgeText())
	}
}
