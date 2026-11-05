// Tests for: ops/hq_base_branch.go — the git zcp runs against HQ ends within
// its bound, even against an HQ that never accepts it (spec-mate
// §10.10): git has no setting for its connect, so a black-holed HQ would hold
// it for curl's 300 s.
package ops

import (
	"strings"
	"testing"
)

// TestHQGit_EveryCommandCarriesTheStallBound: git's own low-speed bound, per
// command and never global config, ends a transfer HQ stops feeding.
func TestHQGit_EveryCommandCarriesTheStallBound(t *testing.T) {
	t.Parallel()
	if command := BuildBaseBranchCommand("/var/www"); !strings.Contains(command, "-c http.lowSpeedLimit=1 -c http.lowSpeedTime=10") {
		t.Errorf("the base branch step runs git against HQ without the stall bound:\n%s", command)
	}
}
