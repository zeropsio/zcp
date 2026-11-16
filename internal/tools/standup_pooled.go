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
	"sync/atomic"
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
// met on the way, and the headline mate.StandupHeadlineOf derives from them.
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
// mate.StandupHeadlineOf.
type standupVerdictRows struct {
	status *standupStatus
	mu     sync.Mutex
	order  []string
	inputs map[string]ops.StandupRowInput
}

func newStandupVerdictRows(status *standupStatus, services []pooledService) *standupVerdictRows {
	r := &standupVerdictRows{status: status, inputs: map[string]ops.StandupRowInput{}}
	for _, s := range services {
		r.order = append(r.order, s.host)
		r.inputs[s.host] = ops.StandupRowInput{Hostname: s.host, Kind: s.kind}
	}
	return r
}

// set changes one service's input and writes the verdict, under the lock:
// the file never takes an older verdict after a newer one.
func (r *standupVerdictRows) set(host string, change func(*ops.StandupRowInput)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	in := r.inputs[host]
	change(&in)
	r.inputs[host] = in
	r.status.verdict(r.verdictLocked())
}

// endBuilding ends the run's verdict: every row still building is failed as
// unreachable — nothing is lost, the next run waits on what is in flight —
// in why's words, and the verdict is written once more.
func (r *standupVerdictRows) endBuilding(why func(ops.StandupRowInput) string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, host := range r.order {
		in := r.inputs[host]
		if ops.StandupRowOf(in).State == mate.RowBuilding {
			in.Class, in.Cause = platform.ClassUnreachable, why(in)
			r.inputs[host] = in
		}
	}
	r.status.verdict(r.verdictLocked())
}

// classOr is err's class, ClassFailed where it has none (a stop of its own
// is still a run that did not get there).
func classOr(err error) platform.ErrorClass {
	if class := platform.ClassOf(err); class != "" {
		return class
	}
	return platform.ClassFailed
}

// verdict is the verdict as the inputs now stand.
func (r *standupVerdictRows) verdict() mate.StandupVerdict {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.verdictLocked()
}

func (r *standupVerdictRows) verdictLocked() mate.StandupVerdict {
	rows := make([]mate.StandupRow, 0, len(r.order))
	for _, host := range r.order {
		rows = append(rows, ops.StandupRowOf(r.inputs[host]))
	}
	return mate.StandupVerdict{Headline: mate.StandupHeadlineOf(rows), Rows: rows}
}

// pooledOutcome is what the pooled run hands the answer: the verdict, main's
// head read for each repository, the repositories a deploy of it started
// for, and main's head where it moved during the run after a build (F13).
type pooledOutcome struct {
	verdict mate.StandupVerdict
	// main, built and ahead are by repository (appId/name).
	main, ahead map[string]string
	built       map[string]bool
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

// hasBuild reports a service of the project that has a build.
func hasBuild(live map[string]*platform.ServiceStack, host string) bool {
	svc := live[host]
	return svc != nil && svc.HasDeployedCode()
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
	out := pooledOutcome{main: map[string]string{}, ahead: map[string]string{}, built: map[string]bool{}}
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
		if sp.failed == "" && !hasBuild(live, pool) {
			go func() {
				if _, err := ops.EnsureBearer(ctx, d.leaseDeps(), pool); err != nil {
					progress.say(fmt.Sprintf("writing %s's bearer reference before its first build failed; it is confirmed after the build: %v", pool, err))
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
				if s.pair != sp {
					continue
				}
				own := cause
				if sp.failedHost != "" && s.host != sp.failedHost {
					own = fmt.Sprintf("%s waits on %s, which stopped its part: %s", s.host, sp.failedHost, cause)
				}
				rows.set(s.host, func(in *ops.StandupRowInput) { in.Class, in.Cause = sp.failedClass(), own })
			}
			continue
		}
		wg.Go(func() {
			commit, built, ahead := d.buildPooledPair(ctx, hqc, sp, services, live, rows, progress)
			mu.Lock()
			defer mu.Unlock()
			if commit != "" {
				out.main[sp.repository] = commit
				out.built[sp.repository] = built
			}
			if ahead != "" {
				out.ahead[sp.repository] = ahead
			}
		})
	}
	wg.Wait()
	rows.endBuilding(func(in ops.StandupRowInput) string {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) && d.runBound > 0 {
			return fmt.Sprintf("%s did not finish within the stand-up's %s", in.Hostname, d.runBound)
		}
		status := in.AppVersionStatus
		if status == "" {
			status = "building"
		}
		return fmt.Sprintf("%s was still %s when the stand-up ended", in.Hostname, status)
	})
	out.verdict = rows.verdict()
	return out
}

// buildPooledPair builds one pooled part: reads main's head once, checks the
// zerops.yaml at it names each service's setup (F4), commit-deploys it to
// every service of the part with no build, waits for each build, and reads
// main again (F13). Answers the commit built and main's head when it moved.
func (d standupDeps) buildPooledPair(ctx context.Context, hqc hq.Client, sp *standupPair, services []pooledService, live map[string]*platform.ServiceStack, rows *standupVerdictRows, progress *standupProgress) (commit string, built bool, ahead string) {
	var mine []pooledService
	for _, s := range services {
		if s.pair == sp {
			mine = append(mine, s)
		}
	}
	failAll := func(class platform.ErrorClass, cause string) {
		if class == "" {
			class = platform.ClassFailed
		}
		for _, s := range mine {
			if !hasBuild(live, s.host) {
				rows.set(s.host, func(in *ops.StandupRowInput) { in.Class, in.Cause = class, cause })
			}
		}
	}
	var toBuild []pooledService
	for _, s := range mine {
		switch svc := live[s.host]; {
		case svc == nil:
			rows.set(s.host, func(in *ops.StandupRowInput) {
				in.Class, in.Cause = platform.ClassFailed, "nothing to adopt yet: "+s.host+" is not in this project"
			})
		case svc.HasDeployedCode():
			d.keepBuilt(ctx, s, svc, rows)
		default:
			if failed := d.bootSkipsFailed(ctx, svc); failed != nil {
				rows.set(s.host, func(in *ops.StandupRowInput) {
					in.AppVersionID, in.AppVersionStatus = failed.ID, failed.Status
				})
				continue
			}
			toBuild = append(toBuild, s)
		}
	}
	if len(toBuild) == 0 {
		return "", false, ""
	}

	scratch, err := hqc.OpenScratch(ctx, sp.appID(), sp.pair.RepoName)
	if err != nil {
		failAll(platform.ClassOf(err), fmt.Sprintf("could not open %s in HQ: %v", sp.repository, err))
		return "", false, ""
	}
	defer scratch.Close()
	commit, found, err := scratch.FetchTip(ctx, hqBase)
	switch {
	case err != nil:
		failAll(platform.ClassOf(err), fmt.Sprintf("could not read %s's %s from HQ: %v", sp.repository, hqBase, err))
		return "", false, ""
	case !found:
		failAll(platform.ClassRefused, fmt.Sprintf("%s has no %s branch in HQ", sp.repository, hqBase))
		return "", false, ""
	}
	setups, yamlName, class, err := zeropsYamlSetups(ctx, scratch, commit)
	if err != nil {
		failAll(class, fmt.Sprintf("%s at %s: %v", sp.repository, shortCommit(commit), err))
		return "", false, ""
	}

	var wg sync.WaitGroup
	var started atomic.Bool
	for _, s := range toBuild {
		if !slices.Contains(setups, s.setup) {
			rows.set(s.host, func(in *ops.StandupRowInput) {
				in.Commit, in.Class = commit, platform.ClassRefused
				in.Cause = fmt.Sprintf("%s at %s in %s has no setup %q, which %s builds by", yamlName, shortCommit(commit), sp.repository, s.setup, s.host)
			})
			continue
		}
		wg.Go(func() {
			if d.commitDeployOne(ctx, sp, s, live[s.host], commit, rows, progress) {
				started.Store(true)
			}
		})
	}
	wg.Wait()
	if !started.Load() {
		return commit, false, ""
	}

	if head, found, err := scratch.RemoteHead(ctx, hqBase); err == nil && found && head != commit {
		progress.say(fmt.Sprintf("%s's %s moved to %s during the stand-up; %s was built, and main is ahead of it", sp.repository, hqBase, shortCommit(head), shortCommit(commit)))
		ahead = head
	}
	return commit, true, ahead
}

// keepBuilt is a service that has a build: never written, its row read from
// the version it runs, and for a pool its bearer confirmed.
func (d standupDeps) keepBuilt(ctx context.Context, s pooledService, svc *platform.ServiceStack, rows *standupVerdictRows) {
	rows.set(s.host, func(in *ops.StandupRowInput) {
		in.AppVersionID, in.AppVersionStatus = svc.ActiveAppVersion.ID, platform.AppVersionStatusActive
		in.ProcessStatus = platform.ProcessStatusFinished
	})
	if s.kind == mate.RowPool {
		d.confirmBearer(ctx, s.host, rows)
	}
}

// commitDeployOne submits one commit deploy and follows it to its end: the
// row building with its version and process, then done or failed from the
// version's status, a failure with its build log's last lines (F5). Answers
// whether the deploy started.
func (d standupDeps) commitDeployOne(ctx context.Context, sp *standupPair, s pooledService, svc *platform.ServiceStack, commit string, rows *standupVerdictRows, progress *standupProgress) bool {
	progress.say(fmt.Sprintf("building %s at %s into %s", sp.repository, shortCommit(commit), s.host))
	rows.set(s.host, func(in *ops.StandupRowInput) { in.Commit, in.Class, in.Cause = commit, "", "" })
	res, err := ops.CommitDeploy(ctx, d.batch.client, d.source, ops.CommitDeployRequest{
		ServiceID: svc.ID, Hostname: s.host, Setup: s.setup, Repository: sp.repository, Commit: commit,
	})
	if err != nil {
		rows.set(s.host, func(in *ops.StandupRowInput) {
			in.Class, in.Cause = platform.ClassOf(err), fmt.Sprintf("the commit deploy of %s into %s did not start: %v", shortCommit(commit), s.host, err)
		})
		return false
	}
	rows.set(s.host, func(in *ops.StandupRowInput) { in.AppVersionID, in.ProcessID = res.AppVersionID, res.ProcessID })
	if res.ProcessID == "" {
		progress.say(fmt.Sprintf("%s already builds %s; waiting on it", s.host, shortCommit(commit)))
	}
	processStatus, settled := d.waitBuild(ctx, s.host, svc.ID, res, rows)
	if !settled {
		return true
	}
	version := d.appVersion(ctx, svc.ID, res.AppVersionID)
	rows.set(s.host, func(in *ops.StandupRowInput) {
		in.ProcessStatus = processStatus
		if version == nil {
			in.Class = platform.ClassUnreachable
			in.Cause = fmt.Sprintf("%s's app version %s could not be read after its build", s.host, res.AppVersionID)
			return
		}
		in.AppVersionStatus = version.Status
	})
	if version == nil || version.Status != platform.AppVersionStatusActive {
		return true
	}
	if _, _, err := workflow.RecordExternalDeploy(d.batch.stateDir, s.host); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: stand-up: record the first deploy of %s: %v\n", s.host, err)
	}
	if s.kind == mate.RowPool {
		d.confirmBearer(ctx, s.host, rows)
	}
	return true
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

// standupBuildWait bounds the wait on one service's build in a run that has
// no bound of its own (a tool call); a run that has one (the boot's) waits
// on its builds until that bound, and no other.
const standupBuildWait = 20 * time.Minute

// waitBuild waits on one service's build to its end, under one bound: the
// run's, else standupBuildWait. ops' waits end at budgets of their own, which
// are not the stand-up's: one that ends first is waited again. A build the
// commit deploy started is its process; one already in flight (F8) is the
// service's, waited on as the service settles, never a second build. Answers
// the process's last status and whether the build ended; a build that did
// not end within the bound, or a wait that failed, has its row failed with
// the bound or the error named.
func (d standupDeps) waitBuild(ctx context.Context, host, serviceID string, res ops.CommitDeployResult, rows *standupVerdictRows) (string, bool) {
	bound, whose := d.runBound, "the stand-up's"
	waitCtx := ctx
	if bound <= 0 {
		bound, whose = standupBuildWait, "the stand-up's wait for a build,"
		var cancel context.CancelFunc
		waitCtx, cancel = context.WithTimeout(ctx, bound)
		defer cancel()
	}
	for {
		var waited *ops.WaitResult
		var err error
		if res.ProcessID != "" {
			waited, err = ops.WaitProcesses(waitCtx, d.batch.client, []string{res.ProcessID}, nil)
		} else {
			waited, err = ops.WaitServiceSettled(waitCtx, d.batch.client, d.batch.projectID, host, nil)
		}
		switch {
		case errors.Is(waitCtx.Err(), context.DeadlineExceeded):
			// The bound ended the wait: name it, with where the build stood.
			read, cancel := context.WithTimeout(context.WithoutCancel(ctx), standupBootRead)
			status := "building"
			if v := d.appVersion(read, serviceID, res.AppVersionID); v != nil && v.Status != "" {
				status = v.Status
			}
			cancel()
			rows.set(host, func(in *ops.StandupRowInput) {
				in.Class = platform.ClassUnreachable
				in.Cause = fmt.Sprintf("%s was still %s after %s %s; the build goes on in Zerops", host, status, whose, bound)
			})
			return "", false
		case waitCtx.Err() != nil:
			return "", false // the run was stopped: its end says so (endBuilding)
		case err != nil:
			rows.set(host, func(in *ops.StandupRowInput) {
				in.Class, in.Cause = classOr(err), fmt.Sprintf("waiting on %s's build failed: %v", host, err)
			})
			return "", false
		case waited.TimedOut:
			continue // ops' own budget, not the stand-up's bound
		}
		if len(waited.Processes) > 0 {
			return waited.Processes[0].Status, true
		}
		return "", true
	}
}

// standupBearerAsks bounds how often the bearer's writer is asked after the
// pool's first build, while it answers "written, not resolved yet" — a pool
// whose status still lags the build it just finished.
const standupBearerAsks = 3

// confirmBearer reads the pool's bearer through its one writer, which waits a
// bounded time for the reference to resolve (F10): the row is done only once
// it does. An answer of no bearer and no error is the reference written and
// not resolved yet: the row stays building while it is asked again, and is
// failed naming the variable when it still has not resolved. "" is never
// taken for a bearer.
func (d standupDeps) confirmBearer(ctx context.Context, pool string, rows *standupVerdictRows) {
	for ask := 1; ; ask++ {
		bearer, err := ops.EnsureBearer(ctx, d.leaseDeps(), pool)
		pending := err == nil && bearer == ""
		last := ask >= standupBearerAsks || ctx.Err() != nil
		rows.set(pool, func(in *ops.StandupRowInput) {
			in.BearerResolves = err == nil && bearer != ""
			switch {
			case err != nil:
				in.Class, in.Cause = classOr(err), err.Error()
			case pending && last:
				in.Class = platform.ClassUnreachable
				in.Cause = "still literal after " + pool + "'s build"
			}
		})
		if !pending || last {
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(standupHQRetry):
		}
	}
}

// bootSkipsFailed is, for the boot run, a service's newest version where it
// ended failed: the boot builds only a service never built or with a build
// in flight; rebuilding a failed one is the agent's zerops_standup call.
// nil for any other run, or where the versions cannot be read.
func (d standupDeps) bootSkipsFailed(ctx context.Context, svc *platform.ServiceStack) *platform.AppVersionEvent {
	if !d.boot {
		return nil
	}
	versions, err := d.batch.client.ListServiceAppVersions(ctx, svc.ID)
	if err != nil {
		return nil
	}
	for i := range versions {
		v := &versions[i]
		if v.Source == platform.AppVersionSourceNone {
			continue
		}
		if ops.StandupRowOf(ops.StandupRowInput{AppVersionStatus: v.Status}).State == mate.RowFailed {
			return v
		}
		return nil
	}
	return nil
}

// leaseDeps is what the bearer's writer reads the pool through.
func (d standupDeps) leaseDeps() ops.LeaseDeps {
	return ops.LeaseDeps{
		Client: d.batch.client, ProjectID: d.batch.projectID, StateDir: d.batch.stateDir,
		Swarm: ops.NewSwarmFactory(&http.Client{Timeout: ops.LeaseCallTimeout}),
	}
}

// zeropsYamlSetups are the setups the repository's zerops.yaml names at
// commit, and the file's name; on an error its class: a read's own, a file
// absent or unreadable refused.
func zeropsYamlSetups(ctx context.Context, scratch *hq.Scratch, commit string) ([]string, string, platform.ErrorClass, error) {
	const yamlName = "zerops.yaml"
	for _, name := range []string{yamlName, "zerops.yml"} {
		body, found, err := scratch.File(ctx, commit, name)
		if err != nil {
			return nil, name, classOr(err), fmt.Errorf("could not read %s: %w", name, err)
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
			return nil, name, platform.ClassRefused, fmt.Errorf("%s does not parse: %w", name, err)
		}
		setups := make([]string, 0, len(doc.Zerops))
		for _, z := range doc.Zerops {
			setups = append(setups, z.Setup)
		}
		return setups, name, "", nil
	}
	return nil, yamlName, platform.ClassRefused, errors.New("it has no " + yamlName)
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
		if err == nil || !platform.ClassOf(err).WaitedOn() || !time.Now().Before(deadline) {
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
