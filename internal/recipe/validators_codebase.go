package recipe

import (
	"context"
	"fmt"
	"regexp"
	"strings"
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

// igPlainOrderedItemRE matches plain ordered-list items (`1. `, `2. `)
// that aren't preceded by `###`. The pre-§R shape; rejected (R-1).
var igPlainOrderedItemRE = regexp.MustCompile(`(?m)^\d+\.\s+\S`)

// validateCodebaseIG checks the integration-guide fragment: marker
// present, ≥ 2 `### N.` heading items, first item introduces
// `zerops.yaml`, no scaffold-only filenames in body, item count
// within the surface's ItemCap. Plain ordered-list items (without the
// heading shape) are rejected (run-11 gap R-1) — the engine generates
// item #1 in heading shape; porter-authored items must match.
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
	// Scaffold-only filenames — `migrate.ts`, `main.ts`, `seed.ts`,
	// `api.ts`, helper names. Porter bringing their own code doesn't
	// have these files; an IG item naming them is giving scaffold
	// implementation, not a platform contract.
	scaffoldOnly := []string{
		"migrate.ts", "seed.ts", "main.ts", "api.ts",
	}
	for _, name := range scaffoldOnly {
		if strings.Contains(ig, name) {
			vs = append(vs, violation("codebase-ig-scaffold-filename", path,
				fmt.Sprintf("IG mentions scaffold-only filename %q — porters bringing their own code don't have it", name)))
		}
	}
	// Run-15 E.2 — authoring-tool patrol. The porter operates with
	// framework-canonical commands; tool names like `zerops_*` / `zcli`
	// signal authoring leakage.
	vs = append(vs, scanAuthoringToolLeaks(path, ig, "codebase IG")...)
	return vs, nil
}

// kbTripleFormatRE flags KB bullets that open with the
// `**symptom**:` / `**mechanism**:` / `**fix**:` debugging-runbook
// triple. That shape belongs in CLAUDE.md/notes; KB is porter-facing
// `**Topic** — explanation`. Run-10-readiness §O.
var kbTripleFormatRE = regexp.MustCompile(`(?m)^\s*[-*]\s+\*\*(symptom|mechanism|fix)\*\*\s*:`)

// validateCodebaseKB — knowledge-base fragment contract. Every bullet
// starts with a bold symptom; any bullet whose topic appears in the
// CitationMap must include the guide-id reference. Bullets opening with
// the `**symptom**:` triple are flagged — debugging runbooks live in
// CLAUDE.md/notes, KB uses `**Topic** — prose`.
func validateCodebaseKB(_ context.Context, path string, body []byte, inputs SurfaceInputs) ([]Violation, error) {
	s := string(body)
	var vs []Violation
	kb := extractBetweenMarkers(s, "knowledge-base")
	if kb == "" {
		vs = append(vs, violation("codebase-kb-marker-missing", path,
			"knowledge-base marker missing or body empty"))
		return vs, nil
	}
	// Bullet count.
	bulletRE := regexp.MustCompile(`(?m)^\s*-\s+\S`)
	bullets := bulletRE.FindAllStringIndex(kb, -1)
	boldBullets := boldBulletRE.FindAllStringIndex(kb, -1)
	if len(bullets) > 0 && len(boldBullets) < len(bullets) {
		vs = append(vs, notice("kb-missing-bold-symptom", path,
			fmt.Sprintf("%d of %d KB bullets lack a **bold symptom** opening", len(bullets)-len(boldBullets), len(bullets))))
	}
	// Run-15 F.5 — KB bullet cap (8 per codebase, per spec Surface 5).
	// Run-14 shipped 11-12; that's over-collection. Read the cap from
	// SurfaceContract so spec edits stay single-source.
	if contract, ok := ContractFor(SurfaceCodebaseKB); ok && contract.ItemCap > 0 {
		if len(bullets) > contract.ItemCap {
			vs = append(vs, violation("codebase-kb-too-many-bullets", path,
				fmt.Sprintf(
					"%d KB bullets > %d cap (spec §Surface 5: 5-8 bullets per codebase). Over-collection in KB usually means scaffold decisions, framework quirks, or self-inflicted observations that should be discarded or routed elsewhere — see spec §Counter-examples.",
					len(bullets), contract.ItemCap,
				)))
		}
	}
	for _, m := range kbTripleFormatRE.FindAllString(kb, -1) {
		vs = append(vs, notice("codebase-kb-triple-format-banned", path,
			fmt.Sprintf("KB entries use `**Topic** — prose` format; `**symptom**:` / `**mechanism**:` / `**fix**:` triples belong in CLAUDE.md/notes: %q",
				trimForMessage(strings.TrimSpace(m)))))
	}
	// Citation-required: for every topic in CitationMap that appears
	// anywhere in the KB body, the body must also reference the guide id.
	for topic, guide := range CitationMap {
		if !strings.Contains(strings.ToLower(kb), strings.ToLower(topic)) {
			continue
		}
		// Guide id reference: allow the guide id or its canonical name
		// (they're identical in CitationMap but future-proof for alias).
		if !strings.Contains(kb, guide) {
			vs = append(vs, violation("kb-citation-missing", path,
				fmt.Sprintf("KB mentions %q but does not cite `zerops_knowledge` guide %q", topic, guide)))
		}
	}
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

	// Run-16 §8.1 backstop — Zerops-flavored content must NOT appear in
	// CLAUDE.md. Slot-shape refusal at record-time should have caught
	// this; if it didn't, the validator blocks publication.
	//
	// Run-16 reviewer minor — uses the same word-boundary regexes as
	// slot_shape.checkClaudeMDAll so the validator and record-time
	// refusal agree on what counts as a leak. Substring matching on
	// "zerops_" was looser (would match `Zerops_v1` in regular prose);
	// `\bzerops_[a-z_]+` matches only the tool-name shape.
	bodyStr := string(body)
	if zeropsHeadingRe.MatchString(bodyStr) {
		vs = append(vs, violation("claude-md-zerops-heading", path,
			"`## Zerops` heading found — Zerops platform content belongs in IG/KB/zerops.yaml comments, not CLAUDE.md (R-15-4 closure)"))
	}
	leakChecks := []struct {
		re    *regexp.Regexp
		token string
	}{
		{zscRe, "zsc"},
		{zeropsToolRe, "zerops_*"},
		{zcpRe, "zcp"},
		{zcliRe, "zcli"},
	}
	for _, lc := range leakChecks {
		if lc.re.MatchString(bodyStr) {
			vs = append(vs, violation("claude-md-tool-leak", path,
				fmt.Sprintf("authoring-tool token %q found — CLAUDE.md is the porter's `/init` guide, framework-canonical commands only", lc.token)))
			break // single notice per body — agent re-authors holistically
		}
	}

	// Legacy forbidden-subsection patrol stays as Notice for back-compat
	// (recipes synthesized via legacy sub-slot path may still carry
	// these headings).
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
