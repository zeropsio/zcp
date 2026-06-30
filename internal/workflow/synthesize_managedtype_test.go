package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestManagedTypeAxis_GatesEnvCheatsheets pins RC-D / F7: the per-managed-type
// env cheatsheet fires ONLY when its dep type is in the project. The SQL
// cheatsheet is the sole remaining one — the rare-type cheatsheets
// (clickhouse-kafka / storage / search) were deleted as fully redundant with
// the canonical themes/services.md catalog. A postgres project gets the SQL
// cheatsheet; a kafka-only or managed-dep-free project does NOT.
func TestManagedTypeAxis_GatesEnvCheatsheets(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	mk := func(managed ...string) map[string]bool {
		svcs := make([]ServiceSnapshot, 0, 1+len(managed))
		svcs = append(svcs, ServiceSnapshot{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev, Bootstrapped: true})
		for i, tv := range managed {
			svcs = append(svcs, fixSnapManaged([]string{"db", "q", "s"}[i%3]+string(rune('a'+i)), tv))
		}
		env := StateEnvelope{
			Phase: PhaseDevelopActive, Environment: EnvContainer, Services: svcs,
			WorkSession: &WorkSessionSummary{Intent: "x", Services: []string{"appdev"}},
		}
		matches, err := Synthesize(env, corpus)
		if err != nil {
			t.Fatalf("Synthesize: %v", err)
		}
		got := map[string]bool{}
		for _, m := range matches {
			got[m.AtomID] = true
		}
		return got
	}

	pg := mk("postgresql@16")
	if !pg["develop-env-cheatsheet-sql"] {
		t.Error("postgres project must fire the SQL cheatsheet")
	}

	// A managed dep of a different family must NOT pull in the SQL cheatsheet.
	kafka := mk("kafka@3.9")
	if kafka["develop-env-cheatsheet-sql"] {
		t.Error("kafka-only project must NOT fire the SQL cheatsheet")
	}

	// No managed deps → no cheatsheet at all (e2-class pure runtime).
	none := mk()
	if none["develop-env-cheatsheet-sql"] {
		t.Error("managed-dep-free project must NOT fire the SQL cheatsheet")
	}
}
