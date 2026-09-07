// Package debuglog provides opt-in diagnostic logging for halpradio.
//
// halpradio runs in the alternate screen buffer, so anything written to stdout
// or stderr is invisible (or corrupts the TUI). Diagnostics therefore go to a
// file the user can attach to a bug report:
//
//	halpradio --debug
//	# reproduce, quit, then attach ~/.config/halpradio/debug.log
//
// Logging is disabled by default and every call is a cheap no-op until Init
// succeeds, so instrumentation can be sprinkled across hot paths freely.
package debuglog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// MaxSize is the size at which the log file is rotated to <name>.old. A frozen
// UI can emit a lot of ticks, and a bug report attachment should stay postable.
const MaxSize = 2 << 20 // 2 MiB

var (
	mu      sync.Mutex
	out     io.WriteCloser
	path    string
	written int64
	start   time.Time
)

// Enabled reports whether diagnostic logging is currently active.
func Enabled() bool {
	mu.Lock()
	defer mu.Unlock()
	return out != nil
}

// Path returns the file diagnostics are being written to, or "" when disabled.
func Path() string {
	mu.Lock()
	defer mu.Unlock()
	return path
}

// EnvEnabled reports whether HALPRADIO_DEBUG requests logging. Any value other
// than "", "0", "false" or "no" turns it on.
func EnvEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("HALPRADIO_DEBUG"))) {
	case "", "0", "false", "no", "off":
		return false
	}
	return true
}

// Init opens logFile for appending and starts recording diagnostics. An empty
// logFile selects the default location inside the halpradio config directory.
// Init is idempotent: a second call while logging is active is a no-op.
func Init(logFile string) (string, error) {
	mu.Lock()
	defer mu.Unlock()

	if out != nil {
		return path, nil
	}
	if logFile == "" {
		return "", fmt.Errorf("debuglog: no log file path given")
	}
	if dir := filepath.Dir(logFile); dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return "", fmt.Errorf("debuglog: create %s: %w", dir, err)
		}
	}
	rotateLocked(logFile)

	// 0600: logs carry station URLs and local paths; keep them to the owner.
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return "", fmt.Errorf("debuglog: open %s: %w", logFile, err)
	}
	if info, err := f.Stat(); err == nil {
		written = info.Size()
	}
	out = f
	path = logFile
	start = time.Now()
	return path, nil
}

// rotateLocked moves an oversized log aside so a session always starts small.
func rotateLocked(logFile string) {
	info, err := os.Stat(logFile)
	if err != nil || info.Size() < MaxSize {
		return
	}
	_ = os.Rename(logFile, logFile+".old")
}

// Close flushes and closes the log file.
func Close() {
	mu.Lock()
	defer mu.Unlock()
	if out == nil {
		return
	}
	_, _ = fmt.Fprintf(out, "%s [session] closing after %s\n", time.Now().Format(stamp), time.Since(start).Round(time.Millisecond))
	_ = out.Close()
	out = nil
	path = ""
	written = 0
}

const stamp = "15:04:05.000"

// Logf writes one timestamped line under the given subsystem tag.
func Logf(subsystem, format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()
	if out == nil {
		return
	}
	msg := strings.TrimRight(fmt.Sprintf(format, args...), "\n")
	n, _ := fmt.Fprintf(out, "%s [%s] %s\n", time.Now().Format(stamp), subsystem, msg)
	written += int64(n)
	if written >= MaxSize {
		// Stop rather than fill the disk during a freeze; the tail is the
		// least useful part of a report anyway.
		_, _ = fmt.Fprintf(out, "%s [debuglog] size limit reached, logging stopped\n", time.Now().Format(stamp))
		_ = out.Close()
		out = nil
	}
}

// Header records the environment details every bug report needs. Values are
// deliberately limited to what identifies a platform or terminal quirk.
func Header(version string, extra map[string]string) {
	if !Enabled() {
		return
	}
	Logf("session", "halpradio v%s starting", version)
	Logf("session", "go=%s os=%s arch=%s cpus=%d", runtime.Version(), runtime.GOOS, runtime.GOARCH, runtime.NumCPU())
	Logf("session", "TERM=%q COLORTERM=%q TERM_PROGRAM=%q", os.Getenv("TERM"), os.Getenv("COLORTERM"), os.Getenv("TERM_PROGRAM"))
	Logf("session", "XDG_SESSION_TYPE=%q DBUS_SESSION_BUS_ADDRESS_set=%t WAYLAND_DISPLAY=%q",
		os.Getenv("XDG_SESSION_TYPE"),
		os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "",
		os.Getenv("WAYLAND_DISPLAY"),
	)
	for _, k := range sortedKeys(extra) {
		Logf("session", "%s=%s", k, extra[k])
	}
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// Small maps; insertion sort keeps this dependency-free and stable.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j] < keys[j-1]; j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}
