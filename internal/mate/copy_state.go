package mate

// CopyState is the state of a working copy's declared preview server.
type CopyState string

const (
	CopyParked   CopyState = "parked"
	CopyStarting CopyState = "starting"
	CopyRunning  CopyState = "running"
	CopyStopped  CopyState = "stopped"
	CopyFailed   CopyState = "failed"
	CopyGone     CopyState = "gone"
	CopyUnknown  CopyState = "unknown"
)

// CopyEvent is one event from the lease owner, supervisor, or its observer.
// Report is the supervisor's status for a report or wake (crashed is failed).
type CopyEvent struct {
	Kind   CopyEventKind
	Report string
}

type CopyEventKind string

const (
	CopyRecorded  CopyEventKind = "recorded"
	CopyStart     CopyEventKind = "start"
	CopyListening CopyEventKind = "listening"
	CopyStop      CopyEventKind = "stop"
	CopyExit      CopyEventKind = "exit"
	CopyPark      CopyEventKind = "park"
	CopyWake      CopyEventKind = "wake"
	CopyRelease   CopyEventKind = "release"
	CopyLinkLost  CopyEventKind = "link-lost"
	CopyReport    CopyEventKind = "report"
)

// NextCopyState is the only decision on a copy's state. accepted is false
// for an event the current state cannot take. A report is authoritative even
// when an intermediate event was superseded before the observer read it.
func NextCopyState(state CopyState, event CopyEvent) (next CopyState, accepted bool) {
	if state == CopyGone {
		return state, false
	}
	switch event.Kind {
	case CopyRelease:
		return CopyGone, true
	case CopyPark:
		if state != "" {
			return CopyParked, true
		}
	case CopyRecorded:
		if state == "" {
			return CopyStopped, true
		}
	case CopyLinkLost:
		return CopyUnknown, true
	case CopyReport, CopyWake:
		if event.Kind == CopyWake && state != CopyParked {
			break
		}
		if event.Kind == CopyReport && state == CopyParked {
			break
		}
		switch event.Report {
		case "stopped":
			return CopyStopped, true
		case "starting":
			return CopyStarting, true
		case "running":
			return CopyRunning, true
		case "crashed", "failed":
			return CopyFailed, true
		}
	case CopyStart:
		if state == CopyStopped || state == CopyRunning || state == CopyFailed || state == CopyStarting {
			return CopyStarting, true
		}
	case CopyListening:
		if state == CopyStarting {
			return CopyRunning, true
		}
	case CopyStop:
		if state == CopyStarting || state == CopyRunning || state == CopyFailed || state == CopyStopped {
			return CopyStopped, true
		}
	case CopyExit:
		if state == CopyStarting || state == CopyRunning || state == CopyFailed {
			return CopyFailed, true
		}
	}
	return state, false
}
