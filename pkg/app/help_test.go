package app

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintRootHelp(t *testing.T) {
	var buf bytes.Buffer
	PrintRootHelp(&buf)
	out := buf.String()

	expectedSections := []string{
		"halpradio",
		"Terminal Internet Radio Player & Streamer",
		"Usage:",
		"Core Commands:",
		"play",
		"stations",
		"volume",
		"current",
		"status",
		"Playback & Remote Controls:",
		"toggle",
		"pause / stop",
		"mute",
		"random",
		"remote",
		"Extensions & Catalog:",
		"plugin",
		"update-stations",
		"version",
		"Interactive TUI Flags:",
		"--backend",
		"--theme",
		"--notifications",
		"--autopause",
		"--discord",
		"--mpris",
		"--ipc",
		"Top Automation Examples:",
		"Learn More:",
	}

	for _, sec := range expectedSections {
		if !strings.Contains(out, sec) {
			t.Errorf("PrintRootHelp output missing expected substring %q", sec)
		}
	}
}

func TestPrintSubcommandHelps(t *testing.T) {
	tests := []struct {
		name     string
		printFn  func(buf *bytes.Buffer)
		contains []string
	}{
		{
			name: "play",
			printFn: func(b *bytes.Buffer) {
				PrintPlayHelp(b)
			},
			contains: []string{"halpradio play", "Usage:", "Arguments:", "Flags:", "--volume", "--backend", "--duration", "Examples:"},
		},
		{
			name: "stations",
			printFn: func(b *bytes.Buffer) {
				PrintStationsHelp(b)
			},
			contains: []string{"halpradio stations", "Subcommands:", "list", "search", "fav", "add", "List & Search Flags:", "Examples:"},
		},
		{
			name: "stations list",
			printFn: func(b *bytes.Buffer) {
				PrintStationsListHelp(b)
			},
			contains: []string{"halpradio stations list", "Flags:", "--genre", "--country", "--limit", "--plain", "--json"},
		},
		{
			name: "stations search",
			printFn: func(b *bytes.Buffer) {
				PrintStationsSearchHelp(b)
			},
			contains: []string{"halpradio stations search", "--online", "--limit", "Examples:"},
		},
		{
			name: "stations fav",
			printFn: func(b *bytes.Buffer) {
				PrintStationsFavHelp(b)
			},
			contains: []string{"halpradio stations fav", "add <id>", "remove <id>", "toggle <id>"},
		},
		{
			name: "stations add",
			printFn: func(b *bytes.Buffer) {
				PrintStationsAddHelp(b)
			},
			contains: []string{"halpradio stations add", "--name", "--url", "--genre", "--bitrate"},
		},
		{
			name: "volume",
			printFn: func(b *bytes.Buffer) {
				PrintVolumeHelp(b)
			},
			contains: []string{"halpradio volume", "Arguments:", "+<N>", "-<N>", "mute", "--json"},
		},
		{
			name: "current",
			printFn: func(b *bytes.Buffer) {
				PrintCurrentHelp(b)
			},
			contains: []string{"halpradio current", "--format", "Format Placeholders:", "%s", "%t", "%a", "%p", "%v"},
		},
		{
			name: "status",
			printFn: func(b *bytes.Buffer) {
				PrintStatusHelp(b)
			},
			contains: []string{"halpradio status", "--format", "--json"},
		},
		{
			name: "remote",
			printFn: func(b *bytes.Buffer) {
				PrintRemoteHelp(b)
			},
			contains: []string{"halpradio remote", "Available Remote Actions:", "toggle", "volup", "voldown"},
		},
		{
			name: "plugin",
			printFn: func(b *bytes.Buffer) {
				PrintPluginHelp(b)
			},
			contains: []string{"halpradio plugin", "Commands:", "list", "install", "enable", "disable", "remove", "update"},
		},
		{
			name: "update-stations",
			printFn: func(b *bytes.Buffer) {
				PrintUpdateStationsHelp(b)
			},
			contains: []string{"halpradio update-stations", "Examples:"},
		},
		{
			name: "playback control shortcut",
			printFn: func(b *bytes.Buffer) {
				PrintPlaybackControlHelp("toggle", b)
			},
			contains: []string{"halpradio toggle", "Usage:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			tt.printFn(&buf)
			out := buf.String()
			for _, exp := range tt.contains {
				if !strings.Contains(out, exp) {
					t.Errorf("Subcommand help %q missing expected text %q", tt.name, exp)
				}
			}
		})
	}
}

func TestRouteHelp(t *testing.T) {
	routes := []struct {
		args     []string
		wantOK   bool
		contains string
	}{
		{args: []string{}, wantOK: true, contains: "halpradio - Terminal Internet Radio Player"},
		{args: []string{"play"}, wantOK: true, contains: "halpradio play"},
		{args: []string{"stations"}, wantOK: true, contains: "halpradio stations"},
		{args: []string{"stations", "list"}, wantOK: true, contains: "halpradio stations list"},
		{args: []string{"stations", "search"}, wantOK: true, contains: "halpradio stations search"},
		{args: []string{"stations", "fav"}, wantOK: true, contains: "halpradio stations fav"},
		{args: []string{"stations", "add"}, wantOK: true, contains: "halpradio stations add"},
		{args: []string{"search"}, wantOK: true, contains: "halpradio stations search"},
		{args: []string{"volume"}, wantOK: true, contains: "halpradio volume"},
		{args: []string{"vol"}, wantOK: true, contains: "halpradio volume"},
		{args: []string{"current"}, wantOK: true, contains: "halpradio current"},
		{args: []string{"status"}, wantOK: true, contains: "halpradio status"},
		{args: []string{"remote"}, wantOK: true, contains: "halpradio remote"},
		{args: []string{"plugin"}, wantOK: true, contains: "halpradio plugin"},
		{args: []string{"update-stations"}, wantOK: true, contains: "halpradio update-stations"},
		{args: []string{"toggle"}, wantOK: true, contains: "halpradio toggle"},
		{args: []string{"version"}, wantOK: true, contains: Version},
		{args: []string{"unknowncmd"}, wantOK: false, contains: "is not a valid halpradio command"},
	}

	for _, r := range routes {
		t.Run(strings.Join(r.args, "_"), func(t *testing.T) {
			var buf bytes.Buffer
			ok := RouteHelp(r.args, nil, &buf)
			if ok != r.wantOK {
				t.Errorf("RouteHelp(%v) = %v, want %v", r.args, ok, r.wantOK)
			}
			if !strings.Contains(buf.String(), r.contains) {
				t.Errorf("RouteHelp(%v) output %q does not contain %q", r.args, buf.String(), r.contains)
			}
		})
	}
}

func TestSuggestCommand(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"ply", "play"},
		{"staitons", "stations"},
		{"volme", "volume"},
		{"curren", "current"},
		{"plugn", "plugin"},
		{"randm", "random"},
		{"xyz12345nonexistent", ""},
		{"", ""},
	}

	for _, tt := range tests {
		got := SuggestCommand(tt.input, RootCommands)
		if got != tt.want {
			t.Errorf("SuggestCommand(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestIsHelpArgAndHasHelpFlag(t *testing.T) {
	if !IsHelpArg("help") || !IsHelpArg("--help") || !IsHelpArg("-h") || !IsHelpArg("-help") {
		t.Errorf("IsHelpArg failed for standard help strings")
	}
	if IsHelpArg("play") || IsHelpArg("--play") {
		t.Errorf("IsHelpArg returned true for non-help string")
	}

	if !HasHelpFlag([]string{"somafm", "--help"}) {
		t.Errorf("HasHelpFlag failed to detect --help in args")
	}
	if HasHelpFlag([]string{"somafm", "-v", "50"}) {
		t.Errorf("HasHelpFlag detected help in args without help")
	}
}

func TestSetupAppHelpFlags(t *testing.T) {
	flagVariations := [][]string{
		{"help"},
		{"--help"},
		{"-help"},
		{"-h"},
		{"play", "--help"},
		{"stations", "-h"},
		{"volume", "-help"},
	}

	for _, args := range flagVariations {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var buf bytes.Buffer
			appInst, isDone, err := SetupApp(args, nil, &buf)
			if err != nil {
				t.Fatalf("SetupApp(%v) returned unexpected error: %v", args, err)
			}
			if !isDone {
				t.Errorf("SetupApp(%v) expected isDone true", args)
			}
			if appInst != nil {
				t.Errorf("SetupApp(%v) expected appInst nil for help command", args)
			}
			if !strings.Contains(buf.String(), "Usage:") {
				t.Errorf("SetupApp(%v) output did not contain 'Usage:', got: %s", args, buf.String())
			}
		})
	}
}

func TestSetupAppUnknownCommandTypo(t *testing.T) {
	var buf bytes.Buffer
	_, isDone, err := SetupApp([]string{"ply"}, nil, &buf)
	if err == nil {
		t.Errorf("SetupApp('ply') expected error, got nil")
	}
	if !isDone {
		t.Errorf("SetupApp('ply') expected isDone true")
	}
	out := buf.String()
	if !strings.Contains(out, "Did you mean this?") || !strings.Contains(out, "play") {
		t.Errorf("SetupApp('ply') did not suggest 'play', got: %s", out)
	}
}
