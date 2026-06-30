package ops

import (
	"context"
	"sort"

	"github.com/zeropsio/zcp/internal/platform"
)

// ServiceActivity describes a LIVE in-flight platform operation on one service.
// It is attached to discover's ServiceInfo and consulted by the adopt gate.
//
// The struct exists because a service's resting status (READY_TO_DEPLOY,
// ACTIVE) cannot tell "genuinely idle" from "a build/deploy is running right
// now" — a first buildFromGit deploy reads READY_TO_DEPLOY the entire time a
// build is in flight (live-verified, eval 2026-06-30). Activity carries the
// missing fact.
type ServiceActivity struct {
	// Action is the human-readable operation: build | deploy | restart | scale |
	// start | stop | import (mapped from the live process action; refined to
	// build/deploy by the embedded appVersion phase).
	Action string `json:"action"`
	// Status is the build/deploy PHASE (BUILDING | DEPLOYING) when the build
	// process carries an in-progress appVersion, else the live process status
	// (RUNNING | PENDING | ROLLBACKING | CANCELING).
	Status string `json:"status"`
	// ProcessID is the live process behind the busy verdict. ALWAYS present: a
	// busy service iff a live process references it, so this is the cancel-escape
	// (zerops_process action=cancel) and the loop-safety invariant — every
	// refusal can name a cancelable process.
	ProcessID string `json:"processId"`
}

// ProjectActivity returns hostname -> ServiceActivity for every BUSY service in
// the project (idle services are absent). A service is BUSY iff a live process
// (status PENDING/RUNNING/ROLLBACKING/CANCELING) references its serviceStackId —
// the SOLE busy-truth. The embedded appVersion phase only refines the
// build/deploy LABEL of an already-busy build process; it NEVER makes a service
// busy on its own (a stuck BUILDING with no process has no cancel-escape — gating
// on it would deadlock).
//
// The source is the DIRECT GET /project/{id}/process (client.GetProjectProcessesDirect),
// NOT the Elasticsearch /process/search: the ES search trails the DB after an
// import (load-dependent), so a just-started build could read "not busy" while it
// is in fact building. The direct read is authoritative + lag-free. Processes
// arrive in unspecified order, so they are sorted newest-first here; the first
// live process per known service wins (the current operation, carrying the
// cancelable processId).
//
// The caller passes its already-built serviceID->hostname map (built from
// DiscoverResult.Services / the adopt candidate list). The ephemeral build
// container (a distinct serviceStackId in the stack.build process's
// serviceStacks[] but never in idToHost) is skipped — matching the known target
// id is unambiguous.
//
// Single owner: both the discover steer and the adopt gate call this, so the
// "is it busy" question is answered one way.
func ProjectActivity(ctx context.Context, client platform.Client, projectID string, idToHost map[string]string) (map[string]ServiceActivity, error) {
	processes, err := client.GetProjectProcessesDirect(ctx, projectID)
	if err != nil {
		return nil, err
	}

	// Newest-first by created (the endpoint ordering is unspecified). Malformed
	// timestamps fall back to a string compare; identical instants tie-break on
	// the string — mirrors events.go::sortTimelineDescending.
	sort.SliceStable(processes, func(i, j int) bool {
		ti, ei := parseTimestamp(processes[i].Created)
		tj, ej := parseTimestamp(processes[j].Created)
		if ei != nil || ej != nil {
			return processes[i].Created > processes[j].Created
		}
		if ti.Equal(tj) {
			return processes[i].Created > processes[j].Created
		}
		return ti.After(tj)
	})

	// The process arm — the sole busy-truth. First live process per known
	// service wins. Build-container / unknown refs are skipped.
	busyProc := make(map[string]platform.Process)
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
		// the embedded appVersion is the only source of the build-vs-deploy phase.
		// A lifecycle process (restart/scale/...) is self-describing.
		if proc.ActionName == actionStackBuild && proc.AppVersion != nil && isAppVersionInProgress(proc.AppVersion.Status) {
			status = proc.AppVersion.Status
			if proc.AppVersion.Status == platform.BuildStatusDeploying {
				action = "deploy"
			} else {
				action = "build"
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

// isAppVersionInProgress reports whether an appVersion status is a live
// build/deploy PHASE label ({BUILDING, DEPLOYING}). Used ONLY to refine the
// label of a service already proven busy by a live process — it never makes a
// service busy on its own. Terminal/queued/failed states (ACTIVE, BUILD_FAILED,
// WAITING_TO_BUILD, ...) return false, so a frozen WAITING_TO_BUILD or a stuck
// BUILDING can never false-label.
func isAppVersionInProgress(status string) bool {
	switch status {
	case platform.BuildStatusBuilding, platform.BuildStatusDeploying:
		return true
	default:
		return false
	}
}
