package tools

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// executeReviewBuild takes the entire stage set only after holding its locks.
// The lock scope includes HQ's asynchronous merge decision.
func executeReviewBuild(ctx context.Context, d devGitDeps, hqc hq.Client, app string, change hq.MateChange, metas []*workflow.ServiceMeta, stages []topology.StageRef, read func() (hq.MateStateDocument, error), log func(string)) (bool, string, error) {
	if err := reviewResolveSetups(ctx, hqc, app, change.Repo, metas, stages); err != nil {
		return true, "", fmt.Errorf("read stage setups: %w", err)
	}
	bindings, err := reviewBindStages(ctx, d, stages)
	if err != nil {
		return true, "", fmt.Errorf("find stages: %w", err)
	}
	release, err := reviewLockStages(ctx, d, bindings, log)
	if err != nil {
		return true, "", fmt.Errorf("lock stages: %w", err)
	}
	defer release()

	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	watch := watchMateState(watchCtx, hqIdentity(d.rt).State.Watch, func(err error) {
		if log != nil {
			log(fmt.Sprintf("Review merge watch: %v", err))
		}
	})
	doc, err := read()
	if err != nil {
		return true, "", fmt.Errorf("read current Mate state: %w", err)
	}
	if doc.Link != hq.MateLinkLinked || doc.Mate == nil {
		return true, "", fmt.Errorf("read current Mate state: HQ is not linked")
	}
	current, repo := reviewCurrent(doc.Mate, change.Repo, change.Number)
	if current == nil || repo == nil {
		return false, "", nil
	}
	if current.State == hq.ChangeMerging {
		return false, "", reviewAwaitDecision(ctx, watch, read, change.Repo, change.Number, "", doc.Mate.Rev, true)
	}
	if current.State != hq.ChangeAdding || current.Head == nil || *current.Head == "" || current.ChecksGivenUp != nil {
		return false, "", nil
	}
	// The local stage records can change while the locks were being acquired.
	latest, err := workflow.ListServiceMetas(d.stateDir)
	if err != nil {
		return true, "", fmt.Errorf("reread stage records: %w", err)
	}
	fresh := reviewStages(latest, app, change.Repo)
	if err := reviewResolveSetups(ctx, hqc, app, change.Repo, latest, fresh); err != nil {
		return true, "", fmt.Errorf("reread stage setups: %w", err)
	}
	if !reviewSameStages(stages, fresh) {
		return true, "", fmt.Errorf("stage set changed while waiting")
	}

	head := *current.Head
	selected := make([]hq.ReviewStage, len(bindings))
	for i, b := range bindings {
		selected[i] = b.stage
	}
	take, err := hqc.TakeReviewBuild(ctx, change.Repo, change.Number, hq.ReviewTake{Head: head, Main: repo.MainHead, Stages: selected})
	if err != nil {
		return true, "", fmt.Errorf("take Review build: %w", err)
	}
	switch take.Decision {
	case "declined":
		return false, "", nil
	case hq.ReviewDecisionMerging:
		return false, "", reviewAwaitDecision(ctx, watch, read, change.Repo, change.Number, take.Take, doc.Mate.Rev, false)
	case hq.ReviewDecisionBuild:
		if take.Take == "" {
			return true, "", fmt.Errorf("take Review build: HQ returned no take")
		}
	default:
		return true, "", fmt.Errorf("take Review build: unknown decision %q", take.Decision)
	}
	version := reviewCandidateName(repo.MainHead, head)
	var source ops.DeploySource
	if repo.MainHead == nil {
		source = ops.NewHQDeploySource(d.httpClient, hqIdentity(d.rt))
		version = head
	} else {
		source = ops.NewHQReviewDeploySource(d.httpClient, hqIdentity(d.rt), *repo.MainHead, head)
	}
	parts := make([]hq.ReviewBuildPart, len(bindings))
	var wg sync.WaitGroup
	for i, binding := range bindings {
		wg.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					parts[i] = reviewUnavailablePart(hq.ReviewBuildPart{}, reviewPhaseBuild, fmt.Errorf("worker stopped: %v", r))
				}
				parts[i].Part, parts[i].Setup, parts[i].Stage = binding.stage.Part, binding.stage.Setup, binding.stage.Stage
			}()
			parts[i] = reviewBuildStageWithLock(ctx, d, source, app+"/"+change.Repo, version, binding.ref)
		})
	}
	wg.Wait()
	result, err := reviewFinishResult(ctx, hqc, change.Repo, change.Number, take.Take, parts, watch)
	if err != nil {
		return true, "", fmt.Errorf("write Review build: %w", err)
	}
	if result.Decision == hq.ReviewDecisionMerging {
		if err := reviewAwaitDecision(ctx, watch, read, change.Repo, change.Number, take.Take, doc.Mate.Rev, false); err != nil {
			return true, "", err
		}
	}
	return result.Decision == hq.ReviewDecisionUnavailable, "", nil
}

// A caller holding all stage locks keeps them past this function, through HQ's decision.
func reviewBuildStageWithLock(ctx context.Context, d devGitDeps, source ops.DeploySource, repository, head string, stage topology.StageRef) hq.ReviewBuildPart {
	part := hq.ReviewBuildPart{Part: stage.Hostname, Setup: stage.SetupName, Stage: stage.Hostname}
	unavailable := func(step string, err error) hq.ReviewBuildPart {
		return reviewUnavailablePart(part, step, err)
	}
	services, err := ops.ListProjectServicesDirect(ctx, d.client, d.rt.ProjectID)
	if err != nil {
		return unavailable("read stage", err)
	}
	svc, err := ops.FindService(services, stage.Hostname)
	if err != nil {
		return unavailable("resolve stage", err)
	}
	svc, err = d.client.GetService(ctx, svc.ID)
	if err != nil {
		return unavailable("read stage", err)
	}
	if svc == nil {
		return unavailable("read stage", fmt.Errorf("stage not returned"))
	}
	access, err := ops.ObservePublicAccess(ctx, d.client, d.rt.ProjectID, svc)
	if err != nil {
		return unavailable("read public access", err)
	}
	publicURL := access.URL
	public := svc.SubdomainAccess || len(access.Domains) > 0
	if publicURL == "" && len(access.Domains) > 0 {
		scheme := "http://"
		if access.Domains[0].SSLEnabled {
			scheme = "https://"
		}
		publicURL = scheme + access.Domains[0].Name
	}
	if parsed, parseErr := url.Parse(publicURL); parseErr == nil && parsed.Hostname() != "" {
		part.Stage = parsed.Hostname()
	}
	svc, found, err := reviewReadStageVersion(ctx, d, svc.ID, head)
	if err != nil {
		return unavailable("read stage version", err)
	}
	if found.action == reviewBuild {
		// A prior writer can still be in flight after its own wait ended.
		if err := ops.WaitStageVersions(ctx, d.client, d.rt.ProjectID, svc.ID); err != nil {
			return unavailable("wait for stage", err)
		}
		svc, found, err = reviewReadStageVersion(ctx, d, svc.ID, head)
		if err != nil {
			return unavailable("read stage version", err)
		}
	}
	if found.action == reviewFailed {
		return reviewFailedPart(ctx, d, stage.Hostname, part, found)
	}
	part.AppVersionID = found.id
	if found.action == reviewUnknown {
		return unavailable("read stage version", fmt.Errorf("version %s is %s and has no live build", found.id, found.status))
	}
	port, hasPort := ops.PreferredHTTPPort(svc.Ports)
	if public && !hasPort {
		return unavailable("prepare readiness", fmt.Errorf("stage has no HTTP port"))
	}
	source = reviewReadySource{source: source, setup: stage.SetupName, port: port.Port, public: public}
	if found.action == reviewBuild {
		res, deployErr := ops.CommitDeploy(ctx, d.client, source, ops.CommitDeployRequest{
			ServiceID: svc.ID, Hostname: stage.Hostname, Setup: stage.SetupName, Repository: repository, Commit: head,
		})
		if deployErr != nil {
			var conflict *ops.MergeConflictError
			if errors.As(deployErr, &conflict) {
				part.Outcome, part.Phase, part.Reason = reviewOutcomeFailed, reviewPhaseBuild, reviewReason("merge", conflict.Error())
				return part
			}
			var setupErr *reviewSetupError
			if errors.As(deployErr, &setupErr) {
				part.Outcome, part.Phase, part.Reason = reviewOutcomeFailed, reviewPhaseBuild, reviewReason("prepare stage", setupErr.Error())
				return part
			}
			return unavailable("start build", deployErr)
		}
		found = reviewVersion{action: reviewJoin, id: res.AppVersionID, process: res.ProcessID}
		part.AppVersionID = res.AppVersionID
	}
	if found.action == reviewJoin {
		status, waitErr := reviewWait(ctx, d.client, found.process)
		if waitErr != nil {
			return unavailable("wait for build", waitErr)
		}
		versions, err := d.client.ListServiceAppVersions(ctx, svc.ID)
		if err != nil {
			return unavailable("read finished build", err)
		}
		var version *platform.AppVersionEvent
		for i := range versions {
			if versions[i].ID == found.id {
				version = &versions[i]
				break
			}
		}
		if version == nil {
			return unavailable("read finished build", fmt.Errorf("version %s was not returned", found.id))
		}
		if reviewFailedStatus(version.Status) || status == platform.ProcessStatusFailed {
			return reviewFailedPart(ctx, d, stage.Hostname, part, reviewVersion{id: version.ID, status: version.Status, reason: status})
		}
		if version.Status != platform.AppVersionStatusActive {
			return unavailable("read finished build", fmt.Errorf("version %s is %s", version.ID, version.Status))
		}
	}
	path := ""
	if public {
		opened, readErr := source.Open(ctx, repository, head)
		if readErr != nil {
			return unavailable("read health path", readErr)
		}
		_ = opened.Archive.Close()
		path, readErr = reviewHealthPath(opened.ZeropsYaml, stage.SetupName)
		if readErr != nil {
			return unavailable("read health path", readErr)
		}
	}
	probe := ops.ProbeStage(ctx, d.client, d.httpClient, svc.ID, found.id, publicURL, path, public)
	part.Outcome = probe.Outcome
	if probe.Reason != "" {
		part.Reason = reviewReason("health", probe.Reason)
	}
	if probe.Outcome == reviewOutcomeFailed {
		part.Phase = "health"
	}
	if !probe.At.IsZero() {
		part.Probe = &hq.ReviewBuildProbe{URL: probe.URL, At: probe.At.UTC().Format(time.RFC3339)}
		if probe.Status != 0 {
			part.Probe.Status = &probe.Status
		}
	}
	return part
}

func reviewReadStageVersion(ctx context.Context, d devGitDeps, serviceID, head string) (*platform.ServiceStack, reviewVersion, error) {
	svc, err := d.client.GetService(ctx, serviceID)
	if err != nil {
		return nil, reviewVersion{}, fmt.Errorf("read stage: %w", err)
	}
	if svc == nil {
		return nil, reviewVersion{}, fmt.Errorf("stage not returned")
	}
	versions, err := d.client.ListServiceAppVersions(ctx, serviceID)
	if err != nil {
		return nil, reviewVersion{}, fmt.Errorf("read stage versions: %w", err)
	}
	processes, err := d.client.GetProjectProcessesDirect(ctx, d.rt.ProjectID)
	if err != nil {
		return nil, reviewVersion{}, fmt.Errorf("read stage builds: %w", err)
	}
	names := make(map[string]string, len(versions))
	for _, process := range processes {
		if process.AppVersion != nil && process.AppVersion.Name != "" && reviewTouches(process, serviceID) {
			names[process.AppVersion.ID] = process.AppVersion.Name
		}
	}
	missing := false
	for i := range versions {
		name, ok := names[versions[i].ID]
		if !ok {
			missing = true
		}
		versions[i].Name = name
	}
	active := ""
	if svc.ActiveAppVersion != nil {
		active = svc.ActiveAppVersion.ID
	}
	found := reviewVersionOf(head, serviceID, active, versions, processes)
	if found.action != reviewBuild || !missing {
		return svc, found, nil
	}
	indexed, err := d.client.ListServiceAppVersionNames(ctx, d.rt.ProjectID, serviceID)
	if err != nil {
		return nil, reviewVersion{}, fmt.Errorf("read stage version names: %w", err)
	}
	for id, name := range indexed {
		if _, known := names[id]; !known {
			names[id] = name
		}
	}
	for i := range versions {
		versions[i].Name = names[versions[i].ID]
	}
	found = reviewVersionOf(head, serviceID, active, versions, processes)
	if found.action != reviewBuild {
		return svc, found, nil
	}
	for i := range versions {
		if _, ok := names[versions[i].ID]; !ok {
			return nil, reviewVersion{}, fmt.Errorf("stage version %s is not yet in the name search", versions[i].ID)
		}
	}
	return svc, found, nil
}

func reviewUnavailablePart(part hq.ReviewBuildPart, step string, err error) hq.ReviewBuildPart {
	part.Outcome, part.Reason = reviewUnavailable, reviewReason(step, err.Error())
	return part
}

func reviewFailedPart(ctx context.Context, d devGitDeps, serviceHostname string, part hq.ReviewBuildPart, version reviewVersion) hq.ReviewBuildPart {
	part.Outcome, part.Phase, part.AppVersionID = reviewOutcomeFailed, reviewFailurePhase(version.status), version.id
	reason := version.reason
	lines, err := ops.FetchVersionBuildLogs(ctx, d.client, d.logFetcher, d.rt.ProjectID, serviceHostname, version.id, 200)
	if err == nil && len(lines) > 0 {
		if line := reviewFailureLine(lines); line != "" {
			reason = line
		}
	}
	part.Reason = reviewReason(part.Phase, reason)
	return part
}

// Build output is unstructured: prefer the last recognizable diagnostic to
// the builder's generic final wrapper, and retain a line when none is marked.
func reviewFailureLine(entries []string) string {
	lines := strings.Split(strings.Join(entries, "\n"), "\n")
	fallback, wrapper := "", ""
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		lower := strings.ToLower(line)
		if lower == "[build error] running build commands failed" {
			wrapper = line
			continue
		}
		if strings.HasPrefix(lower, "npm notice") || strings.HasPrefix(line, "━━") || strings.HasPrefix(lower, "at ") {
			continue
		}
		if fallback == "" {
			fallback = line
		}
		if ops.IsErrorLogLine(line) {
			return line
		}
		for _, marker := range []string{"not found", "undefined", "permission denied", "out of memory", "oom alert", "oom-kill", "killed"} {
			if strings.Contains(lower, marker) {
				return line
			}
		}
	}
	if fallback != "" {
		return fallback
	}
	return wrapper
}

func reviewWait(ctx context.Context, client platform.Client, process string) (string, error) {
	if process == "" {
		return "", fmt.Errorf("the build has no process id")
	}
	wait, cancel := context.WithTimeout(ctx, standupBuildWait)
	defer cancel()
	for {
		result, err := ops.WaitProcesses(wait, client, []string{process}, nil)
		if err != nil {
			return "", fmt.Errorf("wait for process: %w", err)
		}
		if wait.Err() != nil {
			return "", fmt.Errorf("wait for process: %w", wait.Err())
		}
		if !result.TimedOut {
			if len(result.Processes) == 0 {
				return "", fmt.Errorf("process was not returned")
			}
			return result.Processes[0].Status, nil
		}
	}
}

func reviewHealthPath(body, setup string) (string, error) {
	doc, err := ops.ParseZeropsYmlContent([]byte(body), "zerops.yaml")
	if err != nil {
		return "", fmt.Errorf("parse setup: %w", err)
	}
	entry := doc.FindEntry(setup)
	if entry == nil {
		return "", fmt.Errorf("setup %s is not in the version's zerops.yaml", setup)
	}
	check, _ := entry.Deploy.ReadinessCheck.(map[string]any)
	if check == nil {
		check, _ = entry.Run.HealthCheck.(map[string]any)
	}
	get, _ := check["httpGet"].(map[string]any)
	path, _ := get["path"].(string)
	return strings.TrimSpace(path), nil
}

// reviewReadySource changes only the build's setup, leaving its source tree intact.
type reviewReadySource struct {
	source ops.DeploySource
	setup  string
	port   int
	public bool
}

type reviewSetupError struct{ err error }

func (e *reviewSetupError) Error() string { return e.err.Error() }
func (e *reviewSetupError) Unwrap() error { return e.err }

func (s reviewReadySource) Open(ctx context.Context, repository, commit string) (ops.CommitSource, error) {
	opened, err := s.source.Open(ctx, repository, commit)
	if err != nil {
		var missing *ops.MissingDeployConfigError
		if errors.As(err, &missing) {
			return ops.CommitSource{}, &reviewSetupError{err: err}
		}
		return ops.CommitSource{}, fmt.Errorf("open stage source: %w", err)
	}
	yaml, _, err := ops.ReviewStageSetup(opened.ZeropsYaml, s.setup, s.port, s.public)
	if err != nil {
		_ = opened.Archive.Close()
		return ops.CommitSource{}, &reviewSetupError{err: fmt.Errorf("prepare stage readiness: %w", err)}
	}
	opened.ZeropsYaml = yaml
	return opened, nil
}
