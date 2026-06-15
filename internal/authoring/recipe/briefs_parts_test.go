package recipe

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Run-31 follow-up review fixes. Tests for the partWriter primitives
// added/changed by the multi-file briefs review:
//   - Fix 1 — WriteOrSplit opens a fresh part when the current one
//     overflows.
//   - Fix 2 — index.md body stays under PerPartTokenCeiling even with
//     many real parts.
//   - Fix 4 — Persist populates BriefMetrics so the dispatch envelope
//     can surface multi-file emission size signals.
//   - Fix 5 — duplicate slug across StartPart calls within the same
//     composer is refused so part-N filenames carry distinct slug
//     semantics.
//   - Fix 7 — Persist produces deterministic paths for identical inputs;
//     re-emitting a brief overwrites the prior dir rather than racing
//     a fresh timestamp suffix.
//   - Fix 8 — Persist failure cleans up partial files instead of leaving
//     a half-written brief dir.

// TestPartWriter_WriteOrSplit_OpensFreshPartOnOverflow — WriteOrSplit
// must open a fresh part with the given slug when the current part's
// remaining headroom can't fit the body. The body lands intact on the
// new part; the prior part is finalized as-is.
func TestPartWriter_WriteOrSplit_OpensFreshPartOnOverflow(t *testing.T) {
	t.Parallel()
	w := newPartWriter()
	if err := w.StartPart("first", "first part"); err != nil {
		t.Fatalf("StartPart first: %v", err)
	}
	// Fill the first part close to the cap.
	bigBody := strings.Repeat("x", PartFileCap-100)
	if err := w.Write(bigBody); err != nil {
		t.Fatalf("Write filler: %v", err)
	}
	// This block would push the first part over the cap — WriteOrSplit
	// should open a new part with slug "second" and write there.
	overflow := strings.Repeat("y", 1024)
	if err := w.WriteOrSplit("second", "second part", overflow); err != nil {
		t.Fatalf("WriteOrSplit overflow: %v", err)
	}
	if got := len(w.parts); got != 2 {
		t.Fatalf("expected 2 parts after overflow split; got %d", got)
	}
	if w.parts[1].slug != "second" {
		t.Errorf("expected fresh part slug 'second'; got %q", w.parts[1].slug)
	}
	if !strings.Contains(w.parts[1].body.String(), overflow) {
		t.Errorf("overflow body missing from second part")
	}
	if strings.Contains(w.parts[0].body.String(), overflow) {
		t.Errorf("overflow body leaked onto first part — should have moved to fresh part")
	}
}

// TestPartWriter_WriteOrSplit_NoSplitWhenFits — when the body fits,
// WriteOrSplit MUST keep writing on the current part (no fresh part).
func TestPartWriter_WriteOrSplit_NoSplitWhenFits(t *testing.T) {
	t.Parallel()
	w := newPartWriter()
	if err := w.StartPart("only", "only part"); err != nil {
		t.Fatalf("StartPart: %v", err)
	}
	if err := w.WriteOrSplit("never-used", "never used", "small body"); err != nil {
		t.Fatalf("WriteOrSplit: %v", err)
	}
	if got := len(w.parts); got != 1 {
		t.Errorf("expected 1 part when body fits; got %d", got)
	}
	if !strings.Contains(w.parts[0].body.String(), "small body") {
		t.Errorf("body missing from current part")
	}
}

// TestPartWriter_IndexUnderPerPartCeiling_RealSlug — under realistic
// per-part counts and absolute paths, index.md itself fits comfortably
// under the per-part token cap. Buildable on the same fixture as the
// production multi-file composers.
func TestPartWriter_IndexUnderPerPartCeiling_RealSlug(t *testing.T) {
	t.Parallel()
	plan := realShowcasePlan()
	facts := runShapedFactsCorpus()
	outRoot := t.TempDir()
	brief, err := buildCodebaseContentBriefMultiFile(plan, plan.Codebases[0], nil, facts, outRoot)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	body, err := os.ReadFile(brief.IndexPath)
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	tokens := EstimateTokens(string(body))
	if tokens >= PerPartTokenCeiling {
		t.Errorf("index.md %d tokens (%d bytes) >= per-part ceiling %d", tokens, len(body), PerPartTokenCeiling)
	}
}

// TestPartWriter_Persist_PopulatesBriefMetrics — Persist returns metrics
// describing the multi-file emission so the dispatch handler can stamp
// observability on the wire envelope. Parts/TotalBytes/MaxPartBytes/
// MaxPartEstimatedTokens all populated.
func TestPartWriter_Persist_PopulatesBriefMetrics(t *testing.T) {
	t.Parallel()
	w := newPartWriter()
	if err := w.StartPart("first", "first part"); err != nil {
		t.Fatalf("StartPart first: %v", err)
	}
	if err := w.Write(strings.Repeat("a", 100)); err != nil {
		t.Fatalf("Write a: %v", err)
	}
	if err := w.StartPart("second", "second part"); err != nil {
		t.Fatalf("StartPart second: %v", err)
	}
	if err := w.Write(strings.Repeat("b", 500)); err != nil {
		t.Fatalf("Write b: %v", err)
	}
	dir := t.TempDir()
	_, _, metrics, err := w.Persist(dir, "header\n", "closing\n", BriefCodebaseContent, "api")
	if err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if metrics.Parts != 2 {
		t.Errorf("Parts: want 2, got %d", metrics.Parts)
	}
	// TotalBytes should sum at least the part bodies (100 + 500); index adds more.
	if metrics.TotalBytes < 600 {
		t.Errorf("TotalBytes: want >= 600, got %d", metrics.TotalBytes)
	}
	if metrics.MaxPartBytes < 500 {
		t.Errorf("MaxPartBytes: want >= 500, got %d", metrics.MaxPartBytes)
	}
	if metrics.MaxPartEstimatedTokens != EstimateTokens(strings.Repeat("b", 500)) {
		t.Errorf("MaxPartEstimatedTokens: want %d, got %d",
			EstimateTokens(strings.Repeat("b", 500)), metrics.MaxPartEstimatedTokens)
	}
}

// TestPartWriter_DuplicateSlug_Refused — StartPart called twice with the
// same slug in one composer call returns an error. Filenames stay
// `part-N-<slug>.md` and N alone keeps them unique on disk, but the
// slug carries semantics; reuse means ambiguous part identity.
func TestPartWriter_DuplicateSlug_Refused(t *testing.T) {
	t.Parallel()
	w := newPartWriter()
	if err := w.StartPart("alpha", "first"); err != nil {
		t.Fatalf("StartPart alpha 1: %v", err)
	}
	if err := w.StartPart("beta", "second"); err != nil {
		t.Fatalf("StartPart beta: %v", err)
	}
	err := w.StartPart("alpha", "duplicate")
	if err == nil {
		t.Fatalf("expected duplicate-slug error; got nil")
	}
	if !strings.Contains(err.Error(), "alpha") {
		t.Errorf("error should mention duplicate slug; got %q", err)
	}
}

// TestPartWriter_Persist_DeterministicPaths — same composer inputs (kind
// + codebase) produce the same brief dir. Re-emitting the brief
// overwrites the prior content rather than racing a unix-nano suffix.
func TestPartWriter_Persist_DeterministicPaths(t *testing.T) {
	t.Parallel()
	build := func() (string, []string) {
		w := newPartWriter()
		if err := w.StartPart("only", "only part"); err != nil {
			t.Fatalf("StartPart: %v", err)
		}
		if err := w.Write("body\n"); err != nil {
			t.Fatalf("Write: %v", err)
		}
		dir := "/tmp/zcp-deterministic-test-" + t.Name()
		// Use a fixed dir for both runs.
		idx, parts, _, err := w.Persist(dir, "header\n", "closing\n", BriefCodebaseContent, "api")
		if err != nil {
			t.Fatalf("Persist: %v", err)
		}
		return idx, parts
	}
	// Cleanup any previous run-tree.
	rootTmp := t.TempDir()
	w1 := newPartWriter()
	if err := w1.StartPart("only", "only part"); err != nil {
		t.Fatalf("StartPart 1: %v", err)
	}
	if err := w1.Write("body\n"); err != nil {
		t.Fatalf("Write 1: %v", err)
	}
	idx1, parts1, _, err := w1.Persist(rootTmp, "header\n", "closing\n", BriefCodebaseContent, "api")
	if err != nil {
		t.Fatalf("Persist 1: %v", err)
	}
	w2 := newPartWriter()
	if err := w2.StartPart("only", "only part"); err != nil {
		t.Fatalf("StartPart 2: %v", err)
	}
	if err := w2.Write("body\n"); err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	idx2, parts2, _, err := w2.Persist(rootTmp, "header\n", "closing\n", BriefCodebaseContent, "api")
	if err != nil {
		t.Fatalf("Persist 2: %v", err)
	}
	if idx1 != idx2 {
		t.Errorf("expected deterministic indexPath; got %q vs %q", idx1, idx2)
	}
	if len(parts1) != len(parts2) {
		t.Fatalf("parts length differs: %d vs %d", len(parts1), len(parts2))
	}
	for i := range parts1 {
		if parts1[i] != parts2[i] {
			t.Errorf("part[%d] path differs: %q vs %q", i, parts1[i], parts2[i])
		}
	}
	// Silence unused warning in case build() is removed later.
	_ = build
}

// TestPartWriter_Persist_AtomicCleanupOnFailure — when a write fails
// mid-Persist, no partial dir survives. Verified by injecting a
// non-writable dir at a path Persist will try to create files under.
//
// non-parallel: mutates writePartFileAtomicHook (package-global).
func TestPartWriter_Persist_AtomicCleanupOnFailure(t *testing.T) {
	root := t.TempDir()
	// Pre-create the brief dir as a FILE so MkdirAll inside Persist fails.
	// Persist computes dir = root/.briefs/<kind>-<cb>; if root/.briefs is a
	// file, MkdirAll for the brief subdir returns an error.
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdirall root: %v", err)
	}
	briefsParent := filepath.Join(root, ".briefs")
	if err := os.WriteFile(briefsParent, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("create file at .briefs: %v", err)
	}
	w := newPartWriter()
	if err := w.StartPart("only", "only part"); err != nil {
		t.Fatalf("StartPart: %v", err)
	}
	if err := w.Write("body\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, _, _, err := w.Persist(root, "header\n", "closing\n", BriefCodebaseContent, "api"); err == nil {
		t.Fatalf("expected Persist to fail when .briefs is a file; got nil")
	}
	// Verify no half-written brief dir remains under the (now-broken) root.
	// We can't readdir under .briefs (it's a file), but the test invariant
	// is "Persist returned an error and didn't leave a partial brief dir".
	// Confirm .briefs is still the file we put there and nothing else
	// snuck in.
	info, err := os.Stat(briefsParent)
	if err != nil {
		t.Fatalf("stat .briefs: %v", err)
	}
	if info.IsDir() {
		t.Errorf(".briefs unexpectedly became a directory; cleanup failed?")
	}

	// Second-failure-mode: dir creation succeeds but a part-write fails
	// because the dir is read-only after MkdirAll. Verify cleanup removes
	// the dir on rollback.
	root2 := t.TempDir()
	w2 := newPartWriter()
	if err := w2.StartPart("only", "only part"); err != nil {
		t.Fatalf("StartPart 2: %v", err)
	}
	if err := w2.Write("body\n"); err != nil {
		t.Fatalf("Write 2: %v", err)
	}
	// Inject failure: stub the writePartFile hook.
	prev := writePartFileAtomicHook
	t.Cleanup(func() { writePartFileAtomicHook = prev })
	writePartFileAtomicHook = func(_, _, _ string) error {
		return errors.New("synthetic write failure")
	}
	if _, _, _, err := w2.Persist(root2, "header\n", "closing\n", BriefCodebaseContent, "api"); err == nil {
		t.Fatalf("expected Persist to fail with injected write failure; got nil")
	}
	// The brief dir should NOT exist after cleanup.
	briefDir := filepath.Join(root2, ".briefs")
	entries, err := os.ReadDir(briefDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("readdir .briefs: %v", err)
	}
	for _, e := range entries {
		t.Errorf("partial brief dir survived cleanup: %s", e.Name())
	}
}

// TestBrief_Validate_RejectsAmbiguousShape — a Brief MUST be either
// single-file (Body populated, IndexPath empty) or multi-file (IndexPath
// populated, Body empty). Both populated means a future caller can't
// pick a transport; Validate refuses.
func TestBrief_Validate_RejectsAmbiguousShape(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		brief   Brief
		wantErr bool
	}{
		{
			name:    "single-file/body-only/ok",
			brief:   Brief{Kind: BriefScaffold, Body: "hello"},
			wantErr: false,
		},
		{
			name:    "multi-file/index-only/ok",
			brief:   Brief{Kind: BriefCodebaseContent, IndexPath: "/tmp/idx.md", PartPaths: []string{"/tmp/p1.md"}},
			wantErr: false,
		},
		{
			name:    "ambiguous/body+index",
			brief:   Brief{Kind: BriefCodebaseContent, Body: "hello", IndexPath: "/tmp/idx.md"},
			wantErr: true,
		},
		{
			name:    "empty/no-body-no-index",
			brief:   Brief{Kind: BriefScaffold},
			wantErr: true,
		},
		{
			name:    "multi-file/index-no-parts",
			brief:   Brief{Kind: BriefCodebaseContent, IndexPath: "/tmp/idx.md"},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.brief.Validate()
			if tc.wantErr && err == nil {
				t.Errorf("expected error; got nil for %+v", tc.brief)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("unexpected error %v for %+v", err, tc.brief)
			}
		})
	}
}

// TestPartWriter_Persist_HandlerSurfacesMetrics — when the production
// dispatch handler runs over the multi-file path, BriefSize is the index
// file's byte count. Sanity test that Persist's metrics align with the
// handler's stat-based BriefSize so callers can rely on either signal.
func TestPartWriter_Persist_HandlerSurfacesMetrics(t *testing.T) {
	t.Parallel()
	w := newPartWriter()
	if err := w.StartPart("a", "first"); err != nil {
		t.Fatalf("StartPart a: %v", err)
	}
	if err := w.Write("hello world\n"); err != nil {
		t.Fatalf("Write: %v", err)
	}
	dir := t.TempDir()
	idx, parts, metrics, err := w.Persist(dir, "header\n", "closing\n", BriefCodebaseContent, "api")
	if err != nil {
		t.Fatalf("Persist: %v", err)
	}
	if metrics.Parts != len(parts) {
		t.Errorf("metrics.Parts %d != len(parts) %d", metrics.Parts, len(parts))
	}
	idxStat, err := os.Stat(idx)
	if err != nil {
		t.Fatalf("stat index: %v", err)
	}
	// TotalBytes is index + every part; index alone is part of it.
	if metrics.TotalBytes < int(idxStat.Size()) {
		t.Errorf("metrics.TotalBytes %d < index size %d", metrics.TotalBytes, idxStat.Size())
	}
}
