package recipe

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Run-8-readiness Workstream D — codebase-scoped validators.
// validators.go covers the root+env surfaces; this file covers the
// per-codebase README fragments (IG, KB), CLAUDE.md, and zerops.yaml.
//
// See docs/spec-content-surfaces.md §"Surface 4-7" for the contracts
// each of these validators enforces.

// igHeadingItemRE matches the canonical `### N. <title>` IG item
// shape. Matches the heading on its own line — the engine-generated
// item #1 ("### 1. Adding zerops.yaml") and any porter-authored
// "### 2. <title>", "### 3. <title>" items.
var igHeadingItemRE = regexp.MustCompile(`(?m)^### \d+\.\s+\S`)

// kbHeadingRE matches the un-numbered `### <stem>` H3 shape that
// codebase KBs author. Run-48 — runs 44-47 shipped every codebase KB
// in this shape, but citationBlockSplits previously required either
// `- **stem**` bullets or `### N. <title>` numbered IG headings. The
// validator's display↔URL agreement check was silently inert against
// published KB content for four runs because the splitter returned
// nil. The regex requires a non-digit, non-space first stem char so
// numbered IG headings still split via igHeadingItemRE — kbHeadingRE
// runs last in citationBlockSplits as a fallback.
var kbHeadingRE = regexp.MustCompile(`(?m)^### [^\d\s]`)

// igPlainOrderedItemRE matches plain ordered-list items (`1. `, `2. `)
// that aren't preceded by `###`. The pre-§R shape; rejected (R-1).
var igPlainOrderedItemRE = regexp.MustCompile(`(?m)^\d+\.\s+\S`)

// validateCodebaseIG checks the integration-guide fragment: marker
// present, ≥ 2 `### N.` heading items, first item introduces
// `zerops.yaml`, item count within the surface's ItemCap, no
// authoring-tool leaks. Plain ordered-list items (without the heading
// shape) are rejected (run-11 gap R-1) — the engine generates item #1
// in heading shape; porter-authored items must match. The legacy
// scaffold-only-filename Blocking gate moved to a record-time Notice
// in handleRecordFragment (run-29 Fix #2).
//
// Run-15 F.5 — adds the `codebase-ig-too-many-items` cap (5 items
// including engine-emitted IG #1, per spec). Run-14 shipped 8-10 IG
// items per codebase; the spec settles at 4-5 across both reference
// recipes.
func validateCodebaseIG(_ context.Context, path string, body []byte, _ SurfaceInputs) ([]Violation, error) {
	s := string(body)
	var vs []Violation
	ig := extractBetweenMarkers(s, "integration-guide")
	if ig == "" {
		vs = append(vs, violation("codebase-ig-marker-missing", path,
			"integration-guide marker missing or body empty"))
		return vs, nil
	}
	for _, plain := range igPlainOrderedItemRE.FindAllString(ig, -1) {
		vs = append(vs, violation("codebase-ig-plain-ordered-list", path,
			fmt.Sprintf("IG item uses plain ordered-list shape (%q); IG items must use `### N. <title>` headings to match the engine-generated item #1 — see scaffold brief",
				trimForMessage(strings.TrimSpace(plain)))))
	}
	items := igHeadingItemRE.FindAllString(ig, -1)
	if len(items) < 2 {
		vs = append(vs, violation("codebase-ig-too-few-items", path,
			fmt.Sprintf("%d `### N.` heading items < 2 expected", len(items))))
	}
	// F.5 — item-cap from the spec (Surface 4: 4-5 items per codebase
	// including engine-emitted IG #1). Read from SurfaceContract so the
	// validator stays single-source with the spec.
	if contract, ok := ContractFor(SurfaceCodebaseIG); ok && contract.ItemCap > 0 {
		if len(items) > contract.ItemCap {
			vs = append(vs, violation("codebase-ig-too-many-items", path,
				fmt.Sprintf(
					"%d `### N.` IG items > %d cap (spec §Surface 4: 4-5 items per codebase including engine-emitted IG #1). Showcase recipes do not get a higher cap; scope adds breadth via more codebases, not more items per codebase. Consider folding adjacent items, demoting recipe-internal scaffold descriptions to code comments, or removing items that explain the recipe's own helpers (which the porter doesn't have).",
					len(items), contract.ItemCap,
				)))
		}
	}
	// First numbered item must introduce zerops.yaml — IG is a porter's
	// step-by-step and the yaml is the first platform-specific change.
	if len(items) >= 1 {
		firstBlock := ig
		if idx := igHeadingItemRE.FindAllStringIndex(ig, 2); len(idx) >= 2 {
			firstBlock = ig[idx[0][0]:idx[1][0]]
		}
		if !strings.Contains(strings.ToLower(firstBlock), "zerops.yaml") {
			vs = append(vs, violation("codebase-ig-first-item-not-zerops-yaml", path,
				"first IG item must introduce `zerops.yaml`"))
		}
	}
	// Run-29 Fix #2 — the scaffold-only filename ban (`migrate.ts` /
	// `seed.ts` / `main.ts` / `api.ts`) was removed: it triggered on
	// engine-stamped IG #1 yaml content (which legitimately names the
	// codebase's own initCommands sources), and the agent's evasion
	// (delete the engine emit) became the shape that shipped. The
	// underlying signal is now a record-time Notice in
	// handlers_fragments.go::recordFragment scoped to IG fragment text
	// OUTSIDE the engine-emitted yaml fenced block (see system.md §4
	// catalog-drift corrective example for IG #1).
	// Run-15 E.2 — authoring-tool patrol. The porter operates with
	// framework-canonical commands; tool names like `zerops_*` / `zcli`
	// signal authoring leakage.
	vs = append(vs, scanAuthoringToolLeaks(path, ig, "codebase IG")...)
	// Run-46 Item 5 G1-followup — citation display-text ↔ URL ↔ topic
	// agreement also runs on IG bodies. The motivating run-45 fixture
	// (apidev IG #4: rolling-deploys friendly text + init-commands
	// canonical URL) was an IG bullet, not a KB bullet; the validator
	// was originally wired only to KB. The check is surface-agnostic —
	// it parses markdown links + form-(a) backtick citations from any
	// bullet-shaped markdown and asserts agreement against
	// CitationGuideURL / FriendlyDisplayName.
	vs = append(vs, validateCitationDisplayAgreement(path, ig)...)
	return vs, nil
}

// kbTripleFormatRE flags KB bullets that open with the
// `**symptom**:` / `**mechanism**:` / `**fix**:` debugging-runbook
// triple. That shape belongs in CLAUDE.md/notes; KB is porter-facing
// `**Topic** — explanation`. Run-10-readiness §O.
var kbTripleFormatRE = regexp.MustCompile(`(?m)^\s*[-*]\s+\*\*(symptom|mechanism|fix)\*\*\s*:`)

// kbExtractStart / kbExtractEnd are the literal marker strings the
// engine renders around the KB fragment. Used by kbMarkersPresent to
// distinguish "marker missing" (true defect) from "body empty between
// markers" (spec-permitted per Surface 5 dual-shape recalibration).
const (
	kbExtractStart = "<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->"
	kbExtractEnd   = "<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->"
)

// kbMarkersPresent reports whether the rendered body carries both the
// KB start and end extract markers. Run-48 spec recalibration permits
// an empty body BETWEEN the markers as a legitimate authoring outcome
// (the IG / yaml comments / CLAUDE.md cover everything the porter
// needs); only true marker absence is a defect.
func kbMarkersPresent(body string) bool {
	return strings.Contains(body, kbExtractStart) && strings.Contains(body, kbExtractEnd)
}

// gotchasH3RE matches a body that opens with `### Gotchas` (the
// run-48 dual-shape opt-in marker for shape (2): symptom-first bullets
// under the `### Gotchas` wrapper).
var gotchasH3RE = regexp.MustCompile(`(?m)^\s*###\s+Gotchas\b`)

// isSymptomFirstShape returns true when the KB body opts into shape
// (2) — the symptom-first `### Gotchas` bullet list. Detection
// heuristic: the body either (a) opens with a `### Gotchas` H3 header,
// or (b) carries any bold-bullet (`- **...**`) shape. Both signals are
// run-48-explicit opt-ins per spec §Surface 5 dual-shape.
//
// The boldBulletRE branch is the "implicit opt-in" path: ANY
// `- **stem**` bullet anywhere in the body — not just under `###
// Gotchas` — flips the body to shape (2). The rationale is that a
// bold-stem bullet is itself the engine-convention shape from the
// goldens (laravel-jetstream `### Gotchas` body uses `- **Topic**`
// bullets verbatim); an author writing bold-stem bullets has chosen
// the shape regardless of whether they wrote the wrapper. Tightening
// to require the explicit `### Gotchas` header would route
// bare-bold-bullet bodies to shape (1) and silently bypass stem-shape
// enforcement on content the author clearly intended as Surface 5
// shape (2). Pinned by the bullet-only fixtures in
// TestCheckSlotShape_KB_AcceptsTopicShape and
// TestCheckSlotShape_KB_NoCapAtTwelveBullets.
//
// Used by validateCodebaseKB (bold-symptom check) and by
// slot_shape.go::checkCodebaseKBAll (symptom-first stem regex
// enforcement) so the shape gate fires from one place.
func isSymptomFirstShape(body string) bool {
	if gotchasH3RE.MatchString(body) {
		return true
	}
	if boldBulletRE.MatchString(body) {
		return true
	}
	return false
}

// validateCodebaseKB — knowledge-base fragment contract.
//
// Run-48 recalibration — Surface 5 is dual-shape per spec:
//
//	(1) Forward-looking H3 operational sections (jetstream-shape) —
//	    no symptom-first bullet requirement.
//	(2) Symptom-first `### Gotchas` bullet list (engine-convention) —
//	    each bullet starts with `**Bold Topic**` opener.
//
// The validator detects which shape the body opts into and applies
// shape-(2)-specific checks (bold-symptom opener) only when shape (2)
// is detected. The fragment may be EMPTY (markers present, body
// blank) when the IG, yaml comments, and CLAUDE.md cover everything
// the porter needs — that's a positive signal, not a defect.
//
// Marker-missing fires only when the start/end markers are absent from
// the rendered body (true "no marker" case), not when markers are
// present and the body between them is empty.
//
// Citation-required + display-text-↔-URL agreement checks apply to
// EITHER shape — citation rules are surface-level, not shape-level.
func validateCodebaseKB(_ context.Context, path string, body []byte, inputs SurfaceInputs) ([]Violation, error) {
	s := string(body)
	var vs []Violation
	// Marker-presence is distinct from empty-body. The start/end markers
	// must be present in the rendered output; the body between them may
	// legitimately be empty (spec §Surface 5 "May be empty").
	if !kbMarkersPresent(s) {
		vs = append(vs, violation("codebase-kb-marker-missing", path,
			"knowledge-base extract markers missing from rendered body"))
		return vs, nil
	}
	kb := extractBetweenMarkers(s, "knowledge-base")
	if kb == "" {
		// Markers present, body empty — spec §Surface 5 permits this.
		// No further per-bullet / citation checks are meaningful on an
		// empty body; return clean.
		return vs, nil
	}
	// Bullet count + shape-(2) opt-in detection. Shape (2) applies when
	// the body opens with `### Gotchas` OR carries any bold-bullet
	// (`- **...**`) shape. Bold-symptom enforcement is scoped to shape
	// (2); shape (1) bodies (forward-looking H3 operational sections
	// like `### Maintenance Mode`) have no bold-symptom requirement.
	bulletRE := regexp.MustCompile(`(?m)^\s*-\s+\S`)
	bullets := bulletRE.FindAllStringIndex(kb, -1)
	boldBullets := boldBulletRE.FindAllStringIndex(kb, -1)
	if isSymptomFirstShape(kb) {
		if len(bullets) > 0 && len(boldBullets) < len(bullets) {
			vs = append(vs, notice("kb-missing-bold-symptom", path,
				fmt.Sprintf("%d of %d KB bullets lack a **bold symptom** opening (shape (2) `### Gotchas` body)", len(bullets)-len(boldBullets), len(bullets))))
		}
	}
	// Run-48 — ItemCap retired. Spec §Surface 5: "no floor, no cap;
	// bar is salience". Over-collection signal moved to the
	// kb-self-inflicted-reversible gate + the brief's discriminator.
	for _, m := range kbTripleFormatRE.FindAllString(kb, -1) {
		vs = append(vs, notice("codebase-kb-triple-format-banned", path,
			fmt.Sprintf("KB entries use `**Topic** — prose` format; `**symptom**:` / `**mechanism**:` / `**fix**:` triples belong in CLAUDE.md/notes: %q",
				trimForMessage(strings.TrimSpace(m)))))
	}
	// Citation-required: for every topic in CitationMap that appears
	// anywhere in the KB body, the body must also reference the guide
	// id OR its canonical docs.zerops.io URL.
	//
	// Run-44 G1 — canonical-URL acceptance closes the substrate-internal
	// contradiction where refinement-2's suggested fix (form-(b)/form-(c)
	// citation by canonical URL) tripped this validator's slug-stem-only
	// substring check. The URL match is ANCHORED (see kbBodyCitesGuideURL)
	// so prefix collisions like `features/scaling-ha-advanced` do NOT
	// satisfy a `features/scaling-ha` citation.
	for topic, guide := range CitationMap {
		if !strings.Contains(strings.ToLower(kb), strings.ToLower(topic)) {
			continue
		}
		// Guide id reference: allow the guide id or its canonical name
		// (they're identical in CitationMap but future-proof for alias).
		if strings.Contains(kb, guide) {
			continue
		}
		// Run-44 G1 — canonical URL also satisfies the citation.
		if url, ok := CitationGuideURL[guide]; ok && kbBodyCitesGuideURL(kb, url) {
			continue
		}
		vs = append(vs, violation("kb-citation-missing", path,
			fmt.Sprintf("KB mentions %q but does not cite `zerops_knowledge` guide %q", topic, guide)))
	}
	// Run-46 Item 5 — citation display-text ↔ URL ↔ topic agreement.
	// Closes the run-45 apidev IG #4 hole where a wrong-URL form-(b)
	// citation passed kb-citation-missing because the URL canonical-
	// matched a different guide than the bullet's stated topic.
	vs = append(vs, validateCitationDisplayAgreement(path, kb)...)
	// V-2: paraphrase detection vs the cited guide's key phrases.
	vs = append(vs, validateKBParaphrase(path, kb)...)
	// V-3: each bullet must mention at least one platform-side
	// mechanism term — pure framework-quirk bullets get flagged.
	vs = append(vs, validateKBNoPlatformMention(path, kb, inputs.Plan)...)
	// V-4: regex-flag bullets in first-person/recipe-author voice.
	vs = append(vs, validateKBSelfInflictedShape(path, kb)...)
	// O-2: regex-flag "Cited guide: <name>" boilerplate tails.
	vs = append(vs, validateKBCitedGuideBoilerplate(path, kb)...)
	// Run-15 E.2 — authoring-tool patrol. Citation-by-name (e.g. "the
	// `env-var-model` guide") is fine; tool invocations like `zerops_browser
	// action=...` are not.
	vs = append(vs, scanAuthoringToolLeaks(path, kb, "codebase KB")...)
	return vs, nil
}

// claudeMDLineCap is the upper length bound for a codebase-specific
// CLAUDE.md. Run-16 §8.1 raised the cap from 60 → 80 to fit the
// `/init`-shape sub-agent output (project overview + Build & run +
// Architecture) for codebases with denser per-script labels and
// framework-canonical layouts.
const claudeMDLineCap = 80

// claudeMDForbiddenSubsections are cross-codebase operational notes
// that don't belong in a codebase-specific CLAUDE.md (identical across
// every codebase in a recipe). They inflate each codebase's length;
// they belong in the recipe-level root README or a single dev-loop
// guide. Matched case-insensitively against H2 / H3 headers.
var claudeMDForbiddenSubsections = []string{
	"Quick curls",
	"Smoke test",
	"Smoke tests",
	"Local curl",
	"In-container curls",
	"Redeploy vs edit",
	"Boot-time connectivity",
}

// validateCodebaseCLAUDE backs up the run-16 record-time slot-shape
// refusal at finalize. Primary contract is enforced at record-fragment
// time (§8.1) — the claudemd-author sub-agent's brief is strictly
// Zerops-free, slot-shape refusal blocks `## Zerops` / `zsc` /
// `zerops_*` / `zcp` / `zcli` / managed-service hostname leakage at
// the moment of recording. This validator runs at finalize stitch as
// the last-line-of-defense backstop.
//
// Run-16 §6.8 — the validator confirms the sub-agent's output shape:
//
//   - body ≤ claudeMDLineCap (80 lines per §8.1)
//   - no `## Zerops` heading variants leaked through
//   - no authoring-tool leak (zsc / zerops_* / zcp / zcli)
//   - legacy claudeMDForbiddenSubsections still flagged as Notice for
//     pre-run-16 recipes the back-compat synthesis path renders
func validateCodebaseCLAUDE(_ context.Context, path string, body []byte, _ SurfaceInputs) ([]Violation, error) {
	var vs []Violation
	if len(body) < 200 {
		// Lowered from 1200 to 200 — the /init-shape sub-agent output is
		// shorter than the legacy "Zerops service facts + Notes" shape
		// (run-15 reference apidev/CLAUDE.md was ~1500 bytes; the new
		// shape lands ~600-1000 bytes for a small codebase).
		vs = append(vs, violation("claude-md-too-short", path,
			fmt.Sprintf("%d bytes < 200 minimum (sub-agent likely failed to author)", len(body))))
	}
	lines := strings.Count(string(body), "\n")
	if !strings.HasSuffix(string(body), "\n") {
		lines++
	}
	if lines > claudeMDLineCap {
		vs = append(vs, violation("claude-md-too-long", path,
			fmt.Sprintf("%d lines > %d cap — CLAUDE.md is a /init-shape codebase guide (project overview + Build & run + Architecture). The sub-agent's output drifted; re-record with `record-fragment mode=replace`.",
				lines, claudeMDLineCap)))
	}

	// Run-21 R2-5 — Zerops-content guards on CLAUDE.md retired.
	// `## Zerops` heading + `zsc`/`zerops_*`/`zcp`/`zcli` tool-token
	// checks + per-managed-service hostname refusal are gone. The
	// brief at `briefs/claudemd-author/zerops_free_prohibition.md`
	// teaches the agent to author Zerops-free CLAUDE.md; engine-side
	// word-blacklisting added false-positive friction (4× rejection
	// cycle in run-21 around common English tokens). Cross-surface
	// duplication is covered by `gateCrossSurfaceDuplication` at
	// codebase-content phase, not by content guards here. Spec:
	// docs/spec-content-surfaces.md §Surface 6.

	// Legacy forbidden-subsection patrol stays as Notice for back-compat
	// (recipes synthesized via legacy sub-slot path may still carry
	// these headings).
	bodyStr := string(body)
	headerRE := regexp.MustCompile(`(?m)^##+\s+(.+)$`)
	for _, m := range headerRE.FindAllStringSubmatch(bodyStr, -1) {
		title := strings.TrimSpace(m[1])
		lower := strings.ToLower(title)
		for _, banned := range claudeMDForbiddenSubsections {
			if lower == strings.ToLower(banned) {
				vs = append(vs, notice("claude-md-forbidden-subsection", path,
					fmt.Sprintf("%q is a cross-codebase operational note — move to the recipe root README, not this codebase-specific CLAUDE.md", title)))
				break
			}
		}
	}
	return vs, nil
}

// authoringToolPatrolNeedles — tool names that signal authoring-time
// content leaking into porter-facing surfaces. Run-15 E.2 extends the
// existing CLAUDE.md patrol into apps-repo zerops.yaml and IG/KB body
// content. The porter operates with framework-canonical commands
// (`npm`, `composer`, `php artisan`, `ssh`, `git`); these tool names
// are how the recipe was BUILT, not how the porter USES it.
var authoringToolPatrolNeedles = []string{
	"zerops_browser",
	"zerops_subdomain",
	"zerops_knowledge",
	"zerops_recipe",
	"zerops_workflow",
	"zerops_workspace_manifest",
	"zerops_record_fact",
	"zerops_dev_server",
	"zerops_discover",
	"zerops_logs",
	"zerops_events",
	"zerops_process",
	"zerops_scale",
	"zerops_deploy",
	"zerops_import",
	"zerops_mount",
	"zerops_env",
	"zcli ",
	"zcp ",
}

// scanAuthoringToolLeaks returns one notice per authoring-tool needle
// hit in a piece of body text. Used by codebase yaml + IG/KB
// validators to enforce the porter-voice audience rule across every
// apps-repo surface, not just CLAUDE.md.
func scanAuthoringToolLeaks(path, body, surface string) []Violation {
	var vs []Violation
	lower := strings.ToLower(body)
	for _, needle := range authoringToolPatrolNeedles {
		if !strings.Contains(lower, strings.ToLower(needle)) {
			continue
		}
		vs = append(vs, notice("authoring-tool-leak", path,
			fmt.Sprintf("%s contains authoring-tool name %q — comments / IG / KB are porter-facing; the porter operates with framework-canonical commands (`npm`, `composer`, `ssh`, `git`), never %s",
				surface, needle, needle)))
	}
	return vs
}

// validateCodebaseYAML enforces the codebase yaml-comment contract.
// Comments are grouped into BLOCKS — runs of adjacent `#` lines, with
// bare `#` treated as an in-block paragraph separator per the
// reference style at /Users/fxck/www/laravel-showcase-app/zerops.yaml.
// Each block passes if ANY line in it carries a causal word /
// em-dash; blocks whose lines are all short labels (≤40 chars after
// stripping the `#`) pass unconditionally. One violation per block,
// not per line — so a multi-line prose block that forgets rationale
// emits a single report. Run-10-readiness §N.
//
// Run-15 E.2 — additionally patrols comments for authoring-tool name
// leaks (zcli / zerops_* / zcp). Tool names are how the recipe was
// BUILT, not how the porter USES it.
func validateCodebaseYAML(_ context.Context, path string, body []byte, _ SurfaceInputs) ([]Violation, error) {
	var vs []Violation
	for _, block := range parseYAMLCommentBlocks(body) {
		if !blockNeedsCausalWord(block) {
			continue
		}
		if blockHasCausalWord(block) {
			continue
		}
		first := block[0]
		vs = append(vs, notice("yaml-comment-missing-causal-word", path,
			fmt.Sprintf("comment block lacks a causal word (`because`, `so that`, `otherwise`, `trade-off`, em-dash) on any line: %q",
				trimForMessage(first))))
	}
	// E.2 — only inspect comment lines; code referencing `zerops_env` as
	// a yaml field name is fine. parseYAMLCommentBlocks already strips
	// the `#` prefix and collects comment bodies.
	var commentBody strings.Builder
	for _, block := range parseYAMLCommentBlocks(body) {
		for _, line := range block {
			commentBody.WriteString(line)
			commentBody.WriteByte('\n')
		}
	}
	vs = append(vs, scanAuthoringToolLeaks(path, commentBody.String(), "apps-repo zerops.yaml comment")...)
	return vs, nil
}

// parseYAMLCommentBlocks groups adjacent `#` comment lines into blocks.
// Bare `#` lines stay in-block (paragraph separators, reference style).
// Each returned block is a slice of comment bodies (already stripped of
// the leading `#` + whitespace). The zeropsPreprocessor directive is
// skipped — it's a directive, not a rationale comment.
func parseYAMLCommentBlocks(body []byte) [][]string {
	lines := strings.Split(string(body), "\n")
	var blocks [][]string
	var current []string
	for _, raw := range lines {
		trimmed := strings.TrimLeft(raw, " \t")
		if !strings.HasPrefix(trimmed, "#") {
			if len(current) > 0 {
				blocks = append(blocks, current)
				current = nil
			}
			continue
		}
		comment := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		if strings.HasPrefix(comment, "zeropsPreprocessor") {
			continue
		}
		current = append(current, comment)
	}
	if len(current) > 0 {
		blocks = append(blocks, current)
	}
	return blocks
}

// blockNeedsCausalWord reports whether a comment block requires at
// least one causal word. Blocks whose every non-blank line is a short
// label (≤40 chars) pass unconditionally — label blocks never need
// rationale.
func blockNeedsCausalWord(block []string) bool {
	for _, line := range block {
		if line == "" {
			continue
		}
		if len(line) > 40 {
			return true
		}
	}
	return false
}

// blockHasCausalWord reports whether the block carries a causal word /
// em-dash anywhere. Lines are joined with a single space before the
// scan so:
//
//   - causal phrases that wrap across line breaks ("…burn the migrate
//     key, so the\nnext container retry…") still match the trailing-
//     space-scoped tokens like "so the ".
//   - em-dashes at end-of-line ("…by default —\n") sit inside " — "
//     after the join, satisfying the em-dash token.
//
// Run-22 §N2-N5.
func blockHasCausalWord(block []string) bool {
	joined := " " + strings.Join(block, " ") + " "
	return containsAnyCausal(joined)
}

func trimForMessage(s string) string {
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

// markdownLinkRE matches a markdown link — `[display text](URL)`. The
// URL is the second capture group; display text is the first. Run-46
// Item 5 — the citation-display-text validator parses these to check
// display↔URL agreement.
var markdownLinkRE = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)

// friendlyDisplayToGuide is the reverse of FriendlyDisplayName — keyed
// by the lower-case friendly display name, valued by the guide id.
// Built once at package init by iterating CitationGuideURL and
// resolving each guide's friendly through FriendlyDisplayName so the
// two maps stay single-source. Run-48 — used by Check 4 in
// validateCitationDisplayAgreement: when a markdown link's display
// text IS a known friendly but its URL doesn't resolve to ANY guide
// (e.g. run-47's worker KB pointing the "zero-downtime deploys with
// multi-container setups" display at `#minrunningcontainers-`), the
// validator can flag the display↔URL drift instead of skipping on
// `matchedGuide == ""`.
var (
	friendlyDisplayToGuide     map[string]string
	friendlyDisplayToGuideOnce sync.Once
)

func ensureFriendlyDisplayToGuide() {
	friendlyDisplayToGuideOnce.Do(func() {
		friendlyDisplayToGuide = make(map[string]string, len(CitationGuideURL))
		for guide := range CitationGuideURL {
			display := FriendlyDisplayName(guide)
			if display == "" {
				continue
			}
			friendlyDisplayToGuide[strings.ToLower(display)] = guide
		}
	})
}

// lookupGuideIDFromFriendlyDisplay returns the guide id whose
// FriendlyDisplayName matches the given display text (case-insensitive),
// or "" when no friendly registration matches.
func lookupGuideIDFromFriendlyDisplay(displayText string) string {
	ensureFriendlyDisplayToGuide()
	return friendlyDisplayToGuide[strings.ToLower(strings.TrimSpace(displayText))]
}

// DetectBulletTopic returns the canonical topic id when a KB bullet's
// PRIMARY teaching is unambiguously one citation-map topic. Returns ""
// on ambiguity (multi-topic bullets are legitimate; only hard-fail on
// clearly mis-cited single-topic bullets).
//
// Heuristic: scan the body for canonical-topic keywords. A topic
// "dominates" when its distinct keyword hits ≥ 2 AND no other topic
// has any hit. Single-keyword-only bullets return "" (not enough
// signal). Multi-topic bullets return "" (ambiguity).
//
// Run-46 Item 5 — the validator caller hard-fails ONLY on
// unambiguous topic detection; ambiguous bullets pass through to the
// pre-existing kb-citation-missing check. Multi-topic bullets are
// legitimate per the writer contract.
func DetectBulletTopic(body string) string {
	// Per-topic keyword sets live in bulletTopicKeywords (package scope
	// since Run-46 Item 1 G2-followup added DetectBulletTopicCandidates).
	// Drift from CitationMap is intentional — CitationMap is the over-
	// broad "topic appears anywhere on this surface" lookup;
	// DetectBulletTopic is the narrow "this bullet is PRIMARILY about"
	// decision.
	lower := strings.ToLower(body)
	type hit struct {
		topic string
		count int
	}
	var hits []hit
	for topic, kws := range bulletTopicKeywords {
		c := 0
		for _, k := range kws {
			if strings.Contains(lower, k) {
				c++
			}
		}
		if c > 0 {
			hits = append(hits, hit{topic: topic, count: c})
		}
	}
	if len(hits) == 0 {
		return ""
	}
	if len(hits) == 1 {
		// Single topic — require ≥ 2 distinct keyword hits to call it
		// "primary." A bare keyword mention does not trigger the topic
		// requirement (matches the brief's "Keyword-over-match guard"
		// clause).
		if hits[0].count >= 2 {
			return hits[0].topic
		}
		return ""
	}
	// Multiple topics — find the dominant. A topic "dominates" when its
	// hit count is at least twice the second-best AND ≥ 2 distinct
	// keywords matched. The 2x ratio guard keeps mixed-topic bullets
	// (where multiple topics carry comparable weight) ambiguous.
	bestIdx := 0
	for i := range hits {
		if hits[i].count > hits[bestIdx].count {
			bestIdx = i
		}
	}
	best := hits[bestIdx]
	secondBest := 0
	for i := range hits {
		if i == bestIdx {
			continue
		}
		if hits[i].count > secondBest {
			secondBest = hits[i].count
		}
	}
	if best.count >= 2 && best.count >= 2*secondBest+1 {
		return best.topic
	}
	return ""
}

// DetectBulletTopicCandidates returns every citation-map topic id whose
// keyword set produces at least one hit in the body. Sort order is
// stable (alphabetical by topic id). Used by the refinement-2 manifest
// generator to surface per-entry topic candidates without forcing the
// per-bullet "dominant topic" decision DetectBulletTopic makes.
//
// Run-46 Item 1 G2-followup — manifest entries surface candidates so
// the audit can route findings to the matching citation guide(s)
// without re-detecting per dispatch.
func DetectBulletTopicCandidates(body string) []string {
	lower := strings.ToLower(body)
	type hit struct {
		topic string
		count int
	}
	var hits []hit
	for topic, kws := range bulletTopicKeywords {
		c := 0
		for _, k := range kws {
			if strings.Contains(lower, k) {
				c++
			}
		}
		if c > 0 {
			hits = append(hits, hit{topic: topic, count: c})
		}
	}
	if len(hits) == 0 {
		return nil
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].topic < hits[j].topic })
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.topic)
	}
	return out
}

// bulletTopicKeywords mirrors the topic→keyword map literal inside
// DetectBulletTopic. Lifted to package scope so DetectBulletTopicCandidates
// reuses the same source-of-truth without copy/pasting per call site.
// Drift between the literal in DetectBulletTopic and this map is
// pinned by TestBulletTopicKeywords_Symmetric.
var bulletTopicKeywords = map[string][]string{
	"rolling-deploys": {
		"rolling-deploy", "rolling deploys",
		"mincontainers", "zero-downtime",
		"sigterm",
	},
	"init-commands": {
		"init-command", "init commands", "initcommands",
		"execonce", "${appversionid}",
	},
	"object-storage": {
		"object storage", "object-storage",
		"forcepathstyle", "minio", "s3",
	},
	"env-var-model": {
		"env-var-model", "cross-service env",
		"self-shadow", "envisolation",
	},
	"http-support": {
		"http-support", "l7-balancer", "l7 balancer",
		"trust proxy", "trust-proxy",
		"subdomain access", "httpsupport",
		"bind 0.0.0.0", "bind to 0.0.0.0",
	},
	"deploy-files": {
		"deploy-files", "deployfiles",
		"tilde syntax", "tilde-suffix",
		"static runtime",
	},
	"readiness-health-checks": {
		"readiness check", "readiness-check",
		"health check", "health-check",
	},
}

// lookupGuideIDFromURL returns the guide id whose canonical URL
// host+path+fragment matches the link URL, or "" when no map URL
// matches. Mirrors the host+path-match rule defined in
// briefs_refinement2.go (scheme + host case-insensitive, path equality
// with trailing slash optional, fragment anchored to map URL's
// fragment).
//
// Run-47 Item B — fragment-distinguished entries on the same canonical
// path (#initcommands- / #deployfiles- / #envvariables- all on
// /zerops-yaml/specification) used to collapse to a single guide
// because the lookup stripped fragments before path-match. The fix
// matches on host+path+fragment so each fragment-bearing map entry
// resolves to its own guide; fragment-less map entries still match
// fragment-less link URLs (the no-fragment-map rule).
func lookupGuideIDFromURL(linkURL string) string {
	// Normalize: strip scheme, lowercase host portion.
	stripped := strings.TrimPrefix(linkURL, "https://")
	stripped = strings.TrimPrefix(stripped, "http://")
	stripped = strings.TrimPrefix(stripped, "//")
	fullWithFrag := canonicalURLPathWithFragment(stripped)
	pathOnly := canonicalURLPath(stripped)
	for guide, url := range CitationGuideURL {
		_, _, mapHasFrag := strings.Cut(url, "#")
		if mapHasFrag {
			// Fragment-distinguished map entry: require exact path+frag
			// match (the run-47 Item B fix — `#deployfiles-` no longer
			// collapses into `#initcommands-` on the same path).
			if strings.EqualFold(canonicalURLPathWithFragment(url), fullWithFrag) {
				return guide
			}
			continue
		}
		// No-fragment map entry: any in-page anchor on the same doc is
		// acceptable; match on path only (briefs_refinement2.go's
		// no-fragment-map rule).
		if strings.EqualFold(canonicalURLPath(url), pathOnly) {
			return guide
		}
	}
	return ""
}

// canonicalURLPath returns the path portion of a citation URL with the
// fragment stripped. Used by callers that want fragment-blind path-
// family matching (e.g., the no-fragment-map fall-through where any
// in-page anchor on the same doc is acceptable).
func canonicalURLPath(url string) string {
	path, _, _ := strings.Cut(url, "#")
	path = strings.TrimRight(path, "/")
	return strings.ToLower(path)
}

// canonicalURLPathWithFragment returns the path+fragment portion of a
// citation URL. Run-47 Item B — used by lookupGuideIDFromURL so
// fragment-distinguished entries on the same canonical path resolve
// to their own guide ids.
func canonicalURLPathWithFragment(url string) string {
	path, frag, hasFrag := strings.Cut(url, "#")
	path = strings.TrimRight(path, "/")
	if !hasFrag {
		return strings.ToLower(path)
	}
	return strings.ToLower(path + "#" + frag)
}

// citationFormARE matches a form-(a) citation: the backticked guide id
// used IN citation framing. Captures the guide id (group 1). Anchored
// to "the `<id>` guide" / "see the `<id>` guide" / "`<id>` guide
// covers" etc. — the same shape briefs_refinement2.go::citationMapBlock
// teaches.
var citationFormARE = regexp.MustCompile("`([a-z][a-z0-9-]+)`\\s+(?:guide|reference)")

// validateCitationDisplayAgreement — Run-46 Item 5 + Run-48 bidirectional
// check. Four checks across the per-bullet/per-IG-item citation set:
//
//  1. Display-text ↔ URL agreement: when a markdown link's URL matches a
//     known guide URL, the display text MUST be the friendly display
//     name for THAT guide. Run-45 apidev IG #4 fixture: display named
//     rolling-deploys; URL was init-commands canonical — the wrong-URL
//     form-(b) citation passed kbBodyCitesGuideURL's anchored-match
//     check because the URL was a valid init-commands citation.
//
//  2. URL ↔ topic agreement: when the block's topic is unambiguously
//     detectable (DetectBulletTopic returns a topic, not ""), the URL
//     in any form-(b) citation MUST match the canonical for that topic.
//
//  3. Form-(a) guide-id ↔ topic agreement: when the block's topic is
//     unambiguously detectable AND the block cites a guide id via the
//     "the `<id>` guide" shape, the cited guide MUST match the topic.
//
//  4. Run-48 — Display-text ↔ canonical URL agreement (the bidirectional
//     check). When the URL does NOT resolve to any known guide BUT the
//     display text IS a known friendly display name, the link is
//     citing a friendly name with a non-canonical URL — refuse with
//     kb-citation-display-name-without-canonical-url. Run-47 fixture:
//     worker KB pointed "zero-downtime deploys with multi-container
//     setups" (the canonical friendly for rolling-deploys) at
//     `zerops-yaml/specification#minrunningcontainers-`, which is not
//     in the citation map. The legacy validator skipped on
//     `matchedGuide == ""`; the new check fires instead.
//
// Blocks without any citation don't fire; the existing
// kb-citation-missing check covers the missing-citation case.
//
// G1-followup — surface-symmetric. KB bodies are split per `- **stem**`
// bullet OR un-numbered `### <stem>` H3 (the codebase-KB authoring
// shape across runs 44+); IG bodies are split per `### N. <title>`
// numbered heading. citationBlockSplits picks the right delimiter
// automatically. The motivating run-45 case was an IG bullet (apidev
// IG #4); the run-47 frontier was un-numbered H3 KBs (worker KB #1,
// #2; apidev KB #1).
func validateCitationDisplayAgreement(path, body string) []Violation {
	var vs []Violation
	blocks := citationBlockSplits(body)
	if len(blocks) == 0 {
		return nil
	}
	for _, block := range blocks {
		// Find every markdown link + form-(a) backtick citation in the
		// block.
		links := markdownLinkRE.FindAllStringSubmatch(block, -1)
		formAMatches := citationFormARE.FindAllStringSubmatch(block, -1)
		if len(links) == 0 && len(formAMatches) == 0 {
			continue
		}
		// Detect block topic ONCE per block — best-effort. Ambiguity
		// returns "" and the topic-match check is silently skipped on
		// this block.
		blockTopic := DetectBulletTopic(block)
		for _, link := range links {
			displayText := strings.TrimSpace(link[1])
			linkURL := strings.TrimSpace(link[2])
			matchedGuide := lookupGuideIDFromURL(linkURL)
			if matchedGuide == "" {
				// Run-48 Check 4 — bidirectional display→guide→URL. The
				// URL doesn't resolve to any known guide, but if the
				// display text IS a known friendly display name, the
				// author is naming a canonical guide with a non-
				// canonical URL — refuse instead of silently skipping
				// (the run-47 worker KB #1/#2 defect: known friendly
				// "zero-downtime deploys with multi-container setups"
				// pointed at `#minrunningcontainers-`, which isn't in
				// the citation map).
				if displayGuide := lookupGuideIDFromFriendlyDisplay(displayText); displayGuide != "" {
					canonicalURL := CitationGuideURL[displayGuide]
					vs = append(vs, violation("kb-citation-display-name-without-canonical-url", path,
						fmt.Sprintf("citation display text %q is the canonical friendly name for the %q guide, but the URL %s does not resolve to any known guide (expected canonical %s). Either fix the URL to the guide's canonical, or change the display text so it doesn't claim the friendly name.",
							displayText, displayGuide, linkURL, canonicalURL)))
				}
				// URL doesn't match any known guide and (if reached
				// here) display text isn't a known friendly either —
				// skip; this isn't a citation-map URL, so display-text
				// doesn't have to match anything in particular.
				continue
			}
			// Check 1 — display text MUST be the friendly name for the
			// URL's matched guide.
			expectedDisplay := FriendlyDisplayName(matchedGuide)
			if expectedDisplay != "" && !strings.EqualFold(displayText, expectedDisplay) {
				vs = append(vs, violation("kb-citation-display-mismatch", path,
					fmt.Sprintf("citation display text %q does not match the friendly name %q for the URL's matched guide %q (URL %s). Either fix the display text to match the URL's guide, or change the URL to point at the guide the display text names.",
						displayText, expectedDisplay, matchedGuide, linkURL)))
			}
			// Check 2 — if topic is unambiguous, URL MUST canonical-match.
			if blockTopic != "" && matchedGuide != blockTopic {
				canonicalURL := CitationGuideURL[blockTopic]
				vs = append(vs, violation("kb-citation-topic-mismatch", path,
					fmt.Sprintf("citation URL %s does not match canonical for the block's detected topic %q (canonical is %s). The block's primary teaching is the detected topic; the citation must point the porter at that guide.",
						linkURL, blockTopic, canonicalURL)))
			}
		}
		// Check 3 — form-(a) backtick guide-id citations. Each match
		// captures the cited guide id; if a known guide is named AND
		// the block's topic is unambiguous AND they disagree, fire.
		if blockTopic == "" {
			continue
		}
		for _, fa := range formAMatches {
			citedGuide := fa[1]
			// Only check against known guides — random backticked
			// tokens that happen to precede the word "guide" are not
			// citation-map matches.
			if _, ok := CitationGuideURL[citedGuide]; !ok {
				continue
			}
			if citedGuide == blockTopic {
				continue
			}
			canonicalURL := CitationGuideURL[blockTopic]
			vs = append(vs, violation("kb-citation-topic-mismatch", path,
				fmt.Sprintf("citation names the %q guide but the block's detected topic is %q (canonical URL %s). The block's primary teaching is the detected topic; cite the guide that matches.",
					citedGuide, blockTopic, canonicalURL)))
		}
	}
	return vs
}

// citationBlockSplits returns the per-block substrings the citation
// display-text validator walks. The function picks the right delimiter
// per surface shape, in priority order:
//
//  1. KB bodies that open each bullet with `- **stem**` — split on
//     boldBulletRE.
//  2. IG bodies that open each item with `### N. <title>` — split on
//     igHeadingItemRE (numbered heading shape).
//  3. Codebase KB bodies that open each entry with an un-numbered
//     `### <stem>` H3 — split on kbHeadingRE.
//
// Run-48 — fallback #3 closes the structural-inertness gap surfaced in
// run-47: every codebase KB across runs 44-47 used un-numbered H3, so
// the splitter previously returned nil and validateCitationDisplayAgreement
// silently skipped the body. The numbered-heading regex stays a higher-
// priority match so IG bodies that already use `### N. <title>` (incl.
// the engine-stamped IG #1 "### 1. Adding zerops.yaml") still split on
// the numbered shape; kbHeadingRE requires a non-digit first stem char
// so the two shapes are unambiguous.
//
// When multiple shapes match (mixed body) the first matching delimiter
// in the precedence order above wins — boldBulletRE > igHeadingItemRE
// > kbHeadingRE — and the lower-priority shapes are not consulted; any
// `### <stem>` lines fall inside whichever bold-bullet or numbered-
// heading block contains them. Pinned by
// TestCitationBlockSplits_UnumberedH3KB cases "mixed body — bold-bullet
// wins" and "mixed numbered + unnumbered H3 — numbered wins".
//
// When neither matches (no bullets, no headings) returns an empty slice
// so the caller treats the body as carrying no countable blocks.
func citationBlockSplits(body string) []string {
	if starts := boldBulletRE.FindAllStringIndex(body, -1); len(starts) > 0 {
		return splitAtIndexes(body, starts)
	}
	if starts := igHeadingItemRE.FindAllStringIndex(body, -1); len(starts) > 0 {
		return splitAtIndexes(body, starts)
	}
	if starts := kbHeadingRE.FindAllStringIndex(body, -1); len(starts) > 0 {
		return splitAtIndexes(body, starts)
	}
	return nil
}

// splitAtIndexes carves body into substrings — each starts at one of the
// given match start positions and extends to the next match start (or
// end-of-body for the last).
func splitAtIndexes(body string, starts [][]int) []string {
	out := make([]string, 0, len(starts))
	for i, m := range starts {
		var end int
		if i+1 < len(starts) {
			end = starts[i+1][0]
		} else {
			end = len(body)
		}
		out = append(out, body[m[0]:end])
	}
	return out
}

// kbBodyCitesGuideURL — Run-44 G1. Returns true when the body cites
// the guide via the canonical URL, anchored so a prefix-extending URL
// (e.g. `features/scaling-ha-advanced` for a `features/scaling-ha`
// guide) does NOT count as a citation.
//
// The URL is matched case-insensitive. Acceptable trailing characters:
// end-of-string, whitespace, `)`, `]`, `,`, `;`, `.`, `?`, `!`, `"`,
// `'`, `>`, `<`, `#` (fragment extension), `/` (trailing slash before
// path-end). Any other character — alphanumeric, `-`, `_` — extends
// the URL and rejects the match.
//
// Acceptable leading characters: start-of-string, whitespace, `(`,
// `[`, `<`, `'`, `"`. The validator also accepts the URL when
// preceded by `https://`, `http://`, or `//` (protocol-relative); the
// scheme prefix is folded into the match by stripping it off the
// scanned body before the check.
//
// The match is implemented as a substring scan with a per-hit anchor
// test; we don't compile a regex because the URL is dynamic per-topic.
func kbBodyCitesGuideURL(body, url string) bool {
	if url == "" {
		return false
	}
	lowerBody := strings.ToLower(body)
	lowerURL := strings.ToLower(url)
	// Trim any leading scheme prefix so the body's `https://docs.zerops.io/...`
	// matches the URL constant `docs.zerops.io/...`.
	start := 0
	for {
		idx := strings.Index(lowerBody[start:], lowerURL)
		if idx < 0 {
			return false
		}
		absStart := start + idx
		absEnd := absStart + len(lowerURL)
		// Anchored tail check — character right after the URL must NOT
		// extend it into a different path segment / domain. Allowed:
		// path-boundary characters (whitespace, punctuation, `#`, `/`,
		// `)`) or end-of-string.
		if absEnd < len(lowerBody) {
			next := lowerBody[absEnd]
			if isURLPathChar(next) {
				start = absStart + 1
				continue
			}
		}
		return true
	}
}

// isURLPathChar reports whether the byte would extend a URL path
// segment past the canonical match point. Mirrors RFC 3986 unreserved
// path characters minus the boundary delimiters our anchor allows.
func isURLPathChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= 'A' && b <= 'Z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '-' || b == '_' || b == '.' || b == '~':
		// `.` is included — `features/scaling-ha.json` would extend the
		// match in a way the citation doesn't intend. Cosmetic dot at
		// end of sentence (`...scaling-ha.`) gets caught here too, but
		// real prose pastes the URL inside parens / brackets / markdown
		// link form, so this is acceptable: citation patrols are
		// authoring-time, and authors don't end sentences with a bare
		// URL.
		return true
	}
	return false
}

// validateCrossSurfaceUniqueness — run-8 §2.D + spec-content-surfaces.md
// "Cross-surface duplication" rule. A fact's Topic appears on exactly
// one stitched surface body (cross-references via "See:" allowed but
// not validated here — exactness on the Topic key per Q6).
//
// surfaces maps filename → body (caller collects them from disk).
// facts is the publishable facts log (C-filtered upstream).
func validateCrossSurfaceUniqueness(surfaces map[string]string, facts []FactRecord) []Violation {
	var vs []Violation
	for _, f := range facts {
		if f.Topic == "" {
			continue
		}
		var surfaceHits []string
		for name, body := range surfaces {
			if strings.Contains(strings.ToLower(body), strings.ToLower(f.Topic)) {
				surfaceHits = append(surfaceHits, name)
			}
		}
		if len(surfaceHits) > 1 {
			vs = append(vs, Violation{
				Code:    "cross-surface-duplication",
				Path:    strings.Join(surfaceHits, ", "),
				Message: fmt.Sprintf("fact topic %q appears on %d surfaces; each topic must land on exactly one", f.Topic, len(surfaceHits)),
			})
		}
	}
	return vs
}
