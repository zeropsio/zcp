package devsup

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sync"
	"unsafe"

	"golang.org/x/sys/unix"
)

// inotifyMask holds the events a content change makes. IN_ATTRIB is not one:
// a read over the zcp mount moves the access time, and that must never
// restart a server; a write, a create and a rename are all here.
const inotifyMask = unix.IN_MODIFY | unix.IN_CLOSE_WRITE | unix.IN_CREATE | unix.IN_DELETE |
	unix.IN_MOVED_FROM | unix.IN_MOVED_TO

// watcher reports changes under a server's watch paths through inotify. It
// watches directories, not files, so a file a git checkout renames over keeps
// being seen; a directory created later is watched as it appears.
type watcher struct {
	root string
	w    *Watch
	f    *os.File
	d    *debouncer
	mu   sync.Mutex
	wds  map[int]string // watch descriptor → repo-relative dir
	dirs map[string]bool
}

func newWatcher(root string, w *Watch, onChange func(rel string)) (*watcher, error) {
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		return nil, fmt.Errorf("inotify: %w", err)
	}
	wt := &watcher{
		root: root, w: w, f: os.NewFile(uintptr(fd), "inotify"),
		d:   &debouncer{w: w, onChange: onChange},
		wds: map[int]string{}, dirs: map[string]bool{},
	}
	for _, rel := range watchedDirs(root, w) {
		wt.add(rel)
	}
	go wt.loop()
	return wt, nil
}

func (wt *watcher) add(rel string) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	if wt.dirs[rel] {
		return
	}
	wd, err := unix.InotifyAddWatch(int(wt.f.Fd()), filepath.Join(wt.root, filepath.FromSlash(rel)), inotifyMask)
	if err != nil {
		return
	}
	wt.wds[wd] = rel
	wt.dirs[rel] = true
}

func (wt *watcher) loop() {
	buf := make([]byte, 64<<10)
	for {
		n, err := wt.f.Read(buf)
		if err != nil {
			return
		}
		for off := 0; off+unix.SizeofInotifyEvent <= n; {
			ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
			name := buf[off+unix.SizeofInotifyEvent : off+unix.SizeofInotifyEvent+int(ev.Len)]
			off += unix.SizeofInotifyEvent + int(ev.Len)
			wt.mu.Lock()
			dir, ok := wt.wds[int(ev.Wd)]
			wt.mu.Unlock()
			if !ok {
				continue
			}
			rel := path.Join(dir, cString(name))
			if ev.Mask&unix.IN_ISDIR != 0 && ev.Mask&(unix.IN_CREATE|unix.IN_MOVED_TO) != 0 {
				sub := &Watch{Paths: []string{rel}, Ignore: wt.w.Ignore}
				for _, d := range watchedDirs(wt.root, sub) {
					wt.add(d)
				}
			}
			wt.d.changed(rel)
		}
	}
}

func (wt *watcher) close() {
	wt.d.close()
	_ = wt.f.Close()
}

func cString(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
