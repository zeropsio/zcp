package eval

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// SeedEmpty cleans the project and workdir. Alias for CleanupProject; provided
// for scenario-runner symmetry with SeedImported / SeedDeployed.
func SeedEmpty(ctx context.Context, client platform.Client, projectID, workDir string) error {
	return CleanupProject(ctx, client, projectID, workDir)
}

// SeedImported cleans the project, then applies the given fixture YAML as a
// service import. The suiteID is interpolated into any `${suiteId}` placeholder
// in the fixture. Blocks until every imported stack has a completed process
// (or the context cancels / one fails).
func SeedImported(ctx context.Context, client platform.Client, projectID, fixturePath, workDir, suiteID string) error {
	yamlBytes, err := os.ReadFile(fixturePath)
	if err != nil {
		return fmt.Errorf("read fixture %q: %w", fixturePath, err)
	}

	if err := SeedEmpty(ctx, client, projectID, workDir); err != nil {
		return fmt.Errorf("seed imported: cleanup: %w", err)
	}

	yaml := strings.ReplaceAll(string(yamlBytes), "${suiteId}", suiteID)

	result, err := client.ImportServices(ctx, projectID, yaml)
	if err != nil {
		return fmt.Errorf("import services: %w", err)
	}

	for _, stack := range result.ServiceStacks {
		if stack.Error != nil {
			return fmt.Errorf("import %s failed: %s", stack.Name, stack.Error.Message)
		}
		for _, proc := range stack.Processes {
			if err := pollProcess(ctx, client, proc.ID); err != nil {
				return fmt.Errorf("wait for %s: %w", stack.Name, err)
			}
		}
	}

	return nil
}

// SeedDeployed is SeedImported plus waiting for every user service to reach
// ACTIVE status. Use for scenarios that need the runtime already provisioned
// (develop-flow and adopt-flow scenarios). Fixtures using buildFromGit are
// supported — Zerops clones + builds + deploys; the 15min wait budget covers
// the slow path. Fixtures without buildFromGit rely on natural post-import
// ACTIVE (managed services, or runtimes whose noop-start is acceptable).
func SeedDeployed(ctx context.Context, client platform.Client, projectID, fixturePath, workDir, suiteID string) error {
	if err := SeedImported(ctx, client, projectID, fixturePath, workDir, suiteID); err != nil {
		return err
	}
	return waitAllActive(ctx, client, projectID, 15*time.Minute)
}

// waitAllActive polls ListServices until every non-system service reports ACTIVE.
// Returns early if any service enters a terminal failure status.
func waitAllActive(ctx context.Context, client platform.Client, projectID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		services, err := ops.ListProjectServices(ctx, client, projectID)
		if err != nil {
			return fmt.Errorf("list services: %w", err)
		}

		allActive := true
		for _, svc := range services {
			if svc.IsSystem() {
				continue
			}
			switch svc.Status {
			case "ACTIVE":
				continue
			case "FAILED", "DELETING":
				return fmt.Errorf("service %s entered terminal status %q", svc.Name, svc.Status)
			default:
				allActive = false
			}
		}
		if allActive {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for services ACTIVE (after %s)", timeout)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
