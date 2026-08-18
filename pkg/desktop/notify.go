package desktop

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Notifier defines the interface for desktop notifications.
type Notifier interface {
	NotifySong(stationName, trackTitle string)
	Notify(title, message string)
	ResetDeduplication()
}

// CommandRunner allows injecting a command execution function for unit testing.
type CommandRunner func(ctx context.Context, name string, args ...string) error

func defaultCommandRunner(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Run()
}

// DesktopNotifier manages desktop notification dispatching with track deduplication.
type DesktopNotifier struct {
	mu             sync.Mutex
	enabled        bool
	lastStation    string
	lastTrack      string
	lastNotifyTime time.Time
	runner         CommandRunner
	goos           string
}

// NewNotifier creates a new DesktopNotifier.
func NewNotifier(enabled bool) *DesktopNotifier {
	return &DesktopNotifier{
		enabled: enabled,
		runner:  defaultCommandRunner,
		goos:    runtime.GOOS,
	}
}

// SetRunner sets the command runner for desktop notifications (useful for tests and custom runners).
func (n *DesktopNotifier) SetRunner(r CommandRunner) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if r == nil {
		r = defaultCommandRunner
	}
	n.runner = r
}

// GetRunner returns the active command runner.
func (n *DesktopNotifier) GetRunner() CommandRunner {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.runner
}

// SetEnabled enables or disables notifications.
func (n *DesktopNotifier) SetEnabled(enabled bool) {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.enabled = enabled
}

// IsEnabled reports whether notifications are enabled.
func (n *DesktopNotifier) IsEnabled() bool {
	if n == nil {
		return false
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.enabled
}

// ResetDeduplication clears the track deduplication cache.
func (n *DesktopNotifier) ResetDeduplication() {
	if n == nil {
		return
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	n.lastStation = ""
	n.lastTrack = ""
	n.lastNotifyTime = time.Time{}
}

// FormatSongNotification formats the station name and track title into standard notification title and body.
func FormatSongNotification(stationName, trackTitle string) (title string, body string) {
	stationName = strings.TrimSpace(stationName)
	trackTitle = strings.TrimSpace(trackTitle)

	if stationName != "" {
		title = fmt.Sprintf("📻 halpradio — %s", stationName)
	} else {
		title = "📻 halpradio"
	}

	if trackTitle != "" {
		body = fmt.Sprintf("🎶 %s", trackTitle)
	} else {
		body = "Now Playing"
	}

	return title, body
}

// NotifySong dispatches a song change notification if not duplicate and enabled.
func (n *DesktopNotifier) NotifySong(stationName, trackTitle string) {
	if n == nil {
		return
	}

	stationName = strings.TrimSpace(stationName)
	trackTitle = strings.TrimSpace(trackTitle)

	if stationName == "" && trackTitle == "" {
		return
	}

	n.mu.Lock()
	if !n.enabled {
		n.mu.Unlock()
		return
	}

	// Deduplication check
	now := time.Now()
	if n.lastStation == stationName && n.lastTrack == trackTitle && now.Sub(n.lastNotifyTime) < 30*time.Second {
		n.mu.Unlock()
		return
	}

	n.lastStation = stationName
	n.lastTrack = trackTitle
	n.lastNotifyTime = now
	runner := n.runner
	targetOS := n.goos
	n.mu.Unlock()

	title, body := FormatSongNotification(stationName, trackTitle)
	go dispatchOSNotification(runner, targetOS, title, body)
}

// Notify dispatches an arbitrary title & message notification.
func (n *DesktopNotifier) Notify(title, message string) {
	if n == nil {
		return
	}

	n.mu.Lock()
	if !n.enabled {
		n.mu.Unlock()
		return
	}
	runner := n.runner
	targetOS := n.goos
	n.mu.Unlock()

	go dispatchOSNotification(runner, targetOS, title, message)
}

// sanitizeNotificationString strips ASCII control characters and truncates length.
func sanitizeNotificationString(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		// Keep printable characters, spaces, and standard unicode
		if r >= 0x20 && r != 0x7f {
			b.WriteRune(r)
		}
	}
	res := b.String()
	if len(res) > maxLen {
		res = res[:maxLen]
	}
	return res
}

func dispatchOSNotification(runner CommandRunner, targetOS, title, message string) {
	if runner == nil {
		runner = defaultCommandRunner
	}

	// Strict sanitization to prevent terminal and command injection
	title = sanitizeNotificationString(title, 256)
	message = sanitizeNotificationString(message, 1024)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	switch targetOS {
	case "darwin":
		// macOS AppleScript notification via argv passing (immune to script/command injection)
		_ = runner(
			ctx,
			"osascript",
			"-e", "on run argv",
			"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
			"-e", "end run",
			title,
			message,
		)

	case "linux":
		// Linux notification via notify-send (silent hint suppresses sound on notification daemons)
		_ = runner(ctx, "notify-send", "-a", "halpradio", "-u", "normal", "-h", "boolean:suppress-sound:true", title, message)

	case "windows":
		// Windows PowerShell Toast Notification passing parameters as isolated arguments without string interpolation
		psScript := `[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null; $template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02); $textNodes = $template.GetElementsByTagName('text'); $textNodes.Item(0).AppendChild($template.CreateTextNode($args[0])) > $null; $textNodes.Item(1).AppendChild($template.CreateTextNode($args[1])) > $null; $audio = $template.CreateElement('audio'); $audio.SetAttribute('silent', 'true'); $template.DocumentElement.AppendChild($audio) > $null; $toast = [Windows.UI.Notifications.ToastNotification]::new($template); [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('halpradio').Show($toast);`
		_ = runner(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psScript, title, message)
	}
}
