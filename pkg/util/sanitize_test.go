package util

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func TestSanitizeDisplay(t *testing.T) {
	tests := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"plain", "Radio Paradise", 0, "Radio Paradise"},
		{"unicode kept", "Café del Mar ☕ 東京", 0, "Café del Mar ☕ 東京"},
		{"osc52 clipboard write", "Cool FM \x1b]52;c;ZWNobyBwd25lZA==\x07", 0, "Cool FM ]52;c;ZWNobyBwd25lZA=="},
		{"csi clear screen", "\x1b[2Jhello", 0, "[2Jhello"},
		{"c1 csi", "a\u009b31mb", 0, "a31mb"},
		{"c1 osc and st", "a\u009d0;title\u009cb", 0, "a0;titleb"},
		{"bidi override", "abc\u202egnp.exe", 0, "abcgnp.exe"},
		{"bidi isolates", "\u2066a\u2069", 0, "a"},
		{"zero width and bom", "a\u200b\u200d\ufeffb", 0, "ab"},
		{"del and backspace", "ab\x7f\x08c", 0, "abc"},
		{"newlines collapse", "line1\r\nline2\n\n\tline3", 0, "line1 line2 line3"},
		{"trim", "  \t padded \n ", 0, "padded"},
		{"only controls", "\x1b\x07\x00", 0, ""},
		{"invalid utf8", "a\xffb", 0, "a\ufffdb"},
		{"cap", "abcdefgh", 3, "abc"},
		{"cap does not end on space", "ab cd", 3, "ab"},
		{"cap counts runes not bytes", "日本語テキスト", 3, "日本語"},
		{"empty", "", 10, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeDisplay(tt.in, tt.max)
			if got != tt.want {
				t.Errorf("SanitizeDisplay(%q, %d) = %q, want %q", tt.in, tt.max, got, tt.want)
			}
			assertPrintable(t, got)
		})
	}
}

func TestSanitizeDisplayIdempotentAndBounded(t *testing.T) {
	in := strings.Repeat("x\x1b[31m \u202e", 50_000)
	got := SanitizeDisplay(in, 120)
	if n := utf8.RuneCountInString(got); n > 120 {
		t.Fatalf("got %d runes, want <= 120", n)
	}
	if again := SanitizeDisplay(got, 120); again != got {
		t.Fatalf("not idempotent: %q != %q", again, got)
	}
	assertPrintable(t, got)
}

func TestSanitizeMultiline(t *testing.T) {
	in := "first \x1b[2Jline\r\nsecond\u009b line\rthird\n\nfifth"
	got := SanitizeMultiline(in, 0, 0)
	want := "first [2Jline\nsecond line\nthird\n\nfifth"
	if got != want {
		t.Fatalf("SanitizeMultiline = %q, want %q", got, want)
	}

	if got := SanitizeMultiline("a\nb\nc\nd", 0, 2); got != "a\nb" {
		t.Errorf("line cap: got %q, want %q", got, "a\nb")
	}
	if got := SanitizeMultiline("abcdef\nghijkl", 3, 0); got != "abc\nghi" {
		t.Errorf("rune cap: got %q, want %q", got, "abc\nghi")
	}
	if got := SanitizeMultiline("", 10, 10); got != "" {
		t.Errorf("empty: got %q", got)
	}
	for _, line := range strings.Split(SanitizeMultiline(in, 0, 0), "\n") {
		assertPrintable(t, line)
	}
}

func TestIsDisplaySafe(t *testing.T) {
	safe := []string{"", "Weather Plugin", "api.example.com", "Café ☕"}
	for _, s := range safe {
		if !IsDisplaySafe(s) {
			t.Errorf("IsDisplaySafe(%q) = false, want true", s)
		}
	}
	unsafe := []string{"a\x1b[2K", "a\u009bb", "a\u202eb", "a\nb", " leading", "trailing ", "two  spaces", "a\u200bb"}
	for _, s := range unsafe {
		if IsDisplaySafe(s) {
			t.Errorf("IsDisplaySafe(%q) = true, want false", s)
		}
	}
}

func assertPrintable(t *testing.T, s string) {
	t.Helper()
	for i, r := range s {
		if !unicode.IsPrint(r) {
			t.Errorf("non-printable rune %U at byte %d in %q", r, i, s)
		}
	}
}
