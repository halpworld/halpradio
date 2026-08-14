package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
	pm := player.NewManager("auto", 80, nil)

	return NewModel(store, pm, cfg)
}

func TestCatalogTabDisplaysAllStations(t *testing.T) {
	m := createTestModel()

	// Initial state on catalog tab (0)
	if m.ActiveTab != 0 {
		t.Fatalf("Expected initial ActiveTab to be 0, got %d", m.ActiveTab)
	}
	if len(m.Stations) != 4 {
		t.Fatalf("Expected 4 stations on catalog tab, got %d", len(m.Stations))
	}

	// Switch to Genres tab (2) and select "Ambient"
	m.SwitchTab(2)
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

	// Switch back to Catalog tab (0)
	m.SwitchTab(0)
	if m.ActiveFocus != FocusMainList {
		t.Errorf("Expected ActiveFocus to be FocusMainList on Catalog tab")
	}

	if len(m.Stations) != 4 {
		t.Fatalf("Expected all 4 stations on Catalog tab despite SelectedGenre, got %d", len(m.Stations))
	}
}

func TestActivitiesTabFiltering(t *testing.T) {
	m := createTestModel()

	// Switch to Activities tab (1)
	m.SwitchTab(1)
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

	// Switch to Activities tab (1)
	m.SwitchTab(1)
	if m.ActiveFocus != FocusSidebar {
		t.Errorf("Expected ActiveFocus to be FocusSidebar when switching to Activities tab")
	}

	// Switch to Genres tab (2)
	m.SwitchTab(2)
	if m.ActiveFocus != FocusSidebar {
		t.Errorf("Expected ActiveFocus to be FocusSidebar when switching to Genres tab")
	}

	// Switch to Favorites tab (3)
	m.SwitchTab(3)
	if m.ActiveFocus != FocusMainList {
		t.Errorf("Expected ActiveFocus to be FocusMainList when switching to Favorites tab")
	}

	// Switch to Catalog tab (0)
	m.SwitchTab(0)
	if m.ActiveFocus != FocusMainList {
		t.Errorf("Expected ActiveFocus to be FocusMainList when switching to Catalog tab")
	}
}

func TestUpDownNavigationOnCatalogMovesStationCursor(t *testing.T) {
	m := createTestModel()

	// Ensure we are on Catalog tab
	m.SwitchTab(0)
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
