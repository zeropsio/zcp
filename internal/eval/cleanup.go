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

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// ProtectedService is the controlling service that must never be deleted.
const ProtectedService = "zcp"

// protectedPaths are files/dirs in the working directory that survive cleanup.
// CLAUDE.md is intentionally NOT protected: each scenario regenerates it via
// init.Run after seed, so carrying stale REFLOG/service references between
// runs only pollutes the next agent's context.
var protectedPaths = map[string]bool{
	".claude":   true,
	".mcp.json": true,
	".zcp":      true,
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

	services, err := client.ListServices(ctx, projectID)
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

// CleanupProject performs a full project cleanup after an eval run:
//  1. Delete all services except zcp (and system services)
//  2. Clean generated files in workDir (keep protected paths)
//  3. Reset workflow state
//
// This is the deterministic equivalent of the zcp /cleanup slash command,
// saving tokens by not requiring an LLM agent.
func CleanupProject(ctx context.Context, client platform.Client, projectID, workDir string) error {
	// 1. Delete all non-protected services
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return fmt.Errorf("cleanup list services: %w", err)
	}

	var toDelete []platform.ServiceStack
	for _, svc := range services {
		if svc.IsSystem() || svc.Name == ProtectedService {
			continue
		}
		toDelete = append(toDelete, svc)
	}

	if len(toDelete) > 0 {
		fmt.Fprintf(os.Stderr, "  deleting %d services...\n", len(toDelete))
		if err := deleteServices(ctx, client, toDelete); err != nil {
			return fmt.Errorf("cleanup delete services: %w", err)
		}
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

// deleteServices deletes a list of services and polls each to completion.
func deleteServices(ctx context.Context, client platform.Client, services []platform.ServiceStack) error {
	for _, svc := range services {
		proc, err := client.DeleteService(ctx, svc.ID)
		if err != nil {
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

// cleanClaudeMemory removes Claude auto-memory files so the next eval run
// starts with a blank slate. This prevents cross-contamination between
// sequential recipe evaluations.
func cleanClaudeMemory() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("user home dir: %w", err)
	}
	return cleanClaudeMemoryDir(filepath.Join(home, ".claude", "projects"))
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
	ticker := time.NewTicker(3 * time.Second)
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
			case "FINISHED":
				return nil
			case "FAILED":
				reason := "unknown"
				if proc.FailReason != nil {
					reason = *proc.FailReason
				}
				return fmt.Errorf("process %s failed: %s", processID, reason)
			case "CANCELED":
				return fmt.Errorf("process %s canceled", processID)
			}
		}
	}
}
