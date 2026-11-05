package tools

import (
	"context"
	"fmt"
	"maps"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/zeropsio/zcp/internal/mate"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// The stand-up reports itself twice: to the model, as the progress stream
// and the answer, and to the mate server, as the stand-up section of the
// setup status file (mate.Status) — per service the step it is on (build,
// deploy, verify), its state, the platform process behind it and what
// failed. The server relays that section to the run card, so whoever watches
// the Mate sees the stand-up's processes, not only the model's words about
// them.

// standupStatus writes the stand-up's section. A nil one, or one with no
// path, writes nothing: a container without the file is an older shape, and
// the stand-up does not need it to run. One MCP server holds one, across its
// calls: the stand-up is one from its first call until its stages return.
type standupStatus struct {
	path string
	mu   sync.Mutex
	now  func() time.Time
	// write supplies a status-write receipt to tests; nil uses the real writer.
	write func(string, func(*mate.StandupStatus)) error
}

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
	write := s.write
	if write == nil {
		write = mate.UpdateStandup
	}
	if err := write(s.path, change); err != nil {
		fmt.Fprintf(os.Stderr, "zcp: stand-up status: %v\n", err)
	}
}

func (s *standupStatus) stamp() string { return s.now().UTC().Format(time.RFC3339) }

// begin starts a call's section, naming this process as the one running
// it and stamping the call's start: afresh, a call reporting the halves it touches — unless the stand-up
// waits for this call to build its stages (carried), when the call goes on
// with it: the same start, the same halves, running throughout.
func (s *standupStatus) begin() {
	if s == nil {
		return
	}
	self := mate.StandupProcess{PID: os.Getpid(), Start: workflow.CurrentProcessStartTime()}
	at := s.stamp()
	s.update(func(st *mate.StandupStatus) {
		if carried(*st, self) {
			st.State, st.Error, st.CallStartedAt = mate.StandupRunning, "", at
			return
		}
		*st = mate.StandupStatus{State: mate.StandupRunning, Phase: mate.PhaseDevelopment, StartedAt: at, Process: &self, CallStartedAt: at}
	})
}

// carried is a section a first call of this MCP server left running for the
// stages: one MCP server holds one stand-up across its calls, and a section
// another process left is not this stand-up.
func carried(st mate.StandupStatus, self mate.StandupProcess) bool {
	return st.State == mate.StandupRunning && st.Phase == mate.PhaseStage && st.EndedAt == "" &&
		st.Process != nil && *st.Process == self
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
// running, in the stage phase, until the call that builds the stages begins.
// zcp cannot see the agent's turn end; the mate server can, and a turn that
// ends without that call is the server's to read as the stages not built.
func (s *standupStatus) awaitStages() {
	s.update(func(st *mate.StandupStatus) {
		st.State, st.Phase, st.EndedAt, st.Error = mate.StandupRunning, mate.PhaseStage, "", ""
	})
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

// standupTracking reports, while the call's batches deploy, which step each
// target is on and the process behind it, from the project's live processes
// (the lag-free read ops.ProjectActivity owns): a build process building is
// the build, the same process deploying is the deploy, and a target whose
// process has ended while its deploy has not returned is being verified (its
// container answering SSH, its public access, its supervisor's dev servers).
// One per call, the session's one reader past the wait for the runtimes, for
// every batch — concurrent ones among them: it reads the processes when a
// batch starts and again at each of the project's pushes (ZEROPS-078), never
// on a timer.
type standupTracking struct {
	batch   batchDeployer
	session *standupPushes
	status  *standupStatus
	once    sync.Once
	// started is a batch that started: read at once (coalesced).
	started chan struct{}
	// mu guards targets and seen, and is held while a look writes, so a
	// batch's stop is never followed by a write for its targets.
	mu      sync.Mutex
	targets map[string]string // service id → hostname, of the batches deploying
	seen    map[string]bool
}

func newStandupTracking(batch batchDeployer, session *standupPushes, status *standupStatus) *standupTracking {
	return &standupTracking{
		batch: batch, session: session, status: status,
		started: make(chan struct{}, 1), targets: map[string]string{}, seen: map[string]bool{},
	}
}

// track follows a batch's targets until the returned stop, after which
// nothing is written for them, so never after the batch's own results. ctx is
// the call's: the first batch's starts the reader, which ends with it.
func (t *standupTracking) track(ctx context.Context, hosts []string, live map[string]*platform.ServiceStack) func() {
	if t.status == nil || len(hosts) == 0 {
		return func() {}
	}
	var ids []string
	t.mu.Lock()
	for _, h := range hosts {
		if svc := live[h]; svc != nil {
			t.targets[svc.ID] = h
			ids = append(ids, svc.ID)
		}
	}
	t.mu.Unlock()
	t.once.Do(func() { go t.follow(ctx, t.session.wake(ctx)) })
	select {
	case t.started <- struct{}{}:
	default:
	}
	return func() {
		t.mu.Lock()
		defer t.mu.Unlock()
		for _, id := range ids {
			delete(t.seen, t.targets[id])
			delete(t.targets, id)
		}
	}
}

// follow looks at each batch's start and at each push until the call ends.
func (t *standupTracking) follow(ctx context.Context, wakes <-chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.started:
		case <-wakes:
		}
		t.look(ctx)
	}
}

func (t *standupTracking) look(ctx context.Context) {
	t.mu.Lock()
	idToHost := maps.Clone(t.targets)
	t.mu.Unlock()
	if len(idToHost) == 0 {
		return
	}
	activity, err := ops.ProjectActivity(ctx, t.batch.client, t.batch.projectID, idToHost)
	if err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for id, h := range idToHost {
		if t.targets[id] != h {
			continue // its batch stopped during the read
		}
		if op, ok := standupBuildOp(activity[h]); ok {
			t.seen[h] = true
			t.status.step(h, op.step, mate.StepRunning, op.processID, "")
		} else if t.seen[h] {
			t.status.step(h, mate.StepVerify, mate.StepRunning, "", "")
		}
	}
}

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
