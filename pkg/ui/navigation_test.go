package ui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/util"
)

func createTestModel() Model {
	yamlCatalog := []byte(`
stations:
  - id: ambient-1
    name: "Ambient One"
    url: "http://example.com/1"
    genre: "Ambient"
    country: "US"
    bitrate: 128
    codec: "MP3"

  - id: ambient-2
    name: "Ambient Two"
    url: "http://example.com/2"
    genre: "Ambient"
    country: "US"
    bitrate: 128
    codec: "MP3"

  - id: rock-1
    name: "Rock Heavy"
    url: "http://example.com/3"
    genre: "Rock"
    country: "DE"
    bitrate: 320
    codec: "MP3"

  - id: jazz-1
    name: "Smooth Jazz"
    url: "http://example.com/4"
    genre: "Jazz"
    country: "US"
    bitrate: 128
    codec: "MP3"
`)
	store := radio.NewStore()
	_ = store.Load(yamlCatalog)

	cfg := util.DefaultConfig()
	pm := player.NewMockPlayer(80, nil)

	return NewModel(store, pm, cfg)
}

func TestActivitiesLandingPage(t *testing.T) {
	m := createTestModel()

	// Initial state on Activities landing tab (0)
	if m.ActiveTab != 0 {
		t.Fatalf("Expected initial ActiveTab to be 0 (Activities), got %d", m.ActiveTab)
	}
	if m.ActiveFocus != FocusSidebar {
		t.Errorf("Expected initial ActiveFocus to be FocusSidebar on Activities landing page")
	}
	if len(m.Stations) != 4 {
		t.Fatalf("Expected 4 stations on activities tab, got %d", len(m.Stations))
	}

	// Switch to Genres tab (3) and select "Ambient"
	m.SwitchTab(3)
	if m.ActiveFocus != FocusSidebar {
		t.Errorf("Expected ActiveFocus to be FocusSidebar on Genres tab")
	}

	// Select first genre "Ambient"
	m.GenreIndex = 1
	m.SelectedGenre = m.Genres[0] // "Ambient"
	m.RefreshStations()

	if len(m.Stations) != 2 {
		t.Fatalf("Expected 2 Ambient stations on Genres tab, got %d", len(m.Stations))
	}

	// Switch to Catalog tab (1)
	m.SwitchTab(1)
	if m.ActiveFocus != FocusMainList {
		t.Errorf("Expected ActiveFocus to be FocusMainList on Catalog tab")
	}

	if len(m.Stations) != 4 {
		t.Fatalf("Expected all 4 stations on Catalog tab despite SelectedGenre, got %d", len(m.Stations))
	}
}

func TestCountriesTabFiltering(t *testing.T) {
	m := createTestModel()

	// Switch to Countries tab (2)
	m.SwitchTab(2)
	if m.ActiveFocus != FocusSidebar {
		t.Errorf("Expected ActiveFocus to be FocusSidebar on Countries tab")
	}

	// In createTestModel, we have US (3 stations) and DE (1 station)
	if len(m.Countries) != 2 {
		t.Fatalf("Expected 2 countries, got %d", len(m.Countries))
	}

	// Select Germany (DE)
	for i, c := range m.Countries {
		if c.Code == "DE" {
			m.CountryIndex = i + 1
			m.SelectedCountry = "DE"
			break
		}
	}
	m.RefreshStations()

	if len(m.Stations) != 1 || m.Stations[0].Country != "DE" {
		t.Errorf("Expected 1 German station on Countries tab, got %d", len(m.Stations))
	}

	// Press 'C' key to toggle focus between sidebar and main list
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'C'}})
	m = updatedModel.(Model)
	if m.ActiveFocus != FocusMainList {
		t.Errorf("Expected ActiveFocus to switch to FocusMainList after 'C', got %v", m.ActiveFocus)
	}
}

func TestActivitiesTabFiltering(t *testing.T) {
	m := createTestModel()

	// Landing is Activities tab (0)
	if m.ActiveFocus != FocusSidebar {
		t.Errorf("Expected ActiveFocus to be FocusSidebar on Activities tab")
	}

	// Select "programming" activity
	m.ActivityIndex = 1
	m.SelectedActivity = "programming"
	m.RefreshStations()

	// Ambient stations match "programming" via fallback
	if len(m.Stations) < 1 {
		t.Errorf("Expected matching programming stations, got %d", len(m.Stations))
	}
}

func TestTabSwitchingResetsFocusToMainList(t *testing.T) {
	m := createTestModel()

	// Landing is Activities tab (0)
	if m.ActiveFocus != FocusSidebar {
		t.Errorf("Expected ActiveFocus to be FocusSidebar on Activities tab")
	}

	// Switch to Catalog tab (1)
	m.SwitchTab(1)
	if m.ActiveFocus != FocusMainList {
		t.Errorf("Expected ActiveFocus to be FocusMainList when switching to Catalog tab")
	}

	// Switch to Countries tab (2)
	m.SwitchTab(2)
	if m.ActiveFocus != FocusSidebar {
		t.Errorf("Expected ActiveFocus to be FocusSidebar when switching to Countries tab")
	}

	// Switch to Genres tab (3)
	m.SwitchTab(3)
	if m.ActiveFocus != FocusSidebar {
		t.Errorf("Expected ActiveFocus to be FocusSidebar when switching to Genres tab")
	}

	// Switch to Favorites tab (4)
	m.SwitchTab(4)
	if m.ActiveFocus != FocusMainList {
		t.Errorf("Expected ActiveFocus to be FocusMainList when switching to Favorites tab")
	}

	// Switch to Activities tab (0)
	m.SwitchTab(0)
	if m.ActiveFocus != FocusSidebar {
		t.Errorf("Expected ActiveFocus to be FocusSidebar when switching to Activities tab")
	}
}

func TestUpDownNavigationOnCatalogMovesStationCursor(t *testing.T) {
	m := createTestModel()

	// Switch to Catalog tab (1)
	m.SwitchTab(1)
	if m.SelectedIndex != 0 {
		t.Fatalf("Expected SelectedIndex 0, got %d", m.SelectedIndex)
	}

	// Send Down key ('j')
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updatedModel.(Model)

	if m.SelectedIndex != 1 {
		t.Errorf("Expected SelectedIndex 1 after pressing 'j', got %d", m.SelectedIndex)
	}
	if len(m.Stations) != 4 {
		t.Errorf("Expected station list count to remain 4 on catalog tab, got %d", len(m.Stations))
	}

	// Send Down key ('j') again
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updatedModel.(Model)

	if m.SelectedIndex != 2 {
		t.Errorf("Expected SelectedIndex 2 after pressing 'j' again, got %d", m.SelectedIndex)
	}
	if len(m.Stations) != 4 {
		t.Errorf("Expected station list count to remain 4 on catalog tab, got %d", len(m.Stations))
	}

	// Send Up key ('k')
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updatedModel.(Model)

	if m.SelectedIndex != 1 {
		t.Errorf("Expected SelectedIndex 1 after pressing 'k', got %d", m.SelectedIndex)
	}
}

func TestStationNavigationHelpersDirect(t *testing.T) {
	m := createTestModel()
	m.SyncDesktop() // Should be safe with nil Desktop

	mockRunner := func(ctx context.Context, name string, args ...string) error {
		return nil
	}
	desktopMgr := desktop.NewManager(desktop.DesktopConfig{
		NotificationsEnabled: true,
		MPRISEnabled:         false,
		IPCEnabled:           false,
		Runner:               mockRunner,
	}, nil)
	defer desktopMgr.Close()
	m.SetDesktop(desktopMgr)

	// Play next station
	m.PlayNextStation()
	if m.SelectedIndex != 1 {
		t.Errorf("expected selectedIndex 1 after PlayNextStation(), got %d", m.SelectedIndex)
	}

	// Play prev station
	m.PlayPrevStation()
	if m.SelectedIndex != 0 {
		t.Errorf("expected selectedIndex 0 after PlayPrevStation(), got %d", m.SelectedIndex)
	}

	// Toggle play pause
	m.TogglePlayPause()
	m.TogglePlayPause()

	// Empty list safety
	emptyModel := NewModel(radio.NewStore(), player.NewMockPlayer(80, nil), util.DefaultConfig())
	emptyModel.Stations = nil
	emptyModel.PlayNextStation()
	emptyModel.PlayPrevStation()
	emptyModel.TogglePlayPause()
}

func TestGlobeAndTunerNavigation(t *testing.T) {
	m := createTestModel()

	// Switch to Tab 8 (Globe) via '9' key
	updatedModel, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}})
	m = updatedModel.(Model)

	if m.ActiveTab != 8 {
		t.Fatalf("expected ActiveTab=8 after pressing '9', got %d", m.ActiveTab)
	}
	if m.ActiveTuner {
		t.Errorf("expected ActiveTuner=false on initial Globe tab")
	}

	// Rotate globe: 'l' (East), 'h' (West), 'k' (North), 'j' (South)
	initialLon := m.GlobeLon
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updatedModel.(Model)
	if m.GlobeLon != initialLon+6.0 {
		t.Errorf("expected Lon to increase by 6, got %f", m.GlobeLon)
	}

	initialLat := m.GlobeLat
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updatedModel.(Model)
	if m.GlobeLat != initialLat+5.0 {
		t.Errorf("expected Lat to increase by 5, got %f", m.GlobeLat)
	}

	// Zoom in with '+' and zoom out with '-'
	initialZoom := m.GlobeZoom
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'+'}})
	m = updatedModel.(Model)
	if m.GlobeZoom != initialZoom+0.25 {
		t.Errorf("expected Zoom to increase, got %f", m.GlobeZoom)
	}

	// When ExperimentalTuner is false (default), 'F' and '0' do NOT activate tuner
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	m = updatedModel.(Model)
	if m.ActiveTuner {
		t.Errorf("expected ActiveTuner=false when ExperimentalTuner is false")
	}
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m = updatedModel.(Model)
	if m.ActiveTuner {
		t.Errorf("expected ActiveTuner=false when ExperimentalTuner is false")
	}

	// Enable ExperimentalTuner to test experimental tuner navigation
	m.Config.ExperimentalTuner = true

	// Switch to Analog Tuner via 'F'
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	m = updatedModel.(Model)
	if !m.ActiveTuner {
		t.Fatalf("expected ActiveTuner=true after pressing 'F'")
	}

	// Sweep needle: 'l' (right/up), 'h' (left/down)
	initialFreq := m.TunerFreq
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = updatedModel.(Model)
	if m.TunerFreq <= initialFreq {
		t.Errorf("expected TunerFreq to increase on 'l', got %f vs %f", m.TunerFreq, initialFreq)
	}

	// Switch Band via 'b'
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = updatedModel.(Model)
	if m.TunerBand != "AM" {
		t.Errorf("expected band to switch to AM, got %s", m.TunerBand)
	}

	// Toggle back to Globe via 'tab'
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}})
	m = updatedModel.(Model)
	if m.ActiveTuner {
		t.Errorf("expected ActiveTuner=false after pressing 'M'")
	}

	// Switch to Tuner via '0' key
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
	m = updatedModel.(Model)
	if !m.ActiveTuner {
		t.Errorf("expected ActiveTuner=true after pressing '0'")
	}

	// Switch to Tab 1 (Catalog) via '2' key - should cleanly deactivate tuner mode
	updatedModel, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = updatedModel.(Model)
	if m.ActiveTuner {
		t.Errorf("expected ActiveTuner=false after switching to Catalog")
	}
	if m.ActiveTab != 1 {
		t.Errorf("expected ActiveTab=1, got %d", m.ActiveTab)
	}
}
