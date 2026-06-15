package recipe

import (
	"path/filepath"
	"strings"
	"testing"
)

// run47_item_g_test.go — Run-47 Item G pin tests.
//
// Item G — Refuse refinement-2 auto-override when classification is
// ambiguous. The auto-DROP path in EnrichFindings unconditionally
// overrode mode→DROP whenever the resolved fact carried a DISCARD
// class (framework-quirk / library-metadata / self-inflicted). The
// run-46 worker IG #2 case witnessed scaffold-time framework-quirk
// drove auto-DROP even though main-agent surface test HELD
// (createApplicationContext is Zerops-rolling-deploys-forced).
//
// Fix: when the candidate class is framework-quirk AND the fact's
// topic has cross-surface evidence (≥2 surfaces OR citation-map topic
// hit), refuse the auto-override. Emit ClassificationAmbiguous=true
// + AutoOverrideRefused=<reason>; do NOT change Mode. Main agent
// settles via S4 surface test.
//
// Principle: ambiguity refuses, doesn't re-classify. Replacing one
// brittle classifier with another is the monkey-patch pattern codex
// flagged.
//
// Plan: plans/run-47-substrate-fixes.md §"Item G".

// --------------------------------------------------------------------
// hasCrossSurfaceEvidence — deterministic observation, no classification
// --------------------------------------------------------------------

// TestHasCrossSurfaceEvidence_Deterministic — same inputs, same output
// across N invocations. Pins that the helper observes rather than
// classifies; no randomness, no map-iteration ordering leakage.
func TestHasCrossSurfaceEvidence_Deterministic(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.Fragments = map[string]string{
		"codebase/worker/integration-guide/2": `### Application context, rolling deploys, minContainers
The worker uses createApplicationContext to keep state alive across rolling-deploy SIGTERM. Set minContainers >= 2 to avoid drop.`,
		"codebase/worker/knowledge-base": `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **Queue group survives rolling deploys** — minContainers >= 2 keeps a worker available for SIGTERM-driven failover during a rolling-deploy.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->`,
		"codebase/worker/zerops-yaml": `zerops:
  - setup: worker
    run:
      base: nodejs@22
`,
	}
	fact := FactRecord{
		Kind:           FactKindPorterChange,
		Topic:          "rolling-deploys",
		Service:        "worker",
		RecordedAt:     "2026-05-13T10:00:00Z",
		CandidateClass: "framework-quirk",
	}
	first := hasCrossSurfaceEvidence(fact, plan)
	for i := range 10 {
		got := hasCrossSurfaceEvidence(fact, plan)
		if got != first {
			t.Fatalf("hasCrossSurfaceEvidence non-deterministic at iter %d: got %v, want %v", i, got, first)
		}
	}
	// And the answer must be true for this fixture (IG slot + KB bullet
	// both mention rolling-deploys keywords).
	if !first {
		t.Errorf("expected cross-surface evidence on rolling-deploys topic across IG+KB; got false")
	}
}

// TestHasCrossSurfaceEvidence_CitationMapTopicMatch — a fact whose
// topic resolves to a citation-map guide id returns true even when
// only one surface (or none) carries keyword hits. Documented
// platform topics are NOT framework quirks by definition.
func TestHasCrossSurfaceEvidence_CitationMapTopicMatch(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	// Intentionally no fragments — only the citation-map match should
	// drive evidence true.
	plan.Fragments = map[string]string{}
	plan.EnvComments = map[string]EnvComments{}

	cases := []struct {
		name      string
		factTopic string
		want      bool
	}{
		{
			name:      "rolling-deploys is a citation-map topic",
			factTopic: "rolling-deploys",
			want:      true,
		},
		{
			name:      "init-commands is a citation-map topic",
			factTopic: "init-commands",
			want:      true,
		},
		{
			name:      "managed-services-postgresql is a citation-map topic",
			factTopic: "managed-services-postgresql",
			want:      true,
		},
		{
			name:      "execOnce maps to init-commands guide",
			factTopic: "execOnce",
			want:      true,
		},
		{
			name:      "obscure framework quirk has no citation entry",
			factTopic: "some-random-framework-quirk-topic-xxx",
			want:      false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fact := FactRecord{
				Kind:           FactKindPorterChange,
				Topic:          tc.factTopic,
				CandidateClass: "framework-quirk",
			}
			if got := hasCrossSurfaceEvidence(fact, plan); got != tc.want {
				t.Errorf("hasCrossSurfaceEvidence(topic=%q): got %v, want %v", tc.factTopic, got, tc.want)
			}
		})
	}
}

// TestHasCrossSurfaceEvidence_SingleSurfaceNoCitation — a fact's topic
// appears in exactly one surface AND has no citation-map entry →
// returns false (unambiguous → auto-override fires per existing
// behavior).
func TestHasCrossSurfaceEvidence_SingleSurfaceNoCitation(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.Fragments = map[string]string{
		// Body mentions a keyword that no citation-map topic recognises.
		"codebase/api/knowledge-base": `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **Some bespoke framework quirk** — this is a one-off framework idiosyncrasy with no platform docs coverage.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->`,
	}
	plan.EnvComments = map[string]EnvComments{}
	fact := FactRecord{
		Kind:           FactKindPorterChange,
		Topic:          "some-bespoke-quirk-only-here",
		CandidateClass: "framework-quirk",
	}
	if got := hasCrossSurfaceEvidence(fact, plan); got {
		t.Errorf("hasCrossSurfaceEvidence: expected false for single-surface non-citation topic; got true")
	}
}

// --------------------------------------------------------------------
// EnrichFindings — auto-override behavior
// --------------------------------------------------------------------

// TestEnrichFindings_RefusesAutoOverrideOnAmbiguousClass — fact tagged
// framework-quirk + cited in IG + KB + yaml (cross-surface evidence)
// → auto-override refused; finding mode unchanged from original audit
// emission; ClassificationAmbiguous: true; AutoOverrideRefused: <non
// -empty>.
func TestEnrichFindings_RefusesAutoOverrideOnAmbiguousClass(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.Fragments = map[string]string{
		"codebase/worker/integration-guide/2": `### Application context, rolling deploys, minContainers
The worker uses createApplicationContext to keep state alive across rolling-deploy SIGTERM. Set minContainers >= 2.`,
		"codebase/worker/knowledge-base": `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **Queue group survives rolling deploys** — minContainers >= 2 keeps a worker available for SIGTERM-driven failover during a rolling-deploy.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->`,
		"codebase/worker/zerops-yaml": `zerops:
  - setup: worker
    run:
      base: nodejs@22
      ports: []
`,
	}
	facts := []FactRecord{{
		Kind:           FactKindPorterChange,
		Topic:          "rolling-deploys",
		Service:        "worker",
		RecordedAt:     "2026-05-13T10:00:00Z",
		CandidateClass: "framework-quirk",
		Why:            "createApplicationContext is required for rolling-deploy SIGTERM",
	}}
	slim := SlimFinding{
		Surface:                "S4",
		Scope:                  "worker",
		ItemReference:          "IG #2",
		SurfaceTestFailureMode: "REWRITE",
		Topic:                  "rolling-deploys",
		FactRef:                "rolling-deploys@2026-05-13T10:00:00Z",
		Rationale:              "audit thinks this is a quirk; surface test will say otherwise",
	}
	env := &FindingsEnvelope{Findings: []SlimFinding{slim}}
	out := EnrichFindingsWithPlan(env, facts, plan)
	if len(out.Findings) != 1 {
		t.Fatalf("expected 1 enriched finding; got %d", len(out.Findings))
	}
	got := out.Findings[0]
	if got.SurfaceTestFailureMode != "REWRITE" {
		t.Errorf("Mode auto-overridden despite cross-surface evidence: got %q, want %q (unchanged from slim)",
			got.SurfaceTestFailureMode, "REWRITE")
	}
	if !got.ClassificationAmbiguous {
		t.Errorf("ClassificationAmbiguous must be true on refused auto-override; got false")
	}
	if got.AutoOverrideRefused == "" {
		t.Errorf("AutoOverrideRefused must be non-empty reason on refused auto-override; got empty")
	}
	// Classification stays empty (engine doesn't decide; main agent settles).
	if got.Classification != "" {
		t.Errorf("Classification must be empty on ambiguity refusal (engine doesn't re-classify); got %q",
			got.Classification)
	}
}

// TestEnrichFindings_AutoOverridesOnUnambiguousClass — fact tagged
// framework-quirk, topic appears in only one surface AND not in the
// citation map → auto-override fires (existing behavior preserved).
// Regression test on existing DISCARD-class override semantics.
func TestEnrichFindings_AutoOverridesOnUnambiguousClass(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.Fragments = map[string]string{
		// Only one surface mentions this bespoke topic; citation map has
		// no entry — truly unambiguous framework-quirk.
		"codebase/api/knowledge-base": `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **Bespoke API helper utility** — this is a one-off framework idiosyncrasy with no platform docs coverage.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->`,
	}
	plan.EnvComments = map[string]EnvComments{}
	facts := []FactRecord{{
		Kind:           FactKindPorterChange,
		Topic:          "bespoke-api-helper-utility-xxx",
		Service:        "api",
		RecordedAt:     "2026-05-13T10:00:00Z",
		CandidateClass: "framework-quirk",
		Why:            "Truly framework-only utility",
	}}
	slim := SlimFinding{
		Surface:                "S5",
		Scope:                  "api",
		ItemReference:          "KB #1",
		SurfaceTestFailureMode: "REWRITE",
		Topic:                  "bespoke-api-helper-utility-xxx",
		FactRef:                "bespoke-api-helper-utility-xxx@2026-05-13T10:00:00Z",
		Rationale:              "framework-only knowledge",
	}
	env := &FindingsEnvelope{Findings: []SlimFinding{slim}}
	out := EnrichFindingsWithPlan(env, facts, plan)
	if len(out.Findings) != 1 {
		t.Fatalf("expected 1 enriched finding; got %d", len(out.Findings))
	}
	got := out.Findings[0]
	// Existing auto-override behavior: framework-quirk → DROP.
	if got.SurfaceTestFailureMode != "DROP" {
		t.Errorf("expected auto-override to DROP on unambiguous framework-quirk; got %q",
			got.SurfaceTestFailureMode)
	}
	if got.ClassificationAmbiguous {
		t.Errorf("ClassificationAmbiguous must be false when auto-override fires; got true")
	}
	if got.AutoOverrideRefused != "" {
		t.Errorf("AutoOverrideRefused must be empty when override fires; got %q", got.AutoOverrideRefused)
	}
	if got.Classification != "" {
		t.Errorf("Classification must remain empty under DISCARD override; got %q", got.Classification)
	}
}

// TestEnrichFindings_RespectsItemAValidation — Item A's pre-mutation
// idKey validation must still fire BEFORE Item G's ambiguity check.
// Refusal on bad walked-ledger key leaves prior session ledger
// untouched even when the findings would otherwise hit Item G's
// refusal path.
func TestEnrichFindings_RespectsItemAValidation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Fragments = map[string]string{
		"codebase/worker/integration-guide/2": `### Rolling deploys / minContainers
The worker uses createApplicationContext for rolling-deploy SIGTERM survival.`,
		"codebase/worker/knowledge-base": `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **Queue group survives rolling deploys** — minContainers >= 2 plus zero-downtime SIGTERM handling.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->`,
		"codebase/worker/zerops-yaml": `zerops:
  - setup: worker
`,
	}
	sess.OutputRoot = dir

	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	priorWalked := append([]string(nil), manifest.AllKeys()...)
	sess.Refinement2Ledger = &Refinement2Ledger{
		Walked:                        priorWalked,
		CrossSurfaceUniquenessScanned: len(priorWalked),
	}

	// Append a fact so the ambiguity branch could activate.
	if err := sess.FactsLog.Append(FactRecord{
		Kind:             FactKindPorterChange,
		Topic:            "rolling-deploys",
		Service:          "worker",
		RecordedAt:       "2026-05-13T10:00:00Z",
		CandidateClass:   "framework-quirk",
		CandidateSurface: "CODEBASE_IG",
		Why:              "createApplicationContext SIGTERM survival",
	}); err != nil {
		t.Fatalf("Append fact: %v", err)
	}

	// Walked array contains a bad key — Item A must refuse and short-
	// circuit BEFORE the ambiguity branch even runs.
	in := RecipeInput{
		Action: "enrich-findings",
		Findings: &FindingsEnvelope{Findings: []SlimFinding{{
			Surface:                "S4",
			Scope:                  "worker",
			ItemReference:          "IG #2",
			SurfaceTestFailureMode: "REWRITE",
			Topic:                  "rolling-deploys",
			FactRef:                "rolling-deploys@2026-05-13T10:00:00Z",
			Rationale:              "audit thinks quirk; surface test holds",
		}}},
		Walked: []string{"definitely-not-a-real-idkey"},
	}
	r := enrichFindingsAction(sess, in, RecipeResult{Action: "enrich-findings"})
	if r.OK {
		t.Fatal("Item A refusal must fire; got OK=true (Item G should not run when Item A refuses)")
	}
	if !strings.Contains(r.Error, ".refinement-2-manifest.json") {
		t.Errorf("expected Item A refusal message naming manifest path; got %q", r.Error)
	}
	// Prior ledger preserved.
	if got := len(sess.Refinement2Ledger.Walked); got != len(priorWalked) {
		t.Errorf("prior ledger Walked len mutated by Item-A-refused call: got %d, want %d",
			got, len(priorWalked))
	}
}
