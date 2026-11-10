package radio

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/halpworld/halpradio/pkg/util"
	"gopkg.in/yaml.v3"
)

// Hostile station data from the NET-1 / TTY-1 / NET-9 findings: an OSC 52
// clipboard write, a C1 CSI, a bidi override, an oversized name and a
// file:// stream URL.
const (
	hostOSC52 = "\x1b]52;c;Y3VybCBldmlsLnNoIHwgc2g=\x07"
	hostC1CSI = "\u009b2J"
	hostBidi  = "‮"
	badURL    = "file:///etc/passwd"
)

var hugeName = strings.Repeat("A", 50000)

func hostileName() string {
	return "Chill FM" + hostOSC52 + hostC1CSI + hostBidi + hugeName
}

// hostileStations returns one hostile station with a valid ID and URL (it
// must survive, cleaned) and one with the file:// URL (it must be dropped).
func hostileStations() []Station {
	return []Station{
		{ID: "hostile-ok", Name: hostileName(), URL: "https://stream.example.com/live", Genre: "lofi" + hostOSC52, Country: "US" + hostC1CSI, Homepage: "javascript:alert(1)", Activities: []string{"focus" + hostBidi}},
		{ID: "hostile-bad", Name: "Bad" + hostOSC52, URL: badURL},
	}
}

// assertStationsClean fails if any string field of any station is not
// display-safe, too long, or carries the bad URL.
func assertStationsClean(t *testing.T, where string, sts []Station) {
	t.Helper()
	for _, st := range sts {
		v := reflect.ValueOf(st)
		for i := 0; i < v.NumField(); i++ {
			f := v.Field(i)
			var vals []string
			switch f.Kind() {
			case reflect.String:
				vals = []string{f.String()}
			case reflect.Slice:
				if f.Type().Elem().Kind() == reflect.String {
					for j := 0; j < f.Len(); j++ {
						vals = append(vals, f.Index(j).String())
					}
				}
			}
			for _, s := range vals {
				if !util.IsDisplaySafe(s) {
					t.Errorf("%s: station %q field %s is not display-safe: %q", where, st.ID, v.Type().Field(i).Name, truncate(s))
				}
				if len([]rune(s)) > MaxStreamURLLen {
					t.Errorf("%s: station %q field %s is %d runes long", where, st.ID, v.Type().Field(i).Name, len([]rune(s)))
				}
			}
		}
		if st.URL == badURL || strings.HasPrefix(st.URL, "file:") {
			t.Errorf("%s: station %q kept the file:// URL", where, st.ID)
		}
		if st.ID == "hostile-bad" {
			t.Errorf("%s: station with file:// URL was not dropped", where)
		}
		if strings.HasPrefix(st.Homepage, "javascript:") {
			t.Errorf("%s: station %q kept a javascript: homepage", where, st.ID)
		}
	}
}

func truncate(s string) string {
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}

func isolateHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("AppData", dir)
	if err := util.EnsureConfigDir(); err != nil {
		t.Fatalf("EnsureConfigDir: %v", err)
	}
	return dir
}

func TestSanitizeStationRejectsAndCleans(t *testing.T) {
	sts := hostileStations()
	clean, ok := SanitizeStation(sts[0])
	if !ok {
		t.Fatalf("valid hostile station was dropped instead of cleaned")
	}
	assertStationsClean(t, "SanitizeStation", []Station{clean})
	if !strings.HasPrefix(clean.Name, "Chill FM") || len([]rune(clean.Name)) > maxNameRunes {
		t.Errorf("name not cleaned/capped: %d runes", len([]rune(clean.Name)))
	}
	if _, ok := SanitizeStation(sts[1]); ok {
		t.Errorf("file:// station accepted")
	}

	for _, id := range []string{"", "a b", "../etc", "id\x1b[31m", strings.Repeat("a", MaxStationIDLen+1), "-lead"} {
		if _, ok := SanitizeStation(Station{ID: id, Name: "x", URL: "https://e.example/s"}); ok {
			t.Errorf("invalid ID %q accepted", id)
		}
	}
	for _, u := range []string{"", "ftp://e.example/s", "https:///nohost", "http://user:pw@e.example/s", "http://e.example/a b", "http://e.example/\x1b]52;c;x\x07", "https://e.example/" + strings.Repeat("a", MaxStreamURLLen)} {
		if _, ok := SanitizeStation(Station{ID: "ok", Name: "x", URL: u}); ok {
			t.Errorf("invalid URL %q accepted", truncate(u))
		}
	}
	// Long valid IDs are not silently truncated into a collision.
	a := Station{ID: "rb-" + strings.Repeat("a", 60) + "1", Name: "a", URL: "https://e.example/a"}
	b := Station{ID: "rb-" + strings.Repeat("a", 60) + "2", Name: "b", URL: "https://e.example/b"}
	ca, _ := SanitizeStation(a)
	cb, _ := SanitizeStation(b)
	if ca.ID == cb.ID {
		t.Errorf("distinct IDs collided after sanitizing: %q", ca.ID)
	}
}

func TestIngestRadioBrowserHostile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"stationuuid": "11111111-2222-3333-4444-555555555555", "name": hostileName(), "url": "http://example.invalid/stream", "url_resolved": "", "tags": "lofi" + hostOSC52, "countrycode": "US" + hostC1CSI, "codec": "MP3", "bitrate": 128},
			{"stationuuid": "22222222-2222-3333-4444-555555555555", "name": "Bad", "url": badURL, "url_resolved": badURL},
			{"stationuuid": "33333333-2222-3333-4444-555555555555", "name": "Ok", "url": "http://example.invalid/s", "url_resolved": badURL},
		})
	}))
	defer srv.Close()

	rb := NewRadioBrowserClient()
	rb.BaseURLs = []string{srv.URL}
	sts, err := rb.Search("chill", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(sts) == 0 {
		t.Fatalf("hostile but valid station was dropped")
	}
	assertStationsClean(t, "RadioBrowser", sts)
}

func TestRadioBrowserBodyLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("["))
		entry := `{"stationuuid":"11111111-2222-3333-4444-555555555555","name":"` + strings.Repeat("A", 1<<20) + `","url":"http://e.example/s"},`
		for i := 0; i < 6; i++ {
			_, _ = w.Write([]byte(entry))
		}
		_, _ = w.Write([]byte(`{}]`))
	}))
	defer srv.Close()
	rb := NewRadioBrowserClient()
	rb.BaseURLs = []string{srv.URL}
	if sts, err := rb.Search("x", 10); err == nil && len(sts) > 0 {
		t.Fatalf("oversized RadioBrowser body was accepted (%d stations)", len(sts))
	}
}

func TestIngestSomaFMHostile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"channels": []map[string]any{
			{"id": "groovesalad", "title": hostileName(), "genre": "ambient" + hostOSC52},
			{"id": "../../evil\x1b]52;c;x\x07", "title": "Evil"},
		}})
	}))
	defer srv.Close()
	c := NewSomaFMClient()
	c.Endpoint = srv.URL
	sts, err := c.FetchChannels()
	if err != nil {
		t.Fatalf("FetchChannels: %v", err)
	}
	if len(sts) != 1 {
		t.Fatalf("expected 1 surviving channel, got %d", len(sts))
	}
	assertStationsClean(t, "SomaFM", sts)
}

func TestIngestStoreLoadHostileFiles(t *testing.T) {
	isolateHome(t)

	bundled, _ := yaml.Marshal(StationCatalog{Stations: hostileStations()})
	local, _ := yaml.Marshal(StationCatalog{Stations: hostileStations()})
	if err := os.WriteFile(util.GetLocalStationsFile(), local, 0600); err != nil {
		t.Fatal(err)
	}
	// Already-poisoned favourites file, including a key/ID mismatch.
	favs := map[string]Station{}
	for _, st := range hostileStations() {
		favs[st.ID] = st
	}
	favs["groove-salad"] = Station{ID: "other-id", Name: "Spoof", URL: "https://attacker.example/ads.mp3"}
	favData, _ := json.Marshal(favs)
	if err := os.WriteFile(util.GetFavoritesFile(), favData, 0600); err != nil {
		t.Fatal(err)
	}

	s := NewStore()
	if err := s.Load(bundled); err != nil {
		t.Fatalf("Load: %v", err)
	}
	assertStationsClean(t, "Load bundled", s.Bundled)
	assertStationsClean(t, "Load local", s.Local)
	var fav []Station
	for id, st := range s.FavItems {
		if id != st.ID {
			t.Errorf("favourite key %q maps to station %q", id, st.ID)
		}
		fav = append(fav, st)
	}
	assertStationsClean(t, "Load favourites", fav)
	if _, ok := s.FavItems["groove-salad"]; ok {
		t.Errorf("favourite with mismatched key/ID was kept")
	}
	assertStationsClean(t, "GetAllStations", s.GetAllStations())
	if len(s.LoadWarnings()) == 0 {
		t.Errorf("dropped entries produced no load warning")
	}
}

func TestIngestReloadBundledFromCacheHostile(t *testing.T) {
	isolateHome(t)
	s := NewStore()
	var base []Station
	for i := 0; i < 4; i++ {
		base = append(base, Station{ID: fmt.Sprintf("st-%d", i), Name: "S", URL: "https://e.example/s"})
	}
	embedded, _ := yaml.Marshal(StationCatalog{Stations: base})
	if err := s.Load(embedded); err != nil {
		t.Fatal(err)
	}

	cache, _ := yaml.Marshal(StationCatalog{Stations: append(append([]Station{}, base...), hostileStations()...)})
	if err := os.WriteFile(util.GetCatalogCacheFile(), cache, 0600); err != nil {
		t.Fatal(err)
	}
	if !s.ReloadBundledFromCache() {
		t.Fatalf("valid cache refused")
	}
	assertStationsClean(t, "ReloadBundledFromCache", s.Bundled)

	// An implausibly small cache (less than half of the embedded catalog) is ignored.
	small, _ := yaml.Marshal(StationCatalog{Stations: base[:1]})
	if err := os.WriteFile(util.GetCatalogCacheFile(), small, 0600); err != nil {
		t.Fatal(err)
	}
	before := len(s.Bundled)
	if s.ReloadBundledFromCache() || len(s.Bundled) != before {
		t.Errorf("implausibly small cache replaced the catalog")
	}
}

func TestIngestAddOrUpdateLocalStationHostile(t *testing.T) {
	isolateHome(t)
	s := NewStore()
	sts := hostileStations()
	if err := s.AddOrUpdateLocalStation(sts[0]); err != nil {
		t.Fatalf("valid hostile station refused: %v", err)
	}
	if err := s.AddOrUpdateLocalStation(sts[1]); err == nil {
		t.Errorf("file:// station accepted")
	}
	if err := s.AddOrUpdateLocalStation(Station{Name: "x", URL: "javascript:alert(1)"}); err == nil {
		t.Errorf("javascript: station accepted")
	}
	assertStationsClean(t, "AddOrUpdateLocalStation", s.Local)

	// Persisted file comes back clean and 0600.
	info, err := os.Stat(util.GetLocalStationsFile())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("local stations file mode %v, want 0600", info.Mode().Perm())
	}
}

func TestLocalIDNotReusedAfterDelete(t *testing.T) {
	isolateHome(t)
	s := NewStore()
	for i := 0; i < 2; i++ {
		if err := s.AddOrUpdateLocalStation(Station{Name: fmt.Sprintf("S%d", i), URL: "https://e.example/s"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteLocalStation(s.Local[0].ID); err != nil {
		t.Fatal(err)
	}
	survivor := s.Local[0]
	if err := s.AddOrUpdateLocalStation(Station{Name: "New", URL: "https://e.example/new"}); err != nil {
		t.Fatal(err)
	}
	if len(s.Local) != 2 {
		t.Fatalf("new station overwrote an existing one: %+v", s.Local)
	}
	if got := s.FindStationByID(survivor.ID); got == nil || got.Name != survivor.Name {
		t.Errorf("existing station %q was overwritten", survivor.ID)
	}
}

func TestIngestToggleFavoriteHostile(t *testing.T) {
	isolateHome(t)
	s := NewStore()
	sts := hostileStations()
	if !s.ToggleFavorite(sts[0]) {
		t.Fatalf("valid hostile station not favourited")
	}
	if s.ToggleFavorite(sts[1]) {
		t.Errorf("file:// station favourited")
	}
	var fav []Station
	for _, st := range s.FavItems {
		fav = append(fav, st)
	}
	assertStationsClean(t, "ToggleFavorite", fav)
	data, err := os.ReadFile(util.GetFavoritesFile())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), `\u001b`) || strings.Contains(string(data), badURL) {
		t.Errorf("favourites file persisted hostile data")
	}
}

func TestFavoriteWinsOverCatalog(t *testing.T) {
	isolateHome(t)
	s := NewStore()
	s.Favorites["groove-salad"] = true
	s.FavItems["groove-salad"] = Station{ID: "groove-salad", Name: "Groove Salad", URL: "https://ice.somafm.com/groovesalad", IsFavorite: true}
	s.Bundled = []Station{{ID: "groove-salad", Name: "Groove Salad", URL: "http://attacker.invalid/ads.mp3", Source: "bundled"}}
	got := s.FindStationByID("groove-salad")
	if got == nil || got.URL != "https://ice.somafm.com/groovesalad" {
		t.Fatalf("catalog entry overrode the favourite: %+v", got)
	}
	s.Local = []Station{{ID: "mine", Name: "Mine", URL: "https://mine.example/s", Source: "local"}}
	s.Bundled = append(s.Bundled, Station{ID: "mine", Name: "Hijack", URL: "http://attacker.invalid/x", Source: "bundled"})
	if got := s.FindStationByID("mine"); got == nil || got.URL != "https://mine.example/s" {
		t.Fatalf("catalog entry overrode a local station: %+v", got)
	}
}

func TestEmbeddedStationsPassGate(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "stations.yaml"))
	if err != nil {
		t.Skipf("stations.yaml not found: %v", err)
	}
	var cat StationCatalog
	if err := yaml.Unmarshal(data, &cat); err != nil {
		t.Fatal(err)
	}
	for _, st := range cat.Stations {
		if _, ok := SanitizeStation(st); !ok {
			t.Errorf("bundled station %q (%q) fails the gate: %v", st.ID, st.URL, errInvalidStation(st))
		}
	}
}

// ---- Catalog updater (NET-3) ----

func newTestUpdater(t *testing.T, srv *httptest.Server) *CatalogUpdater {
	t.Helper()
	dir := t.TempDir()
	u := NewCatalogUpdater(srv.URL+"/stations.yaml", 24)
	u.HTTPClient = srv.Client()
	u.CacheFile = filepath.Join(dir, "catalog_cache.yaml")
	u.MetadataFile = filepath.Join(dir, "catalog_metadata.json")
	return u
}

func catalogYAML(version, n int, extra ...Station) []byte {
	var sts []Station
	for i := 0; i < n; i++ {
		sts = append(sts, Station{ID: fmt.Sprintf("st-%d", i), Name: "S", URL: "https://e.example/s"})
	}
	sts = append(sts, extra...)
	b, _ := yaml.Marshal(StationCatalog{Version: version, Stations: sts})
	return b
}

func TestCatalogUpdaterRefusesHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(catalogYAML(0, 3))
	}))
	defer srv.Close()
	u := newTestUpdater(t, srv)
	if updated, _, err := u.CheckAndUpdate(context.Background(), true); err == nil || updated {
		t.Fatalf("http:// catalog accepted (updated=%v err=%v)", updated, err)
	}
	if _, err := os.Stat(u.CacheFile); err == nil {
		t.Errorf("cache written from http:// catalog")
	}
}

func TestCatalogUpdaterHostileEntriesAndChecks(t *testing.T) {
	var body []byte
	lastMod := ""
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if lastMod != "" {
			w.Header().Set("Last-Modified", lastMod)
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	u := newTestUpdater(t, srv)

	body = catalogYAML(5, 10, hostileStations()...)
	lastMod = "Wed, 01 Jan 2025 00:00:00 GMT"
	updated, n, err := u.CheckAndUpdate(context.Background(), true)
	if err != nil || !updated {
		t.Fatalf("valid catalog refused: %v", err)
	}
	if n != 11 {
		t.Errorf("station count %d, want 11 valid", n)
	}
	info, err := os.Stat(u.CacheFile)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("cache missing or wrong mode: %v %v", err, info)
	}
	// The cache is re-validated on load.
	cached, _ := os.ReadFile(u.CacheFile)
	var cat StationCatalog
	_ = yaml.Unmarshal(cached, &cat)
	clean, _ := sanitizeStations(cat.Stations, "bundled")
	assertStationsClean(t, "catalog cache", clean)

	// Rollback by catalog_version.
	body = catalogYAML(4, 10)
	if updated, _, err := u.CheckAndUpdate(context.Background(), true); err == nil || updated {
		t.Errorf("older catalog_version accepted")
	}
	// Rollback by Last-Modified.
	body = catalogYAML(5, 12)
	lastMod = "Tue, 01 Jan 2024 00:00:00 GMT"
	if updated, _, err := u.CheckAndUpdate(context.Background(), true); err == nil || updated {
		t.Errorf("older Last-Modified accepted")
	}
	lastMod = ""
	// Implausibly small.
	body = catalogYAML(6, 2)
	if updated, _, err := u.CheckAndUpdate(context.Background(), true); err == nil || updated {
		t.Errorf("implausibly small catalog accepted")
	}
	// Oversized body is an error, not a truncated parse.
	body = append([]byte("stations:\n"), make([]byte, MaxCatalogDownloadBytes+10)...)
	if updated, _, err := u.CheckAndUpdate(context.Background(), true); err == nil || updated {
		t.Errorf("oversized catalog accepted")
	}
	// Only invalid entries.
	body, _ = yaml.Marshal(StationCatalog{Version: 7, Stations: []Station{hostileStations()[1]}})
	if updated, _, err := u.CheckAndUpdate(context.Background(), true); err == nil || updated {
		t.Errorf("catalog of only invalid stations accepted")
	}
	after, _ := os.ReadFile(u.CacheFile)
	if string(after) != string(cached) {
		t.Errorf("a refused catalog changed the cache")
	}
}

func TestCatalogSignature(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	old := CatalogSigningPublicKey
	CatalogSigningPublicKey = base64.StdEncoding.EncodeToString(pub)
	defer func() { CatalogSigningPublicKey = old }()

	body := catalogYAML(1, 3)
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, body))
	serveSig := sig
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sig") {
			if serveSig == "" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(serveSig))
			return
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	u := newTestUpdater(t, srv)
	if updated, _, err := u.CheckAndUpdate(context.Background(), true); err != nil || !updated {
		t.Fatalf("signed catalog refused: %v", err)
	}
	cached, _ := os.ReadFile(u.CacheFile)
	if err := verifyCachedCatalog(u.CacheFile, cached); err != nil {
		t.Errorf("cached signed catalog fails verification: %v", err)
	}
	if err := verifyCachedCatalog(u.CacheFile, append(cached, '#')); err == nil {
		t.Errorf("tampered cache verified")
	}

	for name, s := range map[string]string{"missing": "", "wrong": base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)), "garbage": "!!"} {
		serveSig = s
		body = catalogYAML(2, 3)
		u2 := newTestUpdater(t, srv)
		if updated, _, err := u2.CheckAndUpdate(context.Background(), true); err == nil || updated {
			t.Errorf("%s signature accepted", name)
		}
	}

	CatalogSigningPublicKey = "not-a-key"
	serveSig = sig
	body = catalogYAML(1, 3)
	u3 := newTestUpdater(t, srv)
	if updated, _, err := u3.CheckAndUpdate(context.Background(), true); err == nil || updated {
		t.Errorf("malformed signing key did not fail closed")
	}
}
