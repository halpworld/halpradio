//go:build !windows

package desktop

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func shortTempDir(t *testing.T) string {
	t.Helper()
	// Unix socket paths are limited to ~104 bytes on macOS; keep them short.
	dir, err := os.MkdirTemp("", "hr")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

func noopHandler(MediaAction) (*PlaybackInfo, error) { return &PlaybackInfo{}, nil }

func TestIPC_SecondInstanceDoesNotHijackLiveSocket(t *testing.T) {
	sock := filepath.Join(shortTempDir(t), "ipc.sock")
	first, err := StartIPCServer(sock, noopHandler)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	if _, err := StartIPCServer(sock, noopHandler); !errors.Is(err, ErrIPCAlreadyRunning) {
		t.Fatalf("second start: err = %v, want ErrIPCAlreadyRunning", err)
	}
	// The first instance is still reachable.
	if resp, err := SendIPCCommand(sock, "status"); err != nil || !resp.Success {
		t.Fatalf("first instance unreachable after second start: %v %+v", err, resp)
	}
}

func TestIPC_StaleSocketIsReplaced(t *testing.T) {
	sock := filepath.Join(shortTempDir(t), "ipc.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = l.Close() // leaves a dead socket file behind

	s, err := StartIPCServer(sock, noopHandler)
	if err != nil {
		t.Fatalf("stale socket not replaced: %v", err)
	}
	defer s.Close()
	if _, err := SendIPCCommand(sock, "status"); err != nil {
		t.Fatal(err)
	}
}

func TestIPC_RefusesToDeleteNonSocket(t *testing.T) {
	sock := filepath.Join(shortTempDir(t), "ipc.sock")
	if err := os.WriteFile(sock, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := StartIPCServer(sock, noopHandler); err == nil {
		s.Close()
		t.Fatal("expected refusal for a regular file at the socket path")
	}
	if b, _ := os.ReadFile(sock); string(b) != "keep me" {
		t.Fatal("regular file was modified or removed")
	}
}

func TestIPC_SocketModeAndDirectoryChecks(t *testing.T) {
	dir := shortTempDir(t)
	sock := filepath.Join(dir, "ipc.sock")
	s, err := StartIPCServer(sock, noopHandler)
	if err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(sock)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Errorf("socket mode = %v, want 0600", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temporary bind directory left behind: %v", entries)
	}
	s.Close()

	if err := os.Chmod(dir, 0777); err != nil {
		t.Fatal(err)
	}
	if s, err := StartIPCServer(sock, noopHandler); err == nil {
		s.Close()
		t.Fatal("expected refusal for a world-writable socket directory")
	}
}

func TestIPC_CloseDoesNotRemoveForeignSocket(t *testing.T) {
	sock := filepath.Join(shortTempDir(t), "ipc.sock")
	s, err := StartIPCServer(sock, noopHandler)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate another instance replacing the path.
	_ = os.Remove(sock)
	if err := os.WriteFile(sock, nil, 0600); err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	if _, err := os.Lstat(sock); err != nil {
		t.Fatal("Close removed a path it did not create")
	}
}

func TestSanitizeString_C1BidiZeroWidth(t *testing.T) {
	got := SanitizeString("a\u009b31mb‮​c⁦d", 64)
	for _, r := range got {
		if r == 0x9b || r == 0x202e || r == 0x200b || r == 0x2066 {
			t.Fatalf("unsafe rune %U survived: %q", r, got)
		}
	}
}
