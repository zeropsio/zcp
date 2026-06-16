//go:build e2e

// Live verification of the spec-git-delivery-target §4-§5 primitives
// against a real Zerops runtime container — the credential-helper chain
// that replaced the ephemeral-.netrc pattern, end to end:
//
//  1. session-env credential helper authenticates a real ls-remote
//     (BuildGitSessionAuthProbeCommand) — proves env-store → fresh-SSH-
//     session → helper → GitHub works with the SERVICE-scope secret.
//  2. inline candidate-token probe (BuildGitAuthProbeCommand) with the
//     live token read back from the session env — the rotation probe
//     path.
//  3. reconstruction guard no-ops on a PRESENT repo
//     (BuildGitReconstructCommand `test ! -d .git` — non-destructive
//     by construction; asserted by .git surviving + HEAD unchanged).
//  4. tag listing (release-act suggestion input) returns without error.
//
// (The FP-3 visibility-probe case was retired with pipeline-first launch —
// plans/archive/launch-pipeline-first-2026-06-11.md P1.)
//
// Prerequisites:
//   - ZCP_API_KEY + VPN to the eval project
//   - ZCP_E2E_GIT_DELIVERY_SERVICE=<hostname> of a service that has
//     git-push configured (GIT_TOKEN service secret + origin set), e.g.
//     the eval `weather` service wired to a public repo.
//   - ZCP_E2E_GIT_DELIVERY_REMOTE=<https remote of that service>
//
// Run:
//
//	export ZCP_E2E_GIT_DELIVERY_SERVICE=weather
//	export ZCP_E2E_GIT_DELIVERY_REMOTE=https://github.com/krls2020/xy3
//	go test ./e2e/ -tags e2e -count=1 -v -run TestE2E_GitDeliveryPrimitives -timeout 180s
package e2e_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

func TestE2E_GitDeliveryPrimitives(t *testing.T) {
	hostname := os.Getenv("ZCP_E2E_GIT_DELIVERY_SERVICE")
	remote := os.Getenv("ZCP_E2E_GIT_DELIVERY_REMOTE")
	if hostname == "" || remote == "" {
		t.Skip("ZCP_E2E_GIT_DELIVERY_SERVICE / _REMOTE not set — skipping live git-delivery verification")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	ssh := platform.NewSystemSSHDeployer()

	// Pre-state: HEAD before any command (reconstruction no-op assert).
	headBefore, err := ssh.ExecSSH(ctx, hostname, "cd /var/www && git rev-parse HEAD 2>/dev/null || echo NONE")
	if err != nil {
		t.Fatalf("pre-state read: %v", err)
	}

	t.Run("session helper authenticates", func(t *testing.T) {
		out, err := ssh.ExecSSH(ctx, hostname, ops.BuildGitSessionAuthProbeCommand(remote))
		if err != nil {
			t.Fatalf("session-env credential helper failed against %s: %v\n%s", remote, err, out)
		}
		if !strings.Contains(string(out), "HEAD") {
			t.Errorf("expected a HEAD ref line, got: %s", out)
		}
	})

	t.Run("inline probe with live token (rotation path)", func(t *testing.T) {
		// Read the live token from the session env (never echoed into
		// the test log) and feed it back through the candidate-token
		// probe — the same shape git-push-setup rotation runs.
		tok, err := ssh.ExecSSH(ctx, hostname, `printf %s "$GIT_TOKEN"`)
		if err != nil || len(strings.TrimSpace(string(tok))) == 0 {
			t.Skipf("GIT_TOKEN not present in session env (err=%v) — configure git-push-setup first", err)
		}
		out, err := ssh.ExecSSH(ctx, hostname, ops.BuildGitAuthProbeCommand(remote, strings.TrimSpace(string(tok))))
		if err != nil {
			t.Fatalf("inline candidate-token probe failed: %v\n%s", err, out)
		}
	})

	// The visibility-probe case was deleted with the FP-3 gate: pipeline-first
	// launch (plans/archive/launch-pipeline-first-2026-06-11.md P1) removed
	// buildFromGit from the production import, so repo visibility is no
	// longer a launch concern and ops.BuildGitUnauthenticatedLsRemoteCommand
	// no longer exists.

	t.Run("reconstruction no-ops on present repo", func(t *testing.T) {
		if _, err := ssh.ExecSSH(ctx, hostname, ops.BuildGitReconstructCommand("/var/www", remote)); err != nil {
			t.Fatalf("reconstruction guard run failed: %v", err)
		}
		headAfter, err := ssh.ExecSSH(ctx, hostname, "cd /var/www && git rev-parse HEAD 2>/dev/null || echo NONE")
		if err != nil {
			t.Fatalf("post-state read: %v", err)
		}
		if strings.TrimSpace(string(headBefore)) != strings.TrimSpace(string(headAfter)) {
			t.Errorf("reconstruction must no-op on a present repo: HEAD %s → %s", headBefore, headAfter)
		}
	})

	t.Run("tag list for release suggestion", func(t *testing.T) {
		out, err := ssh.ExecSSH(ctx, hostname, "cd /var/www && "+ops.BuildGitTagListCommand(remote))
		if err != nil {
			t.Fatalf("tag list failed: %v\n%s", err, out)
		}
		t.Logf("remote v* tags: %q", strings.TrimSpace(string(out)))
	})
}
