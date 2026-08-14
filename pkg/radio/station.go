package radio

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

type Station struct {
	ID         string   `json:"id" yaml:"id"`
	Name       string   `json:"name" yaml:"name"`
	URL        string   `json:"url" yaml:"url"`
	Genre      string   `json:"genre" yaml:"genre"`
	Country    string   `json:"country" yaml:"country"`
	Bitrate    int      `json:"bitrate" yaml:"bitrate"`
	Codec      string   `json:"codec" yaml:"codec"`
	Homepage   string   `json:"homepage" yaml:"homepage"`
	Activities []string `json:"activities,omitempty" yaml:"activities,omitempty"`
	Source     string   `json:"source" yaml:"source,omitempty"` // "bundled", "local", "radiobrowser"
	IsFavorite bool     `json:"is_favorite" yaml:"is_favorite,omitempty"`
}

type StationCatalog struct {
	Stations []Station `yaml:"stations"`
}

func (s Station) CountryFlag() string {
	if len(s.Country) != 2 {
		if s.Country != "" {
			return strings.ToUpper(s.Country)
		}
		return "🌐"
	}
	code := strings.ToUpper(s.Country)
	// Convert ISO country code to emoji flag
	r1 := rune(code[0]) - 'A' + 0x1F1E6
	r2 := rune(code[1]) - 'A' + 0x1F1E6
	return string([]rune{r1, r2})
}

// ToYAMLSnippet returns a clean YAML block formatted for submitting a Pull Request to stations.yaml
func (s Station) ToYAMLSnippet() string {
	cleanStation := Station{
		ID:         s.ID,
		Name:       s.Name,
		URL:        s.URL,
		Genre:      s.Genre,
		Country:    strings.ToUpper(s.Country),
		Bitrate:    s.Bitrate,
		Codec:      strings.ToUpper(s.Codec),
		Homepage:   s.Homepage,
		Activities: s.Activities,
	}
	if cleanStation.Bitrate == 0 {
		cleanStation.Bitrate = 128
	}
	if cleanStation.Codec == "" {
		cleanStation.Codec = "MP3"
	}

	data, err := yaml.Marshal([]Station{cleanStation})
	if err != nil {
		return fmt.Sprintf("  - id: %s\n    name: %q\n    url: %q\n    genre: %q\n    country: %q\n    bitrate: %d\n    codec: %q\n",
			s.ID, s.Name, s.URL, s.Genre, s.Country, s.Bitrate, s.Codec)
	}
	return string(data)
}
