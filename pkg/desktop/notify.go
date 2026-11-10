package desktop

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	"github.com/halpworld/halpradio/pkg/util"
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

// sanitizeNotificationString removes everything that is not printable text
// (C0/C1 controls, DEL, bidi and zero-width format runes), collapses
// whitespace and caps the result at maxLen runes. It delegates to
// util.SanitizeDisplay so notifications behave like every other display sink.
func sanitizeNotificationString(s string, maxLen int) string {
	return util.SanitizeDisplay(s, maxLen)
}

// escapeNotificationMarkup escapes the three characters that freedesktop
// notification daemons interpret as body markup (<b>, <a href>, <img>, ...).
// Quotes are left alone: they carry no meaning outside a tag, and daemons that
// do not render markup would show a numeric entity literally.
func escapeNotificationMarkup(s string) string {
	return markupEscaper.Replace(s)
}

var markupEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// powershellToastScript is the toast script run on Windows. The two %s verbs
// receive base64 text only (A-Z a-z 0-9 + / =), which cannot leave the
// single-quoted literal it is placed in; the script decodes the values itself.
const powershellToastScript = `$t=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s')); ` +
	`$m=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('%s')); ` +
	`[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null; ` +
	`$template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02); ` +
	`$textNodes = $template.GetElementsByTagName('text'); ` +
	`$textNodes.Item(0).AppendChild($template.CreateTextNode($t)) > $null; ` +
	`$textNodes.Item(1).AppendChild($template.CreateTextNode($m)) > $null; ` +
	`$audio = $template.CreateElement('audio'); $audio.SetAttribute('silent', 'true'); ` +
	`$template.DocumentElement.AppendChild($audio) > $null; ` +
	`$toast = [Windows.UI.Notifications.ToastNotification]::new($template); ` +
	`[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('halpradio').Show($toast);`

// PowerShellToastScript returns the plain-text toast script for title and
// message (exported for tests and for the timer package).
func PowerShellToastScript(title, message string) string {
	b64 := func(v string) string { return base64.StdEncoding.EncodeToString([]byte(v)) }
	return fmt.Sprintf(powershellToastScript, b64(title), b64(message))
}

// EncodePowerShellCommand encodes a script for powershell -EncodedCommand
// (base64 of the UTF-16LE text). With -EncodedCommand nothing on the command
// line is parsed as script except this single base64 token.
func EncodePowerShellCommand(script string) string {
	u := utf16.Encode([]rune(script))
	buf := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(buf[2*i:], c)
	}
	return base64.StdEncoding.EncodeToString(buf)
}

// NotificationCommand returns the program and argv that show a desktop
// notification on targetOS, or ok=false when the platform is unsupported.
// title and message must already be sanitized. No value is ever parsed as
// script or as an option:
//   - darwin: AppleScript "on run argv" receives the values as argv items
//     after "--";
//   - linux: notify-send after "--", body markup-escaped;
//   - windows: -EncodedCommand with the values embedded as base64 literals.
func NotificationCommand(targetOS, title, message string) (name string, args []string, ok bool) {
	switch targetOS {
	case "darwin":
		return "osascript", []string{
			"-e", "on run argv",
			"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
			"-e", "end run",
			// osascript parses options until "--"; without it a title
			// such as "-e" or "-i" would be read as an option.
			"--",
			title,
			message,
		}, true
	case "linux":
		// The summary is plain text per the spec; only the body is markup.
		return "notify-send", []string{
			"-a", "halpradio", "-u", "normal", "-h", "boolean:suppress-sound:true",
			"--", title, escapeNotificationMarkup(message),
		}, true
	case "windows":
		return "powershell", []string{
			"-NoProfile", "-NonInteractive", "-EncodedCommand",
			EncodePowerShellCommand(PowerShellToastScript(title, message)),
		}, true
	}
	return "", nil, false
}

func dispatchOSNotification(runner CommandRunner, targetOS, title, message string) {
	if runner == nil {
		runner = defaultCommandRunner
	}

	// Strip control/format runes so the text is safe for any daemon.
	title = sanitizeNotificationString(title, 256)
	message = sanitizeNotificationString(message, 1024)

	name, args, ok := NotificationCommand(targetOS, title, message)
	if !ok {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	_ = runner(ctx, name, args...)
}
