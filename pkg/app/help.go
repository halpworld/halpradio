package app

import (
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// HelpItem defines a single entry in a command or flag list.
type HelpItem struct {
	Short       string
	Name        string
	Args        string
	Description string
	Default     string
}

// HelpExample defines a single CLI command example with description.
type HelpExample struct {
	Command     string
	Description string
}

var (
	styleTitle       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7aa2f7"))
	styleSubtitle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#a9b1d6"))
	styleSection     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#bb9af7"))
	styleCommand     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7dcfff"))
	styleFlagShort   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#2ac3de"))
	styleFlagLong    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7dcfff"))
	styleArgs        = lipgloss.NewStyle().Foreground(lipgloss.Color("#e0af68"))
	styleDesc        = lipgloss.NewStyle().Foreground(lipgloss.Color("#c0caf5"))
	styleDefault     = lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))
	styleExampleCmd  = lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7"))
	styleExampleDesc = lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))
	styleDim         = lipgloss.NewStyle().Foreground(lipgloss.Color("#565f89"))
	styleError       = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#f7768e"))
	styleSuggestion  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#9ece6a"))
)

// IsHelpArg returns true if the given string represents a help invocation.
func IsHelpArg(arg string) bool {
	switch strings.ToLower(strings.TrimSpace(arg)) {
	case "help", "--help", "-h", "-help":
		return true
	default:
		return false
	}
}

// HasHelpFlag scans an arguments slice for help flags or commands.
func HasHelpFlag(args []string) bool {
	for _, a := range args {
		if IsHelpArg(a) {
			return true
		}
	}
	return false
}

// RenderHelpHeader prints a stylized title and description.
func RenderHelpHeader(out io.Writer, title, desc string) {
	fmt.Fprintf(out, "%s - %s\n\n", styleTitle.Render(title), styleSubtitle.Render(desc))
}

// RenderHelpUsage prints the Usage section with given patterns.
func RenderHelpUsage(out io.Writer, patterns ...string) {
	fmt.Fprintln(out, styleSection.Render("Usage:"))
	for _, p := range patterns {
		fmt.Fprintf(out, "  %s\n", p)
	}
	fmt.Fprintln(out, "")
}

// RenderHelpItems prints an aligned section of commands, flags, or arguments.
func RenderHelpItems(out io.Writer, sectionTitle string, items []HelpItem) {
	if len(items) == 0 {
		return
	}

	fmt.Fprintln(out, styleSection.Render(sectionTitle+":"))

	// Determine max width for left column
	maxLeftWidth := 0
	leftStrings := make([]string, len(items))
	for i, item := range items {
		left := formatItemLeft(item)
		leftStrings[i] = left
		if lipgloss.Width(left) > maxLeftWidth {
			maxLeftWidth = lipgloss.Width(left)
		}
	}

	if maxLeftWidth < 28 {
		maxLeftWidth = 28
	}

	for i, item := range items {
		left := leftStrings[i]
		padding := maxLeftWidth - lipgloss.Width(left) + 2
		if padding < 2 {
			padding = 2
		}

		desc := styleDesc.Render(item.Description)
		if item.Default != "" {
			desc += " " + styleDefault.Render(fmt.Sprintf("(default: %s)", item.Default))
		}

		fmt.Fprintf(out, "  %s%s%s\n", left, strings.Repeat(" ", padding), desc)
	}
	fmt.Fprintln(out, "")
}

func formatItemLeft(item HelpItem) string {
	var b strings.Builder

	if item.Short != "" && item.Name != "" {
		b.WriteString(styleFlagShort.Render(item.Short))
		b.WriteString(styleDim.Render(", "))
		b.WriteString(styleFlagLong.Render(item.Name))
	} else if item.Short != "" {
		b.WriteString(styleFlagShort.Render(item.Short))
	} else if item.Name != "" {
		if strings.HasPrefix(item.Name, "--") || strings.HasPrefix(item.Name, "-") {
			b.WriteString("    ")
			b.WriteString(styleFlagLong.Render(item.Name))
		} else {
			b.WriteString(styleCommand.Render(item.Name))
		}
	}

	if item.Args != "" {
		b.WriteString(" ")
		b.WriteString(styleArgs.Render(item.Args))
	}

	return b.String()
}

// RenderHelpExamples prints formatted example workflows.
func RenderHelpExamples(out io.Writer, sectionTitle string, examples []HelpExample) {
	if len(examples) == 0 {
		return
	}

	title := "Examples:"
	if sectionTitle != "" {
		title = sectionTitle + ":"
	}
	fmt.Fprintln(out, styleSection.Render(title))

	maxCmdWidth := 0
	for _, ex := range examples {
		if len(ex.Command) > maxCmdWidth {
			maxCmdWidth = len(ex.Command)
		}
	}
	if maxCmdWidth < 46 {
		maxCmdWidth = 46
	}

	for _, ex := range examples {
		padding := maxCmdWidth - len(ex.Command) + 2
		if padding < 2 {
			padding = 2
		}
		if ex.Description != "" {
			fmt.Fprintf(out, "  %s%s%s\n",
				styleExampleCmd.Render(ex.Command),
				strings.Repeat(" ", padding),
				styleExampleDesc.Render("# "+ex.Description),
			)
		} else {
			fmt.Fprintf(out, "  %s\n", styleExampleCmd.Render(ex.Command))
		}
	}
	fmt.Fprintln(out, "")
}

// RenderHelpFooter prints footer / learn more hints.
func RenderHelpFooter(out io.Writer, lines ...string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintln(out, styleSection.Render("Learn More:"))
	for _, l := range lines {
		fmt.Fprintf(out, "  %s\n", styleSubtitle.Render(l))
	}
}

// PrintRootHelp renders the top-level halpradio CLI help screen.
func PrintRootHelp(out io.Writer) {
	RenderHelpHeader(out, "halpradio", "Terminal Internet Radio Player & Streamer")

	RenderHelpUsage(out,
		"halpradio [flags]                       "+styleDim.Render("Launch interactive Bubble Tea TUI"),
		"halpradio <command> [arguments] [flags] "+styleDim.Render("Execute standalone CLI command"),
	)

	RenderHelpItems(out, "Core Commands", []HelpItem{
		{Name: "play", Args: "<target> [flags]", Description: "Stream radio directly without TUI (index, ID, name, URL, or random)"},
		{Name: "party", Args: "<create|join|status|...>", Description: "P2P mesh synchronized radio rooms & live ASCII reactions (E2EE)"},
		{Name: "stations", Args: "[list|search|fav]", Description: "Discover, search, filter, and manage station catalog"},
		{Name: "volume", Args: "[value] [flags]", Description: "Query or adjust volume (e.g. halpradio volume +5, 60, mute)"},
		{Name: "current", Args: "[flags]", Description: "Query currently playing track for status bars (tmux, Waybar)"},
		{Name: "status", Args: "[flags]", Description: "Get full playback status snapshot (JSON or formatted template)"},
	})

	RenderHelpItems(out, "Playback & Remote Controls", []HelpItem{
		{Name: "toggle", Description: "Toggle play/pause on active instance"},
		{Name: "pause / stop", Description: "Pause or stop playback"},
		{Name: "next / prev", Description: "Play next or previous station in catalog"},
		{Name: "mute", Description: "Toggle audio mute on active instance"},
		{Name: "random", Description: "Play a random station"},
		{Name: "remote", Args: "<action>", Description: "Send custom IPC action to running instance"},
	})

	RenderHelpItems(out, "Extensions & Catalog", []HelpItem{
		{Name: "theme", Args: "<list|install|preview|...>", Description: "Discover, preview, download, and manage community themes"},
		{Name: "plugin", Args: "<list|install|...>", Description: "Manage sandboxed Wasm plugins"},
		{Name: "update-stations", Description: "Update station catalog from remote repository"},
		{Name: "version", Description: "Show halpradio version"},
	})

	RenderHelpItems(out, "Interactive TUI Flags", []HelpItem{
		{Short: "-b", Name: "--backend", Args: "<engine>", Description: "Audio backend (auto, native, mpv, vlc, ffplay, mplayer, mpg123)", Default: `"auto"`},
		{Short: "-t", Name: "--theme", Args: "<name>", Description: "Color theme (tokyonight, catppuccin, synthwave, nord, gruvbox, dracula)"},
		{Name: "--notifications", Args: "=<bool>", Description: "Enable song change desktop notifications", Default: "true"},
		{Name: "--autopause", Args: "=<bool>", Description: "Pause playback when headphones disconnect", Default: "true"},
		{Name: "--discord", Args: "=<bool>", Description: "Enable Discord Rich Presence (RPC)", Default: "true"},
		{Name: "--mpris", Args: "=<bool>", Description: "Enable Linux MPRIS v2 D-Bus remote interface", Default: "true"},
		{Name: "--ipc", Args: "=<bool>", Description: "Enable local IPC socket for CLI remote control", Default: "true"},
		{Name: "--fingerprint", Args: "=<bool>", Description: "Enable acoustic stream fingerprinting (AcoustID)", Default: "true"},
		{Name: "--auto-identify", Args: "=<bool>", Description: "Automatically identify songs when station lacks metadata", Default: "true"},
		{Name: "--experimental-tuner", Description: "Enable experimental analog frequency tuner (on hold)", Default: "false"},
		{Name: "--debug", Description: "Write a diagnostic log to attach to bug reports", Default: "false"},
		{Name: "--debug-log", Args: "<path>", Description: "Diagnostic log path (default ~/.config/halpradio/debug.log)"},
		{Short: "-v", Name: "--version", Description: "Show halpradio version"},
		{Short: "-h", Name: "--help", Description: "Show halpradio help"},
	})

	RenderHelpExamples(out, "Top Automation Examples", []HelpExample{
		{Command: "halpradio", Description: "Launch interactive Bubble Tea TUI"},
		{Command: "halpradio party create \"team-focus\"", Description: "Create encrypted P2P radio room with teammates"},
		{Command: "halpradio party join 8X2K9P", Description: "Join synchronized radio room using 6-char code"},
		{Command: "halpradio play somafm_groovesalad", Description: "Headless stream SomaFM Groove Salad"},
		{Command: "halpradio play 2 --volume 30", Description: "Headless stream station #2 at 30% volume"},
		{Command: "halpradio play somafm_groovesalad -d 45m", Description: "45-minute focus session with auto-stop"},
		{Command: "halpradio stations list --genre ambient", Description: "Browse ambient stations"},
		{Command: "halpradio stations search \"lofi\" --online", Description: "Search 40,000+ community stations"},
		{Command: "halpradio stations list --plain | fzf | awk '{print $2}' | xargs halpradio play", Description: "Interactive fuzzy finder stream"},
		{Command: "halpradio current --format \"[%p] %s - %t\"", Description: "Status bar ticker for tmux or Waybar"},
		{Command: "halpradio volume +5", Description: "Increase playback volume by 5%"},
		{Command: "halpradio --debug", Description: "Reproduce a bug, then attach ~/.config/halpradio/debug.log"},
	})

	RenderHelpFooter(out,
		`Use "halpradio help <command>" or "halpradio <command> --help" for detailed command help.`,
		`Use "halpradio stations <subcommand> --help" for station catalog subcommand usage.`,
		`Use "halpradio plugin --help" for plugin manager documentation.`,
	)
}

// PrintPlayHelp renders help for `halpradio play`.
func PrintPlayHelp(out io.Writer) {
	RenderHelpHeader(out, "halpradio play", "Stream internet radio directly from the CLI without TUI")

	RenderHelpUsage(out,
		"halpradio play [station|index|url|random] [flags]",
	)

	RenderHelpItems(out, "Arguments", []HelpItem{
		{Name: "<number>", Description: "Play station by 1-based catalog index (e.g. halpradio play 1)"},
		{Name: "<station-id>", Description: "Play station by exact ID (e.g. halpradio play somafm_groovesalad)"},
		{Name: "<name>", Description: "Play station by fuzzy name (e.g. halpradio play \"lofi girl\")"},
		{Name: "<url>", Description: "Play direct HTTP/HTTPS audio stream URL"},
		{Name: "random", Description: "Play a random station (e.g. halpradio play random)"},
	})

	RenderHelpItems(out, "Flags", []HelpItem{
		{Short: "-v", Name: "--volume", Args: "<0-100>", Description: "Playback volume level", Default: "config or 80"},
		{Short: "-b", Name: "--backend", Args: "<engine>", Description: "Audio backend: auto, native, mpv, vlc, ffplay, mplayer, mpg123"},
		{Short: "-d", Name: "--duration", Args: "<time>", Description: "Sleep/focus timer duration (e.g. 45m, 1h, 30s)"},
		{Short: "-g", Name: "--genre", Args: "<genre>", Description: "Filter random station by genre (e.g. jazz, ambient, lofi)"},
		{Short: "-r", Name: "--random", Description: "Pick a random station from catalog"},
		{Short: "-j", Name: "--json", Description: "Output track changes and playback events in JSON"},
		{Name: "--remote", Description: "Forward play command to active halpradio instance over IPC"},
		{Short: "-h", Name: "--help", Description: "Show play command help"},
	})

	RenderHelpExamples(out, "Examples", []HelpExample{
		{Command: "halpradio play 2 --volume 30", Description: "Stream station #2 at 30% volume"},
		{Command: "halpradio play somafm_groovesalad -d 45m", Description: "45-minute focus session with auto-stop"},
		{Command: "halpradio play \"BBC Radio 1\"", Description: "Stream by fuzzy station name"},
		{Command: "halpradio play random --genre synthwave", Description: "Stream random synthwave station"},
		{Command: "halpradio play https://ice1.somafm.com/groovesalad-128-mp3", Description: "Stream direct audio URL"},
		{Command: "halpradio play --remote", Description: "Resume playback on running TUI instance"},
	})
}

// PrintStationsHelp renders help for `halpradio stations`.
func PrintStationsHelp(out io.Writer) {
	RenderHelpHeader(out, "halpradio stations", "Discover, search, filter, and manage radio station catalog")

	RenderHelpUsage(out,
		"halpradio stations <subcommand> [flags]",
		"halpradio stations [flags]               "+styleDim.Render("(defaults to 'list')"),
	)

	RenderHelpItems(out, "Subcommands", []HelpItem{
		{Name: "list", Args: "[flags]", Description: "List stations with optional filters (genre, country, tag)"},
		{Name: "search", Args: "<query> [flags]", Description: "Search stations locally or query 40,000+ RadioBrowser community stations"},
		{Name: "fav", Args: "[list|add|rm|toggle]", Description: "Manage favorite stations"},
		{Name: "add", Args: "[flags]", Description: "Add a custom radio station to local catalog"},
	})

	RenderHelpItems(out, "List & Search Flags", []HelpItem{
		{Short: "-g", Name: "--genre", Args: "<genre>", Description: "Filter stations by genre (e.g. ambient, lofi, jazz)"},
		{Short: "-c", Name: "--country", Args: "<country>", Description: "Filter by 2-letter ISO country code or name (e.g. US, GB, JP)"},
		{Short: "-t", Name: "--tag", Args: "<tag>", Description: "Filter by activity tag (e.g. coding, study, relax)"},
		{Short: "-f", Name: "--favorites", Description: "Show only favorited stations"},
		{Short: "-n", Name: "--limit", Args: "<n>", Description: "Limit number of stations displayed (default: all for list, 25 for search)"},
		{Short: "-q", Name: "--plain", Description: "Output plain tab-separated TSV for fzf / rofi / awk"},
		{Short: "-j", Name: "--json", Description: "Output structured JSON array for jq / scripting"},
		{Short: "-o", Name: "--online", Description: "(search only) Query 40,000+ RadioBrowser community stations"},
		{Short: "-h", Name: "--help", Description: "Show stations help"},
	})

	RenderHelpExamples(out, "Examples", []HelpExample{
		{Command: "halpradio stations list --genre ambient --limit 10", Description: "List top 10 ambient stations"},
		{Command: "halpradio stations search \"lofi\" --online", Description: "Search 40,000+ community stations online"},
		{Command: "halpradio stations list --plain | fzf | awk '{print $2}' | xargs halpradio play", Description: "Interactive fuzzy finder selector"},
		{Command: "halpradio stations fav add somafm_groovesalad", Description: "Add station to favorites"},
		{Command: "halpradio stations add --name \"My Radio\" --url \"https://stream.example/live.mp3\"", Description: "Add custom stream"},
	})

	RenderHelpFooter(out,
		`Use "halpradio stations list --help" for list options.`,
		`Use "halpradio stations search --help" for search options.`,
		`Use "halpradio stations fav --help" for favorites management.`,
		`Use "halpradio stations add --help" for adding custom stations.`,
	)
}

// PrintStationsListHelp renders help for `halpradio stations list`.
func PrintStationsListHelp(out io.Writer) {
	RenderHelpHeader(out, "halpradio stations list", "List and filter stations in the local catalog")

	RenderHelpUsage(out,
		"halpradio stations list [flags]",
	)

	RenderHelpItems(out, "Flags", []HelpItem{
		{Short: "-g", Name: "--genre", Args: "<genre>", Description: "Filter stations by genre (e.g. ambient, synthwave, jazz)"},
		{Short: "-c", Name: "--country", Args: "<country>", Description: "Filter stations by country code or name (e.g. US, GB, JP)"},
		{Short: "-t", Name: "--tag", Args: "<activity>", Description: "Filter stations by tag/activity (e.g. coding, study, relax)"},
		{Short: "-f", Name: "--favorites", Description: "Show only favorited stations"},
		{Short: "-n", Name: "--limit", Args: "<n>", Description: "Limit number of stations displayed (0 for all)"},
		{Short: "-q", Name: "--plain", Description: "Output plain tab-separated format for fzf / shell scripting"},
		{Short: "-j", Name: "--json", Description: "Output full JSON array"},
		{Short: "-h", Name: "--help", Description: "Show list help"},
	})

	RenderHelpExamples(out, "Examples", []HelpExample{
		{Command: "halpradio stations list", Description: "List all catalog stations"},
		{Command: "halpradio stations list --genre ambient", Description: "List ambient stations"},
		{Command: "halpradio stations list --favorites", Description: "List favorite stations"},
		{Command: "halpradio stations list --plain | fzf", Description: "Pipe stations to fzf"},
	})
}

// PrintStationsSearchHelp renders help for `halpradio stations search`.
func PrintStationsSearchHelp(out io.Writer) {
	RenderHelpHeader(out, "halpradio stations search", "Search local catalog or 40,000+ RadioBrowser community stations")

	RenderHelpUsage(out,
		"halpradio stations search <query> [flags]",
		"halpradio search <query> [flags]        "+styleDim.Render("(alias)"),
	)

	RenderHelpItems(out, "Flags", []HelpItem{
		{Short: "-o", Name: "--online", Description: "Query 40,000+ RadioBrowser community stations"},
		{Short: "-g", Name: "--genre", Args: "<genre>", Description: "Filter results by genre"},
		{Short: "-c", Name: "--country", Args: "<country>", Description: "Filter results by country code or name"},
		{Short: "-t", Name: "--tag", Args: "<tag>", Description: "Filter results by tag"},
		{Short: "-n", Name: "--limit", Args: "<n>", Description: "Maximum number of results (default: 25)"},
		{Short: "-q", Name: "--plain", Description: "Output plain tab-separated format for fzf"},
		{Short: "-j", Name: "--json", Description: "Output JSON array"},
		{Short: "-h", Name: "--help", Description: "Show search help"},
	})

	RenderHelpExamples(out, "Examples", []HelpExample{
		{Command: "halpradio stations search \"chillout\"", Description: "Search local catalog"},
		{Command: "halpradio stations search \"BBC\" --online", Description: "Search online RadioBrowser for BBC stations"},
		{Command: "halpradio stations search \"lofi\" -o -n 10", Description: "Find top 10 online lofi stations"},
	})
}

// PrintStationsFavHelp renders help for `halpradio stations fav`.
func PrintStationsFavHelp(out io.Writer) {
	RenderHelpHeader(out, "halpradio stations fav", "Manage favorite radio stations")

	RenderHelpUsage(out,
		"halpradio stations fav [list]           "+styleDim.Render("List all favorite stations"),
		"halpradio stations fav add <id>         "+styleDim.Render("Add station to favorites"),
		"halpradio stations fav remove <id>      "+styleDim.Render("Remove station from favorites"),
		"halpradio stations fav toggle <id>      "+styleDim.Render("Toggle favorite status"),
	)

	RenderHelpExamples(out, "Examples", []HelpExample{
		{Command: "halpradio stations fav", Description: "List favorite stations"},
		{Command: "halpradio stations fav add somafm_groovesalad", Description: "Favorite Groove Salad"},
		{Command: "halpradio stations fav remove lofigirl", Description: "Unfavorite Lofi Girl"},
		{Command: "halpradio stations fav toggle somafm_groovesalad", Description: "Toggle favorite"},
	})
}

// PrintStationsAddHelp renders help for `halpradio stations add`.
func PrintStationsAddHelp(out io.Writer) {
	RenderHelpHeader(out, "halpradio stations add", "Add a custom radio station to local catalog")

	RenderHelpUsage(out,
		"halpradio stations add --name <name> --url <url> [flags]",
	)

	RenderHelpItems(out, "Flags", []HelpItem{
		{Name: "--name", Args: "<name>", Description: "Station name (required)"},
		{Name: "--url", Args: "<url>", Description: "Audio stream URL http/https (required)"},
		{Name: "--genre", Args: "<genre>", Description: "Genre label", Default: `"Various"`},
		{Name: "--country", Args: "<code>", Description: "2-letter ISO country code (e.g. US, GB, JP)", Default: `"US"`},
		{Name: "--id", Args: "<id>", Description: "Unique station identifier (autogenerated if empty)"},
		{Name: "--codec", Args: "<codec>", Description: "Audio codec: MP3, AAC, OGG", Default: `"MP3"`},
		{Name: "--bitrate", Args: "<kbps>", Description: "Audio bitrate in kbps", Default: "128"},
		{Short: "-h", Name: "--help", Description: "Show add station help"},
	})

	RenderHelpExamples(out, "Examples", []HelpExample{
		{
			Command:     "halpradio stations add --name \"My Synthwave Station\" --url \"https://stream.synthwave.example/live.mp3\" --genre \"Synthwave\"",
			Description: "Add custom stream with genre",
		},
	})
}

// PrintVolumeHelp renders help for `halpradio volume`.
func PrintVolumeHelp(out io.Writer) {
	RenderHelpHeader(out, "halpradio volume", "Query or adjust audio playback volume")

	RenderHelpUsage(out,
		"halpradio volume [value] [flags]",
		"halpradio vol [value] [flags]           "+styleDim.Render("(alias)"),
	)

	RenderHelpItems(out, "Arguments", []HelpItem{
		{Name: "(none)", Description: "Query current volume level"},
		{Name: "<0-100>", Description: "Set absolute volume level (e.g. halpradio volume 60)"},
		{Name: "+<N>", Description: "Increase volume by N% (e.g. halpradio volume +5)"},
		{Name: "-<N>", Description: "Decrease volume by N% (e.g. halpradio volume -10)"},
		{Name: "mute", Description: "Toggle mute on running instance"},
	})

	RenderHelpItems(out, "Flags", []HelpItem{
		{Short: "-j", Name: "--json", Description: "Output volume state in JSON format"},
		{Short: "-h", Name: "--help", Description: "Show volume help"},
	})

	RenderHelpExamples(out, "Examples", []HelpExample{
		{Command: "halpradio volume", Description: "Query current volume"},
		{Command: "halpradio volume 75", Description: "Set volume to 75%"},
		{Command: "halpradio volume +5", Description: "Increase volume by 5%"},
		{Command: "halpradio volume -10", Description: "Decrease volume by 10%"},
		{Command: "halpradio volume mute", Description: "Toggle mute"},
	})
}

// PrintCurrentHelp renders help for `halpradio current`.
func PrintCurrentHelp(out io.Writer) {
	RenderHelpHeader(out, "halpradio current", "Query currently playing track for status bars (tmux, Waybar, SketchyBar)")

	RenderHelpUsage(out,
		"halpradio current [flags]",
	)

	RenderHelpItems(out, "Flags", []HelpItem{
		{Short: "-f", Name: "--format", Args: `"<template>"`, Description: "Custom output format with template placeholders"},
		{Short: "-j", Name: "--json", Description: "Output playback status as JSON object"},
		{Short: "-h", Name: "--help", Description: "Show current help"},
	})

	RenderHelpItems(out, "Format Placeholders", []HelpItem{
		{Name: "%s", Description: "Station name"},
		{Name: "%t", Description: "Track title (Artist - Title)"},
		{Name: "%a", Description: "Artist name"},
		{Name: "%T", Description: "Song title"},
		{Name: "%p", Description: "Playback status (PLAYING, PAUSED, STOPPED)"},
		{Name: "%v", Description: "Volume percentage"},
		{Name: "%b", Description: "Active audio backend"},
		{Name: "%r", Description: "Stream bitrate (kbps)"},
	})

	RenderHelpExamples(out, "Examples", []HelpExample{
		{Command: "halpradio current", Description: "Print current station & track"},
		{Command: "halpradio current --format \"[%p] %s - %t\"", Description: "Status bar format for tmux or Waybar"},
		{Command: "halpradio current --json", Description: "Output full JSON for scripting"},
	})
}

// PrintStatusHelp renders help for `halpradio status`.
func PrintStatusHelp(out io.Writer) {
	RenderHelpHeader(out, "halpradio status", "Get full playback status snapshot of active instance")

	RenderHelpUsage(out,
		"halpradio status [flags]",
	)

	RenderHelpItems(out, "Flags", []HelpItem{
		{Short: "-f", Name: "--format", Args: `"<template>"`, Description: "Custom output format"},
		{Short: "-j", Name: "--json", Description: "Output full status snapshot as JSON"},
		{Short: "-h", Name: "--help", Description: "Show status help"},
	})

	RenderHelpExamples(out, "Examples", []HelpExample{
		{Command: "halpradio status", Description: "Print status line"},
		{Command: "halpradio status --json", Description: "Output full JSON status"},
	})
}

// PrintRemoteHelp renders help for `halpradio remote`.
func PrintRemoteHelp(out io.Writer) {
	RenderHelpHeader(out, "halpradio remote", "Send control commands to active halpradio instance over IPC")

	RenderHelpUsage(out,
		"halpradio remote <command> [flags]",
	)

	RenderHelpItems(out, "Available Remote Actions", []HelpItem{
		{Name: "toggle", Description: "Toggle playback between play and pause"},
		{Name: "play / pause / stop", Description: "Control playback state"},
		{Name: "next / prev", Description: "Switch to next or previous station in catalog"},
		{Name: "volup / voldown", Description: "Adjust volume up or down by 5%"},
		{Name: "mute", Description: "Toggle mute on active instance"},
		{Name: "random", Description: "Switch to a random station"},
		{Name: "status / current", Description: "Query playback status from active instance"},
	})

	RenderHelpItems(out, "Flags", []HelpItem{
		{Short: "-j", Name: "--json", Description: "Output response as JSON"},
		{Short: "-h", Name: "--help", Description: "Show remote help"},
	})

	RenderHelpExamples(out, "Examples", []HelpExample{
		{Command: "halpradio remote toggle", Description: "Toggle play/pause"},
		{Command: "halpradio remote next", Description: "Skip to next station"},
		{Command: "halpradio remote volup", Description: "Increase volume by 5%"},
	})
}

// PrintPluginHelp renders help for `halpradio plugin`.
func PrintPluginHelp(out io.Writer) {
	RenderHelpHeader(out, "halpradio plugin", "Sandboxed Wasm Plugin Manager")

	RenderHelpUsage(out,
		"halpradio plugin <command> [arguments]",
	)

	RenderHelpItems(out, "Commands", []HelpItem{
		{Name: "list", Description: "List installed and official registry plugins"},
		{Name: "install", Args: "<plugin-id>", Description: "Install a plugin from official registry"},
		{Name: "enable", Args: "<plugin-id>", Description: "Enable an installed plugin"},
		{Name: "disable", Args: "<plugin-id>", Description: "Disable an installed plugin"},
		{Name: "remove", Args: "<plugin-id>", Description: "Uninstall an installed plugin"},
		{Name: "update", Args: "<id|--all>", Description: "Update installed plugins from registry"},
	})

	RenderHelpExamples(out, "Examples", []HelpExample{
		{Command: "halpradio plugin list", Description: "List available & installed plugins"},
		{Command: "halpradio plugin install visualizer-matrix", Description: "Install a plugin"},
		{Command: "halpradio plugin disable visualizer-matrix", Description: "Disable a plugin"},
		{Command: "halpradio plugin update --all", Description: "Update all plugins"},
	})
}

// PrintUpdateStationsHelp renders help for `halpradio update-stations`.
func PrintUpdateStationsHelp(out io.Writer) {
	RenderHelpHeader(out, "halpradio update-stations", "Update station catalog from remote repository")

	RenderHelpUsage(out,
		"halpradio update-stations",
		"halpradio update-catalog                 "+styleDim.Render("(alias)"),
	)

	RenderHelpExamples(out, "Examples", []HelpExample{
		{Command: "halpradio update-stations", Description: "Fetch and cache latest station catalog"},
	})
}

// PrintPlaybackControlHelp renders help for top-level playback shortcuts (toggle, pause, stop, etc.).
func PrintPlaybackControlHelp(cmd string, out io.Writer) {
	RenderHelpHeader(out, "halpradio "+cmd, fmt.Sprintf("Send '%s' action to active halpradio instance", cmd))
	RenderHelpUsage(out, fmt.Sprintf("halpradio %s", cmd))
	RenderHelpExamples(out, "Examples", []HelpExample{
		{Command: "halpradio " + cmd, Description: fmt.Sprintf("Execute %s on running player", cmd)},
	})
}

// PrintPartyHelp renders help for `halpradio party`.
func PrintPartyHelp(out io.Writer) {
	RenderHelpHeader(out, "halpradio party", "P2P Mesh Synchronized Radio Rooms & Live Reactions (E2EE)")
	RenderHelpUsage(out,
		"halpradio party create [name] [flags]",
		"halpradio party join <room-code> [flags]",
		"halpradio party status [--json]",
		"halpradio party leave",
		"halpradio party react <1-5|emoji>",
		"halpradio party chat <message>",
	)
	RenderHelpItems(out, "Commands", []HelpItem{
		{Name: "create", Args: "[name]", Description: "Create and host an encrypted P2P radio room (generates #8X2K9P)"},
		{Name: "join", Args: "<code|#code>", Description: "Join an active radio room using its 6-character room code"},
		{Name: "status", Description: "Show party status, active room code, role, and connected listeners"},
		{Name: "leave", Description: "Leave current party room (triggers host migration if hosting)"},
		{Name: "react", Args: "<1-5|emoji>", Description: "Broadcast an ASCII reaction (1:🔥, 2:❤️, 3:☕, 4:🚀, 5:👀)"},
		{Name: "chat", Args: "<message>", Description: "Send a 1-line mini-chat ping to all listeners in the room"},
	})
	RenderHelpItems(out, "Flags (create & join)", []HelpItem{
		{Short: "-d", Name: "--dj-pass", Args: "<host|open>", Description: "DJ tuning permission: host (DJ only) or open (democratic)", Default: "host"},
		{Short: "-n", Name: "--nickname", Args: "<name>", Description: "Listener handle shown to peers in party room", Default: "$USER"},
		{Short: "-a", Name: "--address", Args: "<host:port>", Description: "Direct TCP peer address (skips LAN discovery)"},
		{Name: "--port", Args: "<port>", Description: "TCP port to bind for peer mesh connections", Default: "auto"},
		{Name: "--headless", Description: "Stream synchronized audio headlessly in terminal (no TUI)"},
		{Short: "-j", Name: "--json", Description: "Output status and events formatted as JSON"},
	})
	RenderHelpExamples(out, "Examples", []HelpExample{
		{Command: "halpradio party create \"chill-vibes\"", Description: "Create room 'chill-vibes' and launch TUI"},
		{Command: "halpradio party create --dj-pass=open", Description: "Create room allowing any listener to tune stations"},
		{Command: "halpradio party create --headless", Description: "Create and host room headlessly in server/tmux"},
		{Command: "halpradio party join 8X2K9P", Description: "Join radio room #8X2K9P with interactive TUI"},
		{Command: "halpradio party status", Description: "Inspect current party room, listener count, and host"},
		{Command: "halpradio party react 1", Description: "Send live 🔥 reaction to current room"},
		{Command: "halpradio party chat \"loving this track!\"", Description: "Send mini-chat ping to room"},
		{Command: "halpradio party leave", Description: "Disconnect from current party room"},
	})
	RenderHelpFooter(out,
		`Use "halpradio party create --help" or "halpradio party join --help" for detailed flag options.`,
	)
}

// RouteHelp routes `halpradio help <command...>` to the appropriate help renderer.
func RouteHelp(args []string, embeddedCatalog []byte, out io.Writer) bool {
	if len(args) == 0 {
		PrintRootHelp(out)
		return true
	}

	sub := strings.ToLower(args[0])
	switch sub {
	case "play":
		PrintPlayHelp(out)
		return true
	case "party":
		PrintPartyHelp(out)
		return true
	case "stations", "station":
		if len(args) > 1 {
			switch strings.ToLower(args[1]) {
			case "list", "ls":
				PrintStationsListHelp(out)
				return true
			case "search", "find":
				PrintStationsSearchHelp(out)
				return true
			case "fav", "favorite", "favorites":
				PrintStationsFavHelp(out)
				return true
			case "add":
				PrintStationsAddHelp(out)
				return true
			}
		}
		PrintStationsHelp(out)
		return true
	case "search", "find":
		PrintStationsSearchHelp(out)
		return true
	case "volume", "vol":
		PrintVolumeHelp(out)
		return true
	case "current":
		PrintCurrentHelp(out)
		return true
	case "status":
		PrintStatusHelp(out)
		return true
	case "remote":
		PrintRemoteHelp(out)
		return true
	case "plugin", "plugins":
		PrintPluginHelp(out)
		return true
	case "theme", "themes":
		PrintThemeHelp(out)
		return true
	case "update-stations", "update-catalog":
		PrintUpdateStationsHelp(out)
		return true
	case "toggle", "pause", "stop", "next", "prev", "volup", "voldown", "mute", "random":
		PrintPlaybackControlHelp(sub, out)
		return true
	case "version":
		fmt.Fprintf(out, "halpradio v%s - LazyVim-inspired Terminal Internet Radio Streamer\n", Version)
		return true
	default:
		sugg := SuggestCommand(sub, RootCommands)
		if sugg != "" {
			fmt.Fprintf(out, "%s: %q is not a valid halpradio command.\n\nDid you mean this?\n  %s\n\nRun 'halpradio --help' for available commands.\n",
				styleError.Render("halpradio"),
				sub,
				styleSuggestion.Render(sugg),
			)
		} else {
			fmt.Fprintf(out, "%s: %q is not a valid halpradio command. Run 'halpradio --help' for available commands.\n",
				styleError.Render("halpradio"),
				sub,
			)
		}
		return false
	}
}

// RootCommands lists all valid top-level halpradio commands for typo suggestions.
var RootCommands = []string{
	"play", "party", "stations", "volume", "current", "status",
	"toggle", "pause", "stop", "next", "prev", "volup", "voldown", "mute", "random", "remote",
	"theme", "plugin", "update-stations", "update-catalog", "version", "help",
}

// SuggestCommand finds the closest matching command from candidates using Levenshtein distance.
func SuggestCommand(input string, candidates []string) string {
	inputLower := strings.ToLower(strings.TrimSpace(input))
	if inputLower == "" {
		return ""
	}

	// Exact prefix match
	for _, c := range candidates {
		cLower := strings.ToLower(c)
		if strings.HasPrefix(cLower, inputLower) {
			return c
		}
	}

	bestDist := 999
	bestMatch := ""
	for _, c := range candidates {
		cLower := strings.ToLower(c)
		dist := levenshteinDistance(inputLower, cLower)
		if dist < bestDist {
			bestDist = dist
			bestMatch = c
		}
	}

	maxAllowed := 3
	if len(inputLower) <= 3 {
		maxAllowed = 1
	} else if len(inputLower) <= 5 {
		maxAllowed = 2
	}

	if bestDist <= maxAllowed {
		return bestMatch
	}
	return ""
}

func levenshteinDistance(s1, s2 string) int {
	r1, r2 := []rune(s1), []rune(s2)
	n, m := len(r1), len(r2)
	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}

	d := make([][]int, n+1)
	for i := range d {
		d[i] = make([]int, m+1)
		d[i][0] = i
	}
	for j := 0; j <= m; j++ {
		d[0][j] = j
	}

	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			cost := 0
			if r1[i-1] != r2[j-1] {
				cost = 1
			}
			d[i][j] = minInt(
				d[i-1][j]+1,      // deletion
				d[i][j-1]+1,      // insertion
				d[i-1][j-1]+cost, // substitution
			)
		}
	}
	return d[n][m]
}

func minInt(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}
