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
)

const (
	reviewUnavailable   = "unavailable"
	reviewOutcomeFailed = "failed"
	reviewPhaseBuild    = "build"
	reviewPhaseDeploy   = "deploy"
)

// reviewNext decides from HQ's adding phase and this process's running set.
func reviewNext(c hq.MateChange, running bool, _ time.Time) reviewAction {
	if running || c.Repo == hq.RecipeRepo || c.Head == nil || *c.Head == "" || c.ChecksGivenUp != nil {
		return reviewNothing
	}
	if c.State == hq.ChangeAdding {
		return reviewTake
	}
	if c.State == hq.ChangeMerging {
		return reviewRun
	}
	return reviewNothing
}

// reviewMainNext follows main only while no change in the repository is adding.
func reviewMainNext(repo hq.MateRepo, running bool) bool {
	return !running && !repo.Adding && repo.MainHead != nil && *repo.MainHead != ""
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
	reviewUnknown
)

type reviewVersion struct {
	action                      reviewVersionAction
	id, process, status, reason string
}

// reviewVersionOf projects the named, direct stage versions and their builds.
func reviewVersionOf(head, service, active string, versions []platform.AppVersionEvent, processes []platform.Process) reviewVersion {
	for _, p := range processes {
		if p.ActionName != "stack.build" || p.AppVersion == nil || p.AppVersion.ID == "" || p.AppVersion.Name != ops.AppVersionName(head) || !reviewTouches(p, service) {
			continue
		}
		if ops.IsProcessLive(p.Status) && p.ID != "" {
			return reviewVersion{action: reviewJoin, id: p.AppVersion.ID, process: p.ID}
		}
	}
	for _, v := range versions {
		if v.Name == ops.AppVersionName(head) && v.ID == active {
			return reviewVersion{action: reviewProbe, id: v.ID, status: v.Status}
		}
	}
	for _, v := range versions {
		if v.Name != ops.AppVersionName(head) || v.Status == platform.AppVersionStatusUploading {
			continue
		}
		reason := v.Status
		failed := reviewFailedStatus(v.Status)
		for _, p := range processes {
			if p.AppVersion != nil && p.AppVersion.ID == v.ID && p.Status == platform.ProcessStatusFailed && reviewTouches(p, service) {
				failed = true
				if p.FailReason != nil && *p.FailReason != "" {
					reason = *p.FailReason
				}
			}
		}
		if failed {
			return reviewVersion{action: reviewFailed, id: v.ID, status: v.Status, reason: reason}
		}
		if v.Status == platform.BuildStatusBackup || v.Status == platform.BuildStatusDeployed {
			continue
		}
		return reviewVersion{action: reviewUnknown, id: v.ID, status: v.Status}
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

// reviewRetryDelay bounds retries of an unreachable infrastructure call.
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
