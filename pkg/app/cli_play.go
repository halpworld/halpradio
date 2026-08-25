package app

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/util"
)

// TrackChangeJSONEvent represents an ICY track metadata change serialized to JSON.
type TrackChangeJSONEvent struct {
	Event       string `json:"event"`
	StationID   string `json:"station_id"`
	StationName string `json:"station_name"`
	TrackTitle  string `json:"track_title"`
	Timestamp   string `json:"timestamp"`
}

// RunPlay handles `halpradio play [target] [flags]`.
func RunPlay(args []string, embeddedCatalog []byte, out io.Writer) (bool, error) {
	if len(args) > 0 && IsHelpArg(args[0]) {
		PrintPlayHelp(out)
		return true, nil
	}

	if len(args) == 0 {
		// If an active halpradio instance is running, resume playback over IPC
		resp, err := desktop.SendIPCCommand("", "play")
		if err == nil && resp != nil && resp.Success {
			fmt.Fprintln(out, "✓ Playback resumed on active halpradio instance.")
			return true, nil
		}
	}

	fs := flag.NewFlagSet("play", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() {
		PrintPlayHelp(out)
	}

	cfg, _ := util.LoadConfig()

	var vol int
	var backend, durationStr, genre string
	var isRandom, isJSON, remote bool

	fs.IntVar(&vol, "volume", cfg.Volume, "Playback volume (0-100)")
	fs.IntVar(&vol, "v", cfg.Volume, "Playback volume (shorthand)")
	fs.IntVar(&vol, "vol", cfg.Volume, "Playback volume (shorthand)")
	fs.StringVar(&backend, "backend", cfg.PlayerBackend, "Audio backend (auto, native, mpv, vlc, ffplay, mplayer, mpg123)")
	fs.StringVar(&backend, "b", cfg.PlayerBackend, "Audio backend (shorthand)")
	fs.StringVar(&durationStr, "duration", "", "Play for a specific duration then stop (e.g. 30m, 1h, 15s)")
	fs.StringVar(&durationStr, "d", "", "Play for a specific duration then stop (shorthand)")
	fs.StringVar(&durationStr, "timeout", "", "Play for a specific duration then stop")
	fs.StringVar(&durationStr, "t", "", "Play for a specific duration then stop (shorthand)")
	fs.StringVar(&genre, "genre", "", "Filter random or search station by genre")
	fs.StringVar(&genre, "g", "", "Filter by genre (shorthand)")
	fs.BoolVar(&isRandom, "random", false, "Play a random station")
	fs.BoolVar(&isRandom, "r", false, "Play a random station (shorthand)")
	fs.BoolVar(&isJSON, "json", false, "Output track events as JSON")
	fs.BoolVar(&isJSON, "j", false, "Output track events as JSON (shorthand)")
	fs.BoolVar(&remote, "remote", false, "Forward play action to running halpradio instance over IPC")

	flagsWithVal := map[string]bool{
		"volume": true, "v": true, "vol": true,
		"backend": true, "b": true,
		"duration": true, "d": true, "timeout": true, "t": true,
		"genre": true, "g": true,
	}
	reorderedArgs := reorderFlagsFirst(args, flagsWithVal)

	if err := fs.Parse(reorderedArgs); err != nil {
		if err == flag.ErrHelp {
			return true, nil
		}
		return false, err
	}

	if remote {
		resp, err := desktop.SendIPCCommand("", "play")
		if err != nil {
			fmt.Fprintf(out, "Remote play error: %v\n", err)
			return false, err
		}
		if isJSON {
			data, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Fprintln(out, string(data))
		} else {
			fmt.Fprintln(out, "✓ Sent play command to running halpradio instance.")
		}
		return true, nil
	}

	target := strings.Join(fs.Args(), " ")
	if strings.EqualFold(target, "random") || strings.EqualFold(target, "shuffle") {
		isRandom = true
		target = ""
	}

	store := loadStore(embeddedCatalog)
	allStations := store.GetAllStations()

	if len(allStations) == 0 && target == "" && !isRandom {
		fmt.Fprintln(out, "Error: No stations available in catalog.")
		return false, fmt.Errorf("empty station catalog")
	}

	// Resolve target station
	st, err := resolveStation(target, isRandom, genre, allStations)
	if err != nil {
		fmt.Fprintf(out, "Error: %v\n", err)
		fmt.Fprintln(out, "Run 'halpradio stations list' to explore available stations.")
		return false, err
	}

	if vol <= 0 || vol > 100 {
		if vol < 0 {
			vol = 0
		} else if vol > 100 {
			vol = 100
		} else {
			vol = 80
		}
	}

	var duration time.Duration
	if durationStr != "" {
		d, err := time.ParseDuration(durationStr)
		if err != nil {
			fmt.Fprintf(out, "Error: invalid duration format %q (use e.g. 45m, 1h, 30s): %v\n", durationStr, err)
			return false, err
		}
		duration = d
	}

	// Headless streaming
	return executeHeadlessPlay(st, vol, backend, duration, isJSON, out)
}

func resolveStation(target string, isRandom bool, genre string, all []radio.Station) (radio.Station, error) {
	trimmed := strings.TrimSpace(target)

	// 1. Random station
	if isRandom || strings.EqualFold(trimmed, "random") {
		candidates := all
		if genre != "" {
			candidates = radio.FilterWithLocation(all, "", genre, "", "")
		}
		if len(candidates) == 0 {
			return radio.Station{}, fmt.Errorf("no stations found matching genre %q", genre)
		}
		return candidates[rand.Intn(len(candidates))], nil
	}

	// 2. Direct HTTP/HTTPS audio URL
	if player.IsValidStreamURL(trimmed) {
		return radio.Station{
			ID:        "custom-stream",
			Name:      "Direct Audio Stream",
			URL:       trimmed,
			Genre:     "Direct Stream",
			Country:   "",
			Bitrate:   128,
			Codec:     "MP3",
			Broadcast: "Online",
		}, nil
	}

	// 3. If target is empty, default to first station
	if trimmed == "" {
		if len(all) > 0 {
			return all[0], nil
		}
		return radio.Station{}, fmt.Errorf("no stations in catalog")
	}

	// 4. Numeric index (1-based: e.g. "1", "2")
	if idx, err := strconv.Atoi(trimmed); err == nil && idx >= 1 && idx <= len(all) {
		return all[idx-1], nil
	}

	// 5. Exact station ID match
	for _, st := range all {
		if strings.EqualFold(st.ID, trimmed) {
			return st, nil
		}
	}

	// 6. Substring name match
	var matches []radio.Station
	for _, st := range all {
		if strings.Contains(strings.ToLower(st.Name), strings.ToLower(trimmed)) {
			matches = append(matches, st)
		}
	}
	if len(matches) > 0 {
		return matches[0], nil
	}

	// 7. Genre search fallback
	genreMatches := radio.FilterWithLocation(all, trimmed, "", "", "")
	if len(genreMatches) > 0 {
		return genreMatches[0], nil
	}

	return radio.Station{}, fmt.Errorf("station %q not found in catalog", target)
}

func executeHeadlessPlay(st radio.Station, vol int, backend string, duration time.Duration, isJSON bool, out io.Writer) (bool, error) {
	if !isJSON {
		fmt.Fprintf(out, "📻 Streaming: %s\n", st.Name)
		fmt.Fprintf(out, "📡 Stream:    %s\n", st.URL)
		bitrateStr := ""
		if st.Bitrate > 0 {
			bitrateStr = fmt.Sprintf(" (%d kbps %s)", st.Bitrate, st.Codec)
		}
		locStr := ""
		if st.Country != "" {
			locStr = fmt.Sprintf(" | Location: %s %s", radio.CountryFlagForCode(st.Country), st.Country)
		}
		genreStr := ""
		if st.Genre != "" {
			genreStr = fmt.Sprintf(" | Genre: %s", st.Genre)
		}
		fmt.Fprintf(out, "🔊 Volume:    %d%%%s%s%s\n", vol, bitrateStr, genreStr, locStr)
		if duration > 0 {
			fmt.Fprintf(out, "⏱  Timer:     %v (auto-stops at expiry)\n", duration)
		}
		fmt.Fprintln(out, "─────────────────────────────────────────────────────────────────────────────")
		fmt.Fprintln(out, "Press Ctrl+C to stop streaming.")
		fmt.Fprintln(out, "")
	}

	pm := player.NewManager(backend, vol, func(info player.TrackInfo) {
		if isJSON {
			ev := TrackChangeJSONEvent{
				Event:       "track_change",
				StationID:   info.StationID,
				StationName: info.StationName,
				TrackTitle:  info.TrackTitle,
				Timestamp:   time.Now().Format(time.RFC3339),
			}
			data, _ := json.Marshal(ev)
			fmt.Fprintln(out, string(data))
		} else {
			if info.TrackTitle != "" && info.TrackTitle != st.Name {
				fmt.Fprintf(out, "♪ Now Playing: %s\n", info.TrackTitle)
			}
		}
	})
	defer pm.Close()

	if err := pm.Play(st); err != nil {
		fmt.Fprintf(out, "Playback error: %v\n", err)
		return false, err
	}

	if isJSON {
		ev := map[string]interface{}{
			"event":   "playback_started",
			"station": st.Name,
			"url":     st.URL,
			"volume":  vol,
			"backend": pm.ActiveBackend(),
		}
		data, _ := json.Marshal(ev)
		fmt.Fprintln(out, string(data))
	}

	// Trap OS interrupts (SIGINT, SIGTERM, Ctrl+C)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	var timerCh <-chan time.Time
	if duration > 0 {
		timerCh = time.After(duration)
	}

	select {
	case <-sigCh:
		if !isJSON {
			fmt.Fprintln(out, "\n⏹ Playback stopped by user.")
		}
		_ = pm.Stop()
		return true, nil

	case <-timerCh:
		if !isJSON {
			fmt.Fprintf(out, "\n⏱ Duration (%v) elapsed. Stopped streaming.\n", duration)
		}
		_ = pm.Stop()
		return true, nil
	}
}

func printPlayHelp(out io.Writer) {
	PrintPlayHelp(out)
}

// reorderFlagsFirst reorganizes arguments so flags and their values appear before positional arguments.
// This allows Go's flag.FlagSet to parse flags that follow positional arguments (e.g. `play 2 --volume 3`).
func reorderFlagsFirst(args []string, flagsWithVal map[string]bool) []string {
	var flags []string
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			flags = append(flags, arg)
			flagName := strings.TrimLeft(strings.Split(arg, "=")[0], "-")
			if flagsWithVal[flagName] && !strings.Contains(arg, "=") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++
				flags = append(flags, args[i])
			}
		} else {
			positional = append(positional, arg)
		}
	}
	return append(flags, positional...)
}
