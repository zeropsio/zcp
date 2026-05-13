package recipe

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// run47_substrate_fixes_test.go — Run-47 Items A + B + C pin tests.
//
// Item A — idKey-format validation at the enrich-findings boundary.
// Item B — preserve URL fragment in lookupGuideIDFromURL.
// Item C — counter consistency on cross-surface uniqueness receipt.
//
// Plan: plans/run-47-substrate-fixes.md.

// --------------------------------------------------------------------
// Item A — idKey-format validation at enrich-findings.
// --------------------------------------------------------------------

// TestEnrichFindings_RejectsInvalidWalkedIDKey — the sub-agent emits a
// free-form id like `S5:codebase/api/knowledge-base/<name>` instead of
// the manifest grammar `codebase_kb:api:<empty>`. enrich-findings must
// refuse at the boundary with the grammar echoed.
func TestEnrichFindings_RejectsInvalidWalkedIDKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Fragments = map[string]string{
		"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml":    "zerops:\n  - setup: api\n",
	}
	sess.OutputRoot = dir

	in := RecipeInput{
		Action:   "enrich-findings",
		Findings: &FindingsEnvelope{Findings: nil},
		Walked: []string{
			"S5:codebase/api/knowledge-base/nats-auth-violation",
		},
	}
	r := enrichFindingsAction(sess, in, RecipeResult{Action: "enrich-findings"})
	if r.OK {
		t.Fatal("expected ok=false on invalid walked idKey; got OK=true")
	}
	for _, want := range []string{
		"codebase_kb:",
		"codebase_ig:",
		"codebase_zerops_yaml:",
		"tier_yaml_comments:",
		"S5:codebase/api/knowledge-base/nats-auth-violation",
	} {
		if !strings.Contains(r.Error, want) {
			t.Errorf("Error must echo grammar/invalid key anchor %q; got %q", want, r.Error)
		}
	}
}

// TestEnrichFindings_AcceptsValidWalkedIDKey — every walked key matches
// the manifest grammar; enrich-findings succeeds.
func TestEnrichFindings_AcceptsValidWalkedIDKey(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Fragments = map[string]string{
		"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml":    "zerops:\n  - setup: api\n",
	}
	sess.OutputRoot = dir

	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	all := manifest.AllKeys()
	if len(all) == 0 {
		t.Fatal("test fixture: manifest AllKeys() empty")
	}

	in := RecipeInput{
		Action:   "enrich-findings",
		Findings: &FindingsEnvelope{Findings: nil},
		Walked:   all,
	}
	r := enrichFindingsAction(sess, in, RecipeResult{Action: "enrich-findings"})
	if !r.OK {
		t.Fatalf("expected ok=true on valid walked keys; got Error=%q", r.Error)
	}
	if sess.Refinement2Ledger == nil || len(sess.Refinement2Ledger.Walked) != len(all) {
		t.Errorf("expected ledger walked-len=%d; got %v", len(all), sess.Refinement2Ledger)
	}
}

// TestEnrichFindings_RefusalMessageNamesManifestPath — refusal message
// names `.refinement-2-manifest.json` so the sub-agent knows where to
// find the canonical idKey enumeration.
func TestEnrichFindings_RefusalMessageNamesManifestPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Fragments = map[string]string{
		"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml":    "zerops:\n  - setup: api\n",
	}
	sess.OutputRoot = dir

	in := RecipeInput{
		Action:   "enrich-findings",
		Findings: &FindingsEnvelope{Findings: nil},
		Walked:   []string{"not-a-real-key"},
	}
	r := enrichFindingsAction(sess, in, RecipeResult{Action: "enrich-findings"})
	if r.OK {
		t.Fatal("expected ok=false on invalid walked idKey; got OK=true")
	}
	if !strings.Contains(r.Error, ".refinement-2-manifest.json") {
		t.Errorf("Error must name the manifest path so sub-agent knows where to look; got %q", r.Error)
	}
}

// TestEnrichFindings_RefusalDoesNotCorruptPriorLedger — a session
// carrying a valid prior ledger is NOT mutated when a refused walked
// array hits. Pins that validation runs BEFORE the latest-wins
// sess.Refinement2Ledger assignment.
func TestEnrichFindings_RefusalDoesNotCorruptPriorLedger(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Fragments = map[string]string{
		"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml":    "zerops:\n  - setup: api\n",
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

	in := RecipeInput{
		Action:   "enrich-findings",
		Findings: &FindingsEnvelope{Findings: nil},
		Walked: []string{
			"S5:bogus", // invalid grammar — must refuse before overwrite
		},
	}
	r := enrichFindingsAction(sess, in, RecipeResult{Action: "enrich-findings"})
	if r.OK {
		t.Fatal("expected ok=false on invalid walked idKey; got OK=true")
	}
	if sess.Refinement2Ledger == nil {
		t.Fatal("prior ledger was wiped by a refused call")
	}
	if got := len(sess.Refinement2Ledger.Walked); got != len(priorWalked) {
		t.Errorf("prior ledger Walked length mutated: got %d, want %d", got, len(priorWalked))
	}
	for i, want := range priorWalked {
		if sess.Refinement2Ledger.Walked[i] != want {
			t.Errorf("prior ledger Walked[%d] mutated: got %q, want %q", i, sess.Refinement2Ledger.Walked[i], want)
			break
		}
	}
	if sess.Refinement2Ledger.CrossSurfaceUniquenessScanned != len(priorWalked) {
		t.Errorf("prior ledger CrossSurfaceUniquenessScanned mutated: got %d, want %d",
			sess.Refinement2Ledger.CrossSurfaceUniquenessScanned, len(priorWalked))
	}
}

// --------------------------------------------------------------------
// Item B — preserve URL fragment in lookupGuideIDFromURL.
// --------------------------------------------------------------------

// TestLookupGuideIDFromURL_PreservesFragmentAnchor — the three
// fragment-distinguished entries on the same canonical path
// `/zerops-yaml/specification` (#initcommands- / #deployfiles- /
// #envvariables-) must each resolve to their own distinct guide id.
func TestLookupGuideIDFromURL_PreservesFragmentAnchor(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		url       string
		wantGuide string
	}{
		{
			name:      "init-commands",
			url:       "https://docs.zerops.io/zerops-yaml/specification#initcommands-",
			wantGuide: "init-commands",
		},
		{
			name:      "deploy-files",
			url:       "https://docs.zerops.io/zerops-yaml/specification#deployfiles-",
			wantGuide: "deploy-files",
		},
		{
			name:      "env-variables",
			url:       "https://docs.zerops.io/zerops-yaml/specification#envvariables-",
			wantGuide: "env-var-model",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := lookupGuideIDFromURL(tc.url)
			if got != tc.wantGuide {
				t.Errorf("lookupGuideIDFromURL(%q) = %q, want %q", tc.url, got, tc.wantGuide)
			}
		})
	}
}

// TestLookupGuideIDFromURL_NoFragmentReturnsFamily — when a citation
// map URL itself carries NO fragment (e.g. rolling-deploys at
// `docs.zerops.io/features/scaling-ha`), a fragment-less link URL
// matches it; a fragment-bearing link URL ALSO matches the same path
// (the no-fragment-map rule). Preserves prior behavior for fragment-
// less map entries while the fragment-distinguished entries get the
// new distinguishing match path.
func TestLookupGuideIDFromURL_NoFragmentReturnsFamily(t *testing.T) {
	t.Parallel()
	// Map entry rolling-deploys = `docs.zerops.io/features/scaling-ha`
	// (no fragment). A link URL with no fragment should match it.
	if got := lookupGuideIDFromURL("https://docs.zerops.io/features/scaling-ha"); got != "rolling-deploys" {
		t.Errorf("no-fragment URL on no-fragment map entry: got %q, want rolling-deploys", got)
	}
	// A URL pointing at the same path with an in-page anchor (no map
	// fragment) should also match.
	if got := lookupGuideIDFromURL("https://docs.zerops.io/features/scaling-ha#high-availability-via-rolling-deploys"); got != "rolling-deploys" {
		t.Errorf("fragment-on-no-fragment-map URL: got %q, want rolling-deploys", got)
	}
	// A URL pointing at the spec path with no fragment, when the map's
	// spec-path entries are ALL fragment-distinguished, must NOT match
	// any specific fragment-bearing entry (would require collapsing
	// fragments — the exact bug we're fixing).
	got := lookupGuideIDFromURL("https://docs.zerops.io/zerops-yaml/specification")
	if got == "init-commands" || got == "deploy-files" || got == "env-var-model" {
		t.Errorf("fragment-less spec URL collapsed to fragment-bearing guide %q; want \"\"", got)
	}
}

// TestKBCitationDisplayMismatch_DeployFilesURLAccepted — end-to-end:
// a KB body citing the deploy-files guide via friendly display name +
// canonical `#deployfiles-` URL must no longer trigger
// `kb-citation-display-mismatch`. Run-46 forensic: Item 5 validator
// refused 4 legitimate `#deployfiles-` cites because
// lookupGuideIDFromURL collapsed fragments and resolved them to
// init-commands, producing a phantom display↔URL mismatch.
func TestKBCitationDisplayMismatch_DeployFilesURLAccepted(t *testing.T) {
	t.Parallel()
	display := FriendlyDisplayName("deploy-files")
	if display == "" {
		t.Fatal("FriendlyDisplayName(\"deploy-files\") returned empty; fixture broken")
	}
	body := `<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->
- **Deploy files include only what runtime needs** — body.
  See [` + display + `](https://docs.zerops.io/zerops-yaml/specification#deployfiles-)
  for the tilde-suffix mechanic.
<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
`
	vs, err := validateCodebaseKB(context.Background(), "codebases/api/README.md", []byte(body), SurfaceInputs{Plan: &Plan{}})
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if containsCode(vs, "kb-citation-display-mismatch") {
		t.Errorf("deploy-files URL must resolve to deploy-files guide (no fragment-collapse); got %+v", vs)
	}
}

// --------------------------------------------------------------------
// Item C — counter consistency on cross-surface uniqueness receipt.
// --------------------------------------------------------------------

// TestRefinement2CloseGate_RefusesOverScanned — manifest total=71,
// scanned=95 → refuse, with both numbers named so the sub-agent can
// reconcile.
func TestRefinement2CloseGate_RefusesOverScanned(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, sess.Plan)
	sess.OutputRoot = dir
	if _, err := stitchContent(sess); err != nil {
		t.Fatalf("seed stitch-content: %v", err)
	}
	for _, p := range []Phase{
		PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature,
		PhaseCodebaseContent, PhaseEnvContent, PhaseFinalize, PhaseRefinement,
	} {
		if err := sess.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase(%s): %v", p, err)
		}
		sess.Completed[p] = true
	}
	sess.RefinementDispatched = true
	sess.Refinement2Dispatched = true

	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	allKeys := manifest.AllKeys()
	overCount := len(allKeys) + 5
	sess.Refinement2Ledger = &Refinement2Ledger{
		Walked:                        allKeys,
		CrossSurfaceUniquenessScanned: overCount,
	}
	sess.Completed[PhaseRefinement] = false
	sess.Current = PhaseRefinement

	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseRefinement)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if r.OK {
		t.Fatal("expected ok=false on over-scanned uniqueness count; got OK=true")
	}
	for _, want := range []string{
		"crossSurfaceUniquenessScanned",
	} {
		if !strings.Contains(r.Error, want) {
			t.Errorf("Error must name scanned-count anchor %q; got %q", want, r.Error)
		}
	}
	// Both numbers must be echoed.
	if !strings.Contains(r.Error, intStr(overCount)) {
		t.Errorf("Error must echo scanned count %d; got %q", overCount, r.Error)
	}
	if !strings.Contains(r.Error, intStr(len(allKeys))) {
		t.Errorf("Error must echo manifest total %d; got %q", len(allKeys), r.Error)
	}
}

// TestRefinement2CloseGate_RefusesWalkedScannedMismatch — walked=N,
// scanned=N+3 inside the audit's own emission (the run-46 92 vs 95
// fixture) → refuse with both numbers named.
func TestRefinement2CloseGate_RefusesWalkedScannedMismatch(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, sess.Plan)
	sess.OutputRoot = dir
	if _, err := stitchContent(sess); err != nil {
		t.Fatalf("seed stitch-content: %v", err)
	}
	for _, p := range []Phase{
		PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature,
		PhaseCodebaseContent, PhaseEnvContent, PhaseFinalize, PhaseRefinement,
	} {
		if err := sess.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase(%s): %v", p, err)
		}
		sess.Completed[p] = true
	}
	sess.RefinementDispatched = true
	sess.Refinement2Dispatched = true

	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	allKeys := manifest.AllKeys()
	// Walked covers every key; scanned-count is one greater. The
	// per-counter relations are: walked=manifest-total, scanned ≠ walked.
	// To trigger the walked-vs-scanned check (not the over-scanned one),
	// we shrink scanned to manifest-total so the over-scanned check
	// passes, then walk fewer items. Easier: walk N-1, scan N (manifest
	// total = N). The first counter check (scanned == manifest total)
	// passes; the second (walked == scanned) fires.
	if len(allKeys) < 2 {
		t.Fatalf("test fixture needs ≥ 2 manifest entries; got %d", len(allKeys))
	}
	walkedSubset := append([]string(nil), allKeys[:len(allKeys)-1]...)
	sess.Refinement2Ledger = &Refinement2Ledger{
		Walked:                        walkedSubset,
		CrossSurfaceUniquenessScanned: len(allKeys),
	}
	sess.Completed[PhaseRefinement] = false
	sess.Current = PhaseRefinement

	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseRefinement)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if r.OK {
		t.Fatal("expected ok=false on walked-vs-scanned mismatch; got OK=true")
	}
	// The walked-ledger length and scanned count must both appear.
	if !strings.Contains(r.Error, intStr(len(walkedSubset))) {
		t.Errorf("Error must echo walked length %d; got %q", len(walkedSubset), r.Error)
	}
	if !strings.Contains(r.Error, intStr(len(allKeys))) {
		t.Errorf("Error must echo scanned count %d; got %q", len(allKeys), r.Error)
	}
}

// TestRefinement2CloseGate_AcceptsConsistentCounters — walked-len ==
// scanned-count == manifest-total. The gate passes.
func TestRefinement2CloseGate_AcceptsConsistentCounters(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	stageScaffoldYAMLs(t, dir, sess.Plan)
	sess.OutputRoot = dir
	if _, err := stitchContent(sess); err != nil {
		t.Fatalf("seed stitch-content: %v", err)
	}
	for _, p := range []Phase{
		PhaseResearch, PhaseProvision, PhaseScaffold, PhaseFeature,
		PhaseCodebaseContent, PhaseEnvContent, PhaseFinalize, PhaseRefinement,
	} {
		if err := sess.EnterPhase(p); err != nil {
			t.Fatalf("EnterPhase(%s): %v", p, err)
		}
		sess.Completed[p] = true
	}
	sess.RefinementDispatched = true
	sess.Refinement2Dispatched = true

	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	allKeys := manifest.AllKeys()
	sess.Refinement2Ledger = &Refinement2Ledger{
		Walked:                        allKeys,
		CrossSurfaceUniquenessScanned: len(allKeys),
	}
	sess.Current = PhaseRefinement
	// Leave Completed[PhaseRefinement]=true so CompletePhase short-
	// circuits past the gate set (which would require disk surfaces);
	// the counter gates fire BEFORE that short-circuit.

	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseRefinement)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if !r.OK {
		t.Errorf("expected ok=true with consistent counters; got Error=%q", r.Error)
	}
}

// intStr — small helper to render an int as a decimal string for
// substring assertions on error messages.
func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
