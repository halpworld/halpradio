package app

import (
	"flag"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/desktop"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/ui"
	"github.com/halpworld/halpradio/pkg/util"
)

var Version = "0.0.4"

type AppInstance struct {
	Program *tea.Program
	Player  *player.Manager
	Config  util.Config
	Store   *radio.Store
	Desktop *desktop.Manager
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

	model := ui.NewModel(store, pm, cfg)

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
		Program: program,
		Player:  pm,
		Config:  cfg,
		Store:   store,
		Desktop: desktopMgr,
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

	// Clean up player and desktop services on exit
	_ = appInst.Player.Stop()
	if appInst.Desktop != nil {
		_ = appInst.Desktop.Close()
	}
}
