package app

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/debuglog"
	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/theme"
	"github.com/halpworld/halpradio/pkg/ui"
	"github.com/halpworld/halpradio/pkg/util"
)

var Version = "0.8.1"

type AppInstance struct {
	Program      *tea.Program
	Player       *player.Manager
	Config       util.Config
	Store        *radio.Store
	Desktop      *desktop.Manager
	PluginMgr    *plugin.Manager
	PartySession *party.PartySession

	// DebugLogPath is the diagnostic log file for this run, or "" when
	// diagnostic logging is off.
	DebugLogPath string
}

// formatPlaybackInfo interpolates %s, %t, %a, %T, %p, %v, %b, %r into a custom status template.
func formatPlaybackInfo(st *desktop.PlaybackInfo, tmpl string) string {
	if tmpl == "" {
		return ""
	}
	if st == nil {
		st = &desktop.PlaybackInfo{Status: "stopped"}
	}
	stationName := util.SanitizeDisplay(st.StationName, 256)
	if stationName == "" {
		stationName = util.SanitizeDisplay(st.Station, 256)
	}
	track := util.SanitizeDisplay(st.Track, 512)
	if track == "" && (st.Artist != "" || st.Title != "") {
		artist := util.SanitizeDisplay(st.Artist, 256)
		title := util.SanitizeDisplay(st.Title, 256)
		if artist != "" && title != "" {
			track = fmt.Sprintf("%s - %s", artist, title)
		} else if title != "" {
			track = title
		}
	}
	artist := util.SanitizeDisplay(st.Artist, 256)
	title := util.SanitizeDisplay(st.Title, 256)
	if artist == "" && title == "" && track != "" {
		artist, title = desktop.SplitArtistTitle(track)
	}

	cleanStatus := util.SanitizeDisplay(st.Status, 32)
	cleanBackend := util.SanitizeDisplay(st.Backend, 32)

	res := strings.ReplaceAll(tmpl, "%%", "\x00")
	res = strings.ReplaceAll(res, "%s", stationName)
	res = strings.ReplaceAll(res, "%t", track)
	res = strings.ReplaceAll(res, "%a", artist)
	res = strings.ReplaceAll(res, "%T", title)
	res = strings.ReplaceAll(res, "%p", strings.ToUpper(cleanStatus))
	res = strings.ReplaceAll(res, "%v", fmt.Sprintf("%d", st.Volume))
	res = strings.ReplaceAll(res, "%b", cleanBackend)
	res = strings.ReplaceAll(res, "%r", fmt.Sprintf("%d", st.Bitrate))
	res = strings.ReplaceAll(res, "\x00", "%")
	return res
}

// RunCurrent handles `halpradio current [--json] [--format "<tmpl>"]` CLI query mode for tmux / Waybar / status bars.
func RunCurrent(args []string, out io.Writer) (bool, error) {
	if len(args) > 0 && IsHelpArg(args[0]) {
		PrintCurrentHelp(out)
		return true, nil
	}

	isJSON := false
	var formatTmpl string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--json" || arg == "-json" || arg == "-j" {
			isJSON = true
		} else if strings.HasPrefix(arg, "--format=") {
			formatTmpl = strings.TrimPrefix(arg, "--format=")
		} else if strings.HasPrefix(arg, "-f=") {
			formatTmpl = strings.TrimPrefix(arg, "-f=")
		} else if (arg == "--format" || arg == "-format" || arg == "-f") && i+1 < len(args) {
			formatTmpl = args[i+1]
			i++
		}
	}

	resp, err := desktop.SendIPCCommand("", "current")
	if err != nil {
		if formatTmpl != "" {
			fmt.Fprintln(out, formatPlaybackInfo(&desktop.PlaybackInfo{Status: "stopped"}, formatTmpl))
			return true, nil
		}
		if isJSON {
			errPayload := map[string]string{
				"status": "stopped",
				"error":  err.Error(),
			}
			data, _ := json.MarshalIndent(errPayload, "", "  ")
			fmt.Fprintln(out, string(data))
			return false, err
		}
		fmt.Fprintf(out, "Error: %s\n", safe(err.Error()))
		return false, err
	}

	if !resp.Success {
		if formatTmpl != "" {
			fmt.Fprintln(out, formatPlaybackInfo(&desktop.PlaybackInfo{Status: "stopped"}, formatTmpl))
			return true, nil
		}
		if isJSON {
			errPayload := map[string]string{
				"status": "stopped",
				"error":  resp.Message,
			}
			data, _ := json.MarshalIndent(errPayload, "", "  ")
			fmt.Fprintln(out, string(data))
			return false, fmt.Errorf("%s", resp.Message)
		}
		fmt.Fprintf(out, "Error: %s\n", safe(resp.Message))
		return false, fmt.Errorf("%s", resp.Message)
	}

	if formatTmpl != "" {
		fmt.Fprintln(out, formatPlaybackInfo(resp.Status, formatTmpl))
		return true, nil
	}

	if isJSON {
		if resp.Status != nil {
			data, err := json.MarshalIndent(resp.Status, "", "  ")
			if err != nil {
				return false, err
			}
			fmt.Fprintln(out, string(data))
		} else {
			fmt.Fprintln(out, "{}")
		}
		return true, nil
	}

	// Plain text output for status bars
	if resp.Status == nil {
		fmt.Fprintln(out, "[STOPPED]")
		return true, nil
	}

	st := resp.Status
	station := util.SanitizeDisplay(st.StationName, 256)
	if station == "" {
		station = util.SanitizeDisplay(st.Station, 256)
	}
	track := util.SanitizeDisplay(st.Track, 512)
	if track == "" && (st.Artist != "" || st.Title != "") {
		artist := util.SanitizeDisplay(st.Artist, 256)
		title := util.SanitizeDisplay(st.Title, 256)
		if artist != "" && title != "" {
			track = fmt.Sprintf("%s - %s", artist, title)
		} else if title != "" {
			track = title
		}
	}

	switch strings.ToLower(st.Status) {
	case "playing":
		if station != "" && track != "" && track != station {
			fmt.Fprintf(out, "%s: %s\n", station, track)
		} else if station != "" {
			fmt.Fprintf(out, "%s\n", station)
		} else if track != "" {
			fmt.Fprintf(out, "%s\n", track)
		} else {
			fmt.Fprintln(out, "Streaming Live...")
		}
	case "paused":
		if station != "" && track != "" && track != station {
			fmt.Fprintf(out, "[PAUSED] %s: %s\n", station, track)
		} else if station != "" {
			fmt.Fprintf(out, "[PAUSED] %s\n", station)
		} else {
			fmt.Fprintln(out, "[PAUSED]")
		}
	case "stopped":
		fmt.Fprintln(out, "[STOPPED]")
	default:
		cleanStatus := util.SanitizeDisplay(st.Status, 32)
		if station != "" {
			fmt.Fprintf(out, "[%s] %s\n", strings.ToUpper(cleanStatus), station)
		} else {
			fmt.Fprintf(out, "[%s]\n", strings.ToUpper(cleanStatus))
		}
	}

	return true, nil
}

// RunStatus handles `halpradio status [--json] [--format "<tmpl>"]` CLI query mode.
func RunStatus(args []string, out io.Writer) (bool, error) {
	isJSON := false
	var formatTmpl string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--json" || arg == "-json" || arg == "-j" {
			isJSON = true
		} else if strings.HasPrefix(arg, "--format=") {
			formatTmpl = strings.TrimPrefix(arg, "--format=")
		} else if strings.HasPrefix(arg, "-f=") {
			formatTmpl = strings.TrimPrefix(arg, "-f=")
		} else if (arg == "--format" || arg == "-format" || arg == "-f") && i+1 < len(args) {
			formatTmpl = args[i+1]
			i++
		}
	}

	if isJSON || formatTmpl != "" {
		return RunCurrent(args, out)
	}

	resp, err := desktop.SendIPCCommand("", "status")
	if err != nil {
		fmt.Fprintf(out, "Status query error: %s\n", safe(err.Error()))
		return false, err
	}

	if !resp.Success {
		fmt.Fprintf(out, "Error: %s\n", util.SanitizeDisplay(resp.Message, 256))
		return false, fmt.Errorf("%s", resp.Message)
	}

	if resp.Status != nil {
		stName := util.SanitizeDisplay(resp.Status.StationName, 256)
		if stName == "" {
			stName = util.SanitizeDisplay(resp.Status.Station, 256)
		}
		cleanStatus := util.SanitizeDisplay(resp.Status.Status, 32)
		cleanBackend := util.SanitizeDisplay(resp.Status.Backend, 32)
		if stName != "" {
			track := util.SanitizeDisplay(resp.Status.Track, 512)
			if track == "" {
				track = stName
			}
			fmt.Fprintf(out, "[%s] %s - %s (vol: %d%%, backend: %s)\n", cleanStatus, stName, track, resp.Status.Volume, cleanBackend)
		} else {
			fmt.Fprintf(out, "[%s] Volume: %d%%\n", cleanStatus, resp.Status.Volume)
		}
	} else {
		fmt.Fprintln(out, "[STOPPED]")
	}
	return true, nil
}

// RunRemote executes an IPC command against an active halpradio instance.
func RunRemote(args []string, out io.Writer) (bool, error) {
	if len(args) == 0 || IsHelpArg(args[0]) {
		PrintRemoteHelp(out)
		return true, nil
	}

	actionStr := args[0]
	isJSON := false
	for _, arg := range args[1:] {
		if arg == "--json" || arg == "-json" || arg == "-j" {
			isJSON = true
		}
	}

	if actionStr == "current" {
		return RunCurrent(args[1:], out)
	}
	if actionStr == "status" && isJSON {
		return RunCurrent(args[1:], out)
	}

	resp, err := desktop.SendIPCCommand("", actionStr)
	if err != nil {
		if isJSON {
			errPayload := map[string]string{
				"status": "error",
				"error":  err.Error(),
			}
			data, _ := json.MarshalIndent(errPayload, "", "  ")
			fmt.Fprintln(out, string(data))
			return false, err
		}
		fmt.Fprintf(out, "Remote control error: %s\n", safe(err.Error()))
		return false, err
	}

	if !resp.Success {
		if isJSON {
			errPayload := map[string]string{
				"status": "error",
				"error":  resp.Message,
			}
			data, _ := json.MarshalIndent(errPayload, "", "  ")
			fmt.Fprintln(out, string(data))
			return false, fmt.Errorf("%s", resp.Message)
		}
		fmt.Fprintf(out, "Error: %s\n", safe(resp.Message))
		return false, fmt.Errorf("%s", resp.Message)
	}

	if isJSON {
		if resp.Status != nil {
			data, err := json.MarshalIndent(resp.Status, "", "  ")
			if err != nil {
				return false, err
			}
			fmt.Fprintln(out, string(data))
		} else {
			respJSON := map[string]interface{}{
				"success": resp.Success,
				"message": resp.Message,
			}
			data, _ := json.MarshalIndent(respJSON, "", "  ")
			fmt.Fprintln(out, string(data))
		}
		return true, nil
	}

	if resp.Status != nil {
		stName := resp.Status.StationName
		if stName == "" {
			stName = resp.Status.Station
		}
		if stName != "" {
			track := resp.Status.Track
			if track == "" {
				track = stName
			}
			fmt.Fprintf(out, "[%s] %s - %s (vol: %d%%, backend: %s)\n", safe(resp.Status.Status), safe(stName), safe(track), resp.Status.Volume, safe(resp.Status.Backend))
		} else {
			fmt.Fprintf(out, "[%s] Volume: %d%%\n", safe(resp.Status.Status), resp.Status.Volume)
		}
	} else {
		fmt.Fprintf(out, "%s\n", safe(resp.Message))
	}
	return true, nil
}

// RunHelp prints comprehensive CLI usage, commands, flags, and workflow examples.
func RunHelp(out io.Writer) {
	PrintRootHelp(out)
}

// SetupApp parses CLI flags, loads configuration, and initializes the AppInstance.
func SetupApp(args []string, embeddedCatalog []byte, out io.Writer) (*AppInstance, bool, error) {
	if !isInfoOnlyArgs(args) {
		warnUnreadableConfig()
	}
	if len(args) > 0 {
		switch {
		case args[0] == "help":
			done := RouteHelp(args[1:], embeddedCatalog, out)
			if !done {
				return nil, true, fmt.Errorf("unknown help topic: %s", strings.Join(args[1:], " "))
			}
			return nil, true, nil
		case IsHelpArg(args[0]):
			PrintRootHelp(out)
			return nil, true, nil
		case args[0] == "version" || args[0] == "-version" || args[0] == "--version" || args[0] == "-v":
			fmt.Fprintf(out, "halpradio v%s - LazyVim-inspired Terminal Internet Radio Streamer\n", Version)
			return nil, true, nil
		case args[0] == "play":
			_, err := RunPlay(args[1:], embeddedCatalog, out)
			return nil, true, err
		case args[0] == "stations" || args[0] == "station":
			_, err := RunStations(args[1:], embeddedCatalog, out)
			return nil, true, err
		case args[0] == "search" || args[0] == "find":
			_, err := RunStations(append([]string{"search"}, args[1:]...), embeddedCatalog, out)
			return nil, true, err
		case args[0] == "volume" || args[0] == "vol":
			_, err := RunVolume(args[1:], out)
			return nil, true, err
		case args[0] == "remote":
			_, err := RunRemote(args[1:], out)
			return nil, true, err
		case args[0] == "current":
			_, err := RunCurrent(args[1:], out)
			return nil, true, err
		case args[0] == "status":
			_, err := RunStatus(args[1:], out)
			return nil, true, err
		case args[0] == "toggle" || args[0] == "pause" || args[0] == "stop" || args[0] == "next" || args[0] == "prev" || args[0] == "volup" || args[0] == "voldown" || args[0] == "mute" || args[0] == "random":
			if len(args) > 1 && IsHelpArg(args[1]) {
				PrintPlaybackControlHelp(args[0], out)
				return nil, true, nil
			}
			_, err := RunRemote(args, out)
			return nil, true, err
		case args[0] == "party":
			return RunParty(args[1:], embeddedCatalog, out)
		case args[0] == "theme" || args[0] == "themes":
			_, err := RunThemeCLI(args[1:], out)
			return nil, true, err
		case args[0] == "plugin" || args[0] == "plugins":
			_, err := RunPluginCLI(args[1:], out)
			return nil, true, err
		case args[0] == "update-stations" || args[0] == "update-catalog":
			if len(args) > 1 && IsHelpArg(args[1]) {
				PrintUpdateStationsHelp(out)
				return nil, true, nil
			}
			cfg, _ := util.LoadConfig()
			updater := newCatalogUpdater(cfg, embeddedCatalog)
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			updated, count, err := updater.CheckAndUpdate(ctx, true)
			if err != nil {
				fmt.Fprintf(out, "Catalog update failed: %s\n", safe(err.Error()))
				return nil, true, err
			}
			if updated {
				fmt.Fprintf(out, "✓ Successfully updated station catalog (%d stations cached to %s)\n", count, util.GetCatalogCacheFile())
			} else {
				fmt.Fprintf(out, "✓ Station catalog is already up to date (%d stations)\n", count)
			}
			return nil, true, nil
		case !strings.HasPrefix(args[0], "-"):
			sugg := SuggestCommand(args[0], RootCommands)
			if sugg != "" {
				fmt.Fprintf(out, "%s: %q is not a valid halpradio command.\n\nDid you mean this?\n  %s\n\nRun 'halpradio --help' for available commands.\n",
					styleError.Render("halpradio"),
					args[0],
					styleSuggestion.Render(sugg),
				)
			} else {
				fmt.Fprintf(out, "%s: %q is not a valid halpradio command. Run 'halpradio --help' for available commands.\n",
					styleError.Render("halpradio"),
					args[0],
				)
			}
			return nil, true, fmt.Errorf("unknown command: %s", args[0])
		}
	}

	fs := flag.NewFlagSet("halpradio", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() {
		PrintRootHelp(out)
	}

	backendFlag := fs.String("backend", "auto", "Audio player backend: auto, native, mpv, vlc, ffplay, mplayer, mpg123")
	fs.StringVar(backendFlag, "b", "auto", "Audio player backend (shorthand)")
	themeFlag := fs.String("theme", "", "Color theme")
	fs.StringVar(themeFlag, "t", "", "Color theme (shorthand)")
	versionFlag := fs.Bool("version", false, "Show halpradio version")
	fs.BoolVar(versionFlag, "v", false, "Show halpradio version (shorthand)")
	helpFlag := fs.Bool("help", false, "Show halpradio help")
	fs.BoolVar(helpFlag, "h", false, "Show halpradio help (shorthand)")
	updateCatalogFlag := fs.Bool("update-catalog", false, "Update stations catalog from remote repository")
	notificationsFlag := fs.Bool("notifications", true, "Enable desktop notifications on song change")
	autoPauseFlag := fs.Bool("autopause", true, "Pause playback when headphones disconnect (e.g. AirPods taken out)")
	mprisFlag := fs.Bool("mpris", true, "Enable Linux MPRIS v2 D-Bus remote interface")
	ipcFlag := fs.Bool("ipc", true, "Enable local IPC socket for CLI remote control")
	discordFlag := fs.Bool("discord", true, "Enable Discord Rich Presence (RPC)")
	experimentalTunerFlag := fs.Bool("experimental-tuner", false, "Enable experimental analog frequency tuner (on hold)")
	fingerprintFlag := fs.Bool("fingerprint", true, "Enable acoustic stream fingerprinting (AcoustID)")
	autoIdentifyFlag := fs.Bool("auto-identify", true, "Automatically identify songs when station lacks metadata")
	aiDJFlag := fs.Bool("ai-dj", false, "Enable Local AI Radio DJ track announcements")
	djPersonaFlag := fs.String("dj-persona", "", "AI DJ persona (late-night-chill, retro-80s, cyberpunk, bbc-presenter)")
	debugFlag := fs.Bool("debug", false, "Write a diagnostic log for bug reports (see --debug-log)")
	debugLogFlag := fs.String("debug-log", "", "Path for the diagnostic log (default ~/.config/halpradio/debug.log)")

	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil, true, nil
		}
		return nil, false, err
	}

	if *helpFlag {
		PrintRootHelp(out)
		return nil, true, nil
	}

	if *versionFlag {
		fmt.Fprintf(out, "halpradio v%s - LazyVim-inspired Terminal Internet Radio Streamer\n", Version)
		return nil, true, nil
	}

	_ = util.EnsureConfigDir()

	debugLogPath := ""
	if *debugFlag || *debugLogFlag != "" || debuglog.EnvEnabled() {
		target := *debugLogFlag
		if target == "" {
			target = util.GetDebugLogFile()
		}
		p, err := debuglog.Init(target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: could not open debug log: %v\n", err)
		} else {
			debugLogPath = p
			debuglog.Header(Version, map[string]string{"debug_log": p})
		}
	}

	_ = theme.EnsureExampleTheme(util.GetThemesDir())
	_, _ = theme.LoadCustomThemes(util.GetThemesDir())

	// A config that exists but cannot be read comes back fail-closed
	// (plugins and network extras off); keep it rather than replacing it
	// with permissive defaults.
	cfg, _ := util.LoadConfig()

	if *updateCatalogFlag {
		updater := newCatalogUpdater(cfg, embeddedCatalog)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		updated, count, err := updater.CheckAndUpdate(ctx, true)
		if err != nil {
			fmt.Fprintf(out, "Catalog update failed: %s\n", safe(err.Error()))
			return nil, true, err
		}
		if updated {
			fmt.Fprintf(out, "✓ Successfully updated station catalog (%d stations cached to %s)\n", count, util.GetCatalogCacheFile())
		} else {
			fmt.Fprintf(out, "✓ Station catalog is already up to date (%d stations)\n", count)
		}
		return nil, true, nil
	}
	if *backendFlag != "" && *backendFlag != "auto" {
		cfg.PlayerBackend = *backendFlag
	}
	if *themeFlag != "" {
		cfg.Theme = *themeFlag
	}
	if !*notificationsFlag {
		cfg.SongNotifications = false
	}
	if !*autoPauseFlag {
		cfg.AutoPause = false
	}
	if !*mprisFlag {
		cfg.MPRISEnabled = false
	}
	if !*ipcFlag {
		cfg.IPCEnabled = false
	}
	if !*discordFlag {
		cfg.DiscordRPC = false
	}
	if *experimentalTunerFlag {
		cfg.ExperimentalTuner = true
	}
	if !*fingerprintFlag {
		cfg.FingerprintEnabled = false
	}
	if !*autoIdentifyFlag {
		cfg.AutoIdentify = false
	}
	if *aiDJFlag {
		cfg.AIDJEnabled = true
	}
	if strings.TrimSpace(*djPersonaFlag) != "" {
		cfg.AIDJPersona = strings.TrimSpace(*djPersonaFlag)
	}

	store := radio.NewStore()
	if err := store.Load(embeddedCatalog); err != nil {
		fmt.Fprintf(os.Stderr, "Warning loading station store: %s\n", safe(err.Error()))
	}
	// The cached catalog download is only used while auto-update is on.
	if cfg.CatalogAutoUpdate {
		_ = store.ReloadBundledFromCache()
	}

	var program *tea.Program

	pm := player.NewManager(cfg.PlayerBackend, cfg.Volume, func(info player.TrackInfo) {
		if program != nil {
			program.Send(ui.TrackUpdatedMsg(info))
		}
	})
	pm.SetDSP(loadDSPSettings(cfg))
	pm.SetOnAutoPause(func() {
		if program != nil {
			program.Send(ui.AutoPauseMsg{})
		}
	})
	pm.SetAutoPause(cfg.AutoPause)

	pluginMgr := plugin.NewManager(cfg.PluginRegistryURL)
	pluginMgr.SetNotifyHandler(func(title, msg string) {
		if program != nil {
			program.Send(ui.PluginNotificationMsg{Title: title, Message: msg})
		}
	})
	pluginMgr.SetFlashHandler(func(msg string) {
		if program != nil {
			program.Send(ui.PluginFlashMsg(msg))
		}
	})
	// plugins_enabled: false keeps every plugin stopped (absent key = enabled).
	pluginMgr.SetPluginsDisabled(!cfg.PluginsEnabled)
	_ = pluginMgr.Init()

	model := ui.NewModel(store, pm, cfg)
	model.SetPluginManager(pluginMgr)
	model.CatalogMinStations = embeddedStationCount(embeddedCatalog)
	if note := storeWarningStatus(store); note != "" {
		model.StatusMessage = note
	}

	desktopMgr := desktop.NewManager(desktop.DesktopConfig{
		NotificationsEnabled: cfg.SongNotifications,
		MPRISEnabled:         cfg.MPRISEnabled,
		IPCEnabled:           cfg.IPCEnabled,
		DiscordEnabled:       cfg.DiscordRPC,
		DiscordClientID:      cfg.DiscordClientID,
	}, func(action desktop.MediaAction) {
		if program == nil {
			return
		}
		switch action {
		case desktop.ActionPlayPause:
			program.Send(ui.MediaPlayPauseMsg{})
		case desktop.ActionPlay:
			program.Send(ui.MediaPlayMsg{})
		case desktop.ActionPause:
			program.Send(ui.MediaPauseMsg{})
		case desktop.ActionStop:
			program.Send(ui.MediaStopMsg{})
		case desktop.ActionNextStation:
			program.Send(ui.MediaNextMsg{})
		case desktop.ActionPrevStation:
			program.Send(ui.MediaPrevMsg{})
		case desktop.ActionVolumeUp:
			program.Send(ui.MediaVolUpMsg{})
		case desktop.ActionVolumeDown:
			program.Send(ui.MediaVolDownMsg{})
		case desktop.ActionMute:
			program.Send(ui.MediaMuteMsg{})
		case desktop.ActionRandom:
			program.Send(ui.MediaRandomMsg{})
		case desktop.ActionQuit:
			program.Send(ui.MediaQuitMsg{})
		}
	})

	desktopMgr.SetPartyHandler(func(action desktop.MediaAction, payload string) error {
		return dispatchPartyIPC(programSender(program), action, payload)
	})
	if note := reportIPCStart(nil, desktopMgr); note != "" {
		model.StatusMessage = note
	}

	model.SetDesktop(desktopMgr)

	program = tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	debuglog.Logf("session", "config: backend=%s theme=%s notifications=%t autopause=%t mpris=%t ipc=%t discord=%t",
		cfg.PlayerBackend, cfg.Theme, cfg.SongNotifications, cfg.AutoPause, cfg.MPRISEnabled, cfg.IPCEnabled, cfg.DiscordRPC)

	return &AppInstance{
		Program:      program,
		Player:       pm,
		Config:       cfg,
		Store:        store,
		Desktop:      desktopMgr,
		PluginMgr:    pluginMgr,
		DebugLogPath: debugLogPath,
	}, false, nil
}

func Run(embeddedCatalog []byte) {
	appInst, isDone, err := SetupApp(os.Args[1:], embeddedCatalog, os.Stdout)
	if err != nil {
		os.Exit(1)
	}
	if isDone {
		os.Exit(0)
	}

	runErr := func() error {
		_, err := appInst.Program.Run()
		return err
	}()

	// Clean up player, desktop, party, and plugin services on exit
	_ = appInst.Player.Close()
	if appInst.Desktop != nil {
		_ = appInst.Desktop.Close()
	}
	if appInst.PluginMgr != nil {
		_ = appInst.PluginMgr.Close()
	}
	if appInst.PartySession != nil {
		_ = appInst.PartySession.Close()
	}

	debuglog.Close()
	if appInst.DebugLogPath != "" {
		// Printed after the alternate screen is restored so it survives on screen.
		fmt.Fprintf(os.Stderr, "Debug log written to %s — attach it to your bug report.\n", appInst.DebugLogPath)
	}

	if runErr != nil {
		fmt.Fprintf(os.Stderr, "Error running halpradio: %v\n", runErr)
		os.Exit(1)
	}
}
