package recipe

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	{
		// Run-22 R3-RC-3. URL constants two-channel sync —
		// `zerops_env action=set` populates the live workspace project
		// envs, `update-plan projectEnvVars` populates the published
		// tier yamls. Both are required; teaching ONE without the
		// other reintroduces the build-time-bake trap on the missing
		// channel. Canonical home is `principles/cross-service-urls.md`.
		// Fingerprint: the BOL "Both are required." statement that
		// closes the two-channel teaching paragraph (the H2 heading
		// "The canonical fix" sits above it). Run-22 fixup F-3.5
		// addition (codex review).
		name:          "URL constants two-channel sync (zerops_env + update-plan projectEnvVars)",
		canonicalAtom: "internal/recipe/content/principles/cross-service-urls.md",
		fingerprintRE: regexp.MustCompile(`(?m)^Both are required\. Setting one without the other reintroduces`),
	},
	{
		// Run-22 R2-WK-1 + R2-WK-2 (split atom landed at R2-WK-2).
		// Worker subscription queue-group + drain MANDATORY contract.
		// Canonical home moved to feature brief
		// `briefs/feature/worker_subscription_shape.md` after R2 split
		// (was previously combined into showcase_tier_supplements.md).
		// Fingerprint: the H1 title that names BOTH halves (queue group
		// + drain) as MANDATORY. The codebase-content sibling atom
		// `worker_kb_supplements.md` references this canonical home
		// rather than re-stating the contract. Run-22 fixup F-3.5
		// addition (codex review).
		name:          "Worker subscription queue-group + drain MANDATORY",
		canonicalAtom: "internal/recipe/content/briefs/feature/worker_subscription_shape.md",
		fingerprintRE: regexp.MustCompile(`(?m)^# Worker subscription code shape — queue group \+ drain are MANDATORY\s*$`),
	},
}

// walkAtomCorpus finds every match of the fingerprint across the atom
// corpus + knowledge corpus. Returns one ruleHit per matched line so
// the lint can attribute drift precisely.
func walkAtomCorpus(t *testing.T, re *regexp.Regexp) []ruleHit {
	t.Helper()
	corpus := loadCorpusForLint(t)
	return findRuleHits(re, corpus)
}

// loadCorpusForLint reads every md file under the embedded recipe
// content tree and the on-disk knowledge corpus into a flat
// repo-relative-path → body map. Extracted from walkAtomCorpus so the
// matcher logic can be unit-tested against a synthetic in-memory
// corpus (run-22 fixup F-3.5 self-test) — the live corpus produces
// zero violations in steady state, so a matcher that always returns
// nil would pass false-green without an injected-failure pin.
func loadCorpusForLint(t *testing.T) map[string]string {
	t.Helper()
	corpus := map[string]string{}

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
			// Map embedded path to repo-relative path so error messages
			// point at the on-disk file users edit.
			corpus["internal/recipe/"+p] = string(data)
			return nil
		})
		if err != nil {
			t.Fatalf("walk recipe content/%s: %v", root, err)
		}
	}

	// 2. Knowledge corpus on disk (themes + guides). Sibling dir to
	// internal/recipe/, no go:embed handle here.
	for _, root := range []string{"../knowledge/themes", "../knowledge/guides"} {
		readKnowledgeDirInto(t, root, corpus)
	}

	return corpus
}

// readKnowledgeDirInto reads every md file under root into the
// destination map keyed by repo-relative path. Tolerates a missing
// root upfront (minimal CI shape) but propagates all other errors.
func readKnowledgeDirInto(t *testing.T, root string, dst map[string]string) {
	t.Helper()
	if _, err := os.Stat(root); err != nil {
		if os.IsNotExist(err) {
			return
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
		repoPath := "internal/knowledge/" + strings.TrimPrefix(filepath.ToSlash(p), "../knowledge/")
		dst[repoPath] = string(data)
		return nil
	})
	if walkErr != nil {
		t.Fatalf("walk %s: %v", root, walkErr)
	}
}

// findRuleHits scans the corpus for fingerprint matches and returns
// one ruleHit per matched line so callers can attribute drift
// precisely. Pure: takes a path → body map, returns hits. Extracted
// so the lint logic itself can be exercised against a synthetic
// corpus (run-22 fixup F-3.5 self-test).
func findRuleHits(re *regexp.Regexp, corpus map[string]string) []ruleHit {
	// Iterate in sorted-key order so hits are deterministic across
	// test runs (Go map iteration order is randomised).
	paths := make([]string, 0, len(corpus))
	for p := range corpus {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var hits []ruleHit
	for _, p := range paths {
		body := corpus[p]
		if !re.MatchString(body) {
			continue
		}
		for i, line := range strings.Split(body, "\n") {
			if re.MatchString(line) {
				hits = append(hits, ruleHit{path: p, line: i + 1, text: line})
			}
		}
	}
	return hits
}

// TestLoadBearingRuleDrift_InjectedFailureSelfTest — run-22 fixup
// F-3.5 hardening. The original TestNoLoadBearingRuleDrift only
// verifies zero violations on the live corpus; if the matcher logic
// breaks (regex always returns nil; canonical-path comparison always
// evaluates true), the lint passes false-green and drift accumulates
// unobserved. This sub-test pins the lint LOGIC by:
//
//  1. Constructing a synthetic corpus with the fingerprint at the
//     canonical path only — assert ZERO drift hits flagged.
//  2. Adding an off-canonical hit — assert non-zero drift surfaces +
//     names the off-canonical path.
//  3. Removing the canonical hit entirely — assert the regex-matched-
//     zero-atoms branch fires (canonical site drifted away from the
//     fingerprint).
//
// Self-test executes findRuleHits + the rule-evaluation shape directly
// — same code path TestNoLoadBearingRuleDrift exercises, but driven
// by a synthetic corpus the test owns.
func TestLoadBearingRuleDrift_InjectedFailureSelfTest(t *testing.T) {
	t.Parallel()

	// Pick a representative rule from the live registry — the
	// setup-name generic-vs-slot rule has a narrow, easy-to-place
	// fingerprint (`**ALWAYS** use generic`) the synthetic corpus can
	// embed cleanly.
	var rule loadBearingRule
	for _, r := range loadBearingRules {
		if r.name == "setup-name generic-vs-slot rule" {
			rule = r
			break
		}
	}
	if rule.fingerprintRE == nil {
		t.Fatalf("self-test fixture drift: rule %q not in registry", "setup-name generic-vs-slot rule")
	}

	matchingLine := "**ALWAYS** use generic role-contract setup names"

	t.Run("canonical_only_zero_drift", func(t *testing.T) {
		t.Parallel()
		corpus := map[string]string{
			rule.canonicalAtom:                     "...prose before...\n" + matchingLine + "\n...prose after...",
			"internal/recipe/content/unrelated.md": "no fingerprint here\n",
		}
		hits := findRuleHits(rule.fingerprintRE, corpus)
		if len(hits) != 1 {
			t.Fatalf("canonical-only corpus: expected exactly 1 hit, got %d (%+v)", len(hits), hits)
		}
		if hits[0].path != rule.canonicalAtom {
			t.Errorf("canonical-only corpus: hit path %q, want canonical %q", hits[0].path, rule.canonicalAtom)
		}
	})

	t.Run("off_canonical_duplicate_drift_flagged", func(t *testing.T) {
		t.Parallel()
		offPath := "internal/recipe/content/principles/cross-service-urls.md"
		corpus := map[string]string{
			rule.canonicalAtom: "...\n" + matchingLine + "\n",
			offPath:            "drifted teaching: " + matchingLine + " (paraphrased)\n",
		}
		hits := findRuleHits(rule.fingerprintRE, corpus)
		if len(hits) < 2 {
			t.Fatalf("duplicated-teaching corpus: expected ≥2 hits, got %d (%+v)", len(hits), hits)
		}
		// The off-canonical hit must surface so the lint can attribute
		// the drift. If matcher logic broke (e.g. always returned nil),
		// this branch would silently pass — which is exactly the false-
		// green this self-test exists to catch.
		var sawOffCanonical bool
		for _, h := range hits {
			if h.path == offPath {
				sawOffCanonical = true
				break
			}
		}
		if !sawOffCanonical {
			t.Errorf("duplicated-teaching corpus: off-canonical site %q not in hits %+v", offPath, hits)
		}
	})

	t.Run("canonical_drifted_zero_hits", func(t *testing.T) {
		t.Parallel()
		// Fingerprint moved away from canonical (e.g. someone rewrote
		// the canonical line); nothing in the corpus matches. The
		// lint surfaces this as a drift signal too — fingerprint-
		// matched-zero-atoms branch in TestNoLoadBearingRuleDrift.
		corpus := map[string]string{
			rule.canonicalAtom:                     "the canonical phrasing got rewritten and no longer matches\n",
			"internal/recipe/content/unrelated.md": "neither does this\n",
		}
		hits := findRuleHits(rule.fingerprintRE, corpus)
		if len(hits) != 0 {
			t.Errorf("canonical-drifted corpus: expected 0 hits (fingerprint missing everywhere), got %d (%+v)", len(hits), hits)
		}
	})
}
