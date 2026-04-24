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

// validateCodebaseIG checks the integration-guide fragment: marker
// present, ≥ 2 numbered items, first item introduces `zerops.yaml`,
// no scaffold-only filenames in body.
func validateCodebaseIG(_ context.Context, path string, body []byte, _ SurfaceInputs) ([]Violation, error) {
	s := string(body)
	var vs []Violation
	ig := extractBetweenMarkers(s, "integration-guide")
	if ig == "" {
		vs = append(vs, violation("codebase-ig-marker-missing", path,
			"integration-guide marker missing or body empty"))
		return vs, nil
	}
	items := numberedItemRE.FindAllString(ig, -1)
	if len(items) < 2 {
		vs = append(vs, violation("codebase-ig-too-few-items", path,
			fmt.Sprintf("%d numbered items < 2 expected", len(items))))
	}
	// First numbered item must introduce zerops.yaml — IG is a porter's
	// step-by-step and the yaml is the first platform-specific change.
	if len(items) >= 1 {
		firstBlock := ig
		if idx := numberedItemRE.FindAllStringIndex(ig, 2); len(idx) >= 2 {
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
func validateCodebaseKB(_ context.Context, path string, body []byte, _ SurfaceInputs) ([]Violation, error) {
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
		vs = append(vs, violation("kb-missing-bold-symptom", path,
			fmt.Sprintf("%d of %d KB bullets lack a **bold symptom** opening", len(bullets)-len(boldBullets), len(bullets))))
	}
	for _, m := range kbTripleFormatRE.FindAllString(kb, -1) {
		vs = append(vs, violation("codebase-kb-triple-format-banned", path,
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
	return vs, nil
}

// validateCodebaseCLAUDE — size floor + minimum custom sections.
func validateCodebaseCLAUDE(_ context.Context, path string, body []byte, _ SurfaceInputs) ([]Violation, error) {
	var vs []Violation
	if len(body) < 1200 {
		vs = append(vs, violation("claude-md-too-short", path,
			fmt.Sprintf("%d bytes < 1200 minimum", len(body))))
	}
	// Count H2 / H3 headers beyond the default template headers
	// ("# CLAUDE.md", "## Zerops service facts", "## Zerops dev loop",
	// "## Notes"). Anything else is a "custom section."
	headerRE := regexp.MustCompile(`(?m)^##+\s+(.+)$`)
	matches := headerRE.FindAllStringSubmatch(string(body), -1)
	templateHeaders := map[string]bool{
		"Zerops service facts": true,
		"Zerops dev loop":      true,
		"Notes":                true,
	}
	custom := 0
	for _, m := range matches {
		title := strings.TrimSpace(m[1])
		if !templateHeaders[title] {
			custom++
		}
	}
	if custom < 2 {
		vs = append(vs, violation("claude-md-too-few-custom-sections", path,
			fmt.Sprintf("%d custom sections < 2 beyond template", custom)))
	}
	return vs, nil
}

// validateCodebaseYAML enforces the codebase yaml-comment contract.
// Decorative divider lines are banned (run-9 §2.H); surviving comments
// are grouped into BLOCKS — runs of adjacent `#` lines, with bare `#`
// treated as an in-block paragraph separator per the reference style at
// /Users/fxck/www/laravel-showcase-app/zerops.yaml. Each block passes if
// ANY line in it carries a causal word / em-dash; blocks whose lines
// are all short labels (≤40 chars after stripping the `#`) pass
// unconditionally. One violation per block, not per line — so a
// multi-line prose block that forgets rationale emits a single report.
// Run-10-readiness §N.
func validateCodebaseYAML(_ context.Context, path string, body []byte, _ SurfaceInputs) ([]Violation, error) {
	var vs []Violation
	for _, d := range yamlFindDividers(body) {
		vs = append(vs, violation("yaml-comment-divider-banned", path,
			fmt.Sprintf("decorative divider line violates yaml-comment-style (no dividers, no banners): %q",
				trimForMessage(string(d)))))
	}
	for _, block := range parseYAMLCommentBlocks(body) {
		if !blockNeedsCausalWord(block) {
			continue
		}
		if blockHasCausalWord(block) {
			continue
		}
		first := block[0]
		vs = append(vs, violation("yaml-comment-missing-causal-word", path,
			fmt.Sprintf("comment block lacks a causal word (`because`, `so that`, `otherwise`, `trade-off`, em-dash) on any line: %q",
				trimForMessage(first))))
	}
	return vs, nil
}

// parseYAMLCommentBlocks groups adjacent `#` comment lines into blocks.
// Bare `#` lines stay in-block (paragraph separators, reference style).
// Each returned block is a slice of comment bodies (already stripped of
// the leading `#` + whitespace). Divider lines and the zeropsPreprocessor
// directive are skipped — the divider violation is emitted separately
// and the directive is not a rationale comment.
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
		if comment != "" && yamlIsDivider("#"+comment) {
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

// blockHasCausalWord reports whether any line in the block carries a
// causal word / em-dash.
func blockHasCausalWord(block []string) bool {
	for _, line := range block {
		if line == "" {
			continue
		}
		if containsAnyCausal(line) {
			return true
		}
	}
	return false
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
