package ops

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// LockStage orders zcp writers of a service's stage version across processes.
// The caller holds it through its bounded wait. A waiter gives up after five
// minutes or when its context ends.
func LockStage(ctx context.Context, stateDir, serviceID string) (func(), error) {
	if stateDir == "" || serviceID == "" {
		return nil, fmt.Errorf("lock stage: state directory and service id are required")
	}
	dir := filepath.Join(stateDir, "stage-locks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("lock stage: create directory: %w", err)
	}
	key := sha256.Sum256([]byte(serviceID))
	path := filepath.Join(dir, fmt.Sprintf("%x.lock", key))
	wait, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	release, err := lockLeaseKey(wait, path)
	if err != nil {
		return nil, fmt.Errorf("lock stage %s: %w", serviceID, err)
	}
	return release, nil
}

// WaitStageVersions is called under the lock before a writer starts. A prior
// writer's bounded wait may have ended while its platform operation continued.
func WaitStageVersions(ctx context.Context, client platform.Client, projectID, serviceID string) error {
	waitCtx, cancel := context.WithTimeout(ctx, 20*time.Minute)
	defer cancel()
	processes, err := client.GetProjectProcessesDirect(waitCtx, projectID)
	if err != nil {
		return fmt.Errorf("read stage builds: %w", err)
	}
	ids, err := stageVersionProcessIDs(processes, serviceID)
	if err != nil {
		return fmt.Errorf("wait for stage versions: %w", err)
	}
	if len(ids) == 0 {
		return nil
	}
	for {
		result, err := WaitProcesses(waitCtx, client, ids, nil)
		if err != nil {
			return fmt.Errorf("wait for stage builds: %w", err)
		}
		if err := waitCtx.Err(); err != nil {
			return fmt.Errorf("wait for stage builds: %w", err)
		}
		if !result.TimedOut {
			return nil
		}
	}
}

func stageVersionProcessIDs(processes []platform.Process, serviceID string) ([]string, error) {
	var ids []string
	for _, process := range processes {
		if !isStageVersionProcess(process, serviceID) {
			continue
		}
		if process.ID == "" {
			return nil, fmt.Errorf("read stage builds: a live build has no process id")
		}
		ids = append(ids, process.ID)
	}
	return ids, nil
}

func isStageVersionProcess(process platform.Process, serviceID string) bool {
	versionOperation := process.ActionName == actionStackBuild || process.ActionName == "stack.deploy" || process.ActionName == "stack.deploy.backup"
	return IsProcessLive(process.Status) && versionOperation && touches(process, serviceID)
}
