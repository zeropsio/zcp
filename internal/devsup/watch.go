//go:build linux || darwin

package devsup

import (
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// watchSettle is the quiet that ends a burst of changes: the server restarts
// once, when no change has come for that long. One write reaches the watch as
// several changes — a write over sshfs (`cat > file`) truncates the file,
// writes its bytes as they arrive and closes it, more than 200 ms apart — so a
// settle counted from the first change restarted the server once per part.
const watchSettle = 500 * time.Millisecond

// watchSettleMax bounds a burst that never goes quiet (a build writing into a
// watched folder for minutes): counted from its first change, the server
// restarts once that long has passed.
const watchSettleMax = 5 * time.Second

// debouncer turns changed repo paths into one onChange call per burst,
// dropping paths the watch does not cover or ignores. onChange gets the path
// that opened the burst and when the burst's last change came.
type debouncer struct {
	w        *Watch
	onChange func(rel string, last time.Time)
	mu       sync.Mutex
	// open: a burst is waiting for its quiet; first and firstAt are its
	// first change, last its latest; gen numbers the timer that ends it, so
	// a timer replaced by a later change does nothing.
	open          bool
	first         string
	firstAt, last time.Time
	timer         *time.Timer
	gen           int
	closed        bool
}

func (d *debouncer) changed(rel string) {
	if !d.w.covers(rel) || d.w.ignored(rel) {
		return
	}
	now := time.Now()
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return
	}
	if !d.open {
		d.open, d.first, d.firstAt = true, rel, now
	}
	d.last = now
	wait := min(watchSettle, watchSettleMax-now.Sub(d.firstAt))
	if d.timer != nil {
		d.timer.Stop()
	}
	d.gen++
	gen := d.gen
	d.timer = time.AfterFunc(max(wait, 0), func() { d.settled(gen) })
}

// settled ends the burst its timer gen belongs to, unless a later change
// replaced that timer or the watch closed.
func (d *debouncer) settled(gen int) {
	d.mu.Lock()
	if d.closed || gen != d.gen || !d.open {
		d.mu.Unlock()
		return
	}
	first, last := d.first, d.last
	d.open, d.timer = false, nil
	d.mu.Unlock()
	d.onChange(first, last)
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
