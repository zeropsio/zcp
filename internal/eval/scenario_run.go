package eval

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// seedScenario runs the seed mode specified by the scenario. Fixture paths are
// resolved relative to the scenario file.
func (r *Runner) seedScenario(ctx context.Context, sc *Scenario, suiteID string) error {
	switch sc.Seed {
	case ModeEmpty:
		return SeedEmpty(ctx, r.client, r.projectID, r.config.WorkDir)
	case ModeImported:
		fixture := resolveFixturePath(sc)
		return SeedImported(ctx, r.client, r.projectID, fixture, r.config.WorkDir, suiteID)
	case ModeDeployed:
		fixture := resolveFixturePath(sc)
		return SeedDeployed(ctx, r.client, r.projectID, fixture, r.config.WorkDir, suiteID)
	case ModeSettled:
		fixture := resolveFixturePath(sc)
		return SeedSettled(ctx, r.client, r.projectID, fixture, r.config.WorkDir, suiteID)
	case ModeBuilding:
		fixture := resolveFixturePath(sc)
		return SeedBuilding(ctx, r.client, r.projectID, fixture, r.config.WorkDir, suiteID)
	default:
		return fmt.Errorf("unknown seed mode %q", sc.Seed)
	}
}

// runPreseedScript executes the scenario's PreseedScript, if any, with CWD set
// to the eval workdir. The script receives ZCP_SCENARIO_ID, ZCP_SUITE_ID, and
// ZCP_WORK_DIR in env so it can write state files deterministically without
// hardcoding paths. No-op when PreseedScript is empty — most scenarios don't
// need any pre-population beyond what SeedImported / SeedDeployed provides.
func (r *Runner) runPreseedScript(ctx context.Context, sc *Scenario, suiteID string) error {
	if sc.PreseedScript == "" {
		return nil
	}
	path := sc.PreseedScript
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(sc.SourcePath), path)
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("preseed script %q: %w", path, err)
	}
	cmd := exec.CommandContext(ctx, "bash", path)
	cmd.Dir = r.config.WorkDir
	cmd.Env = append(os.Environ(),
		"ZCP_SCENARIO_ID="+sc.ID,
		"ZCP_SUITE_ID="+suiteID,
		"ZCP_WORK_DIR="+r.config.WorkDir,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("preseed script: %w\n--- output ---\n%s", err, string(out))
	}
	return nil
}

// resolveFixturePath resolves fixture path relative to the scenario source.
// Absolute paths are returned unchanged.
func resolveFixturePath(sc *Scenario) string {
	if filepath.IsAbs(sc.Fixture) {
		return sc.Fixture
	}
	return filepath.Join(filepath.Dir(sc.SourcePath), sc.Fixture)
}
