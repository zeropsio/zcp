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
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return nil
	}
	for i := range services {
		if services[i].Name == hostname {
			return &services[i]
		}
	}
	return nil
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
	record := func(errMsg string) {
		attempt.Error = errMsg
		_ = workflow.RecordDeployAttempt(stateDir, hostname, attempt)
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
			record(fmt.Sprintf("zerops.yaml validation failed: %v", vErr))
			return convertError(vErr), nil, nil
		}
	}

	// 1. git repo check. MCP handlers surface the failure in the
	// CallToolResult (convertError) and return a nil outer error.
	//nolint:nilerr // tool-level error lives in CallToolResult
	if _, err := runGit(ctx, workingDir, "rev-parse", "--is-inside-work-tree"); err != nil {
		_ = err
		record("working dir is not a git repo")
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			fmt.Sprintf("workingDir %q is not a git repository", workingDir),
			"Initialize git first: git init && git add -A && git commit -m '<message>'. Then retry.",
		)), nil, nil
	}

	// 2. HEAD has commits.
	//nolint:nilerr // tool-level error lives in CallToolResult
	if _, err := runGit(ctx, workingDir, "rev-parse", "HEAD"); err != nil {
		_ = err
		record("no commits on HEAD")
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"git-push requires at least one commit on HEAD",
			"Commit your changes first: git add -A && git commit -m '<message>'. Then retry.",
		)), nil, nil
	}

	// 3. Resolve origin URL.
	currentOrigin, _ := runGit(ctx, workingDir, "remote", "get-url", "origin")
	current := strings.TrimSpace(currentOrigin)
	switch {
	case current == "" && input.RemoteURL == "":
		record("no origin + no remoteUrl")
		return convertError(platform.NewPlatformError(
			platform.ErrPrerequisiteMissing,
			"no origin remote configured and no remoteUrl provided",
			"Either configure origin in your repo (git remote add origin <url>) or pass remoteUrl=<url> to this call.",
		)), nil, nil
	case current == "" && input.RemoteURL != "":
		if _, err := runGit(ctx, workingDir, "remote", "add", "origin", input.RemoteURL); err != nil {
			record(fmt.Sprintf("git remote add origin failed: %v", err))
			return convertError(platform.NewPlatformError(
				platform.ErrInvalidParameter,
				fmt.Sprintf("git remote add origin %s failed: %v", input.RemoteURL, err),
				"",
			)), nil, nil
		}
	case current != "" && input.RemoteURL != "" && current != input.RemoteURL:
		record("remoteUrl mismatch with existing origin")
		return convertError(platform.NewPlatformError(
			platform.ErrInvalidParameter,
			fmt.Sprintf("origin is %q, you passed remoteUrl=%q — ZCP won't silently rewrite the remote", current, input.RemoteURL),
			"Reconcile manually (git remote set-url origin <url>) or re-run without remoteUrl to use the configured origin.",
		)), nil, nil
	}

	// 4. Resolve branch.
	branch := input.Branch
	if branch == "" {
		out, err := runGit(ctx, workingDir, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			record(fmt.Sprintf("detect branch: %v", err))
			return convertError(platform.NewPlatformError(
				platform.ErrPrerequisiteMissing,
				fmt.Sprintf("could not detect branch in %s: %v", workingDir, err),
				"Pass branch=<name> explicitly, or check your repo state.",
			)), nil, nil
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
		record(fmt.Sprintf("git push: %v", pushErr))
		return convertError(platform.NewPlatformError(
			platform.ErrDeployFailed,
			fmt.Sprintf("git push origin %s failed: %s", branch, truncateStderr(pushOut)),
			"Check your local git credentials (SSH keys, credential manager) and the remote URL. For passphrase-protected keys, ensure ssh-agent is running.",
		)), nil, nil
	}

	status2 := "PUSHED"
	message := fmt.Sprintf("Pushed %s to origin (%s)", branch, currentEffectiveOrigin(current, input.RemoteURL))
	if strings.Contains(pushOut, "Everything up-to-date") {
		status2 = "NOTHING_TO_PUSH"
		message = fmt.Sprintf("Nothing to push on %s — remote already up-to-date", branch)
	}

	result := &ops.GitPushResult{
		Status:    status2,
		RemoteURL: currentEffectiveOrigin(current, input.RemoteURL),
		Branch:    branch,
		Message:   message,
	}

	attempt.SucceededAt = time.Now().UTC().Format(time.RFC3339)
	_ = workflow.RecordDeployAttempt(stateDir, hostname, attempt)

	// If the linked service's meta tracks a PushGitTrigger (Phase A.6 field),
	// the downstream trigger (webhook / actions) fires remotely. If it's
	// empty, the push succeeded but Zerops won't auto-build. Surface that
	// as a warning so the user isn't left wondering why nothing happens.
	if warn := trackTriggerMissingWarning(stateDir, hostname); warn != "" {
		warnings = append(warnings, warn)
	}

	note, progress := sessionAnnotations(stateDir)
	type localGitPushResponse struct {
		*ops.GitPushResult
		Warnings          []string                    `json:"warnings,omitempty"`
		WorkSessionNote   string                      `json:"workSessionNote,omitempty"`
		AutoCloseProgress *workflow.AutoCloseProgress `json:"autoCloseProgress,omitempty"`
		_                 struct{}                    `json:"-"` // authInfo kept for symmetry with SSH handler
	}
	_ = authInfo
	return jsonResult(localGitPushResponse{
		GitPushResult:     result,
		Warnings:          warnings,
		WorkSessionNote:   note,
		AutoCloseProgress: progress,
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

// trackTriggerMissingWarning builds a soft warning when the target
// service's meta is on push-git but has no PushGitTrigger recorded —
// the push succeeded on the git side, but Zerops won't auto-build
// without either a webhook or a GitHub Actions workflow configured.
// FindServiceMeta honors the pair-keyed invariant — a stage-hostname
// target resolves to the dev-keyed meta file (spec-workflows.md §8 E8).
func trackTriggerMissingWarning(stateDir, hostname string) string {
	meta, _ := workflow.FindServiceMeta(stateDir, hostname)
	if meta == nil || meta.DeployStrategy != topology.StrategyPushGit {
		return ""
	}
	if meta.PushGitTrigger != "" {
		return ""
	}
	return fmt.Sprintf("service %q uses push-git but has no downstream trigger configured — the push lands in git but Zerops won't build. Run zerops_workflow action=\"strategy\" strategies={%q:%q} trigger=\"webhook|actions\" to finish setup.", hostname, hostname, topology.StrategyPushGit)
}
