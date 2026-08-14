package radio

import (
	"strings"
)

type Activity struct {
	ID          string `json:"id" yaml:"id"`
	Name        string `json:"name" yaml:"name"`
	Icon        string `json:"icon" yaml:"icon"`
	Description string `json:"description" yaml:"description"`
}

var DefaultActivities = []Activity{
	{
		ID:          "programming",
		Name:        "Programming",
		Icon:        "💻",
		Description: "Focus, coding & synth electronic beats",
	},
	{
		ID:          "cleaning",
		Name:        "Cleaning",
		Icon:        "🧹",
		Description: "Upbeat rock, funk & energetic jams",
	},
	{
		ID:          "reading",
		Name:        "Reading",
		Icon:        "📚",
		Description: "Calm classical, ambient & soft acoustics",
	},
	{
		ID:          "thinking",
		Name:        "Thinking",
		Icon:        "🧠",
		Description: "Deep focus, drone & minimal jazz",
	},
	{
		ID:          "relaxing",
		Name:        "Relaxing",
		Icon:        "☕",
		Description: "Chilling, lofi, vaporwave & lounge",
	},
	{
		ID:          "news",
		Name:        "News & Talk",
		Icon:        "📰",
		Description: "World events, public radio & talk",
	},
}

// GetActivityByID returns the Activity matching the given ID or nil if not found.
func GetActivityByID(id string) *Activity {
	for _, act := range DefaultActivities {
		if strings.EqualFold(act.ID, id) {
			return &act
		}
	}
	return nil
}

// MatchesActivity returns true if the station explicitly lists the activity,
// or if its genre fallback keywords match the activity.
func (s Station) MatchesActivity(activityID string) bool {
	actID := strings.ToLower(strings.TrimSpace(activityID))
	if actID == "" || actID == "all" {
		return true
	}

	// 1. Explicit station activity tag check
	for _, a := range s.Activities {
		if strings.EqualFold(a, actID) {
			return true
		}
	}

	// 2. Genre fallback keyword check if no explicit activities match
	genre := strings.ToLower(s.Genre)
	name := strings.ToLower(s.Name)
	combined := genre + " " + name

	switch actID {
	case "programming":
		keywords := []string{"ambient", "electronic", "synthwave", "vaporwave", "lofi", "hacker", "beats", "chillout", "drone", "indie", "fluxfm"}
		for _, kw := range keywords {
			if strings.Contains(combined, kw) {
				return true
			}
		}
	case "cleaning":
		keywords := []string{"rock", "metal", "funk", "80s", "pop", "new wave", "synth", "upbeat", "retro", "heavy"}
		for _, kw := range keywords {
			if strings.Contains(combined, kw) {
				return true
			}
		}
	case "reading":
		keywords := []string{"classical", "ambient", "drone", "chillout", "jazz", "lofi", "acoustic", "piano"}
		for _, kw := range keywords {
			if strings.Contains(combined, kw) {
				return true
			}
		}
	case "thinking":
		keywords := []string{"ambient", "drone", "classical", "jazz", "lofi", "chillout", "minimal", "downtempo"}
		for _, kw := range keywords {
			if strings.Contains(combined, kw) {
				return true
			}
		}
	case "relaxing":
		keywords := []string{"lofi", "chillout", "vaporwave", "lounge", "spy", "ambient", "jazz", "downtempo", "beats", "funk"}
		for _, kw := range keywords {
			if strings.Contains(combined, kw) {
				return true
			}
		}
	case "news":
		keywords := []string{"news", "talk", "public radio", "world service", "npr", "bbc", "speech"}
		for _, kw := range keywords {
			if strings.Contains(combined, kw) {
				return true
			}
		}
	}

	return false
}
