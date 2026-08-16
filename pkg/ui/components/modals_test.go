package components

import (
	"strings"
	"testing"
	"time"

	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
	"github.com/halpworld/halpradio/pkg/timer"
)

func TestRenderModals(t *testing.T) {
	th := theme.GetTheme("dracula")

	// 1. PR Export Modal
	st := radio.Station{
		ID:       "lofi-station",
		Name:     "Lofi Focus",
		URL:      "http://lofi.stream",
		Genre:    "Lofi",
		Country:  "JP",
		Bitrate:  128,
		Codec:    "MP3",
		Homepage: "https://lofi.example",
	}
	prModal := RenderPRExportModal(st, 80, 24, th)
	if !strings.Contains(prModal, "CONTRIBUTE TO HALPRADIO") || !strings.Contains(prModal, "Lofi Focus") {
		t.Errorf("Expected PR Export modal content, got: %s", prModal)
	}

	// 2. Theme Picker Modal
	themeModal := RenderThemePickerModal("tokyonight", 80, 24, th)
	if !strings.Contains(themeModal, "SELECT COLOR THEME") || !strings.Contains(themeModal, "Tokyo Night") {
		t.Errorf("Expected theme picker modal content, got: %s", themeModal)
	}

	// 3. Add Station Modal
	inputs := []string{"My Radio", "http://stream.example/live", "Chill", "US", "320"}
	addModal := RenderAddStationModal(inputs, 0, "Invalid URL", 80, 24, th)
	if !strings.Contains(addModal, "ADD / EDIT CUSTOM RADIO STATION") || !strings.Contains(addModal, "Invalid URL") {
		t.Errorf("Expected add station modal content, got: %s", addModal)
	}

	// 4. Timer Modals (Screens 0: Dashboard, 1: Sleep Presets, 2: Custom Sleep, 3: Pomodoro Config)
	tm := timer.NewTimer()
	tm.StartPomodoro(timer.DefaultPomodoroConfig())

	configInputs := []string{"25", "5", "15", "4", "focus-station", "break-station", "10"}

	// Screen 0 (Active Timer): Main dashboard
	dashModal := RenderTimerModal(tm, 0, 0, configInputs, 0, "", true, true, 80, 24, th)
	if !strings.Contains(dashModal, "ACTIVE TIMER DASHBOARD") {
		t.Errorf("Expected Active Timer dashboard modal content, got: %s", dashModal)
	}

	// Screen 0 (Inactive Timer): Selection Menu
	inactiveTm := timer.NewTimer()
	menuModal := RenderTimerModal(inactiveTm, 0, 0, configInputs, 0, "", true, true, 80, 24, th)
	if !strings.Contains(menuModal, "TIMER & POMODORO FOCUS MODE") {
		t.Errorf("Expected Selection Menu modal content, got: %s", menuModal)
	}

	// Screen 1: Custom sleep input
	customSleepModal := RenderTimerModal(tm, 1, 0, configInputs, 0, "45", true, true, 80, 24, th)
	if !strings.Contains(customSleepModal, "CUSTOM SLEEP TIMER") {
		t.Errorf("Expected custom sleep modal content, got: %s", customSleepModal)
	}

	// Screen 2: Pomodoro Configuration
	pomoConfigModal := RenderTimerModal(tm, 2, 0, configInputs, 0, "", true, true, 80, 24, th)
	if !strings.Contains(pomoConfigModal, "CONFIGURE POMODORO") {
		t.Errorf("Expected pomodoro configuration modal content, got: %s", pomoConfigModal)
	}

	// Sleep Timer in Dashboard
	sleepTm := timer.NewTimer()
	sleepTm.StartSleep(30*time.Minute, 10*time.Second, true, true, "", 80)
	sleepDashModal := RenderTimerModal(sleepTm, 0, 0, configInputs, 0, "", true, true, 80, 24, th)
	if !strings.Contains(sleepDashModal, "Sleep Countdown") {
		t.Errorf("Expected sleep countdown in dashboard modal, got: %s", sleepDashModal)
	}
}
