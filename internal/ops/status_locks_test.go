package ops

import (
	"context"
	"strings"
	"testing"
)

// recordingGitRunner records the scripts a git.SSHRunner-shaped caller runs.
type recordingGitRunner struct{ scripts []string }

func (r *recordingGitRunner) Run(_ context.Context, _, script string) (string, string, error) {
	r.scripts = append(r.scripts, script)
	return "", "", nil
}

// TestReadOnlyStatusProbes_RunWithOptionalLocksOff pins that the read-only
// git status reads zcp runs on a dev service — the deploy's uncommitted-files
// warning and the crew copies' dirty check — never take the index lock: a
// plain git status refreshes and rewrites .git/index and can collide with the
// user's own git (the probes' shared rule, as the release guard's).
func TestReadOnlyStatusProbes_RunWithOptionalLocksOff(t *testing.T) {
	t.Parallel()
	if strings.Count(crewDirtyCmd, "git ") != strings.Count(crewDirtyCmd, "GIT_OPTIONAL_LOCKS=0 git ") {
		t.Errorf("the crew dirty check runs git without GIT_OPTIONAL_LOCKS=0: %s", crewDirtyCmd)
	}
	r := &recordingGitRunner{}
	_ = uncommittedIn(context.Background(), r, "/var/www", "appdev")
	if len(r.scripts) != 1 || !strings.Contains(r.scripts[0], "GIT_OPTIONAL_LOCKS=0 git status") {
		t.Errorf("the uncommitted-files read = %q, want git status with GIT_OPTIONAL_LOCKS=0", r.scripts)
	}
}
