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
	City       string   `json:"city,omitempty" yaml:"city,omitempty"`
	State      string   `json:"state,omitempty" yaml:"state,omitempty"`
	Broadcast  string   `json:"broadcast,omitempty" yaml:"broadcast,omitempty"` // "FM", "DAB", "AM", "FM/DAB", "Online"
	Frequency  string   `json:"frequency,omitempty" yaml:"frequency,omitempty"` // e.g. "88.5 FM", "104.4 FM", "DAB+"
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

var CountryCodeToName = map[string]string{
	"IE": "Ireland",
	"GB": "United Kingdom",
	"UK": "United Kingdom",
	"US": "United States",
	"FR": "France",
	"DE": "Germany",
	"IT": "Italy",
	"ES": "Spain",
	"NL": "Netherlands",
	"BE": "Belgium",
	"CH": "Switzerland",
	"AT": "Austria",
	"SE": "Sweden",
	"NO": "Norway",
	"DK": "Denmark",
	"FI": "Finland",
	"PT": "Portugal",
	"GR": "Greece",
	"PL": "Poland",
	"CZ": "Czech Republic",
	"JP": "Japan",
	"KR": "South Korea",
	"CN": "China",
	"AU": "Australia",
	"NZ": "New Zealand",
	"CA": "Canada",
	"BR": "Brazil",
	"MX": "Mexico",
	"AR": "Argentina",
	"IN": "India",
	"ZA": "South Africa",
}

// CountryNameToCode converts country names or aliases to a 2-letter ISO code.
func CountryNameToCode(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "fm" || n == "am" || n == "dab" || n == "tv" || n == "hd" {
		return ""
	}
	switch n {
	case "ireland", "irish", "eire", "ie":
		return "IE"
	case "united kingdom", "uk", "great britain", "britain", "england", "scotland", "wales", "gb":
		return "GB"
	case "united states", "usa", "us", "america":
		return "US"
	case "france", "french", "fr":
		return "FR"
	case "germany", "deutschland", "de":
		return "DE"
	case "italy", "italia", "it":
		return "IT"
	case "spain", "españa", "espana", "es":
		return "ES"
	case "netherlands", "holland", "dutch", "nl":
		return "NL"
	case "belgium", "be":
		return "BE"
	case "switzerland", "swiss", "schweiz", "ch":
		return "CH"
	case "austria", "österreich", "at":
		return "AT"
	case "sweden", "sverige", "se":
		return "SE"
	case "norway", "norge", "no":
		return "NO"
	case "denmark", "danmark", "dk":
		return "DK"
	case "finland", "suomi", "fi":
		return "FI"
	case "portugal", "pt":
		return "PT"
	case "greece", "gr":
		return "GR"
	case "poland", "polska", "pl":
		return "PL"
	case "czech republic", "czechia", "cz":
		return "CZ"
	case "japan", "nippon", "jp":
		return "JP"
	case "south korea", "korea", "kr":
		return "KR"
	case "china", "cn":
		return "CN"
	case "australia", "aussie", "au":
		return "AU"
	case "new zealand", "nz":
		return "NZ"
	case "canada", "ca":
		return "CA"
	case "brazil", "brasil", "br":
		return "BR"
	case "mexico", "mx":
		return "MX"
	case "argentina", "ar":
		return "AR"
	case "india", "in":
		return "IN"
	case "south africa", "za":
		return "ZA"
	}
	if len(n) == 2 {
		code := strings.ToUpper(n)
		if _, ok := CountryCodeToName[code]; ok {
			return code
		}
	}
	return ""
}

// CountryFlagForCode returns the emoji flag for any 2-letter ISO country code.
func CountryFlagForCode(countryCode string) string {
	if len(countryCode) != 2 {
		if countryCode != "" {
			return strings.ToUpper(countryCode)
		}
		return "🌐"
	}
	code := strings.ToUpper(countryCode)
	r1 := rune(code[0]) - 'A' + 0x1F1E6
	r2 := rune(code[1]) - 'A' + 0x1F1E6
	return string([]rune{r1, r2})
}

func (s Station) CountryFlag() string {
	return CountryFlagForCode(s.Country)
}

func (s Station) CountryName() string {
	code := strings.ToUpper(s.Country)
	if name, ok := CountryCodeToName[code]; ok {
		return name
	}
	if s.Country != "" {
		return strings.ToUpper(s.Country)
	}
	return "International"
}

func (s Station) LocationString() string {
	flag := s.CountryFlag()
	if s.City != "" {
		return fmt.Sprintf("%s %s", flag, s.City)
	}
	if s.Country != "" {
		return fmt.Sprintf("%s %s", flag, s.CountryName())
	}
	return flag
}

// IsTerrestrial returns true only if the station has a confirmed over-the-air broadcast (FM, AM, DAB, DAB+)
// via explicit catalog metadata, frequency specification, or verified third-party tag.
func (s Station) IsTerrestrial() bool {
	b := strings.ToUpper(strings.TrimSpace(s.Broadcast))
	if b == "ONLINE" || b == "WEB" || b == "INTERNET" || b == "" {
		return s.Frequency != ""
	}
	return b == "FM" || b == "DAB" || b == "DAB+" || b == "AM" || b == "MW" || b == "FM/DAB" || b == "FM/AM" || b == "TERRESTRIAL"
}

// BroadcastType returns the verified broadcast descriptor or defaults strictly to "Online".
// Does not guess or infer terrestrial presence from station name.
func (s Station) BroadcastType() string {
	b := strings.ToUpper(strings.TrimSpace(s.Broadcast))
	if b != "" {
		if b == "WEB" || b == "INTERNET" || b == "ONLINE" {
			return "Online"
		}
		return s.Broadcast
	}
	if s.Frequency != "" {
		freqUpper := strings.ToUpper(s.Frequency)
		if strings.Contains(freqUpper, "DAB") && strings.Contains(freqUpper, "FM") {
			return "FM/DAB"
		}
		if strings.Contains(freqUpper, "DAB") {
			return "DAB"
		}
		if strings.Contains(freqUpper, "AM") {
			return "AM"
		}
		return "FM"
	}
	return "Online"
}

// BroadcastBadge returns a formatted visual badge with emoji (e.g. "📻 FM", "📡 DAB", "🌐 Online").
func (s Station) BroadcastBadge() string {
	bType := s.BroadcastType()
	switch strings.ToUpper(bType) {
	case "FM", "FM/DAB", "FM/AM":
		if s.Frequency != "" {
			return "📻 " + s.Frequency
		}
		return "📻 " + bType
	case "DAB", "DAB+":
		if s.Frequency != "" {
			return "📡 " + s.Frequency
		}
		return "📡 " + bType
	case "AM", "MW":
		if s.Frequency != "" {
			return "📻 " + s.Frequency
		}
		return "📻 " + bType
	default:
		return "🌐 Online"
	}
}

// ShortBroadcastBadge returns a compact badge for table columns.
func (s Station) ShortBroadcastBadge() string {
	bType := s.BroadcastType()
	switch strings.ToUpper(bType) {
	case "FM":
		return "📻 FM"
	case "DAB", "DAB+":
		return "📡 DAB"
	case "AM", "MW":
		return "📻 AM"
	case "FM/DAB":
		return "📻 F/D"
	default:
		return "🌐 Web"
	}
}

// DisplayLocationAndBand returns a combined location and broadcast descriptor.
func (s Station) DisplayLocationAndBand() string {
	loc := s.LocationString()
	badge := s.BroadcastBadge()
	if loc != "" && loc != "🌐" {
		return fmt.Sprintf("%s • %s", loc, badge)
	}
	return badge
}

// ToYAMLSnippet returns a clean YAML block formatted for submitting a Pull Request to stations.yaml
func (s Station) ToYAMLSnippet() string {
	cleanStation := Station{
		ID:         s.ID,
		Name:       s.Name,
		URL:        s.URL,
		Genre:      s.Genre,
		Country:    strings.ToUpper(s.Country),
		City:       s.City,
		State:      s.State,
		Broadcast:  s.Broadcast,
		Frequency:  s.Frequency,
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
