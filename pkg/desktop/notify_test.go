package desktop

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFormatSongNotification(t *testing.T) {
	tests := []struct {
		name      string
		station   string
		track     string
		wantTitle string
		wantBody  string
	}{
		{
			name:      "station and track present",
			station:   "SomaFM Secret Agent",
			track:     "James Bond Theme",
			wantTitle: "📻 halpradio — SomaFM Secret Agent",
			wantBody:  "🎶 James Bond Theme",
		},
		{
			name:      "station with whitespace trimming",
			station:   "  Defcon Radio  ",
			track:     "  Hacker Track  ",
			wantTitle: "📻 halpradio — Defcon Radio",
			wantBody:  "🎶 Hacker Track",
		},
		{
			name:      "empty station name",
			station:   "",
			track:     "Ambient Song",
			wantTitle: "📻 halpradio",
			wantBody:  "🎶 Ambient Song",
		},
		{
			name:      "empty track title",
			station:   "Lofi Girl",
			track:     "",
			wantTitle: "📻 halpradio — Lofi Girl",
			wantBody:  "Now Playing",
		},
		{
			name:      "both empty",
			station:   "",
			track:     "",
			wantTitle: "📻 halpradio",
			wantBody:  "Now Playing",
		},
		{
			name:      "unicode and emojis",
			station:   "Tokyo FM 東京",
			track:     "J-Pop ♫ Melody (Remix)",
			wantTitle: "📻 halpradio — Tokyo FM 東京",
			wantBody:  "🎶 J-Pop ♫ Melody (Remix)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, body := FormatSongNotification(tt.station, tt.track)
			if title != tt.wantTitle {
				t.Errorf("FormatSongNotification(%q, %q) title = %q; want %q", tt.station, tt.track, title, tt.wantTitle)
			}
			if body != tt.wantBody {
				t.Errorf("FormatSongNotification(%q, %q) body = %q; want %q", tt.station, tt.track, body, tt.wantBody)
			}
		})
	}
}

func TestDesktopNotifierDeduplication(t *testing.T) {
	var mu sync.Mutex
	var calls []string

	mockRunner := func(ctx context.Context, name string, args ...string) error {
		mu.Lock()
		calls = append(calls, name+" "+strings.Join(args, " "))
		mu.Unlock()
		return nil
	}

	n := NewNotifier(true)
	n.runner = mockRunner
	n.goos = "linux"

	// First call should dispatch
	n.NotifySong("Station A", "Song 1")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(calls) != 1 {
		t.Fatalf("expected 1 notification call, got %d", len(calls))
	}
	mu.Unlock()

	// Immediate duplicate call should be suppressed
	n.NotifySong("Station A", "Song 1")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(calls) != 1 {
		t.Fatalf("duplicate notification was not suppressed, got %d calls", len(calls))
	}
	mu.Unlock()

	// New track on same station should trigger notification
	n.NotifySong("Station A", "Song 2")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(calls) != 2 {
		t.Fatalf("expected 2 notification calls for new track, got %d", len(calls))
	}
	mu.Unlock()

	// Reset deduplication and trigger same song
	n.ResetDeduplication()
	n.NotifySong("Station A", "Song 2")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(calls) != 3 {
		t.Fatalf("expected 3 notification calls after ResetDeduplication, got %d", len(calls))
	}
	mu.Unlock()

	// Empty station and empty track should be ignored
	n.NotifySong("", "")
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	if len(calls) != 3 {
		t.Fatalf("expected 3 calls (empty song ignored), got %d", len(calls))
	}
	mu.Unlock()
}

func TestDesktopNotifierDisabled(t *testing.T) {
	var mu sync.Mutex
	var calls []string

	mockRunner := func(ctx context.Context, name string, args ...string) error {
		mu.Lock()
		calls = append(calls, name)
		mu.Unlock()
		return nil
	}

	n := NewNotifier(false)
	n.runner = mockRunner

	if n.IsEnabled() {
		t.Errorf("expected notifier to be disabled")
	}

	n.NotifySong("Station A", "Song 1")
	n.Notify("Title", "Message")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	if len(calls) != 0 {
		t.Errorf("expected 0 calls when disabled, got %d", len(calls))
	}
	mu.Unlock()

	n.SetEnabled(true)
	if !n.IsEnabled() {
		t.Errorf("expected notifier to be enabled after SetEnabled(true)")
	}
}

func TestDispatchOSNotification_Darwin_SilentWithoutSound(t *testing.T) {
	tests := []struct {
		name        string
		title       string
		message     string
		wantCommand string
		wantScript  string
	}{
		{
			name:        "standard song change",
			title:       "📻 halpradio — SomaFM Secret Agent",
			message:     "🎶 James Bond Theme",
			wantCommand: "osascript",
			wantScript:  `display notification "🎶 James Bond Theme" with title "📻 halpradio — SomaFM Secret Agent"`,
		},
		{
			name:        "song with quotes and special characters",
			title:       `📻 halpradio — "Rock" 101`,
			message:     `🎶 Guns N' Roses - "Welcome to the Jungle"`,
			wantCommand: "osascript",
			wantScript:  `display notification "🎶 Guns N' Roses - \"Welcome to the Jungle\"" with title "📻 halpradio — \"Rock\" 101"`,
		},
		{
			name:        "generic alert",
			title:       "halpradio",
			message:     "Playback paused",
			wantCommand: "osascript",
			wantScript:  `display notification "Playback paused" with title "halpradio"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var calledName string
			var calledArgs []string

			mockRunner := func(ctx context.Context, name string, args ...string) error {
				calledName = name
				calledArgs = args
				return nil
			}

			dispatchOSNotification(mockRunner, "darwin", tt.title, tt.message)

			if calledName != tt.wantCommand {
				t.Errorf("expected command %q, got %q", tt.wantCommand, calledName)
			}

			if len(calledArgs) != 2 || calledArgs[0] != "-e" {
				t.Fatalf("expected args [-e, script], got %v", calledArgs)
			}

			actualScript := calledArgs[1]
			if actualScript != tt.wantScript {
				t.Errorf("script mismatch:\n  got:  %s\n  want: %s", actualScript, tt.wantScript)
			}

			// Explicitly assert that no sound parameter or chime sound is present
			if strings.Contains(actualScript, "sound name") {
				t.Errorf("macOS notification script must NOT contain 'sound name', got: %s", actualScript)
			}
			if strings.Contains(strings.ToLower(actualScript), "glass") {
				t.Errorf("macOS notification script must NOT contain 'Glass' chime, got: %s", actualScript)
			}
		})
	}
}

func TestDispatchOSNotification_Linux_SilentWithHint(t *testing.T) {
	var calledName string
	var calledArgs []string

	mockRunner := func(ctx context.Context, name string, args ...string) error {
		calledName = name
		calledArgs = args
		return nil
	}

	dispatchOSNotification(mockRunner, "linux", "📻 halpradio — Nightwave", "🎶 Macintosh Plus")

	if calledName != "notify-send" {
		t.Fatalf("expected notify-send, got %s", calledName)
	}

	expectedArgs := []string{
		"-a", "halpradio",
		"-u", "normal",
		"-h", "boolean:suppress-sound:true",
		"📻 halpradio — Nightwave",
		"🎶 Macintosh Plus",
	}

	if len(calledArgs) != len(expectedArgs) {
		t.Fatalf("expected %d args, got %d (%v)", len(expectedArgs), len(calledArgs), calledArgs)
	}

	for i, exp := range expectedArgs {
		if calledArgs[i] != exp {
			t.Errorf("arg[%d] = %q, want %q", i, calledArgs[i], exp)
		}
	}
}

func TestDispatchOSNotification_Windows_SilentWithAudioTag(t *testing.T) {
	var calledName string
	var calledArgs []string

	mockRunner := func(ctx context.Context, name string, args ...string) error {
		calledName = name
		calledArgs = args
		return nil
	}

	dispatchOSNotification(mockRunner, "windows", "📻 halpradio — SomaFM", "🎶 Groove Salad")

	if calledName != "powershell" {
		t.Fatalf("expected powershell, got %s", calledName)
	}

	if len(calledArgs) < 4 || calledArgs[0] != "-NoProfile" || calledArgs[1] != "-NonInteractive" || calledArgs[2] != "-Command" {
		t.Fatalf("unexpected powershell args: %v", calledArgs)
	}

	psScript := calledArgs[3]
	if !strings.Contains(psScript, "SetAttribute('silent', 'true')") {
		t.Errorf("windows powershell script must contain silent audio attribute, got: %s", psScript)
	}
	if !strings.Contains(psScript, "CreateElement('audio')") {
		t.Errorf("windows powershell script must create audio element, got: %s", psScript)
	}
	if !strings.Contains(psScript, "DocumentElement.AppendChild($audio)") {
		t.Errorf("windows powershell script must append audio node, got: %s", psScript)
	}
}

func TestDispatchOSNotification_Platforms(t *testing.T) {
	platforms := []struct {
		os          string
		wantCommand string
		checkArgs   func(t *testing.T, args []string)
	}{
		{
			os:          "darwin",
			wantCommand: "osascript",
			checkArgs: func(t *testing.T, args []string) {
				if len(args) != 2 || args[0] != "-e" {
					t.Errorf("darwin expected [-e <script>], got %v", args)
				}
				if strings.Contains(args[1], "sound name") {
					t.Errorf("darwin script should not have sound name, got %s", args[1])
				}
			},
		},
		{
			os:          "linux",
			wantCommand: "notify-send",
			checkArgs: func(t *testing.T, args []string) {
				expected := []string{"-a", "halpradio", "-u", "normal", "-h", "boolean:suppress-sound:true", "Title", "Message"}
				if len(args) != len(expected) {
					t.Errorf("linux expected %v, got %v", expected, args)
					return
				}
				for i, v := range expected {
					if args[i] != v {
						t.Errorf("linux arg[%d] = %q, want %q", i, args[i], v)
					}
				}
			},
		},
		{
			os:          "windows",
			wantCommand: "powershell",
			checkArgs: func(t *testing.T, args []string) {
				if len(args) < 4 || args[0] != "-NoProfile" || args[1] != "-NonInteractive" || args[2] != "-Command" {
					t.Errorf("windows expected powershell flags, got %v", args)
				}
				if !strings.Contains(args[3], "SetAttribute('silent', 'true')") {
					t.Errorf("windows expected silent audio attribute, got %s", args[3])
				}
				if !strings.Contains(args[3], "CreateToastNotifier('halpradio')") {
					t.Errorf("windows expected toast notifier creation script, got %s", args[3])
				}
			},
		},
		{
			os:          "freebsd",
			wantCommand: "",
			checkArgs: func(t *testing.T, args []string) {
				if len(args) != 0 {
					t.Errorf("unsupported OS should not invoke args, got %v", args)
				}
			},
		},
	}

	for _, p := range platforms {
		t.Run(p.os, func(t *testing.T) {
			var calledName string
			var calledArgs []string

			mockRunner := func(ctx context.Context, name string, args ...string) error {
				calledName = name
				calledArgs = args
				return nil
			}

			dispatchOSNotification(mockRunner, p.os, "Title", "Message")

			if calledName != p.wantCommand {
				t.Errorf("OS %q expected command %q, got %q", p.os, p.wantCommand, calledName)
			}
			p.checkArgs(t, calledArgs)
		})
	}
}

func TestDesktopNotifierDarwinEndToEnd(t *testing.T) {
	var mu sync.Mutex
	var calledCommand string
	var calledArgs []string

	mockRunner := func(ctx context.Context, name string, args ...string) error {
		mu.Lock()
		calledCommand = name
		calledArgs = args
		mu.Unlock()
		return nil
	}

	n := NewNotifier(true)
	n.runner = mockRunner
	n.goos = "darwin"

	n.NotifySong("Nightwave Plaza", "Macintosh Plus - リサフランク420 / 現代のコンピュー")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if calledCommand != "osascript" {
		t.Errorf("expected osascript, got %s", calledCommand)
	}
	if len(calledArgs) != 2 || calledArgs[0] != "-e" {
		t.Fatalf("expected [-e, script], got %v", calledArgs)
	}

	script := calledArgs[1]
	if !strings.HasPrefix(script, `display notification`) {
		t.Errorf("expected script to start with display notification, got %s", script)
	}
	if strings.Contains(script, "sound name") {
		t.Errorf("expected silent notification without sound name, got %s", script)
	}
	if !strings.Contains(script, "Nightwave Plaza") {
		t.Errorf("expected station name in script, got %s", script)
	}
}

func TestDesktopNotifierLinuxEndToEnd(t *testing.T) {
	var mu sync.Mutex
	var calledCommand string
	var calledArgs []string

	mockRunner := func(ctx context.Context, name string, args ...string) error {
		mu.Lock()
		calledCommand = name
		calledArgs = args
		mu.Unlock()
		return nil
	}

	n := NewNotifier(true)
	n.runner = mockRunner
	n.goos = "linux"

	n.NotifySong("Lofi Girl", "Kupla - Kingdom in Blue")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if calledCommand != "notify-send" {
		t.Errorf("expected notify-send, got %s", calledCommand)
	}
	foundSilentHint := false
	for _, arg := range calledArgs {
		if arg == "boolean:suppress-sound:true" {
			foundSilentHint = true
			break
		}
	}
	if !foundSilentHint {
		t.Errorf("expected boolean:suppress-sound:true in args, got %v", calledArgs)
	}
}

func TestDesktopNotifierWindowsEndToEnd(t *testing.T) {
	var mu sync.Mutex
	var calledCommand string
	var calledArgs []string

	mockRunner := func(ctx context.Context, name string, args ...string) error {
		mu.Lock()
		calledCommand = name
		calledArgs = args
		mu.Unlock()
		return nil
	}

	n := NewNotifier(true)
	n.runner = mockRunner
	n.goos = "windows"

	n.NotifySong("Ibiza Global Radio", "Deep House Live")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if calledCommand != "powershell" {
		t.Errorf("expected powershell, got %s", calledCommand)
	}
	if len(calledArgs) < 4 {
		t.Fatalf("expected powershell args, got %v", calledArgs)
	}
	if !strings.Contains(calledArgs[3], "SetAttribute('silent', 'true')") {
		t.Errorf("expected silent audio attribute in windows command, got %s", calledArgs[3])
	}
}

func TestDesktopNotifierGenericNotifyDarwin(t *testing.T) {
	var mu sync.Mutex
	var calledArgs []string

	mockRunner := func(ctx context.Context, name string, args ...string) error {
		mu.Lock()
		calledArgs = args
		mu.Unlock()
		return nil
	}

	n := NewNotifier(true)
	n.runner = mockRunner
	n.goos = "darwin"

	n.Notify("Focus Time", "Session ended")
	time.Sleep(50 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(calledArgs) != 2 {
		t.Fatalf("expected 2 args, got %v", calledArgs)
	}
	if strings.Contains(calledArgs[1], "sound name") {
		t.Errorf("expected no sound name, got %s", calledArgs[1])
	}
	expectedScript := `display notification "Session ended" with title "Focus Time"`
	if calledArgs[1] != expectedScript {
		t.Errorf("expected %q, got %q", expectedScript, calledArgs[1])
	}
}

func TestDesktopNotifierConcurrentSafety(t *testing.T) {
	mockRunner := func(ctx context.Context, name string, args ...string) error {
		return nil
	}

	n := NewNotifier(true)
	n.runner = mockRunner
	n.goos = "darwin"

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				n.NotifySong("Station", "Track")
			} else if idx%3 == 0 {
				n.Notify("Title", "Message")
			} else if idx%5 == 0 {
				n.SetEnabled(idx%2 == 0)
			} else if idx%7 == 0 {
				_ = n.IsEnabled()
			} else {
				n.ResetDeduplication()
			}
		}(i)
	}
	wg.Wait()
}

func TestDesktopNotifierNilSafe(t *testing.T) {
	var n *DesktopNotifier
	// Should not panic on nil receiver
	n.NotifySong("Station", "Track")
	n.Notify("Title", "Message")
	n.ResetDeduplication()
	n.SetEnabled(true)
	if n.IsEnabled() {
		t.Errorf("nil notifier IsEnabled should return false")
	}
}
