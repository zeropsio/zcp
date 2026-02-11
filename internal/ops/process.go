package ops

import (
	"context"
	"fmt"

	"github.com/zeropsio/zcp/internal/platform"
)

const (
	statusFinished = "FINISHED"
	statusFailed   = "FAILED"
	statusCanceled = "CANCELED"
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
		return nil, platform.NewPlatformError(platform.ErrProcessNotFound,
			fmt.Sprintf("Process '%s' not found", processID), "Check the process ID")
	}

	if isTerminal(p.Status) {
		return nil, platform.NewPlatformError(platform.ErrProcessAlreadyTerminal,
			fmt.Sprintf("Process '%s' is already %s", processID, p.Status),
			"Only PENDING or RUNNING processes can be canceled")
	}

	_, err = client.CancelProcess(ctx, processID)
	if err != nil {
		return nil, fmt.Errorf("cancel process %s: %w", processID, err)
	}

	return &ProcessCancelResult{
		ProcessID: processID,
		Status:    statusCanceled,
		Message:   fmt.Sprintf("Process %s canceled", processID),
	}, nil
}

func isTerminal(status string) bool {
	return status == statusFinished || status == statusFailed || status == statusCanceled
}
