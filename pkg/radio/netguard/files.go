package netguard

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// WriteFileAtomic writes data to a temporary file in path's directory, syncs
// it, and renames it over path, so a crash or a full disk never leaves a
// truncated file behind. The file ends up with mode perm (0600 for
// everything halpradio persists from remote sources or user lists).
func WriteFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(name, perm); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(name, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// PruneDir bounds a cache directory: it deletes regular files whose names
// end in one of suffixes and that are older than maxAge (by modification
// time), then deletes the oldest remaining ones until their total size is at
// most maxBytes. Stray temporary files older than an hour are removed too.
// A zero limit disables that check. Errors are ignored: pruning is
// best-effort housekeeping.
func PruneDir(dir string, maxAge time.Duration, maxBytes int64, suffixes ...string) {
	if dir == "" {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type file struct {
		path string
		size int64
		mod  time.Time
	}
	now := time.Now()
	var keep []file
	var total int64
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		name := e.Name()
		info, err := e.Info()
		if err != nil {
			continue
		}
		p := filepath.Join(dir, name)
		if strings.HasSuffix(name, ".tmp") {
			if now.Sub(info.ModTime()) > time.Hour {
				_ = os.Remove(p)
			}
			continue
		}
		matched := len(suffixes) == 0
		for _, s := range suffixes {
			if strings.HasSuffix(name, s) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		if maxAge > 0 && now.Sub(info.ModTime()) > maxAge {
			_ = os.Remove(p)
			continue
		}
		keep = append(keep, file{p, info.Size(), info.ModTime()})
		total += info.Size()
	}
	if maxBytes <= 0 || total <= maxBytes {
		return
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i].mod.Before(keep[j].mod) })
	for _, f := range keep {
		if total <= maxBytes {
			break
		}
		if os.Remove(f.path) == nil {
			total -= f.size
		}
	}
}
