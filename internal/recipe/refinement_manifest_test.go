package recipe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Run-46 Item 1 — surface-manifest-as-JSON-file + walked-ledger receipt.
//
// Refinement-2 sub-agent walked partial state across run-41 → run-45 in
// five distinct ways. Run-45 forensic: the brief rendered absolute paths
// to codebase READMEs and the sub-agent STILL emitted 0 findings on S4/
// S5/S7 across all 3 codebases. Path-correction was monkey-patch; the
// structural fix is to remove the filesystem walk from the sub-agent's
// responsibility — engine writes a manifest enumerating every in-scope
// item, sub-agent applies the single-question test per item, returns a
// `walked` ledger that the close-gate verifies.

// TestRefinement2Manifest_EnumeratesEveryFragment — given a fixture
// plan with three codebases + EnvComments populated, the manifest
// contains every in-scope item across S3 + S4 + S5 + S7. S6 (CLAUDE.md)
// is explicitly out-of-scope per the run-46 design.
func TestRefinement2Manifest_EnumeratesEveryFragment(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	// Populate fragments for IG slots + KB + zerops-yaml across all three
	// codebases so the manifest has rows to enumerate. Three is the
	// real-world scaffold-trio shape (api + app + worker).
	plan.Fragments = map[string]string{
		"codebase/api/integration-guide/2": "## IG slot 2 — `0.0.0.0` bind\n\nBind to 0.0.0.0.",
		"codebase/api/knowledge-base":      "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml":         "zerops:\n  - setup: api\n",
		"codebase/app/integration-guide/3": "## IG slot 3 — trust proxy\n\nSet trust proxy.",
		"codebase/app/knowledge-base":      "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **L7 balancer** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/app/zerops-yaml":         "zerops:\n  - setup: app\n",
		"codebase/worker/knowledge-base":   "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **SIGTERM drain** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/worker/zerops-yaml":      "zerops:\n  - setup: worker\n",
	}

	m, err := BuildRefinement2Manifest(plan)
	if err != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", err)
	}

	// S3 — tier yaml comments. EnvComments populated for tiers 0 + 5;
	// each tier carries a project-block + several per-service blocks.
	if len(m.TierYAMLComments) == 0 {
		t.Fatal("S3 tier-yaml-comments empty; expected tier 0 + 5 populated")
	}
	for _, tierIdx := range []string{"0", "5"} {
		entries, ok := m.TierYAMLComments[tierIdx]
		if !ok {
			t.Errorf("tier %s missing from manifest", tierIdx)
			continue
		}
		if len(entries) == 0 {
			t.Errorf("tier %s has zero entries", tierIdx)
		}
	}

	// S4 — codebase IG. Engine-emitted IG slot 1 (the zerops-yaml mirror)
	// is intentionally NOT in the manifest — IG #1 routes to the
	// underlying yaml fragment per audit_checklist.md. Manifest enumerates
	// the agent-authored slots only.
	if len(m.CodebaseIG) != 3 {
		t.Errorf("S4 CodebaseIG should cover all 3 codebases (api+app+worker), got %d", len(m.CodebaseIG))
	}

	// S5 — codebase KB. Every codebase MUST appear (even worker with a
	// single bullet — the walked ledger needs to confirm the sub-agent
	// looked at it).
	if len(m.CodebaseKB) != 3 {
		t.Errorf("S5 CodebaseKB should cover all 3 codebases, got %d", len(m.CodebaseKB))
	}

	// S7 — codebase zerops.yaml. Same coverage requirement as KB.
	if len(m.CodebaseZeropsYAML) != 3 {
		t.Errorf("S7 CodebaseZeropsYAML should cover all 3 codebases, got %d", len(m.CodebaseZeropsYAML))
	}

	// S6 CLAUDE.md MUST NOT be in the manifest per run-46 design (CLAUDE.md
	// is repo-local, not published; refinement effort wasted). The Go type
	// has no CodebaseCLAUDE field; this assertion is structural.
	marshaled, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if strings.Contains(string(marshaled), "claudeMd") || strings.Contains(string(marshaled), "CodebaseCLAUDE") {
		t.Errorf("manifest must not enumerate S6 CLAUDE.md per run-46 design; got JSON: %s", marshaled)
	}
}

// TestRefinement2Manifest_CarriesG2Metadata — Run-46 Item 1 G2-followup.
// The codex review found Refinement2ManifestEntry missing the
// downstream-gate metadata fields the plan promised:
// TopicCandidates, PorterTunableDirectives, PerFieldComments. This test
// pins the shape — S3 entries carry TopicCandidates from citation-
// keyword detection on the comment body AND PorterTunableDirectives
// when the comment body carries yaml-shape annotations; S4 entries
// carry topic candidates when detectable; S7 entries carry both
// PorterTunableDirectives + PerFieldComments.
func TestRefinement2Manifest_CarriesG2Metadata(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	// S3 — override the tier-0 EnvComments with a service block whose
	// comment body literally carries rolling-deploys citation keywords
	// AND a yaml-shape `  minContainers: 2` annotation. The audit
	// reads tier-yaml comment bodies for both topic-candidate routing
	// + porter-tunable enumeration (per refinement_manifest.go S3
	// branch). A second service block (`db`) carries plain prose with
	// no annotations — manifest must NOT spuriously attach metadata
	// to that entry.
	plan.EnvComments = map[string]EnvComments{
		"0": {
			Project: "AI agent workspace — dev slot per codebase.",
			Service: map[string]string{
				"apidev": "API dev — rolling-deploys with SIGTERM drain;\n" +
					"once traffic warrants it bump:\n" +
					"  minContainers: 2\n" +
					"so zero-downtime rolling-deploys land cleanly.",
				"db": "Postgres for the greetings table.",
			},
		},
		"5": plan.EnvComments["5"],
	}

	// S4 IG slot — body contains rolling-deploys keywords (minContainers
	// + zero-downtime + SIGTERM) so DetectBulletTopicCandidates returns
	// the rolling-deploys family.
	plan.Fragments = map[string]string{
		"codebase/api/integration-guide/2": `## IG slot 2 — SIGTERM and rolling-deploys

minContainers≥2 enables zero-downtime rolling-deploys with SIGTERM drain.`,
		"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **L7 balancer trust proxy** — trust-proxy.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml": `zerops:
  - setup: api
    run:
      base: nodejs@22
      # Bump minContainers to 4 once your traffic profile warrants it
      # — the L7 balancer load tests on stage show 2 handles ~1k rps.
      minContainers: 2
      verticalAutoscaling:
        # Feel free to swap minRam upward once container metrics show
        # steady-state heap above 75% of this floor.
        minRam: 0.5
`,
		"codebase/app/zerops-yaml":       "zerops:\n  - setup: app\n",
		"codebase/app/knowledge-base":    "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **App** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/worker/zerops-yaml":    "zerops:\n  - setup: worker\n",
		"codebase/worker/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **Worker** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
	}

	m, err := BuildRefinement2Manifest(plan)
	if err != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", err)
	}

	// S3 — tier-0 entries enumerated. Find the apidev service block.
	// The comment body contains "rolling-deploys" + "SIGTERM" +
	// "zero-downtime" + a yaml-shape `  minContainers: 2` line, so:
	//   - TopicCandidates MUST contain "rolling-deploys"
	//   - PorterTunableDirectives MUST contain "minContainers"
	tier0Entries, ok := m.TierYAMLComments["0"]
	if !ok || len(tier0Entries) == 0 {
		t.Fatal("S3 tier-0 entries empty; manifest should enumerate apidev + db comment blocks")
	}
	var s3ApidevEntry, s3DbEntry *Refinement2ManifestEntry
	for i := range tier0Entries {
		switch tier0Entries[i].Service {
		case "apidev":
			s3ApidevEntry = &tier0Entries[i]
		case "db":
			s3DbEntry = &tier0Entries[i]
		}
	}
	if s3ApidevEntry == nil {
		t.Fatal("S3 tier-0 apidev entry not found")
	}
	if !slices.Contains(s3ApidevEntry.TopicCandidates, "rolling-deploys") {
		t.Errorf("S3 apidev TopicCandidates must include rolling-deploys (body carries SIGTERM + zero-downtime + minContainers); got %v",
			s3ApidevEntry.TopicCandidates)
	}
	if !slices.Contains(s3ApidevEntry.PorterTunableDirectives, "minContainers") {
		t.Errorf("S3 apidev PorterTunableDirectives must include minContainers (comment body carries the yaml-shape annotation); got %v",
			s3ApidevEntry.PorterTunableDirectives)
	}
	// Negative-shape: the db block is plain prose without citation
	// keywords or yaml-shape annotations — must NOT receive spurious
	// metadata.
	if s3DbEntry == nil {
		t.Fatal("S3 tier-0 db entry not found")
	}
	if len(s3DbEntry.TopicCandidates) != 0 {
		t.Errorf("S3 db TopicCandidates must be empty for plain-prose comment; got %v",
			s3DbEntry.TopicCandidates)
	}
	if len(s3DbEntry.PorterTunableDirectives) != 0 {
		t.Errorf("S3 db PorterTunableDirectives must be empty for plain-prose comment; got %v",
			s3DbEntry.PorterTunableDirectives)
	}

	// S4 — IG slot 2 body carries SIGTERM + zero-downtime + minContainers,
	// so TopicCandidates should include "rolling-deploys".
	apiIG := m.CodebaseIG["api"]
	if len(apiIG) == 0 {
		t.Fatal("api IG entries empty")
	}
	var igGotTopic bool
	for _, e := range apiIG {
		if slices.Contains(e.TopicCandidates, "rolling-deploys") {
			igGotTopic = true
			break
		}
	}
	if !igGotTopic {
		t.Errorf("S4 IG entries must carry TopicCandidates with rolling-deploys; got entries=%+v", apiIG)
	}

	// S7 — api yaml has minContainers + verticalAutoscaling.minRam (two
	// porter-tunable directives) AND per-field comments adjacent to each.
	apiYaml := m.CodebaseZeropsYAML["api"]
	if len(apiYaml) == 0 {
		t.Fatal("api S7 yaml entry empty")
	}
	if len(apiYaml[0].PorterTunableDirectives) < 2 {
		t.Errorf("S7 PorterTunableDirectives should list ≥2 fields (minContainers + verticalAutoscaling.minRam); got %v",
			apiYaml[0].PorterTunableDirectives)
	}
	if len(apiYaml[0].PerFieldComments) < 2 {
		t.Errorf("S7 PerFieldComments should carry ≥2 entries with comment lines; got %v",
			apiYaml[0].PerFieldComments)
	}
	for _, pfc := range apiYaml[0].PerFieldComments {
		if !pfc.PorterTunable {
			t.Errorf("S7 PerFieldComments entry PorterTunable must be true for tunable fields; got %+v", pfc)
		}
		if len(pfc.CommentLines) == 0 {
			t.Errorf("S7 PerFieldComments entry %q should carry adjacent comment block; got empty", pfc.Field)
		}
	}
}

// TestWriteRefinement2Manifest_PersistsJSON — manifest is written to
// `<outputRoot>/.refinement-2-manifest.json` so the sub-agent can Read
// it. Path is fixed (no per-dispatch suffix) — the manifest is
// regenerated on every dispatch.
func TestWriteRefinement2Manifest_PersistsJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	plan := syntheticShowcasePlan()
	plan.Fragments = map[string]string{
		"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml":    "zerops:\n  - setup: api\n",
	}

	path, err := WriteRefinement2Manifest(dir, plan)
	if err != nil {
		t.Fatalf("WriteRefinement2Manifest: %v", err)
	}
	want := filepath.Join(dir, ".refinement-2-manifest.json")
	if path != want {
		t.Errorf("manifest path = %q, want %q", path, want)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var got Refinement2Manifest
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if len(got.CodebaseKB["api"]) == 0 {
		t.Error("manifest written but missing api KB entries")
	}
}

// TestBuildRefinement2Brief_TeachesManifestRead — the rendered brief
// instructs the sub-agent to read the manifest FIRST and lists the
// canonical path. Pillar A pin.
func TestBuildRefinement2Brief_TeachesManifestRead(t *testing.T) {
	t.Parallel()
	plan := &Plan{
		Slug: "manifest-pin",
		Codebases: []Codebase{
			{Hostname: "api", Role: RoleAPI, BaseRuntime: "nodejs@22"},
		},
	}
	brief, err := BuildRefinement2Brief(plan, nil, "/run/dir", nil)
	if err != nil {
		t.Fatalf("BuildRefinement2Brief: %v", err)
	}
	for _, want := range []string{
		".refinement-2-manifest.json",
		"Read",
		"walked",
	} {
		if !strings.Contains(brief.Body, want) {
			t.Errorf("rendered refinement-2 brief missing manifest-instruction anchor %q", want)
		}
	}
}

// TestRefinement2CloseGate_RefusesIncompleteWalkedLedger — close-gate
// fails when the recorded walked ledger covers < 100% of the manifest.
// `walked` ledger lives on session state (mutated via the
// `record-refinement2-ledger` action — added in this PR as part of
// Item 1's manifest infrastructure).
func TestRefinement2CloseGate_RefusesIncompleteWalkedLedger(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.Plan.Fragments = map[string]string{
		"codebase/api/knowledge-base":    "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml":       "zerops:\n  - setup: api\n",
		"codebase/app/knowledge-base":    "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **L7 balancer** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/app/zerops-yaml":       "zerops:\n  - setup: app\n",
		"codebase/worker/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **SIGTERM drain** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/worker/zerops-yaml":    "zerops:\n  - setup: worker\n",
	}

	manifest, err := BuildRefinement2Manifest(sess.Plan)
	if err != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", err)
	}

	// Record a ledger covering only ONE item — the close-gate must refuse.
	var firstKey string
	for k, entries := range manifest.CodebaseKB {
		if len(entries) > 0 {
			firstKey = "codebase_kb:" + k + ":0"
			break
		}
	}
	if firstKey == "" {
		t.Fatal("test fixture: manifest CodebaseKB has no entries")
	}

	ledger := &Refinement2Ledger{
		Walked: []string{firstKey},
	}
	missing := ledger.Missing(manifest)
	if len(missing) == 0 {
		t.Fatal("expected Missing() to return uncovered manifest items; got none")
	}
}

// TestCompletePhaseRefinement_RefusesMissingLedger — completePhase
// refuses the refinement-phase close when sess.Refinement2Ledger is
// nil. The Error message names the walked-ledger contract so the main
// agent can fix the dispatch.
func TestCompletePhaseRefinement_RefusesMissingLedger(t *testing.T) {
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
	// Refinement2Ledger left nil.
	sess.Completed[PhaseRefinement] = false
	sess.Current = PhaseRefinement

	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseRefinement)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if r.OK {
		t.Fatal("expected ok=false when walked-ledger missing; got OK=true")
	}
	for _, want := range []string{"walked-ledger", "Run-46 Item 1"} {
		if !strings.Contains(r.Error, want) {
			t.Errorf("Error message missing walked-ledger anchor %q; got %q", want, r.Error)
		}
	}
}

// TestCompletePhaseRefinement_RefusesIncompleteWalkedLedger — the close
// fails when the ledger is set but covers fewer than 100% of manifest
// entries. Error includes a coverage count + an example list of missing
// idKeys.
func TestCompletePhaseRefinement_RefusesIncompleteWalkedLedger(t *testing.T) {
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
	// Partial coverage — walk only one item.
	manifest, mErr := BuildRefinement2Manifest(sess.Plan)
	if mErr != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", mErr)
	}
	all := manifest.AllKeys()
	if len(all) < 2 {
		t.Fatalf("test fixture: manifest must have ≥ 2 keys to exercise partial coverage; got %d", len(all))
	}
	sess.Refinement2Ledger = &Refinement2Ledger{Walked: all[:1]}
	sess.Completed[PhaseRefinement] = false
	sess.Current = PhaseRefinement

	in := RecipeInput{Action: "complete-phase", Phase: string(PhaseRefinement)}
	r := completePhase(sess, in, RecipeResult{Action: "complete-phase"})
	if r.OK {
		t.Fatal("expected ok=false on partial walked-ledger; got OK=true")
	}
	if !strings.Contains(r.Error, "un-walked") {
		t.Errorf("Error should explain partial coverage; got %q", r.Error)
	}
}

// TestEnrichFindings_PersistsLedgerOnSession — `enrich-findings` writes
// the `walked` payload to sess.Refinement2Ledger. This is the wire-up
// the close-gate consumes.
func TestEnrichFindings_PersistsLedgerOnSession(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	log := OpenFactsLog(filepath.Join(dir, "facts.jsonl"))
	sess := NewSession("synth-showcase", "dev", log, dir, nil)
	sess.Plan = syntheticShowcasePlan()
	sess.OutputRoot = dir

	in := RecipeInput{
		Action:                        "enrich-findings",
		Findings:                      &FindingsEnvelope{Findings: nil},
		Walked:                        []string{"codebase_kb:api:0", "codebase_kb:app:0"},
		CrossSurfaceUniquenessScanned: 2,
		Duplicates:                    []string{"api:kb#0 ↔ app:kb#0"},
	}
	r := enrichFindingsAction(sess, in, RecipeResult{Action: "enrich-findings"})
	if !r.OK {
		t.Fatalf("enrich-findings returned Error=%q", r.Error)
	}
	if sess.Refinement2Ledger == nil {
		t.Fatal("sess.Refinement2Ledger should be set after enrich-findings with walked input")
	}
	if got := len(sess.Refinement2Ledger.Walked); got != 2 {
		t.Errorf("Refinement2Ledger.Walked len = %d, want 2", got)
	}
	if got := sess.Refinement2Ledger.CrossSurfaceUniquenessScanned; got != 2 {
		t.Errorf("Refinement2Ledger.CrossSurfaceUniquenessScanned = %d, want 2", got)
	}
}

// TestRefinement2Ledger_FullCoverageReturnsEmpty — a ledger that names
// every manifest entry yields Missing() == empty; the close-gate path
// passes.
func TestRefinement2Ledger_FullCoverageReturnsEmpty(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	plan.Fragments = map[string]string{
		"codebase/api/knowledge-base": "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->\n- **APP_SECRET shadow** — body.\n<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->",
		"codebase/api/zerops-yaml":    "zerops:\n  - setup: api\n",
	}
	plan.Codebases = plan.Codebases[:1] // shrink to one codebase so the keys list stays manageable
	plan.EnvComments = nil              // S3 empty; manifest still enumerates declared codebases

	manifest, err := BuildRefinement2Manifest(plan)
	if err != nil {
		t.Fatalf("BuildRefinement2Manifest: %v", err)
	}
	all := manifest.AllKeys()
	if len(all) == 0 {
		t.Fatal("test fixture: manifest AllKeys() empty")
	}
	ledger := &Refinement2Ledger{Walked: all}
	if missing := ledger.Missing(manifest); len(missing) != 0 {
		t.Errorf("full coverage ledger reported missing=%v", missing)
	}
}
