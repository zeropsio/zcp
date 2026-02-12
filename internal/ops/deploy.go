package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
)

const defaultWorkingDir = "/var/www"

// DeployResult contains the outcome of a deploy operation.
type DeployResult struct {
	Status          string `json:"status"`
	Mode            string `json:"mode"` // "ssh" or "local"
	SourceService   string `json:"sourceService,omitempty"`
	TargetService   string `json:"targetService"`
	TargetServiceID string `json:"targetServiceId"`
	Message         string `json:"message"`
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
) (*DeployResult, error) {
	if sourceService != "" {
		if sshDeployer == nil {
			return nil, platform.NewPlatformError(
				platform.ErrNotImplemented,
				"SSH deploy is not available (deployer not configured)",
				"SSH deploy requires a running Zerops container with SSH access",
			)
		}
		return deploySSH(ctx, client, projectID, sshDeployer, authInfo,
			sourceService, targetService, setup, workingDir)
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
			targetService, workingDir)
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

	cmd := buildSSHCommand(authInfo, target.ID, setup, workingDir)

	_, err = sshDeployer.ExecSSH(ctx, source.Name, cmd)
	if err != nil {
		return nil, fmt.Errorf("ssh deploy from %s to %s: %w", sourceService, targetService, err)
	}

	return &DeployResult{
		Status:          "DEPLOYED",
		Mode:            "ssh",
		SourceService:   sourceService,
		TargetService:   targetService,
		TargetServiceID: target.ID,
		Message:         fmt.Sprintf("Deployed from %s to %s via SSH", sourceService, targetService),
	}, nil
}

func deployLocal(
	ctx context.Context,
	client platform.Client,
	projectID string,
	localDeployer LocalDeployer,
	targetService string,
	workingDir string,
) (*DeployResult, error) {
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}

	target, err := resolveServiceID(services, targetService)
	if err != nil {
		return nil, err
	}

	args := buildZcliArgs(target.ID, workingDir)

	_, err = localDeployer.ExecZcli(ctx, args...)
	if err != nil {
		return nil, fmt.Errorf("local deploy to %s: %w", targetService, err)
	}

	return &DeployResult{
		Status:          "DEPLOYED",
		Mode:            "local",
		TargetService:   targetService,
		TargetServiceID: target.ID,
		Message:         fmt.Sprintf("Deployed to %s via local zcli", targetService),
	}, nil
}

func buildSSHCommand(authInfo auth.Info, targetServiceID, setup, workingDir string) string {
	var parts []string

	// Login to zcli on the remote host.
	loginCmd := fmt.Sprintf("zcli login %s --zeropsRegion %s", authInfo.Token, authInfo.Region)
	parts = append(parts, loginCmd)

	// Setup command if provided.
	if setup != "" {
		parts = append(parts, setup)
	}

	// Push from workingDir.
	pushCmd := fmt.Sprintf("cd %s && zcli push --serviceId %s", workingDir, targetServiceID)
	parts = append(parts, pushCmd)

	return strings.Join(parts, " && ")
}

func buildZcliArgs(targetServiceID, workingDir string) []string {
	args := []string{"push", "--serviceId", targetServiceID}
	if workingDir != "" {
		args = append(args, "--workingDir", workingDir)
	}
	return args
}
