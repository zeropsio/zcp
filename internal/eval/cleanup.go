package eval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// ProtectedService is the controlling service that must never be deleted.
const ProtectedService = "zcp"

// protectedPaths are files/dirs in the working directory that survive cleanup.
// CLAUDE.md is intentionally NOT protected: each scenario regenerates it via
// init.Run after seed, so carrying stale REFLOG/service references between
// runs only pollutes the next agent's context.
//
// `.zcp-eval-workdir` is the sentinel file written by the local-mode flow-eval
// wrapper to mark a workdir as disposable; protecting it lets CleanupProject
// run multiple times in the same workdir without the second call tripping
// the sentinel gate (the first cleanWorkDir would otherwise remove it).
var protectedPaths = map[string]bool{
	".claude":           true,
	".mcp.json":         true,
	".zcp":              true,
	".zcp-eval-workdir": true,
}

// MatchesEvalPrefix returns true if the hostname starts with the given eval prefix.
// Empty prefix never matches (safety guard).
func MatchesEvalPrefix(hostname, prefix string) bool {
	if prefix == "" {
		return false
	}
	return strings.HasPrefix(hostname, prefix)
}

// CleanupEvalServices deletes all services whose hostname starts with the given prefix.
// It lists services, filters by prefix, deletes each, and polls for completion.
func CleanupEvalServices(ctx context.Context, client platform.Client, projectID, prefix string) error {
	if prefix == "" {
		return fmt.Errorf("cleanup: empty prefix (safety guard)")
	}
	if len(prefix) < 2 {
		return fmt.Errorf("cleanup: prefix %q too short (min 2 chars)", prefix)
	}

	services, err := ops.ListProjectServices(ctx, client, projectID)
	if err != nil {
		return fmt.Errorf("cleanup list services: %w", err)
	}

	var toDelete []platform.ServiceStack
	for _, svc := range services {
		if svc.IsSystem() {
			continue
		}
		if MatchesEvalPrefix(svc.Name, prefix) {
			toDelete = append(toDelete, svc)
		}
	}

	return deleteServices(ctx, client, toDelete)
}

// Cleanup tunables. Exposed as package vars so tests can shrink them to
// millisecond scale without changing production semantics.
var (
	// CleanupSettleTimeout caps how long pre-delete waits for transitional
	// (CREATING/NEW/STARTING) services to reach a deletable state.
	CleanupSettleTimeout = 90 * time.Second
	// CleanupVerifyMaxRetries caps the list → delete → verify loop. The
	// agent-observed friction was: cleanup said "deleting N services" and
	// the delete processes finished, yet the next scenario's first
	// discover saw a residual service stuck in CREATING. Verification
	// catches that and retries instead of silently proceeding.
	CleanupVerifyMaxRetries = 3
	// CleanupVerifyInterval is the spacing between settle/verify polls.
	CleanupVerifyInterval = 3 * time.Second
	// CleanupProcessPollInterval is the spacing between GetProcess polls
	// when waiting for a delete process to finish. Exposed for tests.
	CleanupProcessPollInterval = 3 * time.Second
)

// terminalDeletableStatuses are the service statuses where DeleteService
// is safe to call. Services in NEW/CREATING/STARTING are still being
// provisioned and the platform may reject delete with a transient error
// or leave the service half-created. DELETING is treated as "wait for
// the existing deletion to finish" — already on its way out.
var terminalDeletableStatuses = map[string]bool{
	platform.ServiceStatusActive:        true,
	platform.ServiceStatusReadyToDeploy: true,
	platform.ServiceStatusRunning:       true,
	platform.ServiceStatusFailed:        true,
	platform.ServiceStatusStopped:       true,
}

// CleanupProject performs a full project cleanup after an eval run:
//  1. Delete all services except zcp (and system services)
//  2. Clean generated files in workDir (keep protected paths)
//  3. Reset workflow state
//
// This is the deterministic equivalent of the zcp /cleanup slash command,
// saving tokens by not requiring an LLM agent. Cleanup is dynamic by design:
// the list is fetched fresh and ANY non-system, non-zcp service is deleted,
// regardless of name. Stale ServiceMeta entries from prior runs naming
// services that no longer exist are tolerated.
//
// Sentinel safety: when the env var ZCP_EVAL_SENTINEL_FILE is non-empty,
// cleanup refuses to proceed unless <workDir>/<sentinelName> exists. The
// local-mode flow-eval wrapper sets this so a misconfigured
// ZCP_EVAL_WORK_DIR (e.g. operator's source repo or $HOME) cannot trigger
// destructive cleanup. Container mode leaves the env var unset and behaves
// as before.
func CleanupProject(ctx context.Context, client platform.Client, projectID, workDir string) error {
	if err := assertWorkDirSafe(workDir); err != nil {
		return err
	}

	// 1. Delete all non-protected services with settle-wait + post-verify.
	// Cleanup runs at suite boundaries; agents observed (suite 20260504-142503)
	// that a single list+delete pass can leave a residual mid-CREATING
	// service that confuses the next scenario's first discover. The loop
	// waits for transitional services to settle, deletes, then re-lists to
	// confirm the project is actually empty before declaring success.
	if err := deleteAllUserServices(ctx, client, projectID); err != nil {
		return err
	}

	// 2. Unmount stale SSHFS entries before removing files
	unmountStaleEntries(ctx, workDir)

	// 3. Clean generated files in workDir
	cleaned, cleanErr := cleanWorkDir(workDir)
	if cleanErr != nil {
		fmt.Fprintf(os.Stderr, "  warning: cleanup work dir: %v\n", cleanErr)
	}
	if len(cleaned) > 0 {
		fmt.Fprintf(os.Stderr, "  removed %d files/dirs: %s\n", len(cleaned), strings.Join(cleaned, ", "))
	}

	// 4. Reset all workflow sessions
	stateDir := filepath.Join(workDir, ".zcp", "state")
	sessions, listErr := workflow.ListSessions(stateDir)
	if listErr == nil {
		for _, sess := range sessions {
			if err := workflow.ResetSessionByID(stateDir, sess.SessionID); err != nil {
				return fmt.Errorf("cleanup reset session %s: %w", sess.SessionID, err)
			}
		}
	}

	// 5. Remove ServiceMeta evidence. Without this, a scenario that starts
	// idle + services (adopt seed) inherits stale meta from a prior run —
	// `Bootstrapped=true` for the same hostname flips the idle scenario from
	// `adopt` to `bootstrapped` and the adopt atom never fires.
	if err := os.RemoveAll(filepath.Join(stateDir, "services")); err != nil {
		return fmt.Errorf("cleanup remove service metas: %w", err)
	}

	// 5b. Remove launch-production state. generateLaunchID is deterministic
	// over (sourceProjectID, targetProjectName), so re-running the same
	// scenario after a prior launch lands on the same state file and the
	// idempotent-resume branch (workflow_launch_production.go:166) short-
	// circuits the publish path — the agent sees "launched" against a
	// targetProjectID that the cleanup never actually re-created. Wipe the
	// state directory at scenario boundaries so each run starts from a
	// blank slate, mirroring the services/ removal one step up.
	if err := os.RemoveAll(filepath.Join(stateDir, "launch-production")); err != nil {
		return fmt.Errorf("cleanup remove launch-production state: %w", err)
	}

	// 6. Explicit CLAUDE.md removal + post-verify. cleanWorkDir already
	// removes non-protected entries (and CLAUDE.md is not protected), but
	// REFLOG persistence is the highest-leverage cross-scenario contamination
	// vector — the bootstrap workflow appends `<!-- ZEROPS:REFLOG -->` blocks
	// outside the ZCP managed-section markers, and `init.Run` preserves them
	// by design (real users want REFLOG carried across re-init). For eval-zcp,
	// a stale REFLOG can claim infrastructure exists when live discover shows
	// none of it (see plans/backlog/reflog-vs-live-discover-staleness.md).
	// This belt-and-braces step removes CLAUDE.md explicitly and fails loud
	// if it can't, so init.Run on the next scenario always starts from a
	// REFLOG-free file.
	if err := removeClaudeMD(workDir); err != nil {
		return fmt.Errorf("cleanup remove CLAUDE.md: %w", err)
	}

	return nil
}

// removeClaudeMD deletes the workdir's CLAUDE.md and verifies it is gone.
// IsNotExist on the initial Remove is benign (cleanWorkDir already handled
// it). Any other error, or a file that survives the Remove call, is a hard
// failure: the next scenario's init.Run would otherwise read stale REFLOG
// content and the agent would face a "context claims infrastructure exists
// but discover shows none of it" trap.
func removeClaudeMD(workDir string) error {
	path := filepath.Join(workDir, "CLAUDE.md")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("CLAUDE.md still present at %s after explicit remove", path)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s after remove: %w", path, err)
	}
	return nil
}

// deleteAllUserServices is the robust list → settle → delete → verify loop
// for the service-side half of CleanupProject. Returns a residual-services
// error if non-system, non-zcp services persist after CleanupVerifyMaxRetries
// attempts, so the next scenario fails loud instead of starting on dirty
// state.
func deleteAllUserServices(ctx context.Context, client platform.Client, projectID string) error {
	var lastVerifyErr error
	for attempt := 0; attempt < CleanupVerifyMaxRetries; attempt++ {
		services, err := waitForDeletableSettlement(ctx, client, projectID)
		if err != nil {
			return fmt.Errorf("cleanup wait for settlement: %w", err)
		}
		if len(services) > 0 {
			fmt.Fprintf(os.Stderr, "  deleting %d services (attempt %d)...\n", len(services), attempt+1)
			if err := deleteServices(ctx, client, services); err != nil {
				return fmt.Errorf("cleanup delete services: %w", err)
			}
		}
		verifyErr := verifyProjectEmpty(ctx, client, projectID)
		if verifyErr == nil {
			return nil
		}
		lastVerifyErr = verifyErr
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(CleanupVerifyInterval):
		}
	}
	return fmt.Errorf("cleanup: %w (after %d attempts)", lastVerifyErr, CleanupVerifyMaxRetries)
}

// waitForDeletableSettlement polls the project until every non-system,
// non-zcp service is in a terminal-deletable status, or CleanupSettleTimeout
// elapses. On timeout it returns the latest list anyway so deleteServices
// can surface specific per-service errors instead of waiting indefinitely.
func waitForDeletableSettlement(ctx context.Context, client platform.Client, projectID string) ([]platform.ServiceStack, error) {
	deadline := time.Now().Add(CleanupSettleTimeout)
	for {
		services, err := listDeletableServices(ctx, client, projectID)
		if err != nil {
			return nil, err
		}
		if isAllSettled(services) {
			return services, nil
		}
		if time.Now().After(deadline) {
			return services, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(CleanupVerifyInterval):
		}
	}
}

// isAllSettled reports true when every listed service is in a status
// DeleteService can act on directly.
func isAllSettled(services []platform.ServiceStack) bool {
	for _, svc := range services {
		if !terminalDeletableStatuses[svc.Status] {
			return false
		}
	}
	return true
}

// verifyProjectEmpty re-lists services and returns an error naming any
// residual non-system, non-zcp services. Used post-delete to catch the
// case where DeleteService said FINISHED but the platform's list still
// reports the service as live (eventual consistency window or stuck
// CREATING from a prior session that this cleanup pass didn't catch).
func verifyProjectEmpty(ctx context.Context, client platform.Client, projectID string) error {
	services, err := listDeletableServices(ctx, client, projectID)
	if err != nil {
		return err
	}
	if len(services) == 0 {
		return nil
	}
	names := make([]string, len(services))
	for i, s := range services {
		names[i] = fmt.Sprintf("%s(%s)", s.Name, s.Status)
	}
	return fmt.Errorf("residual services after delete pass: %s", strings.Join(names, ", "))
}

// assertWorkDirSafe gates destructive cleanup behind a caller-supplied
// sentinel file when ZCP_EVAL_SENTINEL_FILE names one. Empty env var =
// no gate (preserves container-mode invariant).
func assertWorkDirSafe(workDir string) error {
	sentinelName := os.Getenv("ZCP_EVAL_SENTINEL_FILE")
	if sentinelName == "" {
		return nil
	}
	sentinelPath := filepath.Join(workDir, sentinelName)
	if _, err := os.Stat(sentinelPath); err != nil {
		return fmt.Errorf("CleanupProject refuses: workdir %q lacks sentinel file %q — local-mode eval marks disposable workdirs with this file; if you're running cleanup manually, either touch the sentinel or unset ZCP_EVAL_SENTINEL_FILE", workDir, sentinelPath)
	}
	return nil
}

// IsProtectedPath returns true if the given filename should survive cleanup.
func IsProtectedPath(name string) bool {
	return protectedPaths[name]
}

// cleanWorkDir removes all non-protected files and directories from dir.
// Returns the list of removed names. Continues on individual errors,
// collecting them via errors.Join so one stale entry doesn't block the rest.
func cleanWorkDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	var removed []string
	var errs []error
	for _, entry := range entries {
		if IsProtectedPath(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			errs = append(errs, fmt.Errorf("remove %s: %w", entry.Name(), err))
			continue
		}
		removed = append(removed, entry.Name())
	}

	return removed, errors.Join(errs...)
}

// unmountStaleEntries runs lazy unmount on non-protected directories in dir.
// This handles stale SSHFS mounts left after service deletion.
// Errors are ignored — umount on a non-mount is harmless.
func unmountStaleEntries(ctx context.Context, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for _, entry := range entries {
		if !entry.IsDir() || IsProtectedPath(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		// Lazy unmount — harmless on non-mounts, cleans stale SSHFS
		_ = exec.CommandContext(ctx, "umount", "-l", path).Run()
	}
}

// listDeletableServices returns the live set of services in the project that
// cleanup should delete: every non-system service whose hostname is not the
// protected zcp control-plane container. Centralising the predicate lets
// callers re-list immediately before delete (race defense) without
// duplicating the filter.
func listDeletableServices(ctx context.Context, client platform.Client, projectID string) ([]platform.ServiceStack, error) {
	services, err := ops.ListProjectServices(ctx, client, projectID)
	if err != nil {
		return nil, err
	}
	var out []platform.ServiceStack
	for _, svc := range services {
		if svc.IsSystem() || svc.Name == ProtectedService {
			continue
		}
		out = append(out, svc)
	}
	return out, nil
}

// deleteServices deletes a list of services and polls each to completion.
// Already-deleted services are tolerated: the API may report "not found" via
// any of three paths (404 → ErrServiceNotFound, errCode like serviceNotFound /
// serviceStackNotFound on a non-404 status, or just the message text). All
// three are treated as benign skips because cleanup runs in environments
// where services can disappear between list and delete (concurrent scenarios,
// retried evals, manual mid-run cleanup). Any other error aborts the
// cleanup.
func deleteServices(ctx context.Context, client platform.Client, services []platform.ServiceStack) error {
	for _, svc := range services {
		proc, err := client.DeleteService(ctx, svc.ID)
		if err != nil {
			if isServiceAlreadyGone(err) {
				fmt.Fprintf(os.Stderr, "  service %q already gone; skipping\n", svc.Name)
				continue
			}
			return fmt.Errorf("delete %q: %w", svc.Name, err)
		}
		if proc != nil {
			if err := pollProcess(ctx, client, proc.ID); err != nil {
				return fmt.Errorf("poll delete %q: %w", svc.Name, err)
			}
		}
	}
	return nil
}

// isServiceAlreadyGone reports whether err means "the service we tried to
// delete doesn't exist anymore" — true regardless of which channel the API
// used to convey it. Substring fallback on the message is intentional: the
// platform error mapper may not always classify non-404 paths as
// ErrServiceNotFound.
func isServiceAlreadyGone(err error) bool {
	if isPlatformErrorCode(err, platform.ErrServiceNotFound) {
		return true
	}
	var pe *platform.PlatformError
	if errors.As(err, &pe) {
		apiCode := strings.ToLower(pe.APICode)
		if strings.Contains(apiCode, "notfound") {
			return true
		}
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

func isPlatformErrorCode(err error, code string) bool {
	var pe *platform.PlatformError
	return errors.As(err, &pe) && pe.Code == code
}

// cleanClaudeMemory removes Claude auto-memory files so the next eval run
// starts with a blank slate. This prevents cross-contamination between
// sequential recipe evaluations.
//
// claudeHome overrides the agent's `.claude/` config dir (which contains
// `projects/<slug>/memory/`). Empty falls back to `<user-home>/.claude` —
// safe on the zcp container where the agent has its own ephemeral home,
// destructive on a developer's Mac where it would wipe every Claude
// project's auto-memory across the whole machine. The local-mode eval
// wrapper points this at a scenario-scoped temp dir.
func cleanClaudeMemory(claudeHome string) error {
	if claudeHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("user home dir: %w", err)
		}
		claudeHome = filepath.Join(home, ".claude")
	}
	return cleanClaudeMemoryDir(filepath.Join(claudeHome, "projects"))
}

// cleanClaudeMemoryDir removes all files inside */memory/ directories under base.
// If base does not exist, it returns nil (no-op).
func cleanClaudeMemoryDir(base string) error {
	projectDirs, err := os.ReadDir(base)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read projects dir: %w", err)
	}

	for _, proj := range projectDirs {
		if !proj.IsDir() {
			continue
		}
		memDir := filepath.Join(base, proj.Name(), "memory")
		entries, err := os.ReadDir(memDir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("read memory dir %s: %w", proj.Name(), err)
		}
		for _, entry := range entries {
			if err := os.RemoveAll(filepath.Join(memDir, entry.Name())); err != nil {
				return fmt.Errorf("remove %s/%s: %w", proj.Name(), entry.Name(), err)
			}
		}
	}
	return nil
}

// pollProcess waits for a process to reach a terminal state.
func pollProcess(ctx context.Context, client platform.Client, processID string) error {
	ticker := time.NewTicker(CleanupProcessPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			proc, err := client.GetProcess(ctx, processID)
			if err != nil {
				return fmt.Errorf("poll process %s: %w", processID, err)
			}
			switch proc.Status {
			case platform.ProcessStatusFinished:
				return nil
			case platform.ProcessStatusFailed:
				reason := "unknown"
				if proc.FailReason != nil {
					reason = *proc.FailReason
				}
				return fmt.Errorf("process %s failed: %s", processID, reason)
			case platform.ProcessStatusCanceled:
				return fmt.Errorf("process %s canceled", processID)
			}
		}
	}
}
