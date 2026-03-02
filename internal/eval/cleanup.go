package eval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// ProtectedService is the controlling service that must never be deleted.
const ProtectedService = "zcpx"

// protectedPaths are files/dirs in the working directory that survive cleanup.
var protectedPaths = map[string]bool{
	"CLAUDE.md": true,
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
//  1. Delete all services except zcpx (and system services)
//  2. Clean generated files in workDir (keep protected paths)
//  3. Reset workflow state
//
// This is the deterministic equivalent of the zcpx /cleanup slash command,
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

	// 2. Clean generated files in workDir
	cleaned, err := cleanWorkDir(workDir)
	if err != nil {
		return fmt.Errorf("cleanup work dir: %w", err)
	}
	if len(cleaned) > 0 {
		fmt.Fprintf(os.Stderr, "  removed %d files/dirs: %s\n", len(cleaned), strings.Join(cleaned, ", "))
	}

	// 3. Reset workflow state
	stateDir := filepath.Join(workDir, ".zcp", "state")
	if err := workflow.ResetSession(stateDir); err != nil {
		return fmt.Errorf("cleanup reset workflow: %w", err)
	}

	return nil
}

// IsProtectedPath returns true if the given filename should survive cleanup.
func IsProtectedPath(name string) bool {
	return protectedPaths[name]
}

// cleanWorkDir removes all non-protected files and directories from dir.
// Returns the list of removed names.
func cleanWorkDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read dir %s: %w", dir, err)
	}

	var removed []string
	for _, entry := range entries {
		if IsProtectedPath(entry.Name()) {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return removed, fmt.Errorf("remove %s: %w", entry.Name(), err)
		}
		removed = append(removed, entry.Name())
	}

	return removed, nil
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
