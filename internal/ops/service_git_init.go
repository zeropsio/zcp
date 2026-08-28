package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
)

// InitServiceGit ensures /var/www/.git/ exists on the target service, with
// identity filled (never stomped) and HEAD reachable. Runs entirely
// container-side via SSH exec — SSH exec mkdir respects the authenticated
// user (zembed's SFTP MKDIR does not, which poisons mount-side git init).
//
// Called once per managed runtime service at bootstrap/adopt post-mount
// (internal/tools/workflow_bootstrap.go::autoMountTargets). Idempotent by
// construction: the test-d guard preserves an existing .git/, identity is
// set-if-absent, and the HEAD guarantee no-ops once HEAD exists. Safe to
// re-run on the same service.
//
// Composes ops.GitEnsureRepoHeadCommand — the single owner shared with
// buildSSHCommand's safety-net and git-push-setup's pre-probe ensure, so
// bootstrap and deploy paths can't drift on what "commit-ready" means.
//
// serviceType (e.g. "nodejs@22") is the target's raw runtime type — the
// caller (autoMountTargets) has it on hand from the same bootstrap plan
// entry as hostname — and selects the language-aware .gitignore block
// GitEnsureRepoHeadCommand backfills. Pass "" when unknown; the backfill
// still writes the base hygiene lines.
func InitServiceGit(ctx context.Context, ssh SSHDeployer, hostname, serviceType string) error {
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

	cmd := GitEnsureRepoHeadCommand(defaultWorkingDir, serviceType)

	if _, err := ssh.ExecSSH(ctx, hostname, cmd); err != nil {
		return fmt.Errorf("init git on %s: %w", hostname, err)
	}
	return nil
}
