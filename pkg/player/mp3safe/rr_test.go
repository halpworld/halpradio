package mp3safe

import (
	"bytes"
	"errors"
	"io"
	"runtime"
	"sync"
	"testing"
)

// RR-E-2: eight empty tags use up maxID3v2Tags; a ninth with an unmasked
// FF FF FF FF size must be refused, not handed to go-mp3 (~512 MiB alloc).
func TestNinthID3TagIsRefused(t *testing.T) {
	var in []byte
	for i := 0; i < maxID3v2Tags; i++ {
		in = append(in, 'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0)
	}
	in = append(in, 'I', 'D', '3', 4, 0, 0, 0xff, 0xff, 0xff, 0xff)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err := NewDecoder(bytes.NewReader(in))
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrTooManyID3) {
		t.Fatalf("err = %v, want ErrTooManyID3", err)
	}
	if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 8<<20 {
		t.Fatalf("decoder allocated %d bytes for a %d-byte input", alloc, len(in))
	}
}

// A ninth tag with a valid small size is refused too: the count cap is the
// only thing standing between go-mp3 and the next header.
func TestNinthValidID3TagIsRefused(t *testing.T) {
	var in []byte
	for i := 0; i <= maxID3v2Tags; i++ {
		in = append(in, 'I', 'D', '3', 4, 0, 0, 0, 0, 0, 0)
	}
	if _, err := NewDecoder(bytes.NewReader(in)); !errors.Is(err, ErrTooManyID3) {
		t.Fatalf("err = %v, want ErrTooManyID3", err)
	}
}

// RR-E-4: Read (audio goroutine) and Err (monitor loop) run concurrently;
// go test -race must stay quiet and Err must report the panic.
func TestDecoderFailedIsRaceFree(t *testing.T) {
	var b []byte
	for i := 0; i < 3; i++ {
		f := make([]byte, 417)
		copy(f, []byte{0xff, 0xfb, 0x90, 0x00})
		b = append(b, f...)
	}
	b = append(b, crashMP3(t)[45:]...)
	d, err := NewDecoder(bytes.NewReader(b))
	if err != nil {
		t.Skipf("construction failed: %v", err)
	}
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = d.Err()
				runtime.Gosched()
			}
		}
	}()
	_, cerr := io.Copy(io.Discard, d)
	close(stop)
	wg.Wait()
	if d.Err() == nil {
		t.Fatalf("expected the crash frames to surface through Err (copy err %v)", cerr)
	}
}
