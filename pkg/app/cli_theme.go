package app

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/halpworld/halpradio/pkg/theme"
	"github.com/halpworld/halpradio/pkg/util"
)

// PrintThemeHelp renders CLI help for `halpradio theme`.
func PrintThemeHelp(out io.Writer) {
	RenderHelpHeader(out, "halpradio theme", "Discover, preview, download, and manage color themes from repository")

	RenderHelpUsage(out,
		"halpradio theme <command> [arguments]",
	)

	RenderHelpItems(out, "Commands", []HelpItem{
		{Name: "list, ls", Description: "List installed and discoverable community themes from repo"},
		{Name: "install, add", Args: "<theme-id>", Description: "Download and install theme from community repository"},
		{Name: "preview", Args: "<theme-id>", Description: "Preview theme palette tokens and mockup in terminal"},
		{Name: "info", Args: "<theme-id>", Description: "Display detailed token information for a theme"},
		{Name: "remove, rm", Args: "<theme-id>", Description: "Uninstall custom theme from local config"},
		{Name: "export", Args: "[name]", Description: "Export active theme as a documented YAML starter file"},
	})

	RenderHelpExamples(out, "Examples", []HelpExample{
		{Command: "halpradio theme list", Description: "Browse installed and online community themes"},
		{Command: "halpradio theme preview rose-pine", Description: "Preview Rosé Pine palette before installing"},
		{Command: "halpradio theme install rose-pine", Description: "Download & install Rosé Pine from themes repo"},
		{Command: "halpradio theme preview everforest", Description: "Inspect Everforest Dark theme tokens"},
		{Command: "halpradio theme export my-custom", Description: "Export starter template to ~/.config/halpradio/themes/"},
		{Command: "halpradio theme remove custom-test", Description: "Delete local custom theme"},
	})

	RenderHelpFooter(out,
		`In TUI mode, press 't' then [Tab] to browse, preview, and install themes interactively.`,
		`Explore the official repo at https://github.com/halpworld/halpradio-themes`,
	)
}

// RunThemeCLI handles theme subcommands: list, install, preview, info, remove, export.
func RunThemeCLI(args []string, out io.Writer) (bool, error) {
	if len(args) == 0 || IsHelpArg(args[0]) {
		PrintThemeHelp(out)
		return true, nil
	}

	cfg, _ := util.LoadConfig()
	themesDir := util.GetThemesDir()
	_ = util.EnsureConfigDir()
	_ = theme.EnsureExampleTheme(themesDir)
	_, _ = theme.LoadCustomThemes(themesDir)

	client := theme.NewRegistryClient(cfg.ThemeRegistryURL)

	cmd := strings.ToLower(args[0])
	switch cmd {
	case "list", "ls":
		installed := theme.GetAllThemes()
		fmt.Fprintln(out, "🎨 INSTALLED THEMES:")
		for i, t := range installed {
			customTag := ""
			if t.IsCustom {
				customTag = " (Custom)"
			}
			swatch := lipgloss.NewStyle().
				Background(t.Primary).
				Foreground(t.BadgeText).
				Bold(true).
				Render(fmt.Sprintf(" %-18s ", t.Name))

			fmt.Fprintf(out, "  %2d. %s %-16s %s\n", i+1, swatch, customTag, t.Description)
		}

		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "🌐 COMMUNITY REPOSITORY HUB (halpradio-themes):")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		regIndex, err := client.FetchRegistry(ctx)
		if err != nil {
			fmt.Fprintf(out, "  (Registry fetch note: %v)\n", err)
		}

		for _, rt := range regIndex.Themes {
			isInst := false
			for _, it := range installed {
				if it.ID == rt.ID || strings.EqualFold(it.Name, rt.Name) {
					isInst = true
					break
				}
			}
			tag := "[Available]"
			if isInst {
				tag = "[Installed]"
			}
			swatch := lipgloss.NewStyle().
				Background(rt.Primary).
				Foreground(rt.BadgeText).
				Bold(true).
				Render(fmt.Sprintf(" %-18s ", truncate(rt.Name, 18)))

			fmt.Fprintf(out, "  • %-20s %s %-12s by %-16s (%s)\n", rt.ID, swatch, tag, rt.Author, rt.Category)
		}
		return true, nil

	case "install", "add", "download":
		if len(args) < 2 || IsHelpArg(args[1]) {
			fmt.Fprintln(out, "Error: theme ID required. Usage: halpradio theme install <theme-id>")
			return false, fmt.Errorf("theme ID required")
		}
		targetID := strings.ToLower(strings.TrimSpace(args[1]))

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		regIndex, err := client.FetchRegistry(ctx)
		if regIndex == nil || len(regIndex.Themes) == 0 {
			fmt.Fprintf(out, "Failed to connect to themes repository: %v\n", err)
			return false, err
		}

		var targetTheme *theme.RegistryTheme
		for _, t := range regIndex.Themes {
			if strings.EqualFold(t.ID, targetID) || strings.EqualFold(t.Name, targetID) {
				targetTheme = &t
				break
			}
		}

		if targetTheme == nil {
			fmt.Fprintf(out, "Theme %q not found in official repository.\n", targetID)
			return false, fmt.Errorf("theme not found: %s", targetID)
		}

		fmt.Fprintf(out, "Downloading and installing %s by %s...\n", targetTheme.Name, targetTheme.Author)
		if err := client.DownloadAndInstall(ctx, *targetTheme, themesDir); err != nil {
			fmt.Fprintf(out, "Install error: %v\n", err)
			return false, err
		}

		destPath := filepath.Join(themesDir, targetTheme.ID+".yaml")
		fmt.Fprintf(out, "✓ Successfully installed %s into %s!\n", targetTheme.Name, destPath)
		fmt.Fprintf(out, "  To use immediately, run: halpradio -theme %s\n", targetTheme.ID)
		return true, nil

	case "preview":
		if len(args) < 2 || IsHelpArg(args[1]) {
			fmt.Fprintln(out, "Error: theme ID required. Usage: halpradio theme preview <theme-id>")
			return false, fmt.Errorf("theme ID required")
		}
		targetID := strings.ToLower(strings.TrimSpace(args[1]))

		// Look in local or remote
		var targetTh theme.Theme
		var author string
		var category string

		foundLocal := false
		for _, t := range theme.GetAllThemes() {
			if strings.EqualFold(t.ID, targetID) || strings.EqualFold(t.Name, targetID) {
				targetTh = t
				author = t.Author
				foundLocal = true
				break
			}
		}

		if !foundLocal {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			regIndex, _ := client.FetchRegistry(ctx)
			if regIndex != nil {
				for _, rt := range regIndex.Themes {
					if strings.EqualFold(rt.ID, targetID) || strings.EqualFold(rt.Name, targetID) {
						targetTh = rt.ToTheme()
						author = rt.Author
						category = rt.Category
						break
					}
				}
			}
		}

		if targetTh.Name == "" {
			fmt.Fprintf(out, "Theme %q not found in local catalog or official repository.\n", targetID)
			return false, fmt.Errorf("theme not found: %s", targetID)
		}

		RenderTerminalThemePreview(out, targetTh, author, category)
		return true, nil

	case "info":
		if len(args) < 2 || IsHelpArg(args[1]) {
			fmt.Fprintln(out, "Error: theme ID required. Usage: halpradio theme info <theme-id>")
			return false, fmt.Errorf("theme ID required")
		}
		targetID := strings.ToLower(strings.TrimSpace(args[1]))
		th := theme.GetTheme(targetID)
		if th.Name == "" || (th.ID == "tokyonight" && targetID != "tokyonight") {
			// Check remote
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			regIndex, _ := client.FetchRegistry(ctx)
			if regIndex != nil {
				for _, rt := range regIndex.Themes {
					if strings.EqualFold(rt.ID, targetID) || strings.EqualFold(rt.Name, targetID) {
						th = rt.ToTheme()
						break
					}
				}
			}
		}

		fmt.Fprintf(out, "🎨 THEME INFO: %s\n", th.Name)
		fmt.Fprintf(out, "  • ID:          %s\n", th.ID)
		fmt.Fprintf(out, "  • Author:      %s\n", th.Author)
		fmt.Fprintf(out, "  • Description: %s\n", th.Description)
		fmt.Fprintf(out, "  • Primary:     %s\n", string(th.Primary))
		fmt.Fprintf(out, "  • Secondary:   %s\n", string(th.Secondary))
		fmt.Fprintf(out, "  • Background:  %s\n", string(th.Background))
		fmt.Fprintf(out, "  • Foreground:  %s\n", string(th.Foreground))
		fmt.Fprintf(out, "  • Highlight:   %s\n", string(th.Highlight))
		fmt.Fprintf(out, "  • Playing:     %s\n", string(th.Playing))
		fmt.Fprintf(out, "  • Favorite:    %s\n", string(th.Favorite))
		fmt.Fprintf(out, "  • Border:      %s\n", string(th.Border))
		fmt.Fprintf(out, "  • Badge:       %s (text: %s)\n", string(th.Badge), string(th.BadgeText))
		fmt.Fprintf(out, "  • HeaderAscii: %s\n", string(th.HeaderAscii))
		return true, nil

	case "remove", "rm", "uninstall", "delete":
		if len(args) < 2 || IsHelpArg(args[1]) {
			fmt.Fprintln(out, "Error: theme ID required. Usage: halpradio theme remove <theme-id>")
			return false, fmt.Errorf("theme ID required")
		}
		targetID := strings.ToLower(strings.TrimSpace(args[1]))
		if err := client.Uninstall(targetID, themesDir); err != nil {
			fmt.Fprintf(out, "Remove error: %v\n", err)
			return false, err
		}
		fmt.Fprintf(out, "✓ Successfully removed custom theme %s\n", targetID)
		return true, nil

	case "export":
		name := "exported-theme"
		if len(args) > 1 && !IsHelpArg(args[1]) {
			name = args[1]
		}
		activeTh := theme.GetTheme(cfg.Theme)
		activeTh.Name = name
		activeTh.ID = name
		targetPath, err := theme.ExportActiveTheme(themesDir, activeTh)
		if err != nil {
			fmt.Fprintf(out, "Export error: %v\n", err)
			return false, err
		}
		fmt.Fprintf(out, "✓ Exported theme to %s\n", targetPath)
		return true, nil

	default:
		sugg := SuggestCommand(cmd, []string{"list", "install", "preview", "info", "remove", "export"})
		if sugg != "" {
			fmt.Fprintf(out, "Unknown theme command %q. Did you mean %q? Run 'halpradio theme --help'\n", cmd, sugg)
		} else {
			fmt.Fprintf(out, "Unknown theme command %q. Run 'halpradio theme --help'\n", cmd)
		}
		return false, fmt.Errorf("unknown theme command: %s", cmd)
	}
}

// RenderTerminalThemePreview displays a stylized ANSI palette swatch and mini player mockup in the terminal.
func RenderTerminalThemePreview(out io.Writer, targetTh theme.Theme, author, category string) {
	fmt.Fprintf(out, "\n")
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(targetTh.Primary).
		Render(fmt.Sprintf("🎨 THEME PREVIEW: %s", targetTh.Name))
	if author != "" {
		header += lipgloss.NewStyle().Foreground(targetTh.Secondary).Render(" by " + author)
	}
	if category != "" {
		header += lipgloss.NewStyle().Foreground(targetTh.Muted).Render(" [" + category + "]")
	}
	fmt.Fprintln(out, header)
	if targetTh.Description != "" {
		fmt.Fprintln(out, lipgloss.NewStyle().Foreground(targetTh.Foreground).Italic(true).Render("  "+targetTh.Description))
	}
	fmt.Fprintln(out, "")

	// Swatch chips
	chip := func(bg, fg lipgloss.Color, name string) string {
		return lipgloss.NewStyle().
			Background(bg).
			Foreground(fg).
			Bold(true).
			Padding(0, 1).
			Render(name)
	}

	swatches := lipgloss.JoinHorizontal(
		lipgloss.Center,
		"  ",
		chip(targetTh.Primary, targetTh.BadgeText, "PRIMARY"),
		" ",
		chip(targetTh.Secondary, targetTh.Background, "SECONDARY"),
		" ",
		chip(targetTh.Highlight, targetTh.Background, "HIGHLIGHT"),
		" ",
		chip(targetTh.Playing, targetTh.Background, "PLAYING"),
		" ",
		chip(targetTh.Badge, targetTh.BadgeText, "BADGE"),
		" ",
		chip(targetTh.Favorite, targetTh.Background, "FAVORITE"),
		" ",
		chip(targetTh.Border, targetTh.Foreground, "BORDER"),
	)
	fmt.Fprintln(out, swatches)
	fmt.Fprintln(out, "")

	// Mini mockup card
	miniHeader := lipgloss.JoinHorizontal(
		lipgloss.Center,
		lipgloss.NewStyle().Foreground(targetTh.HeaderAscii).Bold(true).Render("📻 HALPRADIO"),
		" ",
		lipgloss.NewStyle().Background(targetTh.Badge).Foreground(targetTh.BadgeText).Bold(true).Padding(0, 1).Render("LOFI"),
		" ",
		lipgloss.NewStyle().Foreground(targetTh.Secondary).Render("● LIVE 128kbps"),
	)

	miniPlayer := lipgloss.JoinHorizontal(
		lipgloss.Center,
		lipgloss.NewStyle().Foreground(targetTh.Playing).Bold(true).Render("▶ "),
		lipgloss.NewStyle().Foreground(targetTh.Foreground).Bold(true).Render("Tycho - A Walk"),
		"  ",
		lipgloss.NewStyle().Foreground(targetTh.Playing).Render("♫ ▂▃▅▆▇█"),
	)

	miniStation := lipgloss.NewStyle().
		Background(targetTh.Border).
		Foreground(targetTh.Highlight).
		Bold(true).
		Padding(0, 1).
		Render("❯ ★ 1. SomaFM Groove Salad")

	mockupBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(targetTh.Primary).
		Background(targetTh.Background).
		Padding(1, 2).
		Width(64).
		Render(lipgloss.JoinVertical(
			lipgloss.Left,
			miniHeader,
			"",
			miniPlayer,
			miniStation,
		))

	fmt.Fprintln(out, mockupBox)
	fmt.Fprintln(out, "")
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}
