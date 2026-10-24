//go:build darwin

package devsup

import (
	"os"
	"path"
	"path/filepath"
	"time"
)

// pollEvery is how often the polling watcher (macOS, a developer's machine)
// looks; dev containers are Linux and use inotify.
const pollEvery = 100 * time.Millisecond

type fileStamp struct {
	mod  time.Time
	size int64
}

// watcher reports changes under a server's watch paths by polling.
type watcher struct {
	root string
	w    *Watch
	d    *debouncer
	stop chan struct{}
}

func newWatcher(root string, w *Watch, onChange func(rel string, last time.Time)) (*watcher, error) {
	wt := &watcher{root: root, w: w, d: &debouncer{w: w, onChange: onChange}, stop: make(chan struct{})}
	prev := wt.snapshot()
	go func() {
		t := time.NewTicker(pollEvery)
		defer t.Stop()
		for {
			select {
			case <-wt.stop:
				return
			case <-t.C:
			}
			cur := wt.snapshot()
			for rel, st := range cur {
				if p, ok := prev[rel]; !ok || p != st {
					wt.d.changed(rel)
				}
			}
			for rel := range prev {
				if _, ok := cur[rel]; !ok {
					wt.d.changed(rel)
				}
			}
			prev = cur
		}
	}()
	return wt, nil
}

func (wt *watcher) snapshot() map[string]fileStamp {
	out := map[string]fileStamp{}
	for _, dir := range watchedDirs(wt.root, wt.w) {
		entries, err := os.ReadDir(filepath.Join(wt.root, filepath.FromSlash(dir)))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if fi, err := e.Info(); err == nil && !e.IsDir() {
				out[path.Join(dir, e.Name())] = fileStamp{fi.ModTime(), fi.Size()}
			}
		}
	}
	return out
}

func (wt *watcher) close() {
	wt.d.close()
	close(wt.stop)
}
