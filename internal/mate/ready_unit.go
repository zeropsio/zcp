package mate

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// RestartReadiness observes only the unit attempt started after its snapshot.
// Take a fresh snapshot before each nonblocking restart, including rollback.
type RestartReadiness struct {
	before readinessUnitState
	// show runs the unit-properties query; nil runs it as a process.
	show unitQuery
}

// unitQuery runs the command name with args and returns its standard output.
type unitQuery func(ctx context.Context, name string, args ...string) ([]byte, error)

func execUnitQuery(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// ObserveRestart captures the old unit identity before requesting a restart and
// removes the boot file, so every receipt in it afterwards is the new boot's.
// An unavailable observation is not proof of exit: the boot's receipts and their
// progress window still decide the result when unit state cannot be read.
func ObserveRestart(ctx context.Context) RestartReadiness {
	return observeRestart(ctx, execUnitQuery)
}

func observeRestart(ctx context.Context, show unitQuery) RestartReadiness {
	_ = os.Remove(BootFilePath())
	return RestartReadiness{before: readReadinessUnit(ctx, show), show: show}
}

// WaitReady waits for the complete readiness proof while the restarted boot keeps
// advancing: it fails when the unit's process exits, the new unit attempt fails, the
// boot reports a failure or runs another version, or window passes without a
// receipt of boot progress. Elapsed time alone never fails a boot that advances.
func (r RestartReadiness) WaitReady(version, oldBoot string, window time.Duration) error {
	return waitReady(version, oldBoot, time.Now().Add(window), window, &r)
}

const failedUnitState = "failed"

type readinessUnitState struct {
	valid                   bool
	mainPID                 int
	active, sub, invocation string
	start                   uint64
	status                  int
}

func readReadinessUnit(ctx context.Context, show unitQuery) readinessUnitState {
	if show == nil {
		show = execUnitQuery
	}
	// Use the existing one-second probe bound of each readiness observation.
	probeCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	out, err := show(probeCtx, "systemctl", "show", "zerops@mate.service", "--property=MainPID,ActiveState,SubState,ExecMainStatus,InvocationID,ExecMainStartTimestampMonotonic")
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

func exitedReadinessUnit(ctx context.Context, restart *RestartReadiness) error {
	if restart == nil || !restart.before.valid {
		return nil
	}
	before := restart.before
	current := readReadinessUnit(ctx, restart.show)
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
