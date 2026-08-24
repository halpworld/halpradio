package app

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/radio"
)

// StationJSONEntry represents the serialized JSON output for stations.
type StationJSONEntry struct {
	Index      int      `json:"index"`
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	Genre      string   `json:"genre"`
	Country    string   `json:"country"`
	City       string   `json:"city,omitempty"`
	Bitrate    int      `json:"bitrate"`
	Codec      string   `json:"codec"`
	Broadcast  string   `json:"broadcast,omitempty"`
	Frequency  string   `json:"frequency,omitempty"`
	IsFavorite bool     `json:"is_favorite"`
	Activities []string `json:"activities,omitempty"`
	Homepage   string   `json:"homepage,omitempty"`
}

// RunStations handles `halpradio stations [list|search|fav|add] [flags]`.
func RunStations(args []string, embeddedCatalog []byte, out io.Writer) (bool, error) {
	if len(args) > 0 && (args[0] == "help" || args[0] == "--help" || args[0] == "-h") {
		printStationsHelp(out)
		return true, nil
	}

	subcmd := "list"
	subargs := args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcmd = args[0]
		subargs = args[1:]
	}

	store := loadStore(embeddedCatalog)

	switch subcmd {
	case "list", "ls":
		return runStationsList(subargs, store, out)
	case "search", "find":
		return runStationsSearch(subargs, store, out)
	case "fav", "favorite", "favorites":
		return runStationsFav(subargs, store, out)
	case "add":
		return runStationsAdd(subargs, store, out)
	default:
		// If unknown subcommand, treat first arg as search term or list filter
		return runStationsSearch(args, store, out)
	}
}

func loadStore(embeddedCatalog []byte) *radio.Store {
	store := radio.NewStore()
	_ = store.Load(embeddedCatalog)
	_ = store.ReloadBundledFromCache()
	return store
}

func runStationsList(args []string, store *radio.Store, out io.Writer) (bool, error) {
	fs := flag.NewFlagSet("stations list", flag.ContinueOnError)
	fs.SetOutput(out)

	var genre, country, tag string
	var favorites, plain, isJSON bool
	var limit int

	fs.StringVar(&genre, "genre", "", "Filter stations by genre (e.g. ambient, jazz, lofi)")
	fs.StringVar(&genre, "g", "", "Filter stations by genre (shorthand)")
	fs.StringVar(&country, "country", "", "Filter stations by country code or name (e.g. US, GB, JP)")
	fs.StringVar(&country, "c", "", "Filter stations by country (shorthand)")
	fs.StringVar(&tag, "tag", "", "Filter stations by tag/activity (e.g. coding, study, relax)")
	fs.StringVar(&tag, "activity", "", "Filter stations by tag/activity")
	fs.StringVar(&tag, "t", "", "Filter stations by tag/activity (shorthand)")
	fs.BoolVar(&favorites, "favorites", false, "Show only favorited stations")
	fs.BoolVar(&favorites, "fav", false, "Show only favorited stations")
	fs.BoolVar(&favorites, "f", false, "Show only favorited stations (shorthand)")
	fs.IntVar(&limit, "limit", 0, "Limit number of stations displayed (0 for all)")
	fs.IntVar(&limit, "n", 0, "Limit number of stations displayed (shorthand)")
	fs.BoolVar(&plain, "plain", false, "Output plain tab-separated format for fzf / shell scripting")
	fs.BoolVar(&plain, "q", false, "Output plain tab-separated format (shorthand)")
	fs.BoolVar(&isJSON, "json", false, "Output full JSON array")
	fs.BoolVar(&isJSON, "j", false, "Output full JSON array (shorthand)")

	listFlagsWithVal := map[string]bool{
		"genre": true, "g": true,
		"country": true, "c": true,
		"tag": true, "activity": true, "t": true,
		"limit": true, "n": true,
	}
	if err := fs.Parse(reorderFlagsFirst(args, listFlagsWithVal)); err != nil {
		return false, err
	}

	var all []radio.Station
	if favorites {
		all = store.GetFavorites()
	} else {
		all = store.GetAllStations()
	}

	filtered := radio.FilterWithLocation(all, "", genre, tag, country)
	if limit > 0 && len(filtered) > limit {
		filtered = filtered[:limit]
	}

	if isJSON {
		return renderStationsJSON(filtered, out)
	}
	if plain {
		return renderStationsPlain(filtered, out)
	}
	return renderStationsTable(filtered, out, "STATION CATALOG")
}

func runStationsSearch(args []string, store *radio.Store, out io.Writer) (bool, error) {
	fs := flag.NewFlagSet("stations search", flag.ContinueOnError)
	fs.SetOutput(out)

	var genre, country, tag string
	var plain, isJSON, online bool
	var limit int

	fs.StringVar(&genre, "genre", "", "Filter by genre")
	fs.StringVar(&genre, "g", "", "Filter by genre (shorthand)")
	fs.StringVar(&country, "country", "", "Filter by country")
	fs.StringVar(&country, "c", "", "Filter by country (shorthand)")
	fs.StringVar(&tag, "tag", "", "Filter by tag/activity")
	fs.StringVar(&tag, "t", "", "Filter by tag/activity (shorthand)")
	fs.IntVar(&limit, "limit", 25, "Maximum number of results")
	fs.IntVar(&limit, "n", 25, "Maximum number of results (shorthand)")
	fs.BoolVar(&plain, "plain", false, "Output plain tab-separated format for fzf")
	fs.BoolVar(&plain, "q", false, "Output plain tab-separated format (shorthand)")
	fs.BoolVar(&isJSON, "json", false, "Output JSON array")
	fs.BoolVar(&isJSON, "j", false, "Output JSON array (shorthand)")
	fs.BoolVar(&online, "online", false, "Query 40,000+ RadioBrowser community stations")
	fs.BoolVar(&online, "o", false, "Query RadioBrowser community stations (shorthand)")

	searchFlagsWithVal := map[string]bool{
		"genre": true, "g": true,
		"country": true, "c": true,
		"tag": true, "t": true,
		"limit": true, "n": true,
	}
	if err := fs.Parse(reorderFlagsFirst(args, searchFlagsWithVal)); err != nil {
		return false, err
	}

	query := strings.Join(fs.Args(), " ")

	var results []radio.Station
	if online && query != "" {
		rb := radio.NewRadioBrowserClient()
		searchParams := radio.ParseSearchQuery(query)
		if genre != "" {
			searchParams.Tag = genre
		}
		if country != "" {
			code := radio.CountryNameToCode(country)
			if code != "" {
				searchParams.CountryCode = code
			} else {
				searchParams.Country = country
			}
		}
		searchParams.Limit = limit
		onlineResults, err := rb.SearchWithParams(searchParams)
		if err == nil {
			results = onlineResults
		}
	} else {
		all := store.GetAllStations()
		results = radio.FilterWithLocation(all, query, genre, tag, country)
	}

	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}

	if isJSON {
		return renderStationsJSON(results, out)
	}
	if plain {
		return renderStationsPlain(results, out)
	}

	title := fmt.Sprintf("SEARCH RESULTS (%d matches)", len(results))
	if query != "" {
		title = fmt.Sprintf("SEARCH RESULTS for %q (%d matches)", query, len(results))
	}
	return renderStationsTable(results, out, title)
}

func runStationsFav(args []string, store *radio.Store, out io.Writer) (bool, error) {
	if len(args) == 0 || args[0] == "list" || args[0] == "ls" {
		favs := store.GetFavorites()
		if len(favs) == 0 {
			fmt.Fprintln(out, "No favorite stations yet. Add favorites using 'halpradio stations fav add <station-id>'.")
			return true, nil
		}
		return renderStationsTable(favs, out, "FAVORITE STATIONS")
	}

	action := args[0]
	if len(args) < 2 {
		fmt.Fprintf(out, "Usage: halpradio stations fav %s <station-id>\n", action)
		return false, fmt.Errorf("station ID required for fav %s", action)
	}

	targetID := strings.TrimSpace(args[1])
	all := store.GetAllStations()
	var targetStation *radio.Station
	for _, st := range all {
		if strings.EqualFold(st.ID, targetID) {
			targetStation = &st
			break
		}
	}

	if targetStation == nil {
		fmt.Fprintf(out, "Station %q not found in catalog.\n", targetID)
		return false, fmt.Errorf("station not found: %s", targetID)
	}

	switch action {
	case "add":
		if store.Favorites[targetStation.ID] {
			fmt.Fprintf(out, "Station %q is already in favorites.\n", targetStation.Name)
			return true, nil
		}
		_ = store.ToggleFavorite(*targetStation)
		fmt.Fprintf(out, "✓ Added %q (%s) to favorites.\n", targetStation.Name, targetStation.ID)
		return true, nil

	case "remove", "rm", "del", "delete":
		if !store.Favorites[targetStation.ID] {
			fmt.Fprintf(out, "Station %q is not in favorites.\n", targetStation.Name)
			return true, nil
		}
		_ = store.ToggleFavorite(*targetStation)
		fmt.Fprintf(out, "✓ Removed %q (%s) from favorites.\n", targetStation.Name, targetStation.ID)
		return true, nil

	case "toggle":
		isFav := store.ToggleFavorite(*targetStation)
		if isFav {
			fmt.Fprintf(out, "✓ Added %q (%s) to favorites.\n", targetStation.Name, targetStation.ID)
		} else {
			fmt.Fprintf(out, "✓ Removed %q (%s) from favorites.\n", targetStation.Name, targetStation.ID)
		}
		return true, nil

	default:
		fmt.Fprintf(out, "Unknown fav command %q. Options: list, add, remove, toggle\n", action)
		return false, fmt.Errorf("unknown fav action: %s", action)
	}
}

func runStationsAdd(args []string, store *radio.Store, out io.Writer) (bool, error) {
	fs := flag.NewFlagSet("stations add", flag.ContinueOnError)
	fs.SetOutput(out)

	var name, urlStr, genre, country, idStr, codec string
	var bitrate int

	fs.StringVar(&name, "name", "", "Station name (required)")
	fs.StringVar(&urlStr, "url", "", "Audio stream URL (required, http/https)")
	fs.StringVar(&genre, "genre", "Various", "Genre (e.g. Ambient, Synthwave, Jazz)")
	fs.StringVar(&country, "country", "US", "2-letter ISO country code (e.g. US, GB, JP)")
	fs.StringVar(&idStr, "id", "", "Unique station identifier (optional, autogenerated if empty)")
	fs.StringVar(&codec, "codec", "MP3", "Audio stream codec (MP3, AAC, OGG)")
	fs.IntVar(&bitrate, "bitrate", 128, "Audio bitrate in kbps (e.g. 128, 192, 320)")

	if err := fs.Parse(args); err != nil {
		return false, err
	}

	if strings.TrimSpace(name) == "" || strings.TrimSpace(urlStr) == "" {
		fmt.Fprintln(out, "Error: --name and --url are required flags to add a station.")
		fmt.Fprintln(out, "Usage: halpradio stations add --name \"My Station\" --url \"https://stream.example.com/live.mp3\" [--genre \"Synthwave\"]")
		return false, fmt.Errorf("missing required station parameters")
	}

	st := radio.Station{
		ID:        strings.TrimSpace(idStr),
		Name:      strings.TrimSpace(name),
		URL:       strings.TrimSpace(urlStr),
		Genre:     strings.TrimSpace(genre),
		Country:   strings.ToUpper(strings.TrimSpace(country)),
		Bitrate:   bitrate,
		Codec:     strings.ToUpper(strings.TrimSpace(codec)),
		Broadcast: "Online",
	}

	if err := store.AddOrUpdateLocalStation(st); err != nil {
		fmt.Fprintf(out, "Failed to save station: %v\n", err)
		return false, err
	}

	fmt.Fprintf(out, "✓ Successfully added custom station %q (%s) to local catalog.\n", st.Name, st.ID)
	return true, nil
}

func renderStationsJSON(stations []radio.Station, out io.Writer) (bool, error) {
	entries := make([]StationJSONEntry, len(stations))
	for i, st := range stations {
		entries[i] = StationJSONEntry{
			Index:      i + 1,
			ID:         st.ID,
			Name:       st.Name,
			URL:        st.URL,
			Genre:      st.Genre,
			Country:    st.Country,
			City:       st.City,
			Bitrate:    st.Bitrate,
			Codec:      st.Codec,
			Broadcast:  st.BroadcastType(),
			Frequency:  st.Frequency,
			IsFavorite: st.IsFavorite,
			Activities: st.Activities,
			Homepage:   st.Homepage,
		}
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return false, err
	}
	fmt.Fprintln(out, string(data))
	return true, nil
}

func renderStationsPlain(stations []radio.Station, out io.Writer) (bool, error) {
	for i, st := range stations {
		favMarker := ""
		if st.IsFavorite {
			favMarker = "★"
		}
		cleanID := desktop.SanitizeString(st.ID, 128)
		cleanName := desktop.SanitizeString(st.Name, 256)
		cleanGenre := desktop.SanitizeString(st.Genre, 128)
		cleanCountry := desktop.SanitizeString(st.Country, 32)
		fmt.Fprintf(out, "%d\t%s\t%s\t%s\t%s\t%d\t%s\t%s\n",
			i+1,
			cleanID,
			cleanName,
			cleanGenre,
			cleanCountry,
			st.Bitrate,
			st.URL,
			favMarker,
		)
	}
	return true, nil
}

func renderStationsTable(stations []radio.Station, out io.Writer, header string) (bool, error) {
	if len(stations) == 0 {
		fmt.Fprintln(out, "No stations found matching criteria.")
		return true, nil
	}

	fmt.Fprintf(out, "📻 %s (%d stations)\n", header, len(stations))
	fmt.Fprintln(out, "───────────────────────────────────────────────────────────────────────────────────────────────────")
	fmt.Fprintf(out, "%-4s  %-24s  %-30s  %-18s  %-8s  %-8s\n", "#", "ID", "NAME", "GENRE", "LOC", "BITRATE")
	fmt.Fprintln(out, "───────────────────────────────────────────────────────────────────────────────────────────────────")

	for i, st := range stations {
		name := desktop.SanitizeString(st.Name, 256)
		if len(name) > 30 {
			name = name[:27] + "..."
		}
		id := desktop.SanitizeString(st.ID, 128)
		if len(id) > 24 {
			id = id[:21] + "..."
		}
		genre := desktop.SanitizeString(st.Genre, 128)
		if len(genre) > 18 {
			genre = genre[:15] + "..."
		}
		loc := desktop.SanitizeString(st.Country, 32)
		if loc == "" {
			loc = "🌐"
		} else {
			flag := radio.CountryFlagForCode(loc)
			loc = fmt.Sprintf("%s %s", flag, loc)
		}
		bitrate := "-"
		if st.Bitrate > 0 {
			bitrate = fmt.Sprintf("%d kbps", st.Bitrate)
		}
		favTag := " "
		if st.IsFavorite {
			favTag = "★"
		}

		fmt.Fprintf(out, "%2d%s  %-24s  %-30s  %-18s  %-8s  %-8s\n",
			i+1, favTag, id, name, genre, loc, bitrate)
	}

	fmt.Fprintln(out, "───────────────────────────────────────────────────────────────────────────────────────────────────")
	fmt.Fprintln(out, "Tip: Run 'halpradio play <#|id|name>' to start streaming immediately.")
	return true, nil
}

func printStationsHelp(out io.Writer) {
	fmt.Fprintln(out, "halpradio stations - Discover, search, and manage radio station catalog")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Usage:")
	fmt.Fprintln(out, "  halpradio stations [list] [flags]     List stations with optional filters")
	fmt.Fprintln(out, "  halpradio stations search <query>     Search stations (local & online)")
	fmt.Fprintln(out, "  halpradio stations fav [list|add|rm]  Manage favorite stations")
	fmt.Fprintln(out, "  halpradio stations add [flags]        Add custom station to local catalog")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "List & Search Flags:")
	fmt.Fprintln(out, "  -g, --genre <genre>       Filter by genre (e.g. ambient, lofi, jazz, synthwave)")
	fmt.Fprintln(out, "  -c, --country <country>   Filter by ISO country code or name (e.g. US, GB, JP)")
	fmt.Fprintln(out, "  -t, --tag <tag>           Filter by activity tag (e.g. coding, study, focus)")
	fmt.Fprintln(out, "  -f, --favorites           Show only favorited stations")
	fmt.Fprintln(out, "  -n, --limit <n>           Limit number of stations shown")
	fmt.Fprintln(out, "  -q, --plain               Output tab-separated TSV for fzf / rofi / piping")
	fmt.Fprintln(out, "  -j, --json                Output structured JSON array for jq / scripting")
	fmt.Fprintln(out, "  -o, --online              (search only) Query 40,000+ RadioBrowser stations")
	fmt.Fprintln(out, "")
	fmt.Fprintln(out, "Examples:")
	fmt.Fprintln(out, "  halpradio stations list --genre ambient --limit 10")
	fmt.Fprintln(out, "  halpradio stations search \"lofi\" --online")
	fmt.Fprintln(out, "  halpradio stations list --plain | fzf | awk '{print $2}' | xargs halpradio play")
	fmt.Fprintln(out, "  halpradio stations fav add somafm_groovesalad")
}
