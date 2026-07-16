package content

import (
	"regexp"
	"strings"
	"testing"
)

// wantWelcomeSkillSlugs are the five curated skills v1 ships (spec §6
// W-SKILLS). Both TestReadWelcomeSkillsTree_FiveSlugsWithValidFrontMatter and
// TestWelcomeSkillsAllowlistMatchesEmbedded key off the embedded tree itself
// rather than duplicating this list as an independent "expected" set — the
// embedded tree IS the contract; this slice only pins the count and the
// no-name-typos property (every embedded slug matches one of these).
var wantWelcomeSkillSlugs = []string{
	"tdd-red-green",
	"plan-before-code",
	"debug-scientifically",
	"review-before-done",
	"ship-small",
}

// TestReadWelcomeSkillsTree_FiveSlugsWithValidFrontMatter pins the shape
// every curated welcome skill must have: one SKILL.md per slug, front-matter
// name: equal to the slug, a non-empty description:, source: zerops-curated,
// and a body that isn't a placeholder stub. minBodyNonBlankLines is
// calibrated to the shortest shipped skill (ship-small / review-before-done,
// 8 non-blank body lines each) rather than a round number: it's a
// no-placeholder floor, not a style target, and it must actually hold
// against the reviewed content this test guards.
func TestReadWelcomeSkillsTree_FiveSlugsWithValidFrontMatter(t *testing.T) {
	t.Parallel()
	const minBodyNonBlankLines = 8

	files, err := ReadWelcomeSkillsTree()
	if err != nil {
		t.Fatalf("ReadWelcomeSkillsTree: %v", err)
	}

	bySlug := make(map[string]WelcomeSkillFile, len(files))
	for _, f := range files {
		if f.RelPath != "SKILL.md" {
			t.Errorf("welcome skill %q: unexpected file %q (only SKILL.md expected today)", f.Slug, f.RelPath)
			continue
		}
		if _, dup := bySlug[f.Slug]; dup {
			t.Errorf("duplicate welcome skill slug %q", f.Slug)
		}
		bySlug[f.Slug] = f
	}
	if len(bySlug) != len(wantWelcomeSkillSlugs) {
		t.Fatalf("got %d welcome skills, want %d: %v", len(bySlug), len(wantWelcomeSkillSlugs), keysOf(bySlug))
	}

	for _, slug := range wantWelcomeSkillSlugs {
		f, ok := bySlug[slug]
		if !ok {
			t.Errorf("missing welcome skill %q", slug)
			continue
		}
		fm, body := splitFrontMatter(f.Content)
		if fm == nil {
			t.Errorf("%s: SKILL.md has no --- front-matter block", slug)
			continue
		}
		if fm["name"] != slug {
			t.Errorf("%s: front-matter name = %q, want %q", slug, fm["name"], slug)
		}
		if strings.TrimSpace(fm["description"]) == "" {
			t.Errorf("%s: front-matter description is empty", slug)
		}
		if fm["source"] != "zerops-curated" {
			t.Errorf("%s: front-matter source = %q, want %q", slug, fm["source"], "zerops-curated")
		}

		nonBlank := 0
		for _, line := range strings.Split(body, "\n") {
			if strings.TrimSpace(line) != "" {
				nonBlank++
			}
		}
		if nonBlank < minBodyNonBlankLines {
			t.Errorf("%s: body has %d non-blank lines, want >= %d (no-placeholder guard)", slug, nonBlank, minBodyNonBlankLines)
		}
	}
}

// splitFrontMatter parses a minimal `key: value` YAML front-matter block —
// the fixed, flat shape every welcome-skill SKILL.md carries — and returns
// it alongside the body that follows. Not a general YAML parser; deliberately
// so, since this content's shape is pinned by TestReadWelcomeSkillsTree_
// FiveSlugsWithValidFrontMatter itself, not read from arbitrary input.
func splitFrontMatter(content string) (map[string]string, string) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, content
	}
	fm := map[string]string{}
	i := 1
	for ; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			i++
			break
		}
		key, value, ok := strings.Cut(lines[i], ":")
		if !ok {
			continue
		}
		fm[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return fm, strings.Join(lines[i:], "\n")
}

func keysOf(m map[string]WelcomeSkillFile) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// skillsConstRe captures the body of welcome.js's `const SKILLS = [...]`
// array literal (non-greedy, DOTALL) — TestWelcomeSkillsAllowlistMatchesEmbedded
// parses slugs out of that captured body only, never the whole file, so an
// unrelated `slug:`-shaped string elsewhere in the file can't false-positive
// into the comparison.
var skillsConstRe = regexp.MustCompile(`(?s)const SKILLS = \[(.*?)\n\];`)

// skillSlugRe matches one `slug: "..."` object-literal entry inside the
// SKILLS const body.
var skillSlugRe = regexp.MustCompile(`slug:\s*"([a-z0-9-]+)"`)

// TestWelcomeSkillsAllowlistMatchesEmbedded pins that welcome.js's SKILLS
// allowlist (the webview's ONLY installable slugs, spec §6: "slug must be in
// the shipped allowlist, never a path from the webview") names EXACTLY the
// embedded welcome-skills/ slugs — never an embedded skill with no UI entry
// point, and never a UI entry for content that was never embedded (which
// would 404 the shipped-bytes read at install time). Also pins that "guided"
// — the reserved slug owned by `zcp init --guided` — never appears in the
// JS allowlist.
func TestWelcomeSkillsAllowlistMatchesEmbedded(t *testing.T) {
	t.Parallel()

	files, err := ReadWelcomeSkillsTree()
	if err != nil {
		t.Fatalf("ReadWelcomeSkillsTree: %v", err)
	}
	embedded := make(map[string]bool, len(files))
	for _, f := range files {
		embedded[f.Slug] = true
	}

	js, err := GetTemplate("vscode-bootstrap-welcome.js")
	if err != nil {
		t.Fatalf("GetTemplate(vscode-bootstrap-welcome.js): %v", err)
	}
	m := skillsConstRe.FindStringSubmatch(js)
	if m == nil {
		t.Fatal("vscode-bootstrap-welcome.js: no `const SKILLS = [...]` block found")
	}

	jsSlugSet := map[string]bool{}
	for _, sm := range skillSlugRe.FindAllStringSubmatch(m[1], -1) {
		jsSlugSet[sm[1]] = true
	}
	if len(jsSlugSet) == 0 {
		t.Fatal("no slugs parsed out of the SKILLS const — regex drifted from the JS shape")
	}

	for slug := range embedded {
		if !jsSlugSet[slug] {
			t.Errorf("embedded welcome skill %q has no entry in welcome.js's SKILLS const", slug)
		}
	}
	for slug := range jsSlugSet {
		if !embedded[slug] {
			t.Errorf("welcome.js SKILLS const references %q, which has no embedded welcome-skills/%s/SKILL.md", slug, slug)
		}
	}
	if jsSlugSet["guided"] {
		t.Error(`"guided" must never appear in welcome.js's SKILLS const — it is a reserved slug owned by "zcp init --guided"`)
	}
}
