package mate

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// RestartReadiness observes only the unit attempt started after its snapshot.
// Take a fresh snapshot before each nonblocking restart, including rollback.
type RestartReadiness struct{ before readinessUnitState }

// ObserveRestart captures the old unit identity before requesting a restart.
// An unavailable observation is not proof of exit: HTTP readiness and its
// existing deadline still decide the result when unit state cannot be read.
func ObserveRestart(ctx context.Context) RestartReadiness {
	return RestartReadiness{before: readReadinessUnit(ctx)}
}

// WaitReady retains the complete readiness proof, but stops when the restarted
// unit's process has exited or the new unit attempt has failed.
func (r RestartReadiness) WaitReady(version, oldBoot string, deadline time.Time) error {
	return waitReady(version, oldBoot, deadline, &r.before)
}

const failedUnitState = "failed"

type readinessUnitState struct {
	valid                   bool
	mainPID                 int
	active, sub, invocation string
	start                   uint64
	status                  int
}

func readReadinessUnit(ctx context.Context) readinessUnitState {
	// Use the existing one-second probe bound within the overall startup budget.
	probeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	out, err := exec.CommandContext(probeCtx, "systemctl", "show", "zerops@mate.service", "--property=MainPID,ActiveState,SubState,ExecMainStatus,InvocationID,ExecMainStartTimestampMonotonic").Output()
	if err != nil {
		return readinessUnitState{}
	}
	values := make(map[string]string)
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok {
			values[key] = value
		}
	}
	pid, pidErr := strconv.Atoi(values["MainPID"])
	status, statusErr := strconv.Atoi(values["ExecMainStatus"])
	start, startErr := strconv.ParseUint(values["ExecMainStartTimestampMonotonic"], 10, 64)
	if pidErr != nil || statusErr != nil || startErr != nil || values["ActiveState"] == "" || values["SubState"] == "" {
		return readinessUnitState{}
	}
	return readinessUnitState{valid: true, mainPID: pid, active: values["ActiveState"], sub: values["SubState"], status: status, invocation: values["InvocationID"], start: start}
}

func exitedReadinessUnit(ctx context.Context, before *readinessUnitState) error {
	if before == nil || !before.valid {
		return nil
	}
	current := readReadinessUnit(ctx)
	if !current.valid {
		// Unknown state cannot prove that a process exited.
		return nil
	}
	newStart := current.start > before.start
	newInvocation := current.invocation != "" && current.invocation != before.invocation
	// Stopping the old invocation can fail before the candidate starts.
	// A failed transition alone cannot attribute that exit to the candidate.
	newlyFailed := current.active == failedUnitState && (newInvocation || newStart)
	if newlyFailed || (newStart && current.mainPID == 0) || (newInvocation && current.sub == "auto-restart") {
		return fmt.Errorf("mate unit process exited: MainPID=%d ActiveState=%s SubState=%s ExecMainStatus=%d", current.mainPID, current.active, current.sub, current.status)
	}
	return nil
}
