package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
)

// InitServiceGit ensures /var/www/.git/ exists on the target service with
// the deploy identity configured. Runs entirely container-side via SSH exec
// — SSH exec mkdir respects the authenticated user (zembed's SFTP MKDIR
// does not, which poisons mount-side git init).
//
// Called once per managed runtime service at bootstrap/adopt post-mount
// (internal/tools/workflow_bootstrap.go::autoMountTargets). Idempotent:
// existing .git/ is preserved by the test-d guard; git config overwrites
// already-matching values. Safe to re-run on the same service.
//
// Identity source of truth: ops.DeployGitIdentity (agent@zerops.io).
// buildSSHCommand's safety-net fallback and InitServiceGit read the same
// constant so bootstrap and deploy paths agree on what's written into
// .git/config.
func InitServiceGit(ctx context.Context, ssh SSHDeployer, hostname string) error {
	if hostname == "" {
		return platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"hostname is required",
			"InitServiceGit requires a target service hostname",
		)
	}
	if ssh == nil {
		return platform.NewPlatformError(
			platform.ErrNotImplemented,
			"SSH deployer not configured",
			"InitServiceGit requires a Zerops container with SSH access",
		)
	}

	email := shellQuote(DeployGitIdentity.Email)
	name := shellQuote(DeployGitIdentity.Name)
	cmd := fmt.Sprintf(
		"cd /var/www && (test -d .git || git init -q -b main) && git config user.email %s && git config user.name %s",
		email, name,
	)

	if _, err := ssh.ExecSSH(ctx, hostname, cmd); err != nil {
		return fmt.Errorf("init git on %s: %w", hostname, err)
	}
	return nil
}
