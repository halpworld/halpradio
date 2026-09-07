package debuglog

import (
	"fmt"
	"runtime"
	"sync"
	"time"
)

// StallThreshold is how long an in-flight operation may run before the
// watchdog considers it stuck and dumps every goroutine stack to the log.
//
// The Bubble Tea update loop is single-threaded: anything blocking there stops
// the whole TUI from reacting to the keyboard, so a stall dump is usually the
// difference between "it froze" and a named culprit in a bug report.
var StallThreshold = 5 * time.Second

// SlowThreshold is how long an operation may run before it is logged as slow
// even though it eventually completed.
var SlowThreshold = 250 * time.Millisecond

type watch struct {
	label   string
	started time.Time
	dumped  bool
}

var (
	watchMu      sync.Mutex
	watches      = map[uint64]*watch{}
	nextWatchID  uint64
	watchdogOnce sync.Once
)

// Watch registers an operation for stall detection and returns the function
// that marks it complete. A quiet operation (a high-frequency animation tick,
// say) is tracked but only produces output if it is slow or stalls.
//
//	defer debuglog.Watch("update KeyMsg enter", false)()
func Watch(label string, quiet bool) func() {
	if !Enabled() {
		return func() {}
	}
	watchdogOnce.Do(func() { go watchdogLoop() })

	watchMu.Lock()
	nextWatchID++
	id := nextWatchID
	w := &watch{label: label, started: time.Now()}
	watches[id] = w
	watchMu.Unlock()

	if !quiet {
		Logf("update", "→ %s", label)
	}

	return func() {
		watchMu.Lock()
		delete(watches, id)
		dumped := w.dumped
		watchMu.Unlock()

		elapsed := time.Since(w.started)
		switch {
		case dumped:
			Logf("update", "← %s recovered after %s", label, elapsed.Round(time.Millisecond))
		case elapsed >= SlowThreshold:
			Logf("update", "← %s SLOW %s", label, elapsed.Round(time.Millisecond))
		case !quiet:
			Logf("update", "← %s %s", label, elapsed.Round(time.Millisecond))
		}
	}
}

func watchdogLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if !Enabled() {
			continue
		}
		now := time.Now()
		var stalled []string
		watchMu.Lock()
		for _, w := range watches {
			if w.dumped || now.Sub(w.started) < StallThreshold {
				continue
			}
			w.dumped = true
			stalled = append(stalled, fmt.Sprintf("%s (stuck %s)", w.label, now.Sub(w.started).Round(time.Millisecond)))
		}
		watchMu.Unlock()

		for _, s := range stalled {
			Logf("watchdog", "STALLED: %s", s)
		}
		if len(stalled) > 0 {
			DumpStacks("stalled operation")
		}
	}
}

// DumpStacks writes every goroutine's stack to the log. This is the payload a
// freeze report needs: it names the exact function the UI is wedged in.
func DumpStacks(reason string) {
	if !Enabled() {
		return
	}
	buf := make([]byte, 64<<10)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			buf = buf[:n]
			break
		}
		if len(buf) >= 4<<20 {
			buf = buf[:n]
			break
		}
		buf = make([]byte, 2*len(buf))
	}
	Logf("watchdog", "goroutine dump (%s):\n%s", reason, buf)
}
