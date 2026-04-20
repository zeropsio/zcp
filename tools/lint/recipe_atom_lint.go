// Package main is the atom-tree build-time lint. Enforces the P2 / P6
// / P8 invariants declared in docs/zcprecipator2/03-architecture/
// principles.md + calibration-bars-v35.md §9 on the content under
// internal/content/workflows/recipe/. Runs as part of `make lint-local`.
//
// Exit code: 0 if every rule passes; 1 if any rule fires. On failure,
// stdout enumerates the offending file + line + rule-id so a maintainer
// can locate the offender immediately.
package main

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	atomRoot = "internal/content/workflows/recipe"
	// scope identifiers are relative to the root being scanned — the
	// walk resolves file paths to `<scope>/<name>.md` via filepath.Rel
	// before applying per-rule scope filters. This keeps the rules
	// portable across test fixtures (the -root flag points elsewhere)
	// without hardcoding the production atom-root path.
	briefsScope  = "briefs"
	phasesScope  = "phases"
	maxAtomLines = 300 // B-5
	orphanWindow = 10  // B-7 positive-form window (lines surrounding the prohibition)
)

// rule represents one lint rule. ID names the calibration-bar entry
// (B-1..B-7 + H-4); scope filters on the root-relative path prefix
// ("briefs" or "phases", or "" for tree-wide); check receives the
// file path + line number + line body + prior/later context and
// emits a violation string or "" when the rule passes.
type rule struct {
	id    string
	scope string // "" = tree-wide; "briefs" or "phases" = root-relative prefix
	check func(path string, lineNum int, line string, ctx lineContext) string
}

// lineContext carries the surrounding lines of the current line so rules
// like B-7 can look at siblings without re-reading the file.
type lineContext struct {
	before []string
	after  []string
}

// B-1: no version anchors in the atom tree. Tokens like `v34`, `v8.96`,
// `v8.104.3` are dispatcher-side history that must not leak into sub-
// agent content. Regex is anchored on word boundary + `v` + digit.
var b1VersionAnchor = regexp.MustCompile(`\bv[0-9]+(\.[0-9]+)*\b`)

// B-2: no dispatcher vocabulary inside briefs/. These tokens describe
// the server's composition surface and must not appear in transmitted
// prose — they teach the sub-agent about its caller, which is a leakage
// vector.
var b2DispatcherVocab = []string{
	"compress",
	"verbatim",
	"include as-is",
	"main agent",
	"dispatcher",
}

// B-3: no internal check names inside briefs/. Check names are server-
// side gate identifiers; naming them in briefs teaches the sub-agent
// to game the specific token rather than satisfy the invariant.
var b3InternalCheckNames = regexp.MustCompile(`writer_manifest_|_env_self_shadow|_content_reality|_causal_anchor`)

// B-4: no Go source paths inside briefs/. Paths like
// `internal/tools/workflow_checks_content_manifest.go` are server
// implementation details.
var b4GoSourcePath = regexp.MustCompile(`internal/[^ \t\n\x60]*\.go`)

// B-7 prohibition markers that require a positive-form sibling. See
// principles.md §P8 — prohibition without a positive alternative leaves
// the sub-agent without an action.
var b7Prohibitions = []string{
	"do not",
	"avoid",
	"never",
	"MUST NOT",
}

// b7PositiveForms are tokens that signal the atom is declaring the
// positive alternative in the same neighborhood. Any of these in the
// ±orphanWindow surrounding lines satisfies the rule. The list is a
// heuristic — catching the 90% case without requiring the atoms to
// conform to a rigid schema. Markdown list markers (`- `, `* `, `1. `)
// are treated as positive-form signals too because atoms use lists
// heavily for positive enumeration.
var b7PositiveForms = []string{
	// Explicit positive framing
	"instead",
	"use ",
	"uses ",
	"using ",
	"pass ",
	"passes ",
	"set ",
	"sets ",
	"bind ",
	"binds ",
	"binding ",
	"name ",
	"names ",
	"naming ",
	"write ",
	"writes ",
	"writing ",
	"declare ",
	"declares ",
	"include ",
	"includes ",
	"including ",
	"apply ",
	"applies ",
	"applying ",
	"invoke ",
	"invokes ",
	"call ",
	"calls ",
	"calling ",
	"keep ",
	"keeps ",
	"keeping ",
	"match ",
	"matches ",
	"matching ",
	"ensure ",
	"ensures ",
	"ensuring ",
	"return ",
	"returns ",
	"returning ",
	"relay ",
	"relays ",
	"triggers ",
	"trigger ",
	"escalat", // escalate / escalating / escalation
	"split ",
	"splits ",
	"splitting ",
	"keyed to",
	// Imperative-mood starters (sentence-initial, bullet-leading)
	"every ",
	"each ",
	"always ",
	"the correct ",
	"the positive ",
	// Shape declaration
	"allow-list",
	"allow list",
	// Value-naming (positive WHY alongside a prohibition)
	"is worth",
	"is the rule",
	"is a ",
	"is the ",
	"is how ",
	"is what ",
	// Markdown list markers — atoms use bullets + numbered lists
	// heavily for positive enumeration.
	"\n- ",
	"\n* ",
	"\n1. ",
	"\n2. ",
	"\n3. ",
	// Imperative commands wrapped in backticks (atoms routinely show
	// "`zcp sync recipe export {dir}`" as the positive counterpart to
	// a prohibition about premature export).
	"`zcp ",
	"```",
}

// H-4: step-entry atoms must use positive-P4 form. Forbidden phrasing
// "your tasks for this phase are" — frames the agent as executing a
// dispatcher's plan instead of naming the current-state invariant.
var h4BannedPhrase = regexp.MustCompile(`(?i)your tasks for this phase are`)

// rules is the enumerable rule set. Order matters only for deterministic
// output ordering — every rule runs independently against every file.
var rules = []rule{
	{
		id:    "B-1",
		scope: "",
		check: func(path string, lineNum int, line string, _ lineContext) string {
			if m := b1VersionAnchor.FindString(line); m != "" {
				return fmt.Sprintf("B-1 version anchor %q must not appear in atom content — version numbers are dispatcher-side history", m)
			}
			return ""
		},
	},
	{
		id:    "B-2",
		scope: briefsScope,
		check: func(path string, lineNum int, line string, _ lineContext) string {
			lower := strings.ToLower(line)
			for _, tok := range b2DispatcherVocab {
				if strings.Contains(lower, tok) {
					return fmt.Sprintf("B-2 dispatcher vocabulary %q must not appear in transmitted briefs — it leaks the composition surface to the sub-agent", tok)
				}
			}
			return ""
		},
	},
	{
		id:    "B-3",
		scope: briefsScope,
		check: func(path string, lineNum int, line string, _ lineContext) string {
			if m := b3InternalCheckNames.FindString(line); m != "" {
				return fmt.Sprintf("B-3 internal check name %q must not appear in transmitted briefs — names the server gate identifier", m)
			}
			return ""
		},
	},
	{
		id:    "B-4",
		scope: briefsScope,
		check: func(path string, lineNum int, line string, _ lineContext) string {
			if m := b4GoSourcePath.FindString(line); m != "" {
				return fmt.Sprintf("B-4 Go source path %q must not appear in transmitted briefs — server implementation leak", m)
			}
			return ""
		},
	},
	{
		id:    "B-7",
		scope: "",
		check: func(path string, lineNum int, line string, ctx lineContext) string {
			lower := strings.ToLower(line)
			hit := ""
			for _, p := range b7Prohibitions {
				if strings.Contains(lower, strings.ToLower(p)) {
					hit = p
					break
				}
			}
			if hit == "" {
				return ""
			}
			// The line contains a prohibition marker. Look for a positive-
			// form token in the ±orphanWindow surrounding lines (including
			// this line itself, which may blend both forms).
			neighborhood := append([]string{}, ctx.before...)
			neighborhood = append(neighborhood, line)
			neighborhood = append(neighborhood, ctx.after...)
			neighborhoodLower := strings.ToLower(strings.Join(neighborhood, "\n"))
			for _, pos := range b7PositiveForms {
				if strings.Contains(neighborhoodLower, strings.ToLower(pos)) {
					return ""
				}
			}
			return fmt.Sprintf("B-7 prohibition %q lacks a positive-form sibling in the ±%d surrounding lines — per P8 every forbidden-verb atom must pair with an explicit positive allow-list", hit, orphanWindow)
		},
	},
	{
		id:    "H-4",
		scope: phasesScope,
		check: func(path string, lineNum int, line string, _ lineContext) string {
			// Scope further to `entry.md` files.
			if !strings.HasSuffix(path, "/entry.md") {
				return ""
			}
			if h4BannedPhrase.MatchString(line) {
				return "H-4 entry atom uses forbidden phrase \"your tasks for this phase are\" — use positive P4 form (name the current-state invariant, not the agent's task list)"
			}
			return ""
		},
	},
}

// runLint walks atomRoot and returns the list of violations as
// formatted strings. Also returns the files-walked count for a summary
// line at the end of main.
func runLint(root string) (violations []string, filesScanned int, err error) {
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		filesScanned++
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			violations = append(violations, fmt.Sprintf("%s: read error: %v", path, readErr))
			return nil
		}
		// relPath is the path relative to the scan root — "briefs/..."
		// or "phases/..." or "principles/...". Used for rule-scope
		// filtering so tests can point the linter at a fixture tree
		// whose absolute path differs from the production atomRoot.
		relPath, relErr := filepath.Rel(root, path)
		if relErr != nil {
			relPath = path
		}
		lines := strings.Split(string(data), "\n")
		// B-5 file-size cap (computed here because it's per-file, not per-line).
		if len(lines) > maxAtomLines {
			violations = append(violations, fmt.Sprintf("%s: B-5 file has %d lines (cap %d) — split the atom into smaller cohesive leaves", path, len(lines), maxAtomLines))
		}
		// Per-line rules.
		for i, line := range lines {
			ctx := lineContext{
				before: windowBefore(lines, i, orphanWindow),
				after:  windowAfter(lines, i, orphanWindow),
			}
			for _, r := range rules {
				if r.scope != "" && !strings.HasPrefix(relPath, r.scope+string(filepath.Separator)) && relPath != r.scope {
					continue
				}
				if msg := r.check(path, i+1, line, ctx); msg != "" {
					violations = append(violations, fmt.Sprintf("%s:%d: [%s] %s", path, i+1, r.id, msg))
				}
			}
		}
		return nil
	})
	return violations, filesScanned, walkErr
}

// windowBefore returns up to `n` lines preceding `idx` (exclusive).
func windowBefore(lines []string, idx, n int) []string {
	start := max(idx-n, 0)
	return lines[start:idx]
}

// windowAfter returns up to `n` lines following `idx` (exclusive).
func windowAfter(lines []string, idx, n int) []string {
	end := min(idx+1+n, len(lines))
	if idx+1 >= len(lines) {
		return nil
	}
	return lines[idx+1 : end]
}

// main runs the lint. Usage: `recipe_atom_lint [atom-root]`. Default
// atom-root is `internal/content/workflows/recipe`. Exit 1 on any
// violation; exit 0 when clean.
func main() {
	root := atomRoot
	if len(os.Args) > 1 {
		root = os.Args[1]
	}
	violations, scanned, err := runLint(root)
	out := bufio.NewWriter(os.Stdout)
	// `defer out.Flush()` alone would not fire before `os.Exit` — flush
	// explicitly on every exit path so violations always land on stdout.
	exit := func(code int) {
		_ = out.Flush()
		os.Exit(code)
	}
	if err != nil {
		fmt.Fprintf(out, "recipe_atom_lint: walk %s: %v\n", root, err)
		exit(1)
	}
	if len(violations) == 0 {
		fmt.Fprintf(out, "recipe_atom_lint: %d atom files scanned; 0 violations\n", scanned)
		_ = out.Flush()
		return
	}
	for _, v := range violations {
		fmt.Fprintln(out, v)
	}
	fmt.Fprintf(out, "\nrecipe_atom_lint: %d atom files scanned; %d violations\n", scanned, len(violations))
	exit(1)
}
