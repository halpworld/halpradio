package debuglog

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInitRefusesSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	Close()
	dir := t.TempDir()
	target := filepath.Join(dir, "victim")
	if err := os.WriteFile(target, []byte("original\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "debug.log")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(link); err == nil {
		Close()
		t.Fatal("Init followed a symlink")
	}
	if b, _ := os.ReadFile(target); string(b) != "original\n" {
		t.Fatalf("symlink target was modified: %q", b)
	}
}

func TestInitTightensExistingFileMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes only")
	}
	Close()
	p := filepath.Join(t.TempDir(), "debug.log")
	if err := os.WriteFile(p, nil, 0o666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, 0o666); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(p); err != nil {
		t.Fatal(err)
	}
	defer Close()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestInitRefusesNonRegular(t *testing.T) {
	Close()
	if _, err := Init(t.TempDir()); err == nil {
		Close()
		t.Fatal("Init accepted a directory")
	}
}

func TestRedactURL(t *testing.T) {
	cases := map[string]string{
		"https://user:secret@stream.example/live.mp3?token=abc#frag": "https://stream.example/live.mp3",
		"http://ice.example:8000/stream":                             "http://ice.example:8000/stream",
		"":                                                           "",
		"not a url %zz":                                              RedactedURL,
	}
	for in, want := range cases {
		if got := RedactURL(in); got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", in, got, want)
		}
	}
	args := RedactArgs([]string{"mpv", "--no-video", "--", "https://u:p@h/s?k=v"})
	if strings.Contains(strings.Join(args, " "), "p@") || strings.Contains(strings.Join(args, " "), "k=v") {
		t.Errorf("RedactArgs leaked secrets: %v", args)
	}
	txt := RedactText(`Get "https://u:pw@h.example/s?key=SECRET": dial tcp: refused`)
	if strings.Contains(txt, "SECRET") || strings.Contains(txt, "pw@") {
		t.Errorf("RedactText leaked secrets: %q", txt)
	}
}

func TestRedactKey(t *testing.T) {
	if got := RedactKey("sk-live-123", true, false); strings.Contains(got, "sk-") {
		t.Errorf("text key leaked: %q", got)
	}
	if got := RedactKey("pasted secret", false, true); strings.Contains(got, "secret") {
		t.Errorf("paste leaked: %q", got)
	}
	if got := RedactKey("ctrl+c", false, false); got != "ctrl+c" {
		t.Errorf("named key changed: %q", got)
	}
}

// RR-E-7: a name hard-linked to another file must not be appended to,
// chmodded or rotated.
func TestInitRefusesHardLink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX link counts only")
	}
	Close()
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	if err := os.WriteFile(victim, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "debug.log")
	if err := os.Link(victim, link); err != nil {
		t.Skipf("hard links unsupported here: %v", err)
	}
	if _, err := Init(link); err == nil {
		Logf("test", "should not be written")
		Close()
		t.Fatal("Init accepted a hard-linked log file")
	}
	b, _ := os.ReadFile(victim)
	fi, _ := os.Stat(victim)
	if string(b) != "original\n" || fi.Mode().Perm() != 0o644 {
		t.Fatalf("victim changed: %q mode %v", b, fi.Mode().Perm())
	}
}

// RR-E-8: text without a URL still has its control characters collapsed,
// so a remote error cannot forge log lines or carry terminal escapes.
func TestRedactTextCollapsesControlsWithoutURL(t *testing.T) {
	for _, in := range []string{
		"bad frame\n2026-01-01 [player] forged entry",
		"title \x1b]52;c;ZXZpbA==\x07 end",
		"carriage\rreturn‮bidi\u0085nel",
		"http://u:p@h.example/x?t=1\nforged",
	} {
		out := RedactText(in)
		if strings.ContainsAny(out, "\n\r\x1b\x07‮\u0085") {
			t.Errorf("RedactText(%q) = %q keeps control characters", in, out)
		}
	}
	if got := RedactText("plain message"); got != "plain message" {
		t.Errorf("plain text changed: %q", got)
	}
}
