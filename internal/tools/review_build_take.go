package tools

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

type reviewBinding struct {
	ref   topology.StageRef
	id    string
	stage hq.ReviewStage
}

const reviewChangeNotOpen = "change_not_open"

func reviewBindStages(ctx context.Context, d devGitDeps, stages []topology.StageRef) ([]reviewBinding, error) {
	bindings := make([]reviewBinding, 0, len(stages))
	if len(stages) == 0 {
		return bindings, nil
	}
	services, err := ops.ListProjectServicesDirect(ctx, d.client, d.rt.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("read services: %w", err)
	}
	for _, stage := range stages {
		svc, err := ops.FindService(services, stage.Hostname)
		if err != nil {
			return nil, fmt.Errorf("find %s: %w", stage.Hostname, err)
		}
		svc, err = d.client.GetService(ctx, svc.ID)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", stage.Hostname, err)
		}
		if svc == nil {
			return nil, fmt.Errorf("stage %s was not returned", stage.Hostname)
		}
		access, err := ops.ObservePublicAccess(ctx, d.client, d.rt.ProjectID, svc)
		if err != nil {
			return nil, fmt.Errorf("read access for %s: %w", stage.Hostname, err)
		}
		public := access.URL
		if public == "" && len(access.Domains) > 0 {
			public = "https://" + access.Domains[0].Name
		}
		host := stage.Hostname
		if parsed, err := url.Parse(public); err == nil && parsed.Hostname() != "" {
			host = parsed.Hostname()
		}
		bindings = append(bindings, reviewBinding{ref: stage, id: svc.ID, stage: hq.ReviewStage{Part: stage.Hostname, Setup: stage.SetupName, Stage: host}})
	}
	slices.SortFunc(bindings, func(a, b reviewBinding) int { return strings.Compare(a.id, b.id) })
	return bindings, nil
}

func reviewLockStages(ctx context.Context, d devGitDeps, stages []reviewBinding, log func(string)) (func(), error) {
	var held []func()
	release := func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i]()
		}
	}
	for _, stage := range stages {
		unlock, err := ops.LockStageUntil(ctx, d.stateDir, stage.id, func() {
			if log != nil {
				log("Review build: waiting for stage " + stage.stage.Stage)
			}
		})
		if err != nil {
			release()
			return nil, fmt.Errorf("lock %s: %w", stage.stage.Part, err)
		}
		held = append(held, unlock)
	}
	return release, nil
}

func reviewSameStages(a, b []topology.StageRef) bool {
	if len(a) != len(b) {
		return false
	}
	for _, left := range a {
		if !slices.ContainsFunc(b, func(right topology.StageRef) bool {
			return left.Hostname == right.Hostname && left.SetupName == right.SetupName
		}) {
			return false
		}
	}
	return true
}

func reviewCurrent(state *hq.MateState, repo string, number int) (*hq.MateChange, *hq.MateRepo) {
	var change *hq.MateChange
	var repository *hq.MateRepo
	for i := range state.Changes {
		if state.Changes[i].Repo == repo && state.Changes[i].Number == number {
			change = &state.Changes[i]
			break
		}
	}
	for i := range state.Repos {
		if state.Repos[i].Name == repo {
			repository = &state.Repos[i]
			break
		}
	}
	return change, repository
}

func reviewCandidateName(main *string, head string) string {
	base := ""
	if main != nil {
		base = *main
	}
	sum := sha256.Sum256([]byte(base + "\x00" + head))
	return fmt.Sprintf("%x", sum[:20])
}

// An unanswered result may have claimed the merge. Repeat only when the HQ
// link publishes a recovery event, retaining all stage locks meanwhile.
func reviewFinishResult(ctx context.Context, hqc hq.Client, repo string, number int, take string, parts []hq.ReviewBuildPart, changed <-chan struct{}) (hq.ReviewDecision, error) {
	uncertain := false
	for {
		result, err := hqc.Once().FinishReviewBuild(ctx, repo, number, take, parts)
		if err == nil {
			return result, nil
		}
		var refused *hq.RefusedError
		if errors.As(err, &refused) && (!uncertain || refused.Code == reviewChangeNotOpen) {
			return result, fmt.Errorf("finish take: %w", err)
		}
		// A lost or malformed answer may have committed the merge. A later
		// refusal does not establish what happened to that first request.
		uncertain = true
		select {
		case <-ctx.Done():
			return result, fmt.Errorf("finish take: %w", ctx.Err())
		case _, ok := <-changed:
			if !ok {
				return result, fmt.Errorf("finish take: state watch ended: %w", err)
			}
		}
	}
}

// An HQ answer can precede the link frame that carries it. A newer frame
// must name this take's result, or a settled change, before locks are freed.
func reviewAwaitDecision(ctx context.Context, changed <-chan struct{}, read func() (hq.MateStateDocument, error), repo string, number int, take string, revision int64, alreadyMerging bool) error {
	for {
		doc, err := read()
		if err == nil && doc.Link == hq.MateLinkLinked && doc.Mate != nil && (alreadyMerging || doc.Mate.Rev > revision) {
			change, _ := reviewCurrent(doc.Mate, repo, number)
			if change == nil {
				return nil
			}
			if change.State == hq.ChangeMerging {
				alreadyMerging = true
			}
			if change.State != hq.ChangeMerging && (alreadyMerging || change.State == hq.ChangeMerged || change.State == hq.ChangeClosed || change.State == hq.ChangeOpen || change.ReviewBuild != nil && change.ReviewBuild.Take == take && change.ReviewBuild.State == "passing") {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for HQ merge decision: %w", ctx.Err())
		case _, ok := <-changed:
			if !ok {
				return fmt.Errorf("wait for HQ merge decision: state watch ended")
			}
		}
	}
}

// executeReviewMain follows merges made outside Mate, under the same locks as
// adding builds and only if HQ still says no change is being added.
func executeReviewMain(ctx context.Context, d devGitDeps, hqc hq.Client, app, repo, main string, metas []*workflow.ServiceMeta, stages []topology.StageRef, read func() (hq.MateStateDocument, error), log func(string)) (unavailable bool, runErr error) {
	statusPath := mate.StatusFilePath()
	startedStages := false
	defer func() {
		if runErr == nil || startedStages || ctx.Err() != nil {
			return
		}
		for _, stage := range stages {
			failure := reviewUnavailablePart(hq.ReviewBuildPart{Part: stage.Hostname}, "follow main", runErr)
			if err := recordReviewMain(statusPath, repo, main, failure); err != nil {
				runErr = errors.Join(runErr, err)
			}
		}
	}()
	if err := reviewResolveSetups(ctx, hqc, app, repo, metas, stages); err != nil {
		return true, fmt.Errorf("read main stage setups: %w", err)
	}
	bindings, err := reviewBindStages(ctx, d, stages)
	if err != nil {
		return true, fmt.Errorf("find main stages: %w", err)
	}
	release, err := reviewLockStages(ctx, d, bindings, log)
	if err != nil {
		return true, fmt.Errorf("lock main stages: %w", err)
	}
	defer release()
	doc, err := read()
	if err != nil {
		return true, fmt.Errorf("read current Mate state: %w", err)
	}
	if doc.Link != hq.MateLinkLinked || doc.Mate == nil {
		return true, fmt.Errorf("read current Mate state: HQ is not linked")
	}
	_, current := reviewCurrent(doc.Mate, repo, 0)
	if current == nil || !reviewMainNext(*current, false) || *current.MainHead != main {
		return false, nil
	}
	source := ops.NewHQDeploySource(d.httpClient, hqIdentity(d.rt))
	startedStages = true
	for _, binding := range bindings {
		part := reviewBuildStageWithLock(ctx, d, source, app+"/"+repo, main, binding.ref, log, true)
		if err := recordReviewMain(statusPath, repo, main, part); err != nil {
			unavailable = true
			runErr = errors.Join(runErr, err)
		}
		if part.Outcome != "ok" {
			if log != nil {
				log(fmt.Sprintf("Main stage %s %s: %s", part.Stage, part.Outcome, part.Reason))
			}
			unavailable = unavailable || part.Outcome == reviewUnavailable
		}
	}
	return unavailable, runErr
}

func recordReviewMain(path, repo, head string, part hq.ReviewBuildPart) error {
	stage := mate.StageStatus{Hostname: part.Part, Repo: repo, MainFollow: topology.MainFollowStatus{
		Head: head, Outcome: part.Outcome, Reason: part.Reason,
	}}
	if err := mate.RecordStageMainFollow(path, stage); err != nil {
		return fmt.Errorf("record main-follow result for %s: %w", part.Part, err)
	}
	return nil
}

// Only a stage still owned by the recorded repository carries its last result.
func attachStageMainFollow(stages []workflow.WhereStage, recorded []mate.StageStatus) {
	for i := range stages {
		for _, result := range recorded {
			if stages[i].Hostname != result.Hostname || stages[i].Repo == "" || stages[i].Repo != result.Repo {
				continue
			}
			follow := result.MainFollow
			stages[i].MainFollow = fmt.Sprintf("main %s: %s", follow.Head, follow.Outcome)
			if follow.Reason != "" {
				stages[i].MainFollow += " (" + follow.Reason + ")"
			}
			break
		}
	}
}

func mainFollowStatusGuidance(env workflow.StateEnvelope, statusPath string) string {
	status, err := mate.ReadStatus(statusPath)
	if err != nil {
		return ""
	}
	var stages []workflow.WhereStage
	for _, service := range env.WithParts(nil).Services {
		for _, stage := range service.WireStages {
			stages = append(stages, workflow.WhereStage{Hostname: stage.Hostname, Repo: service.HQRepo})
		}
	}
	attachStageMainFollow(stages, status.Stages)
	var lines []string
	for _, stage := range stages {
		if stage.MainFollow != "" {
			lines = append(lines, fmt.Sprintf("- stage %s: %s", stage.Hostname, stage.MainFollow))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "Main-follow results:\n" + strings.Join(lines, "\n")
}
