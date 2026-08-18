package radio

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/halpworld/halpradio/pkg/util"
	"gopkg.in/yaml.v3"
)

type Store struct {
	mu        sync.RWMutex
	Bundled   []Station          `json:"bundled"`
	Local     []Station          `json:"local"`
	SomaFM    []Station          `json:"somafm"`
	Favorites map[string]bool    `json:"favorites"`
	FavItems  map[string]Station `json:"fav_items"` // Store full station object for radiobrowser favorites
	History   []HistoryEntry     `json:"history,omitempty"`
}

func NewStore() *Store {
	return &Store{
		Bundled:   make([]Station, 0),
		Local:     make([]Station, 0),
		SomaFM:    make([]Station, 0),
		Favorites: make(map[string]bool),
		FavItems:  make(map[string]Station),
		History:   make([]HistoryEntry, 0),
	}
}

// Load reads bundled stations from embedded catalog, local user stations from config dir, and user favorites.
func (s *Store) Load(embeddedYAML []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = util.EnsureConfigDir()

	if len(embeddedYAML) > 0 {
		var catalog StationCatalog
		if err := yaml.Unmarshal(embeddedYAML, &catalog); err == nil {
			for i := range catalog.Stations {
				catalog.Stations[i].Source = "bundled"
			}
			s.Bundled = catalog.Stations
		}
	}

	localFile := util.GetLocalStationsFile()
	if data, err := os.ReadFile(localFile); err == nil && len(data) > 0 {
		var catalog StationCatalog
		if err := yaml.Unmarshal(data, &catalog); err == nil {
			for i := range catalog.Stations {
				catalog.Stations[i].Source = "local"
			}
			s.Local = catalog.Stations
		}
	}

	favFile := util.GetFavoritesFile()
	if data, err := os.ReadFile(favFile); err == nil && len(data) > 0 {
		var favMap map[string]Station
		if err := json.Unmarshal(data, &favMap); err == nil {
			for id, st := range favMap {
				s.Favorites[id] = true
				st.IsFavorite = true
				s.FavItems[id] = st
			}
		} else {
			var favList []string
			if err := json.Unmarshal(data, &favList); err == nil {
				for _, id := range favList {
					s.Favorites[id] = true
				}
			}
		}
	}

	s.syncFavoritesLocked()
	return nil
}

// ReloadBundledFromCache reloads the bundled catalog from the cached file and resyncs favorites.
func (s *Store) ReloadBundledFromCache() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	cacheFile := util.GetCatalogCacheFile()
	cacheData, err := os.ReadFile(cacheFile)
	if err != nil || len(cacheData) == 0 {
		return false
	}

	var catalog StationCatalog
	if err := yaml.Unmarshal(cacheData, &catalog); err != nil || len(catalog.Stations) == 0 {
		return false
	}

	for i := range catalog.Stations {
		catalog.Stations[i].Source = "bundled"
	}
	s.Bundled = catalog.Stations
	s.syncFavoritesLocked()
	return true
}

// LoadSomaFM fetches live, official channels from SomaFM API and updates the Store.
func (s *Store) LoadSomaFM() error {
	client := NewSomaFMClient()
	channels, err := client.FetchChannels()
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.SomaFM = channels
	s.syncFavoritesLocked()
	s.mu.Unlock()
	return nil
}

func (s *Store) syncFavoritesLocked() {
	for i := range s.Bundled {
		s.Bundled[i] = EnrichStation(s.Bundled[i])
		s.Bundled[i].IsFavorite = s.Favorites[s.Bundled[i].ID]
	}
	for i := range s.Local {
		s.Local[i] = EnrichStation(s.Local[i])
		s.Local[i].IsFavorite = s.Favorites[s.Local[i].ID]
	}
	for i := range s.SomaFM {
		s.SomaFM[i] = EnrichStation(s.SomaFM[i])
		s.SomaFM[i].IsFavorite = s.Favorites[s.SomaFM[i].ID]
	}
	for id, st := range s.FavItems {
		s.FavItems[id] = EnrichStation(st)
	}
}

func (s *Store) SyncFavorites() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.syncFavoritesLocked()
}

func (s *Store) SaveFavorites() error {
	_ = util.EnsureConfigDir()
	favFile := util.GetFavoritesFile()
	data, err := json.MarshalIndent(s.FavItems, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(favFile, data, 0600)
}

func (s *Store) SaveLocalStations() error {
	_ = util.EnsureConfigDir()
	localFile := util.GetLocalStationsFile()
	catalog := StationCatalog{Stations: s.Local}
	data, err := yaml.Marshal(catalog)
	if err != nil {
		return err
	}
	return os.WriteFile(localFile, data, 0600)
}

func (s *Store) AddOrUpdateLocalStation(st Station) error {
	if st.ID == "" {
		st.ID = fmt.Sprintf("custom-%d", len(s.Local)+1)
	}
	st.Source = "local"
	st.IsFavorite = s.Favorites[st.ID]

	found := false
	for i, existing := range s.Local {
		if existing.ID == st.ID {
			s.Local[i] = st
			found = true
			break
		}
	}
	if !found {
		s.Local = append(s.Local, st)
	}

	return s.SaveLocalStations()
}

func (s *Store) DeleteLocalStation(id string) error {
	newLocal := make([]Station, 0)
	for _, st := range s.Local {
		if st.ID != id {
			newLocal = append(newLocal, st)
		}
	}
	s.Local = newLocal
	return s.SaveLocalStations()
}

func (s *Store) ToggleFavorite(st Station) bool {
	id := st.ID
	isFav := !s.Favorites[id]
	s.Favorites[id] = isFav

	if isFav {
		st.IsFavorite = true
		s.FavItems[id] = st
	} else {
		delete(s.FavItems, id)
	}

	s.SyncFavorites()
	_ = s.SaveFavorites()
	return isFav
}

func (s *Store) GetAllStations() []Station {
	seen := make(map[string]bool)
	var list []Station

	for _, st := range s.Local {
		if !seen[st.ID] {
			seen[st.ID] = true
			list = append(list, st)
		}
	}
	for _, st := range s.Bundled {
		if !seen[st.ID] {
			seen[st.ID] = true
			list = append(list, st)
		}
	}
	for _, st := range s.SomaFM {
		if !seen[st.ID] {
			seen[st.ID] = true
			list = append(list, st)
		}
	}
	for _, st := range s.FavItems {
		if !seen[st.ID] {
			seen[st.ID] = true
			list = append(list, st)
		}
	}
	return list
}

func (s *Store) GetFavorites() []Station {
	var list []Station
	for _, st := range s.FavItems {
		list = append(list, st)
	}
	sort.Slice(list, func(i, j int) bool {
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	return list
}

func (s *Store) GetCategories() []string {
	catMap := make(map[string]bool)
	for _, st := range s.GetAllStations() {
		if st.Genre != "" {
			parts := strings.Split(st.Genre, "/")
			for _, p := range parts {
				genre := strings.TrimSpace(p)
				if genre != "" {
					catMap[genre] = true
				}
			}
		}
	}
	var cats []string
	for c := range catMap {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	return cats
}

type CountryInfo struct {
	Code  string `json:"code"`
	Name  string `json:"name"`
	Flag  string `json:"flag"`
	Count int    `json:"count"`
}

func (s *Store) GetCountries() []CountryInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()

	stations := s.GetAllStations()
	countryMap := make(map[string]int)

	for _, st := range stations {
		code := strings.ToUpper(strings.TrimSpace(st.Country))
		if code != "" {
			countryMap[code]++
		}
	}

	var list []CountryInfo
	for code, count := range countryMap {
		name := code
		if n, ok := CountryCodeToName[code]; ok {
			name = n
		}
		list = append(list, CountryInfo{
			Code:  code,
			Name:  name,
			Flag:  CountryFlagForCode(code),
			Count: count,
		})
	}

	sort.Slice(list, func(i, j int) bool {
		if list[i].Count != list[j].Count {
			return list[i].Count > list[j].Count // higher station count first
		}
		return list[i].Name < list[j].Name
	})

	return list
}

func (s *Store) GetCities(countryCode string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	cityMap := make(map[string]bool)
	code := strings.ToUpper(strings.TrimSpace(countryCode))

	for _, st := range s.GetAllStations() {
		if code != "" && !strings.EqualFold(st.Country, code) {
			continue
		}
		if st.City != "" {
			cityMap[st.City] = true
		}
	}

	var cities []string
	for c := range cityMap {
		cities = append(cities, c)
	}
	sort.Strings(cities)
	return cities
}

func Filter(stations []Station, query string, genre string) []Station {
	return FilterWithLocation(stations, query, genre, "", "")
}

func FilterWithActivity(stations []Station, query string, genre string, activity string) []Station {
	return FilterWithLocation(stations, query, genre, activity, "")
}

func FilterWithLocation(stations []Station, query string, genre string, activity string, countryCode string) []Station {
	q := strings.ToLower(strings.TrimSpace(query))
	g := strings.ToLower(strings.TrimSpace(genre))
	a := strings.ToLower(strings.TrimSpace(activity))
	c := strings.ToUpper(strings.TrimSpace(countryCode))

	var result []Station
	for _, st := range stations {
		if a != "" && a != "all" {
			if !st.MatchesActivity(a) {
				continue
			}
		}
		if g != "" && g != "all" {
			if !strings.Contains(strings.ToLower(st.Genre), g) {
				continue
			}
		}
		if c != "" && c != "ALL" {
			if !strings.EqualFold(st.Country, c) {
				continue
			}
		}
		if q != "" {
			matchName := strings.Contains(strings.ToLower(st.Name), q)
			matchGenre := strings.Contains(strings.ToLower(st.Genre), q)
			matchCountryCode := strings.Contains(strings.ToLower(st.Country), q)
			matchCountryName := strings.Contains(strings.ToLower(st.CountryName()), q)
			matchCity := strings.Contains(strings.ToLower(st.City), q)
			matchState := strings.Contains(strings.ToLower(st.State), q)
			matchBroadcast := strings.Contains(strings.ToLower(st.Broadcast), q) ||
				strings.Contains(strings.ToLower(st.BroadcastType()), q) ||
				strings.Contains(strings.ToLower(st.Frequency), q)
			if (q == "terrestrial" || q == "fm" || q == "dab" || q == "am") && st.IsTerrestrial() {
				if q == "terrestrial" || strings.Contains(strings.ToLower(st.BroadcastType()), q) {
					matchBroadcast = true
				}
			}
			if (q == "online" || q == "web") && !st.IsTerrestrial() {
				matchBroadcast = true
			}
			matchActivity := false
			for _, act := range st.Activities {
				if strings.Contains(strings.ToLower(act), q) {
					matchActivity = true
					break
				}
			}

			if !matchName && !matchGenre && !matchCountryCode && !matchCountryName && !matchCity && !matchState && !matchActivity && !matchBroadcast {
				continue
			}
		}
		result = append(result, st)
	}
	return result
}
