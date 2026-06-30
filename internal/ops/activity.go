package ops

import (
	"context"
	"sort"

	"github.com/zeropsio/zcp/internal/platform"
)

// LiveOp is ONE in-flight platform operation on a service. A service can carry
// SEVERAL concurrently — a buildFromGit import enqueues stack.build AND
// stack.enableSubdomainAccess at the same instant (live-verified eval
// 2026-06-30: the subdomain toggle sits PENDING, queued behind the build, for
// the build's entire duration). They are ALL reported (see ProjectActivity),
// never collapsed to one by a newest-wins tiebreak that would hide the
// substantive build behind a co-triggered toggle.
type LiveOp struct {
	// Action is the human-readable operation: build | deploy | restart | scale |
	// start | stop | import | subdomain-enable | ... (mapped from the live process
	// action; refined to build/deploy by the embedded appVersion phase).
	Action string `json:"action"`
	// Status is the build/deploy PHASE (BUILDING | DEPLOYING) when this is a build
	// process carrying an in-progress appVersion, else the live process status
	// (RUNNING | PENDING | ROLLBACKING | CANCELING).
	Status string `json:"status"`
	// ProcessID is the process behind this op. ALWAYS present: it is the
	// cancel-escape (zerops_process action="cancel"), the wait target
	// (action="wait"), and the loop-safety invariant — every busy verdict names a
	// cancelable/waitable process.
	ProcessID string `json:"processId"`
}

// ProjectActivity returns hostname -> the FULL set of its LIVE operations: every
// process with status PENDING/RUNNING/ROLLBACKING/CANCELING that references the
// service's serviceStackId. Idle services are absent (no empty slices), so a
// service is BUSY iff its list is non-empty — the SOLE busy-truth. The embedded
// appVersion phase only refines the build/deploy LABEL of a build op; it NEVER
// makes a service busy on its own (a stuck BUILDING with no live process has no
// cancel-escape — gating on it would deadlock).
//
// Reporting ALL live ops (not a single newest-wins representative) is the
// universal contract. A service genuinely can have concurrent ops, and
// collapsing them to one by timestamp hid the substantive build behind a
// co-triggered subdomain toggle (live-verified). Downstream this means:
//   - "is it done?" callers wait until the list DRAINS (WaitServiceSettled),
//     which naturally covers build -> queued subdomain-enable -> any straggler;
//   - gating callers (adopt) refuse while it is non-empty and can name EVERY
//     cancelable processId;
//   - no operation-type heuristic decides what to surface, so an unknown future
//     action is reported identically to a known one.
//
// The source is the DIRECT GET /project/{id}/process (lag-free, unlike the
// ES-backed /process/search which trails the DB after an import). Within each
// service the ops are ordered newest-first — presentation only; the list is
// lossless, so the order hides nothing. The ephemeral build container ref (a
// distinct serviceStackId never in idToHost) is skipped.
//
// Single owner: the discover steer, the adopt gate, and the wait action all read
// this, so "is it busy / what is in flight" is answered one way.
func ProjectActivity(ctx context.Context, client platform.Client, projectID string, idToHost map[string]string) (map[string][]LiveOp, error) {
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

	out := make(map[string][]LiveOp)
	for i := range processes {
		p := processes[i]
		if !IsProcessLive(p.Status) {
			continue
		}
		op := liveOpFromProcess(p)
		// Dedup within a single process: duplicate serviceStack refs (or two refs
		// mapping to the same hostname) must not list the same op twice for one
		// host. A process referencing two DISTINCT known hosts is still added to
		// both.
		seenHost := make(map[string]bool, len(p.ServiceStacks))
		for _, ref := range p.ServiceStacks {
			host, known := idToHost[ref.ID]
			if !known || seenHost[host] {
				continue
			}
			seenHost[host] = true
			out[host] = append(out[host], op)
		}
	}
	return out, nil
}

// liveOpFromProcess maps a live process to a LiveOp, refining a stack.build's
// label to build/deploy via its embedded appVersion phase — the only source of
// the build-vs-deploy distinction, since a single stack.build process spans both
// phases. A lifecycle process (restart/scale/subdomain/...) is self-describing,
// so a stray embedded appVersion on it is ignored.
func liveOpFromProcess(p platform.Process) LiveOp {
	action := mapActionName(p.ActionName)
	status := p.Status
	if p.ActionName == actionStackBuild && p.AppVersion != nil && isAppVersionInProgress(p.AppVersion.Status) {
		status = p.AppVersion.Status
		if p.AppVersion.Status == platform.BuildStatusDeploying {
			action = "deploy"
		} else {
			action = "build"
		}
	}
	return LiveOp{Action: action, Status: status, ProcessID: p.ID}
}

// IsProcessLive reports whether a process is non-terminal (still in flight):
// PENDING, RUNNING, ROLLBACKING, or CANCELING. This is the busy-truth for
// activity detection AND the poll-loop terminal check (PollProcess). It is
// DELIBERATELY BROADER than ops.isProcessCancelable (PENDING/RUNNING only —
// cancel-eligibility): a service mid-rollback or mid-cancel is still busy and
// still being polled, but you cannot re-cancel an already-CANCELING process. The
// terminal set (FINISHED/FAILED/CANCELED) and any unknown status
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
