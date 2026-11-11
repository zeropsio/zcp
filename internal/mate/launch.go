package mate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// LaunchFilePath is where the launcher of the server (`zcp service start
// mate`, the unit's main process) reports its own steps of a launch, the part
// of a boot before the server writes BootFilePath.
func LaunchFilePath() string { return filepath.Join(Prefix(), "launch.json") }

// launchFile is one receipt in LaunchFilePath: the launcher's process
// identity, how many of its steps are done, and, once started, the process id
// of its server, the one process whose boot receipts are this launch's.
type launchFile struct {
	Protocol int    `json:"protocol"`
	PID      int    `json:"pid"`
	Start    string `json:"start"`
	Progress int64  `json:"progress"`
	Server   int    `json:"server,omitempty"`
}

// alive says the launcher that wrote the receipt still runs. It lives exactly
// as long as the server it started.
func (l launchFile) alive() bool {
	return l.Start != "" && processStart(l.PID) == l.Start
}

func readLaunchFile() (launchFile, bool) {
	var l launchFile
	raw, err := os.ReadFile(LaunchFilePath())
	if err == nil {
		err = json.Unmarshal(raw, &l)
	}
	return l, err == nil && l.Protocol == 1 && l.PID > 0
}

// Launch is the launcher's receipts of the launch it runs.
type Launch struct {
	pid      int
	start    string
	progress int64
	server   int
}

// BeginLaunch starts the receipts of a launch: it removes the previous boot's
// receipt, so every receipt in BootFilePath afterwards is this launch's
// server's, and writes the first launch receipt.
func BeginLaunch() *Launch {
	_ = os.Remove(BootFilePath())
	l := &Launch{pid: os.Getpid(), start: processStart(os.Getpid())}
	l.Advance()
	return l
}

// Advance records one more step of the launch. A receipt that cannot be
// written is none: the wait's window decides.
func (l *Launch) Advance() {
	l.progress++
	body, err := json.Marshal(launchFile{Protocol: 1, PID: l.pid, Start: l.start, Progress: l.progress, Server: l.server})
	if err != nil {
		return
	}
	next := LaunchFilePath() + ".next"
	if os.WriteFile(next, body, 0o600) == nil {
		_ = os.Rename(next, LaunchFilePath())
	}
}

// Started records the launch's server process as one more step.
func (l *Launch) Started(server int) {
	l.server = server
	l.Advance()
}

// PostUpdate sends one request to the loopback updater API, bounded to 5 s.
func PostUpdate(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return UpdateRequest(ctx, path, []byte("{}"), nil)
}

// LaunchClaim is a launcher's ownership of the launch it starts: a version
// last-good does not name, launched over a copy of the state from before it.
// The claim is a switch record naming the launcher as its owner, so the
// server boots behind the update fence and admits no work until the version
// is settled; the launcher settles it however long the boot takes (§2.1d
// "The settle rule").
type LaunchClaim struct {
	version string
	// before is the record the claim replaced, nil when there was none.
	before *UpdateState
	// first marks a first install's claim: no record, no copy, no fence.
	first bool
}

// ClaimLaunch runs after PrepareStateForLaunch, before the server starts. It
// claims the launch when a copy is tagged with the version about to launch
// and no other owner holds the record. A switch record already fences the
// server, and its owner (an update worker, init, recovery or the next boot
// repair) settles it. While a live worker stages or drains, the server cannot
// be fenced for this launch, so it starts outside the transaction and its
// copy is dropped (§2.1d "The state transaction"). A first install, with no
// last-good, has no state to restore and no version to return to: its claim
// writes no record and only advances last-good once the version is ready.
// claimed is false when there is nothing for the launcher to settle.
func ClaimLaunch() (claim LaunchClaim, claimed bool, err error) {
	launching, err := InstalledVersion()
	if err != nil {
		return LaunchClaim{}, false, fmt.Errorf("claim launch: %w", err)
	}
	s, err := ReadUpdateState()
	var before *UpdateState
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return LaunchClaim{}, false, fmt.Errorf("claim launch: %w", err)
	case s.Phase == updateSwitching:
		return LaunchClaim{}, false, nil
	case !s.settled() && workerAlive(s):
		return LaunchClaim{}, false, dropStateCopy(BaseDir(), launching)
	default:
		saved := s
		before = &saved
	}
	lastGood, err := LastGoodVersion()
	if errors.Is(err, os.ErrNotExist) {
		return LaunchClaim{version: launching, first: true}, canBeLastGood(launching), nil
	}
	if err != nil {
		return LaunchClaim{}, false, fmt.Errorf("claim launch: %w", err)
	}
	tag, err := stateCopyTag(BaseDir())
	if err != nil || tag != launching {
		return LaunchClaim{}, false, err
	}
	record := WorkerUpdateState(s)
	record.Phase, record.Candidate, record.Previous, record.RunningVersion, record.OldBoot = updateSwitching, launching, lastGood, lastGood, ""
	if err := WriteUpdateState(record); err != nil {
		return LaunchClaim{}, false, fmt.Errorf("claim launch: %w", err)
	}
	return LaunchClaim{version: launching, before: before}, true, nil
}

// Settle follows the claimed launch's server, child, from its start to the end
// of its boot, under the one boot rule. Ready: the version is committed as an
// update worker commits a candidate. Its process gone or no progress for the
// window: the server is ended and current returns to last-good with the
// version remembered as failed, as a boot repair returns an abandoned switch;
// the unit then starts again (Restart=always) and that start restores the
// state from before the version. A stop the launcher was asked for is no
// failure: the claim is given back, and the next launch claims and settles it
// again. A first install's claim only commits last-good once ready, and
// leaves a failed boot to the unit. exited delivers, once the child exited,
// whether a stop was asked.
// Settle returns once the version is settled or the child exited.
func (c *LaunchClaim) Settle(child *os.Process, exited <-chan bool) error {
	var ended, stopped bool
	observeExit := func(block bool) {
		if ended {
			return
		}
		if block {
			relayed, ok := <-exited
			ended, stopped = true, ok && relayed
			return
		}
		select {
		case relayed, ok := <-exited:
			ended, stopped = true, ok && relayed
		default:
		}
	}
	readyErr := waitReady(c.version, "", UpdateProgressWindow, func() (bool, error) {
		if observeExit(false); ended {
			return false, errors.New("the server's process exited")
		}
		return false, nil
	})
	if c.first {
		if readyErr != nil {
			return fmt.Errorf("first launch of %s: %w; last-good stays unset", c.version, readyErr)
		}
		return CommitLastGood(c.version)
	}
	s, err := ReadUpdateState()
	if err != nil {
		return fmt.Errorf("settle launch of %s: %w", c.version, err)
	}
	if s.Phase != updateSwitching || s.Candidate != c.version || s.WorkerPID != os.Getpid() {
		return nil
	}
	if readyErr == nil {
		return commitSwitch(s, c.version, func() error { return PostUpdate("commit") })
	}
	if !ended {
		_ = child.Kill()
		observeExit(true)
	}
	if stopped {
		return c.giveBack()
	}
	if err := returnToLastGood(s); err != nil {
		return errors.Join(fmt.Errorf("launch of %s: %w", c.version, readyErr), err)
	}
	return fmt.Errorf("launch of %s: %w; %s launches next", c.version, readyErr, s.Previous)
}

// giveBack restores the record the claim replaced.
func (c *LaunchClaim) giveBack() error {
	if c.before != nil {
		return WriteUpdateState(*c.before)
	}
	if err := os.Remove(UpdateStatePath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("give back launch claim: %w", err)
	}
	return syncPrefix()
}
