package ops

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
)

const defaultWorkingDir = "/var/www"

// GitIdentity holds user name and email for git commits.
type GitIdentity struct {
	Name  string
	Email string
}

// DeployGitIdentity is the hardcoded identity for internal deploy commits on containers.
// These are infrastructure commits (not user-facing), so a fixed identity prevents
// missing git config errors and keeps deploy history consistent.
var DeployGitIdentity = GitIdentity{Name: "Zerops Agent", Email: "agent@zerops.io"}

// shellQuote wraps a string in POSIX single quotes, escaping embedded single quotes.
// This prevents shell injection from untrusted input (e.g., user names, emails).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// DeploySSH deploys code to a Zerops service via SSH.
//
// Auto-inference:
//   - targetService is required
//   - sourceService == "" → auto-inferred as targetService (self-deploy)
//   - includeGit is derived from the source/target pair: true for self-deploy
//     (the service pushes its own code, .git must stay), false for cross-deploy
//     (dev→stage would otherwise carry the dev container's .git across).
func DeploySSH(
	ctx context.Context,
	client platform.Client,
	projectID string,
	sshDeployer SSHDeployer,
	authInfo auth.Info,
	sourceService string,
	targetService string,
	setup string,
	workingDir string,
) (*DeployResult, error) {
	if sshDeployer == nil {
		return nil, platform.NewPlatformError(
			platform.ErrNotImplemented,
			"SSH deployer not configured",
			"SSH deploy requires a running Zerops container with SSH access",
		)
	}
	if targetService == "" {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"targetService is required",
			"Provide targetService for deploy. Omit sourceService for self-deploy (auto-inferred).",
		)
	}
	if sourceService == "" {
		sourceService = targetService // auto-infer self-deploy
	}
	includeGit := sourceService == targetService

	return deploySSH(ctx, client, projectID, sshDeployer, authInfo,
		sourceService, targetService, setup, workingDir, includeGit)
}

func deploySSH(
	ctx context.Context,
	client platform.Client,
	projectID string,
	sshDeployer SSHDeployer,
	authInfo auth.Info,
	sourceService string,
	targetService string,
	setup string,
	workingDir string,
	includeGit bool,
) (*DeployResult, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	source, err := FindService(services, sourceService)
	if err != nil {
		return nil, err
	}

	target, err := FindService(services, targetService)
	if err != nil {
		return nil, err
	}

	if workingDir == "" {
		workingDir = defaultWorkingDir
	}
	// Reject mount-style paths — workingDir runs INSIDE the container where
	// /var/www/{hostname} doesn't exist. The correct container path is /var/www.
	if workingDir != defaultWorkingDir && strings.HasPrefix(workingDir, defaultWorkingDir+"/") {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("workingDir %q looks like a local SSHFS mount path, not a container path. Inside the container, code lives at /var/www", workingDir),
			"Use workingDir=\"/var/www\" or omit workingDir (defaults to /var/www). The mount path /var/www/{hostname} is only valid on the local machine.",
		)
	}

	// Pre-deploy validation: read zerops.yaml from SSHFS mount (local filesystem).
	// Mount path: /var/www/{sourceService}/ maps to remote /var/www/
	setupName := setup
	if setupName == "" {
		setupName = targetService
	}
	var warnings []string
	mountPath := filepath.Join("/var/www", sourceService)
	serviceType := target.ServiceStackTypeInfo.ServiceStackTypeVersionName
	class := ClassifyDeploy(sourceService, targetService)
	if _, statErr := os.Stat(mountPath); statErr == nil {
		var vErr error
		warnings, vErr = ValidateZeropsYml(mountPath, setupName, serviceType, class)
		// DM-2 violation is a hard error — deploy aborts, warnings (if
		// any) travel with the error for visibility but the caller
		// won't issue a push.
		if vErr != nil {
			return nil, vErr
		}
		// Pre-deploy API validation: Zerops checks the full zerops.yaml
		// (field/syntax/version) server-side before we waste a build
		// cycle on a YAML the platform will reject. Any failure —
		// validation, transport, auth — aborts deploy.
		if err := RunPreDeployValidation(ctx, client, target, setupName, mountPath); err != nil {
			return nil, err
		}
	}

	cmd := buildSSHCommand(authInfo, target.ID, workingDir, setup, includeGit)

	output, err := sshDeployer.ExecSSH(ctx, source.Name, cmd)
	if err != nil {
		if isSSHBuildTriggered(string(output)) {
			// SSH connection dropped after successful zcli push (common exit 255).
			// Build was submitted — let pollDeployBuild take over.
			return &DeployResult{
				Status:            "BUILD_TRIGGERED",
				Mode:              "ssh",
				SourceService:     sourceService,
				TargetService:     targetService,
				TargetServiceID:   target.ID,
				TargetServiceType: target.ServiceStackTypeInfo.ServiceStackTypeVersionName,
				Message:           fmt.Sprintf("Build triggered from %s to %s (SSH session closed after push)", sourceService, targetService),
				MonitorHint:       "Build runs asynchronously. Poll zerops_events for build/deploy FINISHED status.",
				Warnings:          warnings,
			}, nil
		}
		return nil, classifySSHError(err, sourceService, targetService)
	}

	return &DeployResult{
		Status:            "BUILD_TRIGGERED",
		Mode:              "ssh",
		SourceService:     sourceService,
		TargetService:     targetService,
		TargetServiceID:   target.ID,
		TargetServiceType: target.ServiceStackTypeInfo.ServiceStackTypeVersionName,
		Message:           fmt.Sprintf("Build triggered from %s to %s via SSH", sourceService, targetService),
		MonitorHint:       "Build runs asynchronously. Poll zerops_events for build/deploy FINISHED status.",
		Warnings:          warnings,
	}, nil
}

func buildSSHCommand(authInfo auth.Info, targetServiceID, workingDir, setup string, includeGit bool) string {
	parts := make([]string, 0, 2)

	// Login to zcli on the remote host.
	loginCmd := fmt.Sprintf("zcli login -- %s", shellQuote(authInfo.Token))
	parts = append(parts, loginCmd)

	// .git lifecycle (GLC-2 / GLC-3). Three cases must reach a state where
	// `git commit` succeeds with the canonical Zerops Agent identity:
	//
	//   1. Bootstrap-mounted service: InitServiceGit ran post-mount, .git/
	//      exists with identity already configured. gitInit no-ops, gitConfig
	//      re-asserts the same values (idempotent).
	//   2. Cold path (migration / `sudo rm -rf /var/www/.git`): no .git/.
	//      gitInit creates it, gitConfig writes identity.
	//   3. Service provisioned via buildFromGit (or any flow where /var/www/
	//      came from an upstream git clone): .git/ exists but its config
	//      carries the cloning user's identity (or none at all). gitInit
	//      no-ops, gitConfig OVERWRITES with the deploy identity. Without
	//      this overwrite, services where the upstream repo never set
	//      user.email/user.name fail with `fatal: unable to auto-detect
	//      email address` on the deploy commit (B13).
	//
	// gitConfig must therefore live OUTSIDE the OR branch — the same shape
	// InitServiceGit uses — so case (3) actually runs it. The previous
	// "atomic safety-net" form (config inside OR) handled (1) and (2) but
	// silently broke (3); the buildFromGit-deploy regression surfaced in
	// Phase 1.5 eval `develop-pivot-auto-close`. Identity comes from the
	// DeployGitIdentity package constant — single source of truth shared
	// with InitServiceGit, no shell-injection surface.
	email := shellQuote(DeployGitIdentity.Email)
	name := shellQuote(DeployGitIdentity.Name)
	gitInit := "(test -d .git || git init -q -b main)"
	gitConfig := fmt.Sprintf("git config user.email %s && git config user.name %s", email, name)

	// Stage + commit. Skip commit if nothing changed (diff-index quiet).
	// On fresh init after gitInit, HEAD doesn't exist → diff-index fails
	// → || fires → commit runs.
	gitCommit := "git add -A && (git diff-index --quiet HEAD 2>/dev/null || git commit -q -m 'deploy')"

	// Push from workingDir with git handling.
	pushArgs := fmt.Sprintf("zcli push --service-id %s", targetServiceID)
	if setup != "" {
		pushArgs += " --setup " + setup
	}
	if includeGit {
		pushArgs += " -g"
	}

	pushCmd := fmt.Sprintf("cd %s && %s && %s && %s && %s",
		workingDir, gitInit, gitConfig, gitCommit, pushArgs)
	parts = append(parts, pushCmd)

	return strings.Join(parts, " && ")
}
