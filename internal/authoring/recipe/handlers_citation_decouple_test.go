package recipe

import (
	"path/filepath"
	"testing"
)

// Run-52 Fix 3 (defense-in-depth) — citation validators independent of
// the bare-yaml gate. The self-validate scoped switch picks the gate set
// by sess.Current; at PhaseFeature it selects FeatureGates(), which does
// NOT include gateCodebaseSurfaceValidators (the citation validators,
// registered only in CodebaseContentGates()). So a feature-phase
// self-validate on a codebase that already carries IG/KB content
// fragments never ran the citation checks. Fix 2 prevents the
// wrong-phase dispatch on the primary path; this gate is the
// defense-in-depth backstop: when a scaffold/feature scoped close runs on
// a codebase whose IG/KB fragments are present, append the citation gate.

// TestCompletePhaseScoped_FeaturePhase_RunsCitationValidatorsOnContentFragment
// is the run-52 Fix-3 RED test. With a clean bare yaml (scaffold-bare-yaml
// passes) and a KB content fragment present that mentions a CitationMap
// topic without citing the guide, the feature-phase scoped close must
// surface kb-citation-missing.
func TestCompletePhaseScoped_FeaturePhase_RunsCitationValidatorsOnContentFragment(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.OutputRoot = dir
	stageScaffoldYAMLs(t, dir, sess.Plan)
	// KB body mentions `minContainers` (a CitationMap topic mapping to the
	// `rolling-deploys` guide) but never cites the guide id / URL — the
	// citation validator must fire.
	sess.Plan.Fragments = map[string]string{
		"codebase/api/knowledge-base": "### Gotchas\n\n- **Replica restart drops work** — set `minContainers` to keep the queue draining; the worker rebinds on boot.\n",
	}
	forcePhase(sess, PhaseFeature)

	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseFeature), Codebase: "api"}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if !containsCode(r.Violations, "kb-citation-missing") {
		t.Errorf("feature-phase scoped close on a codebase with a KB content fragment must run the citation validators (expected kb-citation-missing); got %v", codeSet(r.Violations))
	}
}

// TestCompletePhaseScoped_FeaturePhase_NoContentFragment_SkipsCitationValidators
// is the no-false-positive companion: with no IG/KB content fragment
// recorded for the codebase, the citation gate is not appended, so a
// clean bare-yaml feature close does not surface a citation violation.
func TestCompletePhaseScoped_FeaturePhase_NoContentFragment_SkipsCitationValidators(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.OutputRoot = dir
	stageScaffoldYAMLs(t, dir, sess.Plan)
	// No codebase content fragments recorded — pure scaffold/feature state.
	forcePhase(sess, PhaseFeature)

	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseFeature), Codebase: "api"}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if containsCode(r.Violations, "kb-citation-missing") {
		t.Errorf("feature-phase scoped close with NO content fragment must not run citation validators (false positive); got %v", codeSet(r.Violations))
	}
}
