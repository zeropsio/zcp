package ops

import (
	"path/filepath"
	"strings"
	"testing"
)

// non-parallel: this package's command tests narrow HQGitBound.
func TestBuildBaseBranchCommand_FirstFetch_DefersBlobsWithFullAncestry(t *testing.T) {
	for _, tc := range []struct {
		name    string
		command func(string) string
		partial bool
	}{
		{"mate", BuildBaseBranchCommand, true},
		{"plain project", func(dir string) string {
			return "printf 'project-key\n' | sh -c " + shellQuote(BuildProjectRepositoryCommand(dir, runGit(t, dir, "remote", "get-url", "origin"), ""))
		}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var historicalCommit, historicalBlob string
			pair := baseLabOn(t, func(t *testing.T, dir string) {
				t.Helper()
				writeLabFile(t, filepath.Join(dir, "app.txt"), "historical contents\n")
				commitAll(t, dir, "historical version")
				historicalCommit = runGit(t, dir, "rev-parse", "HEAD")
				historicalBlob = runGit(t, dir, "rev-parse", "HEAD:app.txt")
				writeLabFile(t, filepath.Join(dir, "app.txt"), "current contents\n")
				commitAll(t, dir, "current version")
			}, zcpInitMarker)
			remote := runGit(t, pair, "remote", "get-url", "origin")
			runGit(t, remote, "config", "uploadpack.allowFilter", "true")

			runShell(t, tc.command(pair))

			missing := runGit(t, pair, "rev-list", "--objects", "HEAD", "--missing=print")
			if got := strings.Contains(missing, "?"+historicalBlob); got != tc.partial {
				t.Fatalf("historical blob deferred = %v, want %v", got, tc.partial)
			}
			if got := runGit(t, pair, "rev-list", "--count", "HEAD"); got != "2" {
				t.Errorf("ancestry count = %s, want 2", got)
			}
			if got := runGit(t, pair, "merge-base", historicalCommit, "HEAD"); got != historicalCommit {
				t.Errorf("merge-base = %s, want historical commit %s", got, historicalCommit)
			}
			if got := runGit(t, pair, "rev-parse", "--is-shallow-repository"); got != "false" {
				t.Errorf("shallow = %s, want false", got)
			}
			if got := runGit(t, pair, "show", "HEAD:app.txt"); got != "current contents" {
				t.Errorf("current contents = %q", got)
			}
			if got := runGit(t, pair, "show", historicalCommit+":app.txt"); got != "historical contents" {
				t.Errorf("lazy historical contents = %q", got)
			}
			if got := runGit(t, pair, "status", "--porcelain"); got != "" {
				t.Errorf("checkout dirty: %s", got)
			}
		})
	}
}
