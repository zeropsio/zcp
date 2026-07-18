package telemetry

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

const (
	// defaultSpoolMaxBytes is the total spool bound (spec §5.4).
	defaultSpoolMaxBytes int64 = 10 * 1024 * 1024
	// defaultSpoolMaxAge is the max segment age before eviction (spec §5.4).
	defaultSpoolMaxAge = 7 * 24 * time.Hour

	spoolFileExt = ".jsonl.gz"
	spoolBadExt  = ".bad"
	spoolTmpGlob = ".spool-*.tmp"
)

// spool persists events that could not be sent immediately so a later flush
// can retry them (spec §5.4). One segment per failed batch: gzip JSONL, one
// wire.Event per line. Bounded to maxBytes / maxAge total, oldest evicted
// first. All I/O is owned by the telemetry worker goroutine — spool has no
// internal locking because it has exactly one caller at a time by
// construction (single-writer invariant), matching Client's worker
// ownership model.
type spool struct {
	dir string

	// maxBytes/maxAge default to the spec-mandated bounds but are plain
	// instance fields (not package-level consts) so tests can shrink them
	// without any global mutable state.
	maxBytes int64
	maxAge   time.Duration

	// writeSeq disambiguates segment filenames written within the same
	// nanosecond. Instance-scoped (only the owning goroutine ever calls
	// write), never a package-level counter.
	writeSeq uint64
}

// newSpool prepares dir (0700) and removes any leftover *.bad segments from
// a prior run (spec §5.4 "*.bad deleted at next startup").
func newSpool(dir string) (*spool, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("mkdir spool dir %s: %w", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read spool dir %s: %w", dir, err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), spoolBadExt) {
			_ = os.Remove(filepath.Join(dir, entry.Name()))
		}
	}
	return &spool{dir: dir, maxBytes: defaultSpoolMaxBytes, maxAge: defaultSpoolMaxAge}, nil
}

// write persists events as one new gzip JSONL segment, then prunes the
// spool to its bounds (spec §5.4).
func (s *spool) write(events []wire.Event) error {
	if len(events) == 0 {
		return nil
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	enc := json.NewEncoder(gz)
	for _, e := range events {
		if err := enc.Encode(e); err != nil {
			gz.Close()
			return fmt.Errorf("encode spool event: %w", err)
		}
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("close gzip writer: %w", err)
	}

	s.writeSeq++
	name := fmt.Sprintf("%020d-%06d%s", time.Now().UnixNano(), s.writeSeq%1_000_000, spoolFileExt)
	finalPath := filepath.Join(s.dir, name)
	if err := writeFileAtomicFsync(s.dir, finalPath, buf.Bytes()); err != nil {
		return fmt.Errorf("write spool segment: %w", err)
	}
	s.prune()
	return nil
}

// oldest returns the events from the oldest non-.bad segment, if any, plus
// its full path (for remove). A segment that fails to gunzip/parse is
// renamed *.bad (spec §5.4) and reported as an error — the caller can call
// oldest again to try the next segment on its next flush cycle.
func (s *spool) oldest() (events []wire.Event, segmentPath string, ok bool, err error) {
	names, err := s.segmentNames()
	if err != nil {
		return nil, "", false, err
	}
	if len(names) == 0 {
		return nil, "", false, nil
	}
	path := filepath.Join(s.dir, names[0])
	events, readErr := readSegment(path)
	if readErr != nil {
		badPath := path + spoolBadExt
		if renameErr := os.Rename(path, badPath); renameErr != nil {
			return nil, "", false, fmt.Errorf("rename corrupt segment %s: %w", path, renameErr)
		}
		return nil, "", false, fmt.Errorf("corrupt spool segment %s renamed to .bad: %w", path, readErr)
	}
	return events, path, true, nil
}

// remove deletes a drained segment. Missing-file is not an error (already
// removed, e.g. by a prior prune).
func (s *spool) remove(segmentPath string) error {
	if err := os.Remove(segmentPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove spool segment %s: %w", segmentPath, err)
	}
	return nil
}

// segmentNames returns non-.bad segment filenames sorted oldest-first —
// lexical order matches chronological order because names are
// nanosecond-timestamp-prefixed and zero-padded.
func (s *spool) segmentNames() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("read spool dir %s: %w", s.dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), spoolFileExt) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// prune evicts oldest-first segments until the spool is within bounds:
// total size ≤ maxBytes and no segment older than maxAge (spec §5.4).
func (s *spool) prune() {
	names, err := s.segmentNames()
	if err != nil {
		return
	}
	now := time.Now()
	type sized struct {
		name string
		size int64
		age  time.Duration
	}
	var segs []sized
	var total int64
	for _, n := range names {
		info, statErr := os.Stat(filepath.Join(s.dir, n))
		if statErr != nil {
			continue
		}
		total += info.Size()
		segs = append(segs, sized{name: n, size: info.Size(), age: now.Sub(info.ModTime())})
	}
	// segs is already oldest-first (from segmentNames' sort), so evicting in
	// order preferentially drops the oldest data first.
	for _, seg := range segs {
		if seg.age <= s.maxAge && total <= s.maxBytes {
			continue
		}
		if err := os.Remove(filepath.Join(s.dir, seg.name)); err == nil {
			total -= seg.size
		}
	}
}

func readSegment(path string) ([]wire.Event, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gunzip %s: %w", path, err)
	}
	defer gr.Close()
	dec := json.NewDecoder(gr)
	var events []wire.Event
	for dec.More() {
		var e wire.Event
		if err := dec.Decode(&e); err != nil {
			return nil, fmt.Errorf("decode event in %s: %w", path, err)
		}
		events = append(events, e)
	}
	return events, nil
}

// writeFileAtomicFsync writes data to finalPath via tmp-write-fsync-rename
// (spec §5.4 explicit fsync requirement — spool durability matters more
// than install.json's plain tmp+rename, since a spool segment IS the only
// copy of an event that failed to send).
func writeFileAtomicFsync(dir, finalPath string, data []byte) error {
	tmp, err := os.CreateTemp(dir, spoolTmpGlob)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return fmt.Errorf("fsync temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, finalPath); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
