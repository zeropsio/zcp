//go:build e2e

// Tests for: InitServiceGit against a live Zerops service.
//
// GLC-1 live verification. Locks the contract that ops.InitServiceGit
// emits a command which, when executed via SSH against a managed runtime
// container, leaves /var/www/.git/ owned by zerops:zerops with the
// DeployGitIdentity values configured. Failure here typically means
// zembed's SSH exec path itself regressed (tracked separately from our
// ZCP-side code) — unit tests already lock the emitted shell shape.
//
// Prerequisites:
//   - ZCP_API_KEY set
//   - VPN to the project active (zcli vpn up <project-id>)
//   - ZCP_E2E_GIT_INIT_SERVICE=<hostname> of a provisioned test service.
//     We do NOT auto-provision here; the target is an opt-in existing
//     service so the test is cheap to re-run and doesn't burn quotas on
//     every CI pass.
//
// Run:
//   export ZCP_E2E_GIT_INIT_SERVICE=probe
//   go test ./e2e/ -tags e2e -count=1 -v -run TestE2E_InitServiceGit -timeout 120s
//
// Cleanup: the test restores the pre-test .git/ state if present, or
// leaves the fresh .git/ it created if the service had none.

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

func TestE2E_InitServiceGit(t *testing.T) {
	hostname := os.Getenv("ZCP_E2E_GIT_INIT_SERVICE")
	if hostname == "" {
		t.Skip("ZCP_E2E_GIT_INIT_SERVICE not set — skipping live git-init verification")
	}
	requireSSH(t, hostname)

	// Snapshot pre-state so we don't leave the service materially changed.
	// If .git/ already existed we leave it; if we created it we leave our
	// creation (it's correct by construction and matches GLC-1's steady
	// state).
	priorOut, _ := sshExec(t, hostname, "test -d /var/www/.git && echo EXISTS || echo MISSING")
	priorHad := strings.Contains(priorOut, "EXISTS")

	// Fresh-init path: remove .git/ so we exercise the cold case.
	if priorHad {
		t.Logf("prior .git/ existed — rotating it out for a fresh-init assertion")
		if out, err := sshExec(t, hostname, "sudo mv /var/www/.git /var/www/.git.e2e-backup"); err != nil {
			t.Fatalf("rotate prior .git: %s (%v)", out, err)
		}
		t.Cleanup(func() {
			// Best-effort: remove test-created .git and restore the backup.
			_, _ = sshExec(t, hostname, "sudo rm -rf /var/www/.git && sudo mv /var/www/.git.e2e-backup /var/www/.git")
		})
	}

	// Run InitServiceGit via the same SSHDeployer the server uses.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ssh := platform.NewSystemSSHDeployer()
	if err := ops.InitServiceGit(ctx, ssh, hostname); err != nil {
		t.Fatalf("InitServiceGit(%s): %v", hostname, err)
	}

	// Assert ownership — the whole point of running this container-side
	// rather than through the SSHFS mount (zembed's SFTP MKDIR ignores
	// authenticated user; SSH exec respects it).
	owner, err := sshExec(t, hostname, "stat -c '%U' /var/www/.git")
	if err != nil {
		t.Fatalf("stat /var/www/.git: %v", err)
	}
	if got := strings.TrimSpace(owner); got != "zerops" {
		t.Errorf("/var/www/.git owner: got %q, want %q", got, "zerops")
	}

	// Assert identity landed in .git/config with the DeployGitIdentity
	// values. These are the values ops.DeployGitIdentity exports and
	// every downstream git commit (buildSSHCommand safety-net or
	// operator running git commit inside the container) will pick up.
	for key, want := range map[string]string{
		"user.email": ops.DeployGitIdentity.Email,
		"user.name":  ops.DeployGitIdentity.Name,
	} {
		out, err := sshExec(t, hostname, "cd /var/www && git config --get "+key)
		if err != nil {
			t.Errorf("git config --get %s: %v", key, err)
			continue
		}
		if got := strings.TrimSpace(out); got != want {
			t.Errorf("git config %s: got %q, want %q", key, got, want)
		}
	}

	// Idempotency: a second call against the same service must succeed
	// without error and not change the on-disk state visibly.
	if err := ops.InitServiceGit(ctx, ssh, hostname); err != nil {
		t.Errorf("InitServiceGit second call: %v", err)
	}
}
