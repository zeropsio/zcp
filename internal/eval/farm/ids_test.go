package farm

import (
	"strings"
	"testing"
)

// TestIDs_Grammar pins the FM-47 id grammar (docs/spec-eval-farm.md §7.6):
// a batch id is `^[a-z0-9][a-z0-9-]{0,62}$`; readable legacy run ids match
// `^[a-z0-9][a-z0-9-]{0,127}$`. Independent oracle: every literal
// below is copied from the brief/spec text or hand-built to a known length,
// never derived from ValidBatchID/ValidRunID's own regexp.
func TestIDs_Grammar(t *testing.T) {
	t.Parallel()

	sixtyFour := strings.Repeat("a", 64) // exceeds the batch cap (63), fits the run cap (128)

	tests := []struct {
		name      string
		id        string
		wantBatch bool
		wantRun   bool
	}{
		{"short alnum", "final1", true, true},
		{"batch with hyphen and digits", "batch-1757590000", true, true},
		{"long hyphenated scenario-shaped id", "final1-recover-failed-buildfromgit-missing-dep", true, true},
		{"empty", "", false, false},
		{"leading hyphen", "-x", false, false},
		{"slash", "x/y", false, false},
		{"dot-dot", "..", false, false},
		{"uppercase", "X", false, false},
		{"64 chars exceeds batch cap but fits run cap", sixtyFour, false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ValidBatchID(tc.id); got != tc.wantBatch {
				t.Errorf("ValidBatchID(%q) = %v, want %v", tc.id, got, tc.wantBatch)
			}
			if got := ValidRunID(tc.id); got != tc.wantRun {
				t.Errorf("ValidRunID(%q) = %v, want %v", tc.id, got, tc.wantRun)
			}
		})
	}
}

func TestRunID_EncodingIsInjectiveAndReversible(t *testing.T) {
	t.Parallel()
	a, err := EncodeRunID("foo", "bar-prod")
	if err != nil {
		t.Fatal(err)
	}
	b, err := EncodeRunID("foo-bar", "prod")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatalf("colliding run ids: %q", a)
	}
	for _, tc := range []struct{ id, batch, scenario string }{
		{a, "foo", "bar-prod"}, {b, "foo-bar", "prod"},
	} {
		batch, scenario, ok := DecodeRunID(tc.id)
		if !ok || batch != tc.batch || scenario != tc.scenario {
			t.Fatalf("DecodeRunID(%q) = %q, %q, %v", tc.id, batch, scenario, ok)
		}
		if !ValidRunID(tc.id) {
			t.Fatalf("new run id rejected: %q", tc.id)
		}
	}
}

func TestRunID_LongScenarioUsesAvailableTotalBudget(t *testing.T) {
	longScenario := strings.Repeat("s", 64)
	id, err := EncodeRunID("batch", longScenario)
	if err != nil || !ValidRunID(id) {
		t.Fatalf("long scenario: id=%q err=%v", id, err)
	}
	if _, _, ok := DecodeRunID(id); !ok {
		t.Fatalf("long scenario did not decode: %q", id)
	}
	if _, err := EncodeRunID("batch", strings.Repeat("s", 125)); err == nil {
		t.Fatal("too-long encoded id accepted")
	}
}

func TestProductionProjectName_IsDisjointFromPrimary(t *testing.T) {
	primary := ProjectPrefix + "foo-prod"
	if productionProjectName("foo") == primary {
		t.Fatal("production name aliases primary foo-prod")
	}
}

func TestRunID_MalformedVersionedIDsAreRejected(t *testing.T) {
	for _, id := range []string{"r1_2_a_b", "r1_01_a_b"} {
		if ValidRunID(id) {
			t.Errorf("ValidRunID(%q) = true", id)
		}
	}
}
