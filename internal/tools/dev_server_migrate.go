package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// devServerMigration is what moving a dev service onto the supervisor did
// (orchestrator decision 2026-10-14 23:05, replacing "answer that the dev
// setup needs the supervisor"): nothing is asked of the user and nothing stays
// on the old SSH path.
type devServerMigration struct {
	hostname, setup, yamlFile, name string
	warnings                        []string
}

// sentence tells the agent in one sentence what changed, so it lands in the
// next change.
func (m *devServerMigration) sentence() string {
	return fmt.Sprintf("zcp moved %s onto the dev-server supervisor: setup %q in %s now installs and runs zcp-devsup (run.prepareCommands, run.start), %s declares %q, and %s was redeployed once with uncommitted work kept — commit both files with your next change.",
		m.hostname, m.setup, m.yamlFile, devsup.DeclarationPath, m.name, m.hostname)
}

// reasonMigrationDeployFailed: the redeploy that moves a dev setup onto the
// supervisor did not finish; the files are written, so a zerops_deploy after
// the fix completes it.
const reasonMigrationDeployFailed = "supervisor_redeploy_failed"

// migrateToSupervisor moves hostname's dev setup onto the supervisor and then
// starts the server under it: declares the server, adds the two lines to the
// dev setup, redeploys the dev service once from inside (the supervisor then
// starts every declared server) and answers as the start would.
func migrateToSupervisor(ctx context.Context, req *mcp.CallToolRequest, d DevServerDeps, p ops.DevServerParams) (*devServerMigration, *ops.DevServerResult, error) {
	host := p.Hostname
	if d.AuthInfo == nil {
		return nil, nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			host+"'s dev setup does not run the dev-server supervisor, and this zcp cannot redeploy it",
			"Run zcp where it can deploy (the zcp service of the project) and start the dev server again.")
	}
	url, err := d.Devsup.URL(ctx)
	if err != nil {
		return nil, nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			host+"'s dev setup does not run the dev-server supervisor yet: "+err.Error(), "")
	}
	setup := ""
	if meta, _ := workflow.FindServiceMeta(d.StateDir, host); meta != nil {
		setup = meta.SetupNameFor(host)
	}
	name, err := ops.DeclareDevServerForStart(ctx, d.SSH, p)
	if err != nil {
		return nil, nil, err
	}
	file, setup, _, err := ops.AdoptSupervisorInDevSetup(ctx, d.SSH, host, setup, url)
	if err != nil {
		return nil, nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("cannot move %s onto the dev-server supervisor: %v", host, err),
			"Fix zerops.yaml on "+host+" (or deploy it once with zerops_deploy setup=<dev setup>), then start the dev server again.")
	}
	m := &devServerMigration{hostname: host, setup: setup, yamlFile: file, name: name}
	deployed := redeployForSupervisor(ctx, req, d, host, setup)
	if deployed.Status != statusDeployed {
		return m, &ops.DevServerResult{
			Action: p.Action, Hostname: host, Reason: reasonMigrationDeployFailed,
			Message: fmt.Sprintf("The redeploy of %s that brings up the supervisor ended %s — read zerops_events and the build log. %s and %s are written; once fixed, zerops_deploy targetService=%s finishes the move and the supervisor starts the server.",
				host, deployed.Status, file, devsup.DeclarationPath, host),
		}, nil
	}
	m.warnings = deployed.Warnings
	result, err := ops.ExecuteDevServer(ctx, d.SSH, d.Client, d.ProjectID, p)
	return m, result, err
}

// DevsupSource finds the zcp-devsup a dev setup installs (ops.DevsupURL):
// Latest is the newest published release's asset, "" when it ships none (nil
// finds none); Override (ZCP_DEVSUP_URL) replaces it.
type DevsupSource struct {
	Latest   func(ctx context.Context) (string, error)
	Override string
}

// URL is the zcp-devsup download a dev setup installs: the override, else
// the newest release's asset.
func (s DevsupSource) URL(ctx context.Context) (string, error) {
	latest := ""
	if s.Override == "" && s.Latest != nil {
		found, err := s.Latest(ctx)
		if err != nil {
			return "", fmt.Errorf("find the newest zcp release: %w", err)
		}
		latest = found
	}
	url, err := ops.DevsupURL(latest, s.Override)
	if err != nil {
		return "", fmt.Errorf("devsup download: %w", err)
	}
	return url, nil
}

// superviseNewDevSetup writes the supervisor's two lines into the dev setup a
// dev service deploys for the first time (docs/spec-mate.md §2.8a.2), so its
// first dev-server start finds the supervisor and redeploys nothing. It
// changes nothing when the target is not a dev-mode dynamic runtime's
// self-deploy, was deployed before (its first start migrates it), or no
// zcp-devsup is published or named yet — the dev setup keeps today's shape.
// It returns the sentence saying what changed, "" when nothing did.
func superviseNewDevSetup(ctx context.Context, client platform.Client, projectID string, ssh ops.SSHDeployer, stateDir string, src DevsupSource, source, target, setup string) (string, error) {
	if source != "" && source != target {
		return "", nil
	}
	meta, _ := workflow.FindServiceMeta(stateDir, target)
	if meta == nil || meta.IsDeployed() {
		return "", nil
	}
	services, err := client.ListServices(ctx, projectID)
	if err != nil {
		return "", nil //nolint:nilerr // no service list: the dev setup keeps today's shape and the first start migrates it
	}
	svc, err := ops.FindService(services, target)
	if err != nil || !isDeferredStartTarget(meta, svc, target) {
		return "", nil //nolint:nilerr // not a dev-mode dynamic runtime here
	}
	url, err := src.URL(ctx)
	if err != nil {
		return "", nil //nolint:nilerr // no zcp-devsup published or named yet: today's shape, the first start migrates it
	}
	file, used, changed, err := ops.AdoptSupervisorInDevSetup(ctx, ssh, target, setup, url)
	if err != nil {
		return "", fmt.Errorf("dev-server supervisor: %w", err)
	}
	if !changed {
		return "", nil
	}
	return fmt.Sprintf("Before this first deploy zcp put %s under the dev-server supervisor: setup %q in %s now installs and runs zcp-devsup (run.prepareCommands, run.start) — commit it with your next change; zerops_dev_server action=start runs your dev server under it.",
		target, used, file), nil
}

// redeployForSupervisor self-deploys hostname with setup and records the
// attempt like every other deploy path.
func redeployForSupervisor(ctx context.Context, req *mcp.CallToolRequest, d DevServerDeps, host, setup string) *ops.DeployResult {
	attempt := workflow.DeployAttempt{
		AttemptedAt: time.Now().UTC().Format(time.RFC3339),
		Setup:       setup,
		Strategy:    deployStrategyZCLILabel,
	}
	result, err := ops.DeploySSH(ctx, d.Client, d.ProjectID, d.SSH, *d.AuthInfo, host, host, setup, "", "")
	if err != nil {
		attempt.Error, attempt.FailureClass = err.Error(), topology.FailureClassNetwork
		_ = workflow.RecordDeployAttempt(d.StateDir, host, attempt)
		return &ops.DeployResult{TargetService: host, Status: err.Error()}
	}
	pollDeployBuild(ctx, d.Client, d.ProjectID, result, buildProgressCallback(ctx, req), d.LogFetcher, d.SSH, d.StateDir)
	switch {
	case result.Status == statusDeployed:
		attempt.SucceededAt = time.Now().UTC().Format(time.RFC3339)
	case result.TimedOut:
		attempt.Error = deployBuildInFlightMsg
	default:
		attempt.Error = "deploy status " + result.Status
		attempt.FailureClass = classifyDeployStatus(result.Status)
	}
	attempt.SHA, attempt.AppVersionID, attempt.Dirty = result.SHA, result.AppVersionID, result.Dirty
	_ = workflow.RecordDeployAttempt(d.StateDir, host, attempt)
	return result
}

// deployListener is "is something listening" after a deploy of hostname
// (docs/spec-workflows.md §8 O3 PA-1, MODEL §9/1b): the supervisor's word when
// the dev setup runs one — it starts every declared server on a redeploy, and
// a dev service with one declared server reports it as result.DevServer —
// else nothing, and ensurePublicAccess falls back to the runtime's class.
func deployListener(ctx context.Context, ssh ops.SSHDeployer, hostname string, result *ops.DeployResult) []bool {
	if ssh == nil {
		return nil
	}
	ds, devServer, err := ops.DevServersAfterDeploy(ctx, ssh, hostname)
	if err != nil {
		var noSup *ops.NoSupervisorError
		if !errors.As(err, &noSup) {
			result.Warnings = append(result.Warnings, "dev servers: "+err.Error())
		}
		return nil
	}
	result.DevServer = devServer
	return []bool{ds.Listening()}
}

// keeperUnitPrefix names the units the deleted dev-server keeper ran on the
// zcp container (zerops@devservers-<hash>).
const keeperUnitPrefix = "devservers-"

// keptDevServersFile is the deleted keeper's record in the state directory.
const keptDevServersFile = "dev-servers.json"

// RetireDevServerKeeper removes what an older zcp's dev-server keeper left on
// this container — its units and its record of kept servers: dev servers run
// under each dev service's supervisor now, and one mechanism runs them.
func RetireDevServerKeeper(ctx context.Context, units ops.UnitRemover, stateDir string) error {
	var errs []error
	if stateDir != "" {
		if err := os.Remove(filepath.Join(stateDir, keptDevServersFile)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove kept dev servers: %w", err))
		}
	}
	if units != nil {
		if err := units.RemoveUnits(ctx, keeperUnitPrefix); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
