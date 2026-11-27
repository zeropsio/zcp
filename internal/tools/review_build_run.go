package tools

import (
	"context"
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

// executeReviewBuild makes one observation of every recorded stage. HQ owns
// the head check and ranks competing results inside the change's lock.
func executeReviewBuild(ctx context.Context, d devGitDeps, hqc hq.Client, app string, change hq.MateChange, metas []*workflow.ServiceMeta, stages []topology.StageRef, take bool) (bool, string, error) {
	head := *change.Head
	now := time.Now().UTC().Truncate(time.Second)
	updatedAt := ""
	if take {
		write := hq.ReviewBuildWrite{Head: head, Step: "taken", At: now.Format(time.RFC3339), Until: now.Add(30 * time.Minute).Format(time.RFC3339)}
		taken, err := hqc.Once().EditChange(ctx, change.Repo, change.Number, hq.ChangeEdit{ReviewBuild: &write})
		if err != nil {
			return true, updatedAt, fmt.Errorf("take Review build: %w", err)
		}
		updatedAt = taken.UpdatedAt
	}
	// Leave the HQ write its own call budget before the take's deadline.
	work, cancel := context.WithDeadline(ctx, reviewWorkDeadline(now, change.ReviewBuild, take))
	defer cancel()
	if err := reviewResolveSetups(work, hqc, app, change.Repo, metas, stages); err != nil {
		return true, updatedAt, fmt.Errorf("read stage setups: %w", err)
	}
	source := ops.NewHQDeploySource(d.httpClient, hqIdentity(d.rt))
	parts := make([]hq.ReviewBuildPart, len(stages))
	var wg sync.WaitGroup
	for i, stage := range stages {
		wg.Go(func() {
			defer func() {
				if r := recover(); r != nil {
					parts[i] = reviewUnavailablePart(hq.ReviewBuildPart{Setup: stage.SetupName, Stage: stage.Hostname}, reviewPhaseBuild, fmt.Errorf("worker stopped: %v", r))
				}
			}()
			parts[i] = reviewBuildStage(work, d, source, app+"/"+change.Repo, head, stage)
		})
	}
	wg.Wait()
	write, retry := reviewResult(head, time.Now(), parts)
	result, err := hqc.Once().EditChange(ctx, change.Repo, change.Number, hq.ChangeEdit{ReviewBuild: &write})
	if err != nil {
		return true, updatedAt, fmt.Errorf("write Review build: %w", err)
	}
	return retry, result.UpdatedAt, nil
}

func reviewWorkDeadline(now time.Time, build *hq.ReviewBuild, take bool) time.Time {
	deadline := now.Add(30 * time.Minute)
	if !take && build != nil && build.State == reviewAwaiting {
		if until, err := time.Parse(time.RFC3339, build.Until); err == nil && now.Before(until) && until.Before(deadline) {
			deadline = until
		}
	}
	return deadline.Add(-hqCallTimeout)
}

func reviewResult(head string, at time.Time, parts []hq.ReviewBuildPart) (hq.ReviewBuildWrite, bool) {
	unavailable, failed := false, false
	for _, part := range parts {
		unavailable = unavailable || part.Outcome == reviewUnavailable
		failed = failed || part.Outcome == reviewOutcomeFailed
	}
	return hq.ReviewBuildWrite{Head: head, Step: "result", At: at.UTC().Format(time.RFC3339), Parts: parts}, unavailable && !failed
}

func reviewBuildStage(ctx context.Context, d devGitDeps, source ops.DeploySource, repository, head string, stage topology.StageRef) hq.ReviewBuildPart {
	part := hq.ReviewBuildPart{Setup: stage.SetupName, Stage: stage.Hostname}
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
	release, err := ops.LockStage(ctx, d.stateDir, svc.ID)
	if err != nil {
		return unavailable("lock stage", err)
	}
	defer release()
	svc, found, err := reviewReadStageVersion(ctx, d, svc.ID, head)
	if err != nil {
		return unavailable("read stage version", err)
	}
	if found.action == reviewBuild {
		// A prior writer can still be in flight after its own wait ended. Only
		// a new writer waits for those processes; a known head joins directly.
		if err := ops.WaitStageVersions(ctx, d.client, d.rt.ProjectID, svc.ID); err != nil {
			return unavailable("wait for stage", err)
		}
		svc, found, err = reviewReadStageVersion(ctx, d, svc.ID, head)
		if err != nil {
			return unavailable("read stage version", err)
		}
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
	if found.action == reviewFailed {
		return reviewFailedPart(ctx, d, stage.Hostname, part, found)
	}
	if found.action == reviewBuild {
		res, deployErr := ops.CommitDeploy(ctx, d.client, source, ops.CommitDeployRequest{
			ServiceID: svc.ID, Hostname: stage.Hostname, Setup: stage.SetupName, Repository: repository, Commit: head,
		})
		if deployErr != nil {
			return unavailable("start build", deployErr)
		}
		found = reviewVersion{action: reviewJoin, id: res.AppVersionID, process: res.ProcessID}
	}
	part.AppVersionID = found.id
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
	active := ""
	if svc.ActiveAppVersion != nil {
		active = svc.ActiveAppVersion.ID
	}
	return svc, reviewVersionOf(head, serviceID, active, versions, processes), nil
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
