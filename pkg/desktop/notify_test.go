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
		station   string
		track     string
		wantTitle string
		wantBody  string
	}{
		{
			station:   "SomaFM Secret Agent",
			track:     "James Bond Theme",
			wantTitle: "📻 halpradio — SomaFM Secret Agent",
			wantBody:  "🎶 James Bond Theme",
		},
		{
			station:   "",
			track:     "Ambient Song",
			wantTitle: "📻 halpradio",
			wantBody:  "🎶 Ambient Song",
		},
		{
			station:   "Lofi Girl",
			track:     "",
			wantTitle: "📻 halpradio — Lofi Girl",
			wantBody:  "Now Playing",
		},
	}

	for _, tt := range tests {
		title, body := FormatSongNotification(tt.station, tt.track)
		if title != tt.wantTitle {
			t.Errorf("FormatSongNotification(%q, %q) title = %q; want %q", tt.station, tt.track, title, tt.wantTitle)
		}
		if body != tt.wantBody {
			t.Errorf("FormatSongNotification(%q, %q) body = %q; want %q", tt.station, tt.track, body, tt.wantBody)
		}
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

func TestDispatchOSNotification_Platforms(t *testing.T) {
	platforms := []string{"darwin", "linux", "windows", "freebsd"}

	for _, p := range platforms {
		t.Run(p, func(t *testing.T) {
			var calledName string
			var calledArgs []string

			mockRunner := func(ctx context.Context, name string, args ...string) error {
				calledName = name
				calledArgs = args
				return nil
			}

			dispatchOSNotification(mockRunner, p, "Title", "Message")

			switch p {
			case "darwin":
				if calledName != "osascript" {
					t.Errorf("darwin expected osascript, got %q", calledName)
				}
			case "linux":
				if calledName != "notify-send" {
					t.Errorf("linux expected notify-send, got %q", calledName)
				}
			case "windows":
				if calledName != "powershell" {
					t.Errorf("windows expected powershell, got %q", calledName)
				}
			case "freebsd":
				if calledName != "" {
					t.Errorf("unsupported OS should not execute command, got %q", calledName)
				}
			}
			_ = calledArgs
		})
	}
}

func TestDesktopNotifierNilSafe(t *testing.T) {
	var n *DesktopNotifier
	// Should not panic on nil receiver
	n.NotifySong("Station", "Track")
	n.Notify("Title", "Message")
	n.ResetDeduplication()
}
