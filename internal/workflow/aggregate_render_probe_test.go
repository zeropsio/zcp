package workflow

import (
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/topology"
)

// TestAggregateRender_TwoPairCollapseStructural pins engine ticket E1's
// structural Redundancy fix on the two-pair fixture. Before E1 the four
// atoms (execute, verify, promote-stage, plus the deleted -cmds split)
// rendered N× per matching service: appdev/apidev for the dev-mode
// modes and all four for verify, doubling the body weight without
// new information. After E1 each atom renders 1× and any per-service
// commands live inside `{services-list:TEMPLATE}` directives.
//
// This test fails (correctly) if a regression re-introduces per-service
// duplication on the two-pair shape — e.g. someone reverts an atom to
// service-scoped without `multiService: aggregate`, or rebuilds the
// -cmds atoms.
func TestAggregateRender_TwoPairCollapseStructural(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	envelope := StateEnvelope{
		Phase:       PhaseDevelopActive,
		Environment: EnvContainer,
		Services: []ServiceSnapshot{
			{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "appstage", Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false},
			{Hostname: "appstage", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage, Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false},
			{Hostname: "apidev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "apistage", Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false},
			{Hostname: "apistage", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage, Strategy: topology.StrategyUnset, Bootstrapped: true, Deployed: false},
		},
	}
	matches, err := Synthesize(envelope, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}

	// Each migrated atom must appear exactly once in the matched set.
	// Pre-E1 baseline:
	//   develop-first-deploy-execute: 1 (envelope-scoped)
	//   develop-first-deploy-execute-cmds: 2 (service-scoped, modes:[dev,simple,standard])
	//   develop-first-deploy-verify: 1 (envelope-scoped)
	//   develop-first-deploy-verify-cmds: 4 (service-scoped, no modes filter)
	//   develop-first-deploy-promote-stage: 2 (service-scoped, modes:[standard])
	// → total 10 renders for the per-service first-deploy axis.
	// Post-E1 baseline: 3 renders (execute + verify + promote-stage), each
	// 1× via aggregate; the two -cmds atoms no longer exist. The directive
	// expansion carries the per-service command lines inside the single body.
	wantOnce := []string{
		"develop-first-deploy-execute",
		"develop-first-deploy-verify",
		"develop-first-deploy-promote-stage",
	}
	for _, id := range wantOnce {
		count := 0
		for _, m := range matches {
			if m.AtomID == id {
				count++
			}
		}
		if count != 1 {
			t.Errorf("atom %q rendered %d times in two-pair envelope, want 1", id, count)
		}
	}

	// Deleted atoms must not surface.
	for _, id := range []string{
		"develop-first-deploy-execute-cmds",
		"develop-first-deploy-verify-cmds",
	} {
		for _, m := range matches {
			if m.AtomID == id {
				t.Errorf("deleted atom %q still rendering — atom file or scenarios pin missed?", id)
			}
		}
	}

	// Expansion must carry both dev hosts (deploy/verify) and both pairs
	// (promote-stage). If the directive parser regresses to dropping the
	// hostname placeholder, these would be missing.
	combined := strings.Join(BodiesOf(matches), "\n")
	for _, want := range []string{
		`zerops_deploy targetService="appdev"`,
		`zerops_deploy targetService="apidev"`,
		`zerops_verify serviceHostname="appdev"`,
		`zerops_verify serviceHostname="apidev"`,
		`zerops_deploy sourceService="appdev" targetService="appstage"`,
		`zerops_deploy sourceService="apidev" targetService="apistage"`,
	} {
		if !strings.Contains(combined, want) {
			t.Errorf("two-pair body missing %q", want)
		}
	}
}

// TestAggregateRender_LocalStageFiresExecuteAtom pins the local-stage
// fix that landed alongside E1: a service in `mode=local-stage` with
// `deployStates=never-deployed` MUST get the first-deploy execute atom
// (prose + per-host directive expansion). Pre-E1 the execute prose
// fired via envelopeDeployStates (env-level), the cmds atom did NOT
// fire (mode filter [dev, simple, standard] excluded local-stage). E1
// merged them under the cmds' filter — which would have regressed
// prose visibility for local-stage envelopes — so the merged filter
// was widened to include local-stage. This atom is now visible AND
// carries a concrete `zerops_deploy targetService=...` line, closing
// the prior content gap where local-stage agents got prose without
// a templated command.
func TestAggregateRender_LocalStageFiresExecuteAtom(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	envelope := StateEnvelope{
		Phase:       PhaseDevelopActive,
		Environment: EnvLocal,
		Services: []ServiceSnapshot{{
			Hostname: "appstage", TypeVersion: "nodejs@22",
			RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeLocalStage,
			Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: false,
		}},
	}
	matches, err := Synthesize(envelope, corpus)
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	var executeBody string
	for _, m := range matches {
		if m.AtomID == "develop-first-deploy-execute" {
			executeBody = m.Body
			break
		}
	}
	if executeBody == "" {
		t.Fatalf("develop-first-deploy-execute did not fire for local-stage envelope")
	}
	for _, want := range []string{
		"Run the first deploy",
		`zerops_deploy targetService="appstage"`,
	} {
		if !strings.Contains(executeBody, want) {
			t.Errorf("execute atom body missing %q\n--- body ---\n%s", want, executeBody)
		}
	}
}

// TestAggregateRender_StructuralDuplicationReduction reports the per-fixture
// body size before/after E1's render-collapse. Pre-E1 the two-pair fixture
// double-rendered the migrated atoms; post-E1 the aggregate path emits each
// once. The size delta is the load-bearing signal for the composition
// rubric's Redundancy gate (cycle-2 §6.2).
//
// Sister single-service fixtures see a smaller / no delta — only multi-
// match envelopes saw the duplication.
func TestAggregateRender_StructuralDuplicationReduction(t *testing.T) {
	t.Parallel()
	corpus, err := LoadAtomCorpus()
	if err != nil {
		t.Fatalf("LoadAtomCorpus: %v", err)
	}
	cases := []struct {
		name string
		env  StateEnvelope
	}{
		{
			name: "single_dev",
			env: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeDev,
					Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: false,
				}},
			},
		},
		{
			name: "single_simple",
			env: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{{
					Hostname: "appdev", TypeVersion: "nodejs@22",
					RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeSimple,
					Strategy: topology.StrategyPushDev, Bootstrapped: true, Deployed: false,
				}},
			},
		},
		{
			name: "single_pair_standard",
			env: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "appstage", Bootstrapped: true, Deployed: false},
					{Hostname: "appstage", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage, Bootstrapped: true, Deployed: false},
				},
			},
		},
		{
			name: "two_pair_standard",
			env: StateEnvelope{
				Phase:       PhaseDevelopActive,
				Environment: EnvContainer,
				Services: []ServiceSnapshot{
					{Hostname: "appdev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "appstage", Bootstrapped: true, Deployed: false},
					{Hostname: "appstage", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage, Bootstrapped: true, Deployed: false},
					{Hostname: "apidev", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStandard, StageHostname: "apistage", Bootstrapped: true, Deployed: false},
					{Hostname: "apistage", TypeVersion: "nodejs@22", RuntimeClass: topology.RuntimeDynamic, Mode: topology.ModeStage, Bootstrapped: true, Deployed: false},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			matches, err := Synthesize(tc.env, corpus)
			if err != nil {
				t.Fatalf("Synthesize: %v", err)
			}
			body := strings.Join(BodiesOf(matches), "\n\n")
			t.Logf("fixture=%s atoms=%d body_bytes=%d", tc.name, len(matches), len(body))
		})
	}
}
