package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// waitTotalBudget bounds a single zerops_process action="wait" call. Matches the
// build-poll horizon (defaultBuildPollConfig) — long enough for a real build to
// finish, short enough that a wedged op returns a soft "still running" the agent
// can re-call rather than hanging the session. Progress notifications keep the
// MCP connection alive across it (same as the deploy/import long-polls).
const waitTotalBudget = 15 * time.Minute

// Local aliases of platform process/build status values — kept so call
// sites stay readable without sprinkling the platform. prefix everywhere.
const (
	statusActive      = platform.ServiceStatusActive
	statusBuilding    = platform.BuildStatusBuilding
	statusBuildFailed = platform.BuildStatusBuildFailed
	statusFinished    = platform.ProcessStatusFinished
	statusFailed      = platform.ProcessStatusFailed
	statusCanceled    = platform.ProcessStatusCanceled
)

// ProcessStatusResult contains the status of a process.
type ProcessStatusResult struct {
	ProcessID  string  `json:"processId"`
	Action     string  `json:"actionName"`
	Status     string  `json:"status"`
	Created    string  `json:"created"`
	Started    *string `json:"started,omitempty"`
	Finished   *string `json:"finished,omitempty"`
	FailReason *string `json:"failReason,omitempty"`
}

// ProcessCancelResult contains the result of a process cancellation.
type ProcessCancelResult struct {
	ProcessID string `json:"processId"`
	Status    string `json:"status"`
	Message   string `json:"message"`
}

// GetProcessStatus retrieves the current status of an async process.
func GetProcessStatus(ctx context.Context, client platform.Client, processID string) (*ProcessStatusResult, error) {
	if processID == "" {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			"Process ID is required", "Provide a valid process ID")
	}

	p, err := client.GetProcess(ctx, processID)
	if err != nil {
		var pe *platform.PlatformError
		if errors.As(err, &pe) {
			return nil, pe
		}
		return nil, platform.NewPlatformError(platform.ErrProcessNotFound,
			fmt.Sprintf("Process '%s' not found", processID), "Check the process ID")
	}

	return &ProcessStatusResult{
		ProcessID:  p.ID,
		Action:     p.ActionName,
		Status:     p.Status,
		Created:    p.Created,
		Started:    p.Started,
		Finished:   p.Finished,
		FailReason: p.FailReason,
	}, nil
}

// CancelProcess cancels a running or pending process.
// Returns PROCESS_ALREADY_TERMINAL if the process is in a terminal state.
func CancelProcess(ctx context.Context, client platform.Client, processID string) (*ProcessCancelResult, error) {
	if processID == "" {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			"Process ID is required", "Provide a valid process ID")
	}

	p, err := client.GetProcess(ctx, processID)
	if err != nil {
		var pe *platform.PlatformError
		if errors.As(err, &pe) {
			return nil, pe
		}
		return nil, platform.NewPlatformError(platform.ErrProcessNotFound,
			fmt.Sprintf("Process '%s' not found", processID), "Check the process ID")
	}

	if !isProcessCancelable(p.Status) {
		return nil, platform.NewPlatformError(platform.ErrProcessAlreadyTerminal,
			fmt.Sprintf("Process '%s' is already %s", processID, p.Status),
			"Only PENDING or RUNNING processes can be canceled")
	}

	_, err = client.CancelProcess(ctx, processID)
	if err != nil {
		var pe *platform.PlatformError
		if errors.As(err, &pe) {
			return nil, pe
		}
		return nil, fmt.Errorf("cancel process %s: %w", processID, err)
	}

	return &ProcessCancelResult{
		ProcessID: processID,
		Status:    statusCanceled,
		Message:   fmt.Sprintf("Process %s canceled", processID),
	}, nil
}

// isProcessCancelable reports whether a process can still be canceled: only
// PENDING or RUNNING. This is DELIBERATELY NARROWER than ops.IsProcessLive (which
// also counts ROLLBACKING/CANCELING as in-flight): you cannot cancel a process
// that is already rolling back or canceling. Cancel-eligibility ONLY — the poll
// loop uses IsProcessLive so it waits through a rollback/cancel to the true
// terminal state, never returning a non-terminal ROLLBACKING/CANCELING as "done".
func isProcessCancelable(status string) bool {
	switch status {
	case platform.ProcessStatusPending, platform.ProcessStatusRunning:
		return true
	default:
		return false
	}
}

// WaitResult is the outcome of zerops_process action="wait": the final (or
// last-observed) status of each process waited on, plus whether the wait SETTLED
// (every target reached terminal / the service drained) or TIMED OUT still
// in-flight. A timeout is NOT an error — the agent re-calls wait or checks
// status. A FAILED process IS reported with settled=true, and the message flags
// it, so "done waiting" can never be misread as "succeeded".
type WaitResult struct {
	Processes []ProcessStatusResult `json:"processes"`
	Settled   bool                  `json:"settled"`
	TimedOut  bool                  `json:"timedOut,omitempty"`
	Message   string                `json:"message"`
}

// WaitProcesses blocks until each given process reaches a terminal state
// (reusing PollProcess), then returns their final statuses. The wait set is
// FIXED at call time — duplicate/empty IDs are dropped, and an op that starts
// later is NOT picked up (deterministic; immune to unrelated churn like an
// autoscale or a crash-restart loop). A poll/total-budget timeout returns a soft
// TimedOut result (not an error), so a slow build never surfaces as a failure.
// onProgress may be nil.
func WaitProcesses(ctx context.Context, client ProcessGetter, processIDs []string, onProgress ProgressCallback) (*WaitResult, error) {
	ids := dedupeNonEmpty(processIDs)
	if len(ids) == 0 {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			"No process to wait on", "Provide processId, processIds, or service")
	}
	ctx, cancel := context.WithTimeout(ctx, waitTotalBudget)
	defer cancel()

	results, timedOut, blockingID, err := waitForProcessSet(ctx, client, ids, onProgress)
	if err != nil {
		return nil, err
	}
	if timedOut {
		return timedOutWait(results,
			fmt.Sprintf("Process %s still running after the wait budget — re-call wait, or check zerops_process action=\"status\".", blockingID)), nil
	}
	return &WaitResult{
		Processes: results,
		Settled:   true,
		Message:   settledMessage(results, fmt.Sprintf("%d process(es) reached a terminal state.", len(results))),
	}, nil
}

// WaitServiceSettled resolves the service's CURRENTLY-live process set once and
// waits exactly that set to terminal. Because the DIRECT read surfaces every
// concurrent op at-creation (build + the subdomain-enable queued behind it +
// create), the resolved set is complete — there is nothing to "catch later", and
// not re-polling for new ops keeps the wait deterministic and immune to unrelated
// churn (an autoscale, a crash-restart loop) that would otherwise never settle.
// `service=<host>` is hostname-grain sugar over WaitProcesses: it resolves the
// fixed set for the agent (freshly, server-side) instead of the agent threading
// process IDs. A poll/total-budget timeout returns a soft TimedOut result.
// onProgress may be nil.
func WaitServiceSettled(ctx context.Context, client platform.Client, projectID, hostname string, onProgress ProgressCallback) (*WaitResult, error) {
	if hostname == "" {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			"Service hostname required", "Provide service=<hostname>")
	}
	ctx, cancel := context.WithTimeout(ctx, waitTotalBudget)
	defer cancel()

	services, err := client.ListServicesDirect(ctx, projectID)
	if err != nil {
		// A slow/timed-out read is "still in flight, re-call", not a failure —
		// same soft contract the poll path honors (a hard error here would break
		// the wait primitive's no-error promise on a transient API hiccup).
		if isWaitTimeout(err) {
			return timedOutWait(nil, fmt.Sprintf("Service %q — read timed out before resolving its work; re-call wait, or re-run zerops_discover.", hostname)), nil
		}
		return nil, fmt.Errorf("wait service %s: list services: %w", hostname, err)
	}
	var targetID string
	for i := range services {
		if services[i].Name == hostname {
			targetID = services[i].ID
			break
		}
	}
	if targetID == "" {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			fmt.Sprintf("No service %q in this project", hostname),
			"Check the hostname with zerops_discover")
	}

	activity, err := ProjectActivity(ctx, client, projectID, map[string]string{targetID: hostname})
	if err != nil {
		if isWaitTimeout(err) {
			return timedOutWait(nil, fmt.Sprintf("Service %q — activity read timed out; re-call wait, or re-run zerops_discover.", hostname)), nil
		}
		return nil, fmt.Errorf("wait service %s: %w", hostname, err)
	}
	live := activity[hostname]
	if len(live) == 0 {
		// Nothing live: the service is settled. If its newest process FAILED (e.g.
		// a <1s clone-preflight fast-fail that terminated before we looked), surface
		// it so a settled verdict is never misread as "succeeded".
		var results []ProcessStatusResult
		if failed := newestFailedProcessForService(ctx, client, projectID, targetID); failed != nil {
			results = append(results, *failed)
		}
		return &WaitResult{
			Processes: results,
			Settled:   true,
			Message:   settledMessage(results, fmt.Sprintf("Service %q settled — no live process.", hostname)),
		}, nil
	}

	ids := make([]string, 0, len(live))
	for _, op := range live {
		ids = append(ids, op.ProcessID)
	}
	results, timedOut, blockingID, err := waitForProcessSet(ctx, client, ids, onProgress)
	if err != nil {
		return nil, fmt.Errorf("wait service %s: %w", hostname, err)
	}
	if timedOut {
		return timedOutWait(results,
			fmt.Sprintf("Service %q still has work in flight (processId=%s) after the wait budget — re-call wait, or re-run zerops_discover.", hostname, blockingID)), nil
	}
	return &WaitResult{
		Processes: results,
		Settled:   true,
		Message:   settledMessage(results, fmt.Sprintf("Service %q settled — waited %d in-flight op(s).", hostname, len(results))),
	}, nil
}

// waitForProcessSet polls each id in the FIXED set to terminal, in order. Returns
// the per-process final statuses, whether the wait timed out (soft — caller turns
// it into a TimedOut result), and the id that was still in flight at timeout.
// ids must already be deduped/non-empty.
func waitForProcessSet(ctx context.Context, client ProcessGetter, ids []string, onProgress ProgressCallback) (results []ProcessStatusResult, timedOut bool, blockingID string, err error) {
	for _, id := range ids {
		proc, perr := PollProcess(ctx, client, id, onProgress)
		if perr != nil {
			if isWaitTimeout(perr) {
				return results, true, id, nil
			}
			return nil, false, "", fmt.Errorf("wait process %s: %w", id, perr)
		}
		results = append(results, toStatusResult(proc))
	}
	return results, false, "", nil
}

// newestFailedProcessForService returns the service's newest process when that
// process is FAILED, else nil. Used by WaitServiceSettled to flag a build that
// failed before the wait even saw it live (the <1s fast-fail), so "settled" is
// never read as "succeeded". A newest process that FINISHED (e.g. a successful
// redeploy after an old failure) returns nil — only the latest verdict matters.
func newestFailedProcessForService(ctx context.Context, client platform.Client, projectID, serviceID string) *ProcessStatusResult {
	procs, err := client.GetProjectProcessesDirect(ctx, projectID)
	if err != nil {
		return nil
	}
	var newest *platform.Process
	for i := range procs {
		refsService := false
		for _, ref := range procs[i].ServiceStacks {
			if ref.ID == serviceID {
				refsService = true
				break
			}
		}
		if !refsService {
			continue
		}
		if newest == nil || procs[i].Created > newest.Created {
			newest = &procs[i]
		}
	}
	if newest == nil || newest.Status != platform.ProcessStatusFailed {
		return nil
	}
	r := toStatusResult(newest)
	return &r
}

// toStatusResult projects a polled Process onto the agent-facing status shape.
func toStatusResult(p *platform.Process) ProcessStatusResult {
	return ProcessStatusResult{
		ProcessID:  p.ID,
		Action:     p.ActionName,
		Status:     p.Status,
		Created:    p.Created,
		Started:    p.Started,
		Finished:   p.Finished,
		FailReason: p.FailReason,
	}
}

// dedupeNonEmpty returns the input IDs with blanks and duplicates removed,
// preserving first-seen order.
func dedupeNonEmpty(ids []string) []string {
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// isWaitTimeout reports whether an error means "still in flight, not a failure":
// the PollProcess API_TIMEOUT or the total-budget context deadline.
func isWaitTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var pe *platform.PlatformError
	return errors.As(err, &pe) && pe.Code == platform.ErrAPITimeout
}

func timedOutWait(results []ProcessStatusResult, msg string) *WaitResult {
	return &WaitResult{Processes: results, Settled: false, TimedOut: true, Message: msg}
}

// settledMessage flags any FAILED process so the agent never reads "done
// waiting" as "succeeded".
func settledMessage(results []ProcessStatusResult, base string) string {
	var failed []string
	for _, r := range results {
		if r.Status == statusFailed {
			failed = append(failed, r.ProcessID)
		}
	}
	if len(failed) > 0 {
		return base + fmt.Sprintf(" WARNING: %d operation(s) FAILED (%s) — check zerops_logs / zerops_events before proceeding.", len(failed), strings.Join(failed, ", "))
	}
	return base
}
