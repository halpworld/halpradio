package lyrics

import (
	"reflect"
	"testing"
	"time"
)

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func TestParseLRC(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []Line
	}{
		{
			name: "empty input",
			raw:  "",
			want: nil,
		},
		{
			name: "whitespace only",
			raw:  "   \n\t\n",
			want: nil,
		},
		{
			name: "centiseconds",
			raw:  "[00:12.34]Hello\n[01:02.50]World",
			want: []Line{
				{At: 12*time.Second + ms(340), Text: "Hello"},
				{At: 62*time.Second + ms(500), Text: "World"},
			},
		},
		{
			name: "milliseconds",
			raw:  "[00:01.001]One\n[00:02.010]Two",
			want: []Line{
				{At: time.Second + ms(1), Text: "One"},
				{At: 2*time.Second + ms(10), Text: "Two"},
			},
		},
		{
			name: "tenths",
			raw:  "[00:05.5]Half",
			want: []Line{{At: 5*time.Second + ms(500), Text: "Half"}},
		},
		{
			name: "no fraction",
			raw:  "[02:03]Plain",
			want: []Line{{At: 2*time.Minute + 3*time.Second, Text: "Plain"}},
		},
		{
			name: "colon fraction separator",
			raw:  "[00:09:25]Odd",
			want: []Line{{At: 9*time.Second + ms(250), Text: "Odd"}},
		},
		{
			name: "multiple timestamps on one line",
			raw:  "[00:10.00][00:40.00] Chorus",
			want: []Line{
				{At: 10 * time.Second, Text: "Chorus"},
				{At: 40 * time.Second, Text: "Chorus"},
			},
		},
		{
			name: "metadata tags skipped",
			raw: "[ar:Daft Punk]\n[ti:Voyager]\n[al:Discovery]\n[by:someone]\n" +
				"[length:03:47]\n[00:00.00]Intro",
			want: []Line{{At: 0, Text: "Intro"}},
		},
		{
			name: "untimed filler skipped",
			raw:  "Lyrics by nobody\n[00:03.00]Line",
			want: []Line{{At: 3 * time.Second, Text: "Line"}},
		},
		{
			name: "positive offset delays lines",
			raw:  "[offset:+500]\n[00:10.00]A\n[00:20.00]B",
			want: []Line{
				{At: 10*time.Second + ms(500), Text: "A"},
				{At: 20*time.Second + ms(500), Text: "B"},
			},
		},
		{
			name: "negative offset advances and clamps at zero",
			raw:  "[offset:-2000]\n[00:01.00]A\n[00:10.00]B",
			want: []Line{
				{At: 0, Text: "A"},
				{At: 8 * time.Second, Text: "B"},
			},
		},
		{
			name: "trailing offset tag still applies",
			raw:  "[00:10.00]A\n[offset:1000]",
			want: []Line{{At: 11 * time.Second, Text: "A"}},
		},
		{
			name: "unsorted input is sorted ascending",
			raw:  "[00:30.00]Third\n[00:10.00]First\n[00:20.00]Second",
			want: []Line{
				{At: 10 * time.Second, Text: "First"},
				{At: 20 * time.Second, Text: "Second"},
				{At: 30 * time.Second, Text: "Third"},
			},
		},
		{
			name: "blank timed lines are kept as gaps",
			raw:  "[00:00.00]\n[00:05.00]Sing",
			want: []Line{
				{At: 0, Text: ""},
				{At: 5 * time.Second, Text: "Sing"},
			},
		},
		{
			name: "enhanced word timings stripped",
			raw:  "[00:12.00]<00:12.00>Hey <00:12.50>there",
			want: []Line{{At: 12 * time.Second, Text: "Hey there"}},
		},
		{
			name: "windows line endings",
			raw:  "[00:01.00]A\r\n[00:02.00]B\r\n",
			want: []Line{
				{At: time.Second, Text: "A"},
				{At: 2 * time.Second, Text: "B"},
			},
		},
		{
			name: "long tracks over 99 minutes",
			raw:  "[100:07.00]Late",
			want: []Line{{At: 100*time.Minute + 7*time.Second, Text: "Late"}},
		},
		{
			name: "garbage without timestamps",
			raw:  "not an lrc file at all\nsecond line",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseLRC(tt.raw)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseLRC(%q)\n got: %v\nwant: %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestParseLRCSortedAndMonotonic(t *testing.T) {
	raw := "[00:40.00]d\n[00:10.00]a\n[00:10.00]b\n[00:30.00]c"
	got := ParseLRC(raw)
	if len(got) != 4 {
		t.Fatalf("expected 4 lines, got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].At < got[i-1].At {
			t.Fatalf("lines not sorted at %d: %v", i, got)
		}
	}
	// Stable sort keeps the original order of equal timestamps.
	if got[0].Text != "a" || got[1].Text != "b" {
		t.Errorf("equal timestamps not stably ordered: %v", got)
	}
}

func TestPlainToLines(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []Line
	}{
		{name: "empty", raw: "", want: nil},
		{name: "blank", raw: "\n\n \n", want: nil},
		{
			name: "trims surrounding blanks and keeps inner blank",
			raw:  "\n\nfirst\n\nsecond\n\n",
			want: []Line{{Text: "first"}, {Text: ""}, {Text: "second"}},
		},
		{
			name: "crlf",
			raw:  "one\r\ntwo",
			want: []Line{{Text: "one"}, {Text: "two"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := plainToLines(tt.raw)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("plainToLines(%q)\n got: %v\nwant: %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestFractionToDuration(t *testing.T) {
	tests := []struct {
		frac string
		want time.Duration
	}{
		{"", 0},
		{"5", 500 * time.Millisecond},
		{"05", 50 * time.Millisecond},
		{"50", 500 * time.Millisecond},
		{"123", 123 * time.Millisecond},
		{"007", 7 * time.Millisecond},
	}
	for _, tt := range tests {
		if got := fractionToDuration(tt.frac); got != tt.want {
			t.Errorf("fractionToDuration(%q) = %v, want %v", tt.frac, got, tt.want)
		}
	}
}
