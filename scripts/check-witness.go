// Command check-witness enforces OWNER.md Testing's admission evidence on Go
// tests: every added case carries a `// witness: <owner path> | <old> → <new>`
// line that still applies, unwitnessed cases never grow against the base, and
// a removed test name carries a `Drops-test: <name> — <class>` trailer or its
// deleted file a `Drops-test-file: <path>` trailer.
//
//	go run scripts/check-witness.go                                   # staged commit
//	go run scripts/check-witness.go --base <rev> [--head <rev>] [--replay]  # pushed range
//
// The staged form (make check-changed, pre-commit) checks touched test files
// only: drop trailers live in a commit message that does not exist yet. The
// range form (scripts/witness-replay.sh, pre-push) also checks witnesses in
// untouched test files that name an owner touched in the range, the drop
// trailers, and with --replay applies each new witness of a short test and
// requires its case to fail. Tests behind an e2e build tag get the static
// checks only.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unicode"
)

// dropClasses is OWNER.md's removal class set; `reworded` also needs the same
// commit to add a case carrying the dropped case's witness line.
var dropClasses = []string{"copy", "duplicate", "fake-input", "mock", "dead",
	"library", "trivia", "harness", "reworded"}

// bareDropsCutoff is this guard's merge base: commits it contains approve a
// drop with the bare `Drops-test: <name>` form they were written under.
const bareDropsCutoff = "0a28d1db212d6a31d1e7c0609a71f9f185b9780d"

const witnessMarker = "// witness: "

// Owners are product code: a witness on a test, fixture or recording proves
// nothing about behaviour.
var nonOwnerPath = regexp.MustCompile(
	`_test\.go$|(?:^|/)(?:testdata|fixtures?|recordings?)/|\.(?:fixture|recording)s?\.[^/]+$`)

type witness struct{ owner, old, next string }

// parseWitness splits `<owner> | <old> → <new>`: the path ends at the first
// " | ", old and new split at the last " →", so code may hold arrows; an
// empty new text deletes the old one.
func parseWitness(text string) (witness, bool) {
	bar := strings.Index(text, " | ")
	if bar <= 0 {
		return witness{}, false
	}
	rest := text[bar+3:]
	arrow := strings.LastIndex(rest, " →")
	end := arrow + len(" →")
	if arrow < 0 || (len(rest) > end && rest[end] != ' ') {
		return witness{}, false
	}
	next := ""
	if len(rest) > end {
		next = rest[end+1:]
	}
	return witness{owner: text[:bar], old: rest[:arrow], next: next}, true
}

// testCase is a top-level Test function or a literal t.Run subtest.
type testCase struct {
	title string
	// own witnesses sit directly above the case; witnesses adds the
	// enclosing cases' ones, which cover every subtest declared inside.
	own, witnesses []string
	// suite marks a case declaring literal subtests: they are its cases.
	suite bool
	line  int
}

// testFile is one parsed _test.go file.
type testFile struct {
	cases []testCase
	e2e   bool
}

func (f testFile) witnessLines() map[string]bool {
	lines := map[string]bool{}
	for _, c := range f.cases {
		for _, w := range c.own {
			lines[w] = true
		}
	}
	return lines
}

// subtestName mirrors the testing package's rewrite of a t.Run name.
func subtestName(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsSpace(r):
			b.WriteByte('_')
		case !strconv.IsPrint(r):
			s := strconv.QuoteRune(r)
			b.WriteString(s[1 : len(s)-1])
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func mentionsTag(expr constraint.Expr, tag string) bool {
	switch e := expr.(type) {
	case *constraint.TagExpr:
		return e.Tag == tag
	case *constraint.NotExpr:
		return mentionsTag(e.X, tag)
	case *constraint.AndExpr:
		return mentionsTag(e.X, tag) || mentionsTag(e.Y, tag)
	case *constraint.OrExpr:
		return mentionsTag(e.X, tag) || mentionsTag(e.Y, tag)
	}
	return false
}

func isTestFunc(fn *ast.FuncDecl) bool {
	name := fn.Name.Name
	if fn.Recv != nil || fn.Body == nil || !strings.HasPrefix(name, "Test") || name == "TestMain" {
		return false
	}
	if rest := name[len("Test"):]; rest != "" && unicode.IsLower([]rune(rest)[0]) {
		return false
	}
	params := fn.Type.Params.List
	if len(params) != 1 || len(params[0].Names) > 1 {
		return false
	}
	star, ok := params[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := star.X.(*ast.SelectorExpr)
	return ok && sel.Sel.Name == "T"
}

// literalRun returns the name and body of `x.Run("literal", func(...) {...})`.
func literalRun(node ast.Node) (string, *ast.FuncLit, bool) {
	call, ok := node.(*ast.CallExpr)
	if !ok || len(call.Args) != 2 {
		return "", nil, false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || sel.Sel.Name != "Run" {
		return "", nil, false
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	body, isFunc := call.Args[1].(*ast.FuncLit)
	if !ok || !isFunc || lit.Kind != token.STRING {
		return "", nil, false
	}
	name, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", nil, false
	}
	return name, body, true
}

func collectCases(file string, src []byte) (testFile, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, file, src, parser.ParseComments)
	if err != nil {
		return testFile{}, fmt.Errorf("parse %s: %w", file, err)
	}
	result := testFile{}
	// A witness sits in the block of whole-line comments directly above its case.
	comments := map[int]string{}
	for _, group := range parsed.Comments {
		for _, c := range group.List {
			pos := fset.Position(c.Slash)
			lineStart := pos.Offset - (pos.Column - 1)
			if !strings.HasPrefix(c.Text, "//") || strings.TrimSpace(string(src[lineStart:pos.Offset])) != "" {
				continue
			}
			comments[pos.Line] = c.Text[2:]
			if c.Slash < parsed.Package {
				if expr, err := constraint.Parse(c.Text); err == nil && mentionsTag(expr, "e2e") {
					result.e2e = true
				}
			}
		}
	}
	witnessesAbove := func(line int) []string {
		var found []string
		for l := line - 1; ; l-- {
			text, ok := comments[l]
			if !ok {
				return found
			}
			if trimmed := strings.TrimLeft(text, " \t"); strings.HasPrefix(trimmed, "witness: ") {
				found = append([]string{strings.TrimRight(trimmed[len("witness: "):], " \t")}, found...)
			}
		}
	}
	var visit func(body ast.Node, parent int, inherited []string)
	add := func(title string, line int, inherited []string) int {
		own := witnessesAbove(line)
		result.cases = append(result.cases, testCase{title: title, own: own,
			witnesses: append(slices.Clone(own), inherited...), line: line})
		return len(result.cases) - 1
	}
	visit = func(body ast.Node, parent int, inherited []string) {
		ast.Inspect(body, func(node ast.Node) bool {
			name, fn, ok := literalRun(node)
			if !ok {
				return true
			}
			result.cases[parent].suite = true
			index := add(result.cases[parent].title+"/"+subtestName(name), fset.Position(node.Pos()).Line, inherited)
			visit(fn.Body, index, result.cases[index].witnesses)
			return false
		})
	}
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !isTestFunc(fn) {
			continue
		}
		// FuncDecl.Pos is the func keyword, so its doc comment is the block above.
		index := add(fn.Name.Name, fset.Position(fn.Pos()).Line, nil)
		visit(fn.Body, index, result.cases[index].witnesses)
	}
	return result, nil
}

// commit is one commit of a pushed range as the drop rules read it.
type commit struct {
	sha      string
	trailers []string
	// legacy commits predate classed trailers.
	legacy bool
	// added holds the witness lines on cases this commit adds; filled for
	// commits carrying a `reworded` trailer.
	added map[string]bool
}

// input is what the rules read; the git layer fills it per mode.
type input struct {
	// base and head hold the touched test files; a file absent on a side
	// did not exist there.
	base, head map[string]testFile
	// witnessed holds untouched test files whose witnesses name a touched owner.
	witnessed map[string]testFile
	touched   map[string]bool
	owner     func(path string) ([]byte, bool)
	// drops enables the trailer rules over commits (range mode).
	drops   bool
	commits []commit
}

func unwitnessed(files map[string]testFile) int {
	count := 0
	for _, f := range files {
		for _, c := range f.cases {
			if !c.suite && len(c.witnesses) == 0 {
				count++
			}
		}
	}
	return count
}

func titles(files map[string]testFile) map[string]bool {
	set := map[string]bool{}
	for _, f := range files {
		for _, c := range f.cases {
			set[c.title] = true
		}
	}
	return set
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func (in input) witnessProblem(text string) string {
	w, ok := parseWitness(text)
	if !ok || path.IsAbs(w.owner) || slices.Contains(strings.Split(w.owner, "/"), "..") {
		return "is not `<owner path> | <old> → <new>` inside the repository"
	}
	if nonOwnerPath.MatchString(w.owner) {
		return "names " + w.owner + ", a test, fixture or recording path, as its owner"
	}
	source, ok := in.owner(w.owner)
	if !ok {
		return "names a missing owner " + w.owner
	}
	if w.old == w.next {
		return "changes nothing: its old text equals its new text"
	}
	count := 0
	if w.old != "" {
		count = bytes.Count(source, []byte(w.old))
	}
	if count != 1 {
		return fmt.Sprintf("old text occurs %d times in %s, not exactly once", count, w.owner)
	}
	return ""
}

// check applies the witness and drop rules; it returns one line per failure.
func check(in input) []string {
	var problems []string
	before, after := titles(in.base), titles(in.head)
	for _, file := range sortedKeys(in.head) {
		for _, c := range in.head[file].cases {
			if !c.suite && len(c.witnesses) == 0 && !before[c.title] {
				problems = append(problems, fmt.Sprintf(
					"%s:%d: added case %s has no `// witness: <owner path> | <old> → <new>` line directly above it.",
					file, c.line, c.title))
			}
		}
	}
	// Touched tests check every witness; untouched ones only those naming a touched owner.
	checkFile := func(file string, f testFile, all bool) {
		seen := map[string]bool{}
		for _, c := range f.cases {
			for _, text := range c.own {
				w, _ := parseWitness(text)
				if seen[text] || (!all && !in.touched[w.owner]) {
					continue
				}
				seen[text] = true
				if problem := in.witnessProblem(text); problem != "" {
					problems = append(problems, fmt.Sprintf("%s:%d: witness %q %s.", file, c.line, text, problem))
				}
			}
		}
	}
	for _, file := range sortedKeys(in.head) {
		checkFile(file, in.head[file], true)
	}
	for _, file := range sortedKeys(in.witnessed) {
		checkFile(file, in.witnessed[file], false)
	}
	if baseCount, headCount := unwitnessed(in.base), unwitnessed(in.head); headCount > baseCount {
		problems = append(problems, fmt.Sprintf(
			"Cases without a witness grew from %d at the base to %d in the touched test files; witness each case.",
			baseCount, headCount))
	}
	if in.drops {
		problems = append(problems, in.dropProblems(before, after)...)
	}
	return problems
}

type approval struct {
	commit commit
	class  string // empty for a bare trailer
}

func (in input) dropProblems(before, after map[string]bool) []string {
	approvals := map[string][]approval{}
	droppedFiles := map[string]bool{}
	for _, c := range in.commits {
		for _, line := range c.trailers {
			if file, ok := strings.CutPrefix(line, "Drops-test-file: "); ok {
				droppedFiles[file] = true
			}
			value, ok := strings.CutPrefix(line, "Drops-test: ")
			if !ok {
				continue
			}
			// Titles may contain " — "; the class is the fixed word after the last one.
			if cut := strings.LastIndex(value, " — "); cut >= 0 && slices.Contains(dropClasses, value[cut+len(" — "):]) {
				approvals[value[:cut]] = append(approvals[value[:cut]], approval{c, value[cut+len(" — "):]})
			}
			approvals[value] = append(approvals[value], approval{commit: c})
		}
	}
	var problems []string
	for _, title := range sortedKeys(before) {
		if after[title] {
			continue
		}
		witnesses := map[string]bool{}
		deleted := false
		for file, f := range in.base {
			for _, c := range f.cases {
				if c.title != title {
					continue
				}
				_, kept := in.head[file]
				deleted = deleted || (droppedFiles[file] && !kept)
				for _, w := range c.witnesses {
					witnesses[w] = true
				}
			}
		}
		approved := deleted
		var reworded []commit
		for _, a := range approvals[title] {
			switch {
			case a.class == "" && a.commit.legacy, a.class != "" && a.class != "reworded":
				approved = true
			case a.class == "reworded":
				reworded = append(reworded, a.commit)
			}
		}
		for _, c := range reworded {
			for w := range witnesses {
				approved = approved || c.added[w]
			}
		}
		if approved {
			continue
		}
		for _, c := range reworded {
			problems = append(problems, fmt.Sprintf(
				"Drops-test: %s — reworded needs commit %.12s to add a case carrying the dropped case's witness line.",
				title, c.sha))
		}
		problems = append(problems, fmt.Sprintf(
			"Dropped test %s needs Drops-test: %s — <class> (%s), or Drops-test-file: <path> deleting its file.",
			title, title, strings.Join(dropClasses, ", ")))
	}
	return problems
}

// replayItem is one new witness of a short test, applied to its owner.
type replayItem struct {
	file, title, text string
	line              int
	witness           witness
}

// newWitnesses lists the own witnesses in touched non-e2e test files that the
// base version of the file did not carry.
func newWitnesses(in input) []replayItem {
	var items []replayItem
	for _, file := range sortedKeys(in.head) {
		f := in.head[file]
		if f.e2e {
			continue
		}
		old := in.base[file].witnessLines()
		for _, c := range f.cases {
			for _, text := range c.own {
				if w, ok := parseWitness(text); ok && !old[text] {
					items = append(items, replayItem{file: file, title: c.title, text: text, line: c.line, witness: w})
				}
			}
		}
	}
	return items
}

// runPattern selects exactly the case: each -run level is anchored.
func runPattern(title string) string {
	parts := strings.Split(title, "/")
	for i, p := range parts {
		parts[i] = "^" + regexp.QuoteMeta(p) + "$"
	}
	return strings.Join(parts, "/")
}

// testEvent is one `go test -json` line; encoding/json matches test2json's
// capitalised keys case-insensitively.
type testEvent struct {
	Action string `json:"action"`
	Test   string `json:"test"`
	Output string `json:"output"`
}

type runner struct {
	root string
	env  []string
}

// testRun is one `go test -json` run: each test's final action and the output.
type testRun struct {
	actions map[string]string
	output  string
}

func (t testRun) failedWithin(title string) bool {
	for test, action := range t.actions {
		if action == "fail" && (test == title || strings.HasPrefix(test, title+"/")) {
			return true
		}
	}
	return false
}

func (t testRun) tail(lines int) string {
	all := strings.Split(strings.TrimRight(t.output, "\n"), "\n")
	return strings.Join(all[max(0, len(all)-lines):], "\n")
}

// goTest runs one package's selected tests.
func (r runner) goTest(ctx context.Context, pkg, pattern, overlay string) (testRun, error) {
	args := []string{"test", "-short", "-count=1", "-json", "-run", pattern}
	if overlay != "" {
		args = append(args, "-overlay", overlay)
	}
	cmd := exec.CommandContext(ctx, "go", append(args, pkg)...) //nolint:gosec // argv, no shell
	cmd.Dir, cmd.Env = r.root, r.env
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		return testRun{}, fmt.Errorf("go test %s: %w", pkg, err)
	}
	run := testRun{actions: map[string]string{}}
	var text strings.Builder
	scanner := bufio.NewScanner(bytes.NewReader(out))
	scanner.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	for scanner.Scan() {
		var event testEvent
		if json.Unmarshal(scanner.Bytes(), &event) != nil {
			text.Write(scanner.Bytes())
			text.WriteByte('\n')
			continue
		}
		text.WriteString(event.Output)
		if event.Test != "" && (event.Action == "pass" || event.Action == "fail" || event.Action == "skip") {
			run.actions[event.Test] = event.Action
		}
	}
	if err := scanner.Err(); err != nil {
		return testRun{}, fmt.Errorf("go test %s output: %w", pkg, err)
	}
	run.output = text.String()
	return run, nil
}

// replay applies each witness to its owner in the working tree's build and
// requires the witnessed case, passing unmutated, to fail with the package
// built. Go owners are mutated through -overlay, so the tree is never
// touched; other owners (read at run time) are rewritten in place and
// restored before the next item.
func (r runner) replay(ctx context.Context, items []replayItem) (problems, notes []string, err error) {
	scratch, err := os.MkdirTemp("", "check-witness-")
	if err != nil {
		return nil, nil, fmt.Errorf("replay scratch: %w", err)
	}
	defer os.RemoveAll(scratch)
	// One unmutated run per package covers every replayed top-level test in it.
	tops := map[string][]string{}
	for _, item := range items {
		pkg := "./" + path.Dir(item.file)
		top, _, _ := strings.Cut(item.title, "/")
		if !slices.Contains(tops[pkg], regexp.QuoteMeta(top)) {
			tops[pkg] = append(tops[pkg], regexp.QuoteMeta(top))
		}
	}
	unmutated := map[string]testRun{}
	for _, pkg := range sortedKeys(tops) {
		if unmutated[pkg], err = r.goTest(ctx, pkg, "^("+strings.Join(tops[pkg], "|")+")$", ""); err != nil {
			return nil, nil, err
		}
	}
	for _, item := range items {
		pkg := "./" + path.Dir(item.file)
		where := fmt.Sprintf("%s:%d: %s witness %q", item.file, item.line, item.title, item.text)
		switch unmutated[pkg].actions[item.title] {
		case "pass":
		case "skip":
			notes = append(notes, where+" not replayed: the case skips under -short.")
			continue
		case "":
			problems = append(problems, fmt.Sprintf("%s: the unmutated run does not run the case:\n%s",
				where, unmutated[pkg].tail(15)))
			continue
		default:
			problems = append(problems, where+": the case fails without the mutation.")
			continue
		}
		run, err := r.mutated(ctx, scratch, pkg, item)
		if err != nil {
			return nil, nil, err
		}
		if run.failedWithin(item.title) {
			fmt.Fprintf(os.Stderr, "check-witness: replayed red: %s\n", where)
			continue
		}
		problems = append(problems, fmt.Sprintf(
			"%s is not witnessed: the mutation does not fail the case (a build error does not count):\n%s",
			where, run.tail(15)))
	}
	return problems, notes, nil
}

func (r runner) mutated(ctx context.Context, scratch, pkg string, item replayItem) (testRun, error) {
	owner := filepath.Join(r.root, filepath.FromSlash(item.witness.owner))
	original, err := os.ReadFile(owner)
	if err != nil {
		return testRun{}, fmt.Errorf("replay owner: %w", err)
	}
	changed := bytes.Replace(original, []byte(item.witness.old), []byte(item.witness.next), 1)
	if strings.HasSuffix(owner, ".go") {
		mutant := filepath.Join(scratch, "mutant.go")
		overlay := filepath.Join(scratch, "overlay.json")
		spec, err := json.Marshal(map[string]map[string]string{"Replace": {owner: mutant}})
		if err != nil {
			return testRun{}, fmt.Errorf("replay overlay: %w", err)
		}
		if err := os.WriteFile(mutant, changed, 0o600); err != nil {
			return testRun{}, fmt.Errorf("replay mutant: %w", err)
		}
		if err := os.WriteFile(overlay, spec, 0o600); err != nil {
			return testRun{}, fmt.Errorf("replay overlay: %w", err)
		}
		return r.goTest(ctx, pkg, runPattern(item.title), overlay)
	}
	info, err := os.Stat(owner)
	if err != nil {
		return testRun{}, fmt.Errorf("replay owner: %w", err)
	}
	if err := os.WriteFile(owner, changed, info.Mode().Perm()); err != nil {
		return testRun{}, fmt.Errorf("replay mutate owner: %w", err)
	}
	run, runErr := r.goTest(ctx, pkg, runPattern(item.title), "")
	if err := os.WriteFile(owner, original, info.Mode().Perm()); err != nil {
		return testRun{}, fmt.Errorf("replay restore %s: %w", item.witness.owner, err)
	}
	return run, runErr
}

// repo reads revisions through git.
type repo struct{ root string }

// command is git with argv in the repository; no shell parses it.
func (g repo) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.root
	return cmd
}

func (g repo) git(ctx context.Context, args ...string) (string, error) {
	cmd := g.command(ctx, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return string(out), nil
}

// blobs reads `<rev>:<path>` specs (`:<path>` is the index) in one process;
// missing specs are absent from the result.
func (g repo) blobs(ctx context.Context, specs []string) (map[string][]byte, error) {
	found := map[string][]byte{}
	if len(specs) == 0 {
		return found, nil
	}
	cmd := g.command(ctx, "cat-file", "--batch")
	cmd.Stdin = strings.NewReader(strings.Join(specs, "\n") + "\n")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git cat-file: %w", err)
	}
	for _, spec := range specs {
		end := bytes.IndexByte(out, '\n')
		if end < 0 {
			return nil, fmt.Errorf("git cat-file: truncated output at %s", spec)
		}
		header := strings.Fields(string(out[:end]))
		out = out[end+1:]
		if len(header) != 3 {
			continue
		}
		size, err := strconv.Atoi(header[2])
		if err != nil || size+1 > len(out) {
			return nil, fmt.Errorf("git cat-file: bad header %q", header)
		}
		found[spec] = out[:size]
		out = out[size+1:]
	}
	return found, nil
}

func (g repo) changed(ctx context.Context, args ...string) ([]string, error) {
	out, err := g.git(ctx, append([]string{"diff", "--name-only", "--no-renames", "-z"}, args...)...)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(strings.Split(out, "\x00"), func(s string) bool { return s == "" }), nil
}

func isTestFile(file string) bool { return strings.HasSuffix(file, "_test.go") }

// sides parses the touched test files at both revisions (prefixes like "HEAD:" or ":").
func (g repo) sides(ctx context.Context, files []string, basePrefix, headPrefix string) (base, head map[string]testFile, err error) {
	var specs []string
	for _, file := range files {
		if isTestFile(file) {
			if basePrefix != "" {
				specs = append(specs, basePrefix+file)
			}
			specs = append(specs, headPrefix+file)
		}
	}
	sources, err := g.blobs(ctx, specs)
	if err != nil {
		return nil, nil, err
	}
	parse := func(prefix string) (map[string]testFile, error) {
		parsed := map[string]testFile{}
		for _, file := range files {
			src, ok := sources[prefix+file]
			if prefix == "" || !ok || !isTestFile(file) {
				continue
			}
			f, err := collectCases(file, src)
			if err != nil {
				return nil, err
			}
			parsed[file] = f
		}
		return parsed, nil
	}
	if base, err = parse(basePrefix); err != nil {
		return nil, nil, err
	}
	if head, err = parse(headPrefix); err != nil {
		return nil, nil, err
	}
	return base, head, nil
}

func (g repo) ownerAt(ctx context.Context, prefix string) func(string) ([]byte, bool) {
	cache := map[string][]byte{}
	return func(owner string) ([]byte, bool) {
		if src, ok := cache[owner]; ok {
			return src, src != nil
		}
		found, err := g.blobs(ctx, []string{prefix + owner})
		cache[owner] = nil
		if err == nil && found[prefix+owner] != nil {
			cache[owner] = found[prefix+owner]
		}
		return cache[owner], cache[owner] != nil
	}
}

// staged reads the commit being made: the index against HEAD.
func (g repo) staged(ctx context.Context) (input, error) {
	files, err := g.changed(ctx, "--cached")
	if err != nil {
		return input{}, err
	}
	basePrefix := "HEAD:"
	if _, err := g.git(ctx, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		basePrefix = ""
	}
	base, head, err := g.sides(ctx, files, basePrefix, ":")
	if err != nil {
		return input{}, err
	}
	return input{base: base, head: head, owner: g.ownerAt(ctx, ":")}, nil
}

// span reads a range: base is the merge base of the given base and head.
func (g repo) span(ctx context.Context, baseRef, headRef string) (input, string, error) {
	headOut, err := g.git(ctx, "rev-parse", "--verify", headRef+"^{commit}")
	if err != nil {
		return input{}, "", err
	}
	head := strings.TrimSpace(headOut)
	baseOut, err := g.git(ctx, "merge-base", baseRef, head)
	if err != nil {
		return input{}, "", err
	}
	base := strings.TrimSpace(baseOut)
	files, err := g.changed(ctx, base, head)
	if err != nil {
		return input{}, "", err
	}
	in := input{touched: map[string]bool{}, owner: g.ownerAt(ctx, head+":"), drops: true}
	for _, file := range files {
		in.touched[file] = true
	}
	if in.base, in.head, err = g.sides(ctx, files, base+":", head+":"); err != nil {
		return input{}, "", err
	}
	if in.witnessed, err = g.witnessedElsewhere(ctx, head, in); err != nil {
		return input{}, "", err
	}
	if in.commits, err = g.commits(ctx, base, head); err != nil {
		return input{}, "", err
	}
	return in, head, nil
}

// witnessedElsewhere parses the untouched test files at head that hold a witness line.
func (g repo) witnessedElsewhere(ctx context.Context, head string, in input) (map[string]testFile, error) {
	cmd := g.command(ctx, "grep", "-l", "-F", "-e", witnessMarker, head, "--", "*_test.go")
	out, err := cmd.Output()
	var exitErr *exec.ExitError
	if err != nil && (!errors.As(err, &exitErr) || exitErr.ExitCode() != 1) {
		return nil, fmt.Errorf("git grep witnesses: %w", err)
	}
	var specs []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if file := strings.TrimPrefix(line, head+":"); line != "" && !in.touched[file] {
			specs = append(specs, head+":"+file)
		}
	}
	sources, err := g.blobs(ctx, specs)
	if err != nil {
		return nil, err
	}
	files := map[string]testFile{}
	for spec, src := range sources {
		file := strings.TrimPrefix(spec, head+":")
		parsed, err := collectCases(file, src)
		if err != nil {
			return nil, err
		}
		for _, c := range parsed.cases {
			for _, text := range c.own {
				if w, ok := parseWitness(text); ok && in.touched[w.owner] {
					files[file] = parsed
				}
			}
		}
	}
	return files, nil
}

func (g repo) commits(ctx context.Context, base, head string) ([]commit, error) {
	// Whole message lines, not git's trailer block: zcp commits follow their
	// Drops-test lines with a paragraph saying why.
	out, err := g.git(ctx, "log", "--format=%x1e%H%n%B", base+".."+head)
	if err != nil {
		return nil, err
	}
	var commits []commit
	for _, entry := range strings.Split(out, "\x1e")[1:] {
		lines := strings.Split(strings.TrimRight(entry, "\n"), "\n")
		c := commit{sha: lines[0]}
		for _, line := range lines[1:] {
			if strings.HasPrefix(line, "Drops-test: ") || strings.HasPrefix(line, "Drops-test-file: ") {
				c.trailers = append(c.trailers, strings.TrimRight(line, " \t\r"))
			}
		}
		for _, line := range c.trailers {
			value, ok := strings.CutPrefix(line, "Drops-test: ")
			if !ok {
				continue
			}
			if strings.HasSuffix(value, " — reworded") && c.added == nil {
				if c.added, err = g.addedWitnesses(ctx, c.sha); err != nil {
					return nil, err
				}
			}
			if !c.legacy {
				c.legacy = g.command(ctx, "merge-base", "--is-ancestor", c.sha, bareDropsCutoff).Run() == nil
			}
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// addedWitnesses returns the witness lines on the cases a commit adds.
func (g repo) addedWitnesses(ctx context.Context, sha string) (map[string]bool, error) {
	out, err := g.git(ctx, "diff-tree", "--root", "--no-commit-id", "-r", "--name-only", "--no-renames", "-z", sha)
	if err != nil {
		return nil, err
	}
	files := strings.Split(out, "\x00")
	parent, current, err := g.sides(ctx, files, sha+"^:", sha+":")
	if err != nil {
		return nil, err
	}
	before := titles(parent)
	added := map[string]bool{}
	for _, f := range current {
		for _, c := range f.cases {
			if !c.suite && !before[c.title] {
				for _, w := range c.witnesses {
					added[w] = true
				}
			}
		}
	}
	return added, nil
}

// replayable refuses a replay whose files differ from the pushed revision:
// the replay builds the working tree.
func (g repo) replayable(ctx context.Context, head string, items []replayItem) error {
	paths := make([]string, 0, 2*len(items))
	for _, item := range items {
		paths = append(paths, item.file, item.witness.owner)
	}
	if out, err := g.git(ctx, append([]string{"diff", "--name-only", head, "--"}, paths...)...); err != nil || out != "" {
		if err != nil {
			return err
		}
		return fmt.Errorf("witness replay builds the working tree, which differs from %.12s in: %s",
			head, strings.Join(strings.Fields(out), " "))
	}
	return nil
}

// run reads ctx, cancelled on SIGINT or SIGTERM, so an in-place mutation is restored before exit.
func run(ctx context.Context) (int, error) {
	baseRef := flag.String("base", "", "check the range from the merge base of this revision to --head")
	headRef := flag.String("head", "HEAD", "the range's last revision")
	doReplay := flag.Bool("replay", false, "replay new witnesses of short tests (range mode)")
	flag.Parse()
	if flag.NArg() != 0 || (*baseRef == "" && (*doReplay || *headRef != "HEAD")) {
		return 2, errors.New("usage: check-witness [--base <rev> [--head <rev>] [--replay]]")
	}
	top, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return 1, fmt.Errorf("repository root: %w", err)
	}
	g := repo{root: strings.TrimSpace(string(top))}
	var in input
	head := ""
	if *baseRef == "" {
		in, err = g.staged(ctx)
	} else {
		in, head, err = g.span(ctx, *baseRef, *headRef)
	}
	if err != nil {
		return 1, err
	}
	problems := check(in)
	if len(problems) == 0 && *doReplay {
		items := newWitnesses(in)
		if err := g.replayable(ctx, head, items); err != nil {
			return 1, err
		}
		local, err := g.git(ctx, "rev-parse", "--local-env-vars")
		if err != nil {
			return 1, err
		}
		// Tests create their own repositories; a hook's GIT_DIR would point them at ours.
		env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
			name, _, _ := strings.Cut(kv, "=")
			return slices.Contains(strings.Fields(local), name)
		})
		var notes []string
		problems, notes, err = runner{root: g.root, env: env}.replay(ctx, items)
		if err != nil {
			return 1, err
		}
		for _, note := range notes {
			fmt.Fprintln(os.Stderr, "check-witness: "+note)
		}
	}
	for _, problem := range problems {
		fmt.Fprintln(os.Stderr, problem)
	}
	if len(problems) > 0 {
		return 1, nil
	}
	fmt.Fprintf(os.Stderr, "check-witness: %d touched test files; added cases carry witnesses that apply.\n", len(in.head))
	return 0, nil
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code, err := run(ctx)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, "check-witness: "+err.Error())
	}
	os.Exit(code)
}
