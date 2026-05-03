package recipe

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

// run-22 followup F-2.5 — system.md §4 verdict-table-vs-code lint.
//
// system.md §4 has 50+ verdict-table entries hand-curated; each cites
// `Pinned by TestX` — the TEACH-side claim that a named test prevents
// the named drift class from recurring. There's currently no test that
// the cited test names actually exist. When code drifts and a test is
// renamed, the verdict table rots silently. This lint is the meta-drift
// closure: drift between the intent-document that defines the
// catalog-drift line and the code state it's curating.
//
// Implementation: walk every `Test\w+` token inside the §4 verdict
// table block, intersect with the declared test functions across
// every `internal/.../*_test.go` package, and fail on any cited name
// that has no declaration. Glob suffixes (`Test...*_BriefTeachingHandlesIt`)
// match prefix-then-arbitrary-then-suffix; bare `TestPrefix*`
// fingerprints match prefix-only.
//
// First-run policy per spec: surfaced drift goes into the verdict-table
// or test name, not into a skip list. Reconciliations land in the same
// commit as this lint.
func TestSystemMD_VerdictTableTestNames_Resolve(t *testing.T) {
	t.Parallel()

	const systemMD = "../../docs/zcprecipator3/system.md"
	raw, err := os.ReadFile(systemMD)
	if err != nil {
		t.Fatalf("read %s: %v", systemMD, err)
	}

	tableBody := extractVerdictTableBody(string(raw))
	if tableBody == "" {
		t.Fatalf("verdict-table region not found in %s — looked for `### The test applied — current artifacts` heading", systemMD)
	}

	cited := extractVerdictTableTestTokens(tableBody)
	if len(cited) == 0 {
		t.Fatalf("verdict-table region carries no `Test\\w+` tokens — extract regex likely drifted")
	}

	declared, err := collectDeclaredTestNames("../../internal")
	if err != nil {
		t.Fatalf("collect declared test names: %v", err)
	}
	if len(declared) < 100 {
		// Sanity — the repo carries many hundreds of tests. A near-zero
		// set means the parser walk or root path drifted.
		t.Fatalf("collected only %d declared test names from internal/...; walker likely misconfigured", len(declared))
	}

	for _, c := range cited {
		if !c.matchesAny(declared) {
			t.Errorf("system.md §4 verdict table cites %q — no matching test declaration found under internal/...; rename the verdict-table entry to the current test name OR rename the test (the test is authoritative for behavior; the verdict-table entry is documentation)",
				c.literal)
		}
	}
}

// citedTestName is one `Test\w+` token (or `Test\w+*` glob) extracted
// from the verdict-table prose. `prefix` is the token without a
// trailing `*`; `suffix` is the trailing fragment after a middle `*`.
type citedTestName struct {
	literal string // verbatim verdict-table token (with `*` if present)
	prefix  string
	suffix  string // empty unless the verdict-table form was Prefix*Suffix
	exact   bool   // true when no `*` glob was present
}

// matchesAny reports whether the cited name resolves to at least one
// declared test name in the corpus. Exact tokens match by equality;
// `Prefix*Suffix` matches HasPrefix(decl, Prefix) && HasSuffix(decl, Suffix).
func (c citedTestName) matchesAny(declared map[string]struct{}) bool {
	if c.exact {
		_, ok := declared[c.literal]
		return ok
	}
	// Glob form: walk the set looking for at least one match.
	for d := range declared {
		if strings.HasPrefix(d, c.prefix) && strings.HasSuffix(d, c.suffix) && len(d) >= len(c.prefix)+len(c.suffix) {
			return true
		}
	}
	return false
}

// extractVerdictTableBody returns the body of the §4 "current
// artifacts" verdict table — the markdown table block between the
// `### The test applied — current artifacts` heading and the next
// section heading.
func extractVerdictTableBody(doc string) string {
	const startHeading = "### The test applied"
	startIdx := strings.Index(doc, startHeading)
	if startIdx < 0 {
		return ""
	}
	// Cut everything before the heading so the next "###" hit is the
	// section closer.
	tail := doc[startIdx:]
	// Skip past the heading line so its own "###" doesn't terminate us.
	if i := strings.Index(tail, "\n"); i >= 0 {
		tail = tail[i+1:]
	}
	if endIdx := strings.Index(tail, "\n### "); endIdx >= 0 {
		tail = tail[:endIdx]
	}
	return tail
}

// extractVerdictTableTestTokens pulls every `Test\w+` token from the
// verdict-table body, including glob forms (`TestPrefix*` and
// `TestPrefix*Suffix`). Deduplicates exact matches.
func extractVerdictTableTestTokens(body string) []citedTestName {
	// Strict token shape: `Test` + identifier chars; optional middle
	// `*`; optional trailing identifier chars; optional trailing `*`.
	re := regexp.MustCompile(`\bTest[A-Za-z0-9_]+(?:\*[A-Za-z0-9_]*)?\*?`)
	seen := map[string]struct{}{}
	out := []citedTestName{}
	for _, raw := range re.FindAllString(body, -1) {
		// Strip backticks Markdown sometimes wraps tokens in.
		token := strings.Trim(raw, "`")
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, parseCitedTestName(token))
	}
	return out
}

func parseCitedTestName(tok string) citedTestName {
	if !strings.Contains(tok, "*") {
		return citedTestName{literal: tok, prefix: tok, exact: true}
	}
	// Glob form. Cases: `Prefix*` and `Prefix*Suffix`.
	parts := strings.SplitN(tok, "*", 2)
	prefix := parts[0]
	suffix := ""
	if len(parts) == 2 {
		// Strip trailing `*` if present (`Prefix*Suffix*` collapses to
		// `Prefix*Suffix`).
		suffix = strings.TrimSuffix(parts[1], "*")
	}
	return citedTestName{literal: tok, prefix: prefix, suffix: suffix}
}

// collectDeclaredTestNames walks every `*_test.go` file under root and
// returns the set of `func Test...(t *testing.T)` declarations.
func collectDeclaredTestNames(root string) (map[string]struct{}, error) {
	out := map[string]struct{}{}
	walkErr := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip vendored / generated dirs — none currently exist
			// inside `internal/` but be defensive.
			if base := filepath.Base(p); base == "testdata" || strings.HasPrefix(base, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		f, perr := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if perr != nil {
			// Repo `_test.go` files always parse; surface the failure
			// so silent walker drift doesn't shrink the declared set.
			return perr
		}
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Recv != nil {
				continue
			}
			name := fd.Name.Name
			if !strings.HasPrefix(name, "Test") {
				continue
			}
			// Filter to standard `func TestX(t *testing.T)` / `(t *testing.B)`
			// signatures so helper symbols don't leak in.
			if fd.Type.Params == nil || len(fd.Type.Params.List) != 1 {
				continue
			}
			out[name] = struct{}{}
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	return out, nil
}
