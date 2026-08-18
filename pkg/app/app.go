package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/plugin"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/ui"
	"github.com/halpworld/halpradio/pkg/util"
)

var Version = "0.0.5"

type AppInstance struct {
	Program   *tea.Program
	Player    *player.Manager
	Config    util.Config
	Store     *radio.Store
	Desktop   *desktop.Manager
	PluginMgr *plugin.Manager
}

// RunPluginCLI handles plugin subcommands: list, install, remove, enable, disable, update.
func RunPluginCLI(args []string, out io.Writer) (bool, error) {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" {
		fmt.Fprintln(out, "halpradio plugin - Sandboxed Wasm Plugin Manager")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "Usage:")
		fmt.Fprintln(out, "  halpradio plugin list                  List installed and official registry plugins")
		fmt.Fprintln(out, "  halpradio plugin install <plugin-id>   Install a plugin from official registry")
		fmt.Fprintln(out, "  halpradio plugin enable <plugin-id>    Enable an installed plugin")
		fmt.Fprintln(out, "  halpradio plugin disable <plugin-id>   Disable an installed plugin")
		fmt.Fprintln(out, "  halpradio plugin remove <plugin-id>    Uninstall an installed plugin")
		fmt.Fprintln(out, "  halpradio plugin update <id|--all>     Update installed plugins from registry")
		return true, nil
	}

	cfg, _ := util.LoadConfig()
	mgr := plugin.NewManager(cfg.PluginRegistryURL)
	_ = mgr.Init()
	defer mgr.Close()

	cmd := args[0]
	switch cmd {
	case "list", "ls":
		installed := mgr.GetPlugins()
		fmt.Fprintln(out, "📦 INSTALLED PLUGINS:")
		if len(installed) == 0 {
			fmt.Fprintln(out, "  (No plugins installed yet. Run 'halpradio plugin install <id>')")
		} else {
			for _, p := range installed {
				status := "[Enabled]"
				if !p.State.PermissionsApproved {
					status = "[Perms Required]"
				} else if !p.State.Enabled {
					status = "[Disabled]"
				}
				perms := "Isolated"
				if len(p.Manifest.Permissions.Network) > 0 {
					perms = fmt.Sprintf("Net: %s", strings.Join(p.Manifest.Permissions.Network, ","))
				}
				fmt.Fprintf(out, "  • %-20s v%-6s %-16s %s (%s)\n", p.Manifest.ID, p.Manifest.Version, status, p.Manifest.Name, perms)
			}
		}

		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "🌐 OFFICIAL REGISTRY:")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		regIndex, err := mgr.RegistryClient().FetchRegistry(ctx)
		if err != nil {
			fmt.Fprintf(out, "  (Registry fetch note: %v)\n", err)
		} else {
			for _, rp := range regIndex.Plugins {
				isInst := false
				for _, ip := range installed {
					if ip.Manifest.ID == rp.ID {
						isInst = true
						break
					}
				}
				instTag := "[Available]"
				if isInst {
					instTag = "[Installed]"
				}
				fmt.Fprintf(out, "  • %-20s v%-6s %-12s %s by %s\n", rp.ID, rp.Version, instTag, rp.Name, rp.Author)
			}
		}
		return true, nil

	case "install", "add":
		if len(args) < 2 {
			fmt.Fprintln(out, "Error: plugin ID required. Usage: halpradio plugin install <plugin-id>")
			return false, fmt.Errorf("plugin ID required")
		}
		targetID := args[1]
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		regIndex, err := mgr.RegistryClient().FetchRegistry(ctx)
		if err != nil {
			fmt.Fprintf(out, "Failed fetching registry: %v\n", err)
			return false, err
		}

		var targetPlugin *plugin.RegistryPlugin
		for _, p := range regIndex.Plugins {
			if p.ID == targetID {
				targetPlugin = &p
				break
			}
		}
		if targetPlugin == nil {
			fmt.Fprintf(out, "Plugin %q not found in official registry.\n", targetID)
			return false, fmt.Errorf("plugin not found: %s", targetID)
		}

		fmt.Fprintf(out, "Installing %s (v%s)...\n", targetPlugin.Name, targetPlugin.Version)
		if err := mgr.InstallFromRegistry(ctx, *targetPlugin); err != nil {
			fmt.Fprintf(out, "Install error: %v\n", err)
			return false, err
		}
		_ = mgr.ApprovePermissions(targetPlugin.ID, true)
		_ = mgr.EnablePlugin(targetPlugin.ID)
		fmt.Fprintf(out, "✓ Successfully installed and enabled %s!\n", targetPlugin.Name)
		return true, nil

	case "enable":
		if len(args) < 2 {
			fmt.Fprintln(out, "Error: plugin ID required. Usage: halpradio plugin enable <plugin-id>")
			return false, fmt.Errorf("plugin ID required")
		}
		targetID := args[1]
		_ = mgr.ApprovePermissions(targetID, true)
		if err := mgr.EnablePlugin(targetID); err != nil {
			fmt.Fprintf(out, "Enable error: %v\n", err)
			return false, err
		}
		fmt.Fprintf(out, "✓ Enabled plugin %s\n", targetID)
		return true, nil

	case "disable":
		if len(args) < 2 {
			fmt.Fprintln(out, "Error: plugin ID required. Usage: halpradio plugin disable <plugin-id>")
			return false, fmt.Errorf("plugin ID required")
		}
		targetID := args[1]
		if err := mgr.DisablePlugin(targetID); err != nil {
			fmt.Fprintf(out, "Disable error: %v\n", err)
			return false, err
		}
		fmt.Fprintf(out, "✓ Disabled plugin %s\n", targetID)
		return true, nil

	case "remove", "rm", "uninstall":
		if len(args) < 2 {
			fmt.Fprintln(out, "Error: plugin ID required. Usage: halpradio plugin remove <plugin-id>")
			return false, fmt.Errorf("plugin ID required")
		}
		targetID := args[1]
		if err := mgr.UninstallPlugin(targetID); err != nil {
			fmt.Fprintf(out, "Uninstall error: %v\n", err)
			return false, err
		}
		fmt.Fprintf(out, "✓ Removed plugin %s\n", targetID)
		return true, nil

	case "update", "upgrade":
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		regIndex, err := mgr.RegistryClient().FetchRegistry(ctx)
		if err != nil {
			fmt.Fprintf(out, "Failed fetching registry: %v\n", err)
			return false, err
		}
		installed := mgr.GetPlugins()
		for _, inst := range installed {
			for _, reg := range regIndex.Plugins {
				if reg.ID == inst.Manifest.ID {
					fmt.Fprintf(out, "Updating %s to v%s...\n", reg.Name, reg.Version)
					_ = mgr.InstallFromRegistry(ctx, reg)
				}
			}
		}
		fmt.Fprintln(out, "✓ Plugins up to date!")
		return true, nil

	default:
		fmt.Fprintf(out, "Unknown plugin command %q. Run 'halpradio plugin --help'\n", cmd)
		return false, fmt.Errorf("unknown plugin command: %s", cmd)
	}
}

// RunRemote executes an IPC command against an active halpradio instance.
func RunRemote(args []string, out io.Writer) (bool, error) {
	if len(args) == 0 {
		fmt.Fprintln(out, "Usage: halpradio remote <command>")
		fmt.Fprintln(out, "Commands: play-pause, play, pause, stop, next, prev, volup, voldown, mute, random, status")
		return true, nil
	}

	actionStr := args[0]
	resp, err := desktop.SendIPCCommand("", actionStr)
	if err != nil {
		fmt.Fprintf(out, "Remote control error: %v\n", err)
		return false, err
	}

	if !resp.Success {
		fmt.Fprintf(out, "Error: %s\n", resp.Message)
		return false, fmt.Errorf("%s", resp.Message)
	}

	if resp.Status != nil {
		if resp.Status.Station != "" {
			track := resp.Status.Track
			if track == "" {
				track = resp.Status.Station
			}
			fmt.Fprintf(out, "[%s] %s - %s (vol: %d%%, backend: %s)\n", resp.Status.Status, resp.Status.Station, track, resp.Status.Volume, resp.Status.Backend)
		} else {
			fmt.Fprintf(out, "[%s] Volume: %d%%\n", resp.Status.Status, resp.Status.Volume)
		}
	} else {
		fmt.Fprintf(out, "%s\n", resp.Message)
	}
	return true, nil
}

// SetupApp parses CLI flags, loads configuration, and initializes the AppInstance.
func SetupApp(args []string, embeddedCatalog []byte, out io.Writer) (*AppInstance, bool, error) {
	if len(args) > 0 && args[0] == "remote" {
		_, err := RunRemote(args[1:], out)
		return nil, true, err
	}
	if len(args) > 0 && args[0] == "plugin" {
		_, err := RunPluginCLI(args[1:], out)
		return nil, true, err
	}

	fs := flag.NewFlagSet("halpradio", flag.ContinueOnError)
	fs.SetOutput(out)

	backendFlag := fs.String("backend", "auto", "Audio player backend: auto, native, mpv, vlc, ffplay, mplayer, mpg123")
	themeFlag := fs.String("theme", "", "Color theme: tokyonight, catppuccin, synthwave, nord, gruvbox, dracula")
	versionFlag := fs.Bool("version", false, "Show halpradio version")
	notificationsFlag := fs.Bool("notifications", true, "Enable desktop notifications on song change")
	mprisFlag := fs.Bool("mpris", true, "Enable Linux MPRIS v2 D-Bus remote interface")
	ipcFlag := fs.Bool("ipc", true, "Enable local IPC socket for CLI remote control")

	if err := fs.Parse(args); err != nil {
		return nil, false, err
	}

	if *versionFlag {
		fmt.Fprintf(out, "halpradio v%s - LazyVim-inspired Terminal Internet Radio Streamer\n", Version)
		return nil, true, nil
	}

	cfg, err := util.LoadConfig()
	if err != nil {
		cfg = util.DefaultConfig()
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
	if !*mprisFlag {
		cfg.MPRISEnabled = false
	}
	if !*ipcFlag {
		cfg.IPCEnabled = false
	}

	store := radio.NewStore()
	if err := store.Load(embeddedCatalog); err != nil {
		fmt.Fprintf(os.Stderr, "Warning loading station store: %v\n", err)
	}

	var program *tea.Program

	pm := player.NewManager(cfg.PlayerBackend, cfg.Volume, func(info player.TrackInfo) {
		if program != nil {
			program.Send(ui.TrackUpdatedMsg(info))
		}
	})

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
	_ = pluginMgr.Init()

	model := ui.NewModel(store, pm, cfg)
	model.SetPluginManager(pluginMgr)

	desktopMgr := desktop.NewManager(desktop.DesktopConfig{
		NotificationsEnabled: cfg.SongNotifications,
		MPRISEnabled:         cfg.MPRISEnabled,
		IPCEnabled:           cfg.IPCEnabled,
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

	model.SetDesktop(desktopMgr)

	program = tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	return &AppInstance{
		Program:   program,
		Player:    pm,
		Config:    cfg,
		Store:     store,
		Desktop:   desktopMgr,
		PluginMgr: pluginMgr,
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

	if _, err := appInst.Program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running halpradio: %v\n", err)
		os.Exit(1)
	}

	// Clean up player, desktop, and plugin services on exit
	_ = appInst.Player.Stop()
	if appInst.Desktop != nil {
		_ = appInst.Desktop.Close()
	}
	if appInst.PluginMgr != nil {
		_ = appInst.PluginMgr.Close()
	}
}
