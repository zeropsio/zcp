package farm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/eval"
)

// TestGateSet_EveryScenarioCarriesRequiredOracles pins the S9 slice brief's
// assignment table (plans/zcp-test-farm-2026-09-10-briefs/S9-gate-scenarios.md
// §"Assignment table"): every scenario id listed in eval/farm/gate-set.txt
// must parse with verification.mode=required and carry at least the oracle
// families the table assigns it. The table is copied here as an independent
// oracle — never derived from what the scenario files happen to contain —
// so an accidental regression in a scenario file (a dropped field, a
// mode flip) fails this test even though the scenario still parses.
func TestGateSet_EveryScenarioCarriesRequiredOracles(t *testing.T) {
	repoRoot := gatesetRepoRoot(t)

	gateSetPath := filepath.Join(repoRoot, "eval", "farm", "gate-set.txt")
	ids := readGateSetIDs(t, gateSetPath)

	type oracles struct {
		o1, o2, o3, o4, o5, o6, o7, o8 bool
	}
	// The assignment table, id -> required families ("at least" these).
	want := map[string]oracles{
		"api-node-postgres-classic-dev":           {o1: true, o2: true, o3: true, o5: true},
		"greenfield-node-postgres-dev-stage":      {o1: true, o2: true, o3: true, o5: true},
		"recipe-nestjs-minimal-standard":          {o2: true, o3: true, o5: true},
		"classic-static-nginx-simple":             {o2: true, o3: true},
		"develop-add-managed-dep-to-existing":     {o3: true, o4: true, o5: true},
		"existing-standard-appdev-only-reminders": {o2: true, o4: true},
		"cross-deploy-stage-promote-from-dev":     {o2: true, o7: true},
		"recover-failed-buildfromgit-missing-dep": {o2: true, o5: true},
		"launch-production-from-standard-pair":    {o6: true, o8: true},
		"launch-failure-build-stuck":              {o5: true, o6: true, o8: true},
		"adopt-existing-standard-pair":            {o3: true, o4: true},
		"resume-after-compaction":                 {o3: true, o4: true},
	}

	if len(ids) != len(want) {
		t.Fatalf("gate-set.txt has %d ids, want table has %d entries — keep them in sync", len(ids), len(want))
	}

	for _, id := range ids {
		t.Run(id, func(t *testing.T) {
			wantRow, ok := want[id]
			if !ok {
				t.Fatalf("gate-set.txt id %q has no row in the assignment table — add it", id)
			}

			scPath := filepath.Join(repoRoot, "eval", "behavioral", "scenarios", id+".md")
			sc, err := eval.ParseScenario(scPath)
			if err != nil {
				t.Fatalf("ParseScenario(%s): %v", scPath, err)
			}

			v := sc.Verification
			if v == nil {
				t.Fatalf("scenario %q: verification block missing entirely", id)
			}
			if v.Mode != eval.VerificationRequired {
				t.Errorf("scenario %q: verification.mode = %q, want %q", id, v.Mode, eval.VerificationRequired)
			}
			if v.Spec == "" {
				t.Errorf("scenario %q: verification.spec missing", id)
			}

			got := oracles{
				o1: v.NodePostgresRecord != nil,
				o2: v.Liveness != nil || hasSubdomainProbe(v.ExpectedServices),
				o3: len(v.ExpectedServices) > 0,
				o4: len(v.Unchanged) > 0 || (v.NodePostgresRecord != nil && v.NodePostgresRecord.Unrelated != ""),
				o5: v.NoFailedProcesses,
				o6: v.LaunchShape != nil,
				o7: len(v.ArtifactPromotion) > 0,
				o8: v.NoFabricatedSecret,
			}

			checkFamily(t, id, "O1 nodePostgresRecord", wantRow.o1, got.o1)
			checkFamily(t, id, "O2 liveness/subdomainProbe", wantRow.o2, got.o2)
			checkFamily(t, id, "O3 expectedServices", wantRow.o3, got.o3)
			checkFamily(t, id, "O4 unchanged", wantRow.o4, got.o4)
			checkFamily(t, id, "O5 noFailedProcesses", wantRow.o5, got.o5)
			checkFamily(t, id, "O6 launchShape", wantRow.o6, got.o6)
			checkFamily(t, id, "O7 artifactPromotion", wantRow.o7, got.o7)
			checkFamily(t, id, "O8 noFabricatedSecret", wantRow.o8, got.o8)
		})
	}
}

func checkFamily(t *testing.T, id, family string, want, got bool) {
	t.Helper()
	if want && !got {
		t.Errorf("scenario %q: missing required family %s", id, family)
	}
}

func hasSubdomainProbe(services []eval.ExpectedService) bool {
	for _, s := range services {
		if s.SubdomainProbe != nil {
			return true
		}
	}
	return false
}

func readGateSetIDs(t *testing.T, path string) []string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gate-set.txt: %v", err)
	}
	var ids []string
	for line := range strings.SplitSeq(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ids = append(ids, line)
		}
	}
	return ids
}

func gatesetRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (go.mod)")
		}
		dir = parent
	}
}
