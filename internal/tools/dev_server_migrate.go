package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// reasonCrewWorkUnsaved: the move onto the supervisor waits because a crew
// copy holds work its redeploy would lose.
const reasonCrewWorkUnsaved = "crew_work_unsaved"

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
	// The move redeploys host unasked; crew copies live outside the deploy
	// (orchestrator decision C), so it waits until their work is committed.
	dirty, err := ops.DirtyCrewCopies(ctx, d.SSH, host)
	if err != nil {
		return nil, nil, err
	}
	if len(dirty) > 0 {
		return nil, &ops.DevServerResult{
			Action: p.Action, Hostname: host, Reason: reasonCrewWorkUnsaved,
			Message: fmt.Sprintf("zcp changed nothing and did not start the dev server: moving %s onto the dev-server supervisor redeploys it, which would lose the uncommitted and untracked work in %s — commit it in each copy (or remove the copy), then start the dev server again.",
				host, strings.Join(dirty, ", ")),
		}, nil
	}
	// A variant-B dev service's repository is on the code volume: its dev
	// setup is edited there, the supervisor runs on it, and the redeploy
	// runs from it (ServiceMeta.WorkingDirFor; "" is /var/www).
	setup, root := "", supervisorRoot(d.StateDir, host)
	if meta, _ := workflow.FindServiceMeta(d.StateDir, host); meta != nil {
		setup = meta.SetupNameFor(host)
	}
	name, err := ops.DeclareDevServerForStart(ctx, d.SSH, p)
	if err != nil {
		return nil, nil, err
	}
	file, setup, _, err := ops.AdoptSupervisorInDevSetup(ctx, d.SSH, host, setup, url, root, root)
	if err != nil {
		return nil, nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("cannot move %s onto the dev-server supervisor: %v", host, err),
			"Fix zerops.yaml on "+host+" (or deploy it once with zerops_deploy setup=<dev setup>), then start the dev server again.")
	}
	m := &devServerMigration{hostname: host, setup: setup, yamlFile: file, name: name}
	deployed := redeployForSupervisor(ctx, req, d, host, setup, root)
	if deployed.Status != statusDeployed {
		return m, &ops.DevServerResult{
			Action: p.Action, Hostname: host, Reason: reasonMigrationDeployFailed,
			Message: fmt.Sprintf("The redeploy of %s that brings up the supervisor ended %s — read zerops_events and the build log. %s and %s are written; once fixed, zerops_deploy targetService=%s finishes the move and the supervisor starts the server.",
				host, deployed.Status, file, devsup.DeclarationPath, host),
		}, nil
	}
	m.warnings = deployed.Warnings
	// The start replaces what the deleted keeper kept on host, as a start
	// replaced it before.
	if err := dropKeeperRecord(d.StateDir, host); err != nil {
		m.warnings = append(m.warnings, err.Error())
	}
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

// devSetupMove is what a deploy carried onto the dev-server supervisor.
type devSetupMove struct {
	// sentence says what changed, "" when nothing did.
	sentence string
	// keptServer: the deploy declared the server the deleted keeper kept on
	// its target; the keeper record goes once the supervisor answers after
	// the deploy.
	keptServer bool
}

// settle drops the keeper record of a kept server the deploy carried once the
// supervisor answers after it (listening is deployListener's word, nil when
// no supervisor answered).
func (m devSetupMove) settle(stateDir, target string, listening []bool, result *ops.DeployResult) {
	if !m.keptServer || listening == nil {
		return
	}
	if err := dropKeeperRecord(stateDir, target); err != nil {
		result.Warnings = append(result.Warnings, err.Error())
	}
}

// superviseDevSetup puts the dev setup a self-deploy from workingDir ("" is
// /var/www) deploys under the supervisor when it is not there yet: carrying
// the server an older zcp's keeper kept on the target, else on the dev
// service's first deploy.
func superviseDevSetup(ctx context.Context, client platform.Client, projectID string, ssh ops.SSHDeployer, stateDir string, src DevsupSource, source, target, setup, workingDir string) (devSetupMove, error) {
	if source != "" && source != target {
		return devSetupMove{}, nil
	}
	kept, ok, err := readKeeperRecord(stateDir, target)
	if err != nil {
		return devSetupMove{}, err
	}
	if ok {
		return carryKeptServer(ctx, ssh, stateDir, src, target, setup, workingDir, kept)
	}
	sentence, err := superviseNewDevSetup(ctx, client, projectID, ssh, stateDir, src, source, target, setup, workingDir)
	return devSetupMove{sentence: sentence}, err
}

// carryKeptServer makes a deploy of target bring back the dev server the
// deleted keeper kept there (orchestrator decision B): without the
// supervisor, the dev setup gains its two lines and the kept server is
// declared from the record, so the supervisor this deploy starts runs it. On
// the supervisor already, the record has nothing left to bring back.
func carryKeptServer(ctx context.Context, ssh ops.SSHDeployer, stateDir string, src DevsupSource, target, setup, workingDir string, kept keeperRecord) (devSetupMove, error) {
	if ssh == nil {
		return devSetupMove{}, nil
	}
	root := supervisorRoot(stateDir, target)
	_, err := ops.ReadDevServers(ctx, ssh, target, root)
	var noSup *ops.NoSupervisorError
	switch {
	case err == nil:
		return devSetupMove{}, dropKeeperRecord(stateDir, target)
	case !errors.As(err, &noSup):
		return devSetupMove{}, fmt.Errorf("the dev server zcp kept on %s: %w", target, err)
	}
	lost := func(err error) error {
		return fmt.Errorf("the dev server zcp kept on %s does not come back after this deploy (start it with zerops_dev_server): %w", target, err)
	}
	url, err := src.URL(ctx)
	if err != nil {
		return devSetupMove{}, lost(err)
	}
	if meta, _ := workflow.FindServiceMeta(stateDir, target); meta != nil && setup == "" {
		setup = meta.SetupNameFor(target)
	}
	name, err := ops.DeclareDevServerForStart(ctx, ssh, ops.DevServerParams{
		Action: "start", Hostname: target, Root: root, Command: kept.Command, Port: kept.Port, HealthPath: kept.HealthPath,
		WaitSeconds: kept.WaitSeconds, WorkDir: kept.WorkDir, NoHTTPProbe: kept.NoHTTPProbe,
	})
	if err != nil {
		return devSetupMove{}, lost(err)
	}
	file, used, _, err := ops.AdoptSupervisorInDevSetup(ctx, ssh, target, setup, url, workingDir, root)
	if err != nil {
		return devSetupMove{}, lost(err)
	}
	return devSetupMove{
		sentence: fmt.Sprintf("Before this deploy zcp put %s under the dev-server supervisor and declared %q, the dev server zcp kept there: setup %q in %s now installs and runs zcp-devsup (run.prepareCommands, run.start) and %s declares it, so the supervisor starts it after this deploy — commit both files with your next change.",
			target, name, used, file, devsup.DeclarationPath),
		keptServer: true,
	}, nil
}

// superviseNewDevSetup writes the supervisor's two lines into the dev setup a
// dev service deploys for the first time (docs/spec-mate.md §2.8a.2), so its
// first dev-server start finds the supervisor and redeploys nothing. On a dev
// service deployed before it only re-pins the zcp-devsup line its dev setup
// already carries, so the deploy the agent asked for installs the newest
// supervisor and nothing redeploys on its own; a dev setup without the line
// stays as written (its first start migrates it). It changes nothing when the
// target is not a dev-mode dynamic runtime's self-deploy or no zcp-devsup is
// published or named yet — the dev setup keeps today's shape.
// It edits the dev setup in workingDir, the folder the deploy runs from, and a
// variant-B dev service's supervisor runs on its code volume. It returns the
// sentence saying what changed, "" when nothing did.
func superviseNewDevSetup(ctx context.Context, client platform.Client, projectID string, ssh ops.SSHDeployer, stateDir string, src DevsupSource, source, target, setup, workingDir string) (string, error) {
	if source != "" && source != target {
		return "", nil
	}
	meta, _ := workflow.FindServiceMeta(stateDir, target)
	if meta == nil {
		return "", nil
	}
	svc, err := ops.LookupService(ctx, client, projectID, target)
	if err != nil || !isDeferredStartTarget(meta, svc, target) {
		return "", nil //nolint:nilerr // not a dev-mode dynamic runtime here, or no service read: the first start migrates it
	}
	url, err := src.URL(ctx)
	if err != nil {
		return "", nil //nolint:nilerr // no zcp-devsup published or named yet: today's shape, the first start migrates it
	}
	if meta.IsDeployed() {
		file, used, from, err := ops.RepinSupervisorInDevSetup(ctx, ssh, target, setup, url, workingDir)
		if err != nil {
			return "", fmt.Errorf("dev-server supervisor: %w", err)
		}
		if from == "" {
			return "", nil
		}
		return fmt.Sprintf("Before this deploy zcp re-pinned zcp-devsup in setup %q of %s from %s to %s, so this deploy installs it on %s — commit it with your next change.",
			used, file, devsupRelease(from), devsupRelease(url), target), nil
	}
	file, used, changed, err := ops.AdoptSupervisorInDevSetup(ctx, ssh, target, setup, url, workingDir, meta.WorkingDirFor(target))
	if err != nil {
		return "", fmt.Errorf("dev-server supervisor: %w", err)
	}
	if !changed {
		return "", nil
	}
	return fmt.Sprintf("Before this first deploy zcp put %s under the dev-server supervisor: setup %q in %s now installs and runs zcp-devsup (run.prepareCommands, run.start) — commit it with your next change; zerops_dev_server action=start runs your dev server under it.",
		target, used, file), nil
}

// devsupRelease names a zcp-devsup download by its release tag, by the whole
// URL when it is no release's.
func devsupRelease(url string) string {
	_, rest, ok := strings.Cut(url, "/releases/download/")
	tag, _, found := strings.Cut(rest, "/")
	if !ok || !found || tag == "" {
		return url
	}
	return tag
}

// redeployForSupervisor self-deploys hostname with setup from workingDir ("" is
// /var/www) and records the attempt like every other deploy path.
func redeployForSupervisor(ctx context.Context, req *mcp.CallToolRequest, d DevServerDeps, host, setup, workingDir string) *ops.DeployResult {
	attempt := workflow.DeployAttempt{
		AttemptedAt: time.Now().UTC().Format(time.RFC3339),
		Setup:       setup,
		Strategy:    deployStrategyZCLILabel,
	}
	result, err := ops.DeploySSH(ctx, d.Client, d.ProjectID, d.SSH, *d.AuthInfo, host, host, setup, workingDir, "")
	if err != nil {
		attempt.Error, attempt.FailureClass = err.Error(), topology.FailureClassNetwork
		_ = workflow.RecordDeployAttempt(d.StateDir, host, attempt)
		return &ops.DeployResult{TargetService: host, Status: err.Error()}
	}
	pollDeployBuild(ctx, d.Client, d.ProjectID, result, buildProgressCallback(ctx, req), d.LogFetcher, d.SSH, d.StateDir, "")
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

// supervisorRoot is the repository root hostname's supervisor runs on: the
// code volume's mount for a variant-B dev service, "" (/var/www) otherwise.
func supervisorRoot(stateDir, hostname string) string {
	if meta, _ := workflow.FindServiceMeta(stateDir, hostname); meta != nil {
		return meta.WorkingDirFor(hostname)
	}
	return ""
}

// deployListener is "is something listening" after a deploy of hostname
// (docs/spec-workflows.md §8 O3 PA-1, MODEL §9/1b): the supervisor's word when
// the dev setup runs one — it starts every declared server on a redeploy, and
// a dev service with one declared server reports it as result.DevServer —
// else nothing, and ensurePublicAccess falls back to the runtime's class.
func deployListener(ctx context.Context, ssh ops.SSHDeployer, stateDir, hostname string, result *ops.DeployResult) []bool {
	if ssh == nil {
		return nil
	}
	ds, devServer, err := ops.DevServersAfterDeploy(ctx, ssh, hostname, supervisorRoot(stateDir, hostname))
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

// keeperRecordFile is the deleted keeper's record in the state directory:
// hostname → the server it kept there.
const keeperRecordFile = "dev-servers.json"

// keeperRecord is the part of the deleted keeper's record that declares the
// server it kept: the start the agent last made on that dev service.
type keeperRecord struct {
	Command     string `json:"command"`
	Port        int    `json:"port,omitempty"`
	HealthPath  string `json:"healthPath,omitempty"`
	WorkDir     string `json:"workDir,omitempty"`
	WaitSeconds int    `json:"waitSeconds,omitempty"`
	NoHTTPProbe bool   `json:"noHttpProbe,omitempty"`
}

// readKeeperRecords reads the keeper record whole (raw entries, so the hosts
// zcp does not touch are written back as they were); empty when there is none.
func readKeeperRecords(stateDir string) (map[string]json.RawMessage, error) {
	none := map[string]json.RawMessage{}
	if stateDir == "" {
		return none, nil
	}
	data, err := os.ReadFile(filepath.Join(stateDir, keeperRecordFile))
	if errors.Is(err, os.ErrNotExist) {
		return none, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the kept dev servers: %w", err)
	}
	var records map[string]json.RawMessage
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, fmt.Errorf("read the kept dev servers: %w", err)
	}
	return records, nil
}

// readKeeperRecord is the server the deleted keeper kept on hostname; ok is
// false when it kept none.
func readKeeperRecord(stateDir, hostname string) (rec keeperRecord, ok bool, err error) {
	records, err := readKeeperRecords(stateDir)
	if err != nil || records[hostname] == nil {
		return rec, false, err
	}
	if err := json.Unmarshal(records[hostname], &rec); err != nil {
		return rec, false, fmt.Errorf("read the dev server kept on %s: %w", hostname, err)
	}
	return rec, strings.TrimSpace(rec.Command) != "", nil
}

// dropKeeperRecord forgets the server kept on hostname once the supervisor
// runs it; the file goes with its last entry. Only this zcp writes the file
// now (the keeper is retired), and a lost race leaves an entry the next
// deploy of that host drops.
func dropKeeperRecord(stateDir, hostname string) error {
	records, err := readKeeperRecords(stateDir)
	if err != nil || records[hostname] == nil {
		return err
	}
	delete(records, hostname)
	path := filepath.Join(stateDir, keeperRecordFile)
	if len(records) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("forget the dev server kept on %s: %w", hostname, err)
		}
		return nil
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return fmt.Errorf("forget the dev server kept on %s: %w", hostname, err)
	}
	if err := os.WriteFile(path+".tmp", data, 0o600); err != nil {
		return fmt.Errorf("forget the dev server kept on %s: %w", hostname, err)
	}
	if err := os.Rename(path+".tmp", path); err != nil {
		return fmt.Errorf("forget the dev server kept on %s: %w", hostname, err)
	}
	return nil
}

// RetireDevServerKeeper removes the units an older zcp's dev-server keeper
// left on this container: dev servers run under each dev service's supervisor
// now, and one mechanism runs them. Its record stays until each host's deploy
// carries the kept server onto the supervisor (carryKeptServer).
func RetireDevServerKeeper(ctx context.Context, units ops.UnitRemover) error {
	if units == nil {
		return nil
	}
	if err := units.RemoveUnits(ctx, keeperUnitPrefix); err != nil {
		return fmt.Errorf("retire the dev-server keeper: %w", err)
	}
	return nil
}
