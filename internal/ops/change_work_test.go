// Tests for: ops/change_work.go — what a change holds beyond `main`, read
// from the pair's checkout for a bare change's title: the newest commit the
// Mate wrote, never one zcp made itself, else the files that differ from
// `main`. Run against the delivery lab's bare "HQ" (delivery_git_test.go).
package ops

import (
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

func TestReadChangeWork(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	tests := []struct {
		name string
		// seed runs in the pair before its branch is cut from main; nil
		// for none.
		seed func(t *testing.T, pair string)
		// work runs in the pair once its branch is cut from main, and
		// answers the landed heads zcp knows of.
		work func(t *testing.T, pair string) []string
		want ChangeWork
	}{
		{
			name: "the Mate's newest commit names it, past the delivery's own commit after it",
			work: func(t *testing.T, pair string) []string {
				t.Helper()
				writeLabFile(t, filepath.Join(pair, "api.js"), "the api\n")
				commitAll(t, pair, "Add the product API")
				writeLabFile(t, filepath.Join(pair, "README.md"), "notes\n")
				runShell(t, BuildDeliveryCommand(pair, "Build the whole shop, backend and storefront", "", ""))
				return nil
			},
			want: ChangeWork{Subject: "Add the product API"},
		},
		{
			name: "only zcp's own commit: what differs from main",
			work: func(t *testing.T, pair string) []string {
				t.Helper()
				writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
				writeLabFile(t, filepath.Join(pair, ".gitignore"), "node_modules/\n")
				writeLabFile(t, filepath.Join(pair, "src", "list.js"), "the list\n")
				runShell(t, BuildDeliveryCommand(pair, "Build a todo app", "", ""))
				return nil
			},
			want: ChangeWork{Count: 3, Files: []string{".gitignore", "index.js"}, Kinds: "A"},
		},
		{
			name: "a merge of main is not the Mate's, nor is main's own history",
			work: func(t *testing.T, pair string) []string {
				t.Helper()
				writeLabFile(t, filepath.Join(pair, "api.js"), "the api\n")
				commitAll(t, pair, "Add the product API")
				landOnRemoteMain(t, pair, "colleague.js", "Add a colleague's page")
				runShell(t, BuildDeliveryCommand(pair, "Build the shop", "", ""))
				return nil
			},
			want: ChangeWork{Subject: "Add the product API"},
		},
		{
			name: "nothing beyond main",
			work: func(t *testing.T, pair string) []string {
				t.Helper()
				runShell(t, BuildDeliveryCommand(pair, "Build the shop", "", ""))
				return nil
			},
			want: ChangeWork{},
		},
		{
			// The stand-up briefing asks for a baseline commit before any
			// change, in every repository: never a title.
			name: "the baseline commit, as the briefing writes it, is not the Mate's work",
			work: func(t *testing.T, pair string) []string {
				t.Helper()
				writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
				runGit(t, pair, "add", "-A")
				runGit(t, pair, "commit", "-q", "-m", "baseline commit", "-m", "Zcp-Commit: baseline")
				writeLabFile(t, filepath.Join(pair, "list.js"), "the list\n")
				runShell(t, BuildDeliveryCommand(pair, "Build a todo app", "", ""))
				return nil
			},
			want: ChangeWork{Count: 2, Files: []string{"index.js", "list.js"}, Kinds: "A"},
		},
		{
			name: "an older checkout's baseline under its own words, on its zcp init, before the join",
			seed: func(t *testing.T, pair string) {
				t.Helper()
				zcpInitMarker(t, pair)
				writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
				commitAll(t, pair, "Initial scaffold")
			},
			work: func(t *testing.T, pair string) []string {
				t.Helper()
				writeLabFile(t, filepath.Join(pair, "list.js"), "the list\n")
				runShell(t, BuildDeliveryCommand(pair, "Build a todo app", "", ""))
				return nil
			},
			want: ChangeWork{Count: 2, Files: []string{"index.js", "list.js"}, Kinds: "A"},
		},
		{
			name: "an older checkout's baseline, under the briefing's words, on HQ's seed",
			work: func(t *testing.T, pair string) []string {
				t.Helper()
				writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
				commitAll(t, pair, "baseline commit")
				runShell(t, BuildDeliveryCommand(pair, "Build a todo app", "", ""))
				return nil
			},
			want: ChangeWork{Count: 1, Files: []string{"index.js"}, Kinds: "A"},
		},
		{
			name: "an older checkout holds a change that landed: zcp knows its head",
			work: func(t *testing.T, pair string) []string {
				t.Helper()
				return []string{squashLandedByMerge(t, pair)}
			},
			want: ChangeWork{Count: 1, Files: []string{"README.md"}, Kinds: "A"},
		},
		{
			name: "an older checkout holds a change that landed: its history is kept",
			work: func(t *testing.T, pair string) []string {
				t.Helper()
				head := squashLandedByMerge(t, pair)
				runGit(t, pair, "update-ref", "refs/zcp/landed/"+runGit(t, pair, "rev-parse", "origin/main"), head)
				return []string{"0123456789abcdef0123456789abcdef01234567"}
			},
			want: ChangeWork{Count: 1, Files: []string{"README.md"}, Kinds: "A"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pair := mateBranchLab(t, tt.seed)
			runShell(t, BuildMateBranchCommand(pair, labBranch))
			landed := tt.work(t, pair)
			out, err := exec.CommandContext(t.Context(), "sh", "-c", BuildChangeWorkCommand(pair, landed)).CombinedOutput() //nolint:gosec // G204: the command under test
			if err != nil {
				t.Fatalf("read the change's work: %v\n%s", err, out)
			}
			got := ReadChangeWork(string(out))
			if got.Subject != tt.want.Subject {
				t.Errorf("subject = %q, want %q\n%s", got.Subject, tt.want.Subject, out)
			}
			if tt.want.Subject == "" && (got.Count != tt.want.Count || !slices.Equal(got.Files, tt.want.Files) || got.Kinds != tt.want.Kinds) {
				t.Errorf("ReadChangeWork = %+v, want %+v\n%s", got, tt.want, out)
			}
		})
	}
}

// squashLandedByMerge is a change landed the way it did before a delivery
// started its next change from a fresh base: the Mate's commit was squashed
// onto main, and the pair took that main in by an ordinary merge, its own
// commit still beyond main. More work follows, committed by the delivery.
// It answers the head that landed.
func squashLandedByMerge(t *testing.T, pair string) string {
	t.Helper()
	writeLabFile(t, filepath.Join(pair, "api.js"), "the api\n")
	commitAll(t, pair, "Add the product API")
	head := runGit(t, pair, "rev-parse", "HEAD")
	runGit(t, pair, "fetch", "-q", "origin")
	squash := runGit(t, pair, "commit-tree", "HEAD^{tree}", "-p", "origin/main", "-m", "Add the product API (#1)")
	runGit(t, pair, "push", "-q", "origin", squash+":refs/heads/main")
	writeLabFile(t, filepath.Join(pair, "README.md"), "notes\n")
	runShell(t, BuildDeliveryCommand(pair, "Build the shop", "", ""))
	return head
}

// TestReadChangeWork_KindsOfDifference: the kinds of difference from main
// are each named once, whatever order git lists them in.
func TestReadChangeWork_KindsOfDifference(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("exercises a real git repository")
	}
	pair := mateBranchLabOn(t, func(t *testing.T, dir string) {
		t.Helper()
		writeLabFile(t, filepath.Join(dir, "a.js"), "a\n")
		writeLabFile(t, filepath.Join(dir, "b.js"), "b\n")
		commitAll(t, dir, "Seed main")
	}, nil)
	runShell(t, BuildMateBranchCommand(pair, labBranch))
	writeLabFile(t, filepath.Join(pair, "a.js"), "a, changed\n")
	runGit(t, pair, "rm", "-q", "b.js")
	writeLabFile(t, filepath.Join(pair, "c.js"), "c\n")
	runShell(t, BuildDeliveryCommand(pair, "Rework the files", "", ""))

	out, err := exec.CommandContext(t.Context(), "sh", "-c", BuildChangeWorkCommand(pair, nil)).CombinedOutput() //nolint:gosec // G204: the command under test
	if err != nil {
		t.Fatalf("read the change's work: %v\n%s", err, out)
	}
	got := ReadChangeWork(string(out))
	kinds := []rune(got.Kinds)
	slices.Sort(kinds)
	if string(kinds) != "ADM" || got.Count != 3 || got.Subject != "" {
		t.Errorf("ReadChangeWork = %+v, want three files of kinds A, D and M and no subject\n%s", got, out)
	}
}

// landOnRemoteMain lands a colleague's commit adding file on the remote's
// main, behind the pair's back.
func landOnRemoteMain(t *testing.T, pair, file, message string) {
	t.Helper()
	remote := filepath.Join(filepath.Dir(pair), "remote.git")
	clone := filepath.Join(t.TempDir(), "colleague")
	runGit(t, filepath.Dir(clone), "clone", "-q", remote, clone)
	runGit(t, clone, "config", "user.email", "colleague@example.invalid")
	runGit(t, clone, "config", "user.name", "colleague")
	writeLabFile(t, filepath.Join(clone, file), "theirs\n")
	commitAll(t, clone, message)
	runGit(t, clone, "push", "-q", "origin", "main")
}
