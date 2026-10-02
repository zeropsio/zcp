package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// The stand-up reports itself twice: to the model, as the progress stream
// and the answer, and to the mate server, as the stand-up section of the
// setup status file (mate.Status) — per service the step it is on (build,
// deploy, verify), its state, the platform process behind it and what
// failed. The server relays that section to the run card, so whoever watches
// the Mate sees the stand-up's processes, not only the model's words about
// them. The runtimes section is the boot import's: the stand-up reads it to
// wait for an import still in flight (awaitBootImport), never writes it.

// standupStatus writes the stand-up's section. A nil one, or one with no
// path, writes nothing: a container without the file is an older shape, and
// the stand-up does not need it to run. One MCP server holds one, across its
// calls: the stand-up is one from its first call until its stages return.
type standupStatus struct {
	path string
	mu   sync.Mutex
	now  func() time.Time
	// beatEvery is how often a running stand-up rewrites the file (0 is
	// mate.StandupBeat); carryWait how long one whose stages wait for the
	// second call stays running without it (0 is standupStageWait).
	beatEvery, carryWait time.Duration
	// carryMu guards carry, the stop of the beat that keeps a stand-up
	// running between its calls.
	carryMu sync.Mutex
	carry   func()
}

// standupStageWait bounds how long a stand-up whose stages are queued stays
// running without the call that builds them. The model is told to make that
// call in the same turn, once it has started the dev servers: minutes. One
// that never comes ends the stand-up as the development it stood up.
const standupStageWait = 15 * time.Minute

func newStandupStatus(path string) *standupStatus {
	if path == "" {
		return nil
	}
	return &standupStatus{path: path, now: time.Now}
}

func (s *standupStatus) update(change func(*mate.StandupStatus)) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := mate.UpdateStandup(s.path, change); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: stand-up status: %v\n", err)
	}
}

func (s *standupStatus) stamp() string { return s.now().UTC().Format(time.RFC3339) }

// begin starts a call's section: afresh, a call reporting the halves it
// touches — unless the stand-up waits for this call to build its stages
// (carried), when the call goes on with it: the same start, the same halves,
// running throughout.
func (s *standupStatus) begin() {
	if s == nil {
		return
	}
	s.stopCarry()
	now := s.now()
	at := now.UTC().Format(time.RFC3339)
	s.update(func(st *mate.StandupStatus) {
		if carried(*st, now) {
			st.State, st.Error = mate.StandupRunning, ""
			return
		}
		*st = mate.StandupStatus{State: mate.StandupRunning, Phase: mate.PhaseDevelopment, StartedAt: at}
	})
}

// carried is a section a first call left running for the stages, still
// alive: its beat stops only when the call that builds them begins.
func carried(st mate.StandupStatus, now time.Time) bool {
	if st.State != mate.StandupRunning || st.Phase != mate.PhaseStage || st.EndedAt != "" {
		return false
	}
	updated, err := time.Parse(time.RFC3339, st.UpdatedAt)
	return err == nil && now.Sub(updated) <= mate.StandupStale
}

// beat rewrites the file every interval until the returned stop, so the
// file says the stand-up is alive (mate.StandupStale); stop waits for it.
// An interval of 0 is the status's own (beatEvery, else mate.StandupBeat).
func (s *standupStatus) beat(interval time.Duration) func() {
	if s == nil {
		return func() {}
	}
	if interval <= 0 {
		interval = s.beatEvery
	}
	if interval <= 0 {
		interval = mate.StandupBeat
	}
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-quit:
				return
			case <-time.After(interval):
				s.update(func(*mate.StandupStatus) {})
			}
		}
	}()
	return func() {
		close(quit)
		<-done
	}
}

func (s *standupStatus) phase(phase string) {
	s.update(func(st *mate.StandupStatus) { st.Phase = phase })
}

// step sets one half's step and state. A process id is kept until another
// replaces it, so a half past its build still names the process that built
// it; an error line is the step's own and goes with it.
func (s *standupStatus) step(host, step, state, processID, errLine string) {
	if s == nil {
		return
	}
	at := s.stamp()
	s.update(func(st *mate.StandupStatus) {
		i := -1
		for k := range st.Services {
			if st.Services[k].Hostname == host {
				i = k
				break
			}
		}
		if i < 0 {
			st.Services = append(st.Services, mate.StandupService{Hostname: host})
			i = len(st.Services) - 1
		}
		e := &st.Services[i]
		if e.Step == step && e.State == state && (processID == "" || e.ProcessID == processID) && e.Error == oneStatusLine(errLine) {
			return
		}
		e.Step, e.State, e.At, e.Error = step, state, at, oneStatusLine(errLine)
		if processID != "" {
			e.ProcessID = processID
		}
	})
}

// awaitStages closes a call that stood development up with the stages
// queued for the next: the stand-up is not over, so its section stays
// running, in the stage phase, and alive — beaten — until the call that
// builds the stages begins (begin stops the beat) or carryWait passes, when
// it ends as the development it stood up.
func (s *standupStatus) awaitStages() {
	if s == nil {
		return
	}
	s.update(func(st *mate.StandupStatus) {
		st.State, st.Phase, st.EndedAt, st.Error = mate.StandupRunning, mate.PhaseStage, "", ""
	})
	wait := s.carryWait
	if wait <= 0 {
		wait = standupStageWait
	}
	stopBeat := s.beat(0)
	quit, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-quit:
			stopBeat()
		case <-time.After(wait):
			stopBeat()
			at := s.stamp()
			s.update(func(st *mate.StandupStatus) {
				if st.State == mate.StandupRunning && st.Phase == mate.PhaseStage {
					st.State, st.Phase, st.EndedAt = mate.StandupDone, mate.PhaseDevelopment, at
				}
			})
		}
	}()
	s.carryMu.Lock()
	s.carry = func() {
		close(quit)
		<-done
	}
	s.carryMu.Unlock()
}

// stopCarry stops the beat that keeps a stand-up running between its calls,
// and waits for it; nothing when none runs.
func (s *standupStatus) stopCarry() {
	if s == nil {
		return
	}
	s.carryMu.Lock()
	stop := s.carry
	s.carry = nil
	s.carryMu.Unlock()
	if stop != nil {
		stop()
	}
}

// end closes the call: failed with errLine when the call refused, else done
// unless a half failed or was left still building.
func (s *standupStatus) end(errLine string) {
	if s == nil {
		return
	}
	at := s.stamp()
	s.update(func(st *mate.StandupStatus) {
		st.EndedAt = at
		st.State, st.Error = mate.StandupDone, ""
		if errLine != "" {
			st.State, st.Error = mate.StandupFailed, oneStatusLine(errLine)
			return
		}
		var failed []string
		for _, e := range st.Services {
			switch e.State {
			case mate.StepFailed:
				failed = append(failed, e.Hostname+": "+e.Error)
			case mate.StepRunning:
				failed = append(failed, e.Hostname+": still building past the stand-up's wait; a second call waits for it")
			}
		}
		if len(failed) > 0 {
			st.State, st.Error = mate.StandupFailed, oneStatusLine(strings.Join(failed, "; "))
		}
	})
}

// statusLineMax bounds an error line in the status file.
const statusLineMax = 300

func oneStatusLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > statusLineMax {
		s = s[:statusLineMax-1] + "…"
	}
	return s
}

// standupStepForPhase is the step a failed deploy stopped at: a build or its
// runtime preparation is the build; the new container's init is the deploy.
func standupStepForPhase(failedPhase string) string {
	if failedPhase == "init" {
		return mate.StepDeploy
	}
	return mate.StepBuild
}

// trackDeploys reports, while a batch deploys, which step each target is on
// and the process behind it, from the project's live processes (the lag-free
// read ops.ProjectActivity owns): a build process building is the build, the
// same process deploying is the deploy, and a target whose process has ended
// while its deploy has not returned is being verified (its container
// answering SSH, its public access, the dev server zcp keeps). The returned
// stop ends the tracking and waits for it, so it never writes after the
// batch's own results.
func (d standupDeps) trackDeploys(ctx context.Context, hosts []string, live map[string]*platform.ServiceStack, status *standupStatus) func() {
	if status == nil || len(hosts) == 0 {
		return func() {}
	}
	idToHost := map[string]string{}
	for _, h := range hosts {
		if svc := live[h]; svc != nil {
			idToHost[svc.ID] = h
		}
	}
	poll := d.trackPoll
	if poll <= 0 {
		poll = standupTrackPoll
	}
	tctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		seen := map[string]bool{}
		for {
			if activity, err := ops.ProjectActivity(tctx, d.batch.client, d.batch.projectID, idToHost); err == nil {
				for _, h := range hosts {
					if op, ok := standupBuildOp(activity[h]); ok {
						seen[h] = true
						status.step(h, op.step, mate.StepRunning, op.processID, "")
					} else if seen[h] {
						status.step(h, mate.StepVerify, mate.StepRunning, "", "")
					}
				}
			}
			select {
			case <-tctx.Done():
				return
			case <-time.After(poll):
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// standupTrackPoll is the time between two looks at a batch's processes.
const standupTrackPoll = 3 * time.Second

type standupOp struct {
	step      string
	processID string
}

// standupBuildOp picks the build or deploy among a target's live operations.
func standupBuildOp(liveOps []ops.LiveOp) (standupOp, bool) {
	for _, op := range liveOps {
		switch op.Action {
		case platformBuildToken:
			return standupOp{mate.StepBuild, op.ProcessID}, true
		case "deploy":
			return standupOp{mate.StepDeploy, op.ProcessID}, true
		}
	}
	return standupOp{}, false
}

// importWaitsClosedOff reports the container's import waiting for the
// project to be closed off (mate.RuntimesWaitingClosedOff): it waits with no
// end of its own, so the stand-up answers the Finish-setup line at once
// instead of waiting for an import that cannot start.
func (d standupDeps) importWaitsClosedOff() bool {
	if d.statusPath == "" {
		return false
	}
	st, err := mate.ReadStatus(d.statusPath)
	return err == nil && st.Runtimes.State == mate.RuntimesPending && st.Runtimes.Error == mate.RuntimesWaitingClosedOff
}

// awaitBootImport waits while the boot import of this Mate's runtimes is in
// flight (pending or importing in the status file), so the stand-up never
// finds a half missing that is being imported, and never tells the model to
// import what the container is importing — but not while it only waits for
// the project to be closed off, which has no end of its own. The import
// always ends its section (done or failed, its own timeout included) and
// every launch resets one a restart cut short, so the wait ends with it;
// bootWait bounds it all the same. Returns what the import says failed, by hostname ("" for the import
// as a whole).
func (d standupDeps) awaitBootImport(ctx context.Context, progress *standupProgress) map[string]string {
	if d.statusPath == "" {
		return nil
	}
	deadline := time.Now().Add(d.bootWait)
	for {
		st, err := mate.ReadStatus(d.statusPath)
		if err != nil {
			return nil
		}
		r := st.Runtimes
		// A pending section that says why it is pending (it waits for the
		// project to be closed off, or cannot read it) is not about to import.
		startingAfterTag := d.closedOffSeen && r.Error == mate.RuntimesWaitingClosedOff
		// Waiting for zcp's own deploy ends on its own, soon: in flight.
		ownDeploy := r.Error == mate.RuntimesWaitingOwnDeploy
		inFlight := (r.State == mate.RuntimesPending && (r.Error == "" || startingAfterTag || ownDeploy)) || r.State == mate.RuntimesImporting
		if !inFlight || !time.Now().Before(deadline) {
			failed := map[string]string{}
			for _, s := range r.Services {
				if s.State == mate.ServiceFailed {
					failed[s.Hostname] = s.Error
				}
			}
			if len(failed) == 0 && r.State == mate.RuntimesFailed && r.Error != "" {
				failed[""] = r.Error
			}
			return failed
		}
		var waiting []string
		for _, s := range r.Services {
			if s.State != mate.ServiceRunning && s.State != mate.ServiceFailed {
				waiting = append(waiting, s.Hostname)
			}
		}
		what := "this Mate's runtimes"
		if len(waiting) > 0 {
			what = strings.Join(waiting, ", ")
		}
		progress.say("waiting for the container's import of " + what)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(d.bootPoll):
		}
	}
}
