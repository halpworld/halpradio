package app

import (
	"flag"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
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
}

// SetupApp parses CLI flags, loads configuration, and initializes the AppInstance.
func SetupApp(args []string, embeddedCatalog []byte, out io.Writer) (*AppInstance, bool, error) {
	fs := flag.NewFlagSet("halpradio", flag.ContinueOnError)
	fs.SetOutput(out)

	backendFlag := fs.String("backend", "auto", "Audio player backend: auto, native, mpv, vlc, ffplay, mplayer, mpg123")
	themeFlag := fs.String("theme", "", "Color theme: tokyonight, catppuccin, synthwave, nord, gruvbox, dracula")
	versionFlag := fs.Bool("version", false, "Show halpradio version")

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
	}, false, nil
}

func Run(embeddedCatalog []byte) {
	appInst, isVersion, err := SetupApp(os.Args[1:], embeddedCatalog, os.Stdout)
	if err != nil {
		os.Exit(1)
	}
	if isVersion {
		os.Exit(0)
	}

	if _, err := appInst.Program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running halpradio: %v\n", err)
		os.Exit(1)
	}

	// Clean up player on exit
	_ = appInst.Player.Stop()
}
