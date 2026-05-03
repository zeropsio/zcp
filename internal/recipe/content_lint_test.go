package recipe

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// scanYAMLBoxDrawing walks the body, tracking ```yaml fenced blocks,
// and reports any U+2500..U+257F (box-drawing) or U+2580..U+259F
// (block-elements) codepoint on a line inside such a block. Used by
// the knowledge + recipe-content unicode-separator regression tests.
func scanYAMLBoxDrawing(t *testing.T, path, body string) {
	t.Helper()
	lines := strings.Split(body, "\n")
	inYAML := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if lang, ok := strings.CutPrefix(trimmed, "```"); ok {
			if !inYAML {
				if lang == "yaml" || lang == "yml" {
					inYAML = true
				}
			} else {
				inYAML = false
			}
			continue
		}
		if !inYAML {
			continue
		}
		for _, r := range line {
			if (r >= 0x2500 && r <= 0x257F) || (r >= 0x2580 && r <= 0x259F) {
				t.Errorf("%s:%d yaml block contains forbidden box-drawing/block-element codepoint U+%04X: %q", path, i+1, r, line)
				break
			}
		}
	}
}

// run-22 R1-RC-2 / R1-RC-4 / R1-RC-7 — content-lint regressions for
// atom corpus quality. These walk the embedded `content/` tree (and
// the wider knowledge corpus where applicable) to pin invariants
// established by run-22 dogfood: project-level shadow trap, Unicode
// box-drawing in yaml blocks, tier-promotion narrative refinement
// rubric. See docs/zcprecipator3/runs/22/FIX_SPEC.md.

// TestBrief_TeachesProjectLevelShadowTrap — run-22 RC-2. The
// scaffold/codebase-content `platform_principles.md` brief must
// extend the same-key shadow warning to project-level vars
// (`${APP_SECRET}`, `${STAGE_API_URL}`), not just cross-service
// auto-injects (`${db_hostname}`). Authoritative source:
// internal/knowledge/guides/environment-variables.md L97-115.
func TestBrief_TeachesProjectLevelShadowTrap(t *testing.T) {
	t.Parallel()
	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	body := brief.Body
	// Must teach the APP_SECRET variant.
	if !strings.Contains(body, "${APP_SECRET}") {
		t.Errorf("scaffold brief missing project-level shadow example ${APP_SECRET}")
	}
	// Must explicitly call out project-level scope.
	if !strings.Contains(body, "Project-level") && !strings.Contains(body, "project-level") {
		t.Errorf("scaffold brief missing the word `project-level` in shadow teaching")
	}
	// Sanity: the shadow-trap heading still anchors the section.
	if !strings.Contains(body, "Same-key shadow trap") {
		t.Errorf("scaffold brief missing `Same-key shadow trap` anchor")
	}
}

// TestRefinementRubric_ForbidsTierPromotionNarrative — run-22 RC-7.
// Spec §108 forbids "promote to tier N+1" / "outgrow" / "graduate"
// narratives in tier README intros. The refinement rubric must
// enumerate the regex set so refinement has reason to flag.
// Run-22 evidence: tier 4 README intro shipped "promote to tier 5
// when one of them becomes the bottleneck".
func TestRefinementRubric_ForbidsTierPromotionNarrative(t *testing.T) {
	t.Parallel()
	body, err := readAtom("briefs/refinement/embedded_rubric.md")
	if err != nil {
		t.Fatalf("read embedded_rubric.md: %v", err)
	}
	for _, mustHave := range []string{
		`\bpromote\b.*\btier\b`,
		`\boutgrow\w*`,
		`\bupgrade from tier\b`,
		`\bgraduate (to|out of)\b`,
		`\bmove (up|to) tier\b`,
		"Tier-promotion narrative",
	} {
		if !strings.Contains(body, mustHave) {
			t.Errorf("embedded_rubric.md missing tier-promotion guard %q", mustHave)
		}
	}
}

// TestBuildRefinementBrief_TeachesTierPromotionGuard — sanity that
// the rubric reaches the refinement brief end-to-end, not just the
// embedded atom file.
func TestBuildRefinementBrief_TeachesTierPromotionGuard(t *testing.T) {
	t.Parallel()
	plan := &Plan{Slug: "x", Codebases: []Codebase{{Hostname: "api"}}}
	brief, err := BuildRefinementBrief(plan, nil, "/run", nil)
	if err != nil {
		t.Fatalf("BuildRefinementBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "Tier-promotion narrative") {
		t.Errorf("refinement brief missing `Tier-promotion narrative` rubric section")
	}
	if !strings.Contains(brief.Body, `\bpromote\b.*\btier\b`) {
		t.Errorf("refinement brief missing tier-promotion regex anchor")
	}
}

// TestYamlCommentStyleAtom_ForbidsUnicodeBoxDrawing — run-22 RC-4.
// The yaml-comment-style atom enumerates ASCII variants in its
// anti-pattern list (`# =====`, `# ---`) but pre-fix did NOT include
// Unicode box-drawing (`# ──`). The agent inferred "not on the list,
// must be fine" and produced 60-char U+2500 separator runs across
// run-22 zerops.yamls. Pin the explicit Unicode forbid in the atom.
func TestYamlCommentStyleAtom_ForbidsUnicodeBoxDrawing(t *testing.T) {
	t.Parallel()
	body, err := readAtom("principles/yaml-comment-style.md")
	if err != nil {
		t.Fatalf("read yaml-comment-style.md: %v", err)
	}
	// Either explicit codepoint name OR the literal box-drawing glyph
	// in the anti-pattern enumeration is acceptable; the spec calls
	// for the codepoint range to be named so authors can search.
	if !strings.Contains(body, "U+2500") {
		t.Errorf("yaml-comment-style.md anti-pattern list missing `U+2500` codepoint anchor")
	}
	if !strings.Contains(body, "box-drawing") && !strings.Contains(body, "Box-drawing") {
		t.Errorf("yaml-comment-style.md anti-pattern list missing word `box-drawing`")
	}
}

// TestNoKnowledgeAtomTeachesUnicodeSeparators — run-22 RC-4 sweep.
// Walk every recipe atom under `internal/knowledge/recipes/`; fail
// if any line inside a yaml fenced block contains a U+2500..U+257F
// or U+2580..U+259F codepoint. Diagrams in non-yaml fenced blocks
// (e.g. ASCII-art network topology in guides like networking.md)
// are out of scope — the harm is yaml comments rendering as
// mojibake on porter terminals, and yaml is the only target surface
// that gets baked into deliverable recipes.
func TestNoKnowledgeAtomTeachesUnicodeSeparators(t *testing.T) {
	t.Parallel()
	// Tests run from internal/recipe; knowledge corpus is sibling.
	root := filepath.Join("..", "knowledge", "recipes")
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			// Tolerate missing root (e.g. on minimal CI shape).
			if filepath.Base(p) == filepath.Base(root) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		scanYAMLBoxDrawing(t, p, string(data))
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
}

// TestNoBriefAtomTeachesUnicodeSeparators — run-22 RC-4. Same sweep
// over `internal/recipe/content/`. Catches any future leak into
// brief atoms.
func TestNoBriefAtomTeachesUnicodeSeparators(t *testing.T) {
	t.Parallel()
	roots := []string{
		"content/briefs",
		"content/principles",
	}
	for _, root := range roots {
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
			scanYAMLBoxDrawing(t, p, string(data))
			return nil
		})
		if err != nil {
			t.Fatalf("walk recipe/%s: %v", root, err)
		}
	}
}

// TestNoBriefAtomTeachesSameKeyShadow — run-22 RC-2 regression. Walk
// every atom under `internal/recipe/content/briefs/` and
// `internal/recipe/content/principles/`; fail if any yaml fenced
// block contains a self-shadow line (`KEY: ${KEY}` with the same
// identifier). Catches future drift in any atom.
func TestNoBriefAtomTeachesSameKeyShadow(t *testing.T) {
	t.Parallel()

	// Walk only well-known authored content roots.
	roots := []string{
		"content/briefs",
		"content/principles",
	}
	// Lines that intentionally demonstrate the trap as anti-pattern
	// must use distinct examples (e.g. `db_hostname: ${db_hostname}`)
	// inside prose, NOT inside a yaml fenced block. This test scans
	// only inside ```yaml fences.
	selfShadow := regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_]*)\s*:\s*\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	for _, root := range roots {
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
			lines := strings.Split(string(data), "\n")
			inYAML := false
			for i, line := range lines {
				trimmed := strings.TrimSpace(line)
				if lang, ok := strings.CutPrefix(trimmed, "```"); ok {
					if !inYAML {
						if lang == "yaml" || lang == "yml" {
							inYAML = true
						}
					} else {
						inYAML = false
					}
					continue
				}
				if !inYAML {
					continue
				}
				m := selfShadow.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				if m[1] == m[2] {
					t.Errorf("%s:%d teaches self-shadow pattern %q in yaml block", p, i+1, strings.TrimSpace(line))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}

// run-22 Round 2 regressions.

// TestAtomSetupNamesMatchRoleContract — run-22 R2-RC-1. Walk every atom
// under `internal/recipe/content/`; for each `- setup: <name>` line in
// a yaml fenced block, assert `<name>` is in the union of
// `RoleContract.ZeropsSetupDev` / `ZeropsSetupProd` across all roles
// (`dev` / `prod`). Slot-named setups (`appdev` / `apistage` /
// `workerdev`) drift from `themes/core.md`'s "ALWAYS use generic
// `setup:` names" rule and leave tier import.yamls' `zeropsSetup: prod`
// references orphaned. Pin the rule across the whole content tree.
func TestAtomSetupNamesMatchRoleContract(t *testing.T) {
	t.Parallel()

	allowed := map[string]bool{}
	for _, role := range Roles() {
		c, ok := role.Contract()
		if !ok {
			continue
		}
		allowed[c.ZeropsSetupDev] = true
		allowed[c.ZeropsSetupProd] = true
	}
	// Sanity — tests rely on the canonical pair.
	if !allowed["dev"] || !allowed["prod"] {
		t.Fatalf("role contract should include `dev` and `prod` setups; got %v", allowed)
	}

	setupRE := regexp.MustCompile(`^\s*-\s*setup:\s*([A-Za-z0-9_-]+)\s*$`)
	roots := []string{
		"content/briefs",
		"content/principles",
	}
	for _, root := range roots {
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
			lines := strings.Split(string(data), "\n")
			inYAML := false
			for i, line := range lines {
				trimmed := strings.TrimSpace(line)
				if lang, ok := strings.CutPrefix(trimmed, "```"); ok {
					if !inYAML {
						if lang == "yaml" || lang == "yml" {
							inYAML = true
						}
					} else {
						inYAML = false
					}
					continue
				}
				if !inYAML {
					continue
				}
				m := setupRE.FindStringSubmatch(line)
				if m == nil {
					continue
				}
				name := m[1]
				if !allowed[name] {
					t.Errorf("%s:%d uses non-generic setup name %q in yaml block; allowed: %v (themes/core.md `ALWAYS use generic setup: names`)", p, i+1, name, sortedAllowedSetups(allowed))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", root, err)
		}
	}
}

func sortedAllowedSetups(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	// stable order for error messages
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// TestFeatureBrief_TeachesEditInPlace — run-22 R2-RC-5. The feature
// brief loads `principles/mount-vs-container.md` (per Table B); the
// edit-in-place rule MUST reach feature-phase agents so they stop
// thrashing dev slots with redundant `zerops_deploy` calls. Run-22
// evidence: 5 unnecessary feature-phase dev redeploys (apidev, appdev,
// workerdev) reasoned as "make new code live" / "apply env-var
// changes". The mount IS already live; restart the dev server for
// env-var changes; redeploy stage only at end of feature.
func TestFeatureBrief_TeachesEditInPlace(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildFeatureBrief(plan)
	if err != nil {
		t.Fatalf("BuildFeatureBrief: %v", err)
	}
	for _, anchor := range []string{
		"edit in place",
		"do not redeploy dev slots",
		// At least one of the forbidden examples reaches the brief.
		"zerops_deploy targetService=<host>dev",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("feature brief missing edit-in-place anchor %q", anchor)
		}
	}
}

// TestScaffoldBrief_TeachesEditInPlace — same atom reaches scaffold
// via the shared `principles/mount-vs-container.md` load.
func TestScaffoldBrief_TeachesEditInPlace(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildScaffoldBrief(plan, plan.Codebases[0], nil)
	if err != nil {
		t.Fatalf("BuildScaffoldBrief: %v", err)
	}
	if !strings.Contains(brief.Body, "edit in place") {
		t.Error("scaffold brief missing edit-in-place anchor (shared principles/mount-vs-container.md)")
	}
	if !strings.Contains(brief.Body, "do not redeploy dev slots") {
		t.Error("scaffold brief missing dev-slot redeploy forbid anchor")
	}
}

// TestContentExtensionAtom_MarkedDeprecated — run-22 R2-RC-5. The atom
// is no longer loaded by `BuildFeatureBrief` (retired in run-16 §6.2)
// and feature-phase teaching now lives in mount-vs-container.md. The
// header comment marks the atom as deprecated so future authors don't
// extend it.
func TestContentExtensionAtom_MarkedDeprecated(t *testing.T) {
	t.Parallel()

	body, err := readAtom("briefs/feature/content_extension.md")
	if err != nil {
		t.Fatalf("read atom: %v", err)
	}
	if !strings.Contains(body, "Deprecated") {
		t.Error("content_extension.md must carry a Deprecated marker (run-22 R2-RC-5)")
	}
	if !strings.Contains(body, "no longer loaded") {
		t.Error("content_extension.md deprecation note must explain the atom is no longer loaded")
	}
}

// TestEnvContentBrief_KeepsTierFlavorComments — run-22 R2-RC-6. The
// per_tier_authoring atom must distinguish "canonical-set dedup"
// (strip cross-tier repetition) from "per-tier flavor" (keep 1-2 line
// framing on every service block at every tier). Run-22 stripped
// flavor along with canonical-set, leaving tiers 1-3 with 6 lines vs
// golden ~25.
func TestEnvContentBrief_KeepsTierFlavorComments(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	brief, err := BuildEnvContentBrief(plan, nil, nil)
	if err != nil {
		t.Fatalf("BuildEnvContentBrief: %v", err)
	}
	for _, anchor := range []string{
		"canonical-set",
		"per-tier flavor",
		// The new rule explicitly calls out keeping a 1-2 line block
		// even when the field shape is identical to the prior tier.
		"1-2 line",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("env-content brief missing tier-flavor anchor %q", anchor)
		}
	}
}

// TestPerTierAuthoringAtom_DistinguishesCanonicalSetFromFlavor — atom-
// level pin for R2-RC-6. The clarification text must be present on
// disk regardless of how the brief composer evolves.
func TestPerTierAuthoringAtom_DistinguishesCanonicalSetFromFlavor(t *testing.T) {
	t.Parallel()

	body, err := readAtom("briefs/env-content/per_tier_authoring.md")
	if err != nil {
		t.Fatalf("read atom: %v", err)
	}
	for _, anchor := range []string{
		"Cross-tier dedup is for the canonical-set teaching",
		"per-tier flavor",
		"Keep at least 1-2 lines of flavor framing",
	} {
		if !strings.Contains(body, anchor) {
			t.Errorf("per_tier_authoring.md missing anchor %q", anchor)
		}
	}
}

// TestFeatureBrief_TeachesQueueGroup_ForShowcaseWorker — run-22 R2-WK-1.
// The codebase-content brief (loaded for showcase worker codebases via
// showcase_tier_supplements.md) MUST teach the queue-group requirement
// for NATS subscriptions. Run-22 evidence:
// `this.nc.subscribe(ITEMS_EVENT_SUBJECT)` without queue option →
// every replica double-indexes at tier 4-5 (minContainers: 2).
func TestFeatureBrief_TeachesQueueGroup_ForShowcaseWorker(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.Tier = tierShowcase
	var worker Codebase
	for _, cb := range plan.Codebases {
		if cb.IsWorker {
			worker = cb
			break
		}
	}
	if worker.Hostname == "" {
		t.Fatalf("synthetic plan must include a worker codebase")
	}
	brief, err := BuildCodebaseContentBrief(plan, worker, nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	for _, anchor := range []string{
		"queue group",
		"queue: 'workers'",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("showcase worker brief missing queue-group anchor %q", anchor)
		}
	}
}

// TestFeatureBrief_TeachesDrainShutdown_ForShowcaseWorker — run-22
// R2-WK-2. The showcase-worker brief MUST teach `await sub.drain()`
// shutdown ordering — `unsubscribe()` drops in-flight events on
// rolling deploys.
func TestFeatureBrief_TeachesDrainShutdown_ForShowcaseWorker(t *testing.T) {
	t.Parallel()

	plan := syntheticShowcasePlan()
	plan.Tier = tierShowcase
	var worker Codebase
	for _, cb := range plan.Codebases {
		if cb.IsWorker {
			worker = cb
			break
		}
	}
	brief, err := BuildCodebaseContentBrief(plan, worker, nil, nil)
	if err != nil {
		t.Fatalf("BuildCodebaseContentBrief: %v", err)
	}
	for _, anchor := range []string{
		"drain",
		"unsubscribe()",
	} {
		if !strings.Contains(brief.Body, anchor) {
			t.Errorf("showcase worker brief missing drain-shutdown anchor %q", anchor)
		}
	}
}

// TestShowcaseTierSupplementsAtom_NamesValidatorGate — atom-level pin.
// The atom must reference the new validator gate file so future authors
// know enforcement teeth exist.
func TestShowcaseTierSupplementsAtom_NamesValidatorGate(t *testing.T) {
	t.Parallel()

	body, err := readAtom("briefs/codebase-content/showcase_tier_supplements.md")
	if err != nil {
		t.Fatalf("read atom: %v", err)
	}
	if !strings.Contains(body, "validators_worker_subscription.go") {
		t.Error("showcase_tier_supplements.md must reference the validator gate (validators_worker_subscription.go)")
	}
}
