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
		// work runs in the pair once its branch is cut from main.
		work func(t *testing.T, pair string)
		want ChangeWork
	}{
		{
			name: "the Mate's newest commit names it, past the delivery's own commit after it",
			work: func(t *testing.T, pair string) {
				t.Helper()
				writeLabFile(t, filepath.Join(pair, "api.js"), "the api\n")
				commitAll(t, pair, "Add the product API")
				writeLabFile(t, filepath.Join(pair, "README.md"), "notes\n")
				runShell(t, BuildDeliveryCommand(pair, "Build the whole shop, backend and storefront", "", ""))
			},
			want: ChangeWork{Subject: "Add the product API"},
		},
		{
			name: "only zcp's own commit: what differs from main",
			work: func(t *testing.T, pair string) {
				t.Helper()
				writeLabFile(t, filepath.Join(pair, "index.js"), "the app\n")
				writeLabFile(t, filepath.Join(pair, ".gitignore"), "node_modules/\n")
				writeLabFile(t, filepath.Join(pair, "src", "list.js"), "the list\n")
				runShell(t, BuildDeliveryCommand(pair, "Build a todo app", "", ""))
			},
			want: ChangeWork{Count: 3, Files: []string{".gitignore", "index.js"}, Kinds: "A"},
		},
		{
			name: "a merge of main is not the Mate's, nor is main's own history",
			work: func(t *testing.T, pair string) {
				t.Helper()
				writeLabFile(t, filepath.Join(pair, "api.js"), "the api\n")
				commitAll(t, pair, "Add the product API")
				landOnRemoteMain(t, pair, "colleague.js", "Add a colleague's page")
				runShell(t, BuildDeliveryCommand(pair, "Build the shop", "", ""))
			},
			want: ChangeWork{Subject: "Add the product API"},
		},
		{
			name: "nothing beyond main",
			work: func(t *testing.T, pair string) {
				t.Helper()
				runShell(t, BuildDeliveryCommand(pair, "Build the shop", "", ""))
			},
			want: ChangeWork{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pair := mateBranchLab(t, nil)
			runShell(t, BuildMateBranchCommand(pair, labBranch))
			tt.work(t, pair)
			out, err := exec.CommandContext(t.Context(), "sh", "-c", BuildChangeWorkCommand(pair)).CombinedOutput() //nolint:gosec // G204: the command under test
			if err != nil {
				t.Fatalf("read the change's work: %v\n%s", err, out)
			}
			got := ReadChangeWork(string(out))
			if got.Subject != tt.want.Subject || got.Count != tt.want.Count || !slices.Equal(got.Files, tt.want.Files) || got.Kinds != tt.want.Kinds {
				t.Errorf("ReadChangeWork = %+v, want %+v\n%s", got, tt.want, out)
			}
		})
	}
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

	out, err := exec.CommandContext(t.Context(), "sh", "-c", BuildChangeWorkCommand(pair)).CombinedOutput() //nolint:gosec // G204: the command under test
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
