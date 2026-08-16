package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/util"
)

func TestAllTabsViewHeightDoesNotOverflowTerminal(t *testing.T) {
	yamlCatalog := []byte(`
stations:
  - id: ambient-1
    name: "Ambient One"
    url: "http://example.com/1"
    genre: "Ambient"
    country: "US"
    bitrate: 128
    codec: "MP3"
  - id: rock-1
    name: "Rock Heavy"
    url: "http://example.com/2"
    genre: "Rock"
    country: "DE"
    bitrate: 320
    codec: "MP3"
`)
	store := radio.NewStore()
	_ = store.Load(yamlCatalog)
	cfg := util.DefaultConfig()
	pm := player.NewManager("auto", 80, nil)

	testSizes := []struct {
		w, h int
	}{
		{50, 18}, // Very small split
		{60, 20}, // Small split
		{70, 22}, // Tmux split pane / narrow terminal
		{80, 24}, // Standard terminal (Ghostty, Terminal.app, iTerm2, Alacritty, WezTerm default)
		{80, 25},
		{90, 28},
		{100, 30},
		{120, 40},
		{160, 50},
		{200, 60},
	}

	tabNames := []string{
		"0: Catalog",
		"1: Activities",
		"2: Genres",
		"3: Favorites",
		"4: RadioBrowser",
		"5: Custom",
		"6: History",
	}

	for _, size := range testSizes {
		for tabIdx, tabName := range tabNames {
			m := NewModel(store, pm, cfg)
			m.Width = size.w
			m.Height = size.h
			m.SwitchTab(tabIdx)

			view := m.View()
			renderedHeight := lipgloss.Height(view)

			if renderedHeight >= size.h {
				lines := strings.Split(view, "\n")
				t.Logf("Total lines: %d", len(lines))
				for i, l := range lines {
					t.Logf("Line %d (w=%d): %q", i, lipgloss.Width(l), l)
				}
				t.Errorf("Size %dx%d Tab %s: Rendered height (%d) is >= terminal height (%d). Causes screen scroll and hides header/title!",
					size.w, size.h, tabName, renderedHeight, size.h)
			}

			lines := strings.Split(view, "\n")
			// Check that no individual line wraps beyond terminal width
			for lineIdx, line := range lines {
				w := lipgloss.Width(line)
				if w > size.w {
					t.Errorf("Size %dx%d Tab %s Line %d: Line width (%d) exceeds terminal width (%d):\n%s",
						size.w, size.h, tabName, lineIdx, w, size.w, line)
				}
			}
		}
	}
}

func TestModalsDoNotOverflowTerminal(t *testing.T) {
	yamlCatalog := []byte(`
stations:
  - id: ambient-1
    name: "Ambient One"
    url: "http://example.com/1"
    genre: "Ambient"
    country: "US"
    bitrate: 128
    codec: "MP3"
`)
	store := radio.NewStore()
	_ = store.Load(yamlCatalog)
	cfg := util.DefaultConfig()
	pm := player.NewManager("auto", 80, nil)

	testSizes := []struct {
		w, h int
	}{
		{70, 22},
		{80, 24},
		{100, 30},
		{120, 40},
	}

	for _, size := range testSizes {
		// Test WhichKey overlay
		{
			m := NewModel(store, pm, cfg)
			m.Width = size.w
			m.Height = size.h
			m.ShowWhichKey = true
			view := m.View()
			if h := lipgloss.Height(view); h > size.h {
				t.Errorf("WhichKey at %dx%d rendered height %d > %d", size.w, size.h, h, size.h)
			}
		}

		// Test PR Export modal
		{
			m := NewModel(store, pm, cfg)
			m.Width = size.w
			m.Height = size.h
			m.ExportStation = m.Stations[0]
			m.ShowPRExport = true
			view := m.View()
			if h := lipgloss.Height(view); h > size.h {
				t.Errorf("PRExport at %dx%d rendered height %d > %d", size.w, size.h, h, size.h)
			}
		}

		// Test Theme Picker modal
		{
			m := NewModel(store, pm, cfg)
			m.Width = size.w
			m.Height = size.h
			m.ShowThemePicker = true
			view := m.View()
			if h := lipgloss.Height(view); h > size.h {
				t.Errorf("ThemePicker at %dx%d rendered height %d > %d", size.w, size.h, h, size.h)
			}
		}

		// Test Add Station modal
		{
			m := NewModel(store, pm, cfg)
			m.Width = size.w
			m.Height = size.h
			m.ShowAddModal = true
			view := m.View()
			if h := lipgloss.Height(view); h > size.h {
				t.Errorf("AddModal at %dx%d rendered height %d > %d", size.w, size.h, h, size.h)
			}
		}

		// Test Timer modal (Menu, Active Dashboard, Custom Sleep, Config)
		for screen := 0; screen <= 2; screen++ {
			m := NewModel(store, pm, cfg)
			m.Width = size.w
			m.Height = size.h
			m.ShowTimerModal = true
			m.TimerModalScreen = screen
			if screen == 0 {
				m.Timer.StartPomodoro(m.Timer.PomodoroCfg)
			}
			view := m.View()
			if h := lipgloss.Height(view); h > size.h {
				t.Errorf("TimerModal (screen %d) at %dx%d rendered height %d > %d", screen, size.w, size.h, h, size.h)
			}
		}
	}
}
