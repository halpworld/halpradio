package app

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/halpworld/halpradio/pkg/player"
	"github.com/halpworld/halpradio/pkg/radio"
	"github.com/halpworld/halpradio/pkg/ui"
	"github.com/halpworld/halpradio/pkg/util"
)

var Version = "0.0.3"

func Run(embeddedCatalog []byte) {
	backendFlag := flag.String("backend", "auto", "Audio player backend: auto, native, mpv, vlc, ffplay, mplayer, mpg123")
	themeFlag := flag.String("theme", "", "Color theme: tokyonight, catppuccin, synthwave, nord, gruvbox, dracula")
	versionFlag := flag.Bool("version", false, "Show halpradio version")
	flag.Parse()

	if *versionFlag {
		fmt.Printf("halpradio v%s - LazyVim-inspired Terminal Internet Radio Streamer\n", Version)
		os.Exit(0)
	}

	cfg := util.DefaultConfig()
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

	if _, err := program.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running halpradio: %v\n", err)
		os.Exit(1)
	}

	// Clean up player on exit
	_ = pm.Stop()
}
