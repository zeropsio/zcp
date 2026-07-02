package telemetry

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/telemetry/wire"
)

func sampleEvents(n int) []wire.Event {
	out := make([]wire.Event, n)
	for i := range n {
		out[i] = wire.Event{EventType: wire.EventToolCall, Tool: "zerops_deploy", Seq: uint64(i + 1)}
	}
	return out
}

func TestSpool_WriteThenOldest_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	sp, err := newSpool(dir)
	if err != nil {
		t.Fatalf("newSpool: %v", err)
	}

	want := sampleEvents(3)
	if err := sp.write(want); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, path, ok, err := sp.oldest()
	if err != nil {
		t.Fatalf("oldest: %v", err)
	}
	if !ok {
		t.Fatal("oldest: ok = false, want true")
	}
	if len(got) != len(want) {
		t.Fatalf("got %d events, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Tool != want[i].Tool || got[i].Seq != want[i].Seq {
			t.Fatalf("event %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if path == "" {
		t.Fatal("segment path is empty")
	}
}

func TestSpool_Oldest_EmptySpoolReturnsNotOK(t *testing.T) {
	dir := t.TempDir()
	sp, err := newSpool(dir)
	if err != nil {
		t.Fatalf("newSpool: %v", err)
	}
	_, _, ok, err := sp.oldest()
	if err != nil {
		t.Fatalf("oldest: unexpected error %v", err)
	}
	if ok {
		t.Fatal("ok = true for an empty spool")
	}
}

func TestSpool_Remove_DeletesSegment(t *testing.T) {
	dir := t.TempDir()
	sp, err := newSpool(dir)
	if err != nil {
		t.Fatalf("newSpool: %v", err)
	}
	if err := sp.write(sampleEvents(1)); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, path, ok, err := sp.oldest()
	if err != nil || !ok {
		t.Fatalf("oldest: ok=%v err=%v", ok, err)
	}
	if err := sp.remove(path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	_, _, ok, err = sp.oldest()
	if err != nil {
		t.Fatalf("oldest after remove: %v", err)
	}
	if ok {
		t.Fatal("segment still present after remove")
	}
}

func TestSpool_OldestFirstOrdering(t *testing.T) {
	dir := t.TempDir()
	sp, err := newSpool(dir)
	if err != nil {
		t.Fatalf("newSpool: %v", err)
	}
	if err := sp.write([]wire.Event{{Tool: "first"}}); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if err := sp.write([]wire.Event{{Tool: "second"}}); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	got, _, ok, err := sp.oldest()
	if err != nil || !ok {
		t.Fatalf("oldest: ok=%v err=%v", ok, err)
	}
	if len(got) != 1 || got[0].Tool != "first" {
		t.Fatalf("oldest segment = %+v, want the first-written segment", got)
	}
}

func TestSpool_CorruptSegment_RenamedToBadAndSkipped(t *testing.T) {
	dir := t.TempDir()
	sp, err := newSpool(dir)
	if err != nil {
		t.Fatalf("newSpool: %v", err)
	}
	if err := sp.write(sampleEvents(1)); err != nil {
		t.Fatalf("write: %v", err)
	}
	names, err := sp.segmentNames()
	if err != nil || len(names) != 1 {
		t.Fatalf("segmentNames: names=%v err=%v", names, err)
	}
	segPath := filepath.Join(dir, names[0])
	if err := os.WriteFile(segPath, []byte("not gzip data"), 0o600); err != nil {
		t.Fatalf("corrupt segment: %v", err)
	}

	_, _, ok, err := sp.oldest()
	if err == nil {
		t.Fatal("oldest: expected error for corrupt segment")
	}
	if ok {
		t.Fatal("ok = true for a corrupt segment")
	}

	if _, statErr := os.Stat(segPath); statErr == nil {
		t.Fatal("corrupt segment still present at its original name")
	}
	if _, statErr := os.Stat(segPath + spoolBadExt); statErr != nil {
		t.Fatalf("corrupt segment was not renamed to .bad: %v", statErr)
	}
}

func TestNewSpool_DeletesLeftoverBadFilesFromPriorRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	badPath := filepath.Join(dir, "leftover.jsonl.gz.bad")
	if err := os.WriteFile(badPath, []byte("junk"), 0o600); err != nil {
		t.Fatalf("seed .bad file: %v", err)
	}

	if _, err := newSpool(dir); err != nil {
		t.Fatalf("newSpool: %v", err)
	}

	if _, err := os.Stat(badPath); err == nil {
		t.Fatal(".bad file from a prior run was not deleted at startup")
	}
}

func TestSpool_Write_NoLeftoverTmpFiles(t *testing.T) {
	dir := t.TempDir()
	sp, err := newSpool(dir)
	if err != nil {
		t.Fatalf("newSpool: %v", err)
	}
	if err := sp.write(sampleEvents(1)); err != nil {
		t.Fatalf("write: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("leftover tmp file: %s", e.Name())
		}
	}
}

func TestSpool_Prune_EvictsOldestWhenOverSizeBound(t *testing.T) {
	dir := t.TempDir()
	sp, err := newSpool(dir)
	if err != nil {
		t.Fatalf("newSpool: %v", err)
	}
	// Instance-scoped override (no global mutable state): force the size
	// bound artificially low so we don't need to write 10 MiB in a test.
	sp.maxBytes = 500

	// Each segment holds a Tool string long enough to push total size over
	// the artificial 500-byte bound after a few writes.
	big := make([]wire.Event, 5)
	for i := range big {
		big[i] = wire.Event{Tool: "zerops_deploy_with_a_long_enough_payload_to_matter_for_size"}
	}
	for range 5 {
		if err := sp.write(big); err != nil {
			t.Fatalf("write: %v", err)
		}
	}

	names, err := sp.segmentNames()
	if err != nil {
		t.Fatalf("segmentNames: %v", err)
	}
	if len(names) >= 5 {
		t.Fatalf("expected pruning to have evicted some segments, got %d remaining", len(names))
	}
}

func TestSpool_Prune_EvictsSegmentsOlderThanMaxAge(t *testing.T) {
	dir := t.TempDir()
	sp, err := newSpool(dir)
	if err != nil {
		t.Fatalf("newSpool: %v", err)
	}
	if err := sp.write(sampleEvents(1)); err != nil {
		t.Fatalf("write: %v", err)
	}
	names, err := sp.segmentNames()
	if err != nil || len(names) != 1 {
		t.Fatalf("segmentNames: names=%v err=%v", names, err)
	}
	segPath := filepath.Join(dir, names[0])
	// Backdate the file's mtime to 8 days ago (beyond the 7-day bound).
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(segPath, old, old); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	sp.prune()

	if _, statErr := os.Stat(segPath); statErr == nil {
		t.Fatal("segment older than the 7-day bound was not evicted")
	}
}
