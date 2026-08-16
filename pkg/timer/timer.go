package timer

import (
	"fmt"
	"time"
)

type TimerType int

const (
	TimerNone TimerType = iota
	TimerSleep
	TimerPomodoro
)

type PomodoroPhase string

const (
	PhaseFocus      PomodoroPhase = "Focus"
	PhaseShortBreak PomodoroPhase = "Short Break"
	PhaseLongBreak  PomodoroPhase = "Long Break"
)

type TimerState string

const (
	StateStopped TimerState = "Stopped"
	StateRunning TimerState = "Running"
	StatePaused  TimerState = "Paused"
)

type EventType string

const (
	EventNone               EventType = "none"
	EventFocusStart         EventType = "focus_start"
	EventFocusComplete      EventType = "focus_complete"
	EventShortBreakStart    EventType = "short_break_start"
	EventShortBreakComplete EventType = "short_break_complete"
	EventLongBreakStart     EventType = "long_break_start"
	EventLongBreakComplete  EventType = "long_break_complete"
	EventSleepFadeStart     EventType = "sleep_fade_start"
	EventSleepComplete      EventType = "sleep_complete"
	EventTimerPaused        EventType = "timer_paused"
	EventTimerResumed       EventType = "timer_resumed"
	EventTimerCancelled     EventType = "timer_cancelled"
)

type Event struct {
	Type                EventType
	Title               string
	Message             string
	Phase               PomodoroPhase
	Cycle               int
	TotalCycles         int
	StationID           string
	StationName         string
	ShouldStopAudio     bool
	ShouldSwitchStation bool
	FadeVolumePercent   float64
}

type PomodoroConfig struct {
	FocusDuration         time.Duration
	ShortBreakDuration    time.Duration
	LongBreakDuration     time.Duration
	CyclesBeforeLongBreak int
	FocusStationID        string
	FocusStationName      string
	BreakStationID        string
	BreakStationName      string
	AutoStartBreaks       bool
	AutoStartFocus        bool
	NotifyDesktop         bool
	NotifyTerminalBell    bool
	CommandHook           string
}

func DefaultPomodoroConfig() PomodoroConfig {
	return PomodoroConfig{
		FocusDuration:         25 * time.Minute,
		ShortBreakDuration:    5 * time.Minute,
		LongBreakDuration:     15 * time.Minute,
		CyclesBeforeLongBreak: 4,
		FocusStationID:        "",
		FocusStationName:      "",
		BreakStationID:        "",
		BreakStationName:      "",
		AutoStartBreaks:       true,
		AutoStartFocus:        true,
		NotifyDesktop:         true,
		NotifyTerminalBell:    true,
		CommandHook:           "",
	}
}

type SleepConfig struct {
	Duration           time.Duration
	FadeDuration       time.Duration
	NotifyDesktop      bool
	NotifyTerminalBell bool
	CommandHook        string
}

func DefaultSleepConfig() SleepConfig {
	return SleepConfig{
		Duration:           30 * time.Minute,
		FadeDuration:       10 * time.Second,
		NotifyDesktop:      true,
		NotifyTerminalBell: true,
		CommandHook:        "",
	}
}

type Timer struct {
	Type             TimerType
	State            TimerState
	PomodoroPhase    PomodoroPhase
	PomodoroCycle    int
	CompletedCycles  int
	TimeRemaining    time.Duration
	TotalDuration    time.Duration
	OriginalVolume   int
	IsFading         bool
	PomodoroCfg      PomodoroConfig
	SleepCfg         SleepConfig
	LastFadeStepTime time.Time
}

func NewTimer() *Timer {
	return &Timer{
		Type:          TimerNone,
		State:         StateStopped,
		PomodoroPhase: PhaseFocus,
		PomodoroCycle: 1,
		PomodoroCfg:   DefaultPomodoroConfig(),
		SleepCfg:      DefaultSleepConfig(),
	}
}

// StartSleep initializes and starts a countdown sleep timer.
func (t *Timer) StartSleep(duration time.Duration, fadeDuration time.Duration, notifyDesktop, notifyBell bool, hook string, currentVolume int) Event {
	if duration <= 0 {
		duration = 30 * time.Minute
	}
	if fadeDuration < 0 {
		fadeDuration = 10 * time.Second
	}
	if fadeDuration > duration {
		fadeDuration = duration / 2
	}
	if currentVolume <= 0 {
		currentVolume = 80
	}

	t.Type = TimerSleep
	t.State = StateRunning
	t.TimeRemaining = duration
	t.TotalDuration = duration
	t.OriginalVolume = currentVolume
	t.IsFading = false
	t.SleepCfg = SleepConfig{
		Duration:           duration,
		FadeDuration:       fadeDuration,
		NotifyDesktop:      notifyDesktop,
		NotifyTerminalBell: notifyBell,
		CommandHook:        hook,
	}

	return Event{
		Type:            EventNone,
		Title:           "Sleep Timer Started",
		Message:         fmt.Sprintf("Playback will stop in %s", FormatDuration(duration)),
		ShouldStopAudio: false,
	}
}

// StartPomodoro initializes and starts a Pomodoro session.
func (t *Timer) StartPomodoro(cfg PomodoroConfig) Event {
	if cfg.FocusDuration <= 0 {
		cfg.FocusDuration = 25 * time.Minute
	}
	if cfg.ShortBreakDuration <= 0 {
		cfg.ShortBreakDuration = 5 * time.Minute
	}
	if cfg.LongBreakDuration <= 0 {
		cfg.LongBreakDuration = 15 * time.Minute
	}
	if cfg.CyclesBeforeLongBreak <= 0 {
		cfg.CyclesBeforeLongBreak = 4
	}

	t.Type = TimerPomodoro
	t.State = StateRunning
	t.PomodoroPhase = PhaseFocus
	t.PomodoroCycle = 1
	t.CompletedCycles = 0
	t.TimeRemaining = cfg.FocusDuration
	t.TotalDuration = cfg.FocusDuration
	t.IsFading = false
	t.PomodoroCfg = cfg

	ev := Event{
		Type:                EventFocusStart,
		Title:               "Pomodoro Focus Started 🍅",
		Message:             fmt.Sprintf("Sprint #1 of %d (%s focus)", cfg.CyclesBeforeLongBreak, FormatDuration(cfg.FocusDuration)),
		Phase:               PhaseFocus,
		Cycle:               1,
		TotalCycles:         cfg.CyclesBeforeLongBreak,
		StationID:           cfg.FocusStationID,
		StationName:         cfg.FocusStationName,
		ShouldSwitchStation: cfg.FocusStationID != "",
	}

	return ev
}

func (t *Timer) Pause() Event {
	if t.State == StateRunning {
		t.State = StatePaused
		return Event{
			Type:    EventTimerPaused,
			Title:   "Timer Paused",
			Message: fmt.Sprintf("Timer paused with %s remaining", FormatDuration(t.TimeRemaining)),
			Phase:   t.PomodoroPhase,
			Cycle:   t.PomodoroCycle,
		}
	}
	return Event{Type: EventNone}
}

func (t *Timer) Resume() Event {
	if t.State == StatePaused {
		t.State = StateRunning
		return Event{
			Type:    EventTimerResumed,
			Title:   "Timer Resumed",
			Message: fmt.Sprintf("Resuming countdown: %s remaining", FormatDuration(t.TimeRemaining)),
			Phase:   t.PomodoroPhase,
			Cycle:   t.PomodoroCycle,
		}
	}
	return Event{Type: EventNone}
}

func (t *Timer) TogglePause() Event {
	if t.State == StateRunning {
		return t.Pause()
	} else if t.State == StatePaused {
		return t.Resume()
	}
	return Event{Type: EventNone}
}

func (t *Timer) Stop() Event {
	if t.State == StateStopped && t.Type == TimerNone {
		return Event{Type: EventNone}
	}

	prevType := t.Type
	t.Type = TimerNone
	t.State = StateStopped
	t.TimeRemaining = 0
	t.TotalDuration = 0
	t.IsFading = false

	return Event{
		Type:    EventTimerCancelled,
		Title:   "Timer Stopped",
		Message: fmt.Sprintf("%s was cancelled", timerTypeName(prevType)),
	}
}

func (t *Timer) ResetCurrentInterval() {
	if t.Type == TimerSleep {
		t.TimeRemaining = t.TotalDuration
		t.IsFading = false
	} else if t.Type == TimerPomodoro {
		switch t.PomodoroPhase {
		case PhaseFocus:
			t.TimeRemaining = t.PomodoroCfg.FocusDuration
			t.TotalDuration = t.PomodoroCfg.FocusDuration
		case PhaseShortBreak:
			t.TimeRemaining = t.PomodoroCfg.ShortBreakDuration
			t.TotalDuration = t.PomodoroCfg.ShortBreakDuration
		case PhaseLongBreak:
			t.TimeRemaining = t.PomodoroCfg.LongBreakDuration
			t.TotalDuration = t.PomodoroCfg.LongBreakDuration
		}
	}
}

func (t *Timer) AddMinutes(mins int) {
	if t.Type != TimerNone && t.State != StateStopped {
		t.TimeRemaining += time.Duration(mins) * time.Minute
		if t.TimeRemaining < time.Second {
			t.TimeRemaining = time.Second
		}
		if t.TimeRemaining > t.TotalDuration {
			t.TotalDuration = t.TimeRemaining
		}
		if t.TimeRemaining > t.SleepCfg.FadeDuration {
			t.IsFading = false
		}
	}
}

// SkipPhase forces an immediate transition to the next phase in Pomodoro mode.
func (t *Timer) SkipPhase() []Event {
	if t.Type != TimerPomodoro || t.State == StateStopped {
		return nil
	}

	t.TimeRemaining = 0
	return t.advancePomodoroPhase()
}

func (t *Timer) advancePomodoroPhase() []Event {
	var events []Event

	switch t.PomodoroPhase {
	case PhaseFocus:
		events = append(events, Event{
			Type:        EventFocusComplete,
			Title:       "Focus Sprint Completed! 🍅",
			Message:     fmt.Sprintf("Great sprint! Completed cycle #%d of %d.", t.PomodoroCycle, t.PomodoroCfg.CyclesBeforeLongBreak),
			Phase:       PhaseFocus,
			Cycle:       t.PomodoroCycle,
			TotalCycles: t.PomodoroCfg.CyclesBeforeLongBreak,
		})

		t.CompletedCycles++
		if t.PomodoroCycle >= t.PomodoroCfg.CyclesBeforeLongBreak {
			t.PomodoroPhase = PhaseLongBreak
			t.TimeRemaining = t.PomodoroCfg.LongBreakDuration
			t.TotalDuration = t.PomodoroCfg.LongBreakDuration

			events = append(events, Event{
				Type:                EventLongBreakStart,
				Title:               "Long Break Time 🌴",
				Message:             fmt.Sprintf("Relax for %s. You've earned it!", FormatDuration(t.PomodoroCfg.LongBreakDuration)),
				Phase:               PhaseLongBreak,
				Cycle:               t.PomodoroCycle,
				TotalCycles:         t.PomodoroCfg.CyclesBeforeLongBreak,
				StationID:           t.PomodoroCfg.BreakStationID,
				StationName:         t.PomodoroCfg.BreakStationName,
				ShouldSwitchStation: t.PomodoroCfg.BreakStationID != "",
			})
		} else {
			t.PomodoroPhase = PhaseShortBreak
			t.TimeRemaining = t.PomodoroCfg.ShortBreakDuration
			t.TotalDuration = t.PomodoroCfg.ShortBreakDuration

			events = append(events, Event{
				Type:                EventShortBreakStart,
				Title:               "Short Break Time ☕",
				Message:             fmt.Sprintf("Rest for %s before sprint #%d.", FormatDuration(t.PomodoroCfg.ShortBreakDuration), t.PomodoroCycle+1),
				Phase:               PhaseShortBreak,
				Cycle:               t.PomodoroCycle,
				TotalCycles:         t.PomodoroCfg.CyclesBeforeLongBreak,
				StationID:           t.PomodoroCfg.BreakStationID,
				StationName:         t.PomodoroCfg.BreakStationName,
				ShouldSwitchStation: t.PomodoroCfg.BreakStationID != "",
			})
		}

		if !t.PomodoroCfg.AutoStartBreaks {
			t.State = StatePaused
		}

	case PhaseShortBreak:
		events = append(events, Event{
			Type:        EventShortBreakComplete,
			Title:       "Short Break Finished ☕",
			Message:     "Ready to dive back into deep focus?",
			Phase:       PhaseShortBreak,
			Cycle:       t.PomodoroCycle,
			TotalCycles: t.PomodoroCfg.CyclesBeforeLongBreak,
		})

		t.PomodoroCycle++
		t.PomodoroPhase = PhaseFocus
		t.TimeRemaining = t.PomodoroCfg.FocusDuration
		t.TotalDuration = t.PomodoroCfg.FocusDuration

		events = append(events, Event{
			Type:                EventFocusStart,
			Title:               "Deep Focus Session 🍅",
			Message:             fmt.Sprintf("Sprint #%d of %d (%s)", t.PomodoroCycle, t.PomodoroCfg.CyclesBeforeLongBreak, FormatDuration(t.PomodoroCfg.FocusDuration)),
			Phase:               PhaseFocus,
			Cycle:               t.PomodoroCycle,
			TotalCycles:         t.PomodoroCfg.CyclesBeforeLongBreak,
			StationID:           t.PomodoroCfg.FocusStationID,
			StationName:         t.PomodoroCfg.FocusStationName,
			ShouldSwitchStation: t.PomodoroCfg.FocusStationID != "",
		})

		if !t.PomodoroCfg.AutoStartFocus {
			t.State = StatePaused
		}

	case PhaseLongBreak:
		events = append(events, Event{
			Type:        EventLongBreakComplete,
			Title:       "Long Break Finished 🌴",
			Message:     "Pomodoro series complete! Starting fresh round.",
			Phase:       PhaseLongBreak,
			Cycle:       t.PomodoroCycle,
			TotalCycles: t.PomodoroCfg.CyclesBeforeLongBreak,
		})

		t.PomodoroCycle = 1
		t.PomodoroPhase = PhaseFocus
		t.TimeRemaining = t.PomodoroCfg.FocusDuration
		t.TotalDuration = t.PomodoroCfg.FocusDuration

		events = append(events, Event{
			Type:                EventFocusStart,
			Title:               "Deep Focus Session 🍅",
			Message:             fmt.Sprintf("Sprint #1 of %d (%s)", t.PomodoroCfg.CyclesBeforeLongBreak, FormatDuration(t.PomodoroCfg.FocusDuration)),
			Phase:               PhaseFocus,
			Cycle:               1,
			TotalCycles:         t.PomodoroCfg.CyclesBeforeLongBreak,
			StationID:           t.PomodoroCfg.FocusStationID,
			StationName:         t.PomodoroCfg.FocusStationName,
			ShouldSwitchStation: t.PomodoroCfg.FocusStationID != "",
		})

		if !t.PomodoroCfg.AutoStartFocus {
			t.State = StatePaused
		}
	}

	return events
}

// Tick steps the timer by delta and returns any lifecycle events triggered.
func (t *Timer) Tick(delta time.Duration) []Event {
	if t.State != StateRunning || t.Type == TimerNone {
		return nil
	}

	var events []Event
	t.TimeRemaining -= delta

	if t.Type == TimerSleep {
		fadeDur := t.SleepCfg.FadeDuration
		if fadeDur > 0 && t.TimeRemaining <= fadeDur && t.TimeRemaining > 0 {
			if !t.IsFading {
				t.IsFading = true
				events = append(events, Event{
					Type:              EventSleepFadeStart,
					Title:             "Sleep Timer Fade-Out 🌙",
					Message:           "Lowering volume before stopping playback...",
					FadeVolumePercent: float64(t.TimeRemaining) / float64(fadeDur),
				})
			} else {
				events = append(events, Event{
					Type:              EventSleepFadeStart,
					FadeVolumePercent: float64(t.TimeRemaining) / float64(fadeDur),
				})
			}
		}

		if t.TimeRemaining <= 0 {
			t.TimeRemaining = 0
			t.State = StateStopped
			t.Type = TimerNone
			t.IsFading = false

			events = append(events, Event{
				Type:            EventSleepComplete,
				Title:           "Sleep Timer Finished 🌙",
				Message:         "Audio playback stopped. Sweet dreams!",
				ShouldStopAudio: true,
			})
		}
	} else if t.Type == TimerPomodoro {
		if t.TimeRemaining <= 0 {
			t.TimeRemaining = 0
			events = append(events, t.advancePomodoroPhase()...)
		}
	}

	return events
}

// BadgeText returns a concise indicator badge for UI bars.
func (t *Timer) BadgeText() string {
	if t.State == StateStopped || t.Type == TimerNone {
		return ""
	}

	timeStr := FormatDuration(t.TimeRemaining)
	if t.State == StatePaused {
		switch t.Type {
		case TimerSleep:
			return fmt.Sprintf("⏸ %s (Sleep Paused)", timeStr)
		case TimerPomodoro:
			return fmt.Sprintf("⏸ %s (%s Paused)", timeStr, t.PomodoroPhase)
		}
	}

	if t.Type == TimerSleep {
		if t.IsFading {
			return fmt.Sprintf("🌙 %s (Sleep Fade)", timeStr)
		}
		return fmt.Sprintf("⏳ %s (Sleep)", timeStr)
	}

	if t.Type == TimerPomodoro {
		switch t.PomodoroPhase {
		case PhaseFocus:
			return fmt.Sprintf("🍅 %s (#%d/%d)", timeStr, t.PomodoroCycle, t.PomodoroCfg.CyclesBeforeLongBreak)
		case PhaseShortBreak:
			return fmt.Sprintf("☕ %s (Break #%d)", timeStr, t.PomodoroCycle)
		case PhaseLongBreak:
			return fmt.Sprintf("🌴 %s (Long Break)", timeStr)
		}
	}

	return ""
}

// WindowTitleBadge returns a compact prefix for the terminal title.
func (t *Timer) WindowTitleBadge() string {
	if t.State == StateStopped || t.Type == TimerNone {
		return ""
	}
	timeStr := FormatDuration(t.TimeRemaining)
	if t.State == StatePaused {
		return fmt.Sprintf("[⏸ %s] ", timeStr)
	}
	switch t.Type {
	case TimerSleep:
		return fmt.Sprintf("[⏳ %s] ", timeStr)
	case TimerPomodoro:
		switch t.PomodoroPhase {
		case PhaseFocus:
			return fmt.Sprintf("[🍅 %s] ", timeStr)
		case PhaseShortBreak:
			return fmt.Sprintf("[☕ %s] ", timeStr)
		case PhaseLongBreak:
			return fmt.Sprintf("[🌴 %s] ", timeStr)
		}
	}
	return ""
}

// FormattedTime returns mm:ss representation of time remaining.
func (t *Timer) FormattedTime() string {
	return FormatDuration(t.TimeRemaining)
}

// Progress returns the completion ratio (0.0 to 1.0) of the current interval.
func (t *Timer) Progress() float64 {
	if t.TotalDuration <= 0 {
		return 0.0
	}
	elapsed := t.TotalDuration - t.TimeRemaining
	if elapsed < 0 {
		elapsed = 0
	}
	p := float64(elapsed) / float64(t.TotalDuration)
	if p < 0.0 {
		return 0.0
	}
	if p > 1.0 {
		return 1.0
	}
	return p
}

// PhaseDescription returns human-readable status for modals.
func (t *Timer) PhaseDescription() string {
	if t.Type == TimerSleep {
		if t.IsFading {
			return "Sleep Countdown (Volume Fade-out)"
		}
		return "Sleep Countdown"
	}
	if t.Type == TimerPomodoro {
		switch t.PomodoroPhase {
		case PhaseFocus:
			return fmt.Sprintf("Focus Interval #%d of %d", t.PomodoroCycle, t.PomodoroCfg.CyclesBeforeLongBreak)
		case PhaseShortBreak:
			return fmt.Sprintf("Short Rest Break #%d", t.PomodoroCycle)
		case PhaseLongBreak:
			return "Long Rest Break (Cycles Completed)"
		}
	}
	return "Inactive"
}

func (t *Timer) IsActive() bool {
	return t.Type != TimerNone && t.State != StateStopped
}

func timerTypeName(tt TimerType) string {
	switch tt {
	case TimerSleep:
		return "Sleep Timer"
	case TimerPomodoro:
		return "Pomodoro Timer"
	default:
		return "Timer"
	}
}

// FormatDuration formats duration into mm:ss (or hh:mm:ss if >= 1 hour).
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSecs := int(d.Round(time.Second).Seconds())
	hours := totalSecs / 3600
	mins := (totalSecs % 3600) / 60
	secs := totalSecs % 60

	if hours > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", hours, mins, secs)
	}
	return fmt.Sprintf("%02d:%02d", mins, secs)
}
