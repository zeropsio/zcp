package sync

import "fmt"

// Status represents the outcome of a sync operation.
type Status int

const (
	Created Status = iota
	Updated
	Skipped
	DryRun
	Error
)

func (s Status) String() string {
	switch s {
	case Created:
		return "created"
	case Updated:
		return "updated"
	case Skipped:
		return "skipped"
	case DryRun:
		return "dry-run"
	case Error:
		return "error"
	default:
		return fmt.Sprintf("unknown(%d)", int(s))
	}
}

// PushResult holds the outcome of pushing a single slug.
type PushResult struct {
	Slug   string
	Status Status
	Reason string // for Skipped
	PRURL  string // for Created
	Diff   string // for DryRun
	Err    error  // for Error
}

// PullResult holds the outcome of pulling a single slug.
type PullResult struct {
	Slug   string
	Status Status
	Reason string
	Diff   string
}
