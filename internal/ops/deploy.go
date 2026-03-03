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
	Status          string   `json:"status"`
	Mode            string   `json:"mode"` // "ssh"
	SourceService   string   `json:"sourceService,omitempty"`
	TargetService   string   `json:"targetService"`
	TargetServiceID string   `json:"targetServiceId"`
	Message         string   `json:"message"`
	MonitorHint     string   `json:"monitorHint,omitempty"`
	BuildStatus     string   `json:"buildStatus,omitempty"`
	BuildDuration   string   `json:"buildDuration,omitempty"`
	Suggestion      string   `json:"suggestion,omitempty"`
	TimedOut        bool     `json:"timedOut,omitempty"`
	NextActions     string   `json:"nextActions,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
	BuildLogs       []string `json:"buildLogs,omitempty"`       // last N lines of build output
	BuildLogsSource string   `json:"buildLogsSource,omitempty"` // "build_container" or empty
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
				Status:          "BUILD_TRIGGERED",
				Mode:            "ssh",
				SourceService:   sourceService,
				TargetService:   targetService,
				TargetServiceID: target.ID,
				Message:         fmt.Sprintf("Build triggered from %s to %s (SSH session closed after push)", sourceService, targetService),
				MonitorHint:     "Build runs asynchronously. Poll zerops_events for build/deploy FINISHED status.",
				Warnings:        warnings,
			}, nil
		}
		return nil, classifySSHError(err, sourceService, targetService)
	}

	return &DeployResult{
		Status:          "BUILD_TRIGGERED",
		Mode:            "ssh",
		SourceService:   sourceService,
		TargetService:   targetService,
		TargetServiceID: target.ID,
		Message:         fmt.Sprintf("Build triggered from %s to %s via SSH", sourceService, targetService),
		MonitorHint:     "Build runs asynchronously. Poll zerops_events for build/deploy FINISHED status.",
		Warnings:        warnings,
	}, nil
}

func buildSSHCommand(authInfo auth.Info, targetServiceID, workingDir string, includeGit bool, id GitIdentity) string {
	parts := make([]string, 0, 2)

	// Login to zcli on the remote host.
	loginCmd := fmt.Sprintf("zcli login %s", authInfo.Token)
	parts = append(parts, loginCmd)

	email := shellQuote(id.Email)
	name := shellQuote(id.Name)
	gitInit := fmt.Sprintf("git init -q && git config user.email %s && git config user.name %s && git add -A && git commit -q -m 'deploy'", email, name)

	// Push from workingDir with git handling.
	pushArgs := fmt.Sprintf("zcli push --serviceId %s", targetServiceID)
	if includeGit {
		pushArgs += " -g"
	}

	// Git-init guard: init only if no .git exists.
	pushCmd := fmt.Sprintf("cd %s && (test -d .git || (%s)) && %s", workingDir, gitInit, pushArgs)
	parts = append(parts, pushCmd)

	return strings.Join(parts, " && ")
}
