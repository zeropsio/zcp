package farm

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestCoverage_CellsDerivedFromStream_SortedDeterministic pins the exact
// (scenario, workflow step, tool decision) cells Coverage derives from the
// two fixture runs under testdata/coverage/basic/ — the expected table is a
// hand-written literal (independent oracle), matching what a human reading
// the two fixture .jsonl files would derive, never produced by running the
// implementation. Running it twice must yield identical bytes (FM-39).
func TestCoverage_CellsDerivedFromStream_SortedDeterministic(t *testing.T) {
	t.Parallel()

	want := []Cell{
		{ScenarioID: "s1", Step: "deploy-active", Decision: "zerops_deploy", Count: 1},
		{ScenarioID: "s1", Step: "develop-active", Decision: "zerops_workflow:status", Count: 2},
		{ScenarioID: "s1", Step: "discover-done", Decision: "zerops_discover", Count: 1},
	}

	for i := range 2 {
		report, err := Coverage("testdata/coverage/basic", "")
		if err != nil {
			t.Fatalf("Coverage: %v", err)
		}
		if !reflect.DeepEqual(report.Cells, want) {
			t.Fatalf("run %d: Cells = %+v, want %+v", i, report.Cells, want)
		}
	}
}

// TestCoverage_RemovedRun_DropsItsCells proves FM-39: deleting a run dir
// from a temp copy of the fixture drops the cells only that run produced —
// run-b's unique (s1, discover-done, zerops_discover) cell vanishes, and the
// shared (s1, develop-active, zerops_workflow:status) cell decrements from
// 2 to 1 rather than disappearing.
func TestCoverage_RemovedRun_DropsItsCells(t *testing.T) {
	t.Parallel()

	dir := copyDirToTemp(t, "testdata/coverage/basic")
	if err := os.RemoveAll(filepath.Join(dir, "run-b")); err != nil {
		t.Fatalf("remove run-b: %v", err)
	}

	report, err := Coverage(dir, "")
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	want := []Cell{
		{ScenarioID: "s1", Step: "deploy-active", Decision: "zerops_deploy", Count: 1},
		{ScenarioID: "s1", Step: "develop-active", Decision: "zerops_workflow:status", Count: 1},
	}
	if !reflect.DeepEqual(report.Cells, want) {
		t.Fatalf("Cells = %+v, want %+v", report.Cells, want)
	}
}

// TestCoverage_NoStream_RunContributesNothingAndIsListed proves a run
// without capture/mcp/ contributes no cells, is listed in the report as
// having no captured stream, and never fails the command.
func TestCoverage_NoStream_RunContributesNothingAndIsListed(t *testing.T) {
	t.Parallel()

	report, err := Coverage("testdata/coverage/nostream", "")
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if len(report.Cells) != 0 {
		t.Errorf("Cells = %+v, want none", report.Cells)
	}
	if want := []string{"run-c"}; !reflect.DeepEqual(report.NoStreamRuns, want) {
		t.Errorf("NoStreamRuns = %v, want %v", report.NoStreamRuns, want)
	}
}

// TestCoverage_Since_AggregatesLaterBatchesOnly proves --since aggregates
// only batch dirs whose manifest.json startedAt is >= the named batch's:
// with --since batch2 over testdata/coverage/since/ (batch1 2026-09-01,
// batch2 2026-09-05, batch3 2026-09-09), only batch2's and batch3's cells
// appear.
func TestCoverage_Since_AggregatesLaterBatchesOnly(t *testing.T) {
	t.Parallel()

	report, err := Coverage("testdata/coverage/since", "batch2")
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	want := []Cell{
		{ScenarioID: "sy", Step: "phase-y", Decision: "zerops_workflow:status", Count: 1},
		{ScenarioID: "sz", Step: "phase-z", Decision: "zerops_workflow:status", Count: 1},
	}
	if !reflect.DeepEqual(report.Cells, want) {
		t.Fatalf("Cells = %+v, want %+v", report.Cells, want)
	}
}

// TestCoverage_RealBundleLayout_ScenarioFromDoneJSON proves Coverage reads
// a run's scenario id from done.json (written by the wrapper for every
// bundle, docs/spec-eval-farm.md §2.3 FM-13) and locates the MCP stream
// under the capture window (capture/capture-<id>/mcp/, resolved the same
// way report.go's resolveCaptureWindowDir does) rather than the flattened
// results/meta.json + capture/mcp/ layout the evaluator never actually
// writes.
func TestCoverage_RealBundleLayout_ScenarioFromDoneJSON(t *testing.T) {
	t.Parallel()

	report, err := Coverage("testdata/coverage/reallayout", "")
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	want := []Cell{
		{ScenarioID: "s1", Step: "deploy-active", Decision: "zerops_deploy", Count: 1},
		{ScenarioID: "s1", Step: "develop-active", Decision: "zerops_workflow:status", Count: 1},
	}
	if !reflect.DeepEqual(report.Cells, want) {
		t.Fatalf("Cells = %+v, want %+v", report.Cells, want)
	}
	if len(report.NoStreamRuns) != 0 {
		t.Errorf("NoStreamRuns = %v, want none", report.NoStreamRuns)
	}
}

// TestCoverage_NotRunBundle_ListedNotFatal proves a bundle that died at the
// binding preflight (done.json present with runnerDimensions.execution
// "error: …", no results/**/meta.json, no capture window at all — the
// live gate2 shape) is listed under NoStreamRuns by its dir name and
// contributes no cells, never aborting the batch.
func TestCoverage_NotRunBundle_ListedNotFatal(t *testing.T) {
	t.Parallel()

	report, err := Coverage("testdata/coverage/notrun", "")
	if err != nil {
		t.Fatalf("Coverage: %v", err)
	}
	if len(report.Cells) != 0 {
		t.Errorf("Cells = %+v, want none", report.Cells)
	}
	if want := []string{"run-nr"}; !reflect.DeepEqual(report.NoStreamRuns, want) {
		t.Errorf("NoStreamRuns = %v, want %v", report.NoStreamRuns, want)
	}
}

// copyDirToTemp makes a writable copy of src under t.TempDir() so a test
// can mutate it (e.g. delete a run dir) without touching the checked-in
// fixture.
func copyDirToTemp(t *testing.T, src string) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, body, 0o644)
	})
	if err != nil {
		t.Fatalf("copy %s: %v", src, err)
	}
	return dst
}
