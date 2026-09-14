package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestEvalFarmReport_ManifestMissingRun_Blocked(t *testing.T) {
	// Non-parallel: captureOutput redirects process stdout/stderr.
	dir := t.TempDir()
	writeFarmReportTestFile(t, dir, "manifest.json", `{"runs":[{"runId":"run-a"},{"runId":"run-b"}]}`)
	writeFarmReportTestFile(t, dir, "run-a/done.json", `{"runId":"run-a"}`)
	var code int
	stdout, _ := captureOutput(t, func() { code = runFarmReport([]string{dir}) })
	if code == 0 || !strings.Contains(stdout, "run run-b (scenario=) verdict=blocked") || !strings.Contains(stdout, "runs: 2") {
		t.Fatalf("code=%d output=%s; want both runs and missing run blocked", code, stdout)
	}
}

func TestFarmReportRunDirs_ManifestInventory(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, manifest string
		want           []string
		wantErr        bool
	}{
		{name: "missing and present", manifest: `{"runs":[{"runId":"run-b"},{"runId":"run-a"}]}`, want: []string{"run-a", "run-b"}},
		{name: "malformed", manifest: `{`, wantErr: true},
		{name: "escaping id", manifest: `{"runs":[{"runId":"../outside"}]}`, wantErr: true},
		{name: "duplicate", manifest: `{"runs":[{"runId":"run-a"},{"runId":"run-a"}]}`, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeFarmReportTestFile(t, dir, "manifest.json", tc.manifest)
			writeFarmReportTestFile(t, dir, "run-a/done.json", `{}`)
			writeFarmReportTestFile(t, dir, "unrelated/notes.txt", "not a batch run")
			got, err := farmReportRunDirs(dir)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want manifest error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var want []string
			for _, id := range tc.want {
				want = append(want, filepath.Join(dir, id))
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("got %v want %v", got, want)
			}
		})
	}
}

// writeFarmReportTestFile writes content to dir/rel, creating parents.
func writeFarmReportTestFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestEvalFarmReport_Verb_PrintsBatchRollup pins docs/spec-eval-farm.md
// §5.2 FM-37: `zcp eval farm report <dir>` over a pulled batch dir (two run
// subdirectories, each with only a done.json — no capture window, so each
// bundle's own outcome is "blocked: no bundle") prints one block per run
// plus a roll-up line, and is refused without the ZCP_AUTHORING gate like
// every other farm verb (FM-17).
func TestEvalFarmReport_Verb_PrintsBatchRollup(t *testing.T) {
	t.Setenv("ZCP_AUTHORING", "1")

	batchDir := t.TempDir()
	// Two runs, neither carrying a done.json — the cheapest bundle shape to
	// assert the dispatcher visits every run subdirectory and rolls them up,
	// without depending on a full capture window.
	writeFarmReportTestFile(t, batchDir, "run-a/results/a.txt", "a")
	writeFarmReportTestFile(t, batchDir, "run-b/results/b.txt", "b")

	var code int
	stdout, _ := captureOutput(t, func() {
		code = runEvalFarm([]string{"report", batchDir})
	})
	if code == 0 {
		t.Fatalf("runEvalFarm(report) = 0, want nonzero (both runs are blocked: no bundle)")
	}
	if !strings.Contains(stdout, "run-a") || !strings.Contains(stdout, "run-b") {
		t.Fatalf("stdout = %q, want a block naming both run-a and run-b", stdout)
	}
	if !strings.Contains(stdout, "blocked: no bundle") {
		t.Fatalf("stdout = %q, want each run's outcome to say %q", stdout, "blocked: no bundle")
	}
	if !strings.Contains(stdout, "Roll-up") && !strings.Contains(stdout, "roll-up") {
		t.Fatalf("stdout = %q, want a roll-up section", stdout)
	}
}

// TestEvalFarmReport_Verb_NoDirArg_ErrorsNonzero pins the argument contract:
// `report` without a directory is a usage error, not a silent no-op.
func TestEvalFarmReport_Verb_NoDirArg_ErrorsNonzero(t *testing.T) {
	t.Setenv("ZCP_AUTHORING", "1")
	code := runEvalFarm([]string{"report"})
	if code == 0 {
		t.Fatal("runEvalFarm([\"report\"]) = 0, want nonzero without a directory argument")
	}
}
