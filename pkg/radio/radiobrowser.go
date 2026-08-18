package radio

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type RadioBrowserStation struct {
	StationUUID string `json:"stationuuid"`
	Name        string `json:"name"`
	URL         string `json:"url_resolved"`
	URLFallback string `json:"url"`
	Homepage    string `json:"homepage"`
	Favicon     string `json:"favicon"`
	Tags        string `json:"tags"`
	CountryCode string `json:"countrycode"`
	Country     string `json:"country"`
	State       string `json:"state"`
	Language    string `json:"language"`
	Votes       int    `json:"votes"`
	Codec       string `json:"codec"`
	Bitrate     int    `json:"bitrate"`
	LastCheckOk int    `json:"lastcheckok"`
}

type SearchParams struct {
	Name        string
	CountryCode string
	Country     string
	State       string
	Tag         string
	TagList     string
	Codec       string
	Order       string
	Reverse     bool
	Limit       int
}

type RadioBrowserClient struct {
	BaseURLs   []string
	HTTPClient *http.Client
}

func NewRadioBrowserClient() *RadioBrowserClient {
	return &RadioBrowserClient{
		BaseURLs: []string{
			"https://de1.api.radio-browser.info",
			"https://nl1.api.radio-browser.info",
			"https://at1.api.radio-browser.info",
			"https://fr1.api.radio-browser.info",
		},
		HTTPClient: &http.Client{
			Timeout: 6 * time.Second,
		},
	}
}

// ParseSearchQuery parses natural language or structured qualifiers into SearchParams.
// Supports qualifiers like:
//   - c:IE / country:ireland
//   - city:dublin / state:dublin / region:cork
//   - tag:fm / tag:dab / t:news
//   - name:rte / n:rte
//
// Also parses natural multi-term queries like "rte ireland", "fm dublin", "bbc london", "france inter".
func ParseSearchQuery(query string) SearchParams {
	p := SearchParams{
		Order:   "votes",
		Reverse: true,
		Limit:   40,
	}

	trimmed := strings.TrimSpace(query)
	if trimmed == "" {
		return p
	}

	tokens := strings.Fields(trimmed)
	var remainingTokens []string
	hasExplicitQualifier := false

	for _, tok := range tokens {
		lowerTok := strings.ToLower(tok)
		if strings.HasPrefix(lowerTok, "c:") || strings.HasPrefix(lowerTok, "country:") {
			parts := strings.SplitN(tok, ":", 2)
			if len(parts) == 2 && parts[1] != "" {
				val := parts[1]
				code := CountryNameToCode(val)
				if code != "" {
					p.CountryCode = code
				} else {
					p.Country = val
				}
				hasExplicitQualifier = true
				continue
			}
		} else if strings.HasPrefix(lowerTok, "city:") || strings.HasPrefix(lowerTok, "state:") || strings.HasPrefix(lowerTok, "region:") {
			parts := strings.SplitN(tok, ":", 2)
			if len(parts) == 2 && parts[1] != "" {
				p.State = parts[1]
				hasExplicitQualifier = true
				continue
			}
		} else if strings.HasPrefix(lowerTok, "tag:") || strings.HasPrefix(lowerTok, "t:") {
			parts := strings.SplitN(tok, ":", 2)
			if len(parts) == 2 && parts[1] != "" {
				p.Tag = parts[1]
				hasExplicitQualifier = true
				continue
			}
		} else if strings.HasPrefix(lowerTok, "name:") || strings.HasPrefix(lowerTok, "n:") {
			parts := strings.SplitN(tok, ":", 2)
			if len(parts) == 2 && parts[1] != "" {
				p.Name = parts[1]
				hasExplicitQualifier = true
				continue
			}
		}
		remainingTokens = append(remainingTokens, tok)
	}

	if hasExplicitQualifier {
		if len(remainingTokens) > 0 {
			if p.Name != "" {
				p.Name = p.Name + " " + strings.Join(remainingTokens, " ")
			} else {
				p.Name = strings.Join(remainingTokens, " ")
			}
		}
		return p
	}

	// Natural language smart multi-word extraction
	var nameTokens []string
	for _, tok := range remainingTokens {
		lower := strings.ToLower(tok)
		// Check for country name or ISO code
		if code := CountryNameToCode(lower); code != "" && p.CountryCode == "" && p.Country == "" {
			p.CountryCode = code
			continue
		}
		// Check for broadcast tags
		if (lower == "fm" || lower == "dab" || lower == "am") && p.Tag == "" {
			p.Tag = lower
			continue
		}
		nameTokens = append(nameTokens, tok)
	}

	p.Name = strings.Join(nameTokens, " ")
	return p
}

// SearchSmart performs an intelligent search by parsing the query into structured parameters
// and falling back to a broad text search if the strict search yields no results.
func (rb *RadioBrowserClient) SearchSmart(searchTerm string, limit int) ([]Station, error) {
	if limit <= 0 {
		limit = 40
	}

	params := ParseSearchQuery(searchTerm)
	params.Limit = limit

	// If we extracted a structured query with country, state, or tag
	if params.CountryCode != "" || params.Country != "" || params.State != "" || params.Tag != "" {
		stations, err := rb.SearchWithParams(params)
		if err == nil && len(stations) > 0 {
			return stations, nil
		}
	}

	// Fallback to searching by name / keyword
	fallbackParams := SearchParams{
		Name:    searchTerm,
		Limit:   limit,
		Order:   "votes",
		Reverse: true,
	}
	return rb.SearchWithParams(fallbackParams)
}

// SearchByCountry fetches the top voted stations for a specific 2-letter ISO country code.
func (rb *RadioBrowserClient) SearchByCountry(countryCode string, limit int) ([]Station, error) {
	if limit <= 0 {
		limit = 40
	}
	return rb.SearchWithParams(SearchParams{
		CountryCode: strings.ToUpper(strings.TrimSpace(countryCode)),
		Limit:       limit,
		Order:       "votes",
		Reverse:     true,
	})
}

// SearchByLocation fetches top stations for a specific country and state/city.
func (rb *RadioBrowserClient) SearchByLocation(countryCode string, stateCity string, limit int) ([]Station, error) {
	if limit <= 0 {
		limit = 40
	}
	return rb.SearchWithParams(SearchParams{
		CountryCode: strings.ToUpper(strings.TrimSpace(countryCode)),
		State:       strings.TrimSpace(stateCity),
		Limit:       limit,
		Order:       "votes",
		Reverse:     true,
	})
}

// SearchWithParams executes a query with full SearchParams against the RadioBrowser API.
func (rb *RadioBrowserClient) SearchWithParams(params SearchParams) ([]Station, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = 40
	}

	queryParams := url.Values{}
	queryParams.Set("limit", fmt.Sprintf("%d", limit))
	if params.Order != "" {
		queryParams.Set("order", params.Order)
	} else {
		queryParams.Set("order", "votes")
	}
	if params.Reverse {
		queryParams.Set("reverse", "true")
	} else {
		queryParams.Set("reverse", "false")
	}
	queryParams.Set("hidebroken", "true")
	queryParams.Set("lastcheckok", "1")

	if params.Name != "" {
		queryParams.Set("name", params.Name)
	}
	if params.CountryCode != "" {
		queryParams.Set("countrycode", strings.ToUpper(params.CountryCode))
	} else if params.Country != "" {
		queryParams.Set("country", params.Country)
	}
	if params.State != "" {
		queryParams.Set("state", params.State)
	}
	if params.Tag != "" {
		queryParams.Set("tag", params.Tag)
	}
	if params.TagList != "" {
		queryParams.Set("tagList", params.TagList)
	}
	if params.Codec != "" {
		queryParams.Set("codec", params.Codec)
	}

	var lastErr error
	for _, baseURL := range rb.BaseURLs {
		endpoint := fmt.Sprintf("%s/json/stations/search?%s", baseURL, queryParams.Encode())

		req, err := http.NewRequest("GET", endpoint, nil)
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "halpradio/1.0")

		resp, err := rb.HTTPClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			lastErr = fmt.Errorf("server %s status %d", baseURL, resp.StatusCode)
			continue
		}

		var rawStations []RadioBrowserStation
		if err := json.NewDecoder(resp.Body).Decode(&rawStations); err != nil {
			resp.Body.Close()
			lastErr = err
			continue
		}
		resp.Body.Close()

		var stations []Station
		for _, r := range rawStations {
			streamURL := r.URL
			if streamURL == "" {
				streamURL = r.URLFallback
			}
			if streamURL == "" {
				continue
			}

			genre := r.Tags
			if len(genre) > 30 {
				genre = genre[:30]
			}

			id := fmt.Sprintf("rb-%s", r.StationUUID)
			if len(id) > 20 {
				id = id[:20]
			}

			tagsLower := strings.ToLower(r.Tags)
			broadcast := "Online"
			frequency := ""

			// Strictly check verified tags from RadioBrowser metadata (never guess from station name)
			rawTags := strings.Split(tagsLower, ",")
			for _, tag := range rawTags {
				t := strings.TrimSpace(tag)
				if t == "dab+" {
					broadcast = "DAB+"
					break
				} else if t == "dab" {
					broadcast = "DAB"
					break
				} else if t == "fm" {
					broadcast = "FM"
					break
				} else if t == "am" || t == "mw" || t == "medium wave" || t == "shortwave" {
					broadcast = "AM"
					break
				}
			}

			st := Station{
				ID:        id,
				Name:      r.Name,
				URL:       streamURL,
				Genre:     genre,
				Country:   r.CountryCode,
				City:      r.State,
				State:     r.State,
				Broadcast: broadcast,
				Frequency: frequency,
				Bitrate:   r.Bitrate,
				Codec:     r.Codec,
				Homepage:  r.Homepage,
				Source:    "radiobrowser",
			}
			st = EnrichStation(st)
			stations = append(stations, st)
		}

		return stations, nil
	}

	return nil, fmt.Errorf("RadioBrowser API failed: %v", lastErr)
}

func (rb *RadioBrowserClient) Search(searchTerm string, limit int) ([]Station, error) {
	return rb.SearchSmart(searchTerm, limit)
}
