package workflow

// Goldens infrastructure for atom-corpus-verification (Phase 1 of
// plans/atom-corpus-verification-2026-05-02.md). Test setup lives in
// internal/workflow/scenarios_golden_test.go; this file carries the
// non-test helpers it depends on so they're discoverable separately
// from the test surface.
//
// Goldens are markdown files at internal/workflow/testdata/atom-goldens/
// <scenario-id>.md. Each file has YAML-style frontmatter (scenario id,
// expected atom IDs in render order, human description) followed by the
// rendered atom-body text. The infrastructure is gated by two env vars:
//
//   - ZCP_GOLDEN_COMPARE: when set, the comparison test runs. When
//     unset (default), the test skips with a TODO message — Phase 1
//     leaves goldens UNREVIEWED, Phase 2 blesses and flips this on.
//   - ZCP_UPDATE_ATOM_GOLDENS: when set, the test regenerates each
//     file from the live envelope+corpus instead of comparing. CI
//     guard: panics if CI=true is also set, so a CI run can never
//     overwrite blessed goldens.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// atomGoldensDir is the on-disk location of the golden files relative
// to internal/workflow/. The package's _test.go files run from this
// directory by default (Go test working dir = package dir).
const atomGoldensDir = "testdata/atom-goldens"

// goldenFile is the parsed shape of one golden markdown file. Frontmatter
// fields are validated up front; Body is opaque markdown.
type goldenFile struct {
	ID           string   // scenario id (e.g. "idle/empty"); must match the filename
	AtomIDs      []string // expected atom IDs in render order
	Description  string   // human-readable scenario description
	Body         string   // rendered atom-body text after the closing frontmatter delimiter
	IsUnreviewed bool     // true when the body opens with the `<!-- UNREVIEWED -->` marker
}

// parseGoldenFile splits a golden markdown file into frontmatter + body.
// Frontmatter delimiters are `---\n` (open) and `\n---\n` (close), same
// shape as ParseAtom for atom .md files. UNREVIEWED marker detection
// flips IsUnreviewed when the body's first non-blank line contains the
// HTML comment <!-- UNREVIEWED -->.
func parseGoldenFile(path string, content string) (goldenFile, error) {
	if !strings.HasPrefix(content, "---\n") {
		return goldenFile{}, fmt.Errorf("golden %q missing opening `---\\n` delimiter", path)
	}
	rest := content[4:]
	front, body, ok := strings.Cut(rest, "\n---\n")
	if !ok {
		return goldenFile{}, fmt.Errorf("golden %q missing closing `\\n---\\n` delimiter", path)
	}
	gf := goldenFile{Body: body}
	for line := range strings.SplitSeq(front, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colon := strings.Index(line, ":")
		if colon <= 0 {
			return goldenFile{}, fmt.Errorf("golden %q frontmatter malformed line: %q", path, line)
		}
		key := strings.TrimSpace(line[:colon])
		value := strings.TrimSpace(line[colon+1:])
		value = strings.Trim(value, "\"")
		switch key {
		case "id":
			gf.ID = value
		case "atomIds":
			gf.AtomIDs = parseInlineList(value)
		case "description":
			gf.Description = value
		default:
			return goldenFile{}, fmt.Errorf("golden %q unknown frontmatter key %q (valid: id, atomIds, description)", path, key)
		}
	}
	if gf.ID == "" {
		return goldenFile{}, fmt.Errorf("golden %q missing required frontmatter key: id", path)
	}
	for line := range strings.SplitSeq(gf.Body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		gf.IsUnreviewed = strings.Contains(line, "<!-- UNREVIEWED -->")
		break
	}
	return gf, nil
}

// parseInlineList parses an inline `[a, b, c]` value into a string slice
// (mirrors workflow.parseYAMLList but stays in the test-only helper so
// the production parser keeps its scope narrow). Empty brackets parse
// to nil; missing brackets return nil with no error so misformatted
// values fail explicitly only if upstream callers depend on non-nil.
func parseInlineList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if !strings.HasPrefix(raw, "[") || !strings.HasSuffix(raw, "]") {
		return nil
	}
	raw = raw[1 : len(raw)-1]
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// renderGoldenFile composes the on-disk shape (frontmatter + body) for
// a scenario. ZCP_UPDATE_ATOM_GOLDENS=1 callers write this content to
// disk; comparison callers parse it back via parseGoldenFile.
//
// `unreviewed`=true prepends the `<!-- UNREVIEWED -->` marker so Phase
// 1 raw outputs are visibly distinct from Phase 2 blessed outputs. The
// marker is stripped by Phase 2's bless step (ZCP_UPDATE_ATOM_GOLDENS=1
// re-emits with unreviewed=false on a passing review).
func renderGoldenFile(id, description string, atomIDs []string, body string, unreviewed bool) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("id: ")
	sb.WriteString(id)
	sb.WriteString("\n")
	sb.WriteString("atomIds: [")
	sb.WriteString(strings.Join(atomIDs, ", "))
	sb.WriteString("]\n")
	if description != "" {
		sb.WriteString("description: \"")
		sb.WriteString(description)
		sb.WriteString("\"\n")
	}
	sb.WriteString("---\n")
	if unreviewed {
		sb.WriteString("<!-- UNREVIEWED -->\n\n")
	}
	sb.WriteString(body)
	if !strings.HasSuffix(body, "\n") {
		sb.WriteString("\n")
	}
	return sb.String()
}

// goldenPathFor returns the on-disk path for a scenario id. Slashes in
// the id are filesystem-friendly: `idle/empty` → `idle/empty.md` under
// `testdata/atom-goldens/`.
func goldenPathFor(id string) string {
	return filepath.Join(atomGoldensDir, id+".md")
}

// ensureGoldenDir creates the parent directory for a golden file path
// if it doesn't exist. Used by ZCP_UPDATE_ATOM_GOLDENS=1 regeneration
// since some scenario ids (e.g. `bootstrap/recipe/provision`) imply a
// nested directory structure that may not exist on first generate.
func ensureGoldenDir(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create golden dir %q: %w", dir, err)
	}
	return nil
}

// guardCIRegenerate panics if both ZCP_UPDATE_ATOM_GOLDENS and CI are
// set — a CI workflow that accidentally sets the regenerate flag would
// silently overwrite blessed goldens. The panic is loud and unambiguous;
// CI systems abort on panic, surfacing the misconfiguration.
//
// Set CI=true in any continuous-integration environment (GitHub Actions
// already does this by default); the env var is the standard signal
// the helper checks.
func guardCIRegenerate() {
	if os.Getenv("ZCP_UPDATE_ATOM_GOLDENS") != "" && os.Getenv("CI") != "" {
		panic("ZCP_UPDATE_ATOM_GOLDENS=1 and CI=true cannot coexist — CI must never overwrite blessed goldens. " +
			"If this is a local regenerate run, unset CI; if this is CI, drop the ZCP_UPDATE_ATOM_GOLDENS flag.")
	}
}
