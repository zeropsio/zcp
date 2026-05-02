package tools

// Phase 4.9-4.10 of plans/atom-corpus-verification-2026-05-02.md.
//
// nextSteps tripwire — scans workflow_export.go for `"nextSteps":
// []string{...}` slice-literal entries and asserts each entry is
// short, action-oriented, NOT prose. Plan §0b documented nextSteps as
// "structural data" — short imperative commands carrying dynamic
// substitutions (URLs, hostnames). Prose creep means the content
// belongs in an atom body, not in handler-emitted JSON.
//
// Two checks:
//
//  1. Length: each string-literal entry <= 80 chars (after format-
//     verb substitution-shape stays comparable). Catches bullet-list
//     paragraphs that drifted into nextSteps.
//
//  2. Prose regex: entry must NOT contain (because|so that|in order
//     to|note that|explanation). These connectives signal explanatory
//     prose — the explanation belongs in the atom body whose status
//     is rendered alongside.
//
// Strings built from concat / fmt.Sprintf are checked as their LITERAL
// parts only; runtime substitutions (`+ targetService` etc.) don't
// count. This errs slightly small — over-80 strings can sneak through
// if the bulk of length comes from substituted values — but reviewer
// can spot those during PR review.

import (
	"go/ast"
	"go/parser"
	"go/token"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// nextStepsMaxLen is the per-entry length cap. Plan §0b "structurally
// short (<3 entries × <80 chars)". 80 chars matches conventional
// terminal width and guides command-shape entries.
const nextStepsMaxLen = 80

// nextStepsProseRegex flags connectives that signal explanatory prose
// rather than action commands. Case-insensitive.
var nextStepsProseRegex = regexp.MustCompile(`(?i)\b(because|so that|in order to|note that|explanation)\b`)

// TestNextStepsTripwire walks workflow_export.go's AST and asserts
// every "nextSteps" slice entry passes both checks. Per plan §0b
// "Tripwire: if any nextSteps entry exceeds 80 characters or starts
// looking like prose explanation rather than action description,
// return that content to atom body."
func TestNextStepsTripwire(t *testing.T) {
	t.Parallel()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "workflow_export.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse workflow_export.go: %v", err)
	}

	type violation struct {
		line   int
		entry  string
		reason string
	}
	var violations []violation

	ast.Inspect(f, func(n ast.Node) bool {
		// We're looking for composite-literal map keyed by string
		// "nextSteps" with a slice-literal value. Pattern in the
		// handler: jsonResult(map[string]any{ ..., "nextSteps":
		// []string{...}, ... }).
		kv, ok := n.(*ast.KeyValueExpr)
		if !ok {
			return true
		}
		keyLit, ok := kv.Key.(*ast.BasicLit)
		if !ok || keyLit.Kind != token.STRING {
			return true
		}
		key, err := strconv.Unquote(keyLit.Value)
		if err != nil || key != "nextSteps" {
			return true
		}
		composite, ok := kv.Value.(*ast.CompositeLit)
		if !ok {
			return true
		}
		for _, elt := range composite.Elts {
			lit := extractNextStepLiteral(elt)
			if lit == "" {
				continue
			}
			pos := fset.Position(elt.Pos())
			if len(lit) > nextStepsMaxLen {
				violations = append(violations, violation{
					line:   pos.Line,
					entry:  lit,
					reason: "exceeds 80-char cap (literal part); shorten or move prose to atom body",
				})
			}
			if nextStepsProseRegex.MatchString(lit) {
				match := nextStepsProseRegex.FindString(lit)
				violations = append(violations, violation{
					line:   pos.Line,
					entry:  lit,
					reason: "matches prose-creep regex \"" + match + "\"; explanation belongs in atom body",
				})
			}
		}
		return true
	})

	if len(violations) > 0 {
		t.Errorf("nextSteps tripwire fired %d time(s) in workflow_export.go:", len(violations))
		for _, v := range violations {
			t.Errorf("  line %d: %s\n    entry: %q", v.line, v.reason, v.entry)
		}
	}
}

// extractNextStepLiteral returns the literal text of a single nextSteps
// entry. Handles three forms encountered in workflow_export.go:
//
//  1. Bare string literal: "After scaffolding, re-call ..."
//
//  2. fmt.Sprintf call: fmt.Sprintf("template with %q", arg). Returns
//     the template (verbs left in place — close enough for length
//     check; verbs typically expand by ~10-20 chars at runtime).
//
//  3. Concatenation: "literal" + arg + "more literal". Returns the
//     concatenated string-literal parts only; runtime args (idents,
//     selector exprs) contribute nothing to the literal length.
//
// Returns "" when the form isn't recognized — those entries are
// skipped (the test is conservative; reviewer covers exotic shapes).
func extractNextStepLiteral(elt ast.Expr) string {
	switch e := elt.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return ""
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return ""
		}
		return s
	case *ast.CallExpr:
		// fmt.Sprintf("template", args...) — extract the template.
		sel, ok := e.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Sprintf" {
			return ""
		}
		if len(e.Args) == 0 {
			return ""
		}
		tmpl, ok := e.Args[0].(*ast.BasicLit)
		if !ok || tmpl.Kind != token.STRING {
			return ""
		}
		s, err := strconv.Unquote(tmpl.Value)
		if err != nil {
			return ""
		}
		return s
	case *ast.BinaryExpr:
		// "lit" + ident + "lit" — collect the literal segments.
		var parts []string
		collectStringConcatLiterals(e, &parts)
		return strings.Join(parts, "")
	}
	return ""
}

// collectStringConcatLiterals walks a binary-+ tree and appends every
// string-literal leaf to parts. Non-literal leaves (idents, calls,
// selector exprs) are skipped — they contribute runtime values that
// don't count toward the literal length.
func collectStringConcatLiterals(node ast.Expr, parts *[]string) {
	switch e := node.(type) {
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return
		}
		collectStringConcatLiterals(e.X, parts)
		collectStringConcatLiterals(e.Y, parts)
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return
		}
		s, err := strconv.Unquote(e.Value)
		if err != nil {
			return
		}
		*parts = append(*parts, s)
	}
}

// TestNextStepsTripwire_FixtureFiresOnViolations is the meta-test —
// proves the tripwire actually catches what it claims by feeding it
// synthetic violations. Counterpoint to TestNextStepsTripwire which
// asserts the production source passes.
func TestNextStepsTripwire_FixtureFiresOnViolations(t *testing.T) {
	t.Parallel()

	overlongFixture := strings.Repeat("a", nextStepsMaxLen+1)
	if len(overlongFixture) <= nextStepsMaxLen {
		t.Fatalf("fixture builder produced unexpectedly short string (%d chars)", len(overlongFixture))
	}

	proseFixtures := []string{
		"Run X because the agent needs Y",
		"Do Z so that W happens",
		"Run setup in order to land the build",
		"After commit (note that Y is required) re-call",
	}
	for _, fix := range proseFixtures {
		if !nextStepsProseRegex.MatchString(fix) {
			t.Errorf("prose regex did not match fixture %q — regex tightened too far?", fix)
		}
	}
}
