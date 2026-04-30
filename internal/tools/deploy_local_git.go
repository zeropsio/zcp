package tools

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/auth"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// resolveTargetForValidation fetches the target ServiceStack from live
// services so pre-deploy validation has the ServiceStackTypeID /
// TypeVersionName required by the Zerops validator endpoint. Returns nil
// (no error) when the service can't be resolved — validation is then
// skipped rather than blocking deploy on a transient list failure.
func resolveTargetForValidation(ctx context.Context, client platform.Client, projectID, hostname string) *platform.ServiceStack {
	if client == nil || projectID == "" || hostname == "" {
		return nil
	}
	svc, _ := ops.LookupService(ctx, client, projectID, hostname)
	return svc
}

// handleLocalGitPush performs `git push` from the user's local git repo
// without ever touching the user's credentials — the local git binary
// inherits whatever auth the user has configured (SSH keys, macOS
// Keychain, credential manager). No GIT_TOKEN, no .netrc. ZCP's role is
// orchestration: validate the repo state, resolve branch, run push,
// record the attempt.
//
// Fail-fast pre-flights (in order):
//  1. workingDir is a git repo.
//  2. HEAD points at a commit.
//  3. origin remote resolvable — set via RemoteURL if not already configured
//     (auto-add only when the user explicitly passed a URL; if origin exists
//     with a different URL we refuse rather than silently rewrite).
//
// Dirty-tree warning is non-blocking; the push still goes through.
// GIT_TERMINAL_PROMPT=0 so a passphrase-protected key without an agent
// fails fast instead of hanging the MCP channel.
func handleLocalGitPush(ctx context.Context, client platform.Client, projectID string, authInfo auth.Info, input DeployLocalInput, stateDir string) (*mcp.CallToolResult, any, error) {
	hostname := input.TargetService
	attempt := workflow.DeployAttempt{
		AttemptedAt: time.Now().UTC().Format(time.RFC3339),
		Setup:       input.Setup,
		Strategy:    deployStrategyGitPush,
	}
	// record stamps the FailureClass alongside the error message so the
	// envelope projection surfaces both. Pre-flight git/config gates are
	// FailureClassConfig (user repo state); the actual git push failure is
	// FailureClassNetwork (transport to the remote). YAML validation is
	// FailureClassConfig.
	record := func(errMsg string, class topology.FailureClass) {
		attempt.Error = errMsg
		attempt.FailureClass = class
		_ = workflow.RecordDeployAttempt(stateDir, hostname, attempt)
	}

	// Meta-based source-of-push + setup-state pre-flight (deploy-decomp P4).
	// Parity with handleGitPush — local + container reject identical shapes.
	if blocked := gitPushMetaPreflight(stateDir, hostname, record); blocked != nil {
		return blocked, nil, nil
	}

	workingDir := input.WorkingDir
	if workingDir == "" {
		if cwd, err := os.Getwd(); err == nil {
			workingDir = cwd
		} else {
			workingDir = "."
		}
	}

	// Pre-push zerops.yaml validation: the Zerops build that triggers on
	// the remote's receipt of our push runs the same platform validator we
	// can call now. Failing here aborts the push before the remote's
	// build cycle starts. Structured validation errors carry APIMeta.
	if target := resolveTargetForValidation(ctx, client, projectID, hostname); target != nil {
		setupName := input.Setup
		if setupName == "" {
			setupName = hostname
		}
		if vErr := ops.RunPreDeployValidation(ctx, client, target, setupName, workingDir); vErr != nil {
			record(fmt.Sprintf("zerops.yaml validation failed: %v", vErr), topology.FailureClassConfig)
			return convertError(vErr, WithRecoveryStatus()), nil, nil
		}
	}

	// 1. git repo check. MCP handlers surface the failure in the
	// CallToolResult (convertError) and return a nil outer error.
	//nolint:nilerr // tool-level error lives in CallToolResult
	if _, err := runGit(ctx, workingDir, "rev-parse", "--is-inside-work-tree"); err != nil {
		_ = err
		record("working dir is not a git repo", topology.FailureClassConfig)
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("workingDir %q is not a git repository", workingDir),
			"Initialize git first: git init && git add -A && git commit -m '<message>'. Then retry.",
		), WithRecoveryStatus()), nil, nil
	}

	// 2. HEAD has commits.
	//nolint:nilerr // tool-level error lives in CallToolResult
	if _, err := runGit(ctx, workingDir, "rev-parse", "HEAD"); err != nil {
		_ = err
		record("no commits on HEAD", topology.FailureClassConfig)
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"git-push requires at least one commit on HEAD",
			"Commit your changes first: git add -A && git commit -m '<message>'. Then retry.",
		), WithRecoveryStatus()), nil, nil
	}

	// 3. Resolve effective remote URL via shared helper — see
	// deploy_git_push.go::resolveEffectiveRemote for the rationale.
	effectiveRemote := resolveEffectiveRemote(stateDir, hostname, input.RemoteURL)

	// 4. Resolve origin URL.
	currentOrigin, _ := runGit(ctx, workingDir, "remote", "get-url", "origin")
	current := strings.TrimSpace(currentOrigin)
	switch {
	case current == "" && effectiveRemote == "":
		record("no origin + no remoteUrl + no stamped meta.RemoteURL", topology.FailureClassConfig)
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"no origin remote configured, no remoteUrl provided, and no remote stamped in service meta",
			"Either configure origin in your repo (git remote add origin <url>), pass remoteUrl=<url> on this call, or run zerops_workflow action=\"git-push-setup\" service=<hostname> remoteUrl=<url> first.",
		), WithRecoveryStatus()), nil, nil
	case current == "" && effectiveRemote != "":
		if _, err := runGit(ctx, workingDir, "remote", "add", "origin", effectiveRemote); err != nil {
			record(fmt.Sprintf("git remote add origin failed: %v", err), topology.FailureClassConfig)
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("git remote add origin %s failed: %v", effectiveRemote, err),
				"",
			), WithRecoveryStatus()), nil, nil
		}
	case current != "" && input.RemoteURL != "" && current != input.RemoteURL:
		record("remoteUrl mismatch with existing origin", topology.FailureClassConfig)
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("origin is %q, you passed remoteUrl=%q — ZCP won't silently rewrite the remote", current, input.RemoteURL),
			"Reconcile manually (git remote set-url origin <url>) or re-run without remoteUrl to use the configured origin.",
		), WithRecoveryStatus()), nil, nil
	}

	// 4. Resolve branch.
	branch := input.Branch
	if branch == "" {
		out, err := runGit(ctx, workingDir, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			record(fmt.Sprintf("detect branch: %v", err), topology.FailureClassConfig)
			return convertError(platform.NewPlatformError(
				platform.ErrPrerequisiteMissing,
				fmt.Sprintf("could not detect branch in %s: %v", workingDir, err),
				"Pass branch=<name> explicitly, or check your repo state.",
			), WithRecoveryStatus()), nil, nil
		}
		branch = strings.TrimSpace(out)
	}

	// 5. Dirty-tree warning (non-blocking).
	var warnings []string
	status, _ := runGit(ctx, workingDir, "status", "--porcelain")
	if strings.TrimSpace(status) != "" {
		warnings = append(warnings, "Uncommitted changes in working tree — only committed code will be pushed.")
	}

	// 6. Push with prompt disabled so credential failures are fast and visible.
	pushOut, pushErr := runGitWithEnv(ctx, workingDir,
		[]string{"GIT_TERMINAL_PROMPT=0"},
		"push", "origin", branch,
	)
	if pushErr != nil {
		// Run the classifier against the git stderr — credential vs network
		// vs ref-rejection all read differently and the agent's recovery
		// path differs accordingly (E2).
		gitWrap := &platform.SSHExecError{Hostname: "local-git", Output: pushOut, Err: pushErr}
		classification := classifyTransportError(gitWrap, "git-push")
		category := topology.FailureClassNetwork
		if classification != nil {
			category = classification.Category
		}
		record(fmt.Sprintf("git push: %v", pushErr), category)
		return convertError(platform.NewPlatformError(
			platform.ErrDeployFailed,
			fmt.Sprintf("git push origin %s failed: %s", branch, truncateStderr(pushOut)),
			"Check your local git credentials (SSH keys, credential manager) and the remote URL. For passphrase-protected keys, ensure ssh-agent is running.",
		), WithRecoveryStatus(), WithFailureClassification(classification)), nil, nil
	}

	status2 := "PUSHED"
	message := fmt.Sprintf("Pushed %s to origin (%s)", branch, currentEffectiveOrigin(current, effectiveRemote))
	if strings.Contains(pushOut, "Everything up-to-date") {
		status2 = "NOTHING_TO_PUSH"
		message = fmt.Sprintf("Nothing to push on %s — remote already up-to-date", branch)
	}

	result := &ops.GitPushResult{
		Status:    status2,
		RemoteURL: currentEffectiveOrigin(current, effectiveRemote),
		Branch:    branch,
		Message:   message,
	}

	// C2 closure (audit-prerelease-internal-testing-2026-04-29): see the
	// extended rationale comment at the matching site in
	// internal/tools/deploy_git_push.go. Local git-push has the same
	// async-build-vs-stamp gap as the container path; auto-stamping
	// FirstDeployedAt on push success made Deployed=true race ahead of the
	// actual build. Now we record the in-flight push attempt (no
	// SucceededAt) and require explicit record-deploy after the agent
	// observes Status=ACTIVE on zerops_events. The result.NextActions text
	// below names that bridge.
	_ = workflow.RecordDeployAttempt(stateDir, hostname, attempt)

	result.NextActions = fmt.Sprintf(
		"Watch the build via zerops_events serviceHostname=%q until Status=ACTIVE, then ack with zerops_workflow action=\"record-deploy\" targetService=%q. The push transmitted bytes; the platform build runs async and FirstDeployedAt will not stamp until you bridge it.",
		hostname, hostname,
	)

	// If the linked service's meta tracks a BuildIntegration (webhook /
	// actions), the downstream build fires remotely. If it's BuildIntegrationNone,
	// the push succeeded but Zerops won't auto-build. Surface that as a warning
	// so the user isn't left wondering why nothing happens.
	if warn := trackTriggerMissingWarning(stateDir, hostname); warn != "" {
		warnings = append(warnings, warn)
	}

	type localGitPushResponse struct {
		*ops.GitPushResult
		Warnings         []string          `json:"warnings,omitempty"`
		WorkSessionState *WorkSessionState `json:"workSessionState,omitempty"`
		_                struct{}          `json:"-"` // authInfo kept for symmetry with SSH handler
	}
	_ = authInfo
	return jsonResult(localGitPushResponse{
		GitPushResult:    result,
		Warnings:         warnings,
		WorkSessionState: sessionAnnotations(stateDir),
	}), nil, nil
}

// runGit runs `git -C <dir> <args...>` with default environment and
// returns stdout (stderr swallowed into the error).
func runGit(ctx context.Context, workingDir string, args ...string) (string, error) {
	return runGitWithEnv(ctx, workingDir, nil, args...)
}

// runGitWithEnv runs git with additional env vars on top of os.Environ.
// Stdout is returned as string; non-zero exit returns stderr-tail in
// the error message so pre-flight rejections stay informative.
func runGitWithEnv(ctx context.Context, workingDir string, extraEnv []string, args ...string) (string, error) {
	fullArgs := append([]string{"-C", workingDir}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return stdout.String(), fmt.Errorf("%s", msg)
	}
	return stdout.String(), nil
}

// truncateStderr keeps error reports compact — only the last few lines
// usually matter for git failures (auth, fast-forward rejections).
func truncateStderr(full string) string {
	lines := strings.Split(strings.TrimSpace(full), "\n")
	if len(lines) <= 5 {
		return strings.Join(lines, "; ")
	}
	return strings.Join(lines[len(lines)-5:], "; ")
}

// currentEffectiveOrigin picks whichever URL is actually configured on
// the repo for the response payload. If the repo had origin already, we
// return that; otherwise we return the URL we just added.
func currentEffectiveOrigin(current, provided string) string {
	if current != "" {
		return current
	}
	return provided
}

// trackTriggerMissingWarning builds a soft warning when the target service's
// meta is on git-push close-mode but has no ZCP-managed BuildIntegration
// configured — the push succeeded on the git side, but no Zerops build
// fires unless the user has independent CI/CD (which ZCP doesn't track).
// FindServiceMeta honors the pair-keyed invariant — a stage-hostname target
// resolves to the dev-keyed meta file (spec-workflows.md §8 E8).
//
// Reads CloseDeployMode + BuildIntegration. UTILITY framing: the warning
// says "no ZCP-managed integration is configured", not "no build will
// fire" — the user's external CI may still pick up the push.
func trackTriggerMissingWarning(stateDir, hostname string) string {
	meta, _ := workflow.FindServiceMeta(stateDir, hostname)
	if meta == nil || meta.CloseDeployMode != topology.CloseModeGitPush {
		return ""
	}
	if meta.BuildIntegration != "" && meta.BuildIntegration != topology.BuildIntegrationNone {
		return ""
	}
	return fmt.Sprintf("service %q is on close-mode=git-push but has no ZCP-managed build integration configured — the push lands in git, but no Zerops build fires unless your own CI/CD picks it up. Run zerops_workflow action=\"build-integration\" service=%q integration=\"webhook|actions\" to finish setup.", hostname, hostname)
}
