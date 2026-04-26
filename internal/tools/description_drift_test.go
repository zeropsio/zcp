package tools

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestToolDescriptionDriftLint pins Phase 4 (C7) of the pipeline-repair
// plan: MCP tool descriptions and CLAUDE.md template content MUST NOT
// contain wording that contradicts the canonical spec / atom corpus.
//
// Tool descriptions are read at MCP registration time (init), so they
// shape the LLM's prior before any per-turn atom fires. A description
// that says "call this tool after every deploy" while the atom corpus
// + spec say "the deploy handler does this; never call this manually"
// produces an LLM that double-actions or escalates spuriously.
//
// Pre-fix surfaces that drifted:
//   - internal/tools/subdomain.go:28 — described as "call once after
//     first deploy" while spec O3 makes it a deploy-handler concern.
//   - internal/content/templates/claude_shared.md:48 — claimed
//     zerops_subdomain "skips the workflow" and "auto-applies without
//     a deploy cycle" — wrong on both counts.
//
// This test scans both surfaces with tool-description-specific
// forbidden-pattern regexes (NOT the atom_lint regex set — those
// target atom prose, which has different phrasing patterns). Initial
// pattern set is documented inline; extends with each newly observed
// drift, never shrinks.
//
// Implementation: separate AST scanner over internal/tools/*.go for
// the Go path (extracts mcp.Tool literal Description and jsonschema
// tag descriptions) plus a flat markdown read of
// internal/content/templates/claude_*.md.
func TestToolDescriptionDriftLint(t *testing.T) {
	t.Parallel()

	patterns := descriptionDriftPatterns()

	var violations []string

	// Input A — Go AST scan over internal/tools/*.go for mcp.AddTool
	// Description fields and jsonschema tag descriptions.
	goSources, err := filepath.Glob("../tools/*.go")
	if err != nil {
		t.Fatalf("glob tools/: %v", err)
	}
	for _, path := range goSources {
		base := filepath.Base(path)
		if strings.HasSuffix(base, "_test.go") {
			continue
		}
		// Self-skip: this file documents the forbidden patterns and
		// would otherwise self-flag.
		if base == "description_drift_test.go" {
			continue
		}
		violations = append(violations, scanGoForDriftPatterns(t, path, patterns)...)
	}

	// Input B — markdown read of internal/content/templates/claude_*.md.
	mdSources, err := filepath.Glob("../content/templates/claude_*.md")
	if err != nil {
		t.Fatalf("glob templates/: %v", err)
	}
	for _, path := range mdSources {
		violations = append(violations, scanMarkdownForDriftPatterns(t, path, patterns)...)
	}

	if len(violations) > 0 {
		t.Errorf("found %d drift-pattern hit(s) in tool descriptions / templates:\n  %s\n\nFix: rewrite to align with spec O3 and the atom corpus. The drift-lint pattern set documents what wording is forbidden and why. If the new wording is legitimately different from the forbidden pattern, the regex needs adjustment — open a PR with the rationale.",
			len(violations), strings.Join(violations, "\n  "))
	}
}

// driftPattern pairs a regex with the canonical fact it contradicts.
// Failure messages cite the fact so the author sees what the
// description must align with, not just "this regex tripped".
type driftPattern struct {
	id    string
	regex *regexp.Regexp
	fact  string
}

// descriptionDriftPatterns returns the closed set of forbidden patterns
// (as of this commit). Initial set targets two observed drift classes:
// subdomain auto-enable wording and "skips the workflow" claims.
//
// Adding a new pattern requires citing the spec / atom that the
// forbidden wording contradicts. Removing patterns requires evidence
// the forbidden wording is no longer wrong.
func descriptionDriftPatterns() []driftPattern {
	return []driftPattern{
		{
			id:    "subdomain-call-after-deploy",
			regex: regexp.MustCompile(`(?i)\b(call|need)\b[^\n]{0,80}\benable\b[^\n]{0,40}\b(after|first)\b[^\n]{0,20}\bdeploy\b`),
			fact:  "spec-workflows.md O3: zerops_deploy auto-enables the L7 subdomain on first deploy for eligible modes; zerops_subdomain is recovery / production opt-in / disable only.",
		},
		{
			id:    "skips-the-workflow",
			regex: regexp.MustCompile(`(?i)\bskip(s|ping)? the workflow\b`),
			fact:  "spec-workflows.md §1.1 Principles: workflow is not a gate — read-only / one-shot tools simply may be called outside an active workflow; they do not 'skip' anything.",
		},
		{
			id:    "subdomain-auto-apply",
			regex: regexp.MustCompile(`(?i)zerops_subdomain[^\n]{0,80}\bauto[- ]?appl(y|ies)\b`),
			fact:  "spec-workflows.md O3: zerops_subdomain is the recovery/opt-in/disable tool; the auto-enable is performed by zerops_deploy on first deploy, not by zerops_subdomain.",
		},
	}
}

// scanGoForDriftPatterns parses one .go file under internal/tools/ and
// returns positions of forbidden-pattern hits in mcp.Tool Description
// fields and jsonschema struct-tag descriptions. Other string literals
// (error messages, etc.) are out of scope — those have their own lint
// path (atom corpus authoring contract).
func scanGoForDriftPatterns(t *testing.T, path string, patterns []driftPattern) []string {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution|parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	var hits []string
	ast.Inspect(file, func(n ast.Node) bool {
		// Composite literal field assignments — `Description: "..."`
		// inside mcp.Tool, jsonschema.Schema, etc.
		kv, ok := n.(*ast.KeyValueExpr)
		if ok {
			if isDescriptionKey(kv.Key) {
				if str, ok := stringLitValue(kv.Value); ok {
					hits = append(hits, checkPatterns(fset, kv.Pos(), str, patterns)...)
				}
			}
		}
		// Struct tags carrying jsonschema:"...description...".
		field, ok := n.(*ast.Field)
		if ok && field.Tag != nil {
			tag := field.Tag.Value
			if desc := extractJSONSchemaDescription(tag); desc != "" {
				hits = append(hits, checkPatterns(fset, field.Tag.Pos(), desc, patterns)...)
			}
		}
		return true
	})
	return hits
}

// scanMarkdownForDriftPatterns runs the pattern set against a markdown
// file's full content. Used for CLAUDE.md template files where the
// content is plain prose rather than embedded in Go literals.
func scanMarkdownForDriftPatterns(t *testing.T, path string, patterns []driftPattern) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	content := string(data)
	var hits []string
	for _, p := range patterns {
		if p.regex.MatchString(content) {
			hits = append(hits, formatHit(path, 0, p))
		}
	}
	return hits
}

func checkPatterns(fset *token.FileSet, pos token.Pos, body string, patterns []driftPattern) []string {
	var hits []string
	for _, p := range patterns {
		if p.regex.MatchString(body) {
			line := fset.Position(pos).Line
			hits = append(hits, formatHit(fset.Position(pos).Filename, line, p))
		}
	}
	return hits
}

func formatHit(path string, line int, p driftPattern) string {
	if line > 0 {
		return path + ":" + itoa(line) + " — pattern " + p.id + ": " + p.fact
	}
	return path + " — pattern " + p.id + ": " + p.fact
}

func itoa(n int) string {
	// Local helper so this test file doesn't pull in strconv just for
	// one int format — keeps the file self-contained.
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func isDescriptionKey(node ast.Expr) bool {
	ident, ok := node.(*ast.Ident)
	return ok && ident.Name == "Description"
}

func stringLitValue(node ast.Expr) (string, bool) {
	lit, ok := node.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	// Strip surrounding quotes; handle backtick-quoted strings too.
	v := lit.Value
	if len(v) >= 2 {
		if v[0] == '"' && v[len(v)-1] == '"' {
			return v[1 : len(v)-1], true
		}
		if v[0] == '`' && v[len(v)-1] == '`' {
			return v[1 : len(v)-1], true
		}
	}
	return v, true
}

// extractJSONSchemaDescription pulls the description value out of a
// `jsonschema:"..."` struct tag. Returns "" when the tag has no
// jsonschema component or no description portion. The grammar is
// loose (jsonschema-go uses comma-separated fragments where the
// description is the bare-string head); this helper uses a tolerant
// parse that handles the common shape.
func extractJSONSchemaDescription(tag string) string {
	// Strip backticks: `jsonschema:"..." json:"..."`
	tag = strings.Trim(tag, "`")
	_, rest, ok := strings.Cut(tag, `jsonschema:"`)
	if !ok {
		return ""
	}
	desc, _, ok := strings.Cut(rest, `"`)
	if !ok {
		return ""
	}
	return desc
}

// TestToolDescriptionDriftPatternsCatchKnownDrifts is the regex
// self-test: each forbidden pattern MUST match the historical drift
// text it was added to catch. Without this, a typo in the regex could
// neutralise the pattern and the production-corpus scan would silently
// pass — same failure mode the atom_lint fires-on-fixture test guards
// against (test-suite-fixes-2026-04-25.md §1.1).
//
// Pre-Phase 3 drift texts (verbatim from the commit history):
//   - subdomain.go:28: "...New services need one enable call after
//     first deploy to activate the L7 route..."
//   - claude_shared.md:48: "Direct tools skip the workflow — ...
//     auto-apply without a deploy cycle."
//
// If a pattern is removed, this test should remove the corresponding
// fixture too. If a pattern is added, add a fixture exemplifying the
// drift it catches.
func TestToolDescriptionDriftPatternsCatchKnownDrifts(t *testing.T) {
	t.Parallel()

	patterns := descriptionDriftPatterns()
	byID := make(map[string]driftPattern, len(patterns))
	for _, p := range patterns {
		byID[p.id] = p
	}

	cases := []struct {
		patternID string
		fixture   string
	}{
		{
			patternID: "subdomain-call-after-deploy",
			fixture:   "Enable or disable zerops.app subdomain. Idempotent. New services need one enable call after first deploy to activate the L7 route.",
		},
		{
			patternID: "skips-the-workflow",
			fixture:   "Direct tools skip the workflow — zerops_discover, zerops_logs ... auto-apply without a deploy cycle.",
		},
		{
			patternID: "subdomain-auto-apply",
			fixture:   "zerops_subdomain auto-applies without a deploy cycle.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.patternID, func(t *testing.T) {
			t.Parallel()
			p, ok := byID[tc.patternID]
			if !ok {
				t.Fatalf("pattern %q not in current set", tc.patternID)
			}
			if !p.regex.MatchString(tc.fixture) {
				t.Errorf("pattern %q failed to match its known-drift fixture; regex %q vs fixture %q",
					tc.patternID, p.regex.String(), tc.fixture)
			}
		})
	}
}
