package recipe

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// bareZeropsURIInStringRe matches a backtick immediately followed by a
// `zerops://` URI — the resource-reader bait — inside a DECODED Go
// string literal. Mirrors bareZeropsURIRe in
// internal/content/agent_facing_uri_lint_test.go, which scans committed
// markdown for the same pattern.
var bareZeropsURIInStringRe = regexp.MustCompile("`zerops://")

// TestNoBareZeropsURIInGoStringLiterals closes the guard-scope gap
// TestNoBareZeropsURIInAgentContent leaves open: that test walks
// git-tracked .md files under five agent-facing directories, so a bare
// `zerops://` URI baked into a Go string literal — e.g. a brief
// composer's return value, which an agent reads as spliced-in brief
// text just as surely as a loaded atom .md — is invisible to it
// (regression: BuildDesignTokenTable in briefs_design_tokens.go
// returned "...`zerops://themes/design-system`..." undetected). This
// test AST-parses every non-test .go file directly under
// internal/authoring/recipe/ and fails on any string literal whose
// DECODED value contains a backtick immediately followed by
// `zerops://`. Doc comments are exempt by construction: ast.Inspect
// walks declarations, not the comment map, so a comment mentioning
// `zerops://themes/...` (there are several, describing where the full
// theme spec lives) never reaches this scanner.
func TestNoBareZeropsURIInGoStringLiterals(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir .: %v", err)
	}
	var violations []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if pos, ok := literalHasBareZeropsURI(n); ok {
				violations = append(violations, fset.Position(pos).String())
			}
			return true
		})
	}

	if len(violations) > 0 {
		t.Errorf("bare backticked `zerops://` inside a Go string literal — an agent reads a composer's RETURNED text, not its source, so this is agent-facing content the markdown-only guard (TestNoBareZeropsURIInAgentContent) can't see; convert to the tool-call form `zerops_knowledge uri=\"zerops://...\"`:\n  %s",
			strings.Join(violations, "\n  "))
	}
}

// TestBareZeropsURIScanner_FiresOnFixture proves the scanner both fires
// on the bait pattern AND stays quiet on the converged tool-call form,
// so a green TestNoBareZeropsURIInGoStringLiterals means "no bare
// URIs", not "scanner blind" (and confirms it wouldn't have flagged the
// fix as a false positive).
func TestBareZeropsURIScanner_FiresOnFixture(t *testing.T) {
	t.Parallel()

	q := `"`
	src := "package x\n" +
		"func bad() string {\n" +
		"\treturn " + q + "see `zerops://themes/design-system` for details" + q + "\n" +
		"}\n" +
		"func good() string {\n" +
		"\treturn " + q + "see `zerops_knowledge uri=zerops://x` for details" + q + "\n" +
		"}\n"

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	count := 0
	ast.Inspect(f, func(n ast.Node) bool {
		if _, ok := literalHasBareZeropsURI(n); ok {
			count++
		}
		return true
	})
	if count != 1 {
		t.Errorf("scanner must flag exactly the bare-URI literal in bad() and stay quiet on good()'s tool-call-shaped literal; got %d, want 1", count)
	}
}

// literalHasBareZeropsURI reports whether n is a string BasicLit whose
// decoded value contains a backtick immediately followed by
// `zerops://`. Raw (backtick-delimited) string literals can never
// contain a backtick byte, so Unquote failure on that account is not a
// violation — it's skipped.
func literalHasBareZeropsURI(n ast.Node) (token.Pos, bool) {
	lit, ok := n.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return token.NoPos, false
	}
	decoded, err := strconv.Unquote(lit.Value)
	if err != nil {
		return token.NoPos, false
	}
	if bareZeropsURIInStringRe.MatchString(decoded) {
		return lit.Pos(), true
	}
	return token.NoPos, false
}
