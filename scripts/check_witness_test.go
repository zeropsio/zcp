package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// non-parallel: the git seam cases share one fixture repository and move its
// HEAD and index; the replay cases share one fixture module.

var fixtures string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "check-witness-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fixtures = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func parsed(t *testing.T, sources map[string]string) map[string]testFile {
	t.Helper()
	files := map[string]testFile{}
	for file, src := range sources {
		f, err := collectCases(file, []byte(src))
		if err != nil {
			t.Fatal(err)
		}
		files[file] = f
	}
	return files
}

func owners(sources map[string]string) func(string) ([]byte, bool) {
	return func(owner string) ([]byte, bool) {
		src, ok := sources[owner]
		return []byte(src), ok
	}
}

func problemWith(problems []string, text string) bool {
	return slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, text) })
}

const ownerSource = "package calc\n\nfunc Double(n int) int { return n * 2 }\n"

// witness: scripts/check-witness.go | if !c.suite && len(c.witnesses) == 0 && !before[c.title] { → if false {
func TestCheck_AddedCaseWithoutWitness_Fails(t *testing.T) {
	base := "package calc\n\nimport \"testing\"\n\nfunc TestOld(t *testing.T) {}\n"
	head := base + "\nfunc TestNew(t *testing.T) {}\n"
	problems := check(input{
		base:  parsed(t, map[string]string{"calc/calc_test.go": base}),
		head:  parsed(t, map[string]string{"calc/calc_test.go": head}),
		owner: owners(map[string]string{"calc/calc.go": ownerSource}),
	})
	if !problemWith(problems, "calc/calc_test.go:7: added case TestNew has no") || problemWith(problems, "added case TestOld") {
		t.Fatalf("want only TestNew reported as an added case without a witness, got %q", problems)
	}
}

// witness: scripts/check-witness.go | if count != 1 { → if count < 1 {
func TestCheck_WitnessOldTextTwiceInOwner_Fails(t *testing.T) {
	head := "package calc\n\nimport \"testing\"\n\n// witness: calc/calc.go | return 1 → return 2\nfunc TestOne(t *testing.T) {}\n"
	problems := check(input{
		base:  map[string]testFile{},
		head:  parsed(t, map[string]string{"calc/calc_test.go": head}),
		owner: owners(map[string]string{"calc/calc.go": "package calc\n\nfunc A() int { return 1 }\nfunc B() int { return 1 }\n"}),
	})
	if !problemWith(problems, "old text occurs 2 times in calc/calc.go, not exactly once") {
		t.Fatalf("want the twice-occurring old text refused, got %q", problems)
	}
}

// witness: scripts/check-witness.go | if nonOwnerPath.MatchString(w.owner) { → if false {
func TestCheck_WitnessOwnerIsTestFile_Fails(t *testing.T) {
	head := "package calc\n\nimport \"testing\"\n\n// witness: calc/other_test.go | want := 2 → want := 3\nfunc TestOne(t *testing.T) {}\n"
	problems := check(input{
		base:  map[string]testFile{},
		head:  parsed(t, map[string]string{"calc/calc_test.go": head}),
		owner: owners(map[string]string{"calc/other_test.go": "package calc\n\nvar _ = func() int { want := 2; return want }\n"}),
	})
	if !problemWith(problems, "names calc/other_test.go, a test, fixture or recording path, as its owner") {
		t.Fatalf("want a test-file owner refused, got %q", problems)
	}
}

// witness: scripts/check-witness.go | checkFile(file, in.witnessed[file], false) → checkFile(file, testFile{}, false)
func TestCheck_RangeOwnerEditRemovesWitnessedText_Fails(t *testing.T) {
	untouched := "package calc\n\nimport \"testing\"\n\n// witness: calc/calc.go | return n * 2 → return n * 3\nfunc TestDouble(t *testing.T) {}\n"
	problems := check(input{
		base:      map[string]testFile{},
		head:      map[string]testFile{},
		witnessed: parsed(t, map[string]string{"calc/calc_test.go": untouched}),
		touched:   map[string]bool{"calc/calc.go": true},
		owner:     owners(map[string]string{"calc/calc.go": "package calc\n\nfunc Double(n int) int { return n + n }\n"}),
		drops:     true,
	})
	if !problemWith(problems, "calc/calc_test.go:6: witness \"calc/calc.go | return n * 2 → return n * 3\" old text occurs 0 times") {
		t.Fatalf("want the untouched test's witness refused after its owner edit, got %q", problems)
	}
}

// witness: scripts/check-witness.go | approved := deleted → approved := false
func TestCheck_DeletedFileWithDropsTestFile_PassesWithoutTitleTrailers(t *testing.T) {
	base := "package calc\n\nimport \"testing\"\n\n// witness: calc/calc.go | return n * 2 → return n * 3\nfunc TestDouble(t *testing.T) {\n\tt.Run(\"one\", func(t *testing.T) {})\n}\n"
	problems := check(input{
		base:    parsed(t, map[string]string{"calc/calc_test.go": base}),
		head:    map[string]testFile{},
		touched: map[string]bool{"calc/calc_test.go": true},
		owner:   owners(map[string]string{"calc/calc.go": ownerSource}),
		drops:   true,
		commits: []commit{{sha: "c1", trailers: []string{"Drops-test-file: calc/calc_test.go"}}},
	})
	if len(problems) != 0 {
		t.Fatalf("want the file deletion to approve TestDouble and TestDouble/one, got %q", problems)
	}
}

// witness: scripts/check-witness.go | headCount > baseCount { → false {
func TestCheck_RenameOntoAnotherPackagesTitle_FailsRatchet(t *testing.T) {
	// Go names are per package: the renamed case takes a name another touched package had, so it is not "added".
	problems := check(input{
		base: parsed(t, map[string]string{
			"calc/calc_test.go": "package calc\n\nimport \"testing\"\n\n// witness: calc/calc.go | return n * 2 → return n * 3\nfunc TestDouble(t *testing.T) {}\n",
			"word/word_test.go": "package word\n\nimport \"testing\"\n\n// witness: word/word.go | \"a\" → \"b\"\nfunc TestTwice(t *testing.T) {}\n",
		}),
		head: parsed(t, map[string]string{
			"calc/calc_test.go": "package calc\n\nimport \"testing\"\n\nfunc TestTwice(t *testing.T) {}\n",
			"word/word_test.go": "package word\n",
		}),
		touched: map[string]bool{"calc/calc_test.go": true, "word/word_test.go": true},
		owner:   owners(map[string]string{"calc/calc.go": ownerSource, "word/word.go": "package word\n\nvar A = \"a\"\n"}),
		drops:   true,
		commits: []commit{{sha: "c1", trailers: []string{"Drops-test: TestDouble — dead"}}},
	})
	if !slices.Equal(problems, []string{"Cases without a witness grew from 0 at the base to 1 in the touched test files; witness each case."}) {
		t.Fatalf("want the ratchet alone to refuse the unwitnessed rename, got %q", problems)
	}
}

// witness: scripts/check-witness.go | c.added[w] → witnesses[w]
func TestCheck_RewordedDropWithoutSameWitnessCase_Fails(t *testing.T) {
	problems := check(input{
		base:    parsed(t, map[string]string{"calc/calc_test.go": "package calc\n\nimport \"testing\"\n\n// witness: calc/calc.go | return n * 2 → return n * 3\nfunc TestDouble(t *testing.T) {}\n"}),
		head:    parsed(t, map[string]string{"calc/calc_test.go": "package calc\n\nimport \"testing\"\n\n// witness: calc/calc.go | func Double → func Twice\nfunc TestDouble_Doubles(t *testing.T) {}\n"}),
		touched: map[string]bool{"calc/calc_test.go": true},
		owner:   owners(map[string]string{"calc/calc.go": ownerSource}),
		drops:   true,
		commits: []commit{{sha: "c0ffee0123456789", trailers: []string{"Drops-test: TestDouble — reworded"},
			added: map[string]bool{"calc/calc.go | func Double → func Twice": true}}},
	})
	if !problemWith(problems, "Drops-test: TestDouble — reworded needs commit c0ffee012345 to add a case carrying the dropped case's witness line.") {
		t.Fatalf("want the reworded drop refused, got %q", problems)
	}
}

// fixtureModule writes the replay module once: a table-driven test of a Go
// owner and a test of a file read at run time.
func fixtureModule(t *testing.T) string {
	t.Helper()
	root := filepath.Join(fixtures, "module")
	files := map[string]string{
		"go.mod":         "module fixture\n\ngo 1.25\n",
		"calc/label.txt": "zcp",
		"calc/calc.go":   "package calc\n\nimport \"os\"\n\nfunc Double(n int) int { return n * 2 }\n\nfunc Label() string {\n\tb, _ := os.ReadFile(\"label.txt\")\n\treturn string(b)\n}\n",
		"calc/calc_test.go": "package calc\n\nimport \"testing\"\n\nfunc TestDouble(t *testing.T) {\n" +
			"\tfor _, tt := range []struct {\n\t\tname     string\n\t\tin, want int\n\t}{{\"one\", 1, 2}, {\"two\", 2, 4}} {\n" +
			"\t\tt.Run(tt.name, func(t *testing.T) {\n\t\t\tif got := Double(tt.in); got != tt.want {\n\t\t\t\tt.Fatalf(\"Double(%d) = %d\", tt.in, got)\n\t\t\t}\n\t\t})\n\t}\n}\n\n" +
			"func TestLabel(t *testing.T) {\n\tif got := Label(); got != \"zcp\" {\n\t\tt.Fatal(got)\n\t}\n}\n",
	}
	for file, src := range files {
		target := filepath.Join(root, file)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func replayOne(t *testing.T, title, text string) (string, []string) {
	t.Helper()
	root := fixtureModule(t)
	w, ok := parseWitness(text)
	if !ok {
		t.Fatalf("bad witness %q", text)
	}
	problems, notes, err := runner{root: root, env: os.Environ()}.replay(t.Context(),
		[]replayItem{{file: "calc/calc_test.go", title: title, text: text, line: 5, witness: w}})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Fatalf("want the case replayed, got notes %q", notes)
	}
	return root, problems
}

// witness: scripts/check-witness.go | if run.failedWithin(item.title) { → if strings.Contains(run.output, "FAIL") {
func TestReplay_MutationBreaksBuild_NotWitnessed(t *testing.T) {
	_, problems := replayOne(t, "TestDouble", `calc/calc.go | return n * 2 → return n * "2"`)
	if !problemWith(problems, "TestDouble witness \"calc/calc.go | return n * 2 → return n * \\\"2\\\"\" is not witnessed") {
		t.Fatalf("want a build-breaking mutation reported as not witnessed, got %q", problems)
	}
}

// witness: scripts/check-witness.go | return strings.Join(parts, "/") → return strings.Join(parts, "/") + "/^$"
func TestReplay_TableDrivenFunctionWitness_RedThroughSubtest(t *testing.T) {
	if _, problems := replayOne(t, "TestDouble", "calc/calc.go | return n * 2 → return n * 3"); len(problems) != 0 {
		t.Fatalf("want TestDouble replayed red through its table rows, got %q", problems)
	}
}

// witness: scripts/check-witness.go | if err := os.WriteFile(owner, original, info.Mode().Perm()); err != nil { → if err := error(nil); err != nil {
func TestReplay_RuntimeReadOwner_RestoredByteIdentical(t *testing.T) {
	root, problems := replayOne(t, "TestLabel", "calc/label.txt | zcp → mcp")
	label, err := os.ReadFile(filepath.Join(root, "calc/label.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 || string(label) != "zcp" {
		t.Fatalf("want TestLabel replayed red and label.txt restored to %q, got %q and %q", "zcp", problems, label)
	}
}

// fixtureRepo is one repository with a root commit; each seam case starts
// its own branch there.
func fixtureRepo(t *testing.T, branch string) repo {
	t.Helper()
	g := repo{root: filepath.Join(fixtures, "repo")}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "git", args...)
		cmd.Dir = g.root
		cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if _, err := os.Stat(filepath.Join(g.root, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Join(g.root, "calc"), 0o755); err != nil {
			t.Fatal(err)
		}
		write(t, g.root, "calc/calc.go", ownerSource)
		write(t, g.root, "calc/calc_test.go", "package calc\n\nimport \"testing\"\n\n// witness: calc/calc.go | return n * 2 → return n * 3\nfunc TestDouble(t *testing.T) {}\n")
		run("init", "-q", "-b", "root")
		run("add", ".")
		run("commit", "-q", "-m", "root")
	}
	// A forced checkout resets the index and tree a previous case left behind.
	run("checkout", "-q", "-f", "-B", branch, "root")
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	return g
}

func write(t *testing.T, root, file, src string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, file), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
}

func gitIn(t *testing.T, g repo, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@t"}, args...)
	if _, err := g.git(t.Context(), full...); err != nil {
		t.Fatal(err)
	}
}

// witness: scripts/check-witness.go | base, head, err := g.sides(ctx, files, basePrefix, ":") → base, head, err := g.sides(ctx, files, basePrefix, "HEAD:")
func TestStaged_UnwitnessedCaseInIndex_Fails(t *testing.T) {
	g := fixtureRepo(t, "staged")
	staged := "package calc\n\nimport \"testing\"\n\n// witness: calc/calc.go | return n * 2 → return n * 3\nfunc TestDouble(t *testing.T) {}\n\nfunc TestHalf(t *testing.T) {}\n"
	write(t, g.root, "calc/calc_test.go", staged)
	gitIn(t, g, "add", "calc/calc_test.go")
	// The working tree witnesses the case; the commit being made does not.
	write(t, g.root, "calc/calc_test.go", strings.Replace(staged, "\nfunc TestHalf", "\n// witness: calc/calc.go | n * 2 → n / 2\nfunc TestHalf", 1))
	in, err := g.staged(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if problems := check(in); !problemWith(problems, "calc/calc_test.go:8: added case TestHalf has no") {
		t.Fatalf("want the staged TestHalf refused, got %q", problems)
	}
}

// witness: scripts/check-witness.go | out, err := g.git(ctx, "log", "--format=%x1e%H%n%B", base+".."+head) → out, err := g.git(ctx, "log", "--format=%x1e%H%n%(trailers)", base+".."+head)
func TestSpan_DropsLineBeforeWhyParagraph_Approves(t *testing.T) {
	g := fixtureRepo(t, "span")
	write(t, g.root, "calc/calc_test.go", "package calc\n")
	gitIn(t, g, "commit", "-q", "-am", "test: drop TestDouble\n\nDrops-test: TestDouble — dead\n\nWhy: Double has no caller left.")
	in, _, err := g.span(t.Context(), "root", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if problems := check(in); len(problems) != 0 {
		t.Fatalf("want the classed Drops-test line to approve TestDouble, got %q", problems)
	}
}
