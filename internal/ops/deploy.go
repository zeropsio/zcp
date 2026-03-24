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

// shellQuote wraps a string in POSIX single quotes, escaping embedded single quotes.
// This prevents shell injection from untrusted input (e.g., user names, emails).
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// DeployResult contains the outcome of a deploy operation.
type DeployResult struct {
	Status            string   `json:"status"`
	Mode              string   `json:"mode"` // "ssh"
	SourceService     string   `json:"sourceService,omitempty"`
	TargetService     string   `json:"targetService"`
	TargetServiceID   string   `json:"targetServiceId"`
	TargetServiceType string   `json:"targetServiceType,omitempty"`
	Message           string   `json:"message"`
	MonitorHint       string   `json:"monitorHint,omitempty"`
	BuildStatus       string   `json:"buildStatus,omitempty"`
	BuildDuration     string   `json:"buildDuration,omitempty"`
	Suggestion        string   `json:"suggestion,omitempty"`
	SSHReady          bool     `json:"sshReady,omitempty"`
	TimedOut          bool     `json:"timedOut,omitempty"`
	NextActions       string   `json:"nextActions,omitempty"`
	Warnings          []string `json:"warnings,omitempty"`
	BuildLogs         []string `json:"buildLogs,omitempty"`       // last N lines of build output
	BuildLogsSource   string   `json:"buildLogsSource,omitempty"` // "build_container" or empty
}

// SSHDeployer executes commands on remote Zerops services.
type SSHDeployer interface {
	ExecSSH(ctx context.Context, hostname string, command string) ([]byte, error)
}

// Deploy deploys code to a Zerops service via SSH.
//
// Auto-inference:
//   - targetService is required
//   - sourceService == "" → auto-inferred as targetService (self-deploy)
//   - sourceService == targetService → includeGit forced to true
func Deploy(
	ctx context.Context,
	client platform.Client,
	projectID string,
	sshDeployer SSHDeployer,
	authInfo auth.Info,
	sourceService string,
	targetService string,
	workingDir string,
	includeGit bool,
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
	if sourceService == targetService {
		includeGit = true // self-deploy always preserves .git
	}

	id := GitIdentity{Name: authInfo.FullName, Email: authInfo.Email}
	return deploySSH(ctx, client, projectID, sshDeployer, authInfo,
		sourceService, targetService, workingDir, includeGit, id)
}

func deploySSH(
	ctx context.Context,
	client platform.Client,
	projectID string,
	sshDeployer SSHDeployer,
	authInfo auth.Info,
	sourceService string,
	targetService string,
	workingDir string,
	includeGit bool,
	id GitIdentity,
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
	// Reject mount-style paths — workingDir runs INSIDE the container where
	// /var/www/{hostname} doesn't exist. The correct container path is /var/www.
	if workingDir != defaultWorkingDir && strings.HasPrefix(workingDir, defaultWorkingDir+"/") {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("workingDir %q looks like a local SSHFS mount path, not a container path. Inside the container, code lives at /var/www", workingDir),
			"Use workingDir=\"/var/www\" or omit workingDir (defaults to /var/www). The mount path /var/www/{hostname} is only valid on the local machine.",
		)
	}

	// Pre-deploy validation: read zerops.yml from SSHFS mount (local filesystem).
	// Mount path: /var/www/{sourceService}/ maps to remote /var/www/
	var warnings []string
	mountPath := filepath.Join("/var/www", sourceService)
	if _, statErr := os.Stat(mountPath); statErr == nil {
		warnings = ValidateZeropsYml(mountPath, targetService)
	}

	cmd := buildSSHCommand(authInfo, target.ID, workingDir, includeGit, id)

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

func buildSSHCommand(authInfo auth.Info, targetServiceID, workingDir string, includeGit bool, id GitIdentity) string {
	parts := make([]string, 0, 2)

	// Login to zcli on the remote host.
	loginCmd := fmt.Sprintf("zcli login %s", authInfo.Token)
	parts = append(parts, loginCmd)

	email := shellQuote(id.Email)
	name := shellQuote(id.Name)

	// Init only if no .git exists. Use -b main for consistent branch name.
	gitInit := "(test -d .git || git init -q -b main)"

	// Always set identity (internal deploy commits, not user-facing).
	gitIdentity := fmt.Sprintf("git config user.email %s && git config user.name %s", email, name)

	// Always stage + commit. Skip commit if nothing changed (diff-index quiet).
	// On fresh init, HEAD doesn't exist -> diff-index fails -> || fires -> commit runs.
	gitCommit := "git add -A && (git diff-index --quiet HEAD 2>/dev/null || git commit -q -m 'deploy')"

	// Push from workingDir with git handling.
	pushArgs := fmt.Sprintf("zcli push --serviceId %s", targetServiceID)
	if includeGit {
		pushArgs += " -g"
	}

	pushCmd := fmt.Sprintf("cd %s && %s && %s && %s && %s",
		workingDir, gitInit, gitIdentity, gitCommit, pushArgs)
	parts = append(parts, pushCmd)

	return strings.Join(parts, " && ")
}
