package ui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TTY-11: a catalog update that removes genres or countries must not leave
// GenreIndex/CountryIndex past the end of the lists; the next key press
// used to index out of range and crash the TUI.
func TestSidebarIndicesClampedAfterCatalogShrinks(t *testing.T) {
	m := createTestModel()
	m.SwitchTab(3)
	m.ActiveFocus = FocusSidebar
	// Genres are sorted: Ambient, Jazz, Rock.
	m.GenreIndex = 3
	m.SelectedGenre = "Rock"
	m.CountryIndex = len(m.Countries)
	m.SelectedCountry = m.Countries[len(m.Countries)-1].Code

	small := []byte(`
stations:
  - id: only-1
    name: "Only One"
    url: "http://example.com/only"
    genre: "Ambient"
    country: "US"
`)
	if err := m.Store.Load(small); err != nil {
		t.Fatal(err)
	}
	m.RefreshStations()

	if m.GenreIndex < 0 || m.GenreIndex > len(m.Genres) {
		t.Fatalf("GenreIndex %d out of range for %d genres", m.GenreIndex, len(m.Genres))
	}
	if m.CountryIndex < 0 || m.CountryIndex > len(m.Countries) {
		t.Fatalf("CountryIndex %d out of range for %d countries", m.CountryIndex, len(m.Countries))
	}
	if m.SelectedGenre != "" {
		t.Errorf("removed genre still selected: %q", m.SelectedGenre)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("key press after catalog shrink panicked: %v", r)
		}
	}()
	for _, k := range []string{"k", "j", "k"} {
		nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		m = nm.(Model)
	}
	m.SwitchTab(2)
	m.ActiveFocus = FocusSidebar
	for _, k := range []string{"k", "j"} {
		nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		m = nm.(Model)
	}
	_ = m.View()
}

// A selection that survives the update keeps pointing at the same value.
func TestSidebarSelectionFollowsValue(t *testing.T) {
	m := createTestModel()
	m.SwitchTab(3)
	m.GenreIndex = 2
	m.SelectedGenre = "Jazz"
	smaller := []byte(`
stations:
  - id: j
    name: "J"
    url: "http://example.com/j"
    genre: "Jazz"
    country: "US"
  - id: r
    name: "R"
    url: "http://example.com/r"
    genre: "Rock"
    country: "DE"
`)
	if err := m.Store.Load(smaller); err != nil {
		t.Fatal(err)
	}
	m.RefreshStations()
	if m.SelectedGenre != "Jazz" || m.GenreIndex != 1 {
		t.Fatalf("selection moved: index %d genre %q", m.GenreIndex, m.SelectedGenre)
	}
}
