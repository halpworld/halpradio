package radio

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/halpworld/halpradio/pkg/util"
)

// HistoryEntry represents a single played track event recorded from stream metadata.
type HistoryEntry struct {
	StationID   string    `json:"station_id" yaml:"station_id"`
	StationName string    `json:"station_name" yaml:"station_name"`
	TrackTitle  string    `json:"track_title" yaml:"track_title"`
	Artist      string    `json:"artist,omitempty" yaml:"artist,omitempty"`
	Title       string    `json:"title,omitempty" yaml:"title,omitempty"`
	PlayedAt    time.Time `json:"played_at" yaml:"played_at"`
}

// MaxHistoryEntries defines the ring buffer capacity for track history.
const MaxHistoryEntries = 100

// ParseArtistAndTitle splits a raw metadata track string (e.g. "Tycho - A Walk") into artist and title.
func ParseArtistAndTitle(raw string) (artist, title string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}

	// Common delimiter " - "
	if idx := strings.Index(raw, " - "); idx != -1 {
		artist = strings.TrimSpace(raw[:idx])
		title = strings.TrimSpace(raw[idx+3:])
		return artist, title
	}

	// Delimiter " : "
	if idx := strings.Index(raw, " : "); idx != -1 {
		artist = strings.TrimSpace(raw[:idx])
		title = strings.TrimSpace(raw[idx+3:])
		return artist, title
	}

	// Delimiter " by " (e.g. "Song Name by Artist Name")
	if idx := strings.Index(strings.ToLower(raw), " by "); idx != -1 {
		title = strings.TrimSpace(raw[:idx])
		artist = strings.TrimSpace(raw[idx+4:])
		return artist, title
	}

	// Delimiter " | "
	if idx := strings.Index(raw, " | "); idx != -1 {
		artist = strings.TrimSpace(raw[:idx])
		title = strings.TrimSpace(raw[idx+3:])
		return artist, title
	}

	// No delimiter recognized: whole string is title
	return "", raw
}

// FullDisplay returns a clean "Artist - Title" or just "Title" string.
func (h HistoryEntry) FullDisplay() string {
	if h.Artist != "" && h.Title != "" {
		return fmt.Sprintf("%s - %s", h.Artist, h.Title)
	}
	if h.TrackTitle != "" {
		return h.TrackTitle
	}
	if h.Title != "" {
		return h.Title
	}
	return h.StationName
}

// FormatBookmarkLine formats the entry for appending to saved_tracks.txt.
func (h HistoryEntry) FormatBookmarkLine() string {
	timeStr := h.PlayedAt.Format("2006-01-02 15:04:05")
	return fmt.Sprintf("[%s] %s | %s\n", timeStr, h.StationName, h.FullDisplay())
}

// AddHistory appends a new track entry to the store's history ring buffer if it is not a duplicate.
func (s *Store) AddHistory(stationID, stationName, trackTitle string) *HistoryEntry {
	trackTitle = strings.TrimSpace(trackTitle)
	if trackTitle == "" {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	// Deduplicate sequential identical metadata updates from the stream
	if len(s.History) > 0 {
		top := s.History[0]
		if (top.StationID == stationID || top.StationName == stationName) &&
			strings.EqualFold(strings.TrimSpace(top.TrackTitle), trackTitle) {
			return nil
		}
	}

	artist, title := ParseArtistAndTitle(trackTitle)
	entry := HistoryEntry{
		StationID:   stationID,
		StationName: stationName,
		TrackTitle:  trackTitle,
		Artist:      artist,
		Title:       title,
		PlayedAt:    time.Now(),
	}

	// Prepend to history ring buffer
	s.History = append([]HistoryEntry{entry}, s.History...)
	if len(s.History) > MaxHistoryEntries {
		s.History = s.History[:MaxHistoryEntries]
	}

	return &entry
}

// GetHistory returns a copy of all recorded history entries.
func (s *Store) GetHistory() []HistoryEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	res := make([]HistoryEntry, len(s.History))
	copy(res, s.History)
	return res
}

// ClearHistory resets the history buffer.
func (s *Store) ClearHistory() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.History = make([]HistoryEntry, 0)
}

// SaveTrackBookmark writes a history track entry to the user's bookmarks file.
func (s *Store) SaveTrackBookmark(entry HistoryEntry) error {
	_ = util.EnsureConfigDir()
	filePath := util.GetSavedTracksFile()

	line := entry.FormatBookmarkLine()
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(line)
	return err
}

// GetBookmarkedTracks reads all saved track lines from the bookmarks file.
func (s *Store) GetBookmarkedTracks() ([]string, error) {
	filePath := util.GetSavedTracksFile()
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	var result []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result, nil
}
