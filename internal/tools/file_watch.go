package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fsnotify/fsnotify"
)

// watchReplacedFile wakes its caller whenever a writer puts a new version of
// the file at path in place: the Mate's server its `state` frame's changes
// (hq.ChangesPath), a zcp process its section of the setup status file
// (mate.StatusFilePath). The returned channel receives once each time path is
// created or renamed into place, wakes coming while one waits folding into
// it. Both writers replace the file by rename, so its directory is watched,
// never the file. While the
// directory does not exist yet, its nearest existing ancestor is watched and
// the watch moves down as each directory on the way is created; a directory
// removed moves it up again. Nothing is read on a timer: a watch that loses
// events (the kernel's queue overflowed) wakes once, so the look reads what
// is there. The watch ends with ctx.
func watchReplacedFile(ctx context.Context, path string) (<-chan struct{}, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watch %s: %w", filepath.Dir(path), err)
	}
	cw := &fileWatch{w: w, path: path, dir: filepath.Dir(path)}
	if _, err := cw.settle(); err != nil {
		_ = w.Close()
		return nil, err
	}
	wake := make(chan struct{}, 1)
	go cw.run(ctx, wake)
	return wake, nil
}

// fileWatch is watchReplacedFile's watch: watched is the directory it
// watches now, dir itself or its nearest existing ancestor.
type fileWatch struct {
	w         *fsnotify.Watcher
	path, dir string
	watched   string
}

func (cw *fileWatch) run(ctx context.Context, wake chan<- struct{}) {
	defer cw.w.Close()
	signal := func() {
		select {
		case wake <- struct{}{}:
		default:
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case ev, open := <-cw.w.Events:
			if !open {
				return
			}
			if cw.watched == cw.dir && ev.Name == cw.path {
				// kqueue may say a replaced file went without saying the new
				// one came: one standing there after it is the new one.
				if ev.Has(fsnotify.Create) || (gone(ev) && fileInPlace(cw.path)) {
					signal()
				}
				continue
			}
			if !ev.Has(fsnotify.Create) && (ev.Name != cw.watched || !gone(ev)) {
				continue
			}
			// A directory on the way to dir appeared, or the watched one went.
			reached, err := cw.settle()
			if err != nil {
				return
			}
			if reached && fileInPlace(cw.path) {
				signal()
			}
		case err, open := <-cw.w.Errors:
			if !open {
				return
			}
			if errors.Is(err, fsnotify.ErrEventOverflow) {
				signal()
			}
		}
	}
}

// settle moves the watch to dir, or the nearest ancestor of it that exists,
// until what it watches still exists and nothing nearer dir does; reached is
// whether the watch arrived at dir in this call. A directory created between
// looking and watching is found by the look after the watch is in place.
func (cw *fileWatch) settle() (reached bool, err error) {
	for {
		target, err := nearestExistingDir(cw.dir)
		if err != nil {
			return false, err
		}
		if target == cw.watched {
			return reached, nil
		}
		if cw.watched != "" {
			_ = cw.w.Remove(cw.watched)
		}
		if err := cw.w.Add(target); err != nil {
			return false, fmt.Errorf("watch %s: %w", target, err)
		}
		cw.watched = target
		reached = target == cw.dir
	}
}

// gone reports whether ev says its name went: removed or renamed away.
func gone(ev fsnotify.Event) bool {
	return ev.Has(fsnotify.Remove) || ev.Has(fsnotify.Rename)
}

// fileInPlace reports whether a file stands at path.
func fileInPlace(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// nearestExistingDir is dir, or its nearest ancestor that exists.
func nearestExistingDir(dir string) (string, error) {
	for {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("watch: no directory of %s exists", dir)
		}
		dir = parent
	}
}
