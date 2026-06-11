package recipe

import (
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"strings"
)

// Brief cap constants. Enforced at dispatch time; the composer produces
// a Brief whose Bytes field the caller compares against the cap.
//
// Run-9-readiness raised the scaffold cap from 5 KB → 8 KB to fit the
// tranche-2 principle atoms (dev-loop, mount-vs-container, yaml-
// comment-style) alongside the existing scaffold content. Feature cap
// held at 5 KB — the feature brief adds only the v3 fact-recording
// section in tranche 1 and still fits.
//
// Run-8-readiness Workstream F raised both caps from 3 KB → 5 KB to
// accommodate the content-authoring placement rubric + the execOnce
// key-shape concept atom.
//
// The writer brief was deleted in A1: fragment authorship is pinned to
// whoever holds the densest context at the moment of authorship, not to
// a post-hoc writer sub-agent.
//
// Run-11 V-5 raised ScaffoldBriefCap from 12 KB → 14 KB to fit the
// Self-inflicted litmus subsection. Run-11 O-1 raised it again to
// 16 KB to fit the Citation map subsection (citations are author-time
// signals, not render output). Run-11 Q-2 raised FeatureBriefCap
// 10 KB → 12 KB for the per-feature commit subsection.
//
// Run-12 §E + §A raised ScaffoldBriefCap 16 → 18 KB to fit the
// rewritten Managed services section (own-key aliasing teaching) and
// the new Alias-type contracts table (TEACH-side fix for the run-11
// `https://${<host>_zeropsSubdomain}` double-prefix bug).
//
// Run-12 §C raised the cap to 20 KB to fit the rewritten CLAUDE.md
// subsection (porter-facing rule + GOOD/BAD examples + what-goes-here
// / what-doesn't lists). TEACH-side fix for the run-11 CLAUDE.md MCP-
// tool leak.
//
// Run-12 §R raised the cap to 22 KB to fit the "Correcting a fragment
// you authored" subsection (mode=replace teaching).
//
// Run-13 §T raised ScaffoldBriefCap 22 → 24 KB for the tier-fact table
// the brief composer adds when the codebase's role is frontend (the
// SPA codebase ships tier-aware prose). API/worker scaffolds still
// fit comfortably under 22 KB; the cap is a hard upper bound for the
// frontend variant.
//
// Run-13 §F raised FeatureBriefCap 12 → 16 KB to fit the showcase
// scenario specification atom (panel-per-category mandate + per-panel
// browser-verification facts). Showcase-tier feature brief lands ~14
// KB; minimal/hello-world tiers stay below 12 KB and could use a
// lower cap, but a single shared cap keeps the engine logic simple.
//
// Run-13 §G2 raised both caps to fit the self-validate-before-
// terminating teaching in scaffold's content_authoring (replaces the
// older "Correcting a fragment" subsection — broader scope, ~500
// bytes more) and feature's content_extension (~400 bytes added).
// Scaffold cap 24 → 26 KB; feature cap 16 → 18 KB.
//
// Run-14 §B.1 + §D.3 + §D.4 raised both caps. Scaffold 26 → 28 KB
// for the frontend-conditional build-tool-host-allowlist atom (D.3,
// ~700 bytes when active) plus the prior-body teaching addition in
// content_extension. Feature 18 → 20 KB for the showcase-scenario
// stable-selectors subsection (D.4, ~520 bytes) and the run-14 §B.1
// mode=replace priorBody-merge teaching.
//
// Run-15 §F.2/F.3/F.5 raised the scaffold cap 28 → 34 KB to fit the
// "record-fragment carries the surface contract" teaching, the
// "classification × surface compatibility" table, and the F.5
// validator-tripwire updates (IG/KB caps, fabricated-yaml-field rule,
// import.yaml audience-voice rule). Scaffold lands ~32 KB when the
// frontend-conditional atoms are active.
//
// Run-20 C2 + C3 raise the cap 34 → 44 KB to fit the new principles:
// cross-service-urls (~5 KB; teaches workspace dual-runtime URL
// pattern), spa_static_runtime (~3 KB; frontend-only `base: static`
// teaching), bare-yaml-prohibition (~2 KB; scaffold yaml without
// inline comments), nats-shapes (~3 KB; pub/sub vs JetStream
// dichotomy). Frontend codebases land near 42 KB.
//
// Run-21 R2-4 raised FeatureBriefCap 20 → 22 KB to absorb the
// stage-slot negative rule appended to mount-vs-container.md (feature
// brief was saturated at ~20.3 KB pre-edit). R3-2 will tighten the
// cap back down once feature/env-content briefs are slimmed.
//
// Run-23 F-21 raised FeatureBriefCap 22 → 32 KB to absorb the pass-
// split atoms: contract_authoring (~3 KB, backend pass) +
// tailwind_componentry (~4 KB, frontend pass) + integration_validator
// (~3 KB, frontend pass). Each atom only loads on its matching pass,
// so an individual brief is always under the cap. The cap covers the
// WORST-CASE per-pass load: the frontend pass on a showcase recipe
// with a UI codebase + design-system table + Tailwind atom +
// integration-validator atom; or the backend pass on a recipe with
// a worker codebase that loads worker_subscription_shape +
// contract_authoring on top of the always-loaded scaffold-residual
// atoms. Pass dispatch is now mandatory — the legacy "single dispatch
// on a non-split caller" path is gone (BuildFeatureBrief rejects
// empty pass per TestBuildFeatureBrief_RejectsEmptyPass).
//
// Run-25 dashboard-reframe raised FeatureBriefCap 32 → 40 KB to
// absorb the showcase scenario rewrite: the brief shifted from "demo
// panels in tabs" to "health-dashboard cards" and gained load-bearing
// content (visualization-vocabulary closed list, live-state fetch
// pattern, counter-delta verification protocol, expanded forbid-list
// for HTML viz primitives). The atom grew from ~7 KB to ~11 KB —
// pushed worst-case frontend-pass over the prior 32 KB cap. The
// added bytes are not slack; trimming them re-opens the loopholes
// codex code review flagged.
//
// Run-30 Fix #4 raised FeatureBriefCap 40 → 44 KB after the
// final-state screenshot teaching landed in
// `briefs/feature/integration_validator.md` (~1.3 KB once trimmed).
// Closes the run-30 ANALYSIS gap where the showcase verification path
// caught wiring regressions but missed visual defects (oval status-
// dots, missing card backgrounds, FOUC mid-render). The screenshot is
// captured at close on the wired-stage URL via `zerops_browser` →
// `["screenshot", "--full", "<outputRoot>/screenshots/dashboard-close.png"]`.
//
// Run-33 architectural fix #4 raised FeatureBriefCap 46 → 47 KB to
// absorb the forbidden-tokens-in-fact-text block in
// briefs/feature/decision_recording.md (cuts recipe-author voice
// contamination at fact record time before it propagates to porter
// prose at synthesis).
//
// Run-32 F-49 raised FeatureBriefCap 44 → 46 KB to absorb the new
// `principles/codebase-name-vs-slot-hostname.md` atom (~870 bytes) —
// pre-empts `complete-phase codebase='apidev'` traps where the
// feature agent reaches for the slot hostname it has been working
// against all phase. The teaching previously lived only in the
// refinement brief (post-trap recovery); now it loads at scaffold +
// feature + codebase-content + env-content composers.
const (
	ScaffoldBriefCap = 48 * 1024
	FeatureBriefCap  = 47 * 1024
)

// BriefKind identifies a sub-agent role. Scaffold + feature have
// always existed; finalize was added run-11 S-1 — high-volume mechanical
// fragment authoring (root + env + import-comment fragments) optionally
// dispatched to a sub-agent. Hand-typed wrappers compounded math
// errors and obsolete paths in run 10; engine-composed brief eliminates
// the wrapper-vs-engine drift.
type BriefKind string

const (
	BriefScaffold        BriefKind = "scaffold"
	BriefFeature         BriefKind = "feature"
	BriefFinalize        BriefKind = "finalize"
	BriefCodebaseContent BriefKind = "codebase-content" // run-16 §6.3
	BriefClaudeMDAuthor  BriefKind = "claudemd-author"  // run-16 §6.7a
	BriefEnvContent      BriefKind = "env-content"      // run-16 §6.3
	BriefRefinement      BriefKind = "refinement"       // run-17 §9
	BriefRefinement2     BriefKind = "refinement2"      // run-41 — cross-surface audit pass
)

// Run-16 §6.2 — content-phase brief size caps. Per-codebase brief is
// pointer-based (atoms + filtered facts + metadata + cross-include
// pointers); target ~28-44 KB. The 48 KB cap is a regression guard
// (§Risk 2) — accidental verbatim embeds get caught before dispatch.
// Run-21 bumped 40→48 KB after synthesis_workflow.md gained
// IG-scaffold-filename, KB-citation, and voice rules (~5 KB).
// Run-22 R1-RC-2 + R1-RC-4 bumped 48→52 KB after platform_principles.md
// extended the same-key shadow trap to project-level vars and
// yaml-comment-style.md added the Unicode box-drawing forbid (~1 KB
// across both atoms; both load into the codebase-content brief).
// Run-22 R2-WK-1 + R2-WK-2 bumped CodebaseContentBriefCap 52→56 KB
// after the queue-group + drain MANDATORY teaching landed (~1 KB) and
// per_tier_authoring.md gained the canonical-set-vs-per-tier-flavor
// clarification (~1.5 KB; loaded via env-content but the codebase-
// content worker variant pushed the hardest). Run-22 followup F-5 then
// split that combined atom (`showcase_tier_supplements.md`) — the
// code-shape MANDATORY moved to the feature brief
// (`worker_subscription_shape.md`) and only the KB-content shape
// (`worker_kb_supplements.md`) remains at codebase-content. The
// EnvContentBriefCap still matches because per_tier_authoring.md is
// loaded there too, and run-22 followup F-4 added the cross-service-
// urls atom (~10.5 KB) at env-content.
// Run-23 F-18 bumped CodebaseContentBriefCap 56→60 KB after the
// "common record-fragment rejections — pre-empt these" section
// landed in `phase_entry/codebase-content.md` (~1.2 KB). The
// rejection-pattern pre-warning targets the 85% iteration rate seen
// at cc-api in run-23; small-brief growth, large authoring-quality
// signal.
// Run-26 F-31 bumped CodebaseContentBriefCap 60→64 KB after the
// "Dispatch contract — pass response.prompt verbatim" section landed
// in `phase_entry/codebase-content.md` (~700 bytes) AND the engine
// began propagating cross-codebase managed-service facts into the
// codebase-content brief (variable size; tested with `nil` facts but
// the cap must headroom the populated-facts case). Closes the run-26
// apidev-NATS factuality drift where worker-scaffold findings about
// `${broker_connectionString}` never reached apidev's brief, leading
// the apidev sub-agent to fall back to atom-corpus generic guidance
// and ship a contradicting KB endorsement.
//
// Run-28 fix #6 bumped CodebaseContentBriefCap 64→68 KB after the
// "Friendly-authority voice scope — never on broken alternatives"
// section landed in `briefs/codebase-content/synthesis_workflow.md`
// (~1.7 KB including the BAD/GOOD worked example). Closes the
// run-27 workerdev case where KB + yaml-comments endorsed
// "Feel free to swap to Pattern B" on an alternative the worker's
// own facts established as broken at boot.
// Run-29 Fix #4 — synthesis_workflow.md gained the surface-ownership +
// authoring-order section (~3 KB) so the codebase-content composer's
// construction-time cap moves 68 → 72 KB. NOTE the cap-treadmill
// warning above no longer applies to inline DELIVERY: Run-29 Fix #1
// introduced BriefDiskFallbackThreshold (40 KB), so any composed brief
// > 40 KB lands on disk regardless. CodebaseContentBriefCap is now a
// construction-side sanity check that protects the composer from
// unbounded growth, not a delivery-channel constraint.
// Run-29 Fix #4 follow-up F-34 — synthesis_workflow.md gained the
// "Cross-reference is not a license to restate" section with workerdev
// NATS BAD/GOOD worked example (~1.5 KB). Cap bumped 72 → 76 KB. Same
// disk-fallback note applies — this is sanity-check headroom, not
// delivery-channel constraint.
//
// Run-30 Fix #1 PARTIAL — caps re-pinned against the Read-tool 25K-token
// ceiling, not raw bytes. v9.71.0 bumped CodebaseContentBriefCap 72→76
// KB in BYTES; with markdown-dense briefs at the empirically observed
// ~2.5 chars/token, 76 KB encodes to ~30K real tokens — over the
// Read-tool ceiling. Run-30 sub-agents (cc-api 35427 tokens / cc-worker
// 35014 / refinement 32297) tripped the ceiling on first Read of the
// disk-stored brief. Cap shrunk 76 → 66 KB for codebase-content;
// EnvContentBriefCap held at 56 KB (already comfortably under).
//
// Run-30 Fix #1 follow-up Fix 2 — the per-byte estimator was migrated
// from len/3 to len/2 after the 3.0 cpt assumption was contradicted by
// observed 2.51 cpt worst-case (see charsPerTokenEstimate doc below).
// The token-ceiling test was re-derived against the Read-tool 25K real
// cap — `ReadToolTokenCeiling` is now 33000 (estimated tokens at len/2
// = ~62.5 KB bytes = ~25K real tokens at observed 2.5 cpt). The byte
// caps below were NOT shrunk in this fix because they remain composer-
// side sanity guards that prevent unbounded growth; the token-ceiling
// test is now the authoritative Read-tool guard.
//
// The `TestBrief_StaysUnderReadToolTokenCeiling` test runs against a
// real-slug plan so the embedded-parent fallback exercises the
// production load shape that bloated run-30 briefs past the ceiling.
//
// Run-31 Fix #1 closure (multi-file architecture). The single-file
// caps `CodebaseContentBriefCap` (66 KB) and `EnvContentBriefCap`
// (56 KB) are gone; under realistic fact corpora (run-31 nestjs-
// showcase: 142 facts, ~93 in cc-api scope), real briefs land at
// 78-94 KB / 36-39K real tokens — the synthetic-nil-facts unit
// fixture had been hiding the production load shape. The replacement
// is `PerPartTokenCeiling` (22000 estimated tokens, ≈44 KB bytes,
// `PartFileCap`), enforced by the part-writer abstraction in
// `briefs_parts.go` and consumed by the multi-file composers in
// `briefs_content_phase_multifile.go` (codebase-content, env-content,
// refinement). Single-file caps no longer apply for those three kinds;
// the brief as a whole is unbounded, each part fits under the Read-
// tool 25K-token cap by construction. See `plans/multi-file-briefs.md`
// for the architectural rationale and
// `briefs_multi_file_test.go::TestBuildCodebaseContentBrief_MultiFile_RealSlug_PartsUnderCap`
// for the per-part invariant pin.
const (
	ClaudeMDBriefCap = 8 * 1024 // §Risk 7 — Zerops-free brief stays small by construction
)

// charsPerTokenEstimate is a conservative chars-per-token proxy used by
// EstimateTokens. Run-30 sub-agents reported real Read-tool token
// counts of 35427 / 35014 / 32297 against on-disk briefs of 88844 /
// 88004 / 83447 bytes — i.e. ~2.51 / 2.51 / 2.58 chars/token at worst.
// English markdown averages 3.5–4.0 cpt, but markdown-dense briefs with
// code fences, JSON examples, and yaml blocks compress to ~2.5 cpt.
// We use 2 (rounded down from 2.51 for safety margin) as a pessimistic
// floor that beats the observed worst case. No real tokenizer
// dependency — the estimator is a guard against the Read-tool ceiling,
// not a precise count. Run-30 follow-up Fix 2 changed this 3 → 2 after
// /3 silently passed the cap when real Read-tool blew the 25K ceiling.
const charsPerTokenEstimate = 2

// ReadToolTokenCeiling is the conservative upper bound we hold composed
// briefs under so a sub-agent can `Read` the disk-stored brief in one
// shot. Read-tool's hard cap is 25K tokens (the value reported in
// run-30 errors).
//
// Run-30 follow-up Fix 2 changed the estimator from len/3 to len/2
// after empirical evidence (88 KB briefs → 35K real tokens, i.e. ~2.5
// cpt — far below the 3.0 cpt that len/3 assumes). Under len/2 the
// estimator is intentionally pessimistic (~25% overshoot vs 2.5 cpt
// reality), so the ceiling we cap on is the ESTIMATED value, not the
// real value. Translation: we want REAL tokens ≤ 25000 (Read-tool
// hard cap); at 2.5 cpt that's bytes ≤ 62500; under len/2 estimator
// those bytes encode to ≤ 31250 estimated tokens. We round up to
// 33000 to leave a small ~1.4 KB byte-headroom for inputs that
// compress slightly differently. Pinned by
// `TestBrief_StaysUnderReadToolTokenCeiling`.
const ReadToolTokenCeiling = 33000

// EstimateTokens returns a conservative token count for s using
// charsPerTokenEstimate. The estimate is intentionally pessimistic
// (overstates) so brief-cap checks fail loud rather than trip the real
// Read-tool ceiling silently.
func EstimateTokens(s string) int {
	return len(s) / charsPerTokenEstimate
}

// BriefDiskFallbackThreshold is the body-size cutoff above which
// build-subagent-prompt persists the composed brief to
// `<sess.OutputRoot>/.briefs/<kind>-<codebase>-<unixnano>.md` and returns
// a pointer (`briefPath` + `briefSize`) instead of an inline `prompt`
// payload. Run-29 Fix #1 — closes the cap-treadmill (44→48→52→56→60→64
// KB across runs 22-28) by making disk-write the designed primary path
// for large briefs rather than the error-recovery fallback.
//
// 40 KB chosen empirically: even with ~5 % JSON-escape inflation +
// envelope overhead, a 40 KB body encodes to ~43 KB tool-result JSON,
// comfortably under the MCP ~80 KB cap. Below the threshold the inline
// path stays; above, the engine writes and returns a pointer. The
// either-or semantics is load-bearing — `Prompt` and `BriefPath` are
// never both populated; callers branch on `BriefPath != ""`.
const BriefDiskFallbackThreshold = 40 * 1024

// RefinementBriefCap caps the refinement-pass brief. Run-26 + run-27
// hit the MCP 25K-token stdin cap (97 KB observed in run-27) when the
// refinement composer streamed every fact + the embedded rubric +
// engine-flagged suspects unbounded; the sub-agent recovered via
// file-fallback at 30-60 s overhead per OOM. Cap is 80 KB — ~40 %
// margin under the 25K-token cap at the typical 3.5 chars/token
// average. Composer keeps rubric blocks (phase entry, synthesis
// workflow, embedded rubric, reference catalog, stitched-output
// pointer, engine-flagged suspects) intact and elides only the
// variable-size fact stream, most-recent-first; the elision marker
// stays in the brief body so the agent reading the brief sees that
// older facts are on disk.
const RefinementBriefCap = 80 * 1024

// FinalizeBriefCap caps the finalize brief size. Sized for a typical
// 3-codebase recipe with ~67 fragments (run 10 had 67 actual; the
// hand-typed wrapper claimed 89 wrongly). Below the scaffold cap
// because finalize doesn't include the principle-level atoms.
//
// Run-13 §T raised this 12 → 14 KB to fit the tier-fact table the
// brief composer adds before the audience-paths section.
const FinalizeBriefCap = 14 * 1024

// Brief is a composed sub-agent brief. Body is the final text handed to
// the Agent tool when the brief composes as a single file; Parts lists
// the section identifiers in order so the harness can trace what came
// from where.
//
// Run-31 Fix #1 closure — multi-file briefs. For codebase-content,
// env-content, and refinement (the kinds that historically blew the
// Read-tool token cap), the composer persists an index.md plus N part
// files to disk during composition. IndexPath holds the absolute path
// to the index; PartPaths holds the absolute paths to part files in
// read order. When IndexPath is set, Body is empty and the dispatch
// envelope returns IndexPath as briefPath. The agent reads the index,
// then reads each part in the order the index lists. Each part is
// bounded by PerPartTokenCeiling (22000 estimated tokens, ~44 KB);
// the composed brief as a whole is unbounded.
type Brief struct {
	Kind  BriefKind `json:"kind"`
	Body  string    `json:"body"`
	Bytes int       `json:"bytes"`
	Parts []string  `json:"parts,omitempty"`
	// IndexPath — absolute path to the index.md file when the brief
	// composes multi-file (codebase-content, env-content, refinement).
	// Empty for single-file kinds (scaffold, feature, finalize,
	// claudemd-author).
	IndexPath string `json:"indexPath,omitempty"`
	// PartPaths — absolute paths to the part files in read order.
	// Populated alongside IndexPath; empty for single-file kinds.
	PartPaths []string `json:"partPaths,omitempty"`
	// Metrics describes the multi-file emission shape (parts, total
	// bytes, max-part bytes, max-part estimated tokens). Populated by
	// the multi-file Persist path; zero for single-file kinds. Surface
	// on the dispatch envelope so future drift between composer-
	// estimated tokens and Read-tool real tokens stays visible.
	// Run-31 multi-file review Fix #4.
	Metrics BriefMetrics `json:"metrics,omitzero"`
}

// Validate reports whether the Brief is in a coherent transport shape.
// Single-file briefs carry Body and an empty IndexPath; multi-file
// briefs carry IndexPath + at least one PartPath and an empty Body.
// A Brief with both Body and IndexPath populated is ambiguous (caller
// can't pick a transport channel) and refused; an empty Brief (no Body
// and no IndexPath) is also refused. Run-31 multi-file review Fix #9 —
// without this guard a future composer / test could produce a Brief
// the dispatch handler silently routes wrong.
func (b Brief) Validate() error {
	hasBody := b.Body != ""
	hasIndex := b.IndexPath != ""
	switch {
	case hasBody && hasIndex:
		return errors.New("brief: ambiguous shape — both Body and IndexPath populated; pick one transport")
	case !hasBody && !hasIndex:
		return errors.New("brief: empty — neither Body nor IndexPath populated")
	case hasIndex && len(b.PartPaths) == 0:
		return errors.New("brief: multi-file shape requires at least one PartPath alongside IndexPath")
	}
	return nil
}

//go:embed all:content
var recipeV3Content embed.FS

// readAtom reads an atom at a path relative to internal/recipe/content/.
// Read-only access to the embedded atom tree.
func readAtom(rel string) (string, error) {
	b, err := fs.ReadFile(recipeV3Content, "content/"+rel)
	if err != nil {
		return "", fmt.Errorf("read atom %q: %w", rel, err)
	}
	return string(b), nil
}

// BuildScaffoldBrief composes a scaffold sub-agent brief for one codebase
// without enumerating reachable recipe slugs. Use
// BuildScaffoldBriefWithResolver in production paths so the brief carries
// the canonical recipe-knowledge slug list (Cluster B.3, R-13-21); the
// shimless variant exists for unit tests that don't need the slug
// section and don't want to construct a recipes mount.
func BuildScaffoldBrief(plan *Plan, cb Codebase, parent *ParentRecipe) (Brief, error) {
	return BuildScaffoldBriefWithResolver(plan, cb, parent, nil)
}

// BuildScaffoldBriefWithResolver is BuildScaffoldBrief plus a Resolver
// whose MountRoot enumerates the recipe slugs the agent may consult via
// `zerops_knowledge recipe=<slug>`. When the resolver is nil or its
// mount root is empty, the slug section is omitted and the brief shape
// matches the legacy BuildScaffoldBrief output.
func BuildScaffoldBriefWithResolver(plan *Plan, cb Codebase, parent *ParentRecipe, resolver *Resolver) (Brief, error) {
	if plan == nil {
		return Brief{}, errors.New("nil plan")
	}
	contract, ok := cb.Role.Contract()
	if !ok {
		return Brief{}, fmt.Errorf("unknown role %q", cb.Role)
	}

	var b strings.Builder
	var parts []string

	fmt.Fprintf(&b, "# Scaffold brief — %s (%s)\n\n", cb.Hostname, cb.Role)
	fmt.Fprintf(&b, "Recipe: %s · Framework: %s · Tier: %s\n\n",
		plan.Slug, plan.Framework, plan.Tier)
	parts = append(parts, "header")

	b.WriteString("## Role contract\n")
	fmt.Fprintf(&b, "- ServesHTTP: %t\n", contract.ServesHTTP)
	fmt.Fprintf(&b, "- RequiresSubdomain: %t\n", contract.RequiresSubdomain)
	fmt.Fprintf(&b, "- ProcessModel: %s\n", contract.ProcessModel)
	fmt.Fprintf(&b, "- zeropsSetup dev: %s, prod: %s\n\n",
		contract.ZeropsSetupDev, contract.ZeropsSetupProd)
	parts = append(parts, "role_contract")

	// Run-21 R2-1 — scaffold brief slim. The full `decision_recording.md`
	// (~14 KB of backend-flavored worked examples) drops in favor of the
	// schema-only `decision_recording_slim.md` (~1.5 KB); the full schema
	// description in handlers.go (run-21 R1-B) is the authoritative
	// reference. `yaml-comment-style.md` drops universally (it taught
	// what good causal comments look like, which contradicts the
	// `bare-yaml-prohibition.md` scaffold rule that yaml is bare and
	// causal comments land via codebase-content fragments). The full
	// `decision_recording.md` stays on disk (worked-example test still
	// pins it); the scaffold composer just stops loading it.
	atoms := []string{
		"briefs/scaffold/platform_principles.md",
		"briefs/scaffold/preship_contract.md",
		// run-22 followup F-2 — `briefs/scaffold/fact_recording.md` was
		// the legacy platform-trap-shape teaching (topic + symptom +
		// mechanism + surfaceHint + citation) and competed with
		// `decision_recording_slim.md` (the canonical kind-keyed schema).
		// Two parallel schemas in two atoms produced 2/53 record-fact
		// failures using a topic name as a kind value. Atom deleted; the
		// runtime back-compat path in `facts.go::Validate` still accepts
		// empty-Kind records, but the teaching channel collapses to one.
		"briefs/scaffold/decision_recording_slim.md",
		"principles/dev-loop.md",
		"principles/mount-vs-container.md",
		// Run-20 C2 layer 1 — workspace dual-runtime URL pattern via
		// project envs. The orphaned-from-zcprecipator3 teaching at
		// internal/content/workflows/recipe.md:534-561 lifted into a
		// principle the scaffold brief actually loads. Closes the
		// run-19 SPA build-time-bake trap that bit appdev/appstage.
		"principles/cross-service-urls.md",
		// Run-20 C3 — bare-yaml prohibition. Re-added the bare-yaml
		// clause after run-19 scaffold sub-agents wrote `# causal
		// comment` lines into zerops.yaml; codebase-content phase
		// authors causal comments later as the whole-yaml fragment.
		"principles/bare-yaml-prohibition.md",
		// Run-32 F-49 — codebase-name-vs-slot-hostname. Pre-empts
		// `complete-phase codebase='<host>dev'` and `fragmentId=
		// codebase/<host>dev/...` traps where agents reach for the
		// slot hostname they've been working with all phase. The
		// teaching previously lived only in the refinement brief.
		"principles/codebase-name-vs-slot-hostname.md",
	}
	// Run-21 R2-1 (#5) — predicate scoped to THIS codebase. Pre-fix the
	// init-commands atom leaked into every codebase's brief whenever any
	// peer authored initCommands.
	if cb.HasInitCommands {
		atoms = append(atoms, "principles/init-commands-model.md")
	}
	// Run-14 §D.3 (R-13-15) — frontend codebases on a Node base ship a
	// bundler dev server (Vite, Webpack, Rollup) whose default
	// host-allowlist rejects Zerops dev/stage subdomains. The atom
	// teaches the positive shape (set the bundler's allowlist knob);
	// other roles don't author bundler config.
	if cb.Role == RoleFrontend && strings.HasPrefix(cb.BaseRuntime, "nodejs") {
		atoms = append(atoms, "briefs/scaffold/build_tool_host_allowlist.md")
		// Run-20 C2 layer 3 — frontend codebases that compile to a
		// static dist (the dominant SPA shape) ship on `base: static`,
		// not nodejs@22 + npx serve. Conditional on frontend role +
		// nodejs base for the build container; the runtime flips to
		// static. Closes the run-19 SPA wrong-base shape.
		atoms = append(atoms, "briefs/scaffold/spa_static_runtime.md")
	}
	for _, atom := range atoms {
		body, err := readAtom(atom)
		if err != nil {
			return Brief{}, err
		}
		if atom == "briefs/scaffold/platform_principles.md" && !contract.ServesHTTP {
			body = stripHTTPSection(body)
		}
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
		parts = append(parts, atom)
	}

	b.WriteString("## Citation map\n\nTopics covered by a `zerops_knowledge` guide: ")
	b.WriteString(strings.Join(citationGuides(), ", "))
	b.WriteString(". When your KB fragment touches any of them, call `zerops_knowledge` on the matching guide id first and cite it by name.\n\n")
	parts = append(parts, "citation_map")

	// Run-14 §B.3 (R-13-21) — emit the canonical reachable recipe slugs
	// the resolver can find on the recipes mount. Sub-agents call
	// `zerops_knowledge recipe=<slug>` against this list verbatim instead
	// of guessing at slugs that never existed (run-13 burned ~10s on
	// `svelte-ssr-hello-world` before recovering). Section is omitted
	// when the resolver is nil/empty so the legacy unit-test shape stays
	// stable.
	if resolver != nil {
		if slugs, err := resolver.ReachableSlugs(); err == nil && len(slugs) > 0 {
			b.WriteString("## Recipe-knowledge slugs you may consult\n\n")
			b.WriteString("`zerops_knowledge recipe=<slug>` accepts only slugs whose recipe-mount entry has an `import.yaml`. The current mount holds:\n\n")
			for _, slug := range slugs {
				b.WriteString("- ")
				b.WriteString(slug)
				b.WriteByte('\n')
			}
			b.WriteByte('\n')
			parts = append(parts, "recipe_slug_list")
		}
	}

	// Run-13 §T — frontend codebases ship tier-aware prose (the SPA
	// IG #1 cross-tier scaling note). Inject the resolved tier
	// capability matrix so the agent authors against engine truth, not
	// extrapolation from the fuzzy `tierAudienceLine()` per-tier
	// summary. API/worker scaffolds rarely author tier-aware prose;
	// brief stays slimmer for them.
	if cb.Role == RoleFrontend {
		b.WriteString(BuildTierFactTable(plan))
		b.WriteByte('\n')
		parts = append(parts, "tier_fact_table")
	}

	if parent != nil {
		if pc, ok := parent.Codebases[cb.Hostname]; ok {
			b.WriteString("## Parent recipe excerpt\n\n")
			b.WriteString("Parent: " + parent.Slug + " (" + parent.Tier + ")\n\n")
			excerpt := excerptREADME(pc.README, 1500)
			if excerpt != "" {
				b.WriteString("```\n")
				b.WriteString(excerpt)
				if !strings.HasSuffix(excerpt, "\n") {
					b.WriteByte('\n')
				}
				b.WriteString("```\n\n")
			}
			parts = append(parts, "parent_excerpt")
		}
	} else if body, parentSlug, ok := embeddedParentBody(plan.Slug, parent); ok {
		// Run-22 R3-RC-0 — embedded-parent baseline section. The
		// scaffold sub-agent inherits setup naming, project-secret
		// posture, and codebase yaml shape from a deployment-verified
		// predecessor. embeddedParentBody prefers the cached
		// parent.EmbeddedBody (loaded once via sess.LoadParent) and
		// falls back to a direct embed.FS read for callers that bypass
		// the session resolver (tests, ad-hoc composer calls).
		b.WriteString("## Parent recipe baseline (embedded)\n\n")
		fmt.Fprintf(&b, "Parent slug: %s — read this for convention inheritance "+
			"(setup names, project-secret posture, comment style, codebase yaml shape). "+
			"Don't re-invent; your showcase should be a superset of these conventions.\n\n",
			parentSlug)
		b.WriteString("```md\n")
		excerpt := excerptREADME(body, 4000)
		b.WriteString(excerpt)
		if !strings.HasSuffix(excerpt, "\n") {
			b.WriteByte('\n')
		}
		b.WriteString("```\n\n")
		parts = append(parts, "embedded_parent_baseline")
	}

	body := b.String()
	return Brief{Kind: BriefScaffold, Body: body, Bytes: len(body), Parts: parts}, nil
}

// FeaturePass discriminates the feature-phase brief composer between
// the backend pass (api + worker scope; routes/queue/contract authoring
// + curl smoke-tests; no design-system / Tailwind atoms) and the
// frontend pass (SPA / monolith UI scope; design-system + Tailwind +
// browser-walk + bounded cross-codebase edit authority). Run-23 F-21
// split the prior single-pass cross-codebase agent into sequential
// backend → frontend passes so each brief loads only what its scope
// needs.
type FeaturePass string

const (
	FeaturePassBackend  FeaturePass = "backend"
	FeaturePassFrontend FeaturePass = "frontend"
)

// BuildFeatureBrief composes the feature sub-agent brief for the named
// pass. Feature kind catalog + scaffold symbol table from the plan's
// codebases + services. Symbol table is flat data — no framework
// instructions.
//
// Run-23 F-21 — pass discriminator. The backend pass loads contract
// authoring + worker subscription shape (when applicable); the frontend
// pass loads design-system tokens + Tailwind componentry +
// integration-validator atoms. Showcase scenario + sshfs warning + the
// universal sub-agent bookkeeping atoms load on both passes.
func BuildFeatureBrief(plan *Plan, pass FeaturePass) (Brief, error) {
	if plan == nil {
		return Brief{}, errors.New("nil plan")
	}
	switch pass {
	case FeaturePassBackend, FeaturePassFrontend:
		// ok
	default:
		return Brief{}, fmt.Errorf("BuildFeatureBrief: unknown pass %q (want %q or %q)",
			pass, FeaturePassBackend, FeaturePassFrontend)
	}
	var b strings.Builder
	var parts []string

	fmt.Fprintf(&b, "# Feature brief — %s\n\n", plan.Slug)
	parts = append(parts, "header")

	// Run-16 §6.2 — feature brief swaps `content_extension.md` (7.4 KB;
	// taught extending IG/KB) for `decision_recording.md` (~2 KB; same
	// porter_change/field_rationale recording shape as scaffold but
	// scoped to feature-added fields + scenario-discovered traps).
	// Net: -5 KB. Legacy atom retired in tranche 6.
	//
	// Run-21 R3-2 — drop `principles/yaml-comment-style.md`. Same
	// reasoning as scaffold-brief R2-1 drop: feature briefs operate
	// under the bare-yaml authoring contract (the comment-style
	// teaching contradicts it). The teaching belongs in the codebase-
	// content brief (which authors the published zerops.yaml comments)
	// where R2-2 already carries it.
	atoms := []string{
		// Run-21 R2-7 — SSHFS warning loaded first so the agent
		// encounters it before any feature-content guidance. Pre-fix
		// the warning sat mid-document at content_extension.md (which
		// is no longer composed into the feature brief — retired in
		// run-16 §6.2), making it effectively invisible. Run-21
		// features-2nd burned 8 min in a Vite-on-SSHFS rabbit hole.
		"briefs/feature/sshfs_warning.md",
		"briefs/feature/feature_kinds.md",
		"briefs/feature/decision_recording.md",
		"principles/mount-vs-container.md",
		// Run-32 F-49 — codebase-name-vs-slot-hostname. Pre-empts
		// `complete-phase codebase='apidev'` and similar slot-hostname
		// reaches when the feature agent has been working against
		// `apidev` mounts all phase.
		"principles/codebase-name-vs-slot-hostname.md",
	}
	if planDeclaresSeed(plan) {
		atoms = append(atoms, "principles/init-commands-model.md")
	}
	// Run-13 §F — showcase-tier recipes demonstrate every managed-
	// service category in the SPA. Hardcoded scenario spec lays out
	// the panel mandate (one panel per category) + design priorities
	// + per-panel browser-verification facts. Hello-world / minimal
	// recipes don't get this — they have fewer managed services.
	if plan.Tier == tierShowcase {
		atoms = append(atoms, "briefs/feature/showcase_scenario.md")
	}
	// Run-22 followup F-5 — worker-subscription code shape (queue group +
	// drain MANDATORY) was previously taught at codebase-content via the
	// combined `showcase_tier_supplements.md` atom. Worker source is
	// authored at the feature phase (per `feature_kinds.md`); the old
	// placement meant the agent first authored worker code with the
	// wrong mental model and was then forced into edit-in-place by
	// `gateWorkerSubscription`. The split atom carries the code shape
	// here at feature; KB-content shape stays at codebase-content
	// (`worker_kb_supplements.md`). Same gate as the codebase-content
	// peer: showcase tier + plan has at least one worker codebase.
	//
	// Run-23 F-21 — restricted to backend pass. Frontend pass consumes
	// the worker contract via curl/browser-walk; it doesn't author
	// worker source. Loading the code-shape atom into the frontend pass
	// is dead weight that bloats the brief.
	if pass == FeaturePassBackend && plan.Tier == tierShowcase && plan.HasWorkerCodebase() {
		atoms = append(atoms, "briefs/feature/worker_subscription_shape.md")
	}
	// Run-23 F-22 (backend pass) — contract authoring atom. Teaches
	// recording `contract`-kind facts + curl smoke-tests at backend
	// close. Frontend pass consumes the contract; backend pass
	// authors it.
	if pass == FeaturePassBackend {
		atoms = append(atoms, "briefs/feature/contract_authoring.md")
	}
	// Run-23 F-22 (frontend pass) — Tailwind / componentry atom +
	// integration-validator atom (curl-before-UI + bounded cross-
	// codebase edit authority). Loaded only on the frontend pass + only
	// when the plan ships a UI codebase (api-only / worker-only recipes
	// don't render anything visible).
	if pass == FeaturePassFrontend && plan.HasUICodebase() {
		atoms = append(atoms,
			"briefs/feature/tailwind_componentry.md",
			"briefs/feature/integration_validator.md",
		)
	}
	for _, atom := range atoms {
		body, err := readAtom(atom)
		if err != nil {
			return Brief{}, err
		}
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
		parts = append(parts, atom)
	}

	// design-system landing — showcase recipes that ship a UI codebase
	// (frontend SPA OR view-rendering monolith) get a compact inline
	// Material 3 design-token table so feature implementations across
	// frameworks (laravel-showcase Blade views, nestjs-showcase SPA
	// panels) land with the same color/type/radius vocabulary. Full
	// spec (per-framework component lineages, do/don't details) lives
	// at `zerops://themes/design-system` and is fetched on demand via
	// `zerops_knowledge`. Token corpus is identical across recipes by
	// construction; the per-component class/element name is whatever
	// is idiomatic for the framework.
	//
	// Run-23 F-21 — frontend-pass-only. The backend pass authors
	// routes/queue/contract; design tokens never reach api+worker
	// briefs that don't render anything visible.
	if pass == FeaturePassFrontend && plan.Tier == tierShowcase && plan.HasUICodebase() {
		b.WriteString(BuildDesignTokenTable())
		parts = append(parts, "design-tokens")
	}

	b.WriteString("## Symbol table (from scaffold phase)\n\n")
	b.WriteString("### Codebases\n\n")
	for _, cb := range plan.Codebases {
		fmt.Fprintf(&b, "- %s · role=%s · runtime=%s · worker=%t",
			cb.Hostname, cb.Role, cb.BaseRuntime, cb.IsWorker)
		if cb.SharesCodebaseWith != "" {
			fmt.Fprintf(&b, " · shares=%s", cb.SharesCodebaseWith)
		}
		b.WriteByte('\n')
	}
	b.WriteString("\n### Services\n\n")
	for _, svc := range plan.Services {
		fmt.Fprintf(&b, "- %s · type=%s · kind=%s\n",
			svc.Hostname, svc.Type, svc.Kind)
	}
	parts = append(parts, "symbol_table")

	out := b.String()
	return Brief{Kind: BriefFeature, Body: out, Bytes: len(out), Parts: parts}, nil
}

// BuildFinalizeBrief composes the finalize sub-agent brief. The brief
// derives every load-bearing fact from Plan: the codebase list with
// SourceRoot paths, the managed-service list, the fragment-shape
// catalog (root/env/* fragments only — no codebase/* in finalize),
// the precomputed fragment-count math. Replaces the hand-typed
// wrapper main agent used in run 10 (gap S) — math errors and
// obsolete paths compounded across runs.
//
// Run-11 gap S-1; Run-12 §B extended with tier map, hostname sets,
// fragment list, and anti-patterns so dispatch is engine brief
// byte-identical (no wrapper padding).
func BuildFinalizeBrief(plan *Plan) (Brief, error) {
	if plan == nil {
		return Brief{}, errors.New("nil plan")
	}
	var b strings.Builder
	var parts []string

	fmt.Fprintf(&b, "# Finalize brief — %s\n\n", plan.Slug)
	fmt.Fprintf(&b, "Recipe: %s · Framework: %s · Tier: %s\n\n",
		plan.Slug, plan.Framework, plan.Tier)
	parts = append(parts, "header")

	atoms := []string{
		"briefs/finalize/intro.md",
		"briefs/finalize/validator_tripwires.md",
		// Run-33 architectural fix #1 — golden-grounded rule substrate
		// (finalize-scoped subset). V1-V6 voice + R1-R6 root + T1-T4
		// tier README + TY1-TY5 tier import.yaml + Y8 only. IG / KB /
		// per-codebase yaml-comment rules excluded (those
		// surfaces aren't authored at finalize). Trimmed to fit the
		// 14 KB FinalizeBriefCap; full rule set stays in the
		// codebase-content / env-content / refinement composers.
		"briefs/finalize/derived_rules_finalize.md",
	}
	for _, atom := range atoms {
		body, err := readAtom(atom)
		if err != nil {
			return Brief{}, err
		}
		b.WriteString(body)
		if !strings.HasSuffix(body, "\n") {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
		parts = append(parts, atom)
	}

	b.WriteString("## Tier map\n\n")
	for _, t := range Tiers() {
		fmt.Fprintf(&b, "- **%d — %s** (`%s`): %s\n",
			t.Index, t.Label, t.Folder, tierAudienceLine(t))
	}
	b.WriteByte('\n')
	parts = append(parts, "tier_map")

	// Run-13 §T — engine pushes resolved per-tier capability matrix
	// (RuntimeMinContainers / ServiceMode / CPUMode / CorePackage /
	// RunsDevContainer / MinFreeRAMGB) plus the per-managed-service HA
	// downgrade table into the finalize brief. Replaces the agent's
	// extrapolation from `tierAudienceLine()`'s fuzzy phrasing
	// ("production replicas" → agent assumed 3; emit is 2). Source
	// of truth for tier README + import-comment prose.
	b.WriteString(BuildTierFactTable(plan))
	b.WriteByte('\n')
	parts = append(parts, "tier_fact_table")

	b.WriteString("## Audience paths\n\n")
	b.WriteString("- Stitched output: `<outputRoot>/`\n")
	b.WriteString("- Per-codebase published content (already stitched at finalize):\n")
	for _, cb := range plan.Codebases {
		fmt.Fprintf(&b, "  - %s → `%s/{README.md, CLAUDE.md, zerops.yaml}`\n",
			cb.Hostname, cb.SourceRoot)
	}
	b.WriteByte('\n')
	parts = append(parts, "audience_paths")

	b.WriteString("## Fragments to author\n\n")
	b.WriteString(formatFinalizeFragmentList(plan))
	b.WriteByte('\n')
	parts = append(parts, "fragment_list")

	b.WriteString("## Fragment count\n\n")
	b.WriteString(finalizeFragmentMath(plan))
	b.WriteByte('\n')
	parts = append(parts, "fragment_math")

	b.WriteString("## Codebases (read-only context)\n\n")
	for _, cb := range plan.Codebases {
		fmt.Fprintf(&b, "- %s · role=%s · runtime=%s · worker=%t\n",
			cb.Hostname, cb.Role, cb.BaseRuntime, cb.IsWorker)
	}
	b.WriteString("\n## Managed services\n\n")
	if len(plan.Services) == 0 {
		b.WriteString("(none)\n")
	} else {
		for _, svc := range plan.Services {
			fmt.Fprintf(&b, "- %s · type=%s · kind=%s\n",
				svc.Hostname, svc.Type, svc.Kind)
		}
	}
	b.WriteByte('\n')
	parts = append(parts, "symbol_table")

	antiBody, err := readAtom("briefs/finalize/anti_patterns.md")
	if err != nil {
		return Brief{}, err
	}
	b.WriteString(antiBody)
	if !strings.HasSuffix(antiBody, "\n") {
		b.WriteByte('\n')
	}
	parts = append(parts, "anti_patterns")

	out := b.String()
	return Brief{Kind: BriefFinalize, Body: out, Bytes: len(out), Parts: parts}, nil
}

// tierAudienceLine returns a one-sentence description of the tier's
// reader, derived from typed tier metadata.
func tierAudienceLine(t Tier) string {
	switch {
	case t.RunsDevContainer:
		return "dev-pair (apidev/apistage slots) — agent and remote-CDE iteration"
	case t.ServiceMode == "HA":
		return "single-slot, managed services in HA mode, dedicated CPU — production replicas"
	case t.RuntimeMinContainers >= 2:
		return "single-slot, runtime min 2 containers — small-production rolling deploys"
	case t.MinFreeRAMGB > 0:
		return "single-slot, free-RAM headroom — stage validation"
	default:
		return "single-slot — local self-host"
	}
}

// formatFinalizeFragmentList returns a deterministic enumeration of
// every fragment id finalize must author, derived from Plan structure.
// Replaces the hand-typed wrapper list that drifted across runs.
func formatFinalizeFragmentList(plan *Plan) string {
	tiers := len(Tiers())
	hosts := len(plan.Codebases) + len(plan.Services)
	// 1 root intro + tiers × (env intro + project comment + per-host comments)
	lines := make([]string, 0, 1+tiers*(2+hosts))
	lines = append(lines, "- `root/intro`")
	for _, t := range Tiers() {
		lines = append(lines, fmt.Sprintf("- `env/%d/intro` (tier %d — %s)",
			t.Index, t.Index, t.Label))
		lines = append(lines, fmt.Sprintf("- `env/%d/import-comments/project`", t.Index))
		for _, cb := range plan.Codebases {
			lines = append(lines, fmt.Sprintf("- `env/%d/import-comments/%s`",
				t.Index, cb.Hostname))
		}
		for _, svc := range plan.Services {
			lines = append(lines, fmt.Sprintf("- `env/%d/import-comments/%s`",
				t.Index, svc.Hostname))
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

// finalizeFragmentMath returns a precomputed sentence describing the
// fragment count Plan implies. Replaces hand-typed wrapper math
// (run-10 wrapper said 89 actual was 67; said 22-each actual was
// 11-each).
func finalizeFragmentMath(plan *Plan) string {
	tiers := len(Tiers())
	cbs := len(plan.Codebases)
	svcs := len(plan.Services)
	importHosts := cbs + svcs
	envIntros := tiers
	envProjectComments := tiers
	envServiceComments := tiers * importHosts
	rootIntro := 1
	total := rootIntro + envIntros + envProjectComments + envServiceComments
	return fmt.Sprintf(
		"Tiers: %d · codebases: %d · managed services: %d (import-comment hosts: %d).\n"+
			"Per tier: 1 env intro + 1 project comment + %d service-block comments = %d fragments.\n"+
			"Total: 1 root intro + %d × (1 env intro + 1 project comment + %d service comments) = %d fragments.\n",
		tiers, cbs, svcs, importHosts,
		importHosts, 2+importHosts,
		tiers, importHosts, total,
	)
}

// stripHTTPSection removes the `## HTTP` section from the platform-
// principles atom when the codebase's role has ServesHTTP=false. The
// section lives between `<!-- HTTP_SECTION_START -->` and
// `<!-- HTTP_SECTION_END -->` markers in the atom. Run-10-readiness §Q1.
func stripHTTPSection(body string) string {
	const (
		start = "<!-- HTTP_SECTION_START -->"
		end   = "<!-- HTTP_SECTION_END -->"
	)
	before, rest, ok := strings.Cut(body, start)
	if !ok {
		return body
	}
	_, after, ok := strings.Cut(rest, end)
	if !ok {
		return body
	}
	out := strings.TrimRight(before, "\n") + "\n" + strings.TrimLeft(after, "\n")
	return out
}

// excerptREADME trims a parent README to at most n bytes, cutting at
// line boundaries so no partial sentence lands in the excerpt.
func excerptREADME(body string, n int) string {
	if len(body) <= n {
		return body
	}
	cut := body[:n]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	return cut
}

// citationGuides returns the unique guide ids from CitationMap in
// deterministic order so brief composition is reproducible.
func citationGuides() []string {
	seen := map[string]bool{}
	var out []string
	for _, g := range CitationMap {
		if seen[g] {
			continue
		}
		seen[g] = true
		out = append(out, g)
	}
	// Sort for deterministic output — map iteration is non-deterministic.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// planDeclaresSeed reports whether the plan's feature kinds include
// anything that authors new initCommands (seed, scout-import). Drives
// the execOnce concept-atom injection into the feature brief.
func planDeclaresSeed(plan *Plan) bool {
	if plan == nil {
		return false
	}
	for _, k := range plan.FeatureKinds {
		switch k {
		case "seed", "scout-import", "bootstrap":
			return true
		}
	}
	return false
}
