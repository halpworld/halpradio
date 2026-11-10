package radio

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/halpworld/halpradio/pkg/player/streamnet"
	"github.com/halpworld/halpradio/pkg/radio/netguard"
	"github.com/halpworld/halpradio/pkg/util"
)

// Limits applied to every station, whatever its source. Text is capped in
// runes after control and format characters are removed.
const (
	MaxStationIDLen   = 96
	MaxStreamURLLen   = 2048
	maxNameRunes      = 120
	maxGenreRunes     = 80
	maxFieldRunes     = 60
	maxHomepageLen    = 512
	maxActivities     = 16
	maxActivityRunes  = 32
	maxSourceRunes    = 32
	maxStationBitrate = 100000
)

// stationIDPattern is the only accepted shape for a station ID. IDs are
// compared, used as map keys and persisted, so they are validated rather than
// cleaned: an invalid ID drops the station instead of being rewritten into
// something that could collide with another station.
var stationIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,95}$`)

// ValidStationID reports whether id has the accepted station ID shape.
func ValidStationID(id string) bool {
	return stationIDPattern.MatchString(id)
}

// ValidStreamURL reports whether raw is acceptable as a station stream URL:
// an absolute http or https URL with a host, no credentials, no control,
// format or whitespace characters, at most MaxStreamURLLen bytes.
func ValidStreamURL(raw string) bool {
	if len(raw) > MaxStreamURLLen || !util.IsDisplaySafe(raw) {
		return false
	}
	_, err := netguard.CheckURL(raw)
	return err == nil
}

// UserSource reports whether source marks a station the user created
// themselves (the local stations file, the add-station form or CLI). Only
// such stations may point at loopback, LAN or link-local hosts; every other
// source (bundled, catalog cache, radiobrowser, somafm, party, anything
// unknown) is remote data and is held to public hosts. An empty source is a
// station that has not been stamped yet (CLI input, legacy entries): the
// remote ingestion paths always stamp theirs before validation.
func UserSource(source string) bool {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "local", "custom", "":
		return true
	}
	return false
}

// PrivateStreamHost reports whether raw's host is local by its form alone:
// a loopback, private, link-local or otherwise reserved IP literal,
// localhost, a local-only suffix (.local, .internal, .home.arpa, .lan), a
// single-label name or an ambiguous numeric host. It does not resolve DNS;
// the player checks resolved addresses at connect time.
func PrivateStreamHost(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return true
	}
	return streamnet.IsRestrictedHostName(u.Hostname())
}

// SanitizeStation is the single gate every station passes before it enters
// the store, whatever its source (embedded catalog, remote catalog cache,
// local stations file, favourites file, RadioBrowser, SomaFM, CLI or TUI
// input). Every text field is stripped of terminal control sequences, C1
// controls, bidi and zero-width characters and capped in length. The second
// result is false when the station must be dropped: an invalid ID, an empty
// name, or a stream URL that is not a plain http/https URL, or (for a
// station whose Source is not the user's own, see UserSource) a stream URL
// whose host is local or private (see PrivateStreamHost).
func SanitizeStation(st Station) (Station, bool) {
	out := Station{
		ID:         strings.TrimSpace(st.ID),
		Name:       util.SanitizeDisplay(st.Name, maxNameRunes),
		URL:        strings.TrimSpace(st.URL),
		Genre:      util.SanitizeDisplay(st.Genre, maxGenreRunes),
		Country:    util.SanitizeDisplay(st.Country, maxFieldRunes),
		City:       util.SanitizeDisplay(st.City, maxFieldRunes),
		State:      util.SanitizeDisplay(st.State, maxFieldRunes),
		Broadcast:  util.SanitizeDisplay(st.Broadcast, maxFieldRunes),
		Frequency:  util.SanitizeDisplay(st.Frequency, maxFieldRunes),
		Bitrate:    st.Bitrate,
		Codec:      util.SanitizeDisplay(st.Codec, maxFieldRunes),
		Language:   util.SanitizeDisplay(st.Language, maxFieldRunes),
		Source:     util.SanitizeDisplay(st.Source, maxSourceRunes),
		IsFavorite: st.IsFavorite,
	}
	if !ValidStationID(out.ID) || out.Name == "" || !ValidStreamURL(out.URL) {
		return Station{}, false
	}
	if !UserSource(out.Source) && PrivateStreamHost(out.URL) {
		return Station{}, false
	}
	if out.Bitrate < 0 || out.Bitrate > maxStationBitrate {
		out.Bitrate = 0
	}
	if hp := strings.TrimSpace(st.Homepage); hp != "" && len(hp) <= maxHomepageLen && util.IsDisplaySafe(hp) {
		if _, err := netguard.CheckURL(hp); err == nil {
			out.Homepage = hp
		}
	}
	for _, a := range st.Activities {
		if len(out.Activities) >= maxActivities {
			break
		}
		if a = util.SanitizeDisplay(a, maxActivityRunes); a != "" {
			out.Activities = append(out.Activities, a)
		}
	}
	return out, true
}

// sanitizeStations runs every station through SanitizeStation, drops the
// ones that fail, drops later duplicates of an ID, and stamps source (when
// non-empty) on the survivors. It reports how many entries were dropped.
func sanitizeStations(in []Station, source string) ([]Station, int) {
	out := make([]Station, 0, len(in))
	seen := make(map[string]bool, len(in))
	dropped := 0
	for _, raw := range in {
		if source != "" {
			// Stamp before validating: the host policy depends on it.
			raw.Source = source
		}
		st, ok := SanitizeStation(raw)
		if !ok || seen[st.ID] {
			dropped++
			continue
		}
		seen[st.ID] = true
		if source != "" {
			st.Source = source
		}
		out = append(out, st)
	}
	return out, dropped
}

// errInvalidStation is returned when a station submitted for saving fails
// SanitizeStation.
func errInvalidStation(st Station) error {
	switch {
	case strings.TrimSpace(st.ID) != "" && !ValidStationID(strings.TrimSpace(st.ID)):
		return fmt.Errorf("invalid station id: use letters, digits, '.', '_', ':' or '-' (max %d)", MaxStationIDLen)
	case util.SanitizeDisplay(st.Name, maxNameRunes) == "":
		return fmt.Errorf("station name is empty")
	default:
		return fmt.Errorf("invalid stream URL: must be an http:// or https:// URL with a host")
	}
}
