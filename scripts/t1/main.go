// Command t1 is zcp's T1: it runs every unit case, and only those, and exits non-zero when one is
// red, when the run takes longer than the budget, or when a unit id does not name exactly one case.
// scripts/gate calls it; run it from anywhere in a worktree:
//
//	go run ./scripts/t1
//
// A unit id is a line of a tracked *_SENTENCES.md that starts with the id and " — " (optionally
// after "- "). Its case is the one top-level Test function in a tracked *_test.go without a
// //go:build line whose second "_"-separated name segment equals the id without its hyphens,
// compared case-insensitively (SV-READY names TestStandupVerdict_SVReady_…). Every id is mapped
// before anything runs; the cases then run in one go test command. The budget counts the
// packages' own run time from go test's JSON stream, not compilation, which the build cache owns.
// Everything goes to stderr; the last line is "T1: N cases, S.Ss".
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/zeropsio/zcp/scripts/t1/unit"
)

const budgetSeconds = 30.0

type event struct {
	Action  string  `json:"Action"`  //nolint:tagliatelle // go test -json wire format
	Package string  `json:"Package"` //nolint:tagliatelle // go test -json wire format
	Test    string  `json:"Test"`    //nolint:tagliatelle // go test -json wire format
	Output  string  `json:"Output"`  //nolint:tagliatelle // go test -json wire format
	Elapsed float64 `json:"Elapsed"` //nolint:tagliatelle // go test -json wire format
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "t1:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := gitOut("", "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("find worktree: %w", err)
	}
	root = strings.TrimSpace(root)

	lists, err := readFiles(root, "*_SENTENCES.md")
	if err != nil {
		return fmt.Errorf("read unit lists: %w", err)
	}
	tests, err := readFiles(root, "*_test.go")
	if err != nil {
		return fmt.Errorf("read unit cases: %w", err)
	}
	ids := unit.ReadIDs(lists)
	byKey := unit.ReadCases(tests)

	var cases []unit.Case
	var unmapped []string
	for _, u := range ids {
		found := byKey[unit.IDKey(u.ID)]
		switch len(found) {
		case 1:
			cases = append(cases, found[0])
		case 0:
			unmapped = append(unmapped, fmt.Sprintf("%s (%s): no case", u.ID, u.List))
		default:
			names := make([]string, 0, len(found))
			for _, c := range found {
				names = append(names, c.Dir+"."+c.Name)
			}
			unmapped = append(unmapped, fmt.Sprintf("%s (%s): %d cases: %s", u.ID, u.List, len(found), strings.Join(names, ", ")))
		}
	}
	if len(unmapped) > 0 {
		for _, m := range unmapped {
			fmt.Fprintln(os.Stderr, "T1 unit id without exactly one case:", m)
		}
		return fmt.Errorf("%d of %d unit ids do not name exactly one case; no test ran", len(unmapped), len(ids))
	}
	if len(cases) == 0 {
		return fmt.Errorf("no unit ids in any tracked *_SENTENCES.md")
	}

	return runCases(root, cases)
}

func runCases(root string, cases []unit.Case) error {
	names := make([]string, 0, len(cases))
	dirSet := map[string]bool{}
	for _, c := range cases {
		names = append(names, regexp.QuoteMeta(c.Name))
		dirSet["./"+c.Dir] = true
	}
	dirs := make([]string, 0, len(dirSet))
	for d := range dirSet {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)

	args := append([]string{"test", "-count=1", "-json", "-run", "^(" + strings.Join(names, "|") + ")$"}, dirs...)
	cmd := exec.CommandContext(context.Background(), "go", args...)
	cmd.Dir = root
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("go test: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("go test: %w", err)
	}

	output := map[string]*bytes.Buffer{} // package + "\x00" + test → its output
	var failed []string
	passed, elapsed := 0, 0.0
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<24)
	for sc.Scan() {
		var e event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			fmt.Fprintln(os.Stderr, sc.Text())
			continue
		}
		key := e.Package + "\x00" + e.Test
		switch e.Action {
		case "build-output":
			fmt.Fprint(os.Stderr, e.Output)
		case "output":
			if output[key] == nil {
				output[key] = &bytes.Buffer{}
			}
			output[key].WriteString(e.Output)
		case "pass", "fail":
			if e.Test == "" {
				elapsed += e.Elapsed
			} else if e.Action == "pass" && !strings.Contains(e.Test, "/") {
				passed++
			}
			if e.Action == "fail" {
				if b := output[key]; b != nil {
					fmt.Fprint(os.Stderr, b.String())
				}
				if e.Test == "" {
					failed = append(failed, "package "+e.Package)
				} else {
					failed = append(failed, e.Package+" "+e.Test)
				}
			}
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read go test output: %w", err)
	}
	waitErr := cmd.Wait()

	var red []string
	for _, f := range failed {
		fmt.Fprintln(os.Stderr, "T1 FAIL:", f)
	}
	if len(failed) > 0 {
		red = append(red, fmt.Sprintf("%d failed", len(failed)))
	}
	if waitErr != nil && len(failed) == 0 {
		red = append(red, fmt.Sprintf("go test: %v", waitErr))
	}
	if passed != len(cases) && len(failed) == 0 {
		red = append(red, fmt.Sprintf("%d of %d cases passed", passed, len(cases)))
	}
	fmt.Fprintf(os.Stderr, "T1: %d cases, %.1fs\n", len(cases), elapsed)
	if elapsed > budgetSeconds {
		fmt.Fprintf(os.Stderr, "T1 over budget: %.1fs > %.0fs\n", elapsed, budgetSeconds)
		red = append(red, "over budget")
	}
	if len(red) > 0 {
		return fmt.Errorf("red: %s", strings.Join(red, "; "))
	}
	return nil
}

func gitFiles(root, pattern string) ([]string, error) {
	out, err := gitOut(root, "ls-files", "-z", "--", pattern)
	if err != nil {
		return nil, fmt.Errorf("git ls-files %s: %w", pattern, err)
	}
	var files []string
	for f := range strings.SplitSeq(out, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

func readFiles(root, pattern string) (map[string][]byte, error) {
	files, err := gitFiles(root, pattern)
	if err != nil {
		return nil, fmt.Errorf("find %s: %w", pattern, err)
	}
	contents := make(map[string][]byte, len(files))
	for _, f := range files {
		data, err := os.ReadFile(path.Join(root, f))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", f, err)
		}
		contents[f] = data
	}
	return contents, nil
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "git", args...)
	cmd.Dir = dir
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
