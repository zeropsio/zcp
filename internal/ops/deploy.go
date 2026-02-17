package ops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
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

// sanitizeQuote strips single quotes from a string to prevent shell injection.
func sanitizeQuote(s string) string {
	return strings.ReplaceAll(s, "'", "")
}

// DeployResult contains the outcome of a deploy operation.
type DeployResult struct {
	Status          string `json:"status"`
	Mode            string `json:"mode"` // "ssh" or "local"
	SourceService   string `json:"sourceService,omitempty"`
	TargetService   string `json:"targetService"`
	TargetServiceID string `json:"targetServiceId"`
	Message         string `json:"message"`
	MonitorHint     string `json:"monitorHint,omitempty"`
	BuildStatus     string `json:"buildStatus,omitempty"`
	BuildDuration   string `json:"buildDuration,omitempty"`
	Suggestion      string `json:"suggestion,omitempty"`
	TimedOut        bool   `json:"timedOut,omitempty"`
}

// SSHDeployer executes commands on remote Zerops services.
type SSHDeployer interface {
	ExecSSH(ctx context.Context, hostname string, command string) ([]byte, error)
}

// LocalDeployer executes zcli commands locally.
type LocalDeployer interface {
	ExecZcli(ctx context.Context, args ...string) ([]byte, error)
}

// Deploy deploys code to a Zerops service via SSH or local zcli.
//
// Mode detection:
//   - sourceService != "" -> SSH mode (targetService required)
//   - sourceService == "" && targetService != "" -> Local mode
//   - neither -> INVALID_PARAMETER error
func Deploy(
	ctx context.Context,
	client platform.Client,
	projectID string,
	sshDeployer SSHDeployer,
	localDeployer LocalDeployer,
	authInfo auth.Info,
	sourceService string,
	targetService string,
	setup string,
	workingDir string,
	includeGit bool,
	freshGit bool,
) (*DeployResult, error) {
	id := GitIdentity{Name: authInfo.FullName, Email: authInfo.Email}

	if sourceService != "" {
		if sshDeployer == nil {
			return nil, platform.NewPlatformError(
				platform.ErrNotImplemented,
				"SSH deploy is not available (deployer not configured)",
				"SSH deploy requires a running Zerops container with SSH access",
			)
		}
		return deploySSH(ctx, client, projectID, sshDeployer, authInfo,
			sourceService, targetService, setup, workingDir, includeGit, id, freshGit)
	}
	if targetService != "" {
		if localDeployer == nil {
			return nil, platform.NewPlatformError(
				platform.ErrNotImplemented,
				"Local deploy is not available (deployer not configured)",
				"Local deploy requires zcli to be installed",
			)
		}
		return deployLocal(ctx, client, projectID, localDeployer,
			targetService, workingDir, includeGit, id, freshGit)
	}
	return nil, platform.NewPlatformError(
		platform.ErrInvalidParameter,
		"Either sourceService (SSH mode) or targetService (local mode) is required",
		"Provide sourceService + targetService for SSH deploy, or targetService for local deploy",
	)
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
	id GitIdentity,
	freshGit bool,
) (*DeployResult, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	source, err := resolveServiceID(services, sourceService)
	if err != nil {
		return nil, err
	}

	target, err := resolveServiceID(services, targetService)
	if err != nil {
		return nil, err
	}

	if workingDir == "" {
		workingDir = defaultWorkingDir
	}

	cmd := buildSSHCommand(authInfo, target.ID, setup, workingDir, includeGit, id, freshGit)

	_, err = sshDeployer.ExecSSH(ctx, source.Name, cmd)
	if err != nil {
		return nil, fmt.Errorf("ssh deploy from %s to %s: %w", sourceService, targetService, err)
	}

	return &DeployResult{
		Status:          "BUILD_TRIGGERED",
		Mode:            "ssh",
		SourceService:   sourceService,
		TargetService:   targetService,
		TargetServiceID: target.ID,
		Message:         fmt.Sprintf("Build triggered from %s to %s via SSH", sourceService, targetService),
		MonitorHint:     "Build runs asynchronously. Poll zerops_events for build/deploy FINISHED status.",
	}, nil
}

func deployLocal(
	ctx context.Context,
	client platform.Client,
	projectID string,
	localDeployer LocalDeployer,
	targetService string,
	workingDir string,
	includeGit bool,
	id GitIdentity,
	freshGit bool,
) (*DeployResult, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	target, err := resolveServiceID(services, targetService)
	if err != nil {
		return nil, err
	}

	// zcli push requires a git repo — auto-init if missing (or reinit if freshGit).
	if workingDir != "" {
		if info, statErr := os.Stat(workingDir); statErr == nil && info.IsDir() {
			if err := prepareGitRepo(ctx, workingDir, id, freshGit); err != nil {
				return nil, fmt.Errorf("prepare git repo in %s: %w", workingDir, err)
			}
		}
	}

	args := buildZcliArgs(target.ID, workingDir, includeGit)

	_, err = localDeployer.ExecZcli(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("local deploy to %s: %w", targetService, err)
	}

	return &DeployResult{
		Status:          "BUILD_TRIGGERED",
		Mode:            "local",
		TargetService:   targetService,
		TargetServiceID: target.ID,
		Message:         fmt.Sprintf("Build triggered for %s via local zcli", targetService),
		MonitorHint:     "Build runs asynchronously. Poll zerops_events for build/deploy FINISHED status.",
	}, nil
}

func buildSSHCommand(authInfo auth.Info, targetServiceID, setup, workingDir string, includeGit bool, id GitIdentity, freshGit bool) string {
	var parts []string

	// Login to zcli on the remote host.
	loginCmd := fmt.Sprintf("zcli login %s --zeropsRegion %s", authInfo.Token, authInfo.Region)
	parts = append(parts, loginCmd)

	// Setup command if provided.
	if setup != "" {
		parts = append(parts, setup)
	}

	email := sanitizeQuote(id.Email)
	name := sanitizeQuote(id.Name)
	gitInit := fmt.Sprintf("git init -q && git config user.email '%s' && git config user.name '%s' && git add -A && git commit -q -m 'deploy'", email, name)

	// Push from workingDir with git handling.
	pushArgs := fmt.Sprintf("zcli push --serviceId %s", targetServiceID)
	if includeGit {
		pushArgs += " -g"
	}

	var pushCmd string
	if freshGit {
		// freshGit: remove existing .git and reinitialize.
		pushCmd = fmt.Sprintf("cd %s && rm -rf .git && %s && %s", workingDir, gitInit, pushArgs)
	} else {
		// Default: git-init guard for non-git directories.
		pushCmd = fmt.Sprintf("cd %s && (test -d .git || (%s)) && %s", workingDir, gitInit, pushArgs)
	}
	parts = append(parts, pushCmd)

	return strings.Join(parts, " && ")
}

// prepareGitRepo ensures workingDir contains a git repository.
// zcli push requires a .git directory. If missing, initializes one
// with all files committed. If freshGit is true, removes any existing
// .git directory and reinitializes.
func prepareGitRepo(ctx context.Context, workingDir string, id GitIdentity, freshGit bool) error {
	gitDir := filepath.Join(workingDir, ".git")

	if freshGit {
		// Remove existing .git to start fresh.
		if err := os.RemoveAll(gitDir); err != nil {
			return fmt.Errorf("remove .git: %w", err)
		}
	} else if _, err := os.Stat(gitDir); err == nil {
		return nil // already a git repo, no-op
	}

	cmds := [][]string{
		{"git", "init", "-q"},
		{"git", "config", "user.email", id.Email},
		{"git", "config", "user.name", id.Name},
		{"git", "add", "-A"},
		{"git", "commit", "-q", "-m", "deploy", "--allow-empty"},
	}
	for _, args := range cmds {
		cmd := exec.CommandContext(ctx, args[0], args[1:]...) //nolint:gosec // args are from trusted API
		cmd.Dir = workingDir
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %w\n%s", strings.Join(args, " "), err, out)
		}
	}
	return nil
}

func buildZcliArgs(targetServiceID, workingDir string, includeGit bool) []string {
	args := []string{"push", "--serviceId", targetServiceID}
	if workingDir != "" {
		args = append(args, "--workingDir", workingDir)
	}
	if includeGit {
		args = append(args, "-g")
	}
	return args
}
