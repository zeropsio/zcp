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
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/topology"
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

// reviewCandidateName is the key a Review build names its candidate's version
// by (ops.AppVersionName): "review" and a hash of main and the change head.
// The marker keeps it from ever being a commit id, so a stage running the
// candidate shows no commit (topology.AppVersionCommit).
func reviewCandidateName(main *string, head string) string {
	base := ""
	if main != nil {
		base = *main
	}
	sum := sha256.Sum256([]byte(base + "\x00" + head))
	return fmt.Sprintf("review %x", sum[:20])
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
			if change.State != hq.ChangeMerging && (alreadyMerging || change.State == hq.ChangeMerged || change.State == hq.ChangeClosed || change.State == hq.ChangeOpen || change.ReviewBuild != nil && change.ReviewBuild.Take == take && change.ReviewBuild.State == hq.ReviewBuildPassing) {
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
