package tools

import (
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/hq"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

type reviewAction uint8

const (
	reviewNothing reviewAction = iota
	reviewTake
	reviewRun
	reviewRetry
)

const (
	reviewAwaiting      = "awaiting"
	reviewFailing       = "failing"
	reviewUnavailable   = "unavailable"
	reviewOutcomeFailed = "failed"
	reviewPhaseBuild    = "build"
	reviewPhaseDeploy   = "deploy"
)

// reviewNext decides from HQ's change and the process's own running set.
func reviewNext(c hq.MateChange, running bool, now time.Time) reviewAction {
	if c.State != hq.ChangeOpen || c.Repo == hq.RecipeRepo || c.Head == nil || *c.Head == "" || c.Ready == nil || !*c.Ready || running {
		return reviewNothing
	}
	if c.ReviewBuild == nil || c.ReviewBuild.Head != *c.Head {
		return reviewTake
	}
	switch c.ReviewBuild.State {
	case reviewAwaiting:
		if until, err := time.Parse(time.RFC3339, c.ReviewBuild.Until); err == nil && now.After(until) {
			return reviewRetry
		}
		return reviewRun
	case reviewUnavailable:
		return reviewRetry
	default:
		return reviewNothing
	}
}

func reviewNeedsTake(action reviewAction, resultStored bool) bool {
	return action == reviewTake && !resultStored
}

// reviewStages reads the same per-pair setup records the stand-up writes.
func reviewStages(metas []*workflow.ServiceMeta, app, repo string) []topology.StageRef {
	var stages []topology.StageRef
	seen := make(map[string]bool)
	for _, meta := range metas {
		if meta == nil || meta.HQ == nil || meta.HQ.AppID != app || meta.HQ.Repo != repo {
			continue
		}
		for _, stage := range meta.Stages {
			if stage.Hostname == "" || seen[stage.Hostname] {
				continue
			}
			stages = append(stages, stage)
			seen[stage.Hostname] = true
		}
	}
	return stages
}

// reviewStageSetups fills an as-yet-unrecorded setup from the same tier rows
// the stand-up uses. A missing row is not a hostname-shaped guess.
func reviewStageSetups(stages []topology.StageRef, tier workflow.MateTier, repo string) error {
	for i := range stages {
		if stages[i].SetupName != "" {
			continue
		}
		for _, pair := range tier.Pairs {
			if pair.RepoName != repo {
				continue
			}
			for _, stage := range pair.Stages {
				if stage.Hostname == stages[i].Hostname {
					stages[i].SetupName = stage.Setup
				}
			}
		}
		if stages[i].SetupName == "" {
			return fmt.Errorf("setup for stage %s is not wired", stages[i].Hostname)
		}
	}
	return nil
}

type reviewVersionAction uint8

const (
	reviewBuild reviewVersionAction = iota
	reviewJoin
	reviewProbe
	reviewFailed
)

type reviewVersion struct {
	action                      reviewVersionAction
	id, process, status, reason string
}

// reviewVersionOf identifies versions only through the same named process
// records used by CommitDeploy. An identity that is not read permits a build.
func reviewVersionOf(head, service, active string, versions []platform.AppVersionEvent, processes []platform.Process) reviewVersion {
	var activeVersion, failed reviewVersion
	for _, p := range processes {
		if p.ActionName != "stack.build" || p.AppVersion == nil || p.AppVersion.ID == "" || p.AppVersion.Name != ops.AppVersionName(head) || !reviewTouches(p, service) {
			continue
		}
		if ops.IsProcessLive(p.Status) && p.ID != "" {
			return reviewVersion{action: reviewJoin, id: p.AppVersion.ID, process: p.ID}
		}
		if p.AppVersion.ID == active {
			activeVersion = reviewVersion{action: reviewProbe, id: active}
		}
		for _, v := range versions {
			if v.ID == p.AppVersion.ID && (reviewFailedStatus(v.Status) || p.Status == platform.ProcessStatusFailed) {
				reason := v.Status
				if p.FailReason != nil && *p.FailReason != "" {
					reason = *p.FailReason
				}
				failed = reviewVersion{action: reviewFailed, id: v.ID, status: v.Status, reason: reason}
				break
			}
		}
	}
	if activeVersion.id != "" {
		return activeVersion
	}
	if failed.id != "" {
		return failed
	}
	return reviewVersion{action: reviewBuild}
}

func reviewTouches(p platform.Process, service string) bool {
	for _, ref := range p.ServiceStacks {
		if ref.ID == service {
			return true
		}
	}
	return false
}

func reviewFailedStatus(status string) bool {
	return status == platform.BuildStatusBuildFailed || status == platform.BuildStatusDeployFailed || status == platform.BuildStatusPreparingRuntimeFail || status == "BUILD_VALIDATION_FAILED"
}

func reviewFailurePhase(status string) string {
	if status == platform.BuildStatusDeployFailed || status == platform.BuildStatusPreparingRuntimeFail {
		return reviewPhaseDeploy
	}
	return reviewPhaseBuild
}

func reviewReason(step, reason string) string {
	lines := strings.Split(strings.TrimSpace(reason), "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	runes := []rune(step + ": " + last)
	if len(runes) > 300 {
		runes = runes[:300]
	}
	return string(runes)
}

// reviewRetryDelay is the only retry clock; the fourth unavailable result
// stays until a change's head/state moves or the service starts again.
func reviewRetryDelay(tries int) time.Duration {
	switch tries {
	case 0:
		return time.Minute
	case 1:
		return 5 * time.Minute
	case 2:
		return 15 * time.Minute
	default:
		return 0
	}
}

// reviewUpdated compares the change's own revision time, not the arrival
// time of a Mate state frame. A stale frame must not restart a spent clock.
func reviewUpdated(current, previous string) bool {
	if current == "" || current == previous {
		return false
	}
	if previous == "" {
		return true
	}
	newTime, newErr := time.Parse(time.RFC3339Nano, current)
	oldTime, oldErr := time.Parse(time.RFC3339Nano, previous)
	return newErr == nil && oldErr == nil && newTime.After(oldTime)
}
