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
// (reusing PollProcess), then returns their final statuses. A poll timeout or
// the total-budget deadline returns a soft TimedOut result (not an error), so a
// slow build never surfaces as a failure. Duplicate/empty IDs are dropped.
// onProgress may be nil.
func WaitProcesses(ctx context.Context, client ProcessGetter, processIDs []string, onProgress ProgressCallback) (*WaitResult, error) {
	ids := dedupeNonEmpty(processIDs)
	if len(ids) == 0 {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			"No process to wait on", "Provide processId, processIds, or service")
	}
	ctx, cancel := context.WithTimeout(ctx, waitTotalBudget)
	defer cancel()

	var results []ProcessStatusResult
	for _, id := range ids {
		proc, err := PollProcess(ctx, client, id, onProgress)
		if err != nil {
			if isWaitTimeout(err) {
				return timedOutWait(results,
					fmt.Sprintf("Process %s still running after the wait budget — re-call wait, or check zerops_process action=\"status\".", id)), nil
			}
			return nil, fmt.Errorf("wait process %s: %w", id, err)
		}
		results = append(results, toStatusResult(proc))
	}
	return &WaitResult{
		Processes: results,
		Settled:   true,
		Message:   settledMessage(results, fmt.Sprintf("%d process(es) reached a terminal state.", len(results))),
	}, nil
}

// WaitServiceSettled blocks until the named service has NO live process. It
// drains build, deploy, and any queued lifecycle op (e.g. a subdomain-enable
// sitting PENDING behind the build), re-checking the service's live set after
// each poll so an op that starts mid-wait is also awaited. This is the universal
// "wait until ready" primitive — it needs zero operation-type knowledge, so an
// unknown future op is drained identically. A poll/total-budget timeout returns
// a soft TimedOut result. onProgress may be nil.
func WaitServiceSettled(ctx context.Context, client platform.Client, projectID, hostname string, onProgress ProgressCallback) (*WaitResult, error) {
	if hostname == "" {
		return nil, platform.NewPlatformError(platform.ErrInvalidParameter,
			"Service hostname required", "Provide service=<hostname>")
	}
	services, err := client.ListServicesDirect(ctx, projectID)
	if err != nil {
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
	idToHost := map[string]string{targetID: hostname}

	ctx, cancel := context.WithTimeout(ctx, waitTotalBudget)
	defer cancel()

	polled := map[string]ProcessStatusResult{}
	var order []string
	// maxRounds is a backstop against an op that keeps respawning; the real bound
	// is waitTotalBudget. A settled service exits in one or two rounds.
	const maxRounds = 60
	firstRound := true
	for range maxRounds {
		activity, aerr := ProjectActivity(ctx, client, projectID, idToHost)
		if aerr != nil {
			if isWaitTimeout(aerr) {
				return timedOutWait(orderedResults(polled, order),
					fmt.Sprintf("Service %q did not settle within the wait budget — re-call wait, or re-run zerops_discover.", hostname)), nil
			}
			return nil, fmt.Errorf("wait service %s: %w", hostname, aerr)
		}
		live := activity[hostname]
		if len(live) == 0 {
			results := orderedResults(polled, order)
			// If nothing was ever live when we looked (the op terminated before the
			// first read — e.g. a <1s clone-preflight fast-fail), surface the
			// service's newest process when it FAILED, so a settled verdict is never
			// misread as "succeeded".
			if firstRound && len(results) == 0 {
				if failed := newestFailedProcessForService(ctx, client, projectID, targetID); failed != nil {
					results = append(results, *failed)
				}
			}
			return &WaitResult{
				Processes: results,
				Settled:   true,
				Message:   settledMessage(results, fmt.Sprintf("Service %q settled — no live process.", hostname)),
			}, nil
		}
		firstRound = false
		for _, op := range live {
			proc, perr := PollProcess(ctx, client, op.ProcessID, onProgress)
			if perr != nil {
				if isWaitTimeout(perr) {
					return timedOutWait(orderedResults(polled, order),
						fmt.Sprintf("Service %q still has work in flight (%s, processId=%s) after the wait budget — re-call wait, or re-run zerops_discover.", hostname, op.Action, op.ProcessID)), nil
				}
				return nil, fmt.Errorf("wait service %s process %s: %w", hostname, op.ProcessID, perr)
			}
			if _, seen := polled[proc.ID]; !seen {
				order = append(order, proc.ID)
			}
			polled[proc.ID] = toStatusResult(proc)
		}
	}
	// Backstop hit. One final drain check: the last polled op may have been the
	// last live one, so re-read before declaring a timeout.
	if activity, aerr := ProjectActivity(ctx, client, projectID, idToHost); aerr == nil && len(activity[hostname]) == 0 {
		return &WaitResult{
			Processes: orderedResults(polled, order),
			Settled:   true,
			Message:   settledMessage(orderedResults(polled, order), fmt.Sprintf("Service %q settled — no live process.", hostname)),
		}, nil
	}
	return timedOutWait(orderedResults(polled, order),
		fmt.Sprintf("Service %q did not settle within the wait budget — re-call wait, or re-run zerops_discover.", hostname)), nil
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

// orderedResults flattens the polled-status map in first-seen order.
func orderedResults(m map[string]ProcessStatusResult, order []string) []ProcessStatusResult {
	out := make([]ProcessStatusResult, 0, len(order))
	for _, id := range order {
		out = append(out, m[id])
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
