// Tests for: ops/delivery_git.go — every git command a delivery runs against
// HQ ends within its bound, even against an HQ that never accepts it (spec-mate
// §10.10): git has no setting for its connect, so a black-holed HQ would hold
// it for curl's 300 s.
package ops

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// hqGitCommands are the commands of a delivery that reach HQ, by name.
func hqGitCommands(pair string) map[string]string {
	return map[string]string{
		"the Mate's branch":  BuildMateBranchCommand(pair, labBranch),
		"a delivery":         BuildDeliveryCommand(pair, "Add a footer", "", ""),
		"a push's sync":      BuildDeliverySyncCommand(pair, "", ""),
		"taking a change in": BuildTakeChangeInCommand(pair, labChange1),
		"pushing a change":   BuildChangePushCommand(pair, labChange1),
	}
}

// TestHQGit_EveryCommandCarriesTheStallBound: git's own low-speed bound, per
// command and never global config, ends a transfer HQ stops feeding.
func TestHQGit_EveryCommandCarriesTheStallBound(t *testing.T) {
	t.Parallel()
	for name, command := range hqGitCommands("/var/www") {
		if !strings.Contains(command, "-c http.lowSpeedLimit=1 -c http.lowSpeedTime=10") {
			t.Errorf("%s runs git against HQ without the stall bound:\n%s", name, command)
		}
	}
}

// TestHQGit_AnHQThatNeverAcceptsEndsWithinTheBound: against a listener that
// never accepts, each command ends within HQGitBound, says so, and reads as
// HQ unable to serve it.
//
// Non-parallel: it narrows HQGitBound, which every command reads.
func TestHQGit_AnHQThatNeverAcceptsEndsWithinTheBound(t *testing.T) {
	if _, err := exec.LookPath("timeout"); err != nil {
		t.Skip("the bound is `timeout`, which this machine lacks")
	}
	prev := HQGitBound
	HQGitBound = time.Second
	t.Cleanup(func() { HQGitBound = prev })
	listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	pair := mateBranchLab(t, nil)
	runGit(t, pair, "commit", "-q", "--allow-empty", "-m", "zcp init")
	runGit(t, pair, "remote", "set-url", "origin", "https://"+listener.Addr().String()+"/git/app-1/appdev.git")
	for name, command := range hqGitCommands(pair) {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			start := time.Now()
			out, err := exec.CommandContext(ctx, "sh", "-c", command).CombinedOutput()
			if took := time.Since(start); err == nil || took > 6*time.Second {
				t.Fatalf("took %s, err %v; want it to fail within its bound\n%s", took, err, out)
			}
			if words, ok := HQGitNoAnswer(string(out)); !ok || words != "no answer within 1s" {
				t.Errorf("HQGitNoAnswer = %q, %v; want the bound named\n%s", words, ok, out)
			}
			if !GitRemoteUnavailable(string(out)) {
				t.Errorf("an HQ that never answered must read as unable to serve:\n%s", out)
			}
		})
	}
}
