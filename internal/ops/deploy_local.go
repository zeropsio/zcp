package ops

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/platform"
)

// commandRunner abstracts command execution for testability.
type commandRunner interface {
	LookPath(file string) (string, error)
	Run(ctx context.Context, name string, args ...string) (stdout, stderr string, err error)
}

// execRunner is the production implementation using os/exec.
type execRunner struct{}

func (execRunner) LookPath(file string) (string, error) { return exec.LookPath(file) }

func (execRunner) Run(ctx context.Context, name string, args ...string) (string, string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf
	err := cmd.Run()
	return out.String(), errBuf.String(), err
}

// runner is the active command runner. Tests override via OverrideRunnerForTest.
var runner commandRunner = execRunner{}

// OverrideRunnerForTest replaces the command runner for testing. Returns a restore function.
func OverrideRunnerForTest(r commandRunner) func() {
	old := runner
	runner = r
	return func() { runner = old }
}

// DeployLocal deploys code from the user's local machine to a Zerops service via zcli push.
//
// Steps:
//  1. Validate zcli in PATH
//  2. Resolve targetService hostname → service ID
//  3. Validate zerops.yml exists at workingDir
//  4. Run zcli login
//  5. Run zcli push <hostname> --working-dir <path> [-g]
//  6. Return BUILD_TRIGGERED on success
func DeployLocal(
	ctx context.Context,
	client platform.Client,
	projectID string,
	authInfo auth.Info,
	targetService string,
	workingDir string,
	includeGit bool,
) (*DeployResult, error) {
	// 1. Validate zcli.
	if _, err := runner.LookPath("zcli"); err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"zcli not found in PATH",
			"Install zcli: https://docs.zerops.io/references/cli",
		)
	}

	// 2. Validate targetService.
	if targetService == "" {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			"targetService is required",
			"Provide targetService hostname for deploy.",
		)
	}

	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("list services: %w", err)
	}
	target, err := resolveServiceID(services, targetService)
	if err != nil {
		return nil, err
	}

	// 3. Default workingDir.
	if workingDir == "" {
		workingDir = "."
	}

	// 4. Validate zerops.yml.
	zeropsYmlPath := workingDir + "/zerops.yml"
	if _, statErr := os.Stat(zeropsYmlPath); statErr != nil {
		return nil, platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("zerops.yml not found at %s", workingDir),
			"Create zerops.yml in your project directory. Use zerops_knowledge for examples.",
		)
	}

	warnings := ValidateZeropsYml(workingDir, targetService)

	// 5. Login.
	_, stderr, err := runner.Run(ctx, "zcli", "login", authInfo.Token)
	if err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrDeployFailed,
			"zcli login failed: "+strings.TrimSpace(lastLines(stderr, 3)),
			"Check your API token. Regenerate in Zerops GUI if expired.",
		)
	}

	// 6. Push.
	args := []string{"push", targetService, "--working-dir", workingDir}
	if includeGit {
		args = append(args, "-g")
	}
	_, stderr, err = runner.Run(ctx, "zcli", args...)
	if err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrDeployFailed,
			"zcli push failed: "+strings.TrimSpace(lastLines(stderr, 5)),
			"Check zerops.yml syntax and build configuration.",
		)
	}

	return &DeployResult{
		Status:            "BUILD_TRIGGERED",
		Mode:              "local",
		TargetService:     targetService,
		TargetServiceID:   target.ID,
		TargetServiceType: target.ServiceStackTypeInfo.ServiceStackTypeVersionName,
		Message:           fmt.Sprintf("Build triggered for %s via zcli push", targetService),
		MonitorHint:       "Build runs asynchronously. Poll zerops_events for build/deploy FINISHED status.",
		Warnings:          warnings,
	}, nil
}

// lastLines is defined in deploy_classify.go
