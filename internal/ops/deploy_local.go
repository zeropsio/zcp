package ops

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops/git"
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
// Uses --service-id and --project-id flags for non-interactive mode (no TTY needed).
// zcli push blocks until the build pipeline completes. pollDeployBuild (in tool handler)
// then confirms the final status via API.
//
// Always runs with --no-git; recipes that need committed history go
// through strategy=git-push (handleLocalGitPush in the tool layer),
// which drives the user's own git CLI on a separate code path.
// sha, when non-empty, resolves via `git rev-parse --verify <sha>^{commit}`
// in workingDir and switches the push to a deploy-from-commit: sha's tree is
// extracted into a fresh temp dir OUTSIDE workingDir (git archive | tar -x,
// never zcli's own --workspace-state archiver — see ops/git.
// ExtractCommitToTemp), and the push runs from THAT tree with
// --version-name <sha> instead of workingDir directly. docs/spec-workflows.md
// §4.5.
func DeployLocal(
	ctx context.Context,
	client platform.Client,
	projectID string,
	authInfo auth.Info,
	targetService string,
	setup string,
	workingDir string,
	sha string,
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
	target, err := FindService(services, targetService)
	if err != nil {
		return nil, err
	}

	// A redeploy of a FAILED / READY_TO_DEPLOY-with-failed-history target is
	// NOT gated: a failed deploy is non-destructive (the prior appVersion
	// keeps serving until a new one activates), so refusing the corrective
	// redeploy was a category error that deadlocked recovery (the only thing
	// clearing the refusal was a successful deploy, which it blocked) and
	// pushed the agent into the destructive zerops_import override escape.
	// The failed-deploy response already carries failureClassification — the
	// agent diagnoses from that and redeploys. Hard gates live only on
	// genuinely destructive ops (zerops_import override) + DM-2 self-deploy.
	// See plans/deploy-gate-category-error-2026-06-04.md.

	// 3. Default workingDir.
	if workingDir == "" {
		workingDir = "."
	}

	// 3b. Resolve sha (docs/spec-workflows.md §4.5). Explicit sha only —
	// empty sha runs today's zcli push args unchanged (no git commands at
	// all: no resolve, no extraction, no --version-name). The RESPONSE is
	// not byte-identical, though: pollDeployBuild always fills
	// AppVersionID from the resolved build event regardless of sha (tools/
	// deploy_poll.go), so every successful deploy's JSON gains that field
	// — a strict superset addition (omitempty), not a behavior change to
	// any existing field. On sha resolution, sha's tree is extracted into
	// a fresh temp dir OUTSIDE workingDir; every step below (yaml
	// validation, push) reads from deployDir instead of workingDir, since
	// deployDir is the exact
	// tree being pushed.
	deployDir := workingDir
	resolvedSHA := ""
	previousSHA := ""
	var cleanupTemp func()
	if sha != "" {
		resolved, resolveErr := git.ResolveSHA(ctx, git.LocalRunner{}, workingDir, sha)
		if resolveErr != nil {
			return nil, platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("sha %q did not resolve to a commit in %s: %v", sha, workingDir, resolveErr),
				`Pass a commit sha reachable via "git rev-parse" in workingDir.`,
			)
		}
		resolvedSHA = resolved
		// Read BEFORE the ledger moves it (WriteLedger runs later, in the
		// tools layer, once the build resolves) — this is the "what's
		// running there now" the response's message names.
		previousSHA, _ = git.ReadEnvRef(ctx, git.LocalRunner{}, workingDir, targetService)

		tmpDir, mkErr := git.MkTempDir(ctx, git.LocalRunner{})
		if mkErr != nil {
			return nil, fmt.Errorf("create archive tmp dir: %w", mkErr)
		}
		cleanupTemp = func() {
			cctx, cancel := cleanupTempCtx(ctx)
			defer cancel()
			_ = git.RemoveTemp(cctx, git.LocalRunner{}, tmpDir)
		}
		if extractErr := git.ExtractCommitToTemp(ctx, git.LocalRunner{}, workingDir, resolvedSHA, tmpDir); extractErr != nil {
			cleanupTemp()
			return nil, fmt.Errorf("extract commit %s: %w", resolvedSHA, extractErr)
		}
		deployDir = tmpDir
	}
	if cleanupTemp != nil {
		defer cleanupTemp()
	}

	// 4. Validate zerops.yaml exists. Try .yaml first (canonical Zerops
	// convention — what every doc, recipe, and atom uses), fall back to
	// .yml for repos created before the rename. Mirrors the same
	// fallback ParseZeropsYml does in deploy_validate.go; the error
	// must name BOTH extensions so an agent doesn't spelunk for a file
	// under one name when the tool was looking for another (flow-eval-
	// local suite 20260506-123002 burned five tool calls on the .yml-
	// only check + .yaml-named error message disagreement).
	yamlPath := filepath.Join(deployDir, "zerops.yaml")
	if _, statErr := os.Stat(yamlPath); statErr != nil {
		ymlPath := filepath.Join(deployDir, "zerops.yml")
		if _, statErr := os.Stat(ymlPath); statErr != nil {
			return nil, platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("zerops.yaml not found in %s (also tried zerops.yml)", deployDir),
				"Create zerops.yaml in your project directory. Use zerops_knowledge for examples.",
			)
		}
	}

	setupName := setup
	if setupName == "" {
		setupName = targetService
	}
	serviceType := target.ServiceStackTypeInfo.ServiceStackTypeVersionName
	// Local deploy is always cross-deploy (DM-1): the local working dir is
	// the source, the Zerops service is a distinct target. The deploy artifact
	// overwrites the target's /var/www/ without touching the local working
	// dir, so DM-2 (source-destruction) does not apply.
	warnings, vErr := ValidateZeropsYml(deployDir, setupName, serviceType, DeployClassCross)
	if vErr != nil {
		return nil, vErr
	}
	// .deployignore lint surfaces every finding (artifact patterns and
	// redundant entries alike) as warnings. The TEACH-side teaching
	// lives in internal/knowledge/themes/core.md; deploy never blocks
	// on filename-pattern matches here.
	ignoreLint, ignoreErr := LintDeployignore(deployDir)
	if ignoreErr != nil {
		return nil, fmt.Errorf("lint .deployignore: %w", ignoreErr)
	}
	warnings = append(warnings, ignoreLint.Warnings...)

	// Pre-deploy API validation: Zerops validates zerops.yaml pre-flight so
	// field/syntax errors surface with field-level apiMeta before any build
	// cycle is wasted. Any failure (validation, transport, auth) aborts
	// deploy — no fallback: if Zerops is unreachable the push step would
	// fail anyway.
	if err := RunPreDeployValidation(ctx, client, target, setupName, deployDir); err != nil {
		return nil, err
	}

	// 5. Login.
	_, stderr, err := runner.Run(ctx, "zcli", "login", "--", authInfo.Token)
	if err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrDeployFailed,
			"zcli login failed: "+strings.TrimSpace(lastLines(stderr, 3)),
			"Check your API token. Regenerate in Zerops GUI if expired.",
		)
	}

	// 6. Push with explicit --service-id + --project-id → non-interactive (no TTY needed).
	// zcli blocks until build+deploy completes, then pollDeployBuild confirms via API.
	args := []string{
		"push",
		"--service-id", target.ID,
		"--project-id", projectID,
		"--working-dir", deployDir,
	}
	if setup != "" {
		args = append(args, "--setup", setup)
	}
	args = append(args, "--no-git")
	if resolvedSHA != "" {
		args = append(args, "--version-name", resolvedSHA)
	}
	_, stderr, err = runner.Run(ctx, "zcli", args...)
	if err != nil {
		return nil, platform.NewPlatformError(
			platform.ErrDeployFailed,
			"zcli push failed: "+strings.TrimSpace(lastLines(stderr, 5)),
			"Check zerops.yaml syntax and build configuration.",
		)
	}

	message := fmt.Sprintf("Build triggered for %s via zcli push", targetService)
	if resolvedSHA != "" {
		message = fmt.Sprintf("Build triggered for %s via zcli push (commit %s)", targetService, resolvedSHA)
	}
	return &DeployResult{
		Status:            "BUILD_TRIGGERED",
		Mode:              "local",
		TargetService:     targetService,
		TargetServiceID:   target.ID,
		TargetServiceType: target.ServiceStackTypeInfo.ServiceStackTypeVersionName,
		Message:           message,
		MonitorHint:       "Build runs asynchronously. Poll zerops_events for build/deploy FINISHED status.",
		Warnings:          warnings,
		SHA:               resolvedSHA,
		PreviousSHA:       previousSHA,
	}, nil
}

// lastLines is defined in deploy_classify.go
