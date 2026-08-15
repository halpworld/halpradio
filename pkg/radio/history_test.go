package radio

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestParseArtistAndTitle(t *testing.T) {
	tests := []struct {
		input      string
		wantArtist string
		wantTitle  string
	}{
		{
			input:      "Tycho - A Walk",
			wantArtist: "Tycho",
			wantTitle:  "A Walk",
		},
		{
			input:      "Boards of Canada - Dayvan Cowboy",
			wantArtist: "Boards of Canada",
			wantTitle:  "Dayvan Cowboy",
		},
		{
			input:      "Khruangbin : Texas Sun",
			wantArtist: "Khruangbin",
			wantTitle:  "Texas Sun",
		},
		{
			input:      "Solaris by Photay",
			wantArtist: "Photay",
			wantTitle:  "Solaris",
		},
		{
			input:      "SomaFM | Secret Agent",
			wantArtist: "SomaFM",
			wantTitle:  "Secret Agent",
		},
		{
			input:      "Just A Song Title",
			wantArtist: "",
			wantTitle:  "Just A Song Title",
		},
		{
			input:      "   ",
			wantArtist: "",
			wantTitle:  "",
		},
	}

	for _, tt := range tests {
		gotArtist, gotTitle := ParseArtistAndTitle(tt.input)
		if gotArtist != tt.wantArtist || gotTitle != tt.wantTitle {
			t.Errorf("ParseArtistAndTitle(%q) = (%q, %q), want (%q, %q)",
				tt.input, gotArtist, gotTitle, tt.wantArtist, tt.wantTitle)
		}
	}
}

func TestStoreHistoryRecordingAndDeduplication(t *testing.T) {
	store := NewStore()

	// Initial history should be empty
	if len(store.GetHistory()) != 0 {
		t.Fatalf("Expected empty initial history, got %d", len(store.GetHistory()))
	}

	// 1. Add track 1
	e1 := store.AddHistory("groovesalad", "SomaFM Groove Salad", "Tycho - A Walk")
	if e1 == nil {
		t.Fatalf("Expected non-nil entry returned from AddHistory")
	}
	if e1.Artist != "Tycho" || e1.Title != "A Walk" {
		t.Errorf("Unexpected parsed entry: %+v", e1)
	}

	hist := store.GetHistory()
	if len(hist) != 1 {
		t.Fatalf("Expected 1 history item, got %d", len(hist))
	}
	if hist[0].TrackTitle != "Tycho - A Walk" {
		t.Errorf("Expected track title 'Tycho - A Walk', got %q", hist[0].TrackTitle)
	}

	// 2. Sequential duplicate from ICY stream update -> should be ignored (deduplicated)
	eDup := store.AddHistory("groovesalad", "SomaFM Groove Salad", "Tycho - A Walk")
	if eDup != nil {
		t.Errorf("Expected nil when adding duplicate sequential track, got %+v", eDup)
	}
	if len(store.GetHistory()) != 1 {
		t.Errorf("Expected history count to remain 1 after duplicate, got %d", len(store.GetHistory()))
	}

	// 3. Add a new track on the same station
	e2 := store.AddHistory("groovesalad", "SomaFM Groove Salad", "Boards of Canada - Dayvan Cowboy")
	if e2 == nil {
		t.Fatalf("Expected non-nil entry for new song")
	}
	hist = store.GetHistory()
	if len(hist) != 2 {
		t.Fatalf("Expected 2 history items, got %d", len(hist))
	}
	// Most recent item should be at index 0
	if hist[0].TrackTitle != "Boards of Canada - Dayvan Cowboy" {
		t.Errorf("Expected most recent track at index 0, got %q", hist[0].TrackTitle)
	}
	if hist[1].TrackTitle != "Tycho - A Walk" {
		t.Errorf("Expected older track at index 1, got %q", hist[1].TrackTitle)
	}

	// 4. Empty track should be ignored
	if store.AddHistory("groovesalad", "SomaFM Groove Salad", "   ") != nil {
		t.Errorf("Expected empty track string to return nil")
	}
}

func TestStoreHistoryRingBufferCapping(t *testing.T) {
	store := NewStore()

	// Add 125 distinct tracks
	for i := 1; i <= 125; i++ {
		store.AddHistory("station-1", "Station 1", fmt.Sprintf("Artist %d - Song %d", i, i))
	}

	hist := store.GetHistory()
	if len(hist) != MaxHistoryEntries {
		t.Fatalf("Expected history capped at %d, got %d", MaxHistoryEntries, len(hist))
	}

	// The newest track (125) should be at index 0
	if hist[0].TrackTitle != "Artist 125 - Song 125" {
		t.Errorf("Expected newest track at index 0, got %q", hist[0].TrackTitle)
	}

	// The oldest preserved track (26) should be at the end (125 - 100 + 1 = 26)
	if hist[MaxHistoryEntries-1].TrackTitle != "Artist 26 - Song 26" {
		t.Errorf("Expected oldest preserved track at end, got %q", hist[MaxHistoryEntries-1].TrackTitle)
	}
}

func TestStoreClearHistory(t *testing.T) {
	store := NewStore()
	store.AddHistory("s1", "Station 1", "Artist - Song 1")
	store.AddHistory("s1", "Station 1", "Artist - Song 2")

	if len(store.GetHistory()) != 2 {
		t.Fatalf("Expected 2 history entries")
	}

	store.ClearHistory()
	if len(store.GetHistory()) != 0 {
		t.Fatalf("Expected 0 history entries after ClearHistory()")
	}
}

func TestStoreConcurrentHistoryAccess(t *testing.T) {
	store := NewStore()
	var wg sync.WaitGroup

	// Concurrently add tracks
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				store.AddHistory(
					fmt.Sprintf("st-%d", workerID),
					fmt.Sprintf("Station %d", workerID),
					fmt.Sprintf("Artist %d - Song %d", workerID, j),
				)
				_ = store.GetHistory()
			}
		}(i)
	}

	wg.Wait()

	hist := store.GetHistory()
	if len(hist) == 0 {
		t.Errorf("Expected recorded history entries under concurrent load")
	}
}

func TestSaveTrackBookmarkAndRead(t *testing.T) {
	// Create a mock entry
	entry := HistoryEntry{
		StationID:   "soma-groove",
		StationName: "SomaFM Groove Salad",
		TrackTitle:  "Tycho - A Walk",
		Artist:      "Tycho",
		Title:       "A Walk",
		PlayedAt:    time.Date(2026, 8, 15, 22, 15, 2, 0, time.UTC),
	}

	line := entry.FormatBookmarkLine()
	expected := "[2026-08-15 22:15:02] SomaFM Groove Salad | Tycho - A Walk\n"
	if line != expected {
		t.Errorf("Unexpected bookmark line format: %q, want %q", line, expected)
	}

	full := entry.FullDisplay()
	if full != "Tycho - A Walk" {
		t.Errorf("Unexpected FullDisplay(): %q", full)
	}

	store := NewStore()
	err := store.SaveTrackBookmark(entry)
	if err != nil {
		t.Fatalf("Failed saving track bookmark: %v", err)
	}

	bookmarks, err := store.GetBookmarkedTracks()
	if err != nil {
		t.Fatalf("Failed reading bookmarked tracks: %v", err)
	}
	if len(bookmarks) == 0 {
		t.Errorf("Expected at least 1 bookmarked track")
	}
}
