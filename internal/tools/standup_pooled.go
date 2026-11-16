package tools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// A pooled part — a dev pool and its stages, keyed by the pool — is stood up
// by commit deploys from zcp (Karel, 2026-11-16: birth by commit deploy from
// zcp): main's head as HQ holds it goes into every pool and stage of the part
// that has no build yet, all at once, through ops.CommitDeploy. No container
// holds a checkout for it and no lease is taken: a pool's first image needs
// none, and a stage's first build comes from the same commit. A stage that
// has a build is never written — the stage is the agent's test bench, and
// nothing returns it to main.
//
// What happened is the verdict (mate.StandupVerdict): a row per pooled
// service, derived by ops.StandupRowOf from the service's app version named
// by the commit, its process, the pool's bearer and the class of a failure
// met on the way, and the headline ops.StandupHeadlineOf derives from them.
// The status file's stand-up section carries it as written; the run card
// shows it and the Mate server wakes the agent by it.
//
// The section's run state follows the verdict, so the two never disagree:
// running while the run lives (headline building or ready); at its end done
// when the headline is done, else failed. A row still building when the
// run's wait ends is failed as unreachable, naming the wait, so a run never
// ends with a row in flight.

const (
	// standupHQWait bounds the wait on an HQ that does not answer (F1).
	standupHQWait = 2 * time.Minute
	// standupHQRetry is how often a wait on HQ asks again.
	standupHQRetry = 10 * time.Second
	// standupLockFile is the one-run-per-project lock, in the state dir.
	standupLockFile = "standup.lock"
)

// pooledService is one pool or stage of a pooled part through the run.
type pooledService struct {
	pair  *standupPair
	host  string
	kind  mate.StandupRowKind
	setup string
}

// pooledServices are the pooled pairs' services, pool first, then stages.
func pooledServices(pairs []*standupPair) []pooledService {
	out := make([]pooledService, 0, 2*len(pairs))
	for _, sp := range pairs {
		out = append(out, pooledService{pair: sp, host: sp.pair.Dev.Hostname, kind: mate.RowPool, setup: sp.pair.Dev.Setup})
		for _, st := range sp.pair.Stages {
			out = append(out, pooledService{pair: sp, host: st.Hostname, kind: mate.RowStage, setup: st.Setup})
		}
	}
	return out
}

// pooledPairs are the pairs keyed by their part's dev pool.
func pooledPairs(pairs []*standupPair) (classic, pooled []*standupPair) {
	for _, sp := range pairs {
		if sp.pair.DevPool {
			pooled = append(pooled, sp)
		} else {
			classic = append(classic, sp)
		}
	}
	return classic, pooled
}

// standupVerdictRows holds the run's row inputs and writes the verdict at
// each change: rows only through ops.StandupRowOf, the headline only through
// ops.StandupHeadlineOf.
type standupVerdictRows struct {
	status   *standupStatus
	mu       sync.Mutex
	order    []string
	inputs   map[string]ops.StandupRowInput
	services map[string]pooledService
}

func newStandupVerdictRows(status *standupStatus, services []pooledService) *standupVerdictRows {
	r := &standupVerdictRows{status: status, inputs: map[string]ops.StandupRowInput{}, services: map[string]pooledService{}}
	for _, s := range services {
		r.order = append(r.order, s.host)
		r.inputs[s.host] = ops.StandupRowInput{Hostname: s.host, Kind: s.kind}
		r.services[s.host] = s
	}
	return r
}

// set changes one service's input and writes the verdict.
func (r *standupVerdictRows) set(host string, change func(*ops.StandupRowInput)) {
	r.mu.Lock()
	in := r.inputs[host]
	change(&in)
	r.inputs[host] = in
	verdict := r.verdictLocked()
	r.mu.Unlock()
	r.status.verdict(verdict)
}

// verdict is the verdict as the inputs now stand.
func (r *standupVerdictRows) verdict() mate.StandupVerdict {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.verdictLocked()
}

func (r *standupVerdictRows) verdictLocked() mate.StandupVerdict {
	rows := make([]mate.StandupRow, 0, len(r.order))
	poolDone := map[string]bool{}
	for _, host := range r.order {
		row := ops.StandupRowOf(r.inputs[host])
		if row.Kind == mate.RowPool && row.State == mate.RowDone {
			poolDone[host] = true
		}
		rows = append(rows, row)
	}
	for i := range rows {
		rows[i].Next = standupRowNext(rows[i], r.services[rows[i].Hostname], poolDone)
	}
	return mate.StandupVerdict{Headline: ops.StandupHeadlineOf(rows), Rows: rows}
}

// standupRowNext is the one next step for a row: nothing while it builds;
// for a row done, how it is used; for a row failed, the way to fix it that
// works with what stands — a stage of a leasable pool is fixed from a lease
// (F6), anything else through main, which the next zerops_standup builds.
func standupRowNext(row mate.StandupRow, s pooledService, poolDone map[string]bool) string {
	pool := row.Hostname
	if s.pair != nil {
		pool = s.pair.pair.Dev.Hostname
	}
	again := "then call zerops_standup again: it builds main's new head into what has no build and skips what is done."
	switch {
	case row.State == mate.RowBuilding:
		return ""
	case row.State == mate.RowDone && row.Kind == mate.RowPool:
		return fmt.Sprintf("Ready to work: lease a container of %s on the branch you work on — zerops_lease action=\"acquire\" pool=%q branch=\"<your branch>\".", pool, pool)
	case row.State == mate.RowDone:
		return fmt.Sprintf("It runs main as this Mate's preview: verify it with zerops_verify serviceHostname=%q.", row.Hostname)
	case row.Class == platform.ClassUnreachable:
		return "Nothing is lost: call zerops_standup again — it waits on what is still in flight and never builds twice."
	case row.Kind == mate.RowStage && poolDone[pool]:
		return fmt.Sprintf("Fix it from a lease: lease %s on a branch (zerops_lease action=\"acquire\" pool=%q branch=\"<branch>\"), fix the build there, deploy it with zerops_deploy sourceService=\"<the lease's alias>\" targetService=%q setup=%q, and propose the fix to main.",
			pool, pool, row.Hostname, s.setup)
	case row.Class == platform.ClassRefused:
		return "Change what the cause names — the repository's zerops.yaml on a branch proposed to main, or what HQ or Zerops refused — " + again
	default:
		return fmt.Sprintf("Fix the build on a branch: check main out on zcp's own container through HQ, fix what the cause names, push the branch and propose it to main; once it is merged, %s", again)
	}
}

// pooledOutcome is what the pooled run hands the answer: the verdict, the
// commit each repository was built from, and main's head where it moved
// during the run (F13).
type pooledOutcome struct {
	verdict mate.StandupVerdict
	// built and ahead are by repository (appId/name).
	built, ahead map[string]string
}

// startPooledRows writes the first verdict, before any wait: a row per
// pooled service, building, a service the import is still creating saying so
// (F2).
func (d standupDeps) startPooledRows(ctx context.Context, tier workflow.MateTier) *standupVerdictRows {
	var pairs []*standupPair
	for _, p := range tier.Pairs {
		if p.DevPool {
			pairs = append(pairs, &standupPair{pair: p})
		}
	}
	if len(pairs) == 0 {
		return nil
	}
	rows := newStandupVerdictRows(d.status, pooledServices(pairs))
	live, _ := d.liveServices(ctx)
	for _, host := range rows.order {
		if svc := live[host]; svc != nil && serviceBeingCreated(svc.Status) {
			rows.set(host, func(in *ops.StandupRowInput) { in.Cause = host + " is still being created (" + svc.Status + ")" })
		}
	}
	d.status.verdict(rows.verdict())
	return rows
}

// serviceBeingCreated is a service the import has not finished creating.
func serviceBeingCreated(status string) bool {
	return status == platform.ServiceStatusNew || status == "CREATING"
}

// buildPooled builds every pooled service that has no build from main's head,
// in parallel, and confirms each pool's bearer. pairs went through
// preparePairs: a pair stopped there gets its rows failed with what stopped
// it; the others go on.
func (d standupDeps) buildPooled(ctx context.Context, hqc hq.Client, pairs []*standupPair, live map[string]*platform.ServiceStack, rows *standupVerdictRows, progress *standupProgress) pooledOutcome {
	out := pooledOutcome{built: map[string]string{}, ahead: map[string]string{}}
	services := pooledServices(pairs)
	if fresh, err := d.liveServices(ctx); err == nil {
		live = fresh
	}
	// The bearer reference is written at adoption, before any deploy: for a
	// pool with no image its writer writes it and answers at once, and it
	// resolves by itself during the pool's first successful deploy (probe
	// P-B). Its outcome here is not the verdict; the confirmation after the
	// build is.
	for _, sp := range pairs {
		pool := sp.pair.Dev.Hostname
		if sp.failed == "" && !live[pool].HasDeployedCode() {
			go func() {
				if _, err := ops.EnsureBearer(ctx, d.leaseDeps(), pool); err != nil {
					progress.say(fmt.Sprintf("%s's bearer is written; it resolves with the pool's first image (%v)", pool, err))
				}
			}()
		}
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, sp := range pairs {
		if sp.failed != "" {
			cause := sp.failed
			if sp.missing {
				cause = "nothing to adopt yet: " + cause
			}
			for _, s := range services {
				if s.pair == sp {
					rows.set(s.host, func(in *ops.StandupRowInput) { in.Class, in.Cause = sp.failedClass(), cause })
				}
			}
			continue
		}
		wg.Go(func() {
			commit, ahead := d.buildPooledPair(ctx, hqc, sp, services, live, rows, progress)
			mu.Lock()
			defer mu.Unlock()
			if commit != "" {
				out.built[sp.repository] = commit
			}
			if ahead != "" {
				out.ahead[sp.repository] = ahead
			}
		})
	}
	wg.Wait()
	out.verdict = rows.verdict()
	return out
}

// buildPooledPair builds one pooled part: reads main's head once, checks the
// zerops.yaml at it names each service's setup (F4), commit-deploys it to
// every service of the part with no build, waits for each build, and reads
// main again (F13). Answers the commit built and main's head when it moved.
func (d standupDeps) buildPooledPair(ctx context.Context, hqc hq.Client, sp *standupPair, services []pooledService, live map[string]*platform.ServiceStack, rows *standupVerdictRows, progress *standupProgress) (built, ahead string) {
	var mine []pooledService
	for _, s := range services {
		if s.pair == sp {
			mine = append(mine, s)
		}
	}
	failAll := func(class platform.ErrorClass, cause string) {
		for _, s := range mine {
			if !live[s.host].HasDeployedCode() {
				rows.set(s.host, func(in *ops.StandupRowInput) { in.Class, in.Cause = class, cause })
			}
		}
	}
	var toBuild []pooledService
	for _, s := range mine {
		if svc := live[s.host]; svc != nil && svc.HasDeployedCode() {
			d.keepBuilt(ctx, s, svc, rows)
			continue
		}
		toBuild = append(toBuild, s)
	}
	if len(toBuild) == 0 {
		return "", ""
	}

	scratch, err := hqc.OpenScratch(ctx, sp.appID(), sp.pair.RepoName)
	if err != nil {
		failAll(hqClassOf(err), fmt.Sprintf("could not open %s in HQ: %v", sp.repository, err))
		return "", ""
	}
	defer scratch.Close()
	commit, found, err := scratch.FetchTip(ctx, hqBase)
	switch {
	case err != nil:
		failAll(hqClassOf(err), fmt.Sprintf("could not read %s's %s from HQ: %v", sp.repository, hqBase, err))
		return "", ""
	case !found:
		failAll(platform.ClassRefused, fmt.Sprintf("%s has no %s branch in HQ", sp.repository, hqBase))
		return "", ""
	}
	setups, yamlName, err := zeropsYamlSetups(ctx, scratch, commit)
	if err != nil {
		failAll(platform.ClassRefused, fmt.Sprintf("%s at %s: %v", sp.repository, shortCommit(commit), err))
		return "", ""
	}

	var wg sync.WaitGroup
	for _, s := range toBuild {
		if !slices.Contains(setups, s.setup) {
			rows.set(s.host, func(in *ops.StandupRowInput) {
				in.Commit, in.Class = commit, platform.ClassRefused
				in.Cause = fmt.Sprintf("%s at %s in %s has no setup %q, which %s builds by", yamlName, shortCommit(commit), sp.repository, s.setup, s.host)
			})
			continue
		}
		wg.Go(func() { d.commitDeployOne(ctx, sp, s, live[s.host], commit, rows, progress) })
	}
	wg.Wait()

	if head, found, err := scratch.RemoteHead(ctx, hqBase); err == nil && found && head != commit {
		progress.say(fmt.Sprintf("%s's %s moved to %s during the stand-up; %s was built, and main is ahead of it", sp.repository, hqBase, shortCommit(head), shortCommit(commit)))
		ahead = head
	}
	return commit, ahead
}

// keepBuilt is a service that has a build: never written, its row read from
// the version it runs, and for a pool its bearer confirmed.
func (d standupDeps) keepBuilt(ctx context.Context, s pooledService, svc *platform.ServiceStack, rows *standupVerdictRows) {
	rows.set(s.host, func(in *ops.StandupRowInput) {
		in.AppVersionID, in.AppVersionStatus = svc.ActiveAppVersion.ID, platform.ServiceStatusActive
		in.ProcessStatus = platform.ProcessStatusFinished
	})
	if s.kind == mate.RowPool {
		d.confirmBearer(ctx, s.host, rows)
	}
}

// commitDeployOne submits one commit deploy and follows it to its end: the
// row building with its version and process, then done or failed from the
// version's status, a failure with its build log's last lines (F5).
func (d standupDeps) commitDeployOne(ctx context.Context, sp *standupPair, s pooledService, svc *platform.ServiceStack, commit string, rows *standupVerdictRows, progress *standupProgress) {
	progress.say(fmt.Sprintf("building %s at %s into %s", sp.repository, shortCommit(commit), s.host))
	rows.set(s.host, func(in *ops.StandupRowInput) { in.Commit, in.Class, in.Cause = commit, "", "" })
	res, err := ops.CommitDeploy(ctx, d.batch.client, d.source, ops.CommitDeployRequest{
		ServiceID: svc.ID, Hostname: s.host, Setup: s.setup, Repository: sp.repository, Commit: commit,
	})
	if err != nil {
		rows.set(s.host, func(in *ops.StandupRowInput) {
			in.Class, in.Cause = platform.ClassOf(err), fmt.Sprintf("the commit deploy of %s into %s did not start: %v", shortCommit(commit), s.host, err)
		})
		return
	}
	rows.set(s.host, func(in *ops.StandupRowInput) {
		in.AppVersionID, in.ProcessID, in.ProcessStatus = res.AppVersionID, res.ProcessID, platform.ProcessStatusRunning
	})
	waited, err := ops.WaitProcesses(ctx, d.batch.client, []string{res.ProcessID}, nil)
	var process ops.ProcessStatusResult
	switch {
	case err != nil:
		rows.set(s.host, func(in *ops.StandupRowInput) {
			in.Class, in.Cause = platform.ClassOf(err), fmt.Sprintf("waiting on %s's build failed: %v", s.host, err)
		})
		return
	case waited.TimedOut || len(waited.Processes) == 0:
		rows.set(s.host, func(in *ops.StandupRowInput) {
			in.Class = platform.ClassUnreachable
			in.Cause = fmt.Sprintf("%s still builds past the stand-up's wait (process %s); a second zerops_standup waits on it, never a second build", s.host, res.ProcessID)
		})
		return
	default:
		process = waited.Processes[0]
	}
	version := d.appVersion(ctx, svc.ID, res.AppVersionID)
	rows.set(s.host, func(in *ops.StandupRowInput) {
		in.ProcessStatus = process.Status
		if version != nil {
			in.AppVersionStatus = version.Status
		}
		if process.Status != platform.ProcessStatusFinished {
			in.Class, in.Cause = platform.ClassFailed, d.buildFailure(ctx, s.host, process, version)
		}
	})
	if process.Status != platform.ProcessStatusFinished {
		return
	}
	if _, _, err := workflow.RecordExternalDeploy(d.batch.stateDir, s.host); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: stand-up: record the first deploy of %s: %v\n", s.host, err)
	}
	if s.kind == mate.RowPool {
		d.confirmBearer(ctx, s.host, rows)
	}
}

// appVersion is the service's version id, read lag-free; nil when unread.
func (d standupDeps) appVersion(ctx context.Context, serviceID, id string) *platform.AppVersionEvent {
	versions, err := d.batch.client.ListServiceAppVersions(ctx, serviceID)
	if err != nil {
		return nil
	}
	for i := range versions {
		if versions[i].ID == id {
			return &versions[i]
		}
	}
	return nil
}

// standupBuildLogLines is how many of a failed build's last log lines its
// row's cause carries.
const standupBuildLogLines = 5

// buildFailure is a failed build in the source's words: the process's fail
// reason, the version's status and its build log's last lines.
func (d standupDeps) buildFailure(ctx context.Context, host string, process ops.ProcessStatusResult, version *platform.AppVersionEvent) string {
	parts := []string{fmt.Sprintf("%s's build ended %s", host, process.Status)}
	if version != nil {
		parts[0] += " (" + version.Status + ")"
	}
	if process.FailReason != nil && *process.FailReason != "" {
		parts = append(parts, *process.FailReason)
	}
	if version != nil && d.batch.logFetcher != nil {
		if _, tail := ops.FetchBuildLogTail(ctx, d.batch.client, d.batch.logFetcher, d.batch.projectID, version, 200, standupBuildLogLines); len(tail) > 0 {
			parts = append(parts, "build log: "+strings.Join(tail, " | "))
		}
	}
	return strings.Join(parts, "; ")
}

// confirmBearer reads the pool's bearer through its one writer, which waits a
// bounded time for the reference to resolve (F10): the row is done only once
// it does.
func (d standupDeps) confirmBearer(ctx context.Context, pool string, rows *standupVerdictRows) {
	bearer, err := ops.EnsureBearer(ctx, d.leaseDeps(), pool)
	rows.set(pool, func(in *ops.StandupRowInput) {
		in.BearerResolves = err == nil && bearer != ""
		switch {
		case err != nil:
			in.Class, in.Cause = platform.ClassOf(err), err.Error()
		case bearer == "":
			in.Class = platform.ClassFailed
			in.Cause = fmt.Sprintf("%s_LEASE_TOKEN on this zcp service is written and does not resolve yet: it resolves with %s's first successful build", pool, pool)
		}
	})
}

// leaseDeps is what the bearer's writer reads the pool through.
func (d standupDeps) leaseDeps() ops.LeaseDeps {
	return ops.LeaseDeps{
		Client: d.batch.client, ProjectID: d.batch.projectID, StateDir: d.batch.stateDir,
		Swarm: ops.NewSwarmFactory(&http.Client{Timeout: ops.LeaseCallTimeout}),
	}
}

// zeropsYamlSetups are the setups the repository's zerops.yaml names at
// commit, and the file's name.
func zeropsYamlSetups(ctx context.Context, scratch *hq.Scratch, commit string) ([]string, string, error) {
	const yamlName = "zerops.yaml"
	for _, name := range []string{yamlName, "zerops.yml"} {
		body, found, err := scratch.File(ctx, commit, name)
		if err != nil {
			return nil, name, fmt.Errorf("could not read %s: %w", name, err)
		}
		if !found {
			continue
		}
		var doc struct {
			Zerops []struct {
				Setup string `yaml:"setup"`
			} `yaml:"zerops"`
		}
		if err := yaml.Unmarshal([]byte(body), &doc); err != nil {
			return nil, name, fmt.Errorf("%s does not parse: %w", name, err)
		}
		setups := make([]string, 0, len(doc.Zerops))
		for _, z := range doc.Zerops {
			setups = append(setups, z.Setup)
		}
		return setups, name, nil
	}
	return nil, yamlName, errors.New("it has no " + yamlName)
}

// hqClassOf is an HQ call's failure as an error class: HQ's refusal is
// refused (a 5xx or 429 among them is unreachable), an HQ that did not answer
// is unreachable; anything else is platform.ClassOf's.
func hqClassOf(err error) platform.ErrorClass {
	var refused *hq.RefusedError
	var unavailable *hq.UnavailableError
	switch {
	case err == nil:
		return ""
	case errors.As(err, &unavailable):
		return platform.ClassUnreachable
	case errors.As(err, &refused) && (refused.Status >= 500 || refused.Status == http.StatusTooManyRequests):
		return platform.ClassUnreachable
	case errors.As(err, &refused):
		return platform.ClassRefused
	}
	return platform.ClassOf(err)
}

// askHQ makes one HQ call, waiting on an HQ that does not answer for up to
// standupHQWait (F1); a refusal is answered at once (F12).
func (d standupDeps) askHQ(ctx context.Context, progress *standupProgress, what string, call func(context.Context) error) error {
	wait := d.hqWait
	if wait == 0 {
		wait = standupHQWait
	}
	deadline := time.Now().Add(wait)
	for {
		err := call(ctx)
		if err == nil || !hqClassOf(err).WaitedOn() || !time.Now().Before(deadline) {
			return err
		}
		progress.say(fmt.Sprintf("HQ does not answer %s yet (%v); asking again", what, err))
		select {
		case <-ctx.Done():
			return err
		case <-time.After(standupHQRetry):
		}
	}
}

// holdStandup takes the project's one stand-up (F9): false when another run —
// zcp's boot run, or an agent's call — holds it.
func holdStandup(stateDir string) (release func(), held bool, err error) {
	if stateDir == "" {
		return func() {}, true, nil
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return nil, false, fmt.Errorf("stand-up lock: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(stateDir, standupLockFile), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, fmt.Errorf("stand-up lock: %w", err)
	}
	ok, err := tryLockFile(f)
	if err != nil || !ok {
		_ = f.Close()
		return nil, false, err
	}
	return func() {
		_ = unlockFile(f)
		_ = f.Close()
	}, true, nil
}

// runningStandupLine is what a second run says of the first, from the status
// file: when it started and its verdict so far.
func runningStandupLine(statusPath string) string {
	line := "A stand-up already runs in this project — the one zcp starts at its boot, or another call — so this call started none."
	st, err := mate.ReadStatus(statusPath)
	if err != nil {
		return line + " Call zerops_standup again once it has ended."
	}
	line += " It started at " + st.Standup.StartedAt
	if v := st.Standup.Verdict; v != nil {
		var rows []string
		for _, r := range v.Rows {
			row := r.Hostname + " " + string(r.State)
			if r.Cause != "" {
				row += " (" + r.Cause + ")"
			}
			rows = append(rows, row)
		}
		line += fmt.Sprintf("; its verdict so far is %s: %s", v.Headline, strings.Join(rows, ", "))
	}
	return line + ". Call zerops_standup again once it has ended: it then reports the finished verdict and builds nothing twice."
}

// projectHasPool reports whether the project holds a Swarm pool — the only
// projects whose stand-up runs at boot.
func projectHasPool(live map[string]*platform.ServiceStack) bool {
	for _, svc := range live {
		if topology.IsPoolType(svc.ServiceStackTypeInfo.ServiceStackTypeVersionName) {
			return true
		}
	}
	return false
}
