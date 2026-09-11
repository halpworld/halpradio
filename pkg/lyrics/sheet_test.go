package lyrics

import (
	"reflect"
	"testing"
	"time"
)

func syncedSheet() *Sheet {
	return &Sheet{
		Artist:   "Daft Punk",
		Title:    "Voyager",
		Duration: 60 * time.Second,
		Synced:   true,
		Source:   SourceLRCLib,
		Lines: []Line{
			{At: 10 * time.Second, Text: "first"},
			{At: 20 * time.Second, Text: "second"},
			{At: 30 * time.Second, Text: "third"},
		},
	}
}

func TestSheetActiveIndex(t *testing.T) {
	synced := syncedSheet()
	unsynced := &Sheet{Lines: []Line{{Text: "a"}, {Text: "b"}}}
	empty := &Sheet{Synced: true}

	tests := []struct {
		name    string
		sheet   *Sheet
		elapsed time.Duration
		want    int
	}{
		{name: "nil sheet", sheet: nil, elapsed: time.Second, want: -1},
		{name: "unsynced sheet", sheet: unsynced, elapsed: time.Second, want: -1},
		{name: "no lines", sheet: empty, elapsed: time.Second, want: -1},
		{name: "before first line", sheet: synced, elapsed: 0, want: -1},
		{name: "just before first line", sheet: synced, elapsed: 9999 * time.Millisecond, want: -1},
		{name: "exactly on first line", sheet: synced, elapsed: 10 * time.Second, want: 0},
		{name: "inside first line", sheet: synced, elapsed: 15 * time.Second, want: 0},
		{name: "exactly on second line", sheet: synced, elapsed: 20 * time.Second, want: 1},
		{name: "inside last line", sheet: synced, elapsed: 35 * time.Second, want: 2},
		{name: "far past the end", sheet: synced, elapsed: time.Hour, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sheet.ActiveIndex(tt.elapsed); got != tt.want {
				t.Errorf("ActiveIndex(%v) = %d, want %d", tt.elapsed, got, tt.want)
			}
		})
	}
}

func TestSheetActiveIndexMatchesLinearScan(t *testing.T) {
	sheet := &Sheet{Synced: true}
	for i := 0; i < 50; i++ {
		sheet.Lines = append(sheet.Lines, Line{At: time.Duration(i) * 3 * time.Second})
	}

	for elapsed := time.Duration(0); elapsed < 160*time.Second; elapsed += 500 * time.Millisecond {
		want := -1
		for i, l := range sheet.Lines {
			if l.At <= elapsed {
				want = i
			}
		}
		if got := sheet.ActiveIndex(elapsed); got != want {
			t.Fatalf("ActiveIndex(%v) = %d, want %d", elapsed, got, want)
		}
	}
}

func TestSheetActiveIndexDuplicateTimestamps(t *testing.T) {
	sheet := &Sheet{
		Synced: true,
		Lines: []Line{
			{At: 5 * time.Second, Text: "a"},
			{At: 5 * time.Second, Text: "b"},
			{At: 9 * time.Second, Text: "c"},
		},
	}
	if got := sheet.ActiveIndex(6 * time.Second); got != 1 {
		t.Errorf("ActiveIndex = %d, want 1 (last line sharing the timestamp)", got)
	}
}

func TestSheetProgress(t *testing.T) {
	synced := syncedSheet()
	noDuration := syncedSheet()
	noDuration.Duration = 0
	unsynced := &Sheet{Lines: []Line{{Text: "a"}}}

	tests := []struct {
		name    string
		sheet   *Sheet
		index   int
		elapsed time.Duration
		want    float64
	}{
		{name: "nil sheet", sheet: nil, index: 0, elapsed: time.Second, want: 0},
		{name: "unsynced", sheet: unsynced, index: 0, elapsed: time.Second, want: 0},
		{name: "negative index", sheet: synced, index: -1, elapsed: 15 * time.Second, want: 0},
		{name: "index past end", sheet: synced, index: 99, elapsed: 15 * time.Second, want: 0},
		{name: "before line start", sheet: synced, index: 1, elapsed: 15 * time.Second, want: 0},
		{name: "at line start", sheet: synced, index: 0, elapsed: 10 * time.Second, want: 0},
		{name: "half way", sheet: synced, index: 0, elapsed: 15 * time.Second, want: 0.5},
		{name: "three quarters", sheet: synced, index: 1, elapsed: 27500 * time.Millisecond, want: 0.75},
		{name: "past line end clamps to one", sheet: synced, index: 0, elapsed: 25 * time.Second, want: 1},
		{name: "last line uses sheet duration", sheet: synced, index: 2, elapsed: 45 * time.Second, want: 0.5},
		{name: "last line without duration is unknown", sheet: noDuration, index: 2, elapsed: 45 * time.Second, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.sheet.Progress(tt.index, tt.elapsed)
			if diff := got - tt.want; diff > 1e-9 || diff < -1e-9 {
				t.Errorf("Progress(%d, %v) = %v, want %v", tt.index, tt.elapsed, got, tt.want)
			}
		})
	}
}

func TestSheetProgressStaysInRange(t *testing.T) {
	sheet := syncedSheet()
	for elapsed := time.Duration(0); elapsed < 90*time.Second; elapsed += time.Second {
		for i := range sheet.Lines {
			p := sheet.Progress(i, elapsed)
			if p < 0 || p > 1 {
				t.Fatalf("Progress(%d, %v) = %v out of range", i, elapsed, p)
			}
		}
	}
}

func TestSheetProgressZeroLengthLine(t *testing.T) {
	sheet := &Sheet{
		Synced: true,
		Lines: []Line{
			{At: 5 * time.Second, Text: "a"},
			{At: 5 * time.Second, Text: "b"},
		},
		Duration: 30 * time.Second,
	}
	if got := sheet.Progress(0, 5*time.Second); got != 0 {
		t.Errorf("Progress on zero-length line = %v, want 0", got)
	}
}

func TestSheetIsEmpty(t *testing.T) {
	tests := []struct {
		name  string
		sheet *Sheet
		want  bool
	}{
		{name: "nil", sheet: nil, want: true},
		{name: "no lines", sheet: &Sheet{}, want: true},
		{name: "only blank lines", sheet: &Sheet{Lines: []Line{{Text: ""}, {Text: "  "}}}, want: true},
		{name: "has text", sheet: &Sheet{Lines: []Line{{Text: ""}, {Text: "hi"}}}, want: false},
		{name: "instrumental marker", sheet: &Sheet{Lines: []Line{{Text: InstrumentalMarker}}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sheet.IsEmpty(); got != tt.want {
				t.Errorf("IsEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSheetPlainLines(t *testing.T) {
	if got := (*Sheet)(nil).PlainLines(); got != nil {
		t.Errorf("nil sheet PlainLines = %v, want nil", got)
	}
	if got := (&Sheet{}).PlainLines(); got != nil {
		t.Errorf("empty sheet PlainLines = %v, want nil", got)
	}

	sheet := syncedSheet()
	want := []string{"first", "second", "third"}
	if got := sheet.PlainLines(); !reflect.DeepEqual(got, want) {
		t.Errorf("PlainLines = %v, want %v", got, want)
	}
}

func TestSheetClone(t *testing.T) {
	if (*Sheet)(nil).clone() != nil {
		t.Fatal("clone of nil sheet should be nil")
	}

	orig := syncedSheet()
	copied := orig.clone()
	copied.Lines[0].Text = "mutated"
	copied.Title = "other"

	if orig.Lines[0].Text != "first" {
		t.Error("clone shares the Lines backing array")
	}
	if orig.Title != "Voyager" {
		t.Error("clone shares scalar fields")
	}
}
