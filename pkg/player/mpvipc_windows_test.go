//go:build windows

package player

import (
	"strings"
	"testing"
)

func TestMPVPipeNameIsUnpredictable(t *testing.T) {
	a, err := newMPVIPCEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	b, err := newMPVIPCEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	if a.Addr == b.Addr {
		t.Fatal("pipe names repeat")
	}
	if !strings.HasPrefix(a.Addr, `\\.\pipe\halpradio-mpv-`) {
		t.Fatalf("unexpected pipe name %q", a.Addr)
	}
	// pid + 32 hex chars of randomness.
	suffix := a.Addr[strings.LastIndex(a.Addr, "-")+1:]
	if len(suffix) != 32 {
		t.Fatalf("random suffix %q is not 128 bits", suffix)
	}
}
