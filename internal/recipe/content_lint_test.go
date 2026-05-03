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
		if strings.HasPrefix(trimmed, "```") {
			if !inYAML {
				lang := strings.TrimPrefix(trimmed, "```")
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
				if strings.HasPrefix(trimmed, "```") {
					if !inYAML {
						lang := strings.TrimPrefix(trimmed, "```")
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
