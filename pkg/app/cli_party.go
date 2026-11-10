package app

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/debuglog"
	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/party"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/ui"
	"github.com/halpworld/halpradio/pkg/ui/components"
	"github.com/halpworld/halpradio/pkg/util"
)

// RunParty handles `halpradio party <subcommand> [flags]`.
func RunParty(args []string, embeddedCatalog []byte, out io.Writer) (*AppInstance, bool, error) {
	if len(args) == 0 || IsHelpArg(args[0]) {
		PrintPartyHelp(out)
		return nil, true, nil
	}

	sub := strings.ToLower(args[0])
	subArgs := args[1:]

	switch sub {
	case "create", "new", "host":
		return runPartyCreate(subArgs, embeddedCatalog, out)
	case "join", "connect":
		return runPartyJoin(subArgs, embeddedCatalog, out)
	case "status", "info", "current":
		return runPartyStatus(subArgs, out)
	case "leave", "exit", "disconnect":
		return runPartyLeave(subArgs, out)
	case "react":
		return runPartyReact(subArgs, out)
	case "chat":
		return runPartyChat(subArgs, out)
	default:
		fmt.Fprintf(out, "Unknown party subcommand %q. Run 'halpradio party --help' for usage.\n", sub)
		return nil, true, fmt.Errorf("unknown party subcommand: %s", sub)
	}
}

func runPartyCreate(args []string, embeddedCatalog []byte, out io.Writer) (*AppInstance, bool, error) {
	if len(args) > 0 && IsHelpArg(args[0]) {
		PrintPartyHelp(out)
		return nil, true, nil
	}

	fs := flag.NewFlagSet("party create", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() { PrintPartyHelp(out) }

	// Keep the fail-closed config when config.yaml exists but is unreadable.
	cfg, _ := util.LoadConfig()

	var djPassStr, nickname, roomCode string
	var port int
	var isHeadless, isJSON bool

	defaultNick := cfg.PartyNickname
	if defaultNick == "" {
		defaultNick = os.Getenv("USER")
		if defaultNick == "" {
			defaultNick = "dj"
		}
	}

	fs.StringVar(&djPassStr, "dj-pass", "host", "DJ tuning permission: host (Host Only) or open (Open Democracy)")
	fs.StringVar(&djPassStr, "d", "host", "DJ tuning permission (shorthand)")
	fs.StringVar(&nickname, "nickname", defaultNick, "Listener handle shown to room peers")
	fs.StringVar(&nickname, "n", defaultNick, "Listener handle (shorthand)")
	fs.StringVar(&roomCode, "code", "", "Custom 16-character room code, e.g. "+party.ExampleRoomCode+" (optional, generated if omitted)")
	fs.IntVar(&port, "port", cfg.PartyPort, "TCP port to bind for peer mesh (default: auto)")
	fs.BoolVar(&isHeadless, "headless", false, "Stream synchronized audio headlessly in terminal")
	fs.BoolVar(&isJSON, "json", false, "Output status and events as JSON")
	fs.BoolVar(&isJSON, "j", false, "Output status and events as JSON (shorthand)")

	flagsWithVal := map[string]bool{
		"dj-pass": true, "d": true,
		"nickname": true, "n": true,
		"code": true, "port": true,
	}
	reorderedArgs := reorderFlagsFirst(args, flagsWithVal)
	if err := fs.Parse(reorderedArgs); err != nil {
		if err == flag.ErrHelp {
			return nil, true, nil
		}
		return nil, false, err
	}

	roomName := strings.Join(fs.Args(), " ")
	if roomName == "" {
		roomName = "team-focus"
	}

	if roomCode != "" {
		// A created code must be hard to guess, not just well-formed.
		if err := party.ValidateNewRoomCode(roomCode); err != nil {
			fmt.Fprintf(out, "Error: this room code cannot be used for a new room: %s\n", safe(err.Error()))
			return nil, false, fmt.Errorf("invalid room code: %w", err)
		}
	}
	nickname = partyCLINick(nickname, "host")

	djPass := party.DJPassHostOnly
	if strings.EqualFold(djPassStr, "open") || strings.EqualFold(djPassStr, "democracy") {
		djPass = party.DJPassOpenDemocracy
	}

	// 1. If halpradio is already running, create party room on the active instance via IPC
	resp, err := desktop.SendIPCCommandWithPayload("", "party-create", roomName)
	if err == nil && resp != nil && resp.Success {
		if isJSON {
			data, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Fprintln(out, string(data))
		} else {
			code := ""
			if resp.Status != nil && resp.Status.Party != nil && resp.Status.Party.RoomCode != "" {
				code = " #" + resp.Status.Party.RoomCode
			}
			fmt.Fprintf(out, "✓ Party room%s created on active halpradio instance!\n", safe(code))
		}
		return nil, true, nil
	}

	// 2. Halpradio is not running.
	if roomCode == "" {
		roomCode = party.GenerateRoomCode()
	} else {
		roomCode = party.NormalizeRoomCode(roomCode)
	}

	sessionCfg := party.SessionConfig{
		RoomCode: roomCode,
		RoomName: roomName,
		Nickname: nickname,
		IsHost:   true,
		DJPass:   djPass,
		Port:     port,
	}

	if isHeadless {
		return nil, true, executeHeadlessParty(sessionCfg, embeddedCatalog, cfg, isJSON, out)
	}

	// Launch TUI with party session initialized
	appInst, err := setupPartyApp(sessionCfg, embeddedCatalog, cfg, out)
	if err != nil {
		return nil, false, err
	}
	return appInst, false, nil
}

func runPartyJoin(args []string, embeddedCatalog []byte, out io.Writer) (*AppInstance, bool, error) {
	if len(args) > 0 && IsHelpArg(args[0]) {
		PrintPartyHelp(out)
		return nil, true, nil
	}

	fs := flag.NewFlagSet("party join", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.Usage = func() { PrintPartyHelp(out) }

	// Keep the fail-closed config when config.yaml exists but is unreadable.
	cfg, _ := util.LoadConfig()

	var nickname, address string
	var isHeadless, isJSON bool

	defaultNick := cfg.PartyNickname
	if defaultNick == "" {
		defaultNick = os.Getenv("USER")
		if defaultNick == "" {
			defaultNick = "listener"
		}
	}

	fs.StringVar(&nickname, "nickname", defaultNick, "Listener handle shown to room peers")
	fs.StringVar(&nickname, "n", defaultNick, "Listener handle (shorthand)")
	fs.StringVar(&address, "address", "", "Direct TCP host:port of host/peer (optional)")
	fs.StringVar(&address, "a", "", "Direct TCP host:port (shorthand)")
	fs.BoolVar(&isHeadless, "headless", false, "Stream synchronized audio headlessly in terminal")
	fs.BoolVar(&isJSON, "json", false, "Output status and events as JSON")
	fs.BoolVar(&isJSON, "j", false, "Output status and events as JSON (shorthand)")

	flagsWithVal := map[string]bool{
		"nickname": true, "n": true,
		"address": true, "a": true,
	}
	reorderedArgs := reorderFlagsFirst(args, flagsWithVal)
	if err := fs.Parse(reorderedArgs); err != nil {
		if err == flag.ErrHelp {
			return nil, true, nil
		}
		return nil, false, err
	}

	if len(fs.Args()) == 0 {
		fmt.Fprintf(out, "Error: invite or room code required (e.g. %s). Usage: halpradio party join <INVITE>\n",
			party.FormatInvite(party.ExampleRoomCode, party.ExampleFingerprint))
		return nil, false, fmt.Errorf("room code required")
	}

	code, hostPin, err := party.ParseInvite(fs.Args()[0])
	if err != nil {
		fmt.Fprintf(out, "Error: invite is invalid: %s\n", safe(err.Error()))
		return nil, false, fmt.Errorf("invalid room code: %w", err)
	}
	invite := party.FormatInvite(code, hostPin)
	nickname = partyCLINick(nickname, "listener")

	// 1. If halpradio is already running, join party room on active instance via IPC
	// Only the invite crosses the IPC socket; a peer address is never
	// forwarded to the running instance.
	resp, err := desktop.SendIPCCommandWithPayload("", "party-join", invite)
	if err == nil && resp != nil && resp.Success {
		if isJSON {
			data, _ := json.MarshalIndent(resp, "", "  ")
			fmt.Fprintln(out, string(data))
		} else {
			fmt.Fprintf(out, "✓ Connected active halpradio instance to party room #%s!\n", code)
			if hostPin == "" {
				fmt.Fprintln(out, "Note: you joined with a bare room code; check the host's fingerprint against the host's screen.")
			}
			if address != "" {
				fmt.Fprintln(out, "Note: --address is not passed to a running instance; enter it in the party room manager's Join form (ctrl+p) instead.")
			}
		}
		return nil, true, nil
	}

	// 2. Halpradio is not running.
	sessionCfg := party.SessionConfig{
		RoomCode: code,
		HostPin:  hostPin,
		Nickname: nickname,
		IsHost:   false,
		Port:     0,
	}

	if isHeadless {
		return nil, true, executeHeadlessParty(sessionCfg, embeddedCatalog, cfg, isJSON, out, address)
	}

	appInst, err := setupPartyApp(sessionCfg, embeddedCatalog, cfg, out, address)
	if err != nil {
		return nil, false, err
	}
	return appInst, false, nil
}

func runPartyStatus(args []string, out io.Writer) (*AppInstance, bool, error) {
	if len(args) > 0 && IsHelpArg(args[0]) {
		PrintPartyHelp(out)
		return nil, true, nil
	}

	isJSON := false
	for _, arg := range args {
		if arg == "--json" || arg == "-json" || arg == "-j" {
			isJSON = true
		}
	}

	resp, err := desktop.SendIPCCommand("", "party-status")
	if err != nil {
		if isJSON {
			errPayload := map[string]interface{}{
				"active": false,
				"error":  err.Error(),
			}
			data, _ := json.MarshalIndent(errPayload, "", "  ")
			fmt.Fprintln(out, string(data))
			return nil, false, err
		}
		fmt.Fprintf(out, "Status query error: %s\n", safe(err.Error()))
		return nil, false, err
	}

	if resp.Status == nil || resp.Status.Party == nil || !resp.Status.Party.Active {
		if isJSON {
			fmt.Fprintln(out, "{\n  \"active\": false\n}")
			return nil, true, nil
		}
		fmt.Fprintln(out, "👥 Party Room: Not currently in a party room.")
		fmt.Fprintln(out, "  Run 'halpradio party create' to host a room or 'halpradio party join <CODE>' to connect.")
		return nil, true, nil
	}

	p := resp.Status.Party
	if isJSON {
		data, _ := json.MarshalIndent(p, "", "  ")
		fmt.Fprintln(out, string(data))
		return nil, true, nil
	}

	role := "Listener"
	if p.IsHost {
		role = "Host (DJ)"
	}
	djPass := "Host Only"
	if p.DJPass == "open" {
		djPass = "Open Democracy"
	}

	stationName := resp.Status.StationName
	if stationName == "" {
		stationName = resp.Status.Station
	}
	if stationName == "" {
		stationName = "None"
	}

	fmt.Fprintf(out, "👥 TERMINAL PARTY ROOM: #%s\n", safe(p.RoomCode))
	if p.RoomName != "" {
		fmt.Fprintf(out, "  Room Name:  %s\n", safe(p.RoomName))
	}
	fmt.Fprintf(out, "  Your Role:  %s\n", role)
	fmt.Fprintf(out, "  Host (DJ):  %s\n", safe(p.Host))
	fmt.Fprintf(out, "  DJ Pass:    %s\n", djPass)
	fmt.Fprintf(out, "  Listeners:  %d (%s)\n", p.Listeners, safeJoin(p.Peers, ", "))
	fmt.Fprintf(out, "  Station:    %s\n", safe(stationName))
	if resp.Status.Track != "" && resp.Status.Track != stationName {
		fmt.Fprintf(out, "  Track:      %s\n", safe(resp.Status.Track))
	}
	return nil, true, nil
}

func runPartyLeave(args []string, out io.Writer) (*AppInstance, bool, error) {
	if len(args) > 0 && IsHelpArg(args[0]) {
		PrintPartyHelp(out)
		return nil, true, nil
	}

	isJSON := false
	for _, arg := range args {
		if arg == "--json" || arg == "-json" || arg == "-j" {
			isJSON = true
		}
	}

	resp, err := desktop.SendIPCCommand("", "party-leave")
	if err != nil {
		if isJSON {
			errPayload := map[string]interface{}{"success": false, "error": err.Error()}
			data, _ := json.MarshalIndent(errPayload, "", "  ")
			fmt.Fprintln(out, string(data))
			return nil, false, err
		}
		fmt.Fprintf(out, "Leave error: %s\n", safe(err.Error()))
		return nil, false, err
	}

	if isJSON {
		data, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Fprintln(out, string(data))
	} else {
		fmt.Fprintln(out, "✓ Left party room.")
	}
	return nil, true, nil
}

func runPartyReact(args []string, out io.Writer) (*AppInstance, bool, error) {
	if len(args) == 0 || IsHelpArg(args[0]) {
		fmt.Fprintln(out, "Usage: halpradio party react <1-5|emoji>")
		fmt.Fprintln(out, "  1: 🔥, 2: ❤️, 3: ☕, 4: 🚀, 5: 👀")
		return nil, true, nil
	}

	key := args[0]
	emoji := key
	switch key {
	case "1":
		emoji = "🔥"
	case "2":
		emoji = "❤️"
	case "3":
		emoji = "☕"
	case "4":
		emoji = "🚀"
	case "5":
		emoji = "👀"
	}

	_, err := desktop.SendIPCCommandWithPayload("", "party-react", emoji)
	if err != nil {
		fmt.Fprintf(out, "Reaction error: %s\n", safe(err.Error()))
		return nil, false, err
	}

	fmt.Fprintf(out, "✓ Sent reaction %s to party room\n", safe(emoji))
	return nil, true, nil
}

func runPartyChat(args []string, out io.Writer) (*AppInstance, bool, error) {
	if len(args) == 0 || IsHelpArg(args[0]) {
		fmt.Fprintln(out, "Usage: halpradio party chat <message>")
		return nil, true, nil
	}

	message := strings.Join(args, " ")
	_, err := desktop.SendIPCCommandWithPayload("", "party-chat", message)
	if err != nil {
		fmt.Fprintf(out, "Chat error: %s\n", safe(err.Error()))
		return nil, false, err
	}

	fmt.Fprintf(out, "✓ Sent chat ping: %s\n", safe(message))
	return nil, true, nil
}

func executeHeadlessParty(
	sessionCfg party.SessionConfig,
	embeddedCatalog []byte,
	cfg util.Config,
	isJSON bool,
	out io.Writer,
	directAddrs ...string,
) error {
	store := loadStore(embeddedCatalog)
	sess, err := party.NewPartySession(sessionCfg)
	if err != nil {
		return fmt.Errorf("failed creating party session: %w", err)
	}
	defer sess.Close()

	for _, addr := range directAddrs {
		if addr != "" {
			if err := sess.ConnectDirect(addr); err != nil {
				fmt.Fprintf(out, "Warning: could not connect to %s: %s\n", safe(addr), safe(err.Error()))
			}
		}
	}

	pm := player.NewManager(cfg.PlayerBackend, cfg.Volume, func(info player.TrackInfo) {
		if isJSON {
			ev := map[string]interface{}{
				"event":   "track_change",
				"station": info.StationName,
				"track":   info.TrackTitle,
				"time":    time.Now().Format(time.RFC3339),
			}
			d, _ := json.Marshal(ev)
			fmt.Fprintln(out, string(d))
		} else if info.TrackTitle != "" {
			fmt.Fprintf(out, "♪ Now Playing: %s\n", safe(info.TrackTitle))
		}
	})
	pm.SetDSP(loadDSPSettings(cfg))
	defer pm.Close()

	hs := &headlessSync{sess: sess, store: store, pm: pm, out: out, isJSON: isJSON}
	sess.SetHandlers(
		hs.handle,
		func(r party.FloatingReaction) {
			if !isJSON {
				hs.printf("✨ [Reaction] %s (%s)\n", safe(r.Emoji), safe(components.CleanPartyNick(r.Sender)))
			}
		},
		func(c party.ChatMessage) {
			if !isJSON {
				hs.printf("💬 [Chat] <%s> %s\n", safe(components.CleanPartyNick(c.Sender)), safe(c.Message))
			}
		},
		func(peers []*party.PeerInfo) {
			hs.printf("%s", headlessRoster(peers))
		},
		func(msg string) {
			if !isJSON {
				hs.printf("ℹ️ [Notice] %s\n", safe(ui.PartyFlashText(sess, msg)))
			}
		},
	)

	// If host, tune initial station if catalog is present
	if sessionCfg.IsHost {
		all := store.GetAllStations()
		if len(all) > 0 {
			st := all[0]
			_ = pm.Play(st)
			_ = sess.BroadcastStationChange(st.ID, st.Name, st.URL, st.Genre, st.Country, st.Codec, st.Bitrate, "playing")
		}
	}

	if !isJSON {
		roleStr := "Listener"
		if sessionCfg.IsHost {
			roleStr = "Host (DJ)"
		}
		fmt.Fprintln(out, "─────────────────────────────────────────────────────────────────────────────")
		fmt.Fprintf(out, "👥 HALPRADIO TERMINAL PARTY ROOM\n")
		fmt.Fprintf(out, "   Invite:    %s\n", safe(sess.Invite()))
		if sessionCfg.IsHost {
			fmt.Fprintf(out, "              %s\n", components.PartyInviteNote)
		}
		fmt.Fprintf(out, "   Role:      %s\n", roleStr)
		fmt.Fprintf(out, "   Nickname:  %s\n", safe(sessionCfg.Nickname))
		if sessionCfg.IsHost {
			fmt.Fprintf(out, "   Room Name: %s\n", safe(sessionCfg.RoomName))
			fmt.Fprintf(out, "   DJ Pass:   %s\n", sessionCfg.DJPass)
		}
		fmt.Fprint(out, headlessFingerprintLine(sessionCfg.IsHost, sessionCfg.Nickname, sess.Fingerprint(), sess.HostPin()))
		fmt.Fprintln(out, "   Zero Central Audio Relay: Audio pulled directly from stream source.")
		fmt.Fprintln(out, "─────────────────────────────────────────────────────────────────────────────")
		fmt.Fprintln(out, "Press Ctrl+C to disconnect and exit party.")
		fmt.Fprintln(out, "")
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	<-sigCh
	if !isJSON {
		fmt.Fprintln(out, "\n⏹ Leaving party room...")
	}
	return nil
}

func setupPartyApp(
	sessionCfg party.SessionConfig,
	embeddedCatalog []byte,
	cfg util.Config,
	out io.Writer,
	directAddrs ...string,
) (*AppInstance, error) {
	_ = util.EnsureConfigDir()

	store := radio.NewStore()
	if err := store.Load(embeddedCatalog); err != nil {
		fmt.Fprintf(os.Stderr, "Warning loading station store: %v\n", err)
	}
	if cfg.CatalogAutoUpdate {
		_ = store.ReloadBundledFromCache()
	}

	sess, err := party.NewPartySession(sessionCfg)
	if err != nil {
		return nil, fmt.Errorf("failed starting party session: %w", err)
	}

	for _, addr := range directAddrs {
		if addr != "" {
			if err := sess.ConnectDirect(addr); err != nil {
				fmt.Fprintf(out, "Warning: could not connect to %s: %s\n", safe(addr), safe(err.Error()))
			}
		}
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
	// plugins_enabled: false keeps every plugin stopped (PLG-10).
	pluginMgr.SetPluginsDisabled(!cfg.PluginsEnabled)
	_ = pluginMgr.Init()

	model := ui.NewModel(store, pm, cfg)
	model.SetPluginManager(pluginMgr)
	model.SetPartySession(sess)
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

	if sessionCfg.IsHost {
		all := store.GetAllStations()
		if len(all) > 0 {
			st := all[0]
			_ = pm.Play(st)
			model.PlayingID = st.ID
			_ = sess.BroadcastStationChange(st.ID, st.Name, st.URL, st.Genre, st.Country, st.Codec, st.Bitrate, "playing")
		}
	}
	model.SyncDesktop()

	program = tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// The room code is the room's only secret: never write it to the debug log.
	debuglog.Logf("session", "party app started: host=%t", sessionCfg.IsHost)

	return &AppInstance{
		Program:      program,
		Player:       pm,
		Config:       cfg,
		Store:        store,
		Desktop:      desktopMgr,
		PluginMgr:    pluginMgr,
		PartySession: sess,
	}, nil
}

// partyCLINick is the --nickname value with anything that reads as a
// fingerprint replaced by "…", or def when nothing is left.
func partyCLINick(nick, def string) string {
	if n := strings.TrimSpace(components.StripFingerprintRuns(nick)); n != "" {
		return n
	}
	return def
}
