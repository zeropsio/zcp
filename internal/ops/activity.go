package ops

import (
	"context"
	"sync"

	"github.com/zeropsio/zcp/internal/platform"
)

// ServiceActivity describes a LIVE in-flight platform operation on one service.
// It is attached to discover's ServiceInfo and consulted by the adopt gate.
//
// The struct exists because a service's resting status (READY_TO_DEPLOY,
// ACTIVE) cannot tell "genuinely idle" from "a build/deploy is running right
// now" — a first buildFromGit deploy reads READY_TO_DEPLOY the entire time a
// build is in flight (live-verified, eval-zcp 2026-06-30). Activity carries the
// missing fact.
type ServiceActivity struct {
	// Action is the human-readable operation: build | deploy | restart | scale |
	// start | stop | import (mapped from the live process action; refined to
	// build/deploy by the appVersion phase).
	Action string `json:"action"`
	// Status is the build/deploy PHASE (BUILDING | DEPLOYING) when the latest
	// appVersion is in-progress, else the live process status (RUNNING |
	// PENDING).
	Status string `json:"status"`
	// ProcessID is the live process behind the busy verdict. ALWAYS present: a
	// busy service iff a live PENDING/RUNNING process references it, so this is
	// the cancel-escape (zerops_process action=cancel) and the loop-safety
	// invariant — every refusal can name a cancelable process.
	ProcessID string `json:"processId"`
}

// activityDefaultLimit is the search window when the caller passes limit<=0.
// Discover passes max(100, len(services)*5) so a busy project's in-progress
// rows are never evicted by the fixed-window default; this floor protects ad-hoc
// callers.
const activityDefaultLimit = 100

// ProjectActivity returns hostname -> ServiceActivity for every BUSY service in
// the project (idle services are absent). A service is BUSY iff a live process
// (status PENDING or RUNNING) references its serviceStackId — the SOLE
// busy-truth. The latest real appVersion only refines the build/deploy phase
// LABEL of an already-busy service; it NEVER makes a service busy on its own (a
// stuck BUILDING whose build container died has no process to cancel — gating
// on it would deadlock the gate forever).
//
// Two project-scoped searches run in parallel (the ops.Events pattern). The
// caller passes its already-built serviceID->hostname map (built from
// DiscoverResult.Services / the adopt candidate list) so there is no extra
// ListServices round-trip and no fake ServiceRef type. The ephemeral build
// container (a distinct serviceStackId that appears in the stack.build process's
// serviceStacks[] but never in idToHost) is skipped — matching the known target
// id is unambiguous because the build-container id never equals the target's
// (live-verified).
//
// Single owner: both the discover steer and the adopt gate call this, so the
// "is it busy" question is answered one way.
func ProjectActivity(ctx context.Context, client platform.Client, projectID string, idToHost map[string]string, limit int) (map[string]ServiceActivity, error) {
	if limit <= 0 {
		limit = activityDefaultLimit
	}

	var (
		processes   []platform.ProcessEvent
		appVersions []platform.AppVersionEvent
		procErr     error
		avErr       error
	)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		processes, procErr = client.SearchProcesses(ctx, projectID, limit)
	}()
	go func() {
		defer wg.Done()
		appVersions, avErr = client.SearchAppVersions(ctx, projectID, limit)
	}()
	wg.Wait()
	if procErr != nil {
		return nil, procErr
	}
	if avErr != nil {
		return nil, avErr
	}

	// The process arm — the sole busy-truth. SearchProcesses returns newest-first
	// by created; the first in-flight process per known service wins (it is the
	// current operation). Build-container / unknown refs are skipped.
	busyProc := make(map[string]platform.ProcessEvent)
	for i := range processes {
		p := processes[i]
		if !IsProcessLive(p.Status) {
			continue
		}
		for _, ref := range p.ServiceStacks {
			if _, known := idToHost[ref.ID]; !known {
				continue
			}
			if _, seen := busyProc[ref.ID]; !seen {
				busyProc[ref.ID] = p
			}
		}
	}

	result := make(map[string]ServiceActivity, len(busyProc))
	for serviceID, proc := range busyProc {
		host := idToHost[serviceID]
		action := mapActionName(proc.ActionName)
		status := proc.Status
		// Phase-label refinement ONLY (never the busy verdict), and ONLY for the
		// build process — a single stack.build process spans build AND deploy, so
		// the appVersion is the only source of the build-vs-deploy phase. A
		// lifecycle process (restart/scale/...) is self-describing; consulting the
		// appVersion there could mislabel it as "build" off a stale BUILDING row
		// (the process action stays authoritative).
		if proc.ActionName == actionStackBuild {
			if av := latestAppVersionForStack(appVersions, serviceID); av != nil && isAppVersionInProgress(av.Status) {
				status = av.Status
				if av.Status == platform.BuildStatusDeploying {
					action = "deploy"
				} else {
					action = "build"
				}
			}
		}
		result[host] = ServiceActivity{Action: action, Status: status, ProcessID: proc.ID}
	}
	return result, nil
}

// IsProcessLive reports whether a process is non-terminal (still in flight):
// PENDING, RUNNING, ROLLBACKING, or CANCELING. This is the busy-truth for
// activity detection and is DELIBERATELY BROADER than ops.isProcessInProgress
// (PENDING/RUNNING only — cancel-eligibility): a service mid-rollback or
// mid-cancel is still busy, but you cannot re-cancel an already-CANCELING
// process. The terminal set (FINISHED/FAILED/CANCELED) and any unknown status
// return false — unknown fails OPEN (degrade to activity-blind, never deadlock).
// Mirrors the SDK ProcessStatusEnum non-terminal members.
func IsProcessLive(status string) bool {
	switch status {
	case platform.ProcessStatusPending,
		platform.ProcessStatusRunning,
		platform.ProcessStatusRollbacking,
		platform.ProcessStatusCanceling:
		return true
	default:
		return false
	}
}

// latestAppVersionForStack returns the most-recent real appVersion for the
// service, or nil. "Latest" = created-desc first match (SearchAppVersions is
// newest-first); startWithoutCode stamps (Source=="NONE" && Build==nil) are
// skipped — they are never real builds. Mirrors the failed_context.go /
// events.go selection so the three callsites agree on "the latest appVersion".
func latestAppVersionForStack(avs []platform.AppVersionEvent, serviceID string) *platform.AppVersionEvent {
	for i := range avs {
		av := avs[i]
		if av.ServiceStackID != serviceID {
			continue
		}
		if av.Source == appVersionSourceNone && av.Build == nil {
			continue
		}
		return &avs[i]
	}
	return nil
}

// isAppVersionInProgress reports whether an appVersion status is a live
// build/deploy PHASE label ({BUILDING, DEPLOYING}). It is the shared owner of
// that set (next to appVersionHintMap / failed_context.go), used ONLY to refine
// the label of a service already proven busy by a live process — it never makes
// a service busy on its own. Terminal/queued/failed states (ACTIVE, BUILD_FAILED,
// WAITING_TO_BUILD, ...) return false, so a frozen WAITING_TO_BUILD or a stuck
// BUILDING with no process can never false-gate.
func isAppVersionInProgress(status string) bool {
	switch status {
	case platform.BuildStatusBuilding, platform.BuildStatusDeploying:
		return true
	default:
		return false
	}
}
