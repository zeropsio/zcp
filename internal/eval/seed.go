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

// SeedBuilding cleans the project, imports the fixture (which MUST use
// buildFromGit), and returns WHILE the first build is still RUNNING — it does
// NOT poll the build to completion. The spawned agent therefore boots mid-build:
// its first zerops_discover lands on a service that is actively building yet
// reads status=READY_TO_DEPLOY (the recipe-first-deploy race). To make the race
// deterministic it waits only until a stack.build process is RUNNING (the
// in-flight window is then open) before returning. Import-time rejections fail
// fast; a build that settles before it is observed RUNNING is tolerated (the
// scenario simply won't exercise the race that run).
func SeedBuilding(ctx context.Context, client platform.Client, projectID, fixturePath, workDir, suiteID string) error {
	yamlBytes, err := os.ReadFile(fixturePath)
	if err != nil {
		return fmt.Errorf("read fixture %q: %w", fixturePath, err)
	}
	if err := SeedEmpty(ctx, client, projectID, workDir); err != nil {
		return fmt.Errorf("seed building: cleanup: %w", err)
	}
	yaml := strings.ReplaceAll(string(yamlBytes), "${suiteId}", suiteID)
	result, err := client.ImportServices(ctx, projectID, yaml)
	if err != nil {
		return fmt.Errorf("import services: %w", err)
	}

	var buildProc string
	for _, stack := range result.ServiceStacks {
		if stack.Error != nil {
			return fmt.Errorf("import %s failed: %s", stack.Name, stack.Error.Message)
		}
		for _, proc := range stack.Processes {
			if proc.ActionName == actionStackBuild && buildProc == "" {
				buildProc = proc.ID
			}
		}
	}
	if buildProc == "" {
		return fmt.Errorf("seed building: fixture produced no stack.build process — ModeBuilding requires a buildFromGit fixture")
	}
	// Wait only until the build is RUNNING (window open), then return — leaving
	// the build in flight for the agent to discover.
	return waitProcessRunning(ctx, client, buildProc, 90*time.Second)
}

// actionStackBuild is the Zerops API action name for the build pipeline.
const actionStackBuild = "stack.build"

// waitProcessRunning blocks until the process reaches RUNNING (returns nil — the
// in-flight window is open) or settles to a terminal status (returns nil — too
// fast to observe in flight, not a seed error). Errors only on API failure or
// timeout. The poll is short (build RUNNING within seconds of import).
func waitProcessRunning(ctx context.Context, client platform.Client, processID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		proc, err := client.GetProcess(ctx, processID)
		if err != nil {
			return fmt.Errorf("poll build process %s: %w", processID, err)
		}
		switch proc.Status {
		case platform.ProcessStatusRunning:
			return nil
		case platform.ProcessStatusFinished, platform.ProcessStatusFailed, platform.ProcessStatusCanceled:
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("build process %s did not reach RUNNING within %s", processID, timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
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

// SeedSettled imports the fixture, then polls ListServices until every
// non-system service reaches a terminal status (ACTIVE/FAILED/READY_TO_DEPLOY/
// RUNNING/STOPPED). Unlike SeedDeployed, FAILED is treated as a valid settled
// state — used by recovery scenarios that intentionally seed a broken runtime
// to test the agent's FAILED-state recovery surface.
//
// Differs from SeedImported in two ways: import-side processes ending in
// FAILED are tolerated (a runtime crashing on init is exactly the seed goal),
// and ListServices is polled afterwards because buildFromGit fixtures keep
// transitioning past the import-process FINISHED boundary.
func SeedSettled(ctx context.Context, client platform.Client, projectID, fixturePath, workDir, suiteID string) error {
	yamlBytes, err := os.ReadFile(fixturePath)
	if err != nil {
		return fmt.Errorf("read fixture %q: %w", fixturePath, err)
	}
	if err := SeedEmpty(ctx, client, projectID, workDir); err != nil {
		return fmt.Errorf("seed settled: cleanup: %w", err)
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
			if err := pollProcessSettled(ctx, client, proc.ID); err != nil {
				return fmt.Errorf("wait for %s: %w", stack.Name, err)
			}
		}
	}
	return waitAllSettled(ctx, client, projectID, 15*time.Minute)
}

// pollProcessSettled mirrors pollProcess but treats every terminal process
// status (FINISHED / FAILED / CANCELED) as success. SeedSettled relies on
// waitAllSettled to verify the actual end state via ListServices, so this
// helper only blocks until the import-side process stops transitioning.
//
// Empirically buildFromGit + missing managed dep can land on either FAILED
// (leaf-process direct failure) or CANCELED (parent process aborted because
// a child deploy failed); tolerating both keeps the seed deterministic for
// recovery scenarios that produce a known-broken topology on purpose.
func pollProcessSettled(ctx context.Context, client platform.Client, processID string) error {
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
			case platform.ProcessStatusFinished, platform.ProcessStatusFailed, platform.ProcessStatusCanceled:
				return nil
			}
		}
	}
}

// waitAllSettled polls ListServices until every non-system service reports
// a terminal (non-transitional) status. Differs from waitAllActive by NOT
// aborting on FAILED. Set lifted from cleanup.go::terminalDeletableStatuses
// so the predicate stays in sync with what cleanup considers safe to operate on.
func waitAllSettled(ctx context.Context, client platform.Client, projectID string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		services, err := ops.ListProjectServices(ctx, client, projectID)
		if err != nil {
			return fmt.Errorf("list services: %w", err)
		}
		allSettled := true
		for _, svc := range services {
			if svc.IsSystem() {
				continue
			}
			if !isTerminalServiceStatus(svc.Status) {
				allSettled = false
				break
			}
		}
		if allSettled {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for services to settle (after %s)", timeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func isTerminalServiceStatus(s string) bool {
	switch s {
	case platform.ServiceStatusActive,
		platform.ServiceStatusReadyToDeploy,
		platform.ServiceStatusRunning,
		platform.ServiceStatusFailed,
		platform.ServiceStatusStopped:
		return true
	}
	return false
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
			case platform.ServiceStatusActive:
				continue
			case platform.ServiceStatusFailed, "DELETING":
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
