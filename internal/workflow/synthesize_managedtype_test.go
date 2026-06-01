package workflow

import (
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestManagedTypeAxis_GatesEnvCheatsheets pins RC-D / F7: per-managed-type env
// cheatsheets fire ONLY when that dep type is in the project. A postgres-only
// project gets the SQL cheatsheet and NOT the kafka/storage/search ones — the
// "70% irrelevant guidance" fix.
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
	for _, rare := range []string{"develop-env-cheatsheet-clickhouse-kafka", "develop-env-cheatsheet-storage", "develop-env-cheatsheet-search"} {
		if pg[rare] {
			t.Errorf("postgres-only project must NOT fire %q", rare)
		}
	}

	kafka := mk("kafka@3.9")
	if !kafka["develop-env-cheatsheet-clickhouse-kafka"] {
		t.Error("kafka project must fire the clickhouse/kafka cheatsheet")
	}
	if kafka["develop-env-cheatsheet-sql"] {
		t.Error("kafka-only project must NOT fire the SQL cheatsheet")
	}

	// No managed deps → no cheatsheets at all (e2-class pure runtime).
	none := mk()
	for _, id := range []string{"develop-env-cheatsheet-sql", "develop-env-cheatsheet-clickhouse-kafka", "develop-env-cheatsheet-storage", "develop-env-cheatsheet-search"} {
		if none[id] {
			t.Errorf("managed-dep-free project must NOT fire %q", id)
		}
	}
}
