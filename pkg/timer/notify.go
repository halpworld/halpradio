package timer

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// DispatchEvent triggers terminal bell, desktop notifications, and command hooks.
func DispatchEvent(ev Event, notifyDesktop, notifyBell bool, hookScript string) {
	if notifyBell {
		RingTerminalBell()
	}

	if notifyDesktop && ev.Title != "" {
		go sendDesktopNotification(ev.Title, ev.Message)
	}

	if strings.TrimSpace(hookScript) != "" && ev.Type != EventNone && ev.Type != EventSleepFadeStart {
		go runCommandHook(hookScript, ev)
	}
}

// RingTerminalBell prints the ASCII bell character \a to standard output.
func RingTerminalBell() {
	_, _ = os.Stdout.WriteString("\a")
}

func sendDesktopNotification(title, message string) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	switch runtime.GOOS {
	case "darwin":
		// macOS AppleScript notification (silent banner without alert sound)
		script := fmt.Sprintf(`display notification %q with title %q`, message, title)
		cmd := exec.CommandContext(ctx, "osascript", "-e", script)
		_ = cmd.Run()

	case "linux":
		// Linux desktop notification via notify-send (silent hint suppresses sound on notification daemons)
		cmd := exec.CommandContext(ctx, "notify-send", "-a", "halpradio", "-u", "normal", "-h", "boolean:suppress-sound:true", title, message)
		_ = cmd.Run()

	case "windows":
		// Windows PowerShell Toast Notification (silent audio attribute suppresses chime)
		cleanTitle := strings.ReplaceAll(title, "'", "''")
		cleanMsg := strings.ReplaceAll(message, "'", "''")
		psScript := fmt.Sprintf(`[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null; $template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02); $textNodes = $template.GetElementsByTagName('text'); $textNodes.Item(0).AppendChild($template.CreateTextNode('%s')) > $null; $textNodes.Item(1).AppendChild($template.CreateTextNode('%s')) > $null; $audio = $template.CreateElement('audio'); $audio.SetAttribute('silent', 'true'); $template.DocumentElement.AppendChild($audio) > $null; $toast = [Windows.UI.Notifications.ToastNotification]::new($template); [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('halpradio').Show($toast);`, cleanTitle, cleanMsg)
		cmd := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", psScript)
		_ = cmd.Run()
	}
}

func runCommandHook(hookCmd string, ev Event) {
	hookCmd = strings.TrimSpace(hookCmd)
	if hookCmd == "" {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/c", hookCmd)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", hookCmd)
	}

	cmd.Env = append(os.Environ(),
		fmt.Sprintf("HALPRADIO_EVENT=%s", ev.Type),
		fmt.Sprintf("HALPRADIO_PHASE=%s", ev.Phase),
		fmt.Sprintf("HALPRADIO_CYCLE=%d", ev.Cycle),
		fmt.Sprintf("HALPRADIO_TOTAL_CYCLES=%d", ev.TotalCycles),
		fmt.Sprintf("HALPRADIO_TITLE=%s", ev.Title),
		fmt.Sprintf("HALPRADIO_MESSAGE=%s", ev.Message),
		fmt.Sprintf("HALPRADIO_STATION_ID=%s", ev.StationID),
		fmt.Sprintf("HALPRADIO_STATION_NAME=%s", ev.StationName),
	)

	_ = cmd.Run()
}
