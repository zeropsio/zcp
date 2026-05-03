package recipe

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// run-22 followup F-3.5 — duplicated-teaching structural lint.
//
// Run-22 R2-RC-1 caught setup-name drift across THREE atoms via the
// methodology "pick a rule, grep all atoms, count where it's taught".
// The same methodology applied to other rules almost certainly finds
// more drift. Without a structural lint, this remains a per-run audit
// cost — every run-N analysis has to re-do the sweep.
//
// This test makes the catalog-drift test (system.md §4) mechanical at
// the TEACH-channel side: every load-bearing rule has ONE canonical
// home. Fingerprints must be NARROW enough to match only authoritative
// teaching, not casual reference. Citation-shape ("...") and
// editorial-pass rubric backstops are NOT load-bearing teaching by
// system.md §4 channel hierarchy and are excluded by fingerprint
// design.
//
// Initial registry seed (5-10 rules per spec). Future rule additions
// extend `loadBearingRules` after a 30-min audit confirms the rule
// has exactly one authoritative teaching site. Until F-13 (manifest)
// lands, new load-bearing rules don't auto-register.
func TestNoLoadBearingRuleDrift(t *testing.T) {
	t.Parallel()

	for _, rule := range loadBearingRules {
		t.Run(rule.name, func(t *testing.T) {
			t.Parallel()
			hits := walkAtomCorpus(t, rule.fingerprintRE)
			if len(hits) == 0 {
				t.Fatalf("rule %q: fingerprint matched zero atoms — canonical site %q drifted, the regex needs an update; see system.md §4 catalog-drift test",
					rule.name, rule.canonicalAtom)
			}
			if len(hits) == 1 {
				if hits[0].path != rule.canonicalAtom {
					t.Errorf("rule %q: fingerprint matched %s, expected canonical %s",
						rule.name, hits[0].path, rule.canonicalAtom)
				}
				return
			}
			// Multiple hits — the duplicated-teaching drift this lint
			// catches. Surface every off-canonical site so the fix in
			// the same commit is grep-driven.
			for _, h := range hits {
				if h.path == rule.canonicalAtom {
					continue
				}
				t.Errorf("rule %q: load-bearing teaching duplicated at %s:%d (canonical home is %s). Remove the duplicate or extract a shared atom; the off-canonical line was: %q",
					rule.name, h.path, h.line, rule.canonicalAtom, strings.TrimSpace(h.text))
			}
		})
	}
}

type loadBearingRule struct {
	name          string
	canonicalAtom string         // path relative to repo root
	fingerprintRE *regexp.Regexp // matches authoritative teaching only
}

type ruleHit struct {
	path string
	line int
	text string
}

// loadBearingRules is the hand-curated registry. Each fingerprint is
// chosen to match ONLY the authoritative teaching site, not casual
// references or cross-citations. When the lint catches a new
// duplication, the resolution is to rewrite the off-canonical site as
// a cross-reference (or extract a shared atom), NOT to broaden the
// fingerprint.
var loadBearingRules = []loadBearingRule{
	{
		// Run-22 R2-RC-1. Drift was atoms teaching `setup: appstage`
		// while engine emits `setup: prod`. Three atoms had to be
		// corrected. Fingerprint: the BOLD-emphasised `**ALWAYS**`
		// canonical phrasing in themes/core.md, not the bare-quote
		// `"ALWAYS use generic..."` cross-references in
		// cross-service-urls.md / spa_static_runtime.md.
		name:          "setup-name generic-vs-slot rule",
		canonicalAtom: "internal/knowledge/themes/core.md",
		fingerprintRE: regexp.MustCompile(`\*\*ALWAYS\*\* use generic\b`),
	},
	{
		// Run-22 R2-RC-5. Edit-in-place during feature phase.
		// Fingerprint: the H2 heading shape in mount-vs-container.md,
		// not the in-text quote in tests / brief composers.
		name:          "edit-in-place during feature phase",
		canonicalAtom: "internal/recipe/content/principles/mount-vs-container.md",
		fingerprintRE: regexp.MustCompile(`(?m)^## During feature phase: edit in place, do not redeploy dev slots\s*$`),
	},
	{
		// Run-20 C3. Bare-yaml at scaffold contract.
		// Fingerprint: the H2-position canonical statement at the top
		// of bare-yaml-prohibition.md ("The bare yaml is the scaffold
		// contract."), not the em-dash mid-sentence cross-reference in
		// content_authoring.md.
		name:          "bare-yaml at scaffold contract",
		canonicalAtom: "internal/recipe/content/principles/bare-yaml-prohibition.md",
		fingerprintRE: regexp.MustCompile(`(?m)^The bare yaml is the scaffold contract\.`),
	},
	{
		// Run-22 R1-RC-7. Tier-promotion narrative ban (refinement
		// rubric is the authority). Fingerprint: the H3 heading shape
		// in embedded_rubric.md.
		name:          "tier-promotion narrative ban",
		canonicalAtom: "internal/recipe/content/briefs/refinement/embedded_rubric.md",
		fingerprintRE: regexp.MustCompile(`(?m)^### Tier-promotion narrative \(forbidden per spec §108\)\s*$`),
	},
	{
		// Run-22 R1-RC-2. Same-key shadow trap teaching at scaffold
		// (platform_principles.md). Fingerprint: the BOLD anchor used
		// only at the canonical site; the other lint
		// (TestNoBriefAtomTeachesSameKeyShadow) catches the YAML-block
		// VIOLATION shape, this one catches the TEACHING claim.
		name:          "same-key shadow trap teaching",
		canonicalAtom: "internal/recipe/content/briefs/scaffold/platform_principles.md",
		fingerprintRE: regexp.MustCompile(`\*\*Same-key shadow trap\*\* — declaring`),
	},
	{
		// Run-22 R3-C-1. Subdomain rotate overclaim is flagged by the
		// refinement rubric. Fingerprint: the H3 heading in
		// embedded_rubric.md ("Subdomain rotation overclaim").
		name:          "subdomain rotation overclaim guard",
		canonicalAtom: "internal/recipe/content/briefs/refinement/embedded_rubric.md",
		fingerprintRE: regexp.MustCompile(`(?m)^### Subdomain rotation overclaim \(factual\)\s*$`),
	},
}

// walkAtomCorpus finds every match of the fingerprint across the atom
// corpus + knowledge corpus. Returns one ruleHit per matched line so
// the lint can attribute drift precisely.
func walkAtomCorpus(t *testing.T, re *regexp.Regexp) []ruleHit {
	t.Helper()
	var hits []ruleHit

	// 1. Embedded recipe content tree.
	embeddedRoots := []string{
		"content/briefs",
		"content/principles",
		"content/phase_entry",
	}
	for _, root := range embeddedRoots {
		err := fs.WalkDir(recipeV3Content, root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".md") {
				return nil
			}
			data, rerr := fs.ReadFile(recipeV3Content, p)
			if rerr != nil {
				return rerr
			}
			body := string(data)
			if !re.MatchString(body) {
				return nil
			}
			// Map embedded path to repo-relative path so error messages
			// point at the on-disk file users edit.
			repoPath := "internal/recipe/" + p
			for i, line := range strings.Split(body, "\n") {
				if re.MatchString(line) {
					hits = append(hits, ruleHit{path: repoPath, line: i + 1, text: line})
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk recipe content/%s: %v", root, err)
		}
	}

	// 2. Knowledge corpus on disk (themes + guides). Sibling dir to
	// internal/recipe/, no go:embed handle here.
	for _, root := range []string{"../knowledge/themes", "../knowledge/guides"} {
		hits = append(hits, walkKnowledgeDir(t, re, root)...)
	}

	return hits
}

// walkKnowledgeDir walks an on-disk knowledge subdirectory and returns
// hits for the fingerprint regex. Tolerates a missing root upfront
// (minimal CI shape) but propagates all other errors so silent
// mis-walks surface in CI.
func walkKnowledgeDir(t *testing.T, re *regexp.Regexp, root string) []ruleHit {
	t.Helper()
	var hits []ruleHit
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return hits
		}
		t.Fatalf("stat %s: %v", root, err)
	}
	walkErr := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		body := string(data)
		if !re.MatchString(body) {
			return nil
		}
		// Map ../knowledge/<sub>/<file> back to internal/knowledge/<sub>/<file>.
		repoPath := "internal/knowledge/" + strings.TrimPrefix(filepath.ToSlash(p), "../knowledge/")
		for i, line := range strings.Split(body, "\n") {
			if re.MatchString(line) {
				hits = append(hits, ruleHit{path: repoPath, line: i + 1, text: line})
			}
		}
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}
	return hits
}
