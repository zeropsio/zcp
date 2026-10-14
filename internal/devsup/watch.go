//go:build linux || darwin

package devsup

import (
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// watchSettle is how long a burst of changes settles before the server
// restarts once.
const watchSettle = 200 * time.Millisecond

// debouncer turns changed repo paths into one onChange call per burst,
// dropping paths the watch does not cover or ignores.
type debouncer struct {
	w        *Watch
	onChange func(rel string)
	mu       sync.Mutex
	timer    *time.Timer
	first    string
	closed   bool
}

func (d *debouncer) changed(rel string) {
	if !d.w.covers(rel) || d.w.ignored(rel) {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	if d.timer == nil {
		d.first = rel
		d.timer = time.AfterFunc(watchSettle, func() {
			d.mu.Lock()
			first, closed := d.first, d.closed
			d.timer = nil
			d.mu.Unlock()
			if !closed {
				d.onChange(first)
			}
		})
	}
}

func (d *debouncer) close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closed = true
	if d.timer != nil {
		d.timer.Stop()
	}
}

// watchedDirs walks the watch's paths under root and returns the repo-relative
// directories to watch, skipping ignored ones. A watched file contributes its
// directory, not walked further.
func watchedDirs(root string, w *Watch) []string {
	seen := map[string]bool{}
	var out []string
	add := func(rel string) {
		if !seen[rel] {
			seen[rel] = true
			out = append(out, rel)
		}
	}
	for _, p := range w.Paths {
		abs := filepath.Join(root, filepath.FromSlash(p))
		fi, err := os.Stat(abs)
		if err != nil {
			continue
		}
		if !fi.IsDir() {
			add(filepath.ToSlash(filepath.Dir(p)))
			continue
		}
		_ = filepath.WalkDir(abs, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.IsDir() {
				return nil //nolint:nilerr // an unreadable entry is skipped, not fatal to the watch
			}
			rel, rerr := filepath.Rel(root, path)
			if rerr != nil {
				return filepath.SkipDir
			}
			rel = filepath.ToSlash(rel)
			if w.ignored(rel) {
				return filepath.SkipDir
			}
			add(rel)
			return nil
		})
	}
	return out
}
