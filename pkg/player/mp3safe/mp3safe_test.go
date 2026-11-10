package mp3safe

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/hajimehoshi/go-mp3"
)

// crashMP3 is a 245-byte reduction of the fuzzer reproducer for PLY-2
// (go-mp3 v0.3.4: "index out of range [38] with length 38" in maindata.go).
const crashMP3Hex = "" +
	"49443304000000000023545353450000000f0000034c61766635372e37312e3130300000000000000000000000e874bc" +
	"8890ba27e1fd3a17ee08513f15b78a574ae65d55680477f572573ccc11550e3305acb0881e4c9fcbfff36288ff2b1c16" +
	"1800d24cdc02f9c160e619421d364e16d043198ced4197ea226ab0e832c8a27ea5a530b04ba5c1d6baed849bf3e98126" +
	"4a6b08807306c5d3249093746430200c420718da1399fc6319b2329b6eaca98c3a63b0200d034d2c4c4c9b14cb04f9a0" +
	"f21f391a0634b819b481d390d2f9b8e4062814a0019706f701e04f209a8b8665b19800e0e16602cf8120f6d03cb2fff3" +
	"62c4eb22b3" +
	""

func crashMP3(t *testing.T) []byte {
	t.Helper()
	b, err := hex.DecodeString(crashMP3Hex)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReproPanicsUnwrapped(t *testing.T) {
	// Guards the regression test: the raw decoder must still panic on this
	// input, otherwise the test below proves nothing.
	panicked := func() (p bool) {
		defer func() { p = recover() != nil }()
		d, err := mp3.NewDecoder(bytes.NewReader(crashMP3(t)))
		if err != nil {
			return false
		}
		_, _ = io.Copy(io.Discard, d)
		return false
	}()
	if !panicked {
		t.Skip("go-mp3 no longer panics on the reproducer")
	}
}

func TestDecoderTurnsPanicIntoError(t *testing.T) {
	d, err := NewDecoder(bytes.NewReader(crashMP3(t)))
	if err != nil {
		if !strings.Contains(err.Error(), "mp3 decode panic") {
			t.Fatalf("unexpected construction error: %v", err)
		}
		return
	}
	_, err = io.Copy(io.Discard, d)
	if err == nil || !strings.Contains(err.Error(), "mp3 decode panic") {
		t.Fatalf("expected a decode-panic error, got %v", err)
	}
	if d.Err() == nil {
		t.Fatal("Err() should report the recovered panic")
	}
	// Subsequent reads keep failing instead of touching the broken decoder.
	if _, err2 := d.Read(make([]byte, 64)); err2 == nil {
		t.Fatal("read after failure succeeded")
	}
}

func TestHugeID3HeaderRejectedWithoutAllocation(t *testing.T) {
	// "ID3" v4, flags 0, size bytes 7F 7F 7F 7F (~256 MiB), then nothing.
	hdr := []byte{'I', 'D', '3', 4, 0, 0, 0x7f, 0x7f, 0x7f, 0x7f}
	if _, err := NewDecoder(bytes.NewReader(hdr)); !errors.Is(err, ErrID3TooLarge) {
		t.Fatalf("err = %v, want ErrID3TooLarge", err)
	}
	// The unmasked FF FF FF FF form from the report is rejected as malformed.
	hdr = []byte{'I', 'D', '3', 4, 0, 0, 0xff, 0xff, 0xff, 0xff}
	if _, err := NewDecoder(bytes.NewReader(hdr)); !errors.Is(err, ErrMalformedID3) {
		t.Fatalf("err = %v, want ErrMalformedID3", err)
	}
}

func TestSmallID3TagIsSkipped(t *testing.T) {
	data := crashMP3(t)
	// The reproducer starts with a valid 0x23-byte ID3v2 tag (45 bytes with
	// its header); exactly that much must be discarded.
	br := bufioReader(data)
	if err := skipID3v2(br); err != nil {
		t.Fatal(err)
	}
	rest, _ := io.ReadAll(br)
	if len(rest) != len(data)-45 {
		t.Fatalf("skipped %d bytes, want 45", len(data)-len(rest))
	}
}

func bufioReader(b []byte) *bufio.Reader { return bufio.NewReader(bytes.NewReader(b)) }
