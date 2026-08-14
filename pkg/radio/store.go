package radio

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/halpworld/halpradio/pkg/util"
	"gopkg.in/yaml.v3"
)

type Store struct {
	Bundled   []Station          `json:"bundled"`
	Local     []Station          `json:"local"`
	SomaFM    []Station          `json:"somafm"`
	Favorites map[string]bool    `json:"favorites"`
	FavItems  map[string]Station `json:"fav_items"` // Store full station object for radiobrowser favorites
}

func NewStore() *Store {
	return &Store{
		Bundled:   make([]Station, 0),
		Local:     make([]Station, 0),
		SomaFM:    make([]Station, 0),
		Favorites: make(map[string]bool),
		FavItems:  make(map[string]Station),
	}
}

// Load reads bundled stations from embedded catalog, local user stations from config dir, and user favorites.
func (s *Store) Load(embeddedYAML []byte) error {
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

	s.SyncFavorites()
	return nil
}

// LoadSomaFM fetches live, official channels from SomaFM API and updates the Store.
func (s *Store) LoadSomaFM() error {
	client := NewSomaFMClient()
	channels, err := client.FetchChannels()
	if err != nil {
		return err
	}
	s.SomaFM = channels
	s.SyncFavorites()
	return nil
}

func (s *Store) SyncFavorites() {
	for i := range s.Bundled {
		s.Bundled[i].IsFavorite = s.Favorites[s.Bundled[i].ID]
	}
	for i := range s.Local {
		s.Local[i].IsFavorite = s.Favorites[s.Local[i].ID]
	}
	for i := range s.SomaFM {
		s.SomaFM[i].IsFavorite = s.Favorites[s.SomaFM[i].ID]
	}
}

func (s *Store) SaveFavorites() error {
	favFile := util.GetFavoritesFile()
	data, err := json.MarshalIndent(s.FavItems, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(favFile, data, 0600)
}

func (s *Store) SaveLocalStations() error {
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

func Filter(stations []Station, query string, genre string) []Station {
	return FilterWithActivity(stations, query, genre, "")
}

func FilterWithActivity(stations []Station, query string, genre string, activity string) []Station {
	q := strings.ToLower(strings.TrimSpace(query))
	g := strings.ToLower(strings.TrimSpace(genre))
	a := strings.ToLower(strings.TrimSpace(activity))

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
		if q != "" {
			matchName := strings.Contains(strings.ToLower(st.Name), q)
			matchGenre := strings.Contains(strings.ToLower(st.Genre), q)
			matchCountry := strings.Contains(strings.ToLower(st.Country), q)
			if !matchName && !matchGenre && !matchCountry {
				continue
			}
		}
		result = append(result, st)
	}
	return result
}
