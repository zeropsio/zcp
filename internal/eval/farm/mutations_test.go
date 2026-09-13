package farm

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// mutationsDir is the on-disk home of the mutation batch (docs/spec-eval-farm.md
// §3.3, docs/spec-scenarios.md §9.4): one small `git apply`-able patch per
// oracle-family regression the farm gate must catch.
func mutationsDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(gatesetRepoRoot(t), "eval", "farm", "mutations")
}

// mutationPatches lists eval/farm/mutations/*.patch, failing loudly (never
// vacuously) when the glob is empty — an empty mutation batch silently
// stops proving the farm gate catches anything.
func mutationPatches(t *testing.T) []string {
	t.Helper()
	dir := mutationsDir(t)
	patches, err := filepath.Glob(filepath.Join(dir, "*.patch"))
	if err != nil {
		t.Fatalf("glob %s/*.patch: %v", dir, err)
	}
	if len(patches) == 0 {
		t.Fatalf("zero patches found under %s — the mutation batch has nothing to apply", dir)
	}
	sort.Strings(patches)
	return patches
}

// TestMutationPatches_ApplyCleanly pins that every eval/farm/mutations/*.patch
// applies cleanly against the current tree. A batch operator (spec-eval-farm.md
// §3.1 maintainer gate) builds the mutated candidate straight from these
// patches (docs/spec-scenarios.md §9.4) — a patch that has drifted from the
// tree fails silently until an operator burns a whole gate batch discovering
// it, so this stays a fast local check instead.
func TestMutationPatches_ApplyCleanly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	root := gatesetRepoRoot(t)
	for _, patch := range mutationPatches(t) {
		t.Run(filepath.Base(patch), func(t *testing.T) {
			cmd := exec.CommandContext(context.Background(), "git", "apply", "--check", patch)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("git apply --check %s: %v\n%s", patch, err, out)
			}
		})
	}
}

// mutationsReadmeFileTable maps a patch basename (as it appears in a
// backtick-quoted table cell, e.g. "`01-env-set-no-restart.patch`") to the
// backtick-quoted file paths in that row's last cell — the README's
// declared write-set for that patch (eval/farm/mutations/README.md).
func mutationsReadmeFileTable(t *testing.T) map[string][]string {
	t.Helper()
	readmePath := filepath.Join(mutationsDir(t), "README.md")
	data, err := os.ReadFile(readmePath)
	if err != nil {
		t.Fatalf("read %s: %v", readmePath, err)
	}
	backtick := regexp.MustCompile("`([^`]+)`")
	table := map[string][]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(trimmed, "|"), "|")
		if len(cells) < 2 {
			continue
		}
		first := strings.TrimSpace(cells[0])
		m := backtick.FindStringSubmatch(first)
		if m == nil || !strings.HasSuffix(m[1], ".patch") {
			continue
		}
		patchName := m[1]
		last := cells[len(cells)-1]
		matches := backtick.FindAllStringSubmatch(last, -1)
		files := make([]string, 0, len(matches))
		for _, fm := range matches {
			files = append(files, fm[1])
		}
		table[patchName] = files
	}
	return table
}

// patchTouchedFiles extracts the "+++ b/<path>" target paths from a unified
// diff, in appearance order.
func patchTouchedFiles(t *testing.T, patch string) []string {
	t.Helper()
	data, err := os.ReadFile(patch)
	if err != nil {
		t.Fatalf("read %s: %v", patch, err)
	}
	var files []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "+++ b/"); ok {
			files = append(files, rest)
		}
	}
	return files
}

// TestMutationPatches_TouchOnlyDeclaredFiles pins that every patch's `+++ b/`
// target paths equal — exactly, no more, no fewer — the file list the
// mutations README declares for that patch. A patch that silently grows a
// second touched file changes the isolation claim ("every other gate cell
// must still pass") without anyone reviewing it.
func TestMutationPatches_TouchOnlyDeclaredFiles(t *testing.T) {
	declared := mutationsReadmeFileTable(t)
	for _, patch := range mutationPatches(t) {
		name := filepath.Base(patch)
		t.Run(name, func(t *testing.T) {
			wantFiles, ok := declared[name]
			if !ok {
				t.Fatalf("mutations/README.md has no table row for %s", name)
			}
			gotFiles := patchTouchedFiles(t, patch)
			want := append([]string(nil), wantFiles...)
			got := append([]string(nil), gotFiles...)
			sort.Strings(want)
			sort.Strings(got)
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("%s touches %v, README declares %v", name, gotFiles, wantFiles)
			}
		})
	}
}
