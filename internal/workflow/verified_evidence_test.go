package workflow

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

func seedEvidenceMeta(t *testing.T, stateDir string) {
	t.Helper()
	if err := WriteServiceMeta(stateDir, &ServiceMeta{
		Hostname:         "appdev",
		Mode:             topology.PlanModeStandard,
		Stages:           []topology.StageRef{{Hostname: "appstage", SetupName: "prod"}},
		PrimarySetupName: "appdev",
		BootstrapSession: "t",
		BootstrappedAt:   "2026-06-10",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// TestRecordVerifiedSetup_RoundTrip pins the F4 durable evidence: a verify
// pass recorded against either pair half lands in the pair-keyed sidecar
// and survives independently of any work session.
func TestRecordVerifiedSetup_RoundTrip(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	seedEvidenceMeta(t, stateDir)

	ev := VerifiedSetupEvidence{
		SetupName:      "prod",
		TargetHostname: "appstage",
		VerifiedAt:     "2026-06-10T12:00:00Z",
		Summary:        "healthy",
	}
	if err := RecordVerifiedSetup(stateDir, "appstage", ev); err != nil {
		t.Fatalf("RecordVerifiedSetup: %v", err)
	}

	// Pair-keyed: the sidecar lives under the canonical dev-half hostname.
	if _, err := os.Stat(filepath.Join(stateDir, "services", "appdev.verified.json")); err != nil {
		t.Fatalf("sidecar not at pair-keyed path: %v", err)
	}

	got, err := ReadVerifiedSetups(stateDir, "appdev")
	if err != nil {
		t.Fatalf("ReadVerifiedSetups: %v", err)
	}
	if got["prod"].VerifiedAt != ev.VerifiedAt || got["prod"].TargetHostname != "appstage" {
		t.Errorf("evidence round-trip mismatch: %+v", got["prod"])
	}

	// Latest-wins per setup.
	ev2 := ev
	ev2.VerifiedAt = "2026-06-10T13:00:00Z"
	if err := RecordVerifiedSetup(stateDir, "appdev", ev2); err != nil {
		t.Fatalf("RecordVerifiedSetup #2: %v", err)
	}
	got, _ = ReadVerifiedSetups(stateDir, "appstage")
	if got["prod"].VerifiedAt != ev2.VerifiedAt {
		t.Errorf("latest evidence must win: %+v", got["prod"])
	}
}

// TestRecordVerifiedSetup_NoMetaNoop pins that evidence without an anchor
// (unknown hostname / no meta) is a silent no-op, and an empty setup name
// records nothing.
func TestRecordVerifiedSetup_NoMetaNoop(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	if err := RecordVerifiedSetup(stateDir, "ghost", VerifiedSetupEvidence{SetupName: "prod", VerifiedAt: "t"}); err != nil {
		t.Fatalf("no-meta record must not error: %v", err)
	}
	seedEvidenceMeta(t, stateDir)
	if err := RecordVerifiedSetup(stateDir, "appdev", VerifiedSetupEvidence{SetupName: "", VerifiedAt: "t"}); err != nil {
		t.Fatalf("empty-setup record must not error: %v", err)
	}
	got, _ := ReadVerifiedSetups(stateDir, "appdev")
	if len(got) != 0 {
		t.Errorf("expected no evidence, got %+v", got)
	}
}

// verifiedSidecars is every verified-setup sidecar in dir's records, by name.
func verifiedSidecars(t *testing.T, dir string) []string {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "services", "*.verified.json"))
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, filepath.Base(p))
	}
	return out
}

// TestDeleteServiceMeta_VerifiedEvidence_PrunedWithTheMeta: a pair's
// verified-setup sidecar goes with its record, whichever way the record is
// deleted — a service deleted through zcp, or a record pruned because none of
// its hostnames is live any more. A sidecar left behind names a pair zcp no
// longer keeps (seen live: boxdev/hqdev/webdev.verified.json after the devs
// were deleted).
func TestDeleteServiceMeta_VerifiedEvidence_PrunedWithTheMeta(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		delete func(stateDir string) error
	}{
		{"deleted by hostname", func(stateDir string) error { return DeleteServiceMeta(stateDir, "appdev") }},
		{"pruned as no longer live", func(stateDir string) error {
			if pruned := PruneServiceMetas(stateDir, map[string]bool{"other": true}); len(pruned) != 1 {
				t.Fatalf("pruned %v, want appdev", pruned)
			}
			return nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stateDir := t.TempDir()
			seedEvidenceMeta(t, stateDir)
			if err := RecordVerifiedSetup(stateDir, "appstage", VerifiedSetupEvidence{SetupName: "prod", TargetHostname: "appstage", VerifiedAt: "2026-10-27T10:00:00Z"}); err != nil {
				t.Fatal(err)
			}
			if err := tt.delete(stateDir); err != nil {
				t.Fatalf("delete: %v", err)
			}
			if left := verifiedSidecars(t, stateDir); len(left) != 0 {
				t.Errorf("sidecars left after the record went: %v", left)
			}
		})
	}
}

// TestMigratePairToPool_VerifiedEvidence_FollowsItsStages: a pair re-keyed
// onto its dev pool — whole, or split one pair per part — keeps the evidence
// of every stage it still holds under its new key, and the dev-keyed sidecar
// goes with the dev-keyed record; evidence of the retired dev service itself
// goes with it, since no pair names that service any more. A revert carries
// it back.
func TestMigratePairToPool_VerifiedEvidence_FollowsItsStages(t *testing.T) {
	t.Parallel()
	record := func(t *testing.T, dir, host, setup string) {
		t.Helper()
		if err := RecordVerifiedSetup(dir, host, VerifiedSetupEvidence{SetupName: setup, TargetHostname: host, VerifiedAt: "2026-10-27T10:00:00Z"}); err != nil {
			t.Fatal(err)
		}
	}
	setups := func(t *testing.T, dir, host string) []string {
		t.Helper()
		got, err := ReadVerifiedSetups(dir, host)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]string, 0, len(got))
		for setup, ev := range got {
			out = append(out, setup+"@"+ev.TargetHostname)
		}
		slices.Sort(out)
		return out
	}

	t.Run("one part", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := WriteServiceMeta(dir, devKeyedPair()); err != nil {
			t.Fatal(err)
		}
		record(t, dir, "appstage", "prod")
		record(t, dir, "appdev", "dev")
		if _, err := MigratePairToPool(dir, appPoolMove()); err != nil {
			t.Fatalf("MigratePairToPool: %v", err)
		}
		if got := setups(t, dir, "apppool"); !slices.Equal(got, []string{"prod@appstage"}) {
			t.Errorf("pool-keyed evidence = %v, want the stage's", got)
		}
		if left := verifiedSidecars(t, dir); !slices.Equal(left, []string{"apppool.verified.json"}) {
			t.Errorf("sidecars = %v, want only the pool-keyed one", left)
		}
		if _, err := RevertPoolPair(dir, appPoolMove()); err != nil {
			t.Fatalf("RevertPoolPair: %v", err)
		}
		if got := setups(t, dir, "appdev"); !slices.Equal(got, []string{"prod@appstage"}) {
			t.Errorf("evidence after the revert = %v, want the stage's back under the dev", got)
		}
		if left := verifiedSidecars(t, dir); !slices.Equal(left, []string{"appdev.verified.json"}) {
			t.Errorf("sidecars after the revert = %v, want only the dev-keyed one", left)
		}
	})

	t.Run("one pair per part", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		if err := WriteServiceMeta(dir, devKeyedMultiPart()); err != nil {
			t.Fatal(err)
		}
		record(t, dir, "webstage", "web")
		record(t, dir, "apistage", "api")
		record(t, dir, "appdev", "dev")
		if _, err := MigratePairToPools(dir, "appdev", multiPartMoves()); err != nil {
			t.Fatalf("MigratePairToPools: %v", err)
		}
		if got := setups(t, dir, "webpool"); !slices.Equal(got, []string{"web@webstage"}) {
			t.Errorf("webpool evidence = %v, want web's stage only", got)
		}
		if got := setups(t, dir, "apipool"); !slices.Equal(got, []string{"api@apistage"}) {
			t.Errorf("apipool evidence = %v, want api's stage only", got)
		}
		if left := verifiedSidecars(t, dir); !slices.Equal(left, []string{"apipool.verified.json", "webpool.verified.json"}) {
			t.Errorf("sidecars = %v, want one per part and none keyed by the dev", left)
		}
	})
}
