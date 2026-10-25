//go:build linux || darwin

package devsup

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Config is how one supervisor runs.
type Config struct {
	// Root is the repo root: the declaration is Root/.zcp/dev-servers.json
	// and a server's cwd is relative to it.
	Root string
	// StateDir holds state.json, the control socket and the logs; it is
	// $HOME/.zcp/devsup, so it survives a container restart and a redeploy
	// starts from nothing.
	StateDir string
	// Service is this dev service's hostname: an entry naming another
	// service is not run here (variant B).
	Service string
	// Env is the environment every server starts with (the service's).
	Env []string
	// Grace is how long a stopped group gets between SIGTERM and SIGKILL.
	Grace time.Duration
	// PortWait bounds the wait for a server's port to be free before it starts.
	PortWait time.Duration
	// StartWindow is how long after a start the server is still starting: a
	// crash within it, or before a server with a port first listens, is
	// started once more, and a watched change within it may be the server's
	// own (watchLoopLimit).
	StartWindow time.Duration
	// Stagger is the gap between servers started together, so they do not
	// all fork their toolchains at once.
	Stagger time.Duration
	// LimitBackoff is the wait before the first start after a server met the
	// service's task limit; it doubles with each such start, up to
	// maxLimitBackoff.
	LimitBackoff time.Duration
	// Tasks reads the task budget the servers share; nil is the supervisor's
	// own cgroup.
	Tasks func() (Tasks, bool)
	// PreviousRun reads systemd's result for the previous run of the
	// supervisor's unit; nil reads the journal.
	PreviousRun func() (string, bool)
	// Version is the release reported in the state.
	Version string
	// Log receives the supervisor's own lines (the service's runtime log).
	Log io.Writer
}

// Defaults for Config (docs/spec-mate.md §2.8a.2).
const (
	DefaultGrace        = 10 * time.Second
	DefaultPortWait     = 10 * time.Second
	DefaultStartWindow  = 10 * time.Second
	DefaultStagger      = time.Second
	DefaultLimitBackoff = 2 * time.Second
)

// maxLimitStarts bounds the starts after a server met the task limit, in a
// row; maxLimitBackoff caps the wait before one.
const (
	maxLimitStarts  = 6
	maxLimitBackoff = time.Minute
)

// watchLoopLimit is how many starts in a row may each be restarted by a
// watched change made within its start window; past it, such a change is the
// server's own output and restarts nothing.
const watchLoopLimit = 10

// Restart reasons that name no path.
const (
	reasonRetry   = "it crashed within its start window"
	reasonAsked   = "a restart was asked for"
	reasonStart   = "a start was asked for"
	reasonDecl    = "its declaration changed"
	reasonBoot    = "the supervisor started again"
	reasonRunSpec = "its run entry changed"
	// reasonEnv: `ctl env` changed the overlay the server's group started
	// under (docs/spec-mate.md §2.8a.2).
	reasonEnv = "env changed"
	// reasonLimit marks a start after the task limit was met; the recorded
	// reason is limitReason.
	reasonLimit = "task limit"
)

// logRotateSize is the size above which a server's log moves to .log.1 when
// the server starts.
const logRotateSize = 10 << 20

// listenWait bounds how long the supervisor looks for a started server's port
// to accept, to record starting → running.
const listenWait = 10 * time.Minute

type server struct {
	name     string
	spec     Server
	source   Source
	desired  Desired
	g        *group
	starting bool
	// startedAt, starts and exit carry over from state.json.
	startedAt *time.Time
	starts    int
	exit      *Exit
	// left is the group a previous instance recorded, reaped on boot.
	leftPgid        int
	leftLeaderStart int64
	watch           *watcher
	// retry: this start is the one more start after a start crash;
	// retryPending: that start is scheduled and has not run yet.
	retry, retryPending bool
	// listened: this start's port has accepted once.
	listened bool
	// fwd is the preview forward of the live group, nil when it has none.
	fwd *forward
	// preview is what its preview forwards saw, kept across them; nil until
	// the first opens.
	preview *previewSeen
	// tasks is the task budget read when this start began; tasksOK says it
	// was read.
	tasks   Tasks
	tasksOK bool
	// limitStarts counts the starts in a row made because the task limit was
	// met; taskLimit is the budget read when it last was, nil when the last
	// exit did not meet it.
	limitStarts int
	taskLimit   *Tasks
	// restartReason says why it last restarted, or why a watched change
	// did not restart it; outOfMemory: that restart is the one after systemd
	// restarted the service because it ran out of memory.
	restartReason string
	outOfMemory   bool
	// windowChanged: a watched change came within this start's window;
	// selfChanged counts the starts in a row that saw one; selfPaths are the
	// paths those changes held — the server's own output — nil once a burst
	// named too many to keep.
	windowChanged bool
	selfChanged   int
	selfPaths     map[string]bool
	selfOverflow  bool
	// startBegun is when the latest start began, before its process: a
	// watched change made before it is on disk when that process starts.
	startBegun time.Time
	// env is the overlay the live group started under.
	env map[string]string
}

// Supervisor holds the dev servers of one repo.
type Supervisor struct {
	cfg       Config
	startedAt time.Time
	// op serialises everything that starts or stops servers.
	op sync.Mutex
	// mu guards servers and decl; never held across a start or a stop.
	mu      sync.Mutex
	servers map[string]*server
	decl    DeclarationInfo
	// persistMu orders state.json writes so the newest snapshot lands last.
	persistMu sync.Mutex
	// closed: Shutdown ran; a pending retry starts nothing.
	closed bool
	// outOfMemory: systemd restarted the service because it ran out of
	// memory. Set once, before Boot starts anything.
	outOfMemory bool
	// slot is the container's ZEROPS_StableNumber, 0 when it has none; with
	// the declaration's previewPortBase it names the preview port.
	slot int
	// previewBase and previewName are the declaration's previewPortBase and
	// the name of its preview server, as last read; guarded by mu.
	previewBase int
	previewName string
	// fwdMu serialises syncPreview's decisions, shuts and listens; it is
	// never held while taking op, nor while an ended forward is waited for.
	fwdMu sync.Mutex
	// env is the overlay every start adds to cfg.Env (SetEnv); it is
	// replaced, never changed in place, so a start may keep the map it read.
	// envRestarting: restartForEnv runs. Both guarded by mu.
	env           map[string]string
	envRestarting bool
}

// New prepares a supervisor and loads the state an earlier instance left in
// cfg.StateDir.
func New(cfg Config) (*Supervisor, error) {
	if cfg.Root == "" || cfg.StateDir == "" {
		return nil, errors.New("devsup: root and state dir are required")
	}
	if cfg.Grace <= 0 {
		cfg.Grace = DefaultGrace
	}
	if cfg.PortWait <= 0 {
		cfg.PortWait = DefaultPortWait
	}
	if cfg.StartWindow <= 0 {
		cfg.StartWindow = DefaultStartWindow
	}
	if cfg.Stagger <= 0 {
		cfg.Stagger = DefaultStagger
	}
	if cfg.LimitBackoff <= 0 {
		cfg.LimitBackoff = DefaultLimitBackoff
	}
	if cfg.Tasks == nil {
		cfg.Tasks = readTasks
	}
	if cfg.PreviousRun == nil {
		cfg.PreviousRun = readPreviousRun
	}
	if cfg.Log == nil {
		cfg.Log = io.Discard
	}
	if err := os.MkdirAll(filepath.Join(cfg.StateDir, logsDir), 0o700); err != nil {
		return nil, fmt.Errorf("devsup state dir: %w", err)
	}
	s := &Supervisor{cfg: cfg, startedAt: time.Now().UTC(), servers: map[string]*server{}, slot: stableNumber(cfg.Env)}
	if prev, err := ReadState(cfg.StateDir); err == nil {
		if checkOverlay(prev.Env) == nil {
			s.env = prev.Env
		}
		for name, p := range prev.Servers {
			sv := &server{
				name: name, source: p.Source, desired: p.Desired, exit: p.Exit, startedAt: p.StartedAt,
				leftPgid: p.Pgid, leftLeaderStart: p.LeaderStart, restartReason: p.LastRestartReason,
			}
			if p.StartedAt != nil {
				sv.starts = p.Restarts + 1
			}
			if p.Source == SourceRuntime {
				sv.spec = Server{Name: name, Command: p.Command, Cwd: p.Cwd, Port: p.Port}
			}
			s.servers[name] = sv
		}
	}
	return s, nil
}

func (s *Supervisor) logf(format string, a ...any) {
	fmt.Fprintf(s.cfg.Log, "%s zcp-devsup: %s\n", time.Now().UTC().Format(time.RFC3339), fmt.Sprintf(format, a...))
}

// Run starts every server the declaration and the kept state ask for, serves
// the control socket, reloads on SIGHUP and stops every server when ctx ends.
// ctx only ends the loop: a boot, reload, control call or shutdown under way
// runs to completion, so no server is left half started or half stopped.
//
//nolint:contextcheck // see above
func (s *Supervisor) Run(ctx context.Context) error {
	ln, err := listenControl(s.cfg.StateDir)
	if err != nil {
		return err
	}
	defer ln.Close()
	s.logf("up pid=%d version=%s root=%s state=%s service=%s", os.Getpid(), s.cfg.Version, s.cfg.Root, s.cfg.StateDir, s.cfg.Service)
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	defer signal.Stop(hup)
	s.Boot()
	go s.serveControl(ln)
	for {
		select {
		case <-hup:
			s.logf("SIGHUP: reload")
			s.Reload()
		case <-ctx.Done():
			s.Shutdown()
			return nil
		}
	}
}

// Boot is the first reconcile of this instance: it reaps the groups an earlier
// instance left running, then starts every server whose desired state is
// running, crashed ones included. When the previous run of the supervisor's
// unit ended because the kernel's OOM killer killed one of its processes,
// systemd stopped every server with it (OOMPolicy=stop on the platform's unit)
// and their restart reason says so.
func (s *Supervisor) Boot() {
	s.op.Lock()
	defer s.op.Unlock()
	if result, ok := s.cfg.PreviousRun(); ok {
		s.logf("the previous run of the unit ended: %s", result)
		s.outOfMemory = result == unitResultOOMKill
	}
	s.reapLeft()
	s.reconcile(true)
}

// Reload re-reads the declaration: an added server starts, a removed one stops
// and is dropped, a changed one restarts; the rest is untouched.
func (s *Supervisor) Reload() {
	s.op.Lock()
	defer s.op.Unlock()
	s.reconcile(false)
}

// reapLeft stops the groups state.json recorded, when they are still the ones
// that instance started.
func (s *Supervisor) reapLeft() {
	s.mu.Lock()
	type left struct {
		name        string
		pgid        int
		leaderStart int64
	}
	var all []left
	for name, sv := range s.servers {
		if sv.leftPgid > 1 {
			all = append(all, left{name, sv.leftPgid, sv.leftLeaderStart})
		}
		sv.leftPgid, sv.leftLeaderStart = 0, 0
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, l := range all {
		if !ownsGroup(l.pgid, l.leaderStart) {
			continue
		}
		wg.Go(func() {
			s.logf("reap %s: group %d an earlier supervisor left running", l.name, l.pgid)
			if err := stopGroup(l.pgid, s.cfg.Grace); err != nil {
				s.logf("reap %s: %v", l.name, err)
			}
		})
	}
	wg.Wait()
}

// reconcile brings the servers to the declaration and the kept state, and
// answers what it did to each.
func (s *Supervisor) reconcile(boot bool) map[string]action {
	decl, err := LoadDeclaration(s.cfg.Root)

	s.mu.Lock()
	want := map[string]wanted{}
	for name, sv := range s.servers {
		// Runtime entries, and every known server while the declaration
		// cannot be read: nothing changes until it reads again.
		if sv.spec.Command != "" && (sv.source == SourceRuntime || err != nil) {
			want[name] = wanted{server: sv.spec, desired: sv.desired}
		}
	}
	declared := map[string]bool{}
	if err != nil {
		s.decl.Error = err.Error()
		s.logf("declaration: %v", err)
	} else {
		s.decl = DeclarationInfo{Version: decl.Version}
		mine := decl.ForService(s.cfg.Service)
		s.previewBase, s.previewName = decl.PreviewPortBase, ""
		if p, ok := PreviewServer(mine); ok {
			s.previewName = p.Name
		}
		for _, spec := range mine {
			declared[spec.Name] = true
			desired := spec.FirstDesired()
			if sv, ok := s.servers[spec.Name]; ok && sv.desired != "" {
				desired = sv.desired
			}
			want[spec.Name] = wanted{server: spec, desired: desired}
		}
	}
	have := map[string]held{}
	for name, sv := range s.servers {
		if sv.spec.Command != "" {
			have[name] = held{server: sv.spec, live: sv.g != nil, exited: sv.exit != nil}
		}
	}
	acts := plan(want, have, boot)
	for name, w := range want {
		sv, ok := s.servers[name]
		if !ok {
			sv = &server{name: name}
			s.servers[name] = sv
		}
		if declared[name] {
			sv.source = SourceDeclared
		} else if sv.source == "" {
			sv.source = SourceRuntime
		}
		sv.desired = w.desired
		if acts[name] != actRestart {
			sv.spec = w.server
		}
	}
	for name, sv := range s.servers {
		// A kept entry the declaration no longer holds and nothing runs.
		if _, ok := want[name]; !ok && sv.g == nil && acts[name] != actRemove {
			if err == nil {
				delete(s.servers, name)
			}
		}
	}
	s.mu.Unlock()

	var stop, start []string
	for name, a := range acts {
		switch a {
		case actStop, actRemove:
			stop = append(stop, name)
		case actRestart:
			stop = append(stop, name)
			start = append(start, name)
		case actStart:
			start = append(start, name)
		case actNone:
		}
	}
	s.stopAll(stop)
	s.mu.Lock()
	for name, a := range acts {
		switch a {
		case actRemove:
			if sv := s.servers[name]; sv != nil && sv.watch != nil {
				sv.watch.close()
			}
			delete(s.servers, name)
		case actRestart:
			s.servers[name].spec = want[name].server
		case actNone, actStart, actStop:
		}
	}
	s.mu.Unlock()
	reason := reasonDecl
	switch {
	case boot && s.outOfMemory:
		reason = reasonOutOfMemoryRestart
	case boot:
		reason = reasonBoot
	}
	s.startAll(start, reason)
	s.syncWatches()
	s.syncPreview()
	s.persist()
	return acts
}

// stopAll ends the named servers' groups in parallel and waits for them.
func (s *Supervisor) stopAll(names []string) {
	var wg sync.WaitGroup
	for _, name := range names {
		s.mu.Lock()
		var g *group
		var fwd *forward
		if sv := s.servers[name]; sv != nil {
			g, sv.g = sv.g, nil
			fwd, sv.fwd = sv.fwd, nil
		}
		s.mu.Unlock()
		if fwd != nil {
			fwd.close()
		}
		if g == nil {
			continue
		}
		wg.Go(func() {
			s.logf("stop %s pgid=%d", name, g.pgid)
			if err := stopGroup(g.pgid, s.cfg.Grace); err != nil {
				s.logf("stop %s: %v", name, err)
			}
			<-g.done
		})
	}
	wg.Wait()
}

// startAll starts the named servers in parallel, a Stagger apart: together
// they share the service's task limit. Each is starting from now on.
func (s *Supervisor) startAll(names []string, reason string) {
	slices.Sort(names)
	s.mu.Lock()
	for _, name := range names {
		if sv := s.servers[name]; sv != nil && sv.g == nil {
			sv.starting = true
		}
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() {
			time.Sleep(time.Duration(i) * s.cfg.Stagger)
			s.start(name, reason)
		})
	}
	wg.Wait()
}

// start waits for the server's port and starts it; reason is why, recorded
// when it is a restart. reasonRetry marks the one more start after a crash
// in the window; reasonLimit a start after the task limit was met, which is
// not that one more start. A failure leaves it crashed with exit
// {code: -1, signal: port_busy|start_failed} and the reason in its log; a
// start the kernel refused at the task limit is started again with backoff.
func (s *Supervisor) start(name, reason string) {
	s.mu.Lock()
	sv := s.servers[name]
	if sv == nil || sv.g != nil {
		if sv != nil {
			sv.starting = false
		}
		s.mu.Unlock()
		return
	}
	spec, env := sv.spec, s.env
	sv.starting, sv.startBegun = true, time.Now()
	sv.retryPending, sv.windowChanged, sv.listened = false, false, false
	if reason == reasonLimit {
		sv.limitStarts++
	} else {
		sv.retry, sv.limitStarts = reason == reasonRetry, 0
	}
	sv.outOfMemory = false
	if sv.starts > 0 {
		sv.restartReason = reason
		sv.outOfMemory = reason == reasonOutOfMemoryRestart
		if reason == reasonLimit {
			sv.restartReason = limitReason(sv.taskLimit)
		}
	}
	sv.starts++
	starts := sv.starts
	s.mu.Unlock()

	before, beforeOK := s.cfg.Tasks()
	g, failure, err := s.launch(name, spec, env, starts)
	now := time.Now().UTC()

	s.mu.Lock()
	sv.starting = false
	sv.startedAt = &now
	sv.tasks, sv.tasksOK = before, beforeOK
	if err != nil {
		s.logf("start %s: %v", name, err)
		sv.exit = &Exit{Code: -1, Signal: failure, At: now}
		after, ok := s.cfg.Tasks()
		sv.taskLimit = nil
		if metTaskLimit(err, before, after, ok && beforeOK) {
			s.limitStart(sv, after, starts)
		}
		s.mu.Unlock()
		return
	}
	s.logf("started %s pgid=%d start#%d", name, g.pgid, starts)
	sv.g, sv.env = g, env
	s.mu.Unlock()
	go s.watchExit(sv, g)
	if spec.Port > 0 {
		go s.awaitListen(sv, g, spec.Port)
	}
}

// launch opens the server's log and starts its group once the port is free,
// with the service's environment plus the overlay env.
func (s *Supervisor) launch(name string, spec Server, env map[string]string, starts int) (*group, string, error) {
	lp := LogPath(s.cfg.StateDir, name)
	if fi, err := os.Stat(lp); err == nil && fi.Size() > logRotateSize {
		_ = os.Rename(lp, lp+".1")
	}
	f, err := os.OpenFile(lp, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, ExitStartFailed, fmt.Errorf("open log: %w", err)
	}
	defer f.Close()
	signal, err := s.preflight(spec)
	if err == nil {
		fmt.Fprintf(f, "--- zcp-devsup: start #%d at %s: %s\n", starts, time.Now().UTC().Format(time.RFC3339), spec.Command)
		var g *group
		if g, err = startGroup(spec.Command, filepath.Join(s.cfg.Root, filepath.FromSlash(spec.Cwd)), withOverlay(s.cfg.Env, env), f); err == nil {
			return g, "", nil
		}
		signal = ExitStartFailed
	}
	fmt.Fprintf(f, "--- zcp-devsup: start #%d failed: %v\n", starts, err)
	return nil, signal, err
}

// preflight checks the cwd and waits for the port; a failure names its exit
// signal.
func (s *Supervisor) preflight(spec Server) (string, error) {
	dir := filepath.Join(s.cfg.Root, filepath.FromSlash(spec.Cwd))
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return ExitStartFailed, fmt.Errorf("cwd %s is not a directory", spec.Cwd)
	}
	if !waitPortFree(spec.Port, s.cfg.PortWait) {
		return ExitPortBusy, fmt.Errorf("port %d is still busy after %s", spec.Port, s.cfg.PortWait)
	}
	return "", nil
}

// watchExit records a server whose leader exited while the supervisor held it
// as crashed, and ends what is left of its group. A start crash, unless that
// start was already the retry, is started once more when the window has
// passed again and no sibling is still starting: servers that crashed racing
// each other at start come back one at a time.
func (s *Supervisor) watchExit(sv *server, g *group) {
	<-g.done
	s.mu.Lock()
	if sv.g != g {
		s.mu.Unlock()
		return
	}
	sv.g = nil
	fwd := sv.fwd
	sv.fwd = nil
	sv.exit = exitOf(g.state)
	starts := sv.starts
	s.logf("crashed %s: exit %d %s", sv.name, sv.exit.Code, sv.exit.Signal)
	after, ok := s.cfg.Tasks()
	sv.taskLimit = nil
	retry := false
	if metTaskLimit(nil, sv.tasks, after, ok && sv.tasksOK) {
		s.limitStart(sv, after, starts)
	} else {
		retry = !sv.retry && sv.startCrash(s.cfg.StartWindow)
		sv.retryPending = retry
	}
	s.mu.Unlock()
	if fwd != nil {
		fwd.close()
	}
	_ = stopGroup(g.pgid, s.cfg.Grace)
	s.persist()
	if retry {
		s.logf("retry %s in %s: it crashed while starting", sv.name, s.cfg.StartWindow)
		deadline := time.Now().Add(listenWait)
		time.AfterFunc(s.cfg.StartWindow, func() { s.retryStart(sv, starts, deadline, reasonRetry) })
	}
}

// limitStart records that the server's start or run met the task limit and,
// unless it has had maxLimitStarts such starts in a row, starts it again
// after backoff — one at a time with its siblings, as a retry does. Callers
// hold mu.
func (s *Supervisor) limitStart(sv *server, t Tasks, starts int) {
	sv.taskLimit = &t
	if sv.limitStarts >= maxLimitStarts {
		s.logf("no start of %s: %s on each of its last %d starts", sv.name, limitReason(&t), maxLimitStarts)
		return
	}
	wait := min(s.cfg.LimitBackoff<<sv.limitStarts, maxLimitBackoff)
	sv.retryPending = true
	s.logf("start %s again in %s: %s (%d tasks)", sv.name, wait, limitReason(&t), t.Current)
	deadline := time.Now().Add(wait + listenWait)
	time.AfterFunc(wait, func() { s.retryStart(sv, starts, deadline, reasonLimit) })
}

// startCrash: the server's exit came within its start window, or before its
// port first accepted (a Maven or Gradle start can outlast any fixed window).
func (sv *server) startCrash(window time.Duration) bool {
	if sv.startedAt == nil {
		return false
	}
	return sv.exit.At.Sub(*sv.startedAt) < window || (sv.spec.Port > 0 && !sv.listened)
}

// retryStart starts a server that crashed while starting once more, or one
// that met the task limit again (reason), unless anything started, stopped or
// removed it since. While a sibling is still starting it looks again one
// window later, until deadline.
func (s *Supervisor) retryStart(sv *server, starts int, deadline time.Time, reason string) {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	still := !s.closed && s.servers[sv.name] == sv && sv.desired == DesiredRunning && sv.g == nil && sv.starts == starts
	wait := still && time.Now().Before(deadline) && s.siblingStarting(sv)
	if !still && sv.starts == starts {
		sv.retryPending = false
	}
	s.mu.Unlock()
	if !still {
		return
	}
	if wait {
		time.AfterFunc(s.cfg.StartWindow, func() { s.retryStart(sv, starts, deadline, reason) })
		return
	}
	s.start(sv.name, reason)
	s.persist()
}

// siblingStarting reports whether another server is still starting: its start
// is under way, or its group lives and has not yet listened (one with a port)
// or passed its start window (one without). Callers hold mu.
func (s *Supervisor) siblingStarting(sv *server) bool {
	for _, o := range s.servers {
		if o == sv {
			continue
		}
		if o.starting {
			return true
		}
		if o.g == nil || o.startedAt == nil {
			continue
		}
		if (o.spec.Port > 0 && !o.listened) || (o.spec.Port == 0 && time.Since(*o.startedAt) < s.cfg.StartWindow) {
			return true
		}
	}
	return false
}

// awaitListen records starting → running once the port accepts.
func (s *Supervisor) awaitListen(sv *server, g *group, port int) {
	deadline := time.Now().Add(listenWait)
	for time.Now().Before(deadline) {
		select {
		case <-g.done:
			return
		default:
		}
		if portAccepts(port) {
			s.mu.Lock()
			if sv.g == g {
				sv.listened = true
			}
			s.mu.Unlock()
			s.syncPreview()
			s.persist()
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func exitOf(ps *os.ProcessState) *Exit {
	e := &Exit{Code: -1, At: time.Now().UTC()}
	if ps == nil {
		return e
	}
	e.Code = ps.ExitCode()
	if ws, ok := ps.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		e.Signal = signalName(ws.Signal())
	}
	return e
}

// signalName is the conventional name (SIGKILL) of a signal.
func signalName(sig syscall.Signal) string {
	if n := sigNames[sig]; n != "" {
		return n
	}
	return sig.String()
}

var sigNames = map[syscall.Signal]string{
	syscall.SIGHUP: "SIGHUP", syscall.SIGINT: "SIGINT", syscall.SIGQUIT: "SIGQUIT", syscall.SIGILL: "SIGILL",
	syscall.SIGABRT: "SIGABRT", syscall.SIGBUS: "SIGBUS", syscall.SIGFPE: "SIGFPE", syscall.SIGKILL: "SIGKILL",
	syscall.SIGSEGV: "SIGSEGV", syscall.SIGPIPE: "SIGPIPE", syscall.SIGTERM: "SIGTERM", syscall.SIGUSR1: "SIGUSR1",
	syscall.SIGUSR2: "SIGUSR2",
}

// syncWatches keeps one watcher per server that has a watch and is meant to
// run; a change restarts it (a crashed one too: the change is the fix). A
// watcher whose watch reads the same — every reconcile parses the
// declaration anew — is kept, with any burst still settling in it: a
// replaced one would drop that burst, and the server would keep the old code.
func (s *Supervisor) syncWatches() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, sv := range s.servers {
		need := sv.desired == DesiredRunning && sv.spec.Watch != nil
		if sv.watch != nil && (!need || !sameWatch(sv.watch.w, sv.spec.Watch)) {
			sv.watch.close()
			sv.watch = nil
		}
		if need && sv.watch == nil {
			w, err := newWatcher(s.cfg.Root, sv.spec.Watch, func(rel string, last time.Time, paths []string) { s.watchFired(name, rel, last, paths) })
			if err != nil {
				s.logf("watch %s: %v", name, err)
				continue
			}
			sv.watch = w
		}
	}
}

// sameWatch reports two watches covering and ignoring the same paths.
func sameWatch(a, b *Watch) bool {
	if a == nil || b == nil {
		return a == b
	}
	return slices.Equal(a.Paths, b.Paths) && slices.Equal(a.Ignore, b.Ignore)
}

// watchFired restarts a server on a burst of watched changes that opened with
// rel, whose last change came at last and which changed paths (nil: too many
// to name). A burst that came before the server's latest start began restarts
// nothing: that start already runs it — a burst that came while a restart
// waited out the old server's stop grace (up to 10 s for one that does not
// end on SIGTERM) would restart the new one again. A change made within the
// start window — by the burst's last change, not by when the burst settled —
// after watchLoopLimit starts in a row each saw one, is the server's own
// output and restarts nothing; a change made outside it restarts, and clears
// the count when the start before it saw no change — except, once the guard
// tripped, a burst of only paths the server's own output changed within its
// windows: a server writing them without a pause keeps each burst open until
// watchSettleMax, so every other burst ends past the window.
func (s *Supervisor) watchFired(name, rel string, last time.Time, paths []string) {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	sv := s.servers[name]
	if sv == nil || sv.desired != DesiredRunning {
		s.mu.Unlock()
		return
	}
	if last.Before(sv.startBegun) {
		starts := sv.starts
		s.mu.Unlock()
		s.logf("no restart of %s: %s changed before its start #%d began, which runs it", name, rel, starts)
		return
	}
	// Decided by when the change was made, never by when its burst settled:
	// the settle ends watchSettle after the last change, past the window for
	// a write made just inside it.
	if sv.startedAt != nil && last.Sub(*sv.startedAt) < s.cfg.StartWindow {
		if !sv.windowChanged {
			sv.windowChanged = true
			sv.selfChanged++
		}
		sv.noteSelfPaths(paths)
		if sv.selfChanged > watchLoopLimit {
			sv.restartReason = fmt.Sprintf("restarts stopped: %s changed during each of its last %d starts; add the path to watch.ignore", rel, watchLoopLimit)
			s.mu.Unlock()
			s.logf("no restart of %s: %s changed during its start, as on each of its last %d starts; add the path to watch.ignore", name, rel, watchLoopLimit)
			s.persist()
			return
		}
	} else if !sv.windowChanged {
		sv.selfChanged, sv.selfPaths, sv.selfOverflow = 0, nil, false
	} else if sv.selfChanged > watchLoopLimit && sv.ownOutput(paths) {
		s.mu.Unlock()
		s.logf("no restart of %s: %s changed after its start window, among the paths only its own output changes; add them to watch.ignore", name, rel)
		return
	}
	sv.starting = true
	s.mu.Unlock()
	s.logf("restart %s: %s changed", name, rel)
	s.stopAll([]string{name})
	s.start(name, rel+" changed")
	s.persist()
}

// noteSelfPaths keeps the paths of a burst inside the start window: the
// server's own output.
func (sv *server) noteSelfPaths(paths []string) {
	if sv.selfOverflow {
		return
	}
	if paths == nil || len(sv.selfPaths)+len(paths) > burstPathsMax {
		sv.selfPaths, sv.selfOverflow = nil, true
		return
	}
	if sv.selfPaths == nil {
		sv.selfPaths = map[string]bool{}
	}
	for _, p := range paths {
		sv.selfPaths[p] = true
	}
}

// ownOutput reports a burst whose every path the server's own output changed
// inside its windows; a burst that names no set, or the server's output too
// many paths to keep, is not.
func (sv *server) ownOutput(paths []string) bool {
	if sv.selfOverflow || len(paths) == 0 {
		return false
	}
	for _, p := range paths {
		if !sv.selfPaths[p] {
			return false
		}
	}
	return true
}

// ErrUnknownServer marks a control call naming a server the supervisor does
// not hold.
var ErrUnknownServer = errors.New("unknown server")

// Start re-reads the declaration, sets a server to run and starts it unless it
// runs.
func (s *Supervisor) Start(name string) error {
	s.op.Lock()
	defer s.op.Unlock()
	s.reconcile(false)
	if err := s.setDesired(name, DesiredRunning); err != nil {
		return err
	}
	s.mu.Lock()
	live := s.servers[name].g != nil
	s.mu.Unlock()
	if !live {
		s.start(name, reasonStart)
	}
	s.syncWatches()
	s.persist()
	return nil
}

// Stop sets a server to stay stopped and ends its process group.
func (s *Supervisor) Stop(name string) error {
	s.op.Lock()
	defer s.op.Unlock()
	if err := s.setDesired(name, DesiredStopped); err != nil {
		return err
	}
	s.stopAll([]string{name})
	s.syncWatches()
	s.persist()
	return nil
}

// Restart re-reads the declaration, then stops a server if it runs and starts
// it again, unless the re-read just did.
func (s *Supervisor) Restart(name string) error {
	s.op.Lock()
	defer s.op.Unlock()
	acts := s.reconcile(false)
	if err := s.setDesired(name, DesiredRunning); err != nil {
		return err
	}
	if a := acts[name]; a == actRestart || a == actStart {
		return nil
	}
	s.mu.Lock()
	s.servers[name].starting = true
	s.mu.Unlock()
	s.stopAll([]string{name})
	s.start(name, reasonAsked)
	s.syncWatches()
	s.persist()
	return nil
}

// RunEntry adds (or replaces) a runtime entry — a server the declaration does
// not hold, such as a crew app — and starts it.
func (s *Supervisor) RunEntry(spec Server) error {
	norm, err := normalizeServer(spec)
	if err != nil {
		return err
	}
	if err := insideRoot(s.cfg.Root, norm.Cwd); err != nil {
		return err
	}
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	sv, ok := s.servers[norm.Name]
	if ok && sv.source == SourceDeclared {
		s.mu.Unlock()
		return fmt.Errorf("%q is declared in %s; it cannot also be a runtime entry", norm.Name, DeclarationPath)
	}
	if !ok {
		sv = &server{name: norm.Name, source: SourceRuntime}
		s.servers[norm.Name] = sv
	}
	restart := sv.g != nil && !sv.spec.SameRun(norm)
	sv.desired = DesiredRunning
	s.mu.Unlock()
	if restart {
		s.stopAll([]string{norm.Name})
	}
	s.mu.Lock()
	sv.spec = norm
	live := sv.g != nil
	s.mu.Unlock()
	if !live {
		reason := reasonStart
		if restart {
			reason = reasonRunSpec
		}
		s.start(norm.Name, reason)
	}
	s.persist()
	return nil
}

// Remove stops and forgets a runtime entry.
func (s *Supervisor) Remove(name string) error {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	sv, ok := s.servers[name]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("%w: %q", ErrUnknownServer, name)
	}
	if sv.source == SourceDeclared {
		s.mu.Unlock()
		return fmt.Errorf("%q is declared in %s; remove it there", name, DeclarationPath)
	}
	s.mu.Unlock()
	s.stopAll([]string{name})
	s.mu.Lock()
	delete(s.servers, name)
	s.mu.Unlock()
	s.persist()
	return nil
}

func (s *Supervisor) setDesired(name string, d Desired) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sv, ok := s.servers[name]
	if !ok || sv.spec.Command == "" {
		return fmt.Errorf("%w: no server named %q", ErrUnknownServer, name)
	}
	sv.desired = d
	return nil
}

// SetEnv replaces the env overlay with env (docs/spec-mate.md §2.8a.2
// `ctl env`): every server started from now on gets it. It returns once the
// overlay is stored in state.json; the live servers started under another
// overlay are then restarted in the supervisor's own goroutine, one at a time
// a Stagger apart — a stop's grace per server could outlast a ctl call's wait.
// An identical overlay changes nothing; a refused one changes nothing either.
func (s *Supervisor) SetEnv(env map[string]string) error {
	if err := checkOverlay(env); err != nil {
		return err
	}
	s.mu.Lock()
	if maps.Equal(s.env, env) {
		s.mu.Unlock()
		return nil
	}
	s.env = overlayCopy(env)
	run := !s.envRestarting && !s.closed
	if run {
		s.envRestarting = true
	}
	s.mu.Unlock()
	s.logf("env overlay set: %s", overlayNames(env))
	s.persist()
	if run {
		go s.restartForEnv()
	}
	return nil
}

// restartForEnv restarts, a Stagger apart, every live server whose group
// started under another overlay than the current one, until none is left; an
// overlay set meanwhile is picked up by the same loop.
func (s *Supervisor) restartForEnv() {
	for i := 0; ; i++ {
		if i > 0 {
			time.Sleep(s.cfg.Stagger)
		}
		if !s.restartOneForEnv() {
			return
		}
	}
}

// restartOneForEnv restarts the first live server, by name, that started
// under another overlay than the current one. It answers false, and marks the
// restarts done, when none is left or the supervisor shut down.
func (s *Supervisor) restartOneForEnv() bool {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	name := ""
	if !s.closed {
		for _, n := range slices.Sorted(maps.Keys(s.servers)) {
			if sv := s.servers[n]; sv.g != nil && sv.desired == DesiredRunning && !maps.Equal(sv.env, s.env) {
				name = n
				break
			}
		}
	}
	if name == "" {
		s.envRestarting = false
		s.mu.Unlock()
		return false
	}
	s.servers[name].starting = true
	s.mu.Unlock()
	s.logf("restart %s: %s", name, reasonEnv)
	s.stopAll([]string{name})
	s.start(name, reasonEnv)
	s.persist()
	return true
}

// checkOverlay refuses an overlay that would set a name the container's own
// value must reach (envNameError) or a value exec cannot carry.
func checkOverlay(env map[string]string) error {
	for _, name := range slices.Sorted(maps.Keys(env)) {
		if err := envNameError(name); err != nil {
			return fmt.Errorf("env: %w", err)
		}
		if strings.ContainsRune(env[name], 0) {
			return fmt.Errorf("env: %q: its value holds a NUL byte", name)
		}
	}
	return nil
}

// overlayCopy is a copy of env that is never nil.
func overlayCopy(env map[string]string) map[string]string {
	out := make(map[string]string, len(env))
	maps.Copy(out, env)
	return out
}

// withOverlay is base with every overlay entry in place of base's own value
// of that name, the overlay's entries last and in name order.
func withOverlay(base []string, env map[string]string) []string {
	if len(env) == 0 {
		return base
	}
	out := make([]string, 0, len(base)+len(env))
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if _, ok := env[name]; !ok {
			out = append(out, kv)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(env)) {
		out = append(out, name+"="+env[name])
	}
	return out
}

// overlayNames names the overlay's variables for the runtime log, which
// project members read: a value never goes there.
func overlayNames(env map[string]string) string {
	if len(env) == 0 {
		return "(none)"
	}
	return strings.Join(slices.Sorted(maps.Keys(env)), ", ")
}

// Shutdown stops every server and keeps what each was set to, so the next
// instance starts the same ones.
func (s *Supervisor) Shutdown() {
	s.op.Lock()
	defer s.op.Unlock()
	s.mu.Lock()
	s.closed = true
	names := make([]string, 0, len(s.servers))
	for name, sv := range s.servers {
		names = append(names, name)
		if sv.watch != nil {
			sv.watch.close()
			sv.watch = nil
		}
	}
	s.mu.Unlock()
	s.logf("shutdown: stopping %d server(s)", len(names))
	s.stopAll(names)
	s.persist()
}

// State is the current state, each status derived now.
func (s *Supervisor) State() State {
	s.mu.Lock()
	type row struct {
		name    string
		sv      server
		preview int
	}
	rows := make([]row, 0, len(s.servers))
	for name, sv := range s.servers {
		rows = append(rows, row{name, *sv, s.previewPortLocked(name)})
	}
	st := State{
		Format:      StateFormat,
		Supervisor:  SupervisorInfo{Version: s.cfg.Version, Pid: os.Getpid(), StartedAt: s.startedAt},
		Declaration: s.decl,
		Env:         overlayCopy(s.env),
		Servers:     make(map[string]ServerState, len(rows)),
	}
	s.mu.Unlock()
	for _, r := range rows {
		st.Servers[r.name] = s.serverState(r.name, &r.sv, r.preview)
	}
	return st
}

// serverState renders one server; it probes the port of a live group, so it
// runs outside mu. The preview server's activity is read under its own lock.
func (s *Supervisor) serverState(name string, sv *server, preview int) ServerState {
	ss := ServerState{
		Source: sv.source, Desired: sv.desired, StartedAt: sv.startedAt, Exit: sv.exit,
		LogPath: LogPath(s.cfg.StateDir, name), LastRestartReason: sv.restartReason, OutOfMemory: sv.outOfMemory,
		PreviewPort: preview,
	}
	if preview != 0 {
		open := 0
		if sv.preview != nil {
			ss.PreviewLastConnAt, open = sv.preview.read()
		}
		ss.PreviewOpenConns = &open
	}
	if sv.starts > 1 {
		ss.Restarts = sv.starts - 1
	}
	if sv.source == SourceRuntime {
		ss.Command, ss.Cwd, ss.Port = sv.spec.Command, sv.spec.Cwd, sv.spec.Port
	}
	if sv.g != nil {
		ss.Pid, ss.Pgid, ss.LeaderStart = sv.g.pgid, sv.g.pgid, sv.g.leaderStart
		ss.RssKB = groupRSSKB(sv.g.pgid)
	}
	switch {
	case sv.desired == DesiredStopped:
		ss.Status = StatusStopped
	case sv.starting:
		ss.Status = StatusStarting
	case sv.g != nil && sv.spec.Port > 0 && !portAccepts(sv.spec.Port):
		ss.Status = StatusStarting
	case sv.g == nil && sv.exit != nil:
		ss.Status = StatusCrashed
		ss.RetryPending = sv.retryPending
		if sv.taskLimit != nil {
			now, ok := s.cfg.Tasks()
			if !ok {
				now = *sv.taskLimit
			}
			ss.TaskLimit = &TaskLimit{Max: sv.taskLimit.Max, Current: now.Current}
		}
	default:
		ss.Status = StatusRunning
	}
	return ss
}

// persist writes the state to state.json, all but the preview activity,
// which lives in memory only: a file a new supervisor finds never claims
// connections it does not hold.
func (s *Supervisor) persist() {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	st := s.State()
	for name, ss := range st.Servers {
		ss.PreviewLastConnAt, ss.PreviewOpenConns = nil, nil
		st.Servers[name] = ss
	}
	if err := writeState(s.cfg.StateDir, st); err != nil {
		s.logf("%v", err)
	}
}

// stableNumber is the container's ZEROPS_StableNumber in env (the last entry
// wins, as for a started process); 0 when it is absent or not a positive
// number.
func stableNumber(env []string) int {
	value := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "ZEROPS_StableNumber="); ok {
			value = v
		}
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 1 {
		return 0
	}
	return n
}

// previewPortLocked is the preview port of server name: previewPortBase plus
// the container's slot when name is the declaration's preview server; 0 when
// there is no base, no slot, another server, or a sum past the port range.
// Callers hold mu.
func (s *Supervisor) previewPortLocked(name string) int {
	if s.previewBase <= 0 || s.slot <= 0 || name != s.previewName {
		return 0
	}
	if port := s.previewBase + s.slot; port <= 65535 {
		return port
	}
	return 0
}

// syncPreview brings the preview forward to what the declaration says: the
// preview server's live group, once its port has accepted, is forwarded from
// its preview port; any other forward ends. A forward that cannot listen
// (its port is busy) is logged and the server runs on without one. The
// forwards it ended are waited for — their copies, a dial to the server not
// yet finished — once fwdMu is let go, so another sync never waits on them.
func (s *Supervisor) syncPreview() {
	for _, f := range s.syncPreviewLocked() {
		f.wait()
	}
}

// syncPreviewLocked is syncPreview's work under fwdMu, answering the forwards
// it shut. The listens stay under the lock with the shuts: every forward
// takes the one preview port, so two syncs must never both open a server's
// forward, nor open one while an ended one still listens there — a shut and
// a listen return at once, unlike the wait that follows a shut.
func (s *Supervisor) syncPreviewLocked() []*forward {
	s.fwdMu.Lock()
	defer s.fwdMu.Unlock()
	type open struct {
		sv           *server
		g            *group
		listen, port int
		seen         *previewSeen
	}
	var stale []*forward
	var opens []open
	s.mu.Lock()
	for name, sv := range s.servers {
		want := 0
		if sv.g != nil && sv.listened && sv.spec.Port > 0 {
			want = s.previewPortLocked(name)
		}
		if sv.fwd != nil && (want == 0 || sv.fwd.listenPort != want || sv.fwd.targetPort != sv.spec.Port) {
			stale = append(stale, sv.fwd)
			sv.fwd = nil
		}
		if want > 0 && sv.fwd == nil {
			if sv.preview == nil {
				sv.preview = &previewSeen{}
			}
			opens = append(opens, open{sv, sv.g, want, sv.spec.Port, sv.preview})
		}
	}
	s.mu.Unlock()
	for _, f := range stale {
		f.shut()
	}
	ended := stale
	for _, o := range opens {
		f, err := openForward(o.listen, o.port, o.seen)
		if err != nil {
			s.logf("preview %s: cannot listen on :%d: %v", o.sv.name, o.listen, err)
			continue
		}
		s.mu.Lock()
		// The group may have ended, or been replaced, while the port opened.
		keep := o.sv.g == o.g && o.sv.fwd == nil
		if keep {
			o.sv.fwd = f
		}
		s.mu.Unlock()
		if !keep {
			f.shut()
			ended = append(ended, f)
			continue
		}
		s.logf("preview %s: :%d -> 127.0.0.1:%d", o.sv.name, o.listen, o.port)
	}
	return ended
}
