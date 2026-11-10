package radio

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"

	"github.com/halpworld/halpradio/pkg/radio/netguard"
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

	embeddedCount int      // valid stations in the embedded catalog
	loadWarnings  []string // problems found while loading station files
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
//
// Every station from every file passes SanitizeStation: entries with an
// invalid ID or stream URL are dropped and all text is made terminal-safe,
// so a poisoned stations.yaml or favorites.json comes out clean in memory.
// Files that cannot be parsed, and dropped entries, are reported through
// LoadWarnings rather than failing the load.
func (s *Store) Load(embeddedYAML []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	_ = util.EnsureConfigDir()
	s.loadWarnings = nil

	if len(embeddedYAML) > 0 {
		var catalog StationCatalog
		if err := yaml.Unmarshal(embeddedYAML, &catalog); err == nil {
			stations, dropped := sanitizeStations(catalog.Stations, "bundled")
			s.Bundled = stations
			s.embeddedCount = len(stations)
			s.warnDropped("bundled catalog", dropped)
		} else {
			s.warn("bundled catalog could not be parsed")
		}
	}

	localFile := util.GetLocalStationsFile()
	if data, err := os.ReadFile(localFile); err == nil && len(data) > 0 {
		var catalog StationCatalog
		if err := yaml.Unmarshal(data, &catalog); err == nil {
			stations, dropped := sanitizeStations(catalog.Stations, "local")
			s.Local = stations
			s.warnDropped(localFile, dropped)
		} else {
			s.warn(fmt.Sprintf("%s could not be parsed and was ignored", localFile))
		}
	}

	favFile := util.GetFavoritesFile()
	if data, err := os.ReadFile(favFile); err == nil && len(data) > 0 {
		var favMap map[string]Station
		if err := json.Unmarshal(data, &favMap); err == nil {
			dropped := 0
			for id, raw := range favMap {
				if strings.TrimSpace(raw.ID) == "" {
					raw.ID = id
				}
				st, ok := SanitizeStation(raw)
				if !ok || st.ID != id {
					dropped++
					continue
				}
				s.Favorites[id] = true
				st.IsFavorite = true
				s.FavItems[id] = st
			}
			s.warnDropped(favFile, dropped)
		} else {
			var favList []string
			if err := json.Unmarshal(data, &favList); err == nil {
				dropped := 0
				for _, id := range favList {
					if !ValidStationID(id) {
						dropped++
						continue
					}
					s.Favorites[id] = true
				}
				s.warnDropped(favFile, dropped)
			} else {
				s.warn(fmt.Sprintf("%s could not be parsed and was ignored", favFile))
			}
		}
	}

	s.syncFavoritesLocked()
	return nil
}

// LoadWarnings returns problems found by the last Load or
// ReloadBundledFromCache: unparsable files and dropped invalid entries. The
// UI or CLI can surface them; the store itself never fails on them.
func (s *Store) LoadWarnings() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]string(nil), s.loadWarnings...)
}

func (s *Store) warn(msg string) {
	s.loadWarnings = append(s.loadWarnings, util.SanitizeDisplay(msg, 300))
}

func (s *Store) warnDropped(where string, n int) {
	if n > 0 {
		s.warn(fmt.Sprintf("%s: ignored %d invalid or duplicate station entries", where, n))
	}
}

// ReloadBundledFromCache reloads the bundled catalog from the cached file and resyncs favorites.
//
// The cache is untrusted input: it is re-verified (signature, when a catalog
// signing key is built in), every entry passes SanitizeStation, and a cache
// that would shrink the catalog to less than half of the embedded one is
// ignored. It returns false and keeps the current catalog when the cache is
// missing or rejected.
func (s *Store) ReloadBundledFromCache() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	cacheFile := util.GetCatalogCacheFile()
	cacheData, err := os.ReadFile(cacheFile)
	if err != nil || len(cacheData) == 0 || len(cacheData) > MaxCatalogDownloadBytes {
		return false
	}
	if err := verifyCachedCatalog(cacheFile, cacheData); err != nil {
		s.warn("cached station catalog ignored: " + err.Error())
		return false
	}

	var catalog StationCatalog
	if err := yaml.Unmarshal(cacheData, &catalog); err != nil || len(catalog.Stations) == 0 {
		return false
	}

	stations, dropped := sanitizeStations(catalog.Stations, "bundled")
	if len(stations) == 0 || len(stations)*2 < s.embeddedCount {
		s.warn("cached station catalog ignored: implausibly small")
		return false
	}
	s.warnDropped("cached station catalog", dropped)
	s.Bundled = stations
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
	channels, _ = sanitizeStations(channels, "somafm")
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
	return netguard.WriteFileAtomic(favFile, data, 0600)
}

func (s *Store) SaveLocalStations() error {
	_ = util.EnsureConfigDir()
	localFile := util.GetLocalStationsFile()
	catalog := StationCatalog{Stations: s.Local}
	data, err := yaml.Marshal(catalog)
	if err != nil {
		return err
	}
	return netguard.WriteFileAtomic(localFile, data, 0600)
}

// AddOrUpdateLocalStation validates st through SanitizeStation and stores it
// in the local catalog. An empty ID gets a fresh "custom-N" ID that
// collides with no existing station. Invalid stations are refused with
// an error and nothing is saved.
func (s *Store) AddOrUpdateLocalStation(st Station) error {
	if strings.TrimSpace(st.ID) == "" {
		st.ID = s.newLocalID()
	}
	// The local catalog is the user's own: stamp it before validation so a
	// LAN or loopback stream the user added is accepted.
	st.Source = "local"
	clean, ok := SanitizeStation(st)
	if !ok {
		return errInvalidStation(st)
	}
	st = clean
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

// newLocalID returns the first "custom-N" ID, counting up from the number
// of local stations plus one, that no station in the store uses yet. The old
// scheme (always len+1) reused an ID after a deletion and overwrote another
// station.
func (s *Store) newLocalID() string {
	for n := len(s.Local) + 1; ; n++ {
		id := fmt.Sprintf("custom-%d", n)
		if !s.idInUse(id) {
			return id
		}
	}
}

func (s *Store) idInUse(id string) bool {
	if s.Favorites[id] {
		return true
	}
	for _, list := range [][]Station{s.Local, s.Bundled, s.SomaFM} {
		for _, st := range list {
			if st.ID == id {
				return true
			}
		}
	}
	return false
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

// ToggleFavorite adds st to the favourites, or removes it when it is already
// one. A station that fails SanitizeStation is never added (the result is
// false); removal works by ID so a bad entry can always be removed.
func (s *Store) ToggleFavorite(st Station) bool {
	id := st.ID
	if s.Favorites[id] {
		delete(s.Favorites, id)
		delete(s.FavItems, id)
		s.SyncFavorites()
		_ = s.SaveFavorites()
		return false
	}

	clean, ok := SanitizeStation(st)
	if !ok {
		return false
	}
	clean.IsFavorite = true
	s.Favorites[clean.ID] = true
	s.FavItems[clean.ID] = clean

	s.SyncFavorites()
	_ = s.SaveFavorites()
	return true
}

// GetAllStations returns every station once per ID. A user's own entries
// win over remote ones: a local station first, then a favourite's stored
// copy, then the bundled/remote catalog and SomaFM. A catalog update can
// therefore never repoint a station the user saved or favourited; the
// favourite keeps the catalog's position in the list.
func (s *Store) GetAllStations() []Station {
	seen := make(map[string]bool)
	var list []Station

	add := func(st Station) {
		if seen[st.ID] {
			return
		}
		if fav, ok := s.FavItems[st.ID]; ok && st.Source != "local" {
			st = fav
		}
		seen[st.ID] = true
		list = append(list, st)
	}
	for _, st := range s.Local {
		add(st)
	}
	for _, st := range s.Bundled {
		add(st)
	}
	for _, st := range s.SomaFM {
		add(st)
	}
	for _, st := range s.FavItems {
		add(st)
	}
	return list
}

// FindStationByID searches all stations in the store for a matching ID.
func (s *Store) FindStationByID(id string) *Station {
	if id == "" {
		return nil
	}
	for _, st := range s.GetAllStations() {
		if st.ID == id {
			copy := st
			return &copy
		}
	}
	return nil
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
			matchCountryNative := st.MatchesCountryNativeName(q)
			matchLanguage := strings.Contains(strings.ToLower(st.Language), q)
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

			if !matchName && !matchGenre && !matchCountryCode && !matchCountryName && !matchCountryNative && !matchLanguage && !matchCity && !matchState && !matchActivity && !matchBroadcast {
				continue
			}
		}
		result = append(result, st)
	}
	return result
}
