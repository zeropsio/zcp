//go:build linux || darwin

package devsup

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

type rig struct {
	t     *testing.T
	root  string
	state string
	// window is the supervisors' start window; zero is the default.
	window time.Duration
	// stagger is the gap between servers started together; zero is 1 ms, so
	// tests that start several servers do not wait the default gap.
	stagger time.Duration
	// tasks, when set, is the supervisors' reading of the service's task limit.
	tasks func() (Tasks, bool)
	// previousRun, when set, is the supervisors' reading of systemd's result
	// for the previous run of their unit.
	previousRun func() (string, bool)
	// env is added to the environment the supervisors start servers with.
	env []string
	// grace is how long a stopped group gets before SIGKILL; zero is 1 s.
	grace time.Duration
}

// newRig makes a repo root and a short state directory (a unix socket path
// must stay under ~100 bytes on macOS).
func newRig(t *testing.T) *rig {
	t.Helper()
	return &rig{t: t, root: t.TempDir(), state: shortDir(t)}
}

func shortDir(t *testing.T) string {
	t.Helper()
	d, err := os.MkdirTemp("/tmp", "devsup") //nolint:usetesting // the unix socket path must stay under 104 bytes on macOS; t.TempDir() is longer
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(d) })
	return d
}

func (r *rig) declare(servers ...Server) {
	r.t.Helper()
	b, err := json.Marshal(Declaration{Version: 1, Servers: servers})
	if err != nil {
		r.t.Fatal(err)
	}
	r.writeDeclaration(string(b))
}

func (r *rig) writeDeclaration(s string) {
	r.t.Helper()
	r.write(DeclarationPath, s)
}

func (r *rig) write(rel, s string) {
	r.t.Helper()
	p := filepath.Join(r.root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) supervisorWith(service string, portWait time.Duration) *Supervisor {
	r.t.Helper()
	stagger := r.stagger
	if stagger == 0 {
		stagger = time.Millisecond
	}
	grace := r.grace
	if grace == 0 {
		grace = time.Second
	}
	s, err := New(Config{
		Root: r.root, StateDir: r.state, Service: service, Env: append(os.Environ(), r.env...), Grace: grace, PortWait: portWait,
		StartWindow: r.window, Stagger: stagger, LimitBackoff: 50 * time.Millisecond, Tasks: r.tasks, PreviousRun: r.previousRun, Version: "v-test",
	})
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(s.Shutdown)
	return s
}

func (r *rig) supervisor() *Supervisor { return r.supervisorWith("", 5*time.Second) }

func (r *rig) listener(name string, port int) Server {
	return Server{Name: name, Port: port, Command: helperCmd(r.t, "listen", "PORT="+strconv.Itoa(port))}
}

func mustServer(t *testing.T, st State, name string) ServerState {
	t.Helper()
	sv, ok := st.Servers[name]
	if !ok {
		t.Fatalf("no server %q in %+v", name, st.Servers)
	}
	return sv
}

func TestSupervisor_AfterARedeployEveryDeclaredServerStarts(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web, api := freePort(t), freePort(t)
	r.declare(r.listener("web", web), r.listener("api", api))

	s := r.supervisor()
	s.Boot()

	eventually(t, "web and api to listen", func() bool { return listening(web) && listening(api) })
	eventually(t, "state.json to show both running", func() bool {
		st, err := ReadState(r.state)
		return err == nil && st.Servers["web"].Status == StatusRunning && st.Servers["api"].Status == StatusRunning
	})
	st, _ := ReadState(r.state)
	if st.Format != StateFormat || st.Supervisor.Version != "v-test" || st.Supervisor.Pid != os.Getpid() || st.Declaration.Version != 1 {
		t.Fatalf("state header = %+v %+v %+v", st.Format, st.Supervisor, st.Declaration)
	}
	for _, name := range []string{"web", "api"} {
		sv := mustServer(t, st, name)
		if sv.Source != SourceDeclared || sv.Desired != DesiredRunning || sv.Pid <= 1 || sv.Pgid != sv.Pid ||
			sv.LeaderStart == 0 || sv.StartedAt == nil || sv.Restarts != 0 || sv.Exit != nil || sv.Command != "" {
			t.Fatalf("%s = %+v", name, sv)
		}
	}
}

func TestSupervisor_EachNamedServerHasItsOwnLogAndPid(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web, api := freePort(t), freePort(t)
	r.declare(r.listener("web", web), r.listener("api", api))
	s := r.supervisor()
	s.Boot()
	eventually(t, "both to listen", func() bool { return listening(web) && listening(api) })

	st := s.State()
	w, a := mustServer(t, st, "web"), mustServer(t, st, "api")
	if w.LogPath == a.LogPath || w.Pid == a.Pid {
		t.Fatalf("web and api share a log or a pid: %+v %+v", w, a)
	}
	// The helper prints after it listens, so the line may trail the port.
	for _, sv := range []ServerState{w, a} {
		own := "listening pid=" + strconv.Itoa(sv.Pid)
		eventually(t, sv.LogPath+" to hold its own server's output", func() bool {
			b, err := os.ReadFile(sv.LogPath)
			return err == nil && strings.Contains(string(b), own)
		})
	}
}

func TestSupervisor_StoppingOneNamedServerLeavesItsSiblingsRunning(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web, api := freePort(t), freePort(t)
	r.declare(r.listener("web", web), r.listener("api", api))
	s := r.supervisor()
	s.Boot()
	eventually(t, "both to listen", func() bool { return listening(web) && listening(api) })
	apiPid := mustServer(t, s.State(), "api").Pid

	if err := s.Stop("web"); err != nil {
		t.Fatal(err)
	}

	released(t, web)
	st := s.State()
	if sv := mustServer(t, st, "web"); sv.Status != StatusStopped || sv.Desired != DesiredStopped || sv.Pid != 0 || sv.Exit != nil {
		t.Fatalf("web = %+v", sv)
	}
	if sv := mustServer(t, st, "api"); sv.Status != StatusRunning || sv.Pid != apiPid || !listening(api) {
		t.Fatalf("api = %+v, listening %v", sv, listening(api))
	}
}

func TestSupervisor_AServerThatExitsStaysDownAndVisible(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.window = 300 * time.Millisecond
	// It exits after its start window: a crash within it is started once more.
	r.declare(Server{Name: "api", Command: "sleep 1; " + helperCmd(t, "exit", "CODE=3")})
	s := r.supervisor()
	s.Boot()

	eventually(t, "api to be recorded as crashed", func() bool {
		return mustServer(t, s.State(), "api").Status == StatusCrashed
	})
	time.Sleep(3 * r.window) // a restart that should not happen has no event to wait on
	s.Reload()
	sv := mustServer(t, s.State(), "api")
	if sv.Status != StatusCrashed || sv.Restarts != 0 || sv.Pid != 0 {
		t.Fatalf("a reload restarted the crashed server: %+v", sv)
	}
	if sv.Exit == nil || sv.Exit.Code != 3 || sv.Exit.Signal != "" || sv.Exit.At.IsZero() {
		t.Fatalf("exit = %+v, want code 3", sv.Exit)
	}
	kept, err := ReadState(r.state)
	if err != nil {
		t.Fatal(err)
	}
	if k := mustServer(t, kept, "api"); k.Status != StatusCrashed || k.Exit == nil || k.Exit.Code != 3 {
		t.Fatalf("state.json = %+v", k)
	}
	b, _ := os.ReadFile(sv.LogPath)
	if !strings.Contains(string(b), "line 30") {
		t.Fatalf("the log does not hold the crashed server's output:\n%s", b)
	}
}

func TestSupervisor_AfterAContainerRestartStoppedStaysStoppedAndCrashedStarts(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web, api := freePort(t), freePort(t)
	r.declare(r.listener("web", web), r.listener("api", api), Server{Name: "job", Command: helperCmd(t, "exit", "CODE=1")})
	s1 := r.supervisor()
	s1.Boot()
	eventually(t, "both to listen and job to crash", func() bool {
		return listening(web) && listening(api) && mustServer(t, s1.State(), "job").Status == StatusCrashed
	})
	if err := s1.Stop("web"); err != nil {
		t.Fatal(err)
	}
	s1.Shutdown() // the container stops; $HOME stays
	if listening(api) {
		t.Fatal("api still listens after shutdown")
	}

	s2 := r.supervisor()
	s2.Boot()

	eventually(t, "api to listen again", func() bool { return listening(api) })
	st := s2.State()
	if sv := mustServer(t, st, "web"); sv.Status != StatusStopped || listening(web) {
		t.Fatalf("web came back after a restart: %+v", sv)
	}
	if sv := mustServer(t, st, "api"); sv.Restarts != 1 {
		t.Fatalf("api restarts = %d, want 1", sv.Restarts)
	}
	if sv := mustServer(t, st, "job"); sv.Restarts != 1 {
		t.Fatalf("the crashed job did not start again on restart: %+v", sv)
	}
}

func TestSupervisor_AfterARedeployAStoppedServerStartsToo(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web := freePort(t)
	r.declare(r.listener("web", web))
	s1 := r.supervisor()
	s1.Boot()
	eventually(t, "web to listen", func() bool { return listening(web) })
	if err := s1.Stop("web"); err != nil {
		t.Fatal(err)
	}
	s1.Shutdown()

	// A redeploy is a new container: $HOME, and with it the state, is new.
	r.state = shortDir(t)
	s2 := r.supervisor()
	s2.Boot()
	eventually(t, "web to listen after the redeploy", func() bool { return listening(web) })
}

func TestSupervisor_AGroupAKilledSupervisorLeftIsReapedAndStartedAgain(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web := freePort(t)
	r.declare(r.listener("web", web))
	s1, err := New(Config{Root: r.root, StateDir: r.state, Env: os.Environ(), Grace: time.Second, PortWait: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	s1.Boot()
	eventually(t, "web to listen", func() bool { return listening(web) })
	old := mustServer(t, s1.State(), "web").Pgid
	// s1 is never shut down: like `kill -9` of the supervisor, its group is
	// left running and zenv starts a new supervisor.

	s2 := r.supervisor()
	s2.Boot()

	sv := mustServer(t, s2.State(), "web")
	if sv.Pgid == old || sv.Pgid == 0 {
		t.Fatalf("web = %+v (old pgid %d)", sv, old)
	}
	if groupAlive(old) {
		t.Fatalf("the left-behind group %d was not reaped", old)
	}
	eventually(t, "the new web to listen", func() bool { return listening(web) })
}

func TestSupervisor_AGroupWhoseLeaderStartDiffersIsNeverReaped(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	port := freePort(t)
	foreign, _, _ := startHelper(t, "listen", port)
	r.declare(r.listener("web", port))
	// state.json names the foreign group with another start time: the pid
	// was reused after the recorded server ended.
	started := time.Now().UTC()
	if err := os.MkdirAll(r.state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeState(r.state, State{Format: 1, Servers: map[string]ServerState{"web": {
		Source: SourceDeclared, Desired: DesiredRunning, Pid: foreign.pgid, Pgid: foreign.pgid,
		LeaderStart: foreign.leaderStart + 7, StartedAt: &started,
	}}}); err != nil {
		t.Fatal(err)
	}
	s := r.supervisorWith("", 300*time.Millisecond)
	s.Boot()

	if !groupAlive(foreign.pgid) || !listening(port) {
		t.Fatal("the supervisor killed a group it did not start")
	}
	sv := mustServer(t, s.State(), "web")
	if sv.Status != StatusCrashed || sv.Exit == nil || sv.Exit.Code != -1 || sv.Exit.Signal != ExitPortBusy {
		t.Fatalf("web = %+v exit %+v", sv, sv.Exit)
	}
}

func TestSupervisor_ReloadRestartsOnlyTheChangedServerAndDropsARemovedOne(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web, api, worker := freePort(t), freePort(t), freePort(t)
	r.declare(r.listener("web", web), r.listener("api", api), r.listener("worker", worker))
	s := r.supervisor()
	s.Boot()
	eventually(t, "all to listen", func() bool { return listening(web) && listening(api) && listening(worker) })
	before := s.State()

	changed := r.listener("api", api)
	changed.Command += " # v2"
	r.declare(r.listener("web", web), changed)
	s.Reload()

	after := s.State()
	if mustServer(t, after, "web").Pid != mustServer(t, before, "web").Pid {
		t.Fatal("web restarted though it did not change")
	}
	if a := mustServer(t, after, "api"); a.Pid == mustServer(t, before, "api").Pid || a.Restarts != 1 {
		t.Fatalf("api did not restart: %+v", a)
	}
	if _, ok := after.Servers["worker"]; ok {
		t.Fatal("worker was not stopped and dropped")
	}
	released(t, worker)
	eventually(t, "api to listen again", func() bool { return listening(api) })
}

// An agent edits .zcp/dev-servers.json and asks for a restart or a start; it
// never has to know about `ctl reload`.
func TestSupervisor_StartAndRestartRunTheDeclarationAsEditedWithoutAReload(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web := freePort(t)
	srv := r.listener("web", web)
	r.declare(srv)
	s := r.supervisor()
	s.Boot()
	eventually(t, "web to listen", func() bool { return listening(web) })

	ranLast := func(marker string) bool {
		b, _ := os.ReadFile(LogPath(r.state, "web"))
		starts := strings.Split(string(b), "--- zcp-devsup: start #")
		return strings.Contains(starts[len(starts)-1], marker)
	}
	v2 := srv
	v2.Command += " # v2"
	r.declare(v2)
	if err := s.Restart("web"); err != nil {
		t.Fatal(err)
	}
	if !ranLast("# v2") {
		t.Fatal("restart ran the command from before the edit")
	}
	if sv := mustServer(t, s.State(), "web"); sv.Restarts != 1 {
		t.Fatalf("the edit restarted web twice: %+v", sv)
	}

	if err := s.Stop("web"); err != nil {
		t.Fatal(err)
	}
	released(t, web)
	v3 := srv
	v3.Command += " # v3"
	r.declare(v3)
	if err := s.Start("web"); err != nil {
		t.Fatal(err)
	}
	if !ranLast("# v3") {
		t.Fatal("start ran the command from before the edit")
	}
	eventually(t, "web to listen again", func() bool { return listening(web) })
}

func TestSupervisor_AnInvalidOrNewerDeclarationChangesNothingAndIsReported(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web := freePort(t)
	r.declare(r.listener("web", web))
	s := r.supervisor()
	s.Boot()
	eventually(t, "web to listen", func() bool { return listening(web) })
	pid := mustServer(t, s.State(), "web").Pid

	for _, bad := range []string{`{"version":2,"servers":[]}`, `{"version":1,"servers":[{"name":"web"}]}`, `{`} {
		r.writeDeclaration(bad)
		s.Reload()
		st := s.State()
		if st.Declaration.Error == "" {
			t.Fatalf("%s: no declaration error reported", bad)
		}
		if sv := mustServer(t, st, "web"); sv.Pid != pid || !listening(web) {
			t.Fatalf("%s: web changed under an unreadable declaration: %+v", bad, sv)
		}
	}
	if st := s.State(); !strings.Contains(st.Declaration.Error, "parse") {
		t.Fatalf("error = %q", st.Declaration.Error)
	}
}

func TestSupervisor_RunsOnlyTheEntriesOfItsOwnService(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web, api := freePort(t), freePort(t)
	w, a := r.listener("web", web), r.listener("api", api)
	w.Service, a.Service = "bundev", "godev"
	r.declare(w, a)
	s := r.supervisorWith("godev", 5*time.Second)
	s.Boot()

	eventually(t, "api to listen", func() bool { return listening(api) })
	if _, ok := s.State().Servers["web"]; ok || listening(web) {
		t.Fatal("godev's supervisor ran bundev's server")
	}
}

func TestSupervisor_AWatchedChangeRestartsTheServerAndIgnoredOutputDoesNot(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	api := freePort(t)
	r.write("apps/api/main.go", "package main")
	r.write("apps/api/node_modules/x/index.js", "1")
	srv := r.listener("api", api)
	srv.Watch = &Watch{Paths: []string{"apps/api"}, Ignore: []string{"apps/api/app*"}}
	r.declare(srv)
	s := r.supervisor()
	s.Boot()
	eventually(t, "api to listen", func() bool { return listening(api) })
	first := mustServer(t, s.State(), "api").Pid

	// Build output and dependencies do not restart it; `go build -o app`
	// also writes app-go-tmp-umask beside its output.
	r.write("apps/api/app", "binary")
	r.write("apps/api/app-go-tmp-umask", "")
	r.write("apps/api/node_modules/x/index.js", "2")
	time.Sleep(4 * watchSettle) // a restart that should not happen has no event to wait on
	if sv := mustServer(t, s.State(), "api"); sv.Pid != first || sv.Restarts != 0 {
		t.Fatalf("ignored output restarted api: %+v", sv)
	}

	// A source change does, once.
	r.write("apps/api/main.go", "package main // v2")
	eventually(t, "api to restart", func() bool {
		sv := mustServer(t, s.State(), "api")
		return sv.Pid != first && sv.Pid != 0 && sv.Restarts == 1
	})
	eventually(t, "api to listen again", func() bool { return listening(api) })
}

// An agent reading the repo over the zcp mount restarted the server it read:
// the read moved the file's access time. Reading a watched file never
// restarts its server; writing one does.
func TestSupervisor_ReadingAWatchedFileNeverRestartsItsServerAndWritingOneDoes(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	api := freePort(t)
	r.write("apps/api/Main.java", "class Main {}")
	srv := r.listener("api", api)
	srv.Watch = &Watch{Paths: []string{"apps/api"}}
	r.declare(srv)
	s := r.supervisor()
	s.Boot()
	eventually(t, "api to listen", func() bool { return listening(api) })
	first := mustServer(t, s.State(), "api").Pid

	// A read: the content, the directory listing and the access time it moves.
	p := filepath.Join(r.root, "apps/api/Main.java")
	if _, err := os.ReadFile(p); err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadDir(filepath.Dir(p)); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, time.Now().Add(time.Hour), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(4 * watchSettle) // a restart that should not happen has no event to wait on
	if sv := mustServer(t, s.State(), "api"); sv.Pid != first || sv.Restarts != 0 {
		t.Fatalf("reading Main.java restarted api: %+v, reason %q", sv, sv.LastRestartReason)
	}

	r.write("apps/api/Main.java", "class Main { int v2; }")
	eventually(t, "the write to restart api", func() bool {
		sv := mustServer(t, s.State(), "api")
		return sv.Restarts == 1 && sv.LastRestartReason == "apps/api/Main.java changed"
	})
}

// One write over sshfs (`cat > file`) restarted a watched server 2–3 times
// in the same second: the write reached the watch as several changes spread
// over more than the settle. A burst of changes, each coming before the last
// settled, restarts the server once, naming the path that started the burst.
func TestSupervisor_ABurstOfWatchedChangesRestartsTheServerOnce(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	api := freePort(t)
	r.write("apps/api/main.go", "v1")
	srv := r.listener("api", api)
	srv.Watch = &Watch{Paths: []string{"apps/api"}}
	r.declare(srv)
	s := r.supervisor()
	s.Boot()
	eventually(t, "api to listen", func() bool { return listening(api) })

	// Truncate, then the bytes, then the close: each a change, the gaps
	// longer than one settle, never as long as the quiet that ends a burst.
	for i, body := range []string{"", "v2", "v2 done"} {
		if i > 0 {
			time.Sleep(300 * time.Millisecond)
		}
		r.write("apps/api/main.go", body)
	}
	eventually(t, "api to restart", func() bool { return mustServer(t, s.State(), "api").Restarts >= 1 })
	eventually(t, "api to listen again", func() bool { return listening(api) })
	time.Sleep(2 * time.Second) // a restart that should not happen has no event to wait on
	if sv := mustServer(t, s.State(), "api"); sv.Restarts != 1 || sv.LastRestartReason != "apps/api/main.go changed" {
		t.Fatalf("api = %d restart(s), reason %q; want one restart for the one write, naming apps/api/main.go", sv.Restarts, sv.LastRestartReason)
	}
}

// A sync's fast-forward that changes the declaration reloads the supervisor a
// few hundred ms after it changed the files a watch covers — inside the
// settle of their burst. A reload, or a start of another server, that
// replaced every watcher dropped that burst, and an unchanged watched server
// kept serving the old code. A watcher whose watch is unchanged is kept, its
// burst with it, so the change still restarts the server once.
func TestSupervisor_AReloadOrAStartOfAnotherServerKeepsAWatchedChangeStillSettling(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		act  func(t *testing.T, s *Supervisor)
	}{
		{"reload", func(_ *testing.T, s *Supervisor) { s.Reload() }},
		{"start of another server", func(t *testing.T, s *Supervisor) {
			if err := s.Start("web"); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			api, web := freePort(t), freePort(t)
			r.write("apps/api/main.go", "v1")
			srv := r.listener("api", api)
			srv.Watch = &Watch{Paths: []string{"apps/api"}}
			other := r.listener("web", web)
			off := false
			other.Autostart = &off
			r.declare(srv, other)
			s := r.supervisor()
			s.Boot()
			eventually(t, "api to listen", func() bool { return listening(api) })

			r.write("apps/api/main.go", "v2 (fast-forward)")
			time.Sleep(200 * time.Millisecond) // the watch saw it; its burst is settling
			tc.act(t, s)
			eventually(t, "the watched change to restart api", func() bool { return mustServer(t, s.State(), "api").Restarts >= 1 })
			time.Sleep(2 * time.Second) // a restart that should not happen has no event to wait on
			if sv := mustServer(t, s.State(), "api"); sv.Restarts != 1 || sv.LastRestartReason != "apps/api/main.go changed" {
				t.Fatalf("api = %d restart(s), reason %q; want one, for the change", sv.Restarts, sv.LastRestartReason)
			}
		})
	}
}

// The 8–10 s between a write and the restart was the old server's stop grace:
// one that does not end on SIGTERM is killed only after it. Every change that
// came while that restart stopped the server is already on disk when the new
// one starts, so it restarts nothing again; a change after the start still
// restarts it, naming its own path.
func TestSupervisor_AChangeMadeWhileARestartStopsTheServerRestartsNothingMore(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.grace = 3 * time.Second
	api := freePort(t)
	r.write("apps/api/main.go", "v1")
	r.write("apps/api/util.go", "v1")
	r.write("apps/api/late.go", "v1")
	srv := Server{Name: "api", Port: api, Command: helperCmd(t, "ignore-term", "PORT="+strconv.Itoa(api)), Watch: &Watch{Paths: []string{"apps/api"}}}
	r.declare(srv)
	s := r.supervisor()
	s.Boot()
	eventually(t, "api to listen", func() bool { return listening(api) })

	r.write("apps/api/main.go", "v2")
	// The restart is stopping api, which waits out its grace: two more
	// changes, each settling on its own, come meanwhile.
	time.Sleep(time.Second)
	r.write("apps/api/util.go", "v2")
	time.Sleep(time.Second)
	r.write("apps/api/main.go", "v3")
	eventually(t, "api to restart", func() bool {
		sv := mustServer(t, s.State(), "api")
		return sv.Restarts >= 1 && sv.Status == StatusRunning && listening(api)
	})
	time.Sleep(r.grace + time.Second) // a second restart would wait out the grace again
	if sv := mustServer(t, s.State(), "api"); sv.Restarts != 1 || sv.LastRestartReason != "apps/api/main.go changed" {
		t.Fatalf("api = %d restart(s), reason %q; want one: the start after the stop already runs every change made during it", sv.Restarts, sv.LastRestartReason)
	}

	r.write("apps/api/late.go", "v2")
	eventually(t, "a change after the start to restart api", func() bool {
		sv := mustServer(t, s.State(), "api")
		return sv.Restarts == 2 && sv.LastRestartReason == "apps/api/late.go changed"
	})
}

func TestSupervisor_StateSaysWhyTheSupervisorLastRestartedAServer(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	api := freePort(t)
	r.write("apps/api/main.go", "package main")
	srv := r.listener("api", api)
	srv.Watch = &Watch{Paths: []string{"apps/api"}}
	r.declare(srv)
	s := r.supervisor()
	s.Boot()
	eventually(t, "api to listen", func() bool { return listening(api) })
	if sv := mustServer(t, s.State(), "api"); sv.LastRestartReason != "" {
		t.Fatalf("a first start has a restart reason: %q", sv.LastRestartReason)
	}

	r.write("apps/api/main.go", "package main // v2")
	eventually(t, "api to restart on the change", func() bool {
		return mustServer(t, s.State(), "api").LastRestartReason == "apps/api/main.go changed"
	})
	eventually(t, "api to listen again", func() bool { return listening(api) })

	if err := s.Restart("api"); err != nil {
		t.Fatal(err)
	}
	if got := mustServer(t, s.State(), "api").LastRestartReason; got != "a restart was asked for" {
		t.Fatalf("reason after ctl restart = %q", got)
	}
	st, err := ReadState(r.state)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustServer(t, st, "api").LastRestartReason; got != "a restart was asked for" {
		t.Fatalf("state.json reason = %q", got)
	}
}

func TestSupervisor_ControlSocketDrivesTheServers(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web := freePort(t)
	r.declare(r.listener("web", web))
	s := r.supervisor()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()
	t.Cleanup(func() { cancel() })
	eventually(t, "web to listen", func() bool { return listening(web) })
	call := func(req Request) Response {
		t.Helper()
		resp, err := Control(r.state, req, 30*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	if resp := call(Request{Verb: VerbStop, Name: "web"}); resp.Code != CodeOK || mustServer(t, resp.State, "web").Status != StatusStopped {
		t.Fatalf("stop: %+v", resp)
	}
	released(t, web)
	if resp := call(Request{Verb: VerbStop, Name: "web"}); resp.Code != CodeOK {
		t.Fatalf("a second stop is not idempotent: %+v", resp)
	}
	if resp := call(Request{Verb: VerbStart, Name: "web"}); resp.Code != CodeOK || mustServer(t, resp.State, "web").Restarts != 1 {
		t.Fatalf("start: %+v", resp)
	}
	if resp := call(Request{Verb: VerbStart, Name: "web"}); mustServer(t, resp.State, "web").Restarts != 1 {
		t.Fatalf("a second start restarted it: %+v", resp)
	}
	if resp := call(Request{Verb: VerbStatus, Name: "web"}); len(resp.State.Servers) != 1 {
		t.Fatalf("status web: %+v", resp)
	}
	if resp := call(Request{Verb: VerbStop, Name: "nope"}); resp.Code != CodeUnknownName {
		t.Fatalf("stop of an unknown server: %+v", resp)
	}
	if resp := call(Request{Verb: VerbStatus, Name: "nope"}); resp.Code != CodeUnknownName {
		t.Fatalf("status of an unknown server: %+v", resp)
	}

	crew := freePort(t)
	if err := os.MkdirAll(filepath.Join(r.root, ".crew", "ada"), 0o755); err != nil {
		t.Fatal(err)
	}
	resp := call(Request{Verb: VerbRun, Server: &Server{Name: "crew-ada", Cwd: ".crew/ada", Port: crew, Command: r.listener("crew-ada", crew).Command}})
	if sv := mustServer(t, resp.State, "crew-ada"); resp.Code != CodeOK || sv.Source != SourceRuntime || sv.Cwd != ".crew/ada" || sv.Port != crew || sv.Command == "" {
		t.Fatalf("run: %+v", resp)
	}
	eventually(t, "the crew app to listen", func() bool { return listening(crew) })
	if resp := call(Request{Verb: VerbRun, Server: &Server{Name: "web", Command: "true"}}); resp.Code != CodeFailed {
		t.Fatalf("a runtime entry replaced a declared server: %+v", resp)
	}
	if resp := call(Request{Verb: VerbRm, Name: "crew-ada"}); resp.Code != CodeOK {
		t.Fatalf("rm: %+v", resp)
	}
	released(t, crew)
	if _, ok := call(Request{Verb: VerbStatus}).State.Servers["crew-ada"]; ok {
		t.Fatal("rm kept the entry")
	}

	r.writeDeclaration(`{"version":2,"servers":[]}`)
	if resp := call(Request{Verb: VerbReload}); resp.Code != CodeInvalidDeclaration || !strings.Contains(resp.Error, "format 2") {
		t.Fatalf("reload of a newer declaration: %+v", resp)
	}

	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if resp, err := Control(r.state, Request{Verb: VerbStatus}, time.Second); !errors.Is(err, ErrNoSupervisor) || resp.Code != CodeNoSupervisor {
		t.Fatalf("control after shutdown: %+v %v", resp, err)
	}
	if listening(web) {
		t.Fatal("web outlived the supervisor's shutdown")
	}
}

func TestSupervisor_AWatchedChangeStartsACrashedServer(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	api := freePort(t)
	srv := r.listener("api", api)
	// It crashes until the fix lands: the change is the fix.
	srv.Command = "if [ -f apps/api/fixed ]; then " + srv.Command + "; else exit 3; fi"
	srv.Watch = &Watch{Paths: []string{"apps/api"}}
	r.write("apps/api/main.go", "package main")
	r.declare(srv)
	s := r.supervisor()
	s.Boot()
	eventually(t, "api to be recorded as crashed", func() bool {
		return mustServer(t, s.State(), "api").Status == StatusCrashed
	})

	r.write("apps/api/fixed", "1")
	eventually(t, "the change to start api", func() bool {
		sv := mustServer(t, s.State(), "api")
		return sv.Status == StatusRunning && sv.Restarts == 1 && listening(api)
	})
	if sv := mustServer(t, s.State(), "api"); sv.Exit == nil || sv.Exit.Code != 3 {
		t.Fatalf("the crash is no longer visible: %+v", sv.Exit)
	}
}

func TestSupervisor_AnAutostartFalseServerStaysStoppedUntilStarted(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web, admin, extra := freePort(t), freePort(t), freePort(t)
	adm := r.listener("admin", admin)
	adm.Autostart = autostart(false)
	r.declare(r.listener("web", web), adm)
	s := r.supervisor()
	s.Boot()
	eventually(t, "web to listen", func() bool { return listening(web) })
	if sv := mustServer(t, s.State(), "admin"); sv.Desired != DesiredStopped || sv.Status != StatusStopped || sv.Pid != 0 || listening(admin) {
		t.Fatalf("admin started on its own: %+v", sv)
	}

	// A name a reload adds gets its first desired state too.
	ext := r.listener("extra", extra)
	ext.Autostart = autostart(false)
	r.declare(r.listener("web", web), adm, ext)
	s.Reload()
	if sv := mustServer(t, s.State(), "extra"); sv.Status != StatusStopped || listening(extra) {
		t.Fatalf("a reload started an autostart:false name: %+v", sv)
	}

	// From then on only ctl changes desired: a start survives a container restart.
	if err := s.Start("admin"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "admin to listen", func() bool { return listening(admin) })
	s.Shutdown()
	again := r.supervisor()
	again.Boot()
	t.Cleanup(again.Shutdown)
	eventually(t, "admin to listen after a container restart", func() bool { return listening(admin) })
	if sv := mustServer(t, again.State(), "extra"); sv.Status != StatusStopped {
		t.Fatalf("extra = %+v, want stopped", sv)
	}
}

// Variant B (docs/spec-mate.md §2.8a): webdev and apidev mount one code
// volume, so both supervisors read one declaration; the stand-up writes it
// after the clone and reloads each.
func TestSupervisor_EachDevServicesSupervisorStartsOnlyTheServersAssignedToIt(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web := r.supervisorWith("webdev", 5*time.Second)
	apiState := shortDir(t)
	api, err := New(Config{Root: r.root, StateDir: apiState, Service: "apidev", Env: os.Environ(), Grace: time.Second, PortWait: 5 * time.Second, Version: "v-test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Shutdown)
	web.Boot()
	api.Boot()

	webPort, workerPort, apiPort := freePort(t), freePort(t), freePort(t)
	w, k, a := r.listener("web", webPort), r.listener("worker", workerPort), r.listener("api", apiPort)
	w.Service, k.Service, a.Service = "webdev", "webdev", "apidev"
	r.declare(w, k, a)
	web.Reload()
	api.Reload()

	eventually(t, "every server to listen", func() bool {
		return listening(webPort) && listening(workerPort) && listening(apiPort)
	})
	for sup, want := range map[*Supervisor][]string{web: {"web", "worker"}, api: {"api"}} {
		got := slices.Sorted(maps.Keys(sup.State().Servers))
		if !slices.Equal(got, want) {
			t.Errorf("supervisor runs %v, want %v", got, want)
		}
	}
}

// After a container replacement every server starts at once; two Maven
// wrappers unpacking one distribution race and the loser dies within seconds.
// One more start after its siblings settle brings it up; a server that keeps
// crashing at start stays down and visible.
func TestSupervisor_AServerThatCrashesInItsStartWindowIsStartedOnceMoreThenStaysDown(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.window = 300 * time.Millisecond
	port := freePort(t)
	flaky := r.listener("flaky", port)
	flaky.Command = "if [ -f .tried ]; then " + flaky.Command + "; else touch .tried; exit 1; fi"
	r.declare(flaky, Server{Name: "broken", Command: helperCmd(t, "exit", "CODE=3")})
	s := r.supervisor()
	s.Boot()

	eventually(t, "flaky to run after one more start", func() bool {
		sv := mustServer(t, s.State(), "flaky")
		return sv.Status == StatusRunning && sv.Restarts == 1 && listening(port)
	})
	if sv := mustServer(t, s.State(), "flaky"); sv.Exit == nil || sv.Exit.Code != 1 {
		t.Fatalf("the first crash is not visible: %+v", sv.Exit)
	}
	if got := mustServer(t, s.State(), "flaky").LastRestartReason; got != "it crashed within its start window" {
		t.Fatalf("flaky restart reason = %q", got)
	}
	eventually(t, "broken to crash on its second start too", func() bool {
		sv := mustServer(t, s.State(), "broken")
		return sv.Status == StatusCrashed && sv.Restarts == 1
	})
	time.Sleep(3 * r.window) // a third start that should not happen has no event to wait on
	if sv := mustServer(t, s.State(), "broken"); sv.Status != StatusCrashed || sv.Restarts != 1 || sv.Exit == nil || sv.Exit.Code != 3 {
		t.Fatalf("broken = %+v exit %+v, want crashed after one more start", sv, sv.Exit)
	}
}

// Four Spring services started at once; each Maven wrapper unpacked the same
// distribution for longer than the start window, all four crashed before they
// ever listened, and none came back. A crash before the server first answers
// on its port is a start crash however long the start took: it is started
// once more, one server at a time while a sibling is still starting. A server
// that crashed after it listened stays down.
func TestSupervisor_AServerThatCrashesBeforeItFirstListensIsStartedOnceMoreOneAtATime(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.window = 300 * time.Millisecond
	vets, visits, late := freePort(t), freePort(t), freePort(t)
	// The first start unpacks past the window and dies; a later start that
	// meets a sibling still unpacking (.unpack held) dies of the race again.
	maven := func(name string, port int) Server {
		srv := r.listener(name, port)
		srv.Command = "if [ -f .tried-" + name + " ]; then mkdir .unpack || exit 9; sleep 0.4; rmdir .unpack; " + srv.Command +
			"; else touch .tried-" + name + "; sleep 0.6; exit 1; fi"
		return srv
	}
	// It listens, then dies: no longer starting.
	crashLate := r.listener("late", late)
	crashLate.Command = "if [ -f .tried-late ]; then exit 4; fi; touch .tried-late; (sleep 1; kill $$) & " + crashLate.Command
	r.declare(maven("vets", vets), maven("visits", visits), crashLate)
	s := r.supervisor()
	s.Boot()

	for name, port := range map[string]int{"vets": vets, "visits": visits} {
		eventually(t, name+" to run after one more start", func() bool {
			sv := mustServer(t, s.State(), name)
			return sv.Status == StatusRunning && sv.Restarts == 1 && listening(port)
		})
		sv := mustServer(t, s.State(), name)
		if sv.Exit == nil || sv.Exit.Code != 1 || sv.LastRestartReason != "it crashed within its start window" {
			t.Fatalf("%s = %+v exit %+v, want its first crash visible and the retry named", name, sv, sv.Exit)
		}
	}
	eventually(t, "late to crash after it listened", func() bool {
		return mustServer(t, s.State(), "late").Status == StatusCrashed
	})
	time.Sleep(3 * r.window) // a start that should not happen has no event to wait on
	if sv := mustServer(t, s.State(), "late"); sv.Status != StatusCrashed || sv.Restarts != 0 {
		t.Fatalf("late = %+v, want it down: it crashed after it listened", sv)
	}
}

// A deploy reply called a server crashed and told the agent to fix it while
// the supervisor had already scheduled its one more start. While that start
// is pending — waiting for its window and for a sibling still starting — the
// state says so; once it starts, or when the retry was the start that crashed,
// it says nothing.
func TestSupervisor_AServerThatCrashedWhileStartingSaysItsOneMoreStartIsPendingUntilItStarts(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.window = 300 * time.Millisecond
	flakyPort, slowPort := freePort(t), freePort(t)
	flaky := r.listener("flaky", flakyPort)
	flaky.Command = "if [ -f .tried ]; then " + flaky.Command + "; else touch .tried; exit 1; fi"
	slow := r.listener("slow", slowPort)
	slow.Command = "sleep 2; " + slow.Command
	r.declare(flaky, slow, Server{Name: "broken", Command: helperCmd(t, "exit", "CODE=3")})
	s := r.supervisor()
	s.Boot()

	eventually(t, "flaky to be crashed with its one more start pending", func() bool {
		sv := mustServer(t, s.State(), "flaky")
		return sv.Status == StatusCrashed && sv.RetryPending
	})
	eventually(t, "flaky to run after its one more start", func() bool {
		sv := mustServer(t, s.State(), "flaky")
		return sv.Status == StatusRunning && sv.Restarts == 1 && listening(flakyPort)
	})
	if sv := mustServer(t, s.State(), "flaky"); sv.RetryPending {
		t.Fatalf("flaky = %+v, still says a start is pending after it started", sv)
	}
	eventually(t, "broken to crash on its one more start too", func() bool {
		sv := mustServer(t, s.State(), "broken")
		return sv.Status == StatusCrashed && sv.Restarts == 1
	})
	if sv := mustServer(t, s.State(), "broken"); sv.RetryPending {
		t.Fatalf("broken = %+v, says a start is pending after its one more start crashed", sv)
	}
	eventually(t, "state.json to hold broken crashed with nothing pending", func() bool {
		kept, err := ReadState(r.state)
		sv, ok := kept.Servers["broken"]
		return err == nil && ok && sv.Status == StatusCrashed && !sv.RetryPending
	})
}

// `uv run` rebuilds the repo's local packages, writing into the watched tree,
// on every start: each start restarted the server again, 900 times. A server
// whose every recent start changed its own watched files is no longer
// restarted by a change made during its start; a change after it still is.
func TestSupervisor_AServerThatChangesItsWatchedFilesOnEveryStartIsNotRestartedByThemForever(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.window = 2 * time.Second // a start, its write, the poll and the settle fit in it under -race
	r.write("apps/api/main.py", "v1")
	r.write("apps/api/pkg/PKG-INFO", "")
	srv := Server{Name: "api", Command: "sleep 0.2; echo $$ > apps/api/pkg/PKG-INFO; exec sleep 300", Watch: &Watch{Paths: []string{"apps/api"}}}
	r.declare(srv)
	s := r.supervisor()
	s.Boot()

	eventually(t, "the self-caused restarts to stop", func() bool {
		return mustServer(t, s.State(), "api").Restarts == watchLoopLimit
	})
	time.Sleep(r.window + 4*watchSettle) // a restart that should not happen has no event to wait on
	sv := mustServer(t, s.State(), "api")
	if sv.Restarts != watchLoopLimit || sv.Status != StatusRunning || sv.Pid == 0 {
		t.Fatalf("api = %+v, want running after %d restarts", sv, watchLoopLimit)
	}
	if want := "restarts stopped: apps/api/pkg/PKG-INFO changed during each of its last 10 starts; add the path to watch.ignore"; sv.LastRestartReason != want {
		t.Fatalf("reason = %q, want %q", sv.LastRestartReason, want)
	}

	r.write("apps/api/main.py", "v2")
	eventually(t, "a change after the start window to restart api", func() bool {
		return mustServer(t, s.State(), "api").Restarts == watchLoopLimit+1
	})
	time.Sleep(r.window + 4*watchSettle)
	if sv := mustServer(t, s.State(), "api"); sv.Restarts != watchLoopLimit+1 || sv.Status != StatusRunning {
		t.Fatalf("api = %+v, want one restart for the edit", sv)
	}
}

// A change is the server's own start output by when it was made, never by
// when its burst settled: the settle (watchSettle of quiet) ends after the
// last change, so a write made just inside the start window is decided after
// the window closed. Decided by the time the restart ran, a server that writes
// its watched files 9.7 s into each start never reached watchLoopLimit and was
// restarted every 10 s, forever.
func TestSupervisor_AChangeMadeInTheStartWindowCountsThoughItsBurstSettlesAfterIt(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.window = 2 * time.Second
	r.write("apps/api/main.py", "v1")
	r.declare(Server{Name: "api", Command: "exec sleep 300", Watch: &Watch{Paths: []string{"apps/api"}}})
	s := r.supervisor()
	s.Boot()
	eventually(t, "api to run", func() bool {
		sv := mustServer(t, s.State(), "api")
		return sv.Status == StatusRunning && sv.Pid != 0
	})
	before := mustServer(t, s.State(), "api").Restarts

	// Each of its last watchLoopLimit starts saw a change of its own; this
	// start began a window and a settle ago, and its change came just
	// inside the window — its burst settled after it.
	started := time.Now().Add(-r.window - watchSettle)
	s.mu.Lock()
	sv := s.servers["api"]
	sv.startBegun, sv.startedAt = started.Add(-time.Millisecond), &started
	sv.selfChanged, sv.windowChanged = watchLoopLimit, false
	s.mu.Unlock()
	s.watchFired("api", "apps/api/pkg/PKG-INFO", started.Add(r.window-watchSettle/2))

	got := mustServer(t, s.State(), "api")
	if got.Restarts != before {
		t.Fatalf("api restarted (%d → %d): a change made inside its start window was taken for one after it", before, got.Restarts)
	}
	if want := "restarts stopped: apps/api/pkg/PKG-INFO changed during each of its last 10 starts; add the path to watch.ignore"; got.LastRestartReason != want {
		t.Fatalf("reason = %q, want %q", got.LastRestartReason, want)
	}
}

// limitRig is a rig whose service's task limit (300) is reached each time a
// server appends a line to .limit in the repo root: the kernel's pids.events
// "max" count, which grows each time a fork is refused at pids.max.
func limitRig(t *testing.T) *rig {
	t.Helper()
	r := newRig(t)
	r.tasks = func() (Tasks, bool) {
		b, _ := os.ReadFile(filepath.Join(r.root, ".limit"))
		return Tasks{Max: 300, Current: 297, Refused: int64(strings.Count(string(b), "\n"))}, true
	}
	return r
}

// After a deploy every dev server crashed at once with EAGAIN — uv's rayon
// pool, pnpm, fork/exec of /bin/sh — because together they reached the
// service's task limit, and each stayed down: its one more start crashed the
// same way. A server whose crash came with the limit reached is started again
// with backoff, one at a time, and that is not its one more start; while it
// waits the state names the limit and the tasks running now.
func TestSupervisor_AServerThatCrashedWhenTheServicesTaskLimitWasReachedIsStartedAgainWithBackoffWithoutSpendingItsOneMoreStart(t *testing.T) {
	t.Parallel()
	r := limitRig(t)
	r.window = 300 * time.Millisecond
	port := freePort(t)
	api := r.listener("api", port)
	// Three starts meet the limit, the fourth runs.
	api.Command = "n=$(cat .starts 2>/dev/null | wc -l); echo s >> .starts; if [ $n -lt 3 ]; then echo refused >> .limit; exit 1; fi; " + api.Command
	r.declare(api)
	s := r.supervisor()
	s.Boot()

	eventually(t, "api to be crashed on the task limit with a start pending", func() bool {
		sv := mustServer(t, s.State(), "api")
		return sv.Status == StatusCrashed && sv.RetryPending && sv.TaskLimit != nil
	})
	if sv := mustServer(t, s.State(), "api"); *sv.TaskLimit != (TaskLimit{Max: 300, Current: 297}) {
		t.Fatalf("api taskLimit = %+v, want the limit 300 and the 297 tasks running now", *sv.TaskLimit)
	}
	eventually(t, "api to run after its starts on the task limit", func() bool {
		sv := mustServer(t, s.State(), "api")
		return sv.Status == StatusRunning && sv.Restarts == 3 && listening(port)
	})
	sv := mustServer(t, s.State(), "api")
	if sv.TaskLimit != nil || sv.RetryPending {
		t.Fatalf("api = %+v, still names the task limit or a pending start while it runs", sv)
	}
	if sv.LastRestartReason != "the service's task limit (300) was reached" {
		t.Fatalf("api lastRestartReason = %q, want the task limit named", sv.LastRestartReason)
	}
}

// The backoff is bounded: a server that meets the task limit on every start
// stops being started again and stays down, still naming the limit.
func TestSupervisor_AServerThatMeetsTheTaskLimitOnEveryStartStopsBeingStartedAndNamesTheLimit(t *testing.T) {
	t.Parallel()
	r := limitRig(t)
	r.window = 300 * time.Millisecond
	r.declare(Server{Name: "worker", Command: "echo refused >> .limit; exit 1"})
	s := r.supervisor()
	s.Boot()

	eventually(t, "worker to stay down after its bounded starts", func() bool {
		sv := mustServer(t, s.State(), "worker")
		return sv.Status == StatusCrashed && !sv.RetryPending && sv.Restarts == maxLimitStarts
	})
	time.Sleep(time.Second) // a start that should not happen has no event to wait on
	sv := mustServer(t, s.State(), "worker")
	if sv.Restarts != maxLimitStarts || sv.TaskLimit == nil || sv.TaskLimit.Max != 300 {
		t.Fatalf("worker = %+v, want %d restarts and the task limit named", sv, maxLimitStarts)
	}
}

// Servers started together are started a gap apart, so N servers do not fork
// their toolchains at the same moment.
func TestSupervisor_ServersStartedTogetherStartAGapApart(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.stagger = 400 * time.Millisecond
	r.declare(
		Server{Name: "a", Command: "sleep 300"},
		Server{Name: "b", Command: "sleep 300"},
		Server{Name: "c", Command: "sleep 300"},
	)
	s := r.supervisor()
	s.Boot()

	st := s.State()
	at := make([]time.Time, 0, 3)
	for _, name := range []string{"a", "b", "c"} {
		sv := mustServer(t, st, name)
		if sv.StartedAt == nil || sv.Status != StatusRunning {
			t.Fatalf("%s = %+v, want it started", name, sv)
		}
		at = append(at, *sv.StartedAt)
	}
	slices.SortFunc(at, func(x, y time.Time) int { return x.Compare(y) })
	for i := 1; i < len(at); i++ {
		if gap := at[i].Sub(at[i-1]); gap < 350*time.Millisecond {
			t.Fatalf("starts %v apart, want at least the 400 ms gap: %v", gap, at)
		}
	}
}

// The kernel's OOM killer killed a dev server's process; the platform unit's
// OOMPolicy=stop made systemd stop the unit, every dev server with it, and
// start it again, and each server's restart reason said only "the supervisor
// started again". The new supervisor reads how the previous run of its unit
// ended and says on every server it started that the service ran out of
// memory and systemd restarted them all, until that server starts again for
// another reason.
func TestSupervisor_AfterSystemdRestartedTheServiceBecauseItRanOutOfMemoryEachServerSaysSo(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web, api := freePort(t), freePort(t)
	r.declare(r.listener("web", web), r.listener("api", api))
	s1 := r.supervisor()
	s1.Boot()
	eventually(t, "both to listen", func() bool { return listening(web) && listening(api) })
	s1.Shutdown() // systemd stops the unit on the OOM kill

	r.previousRun = func() (string, bool) { return "oom-kill", true }
	s2 := r.supervisor()
	s2.Boot()
	eventually(t, "both to listen again", func() bool { return listening(web) && listening(api) })

	const want = "the service ran out of memory: the kernel killed one of its processes and systemd restarted every dev server"
	st, err := ReadState(r.state)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"web", "api"} {
		if sv := mustServer(t, st, name); sv.LastRestartReason != want || !sv.OutOfMemory {
			t.Fatalf("%s lastRestartReason/outOfMemory = %q/%v, want %q/true", name, sv.LastRestartReason, sv.OutOfMemory, want)
		}
	}

	if err := s2.Restart("api"); err != nil {
		t.Fatal(err)
	}
	st = s2.State()
	if sv := mustServer(t, st, "api"); sv.OutOfMemory || sv.LastRestartReason != "a restart was asked for" {
		t.Fatalf("api after ctl restart = %q/%v, want the asked restart and no out-of-memory mark", sv.LastRestartReason, sv.OutOfMemory)
	}
	if sv := mustServer(t, st, "web"); !sv.OutOfMemory || sv.LastRestartReason != want {
		t.Fatalf("web = %q/%v, lost the out-of-memory restart when api restarted", sv.LastRestartReason, sv.OutOfMemory)
	}
}

// A previous run that ended for any other reason, or one the journal does not
// tell, is no out-of-memory restart.
func TestSupervisor_AfterAnyOtherEndOfThePreviousRunTheSupervisorStartedAgain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result string
		ok     bool
	}{
		{"its main process exited with an error", "exit-code", true},
		{"the journal cannot be read", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			api := freePort(t)
			r.declare(r.listener("api", api))
			s1 := r.supervisor()
			s1.Boot()
			eventually(t, "api to listen", func() bool { return listening(api) })
			s1.Shutdown()

			r.previousRun = func() (string, bool) { return tt.result, tt.ok }
			s2 := r.supervisor()
			s2.Boot()
			eventually(t, "api to listen again", func() bool { return listening(api) })
			if sv := mustServer(t, s2.State(), "api"); sv.OutOfMemory || sv.LastRestartReason != "the supervisor started again" {
				t.Fatalf("api = %q/%v, want the supervisor started again and no out-of-memory mark", sv.LastRestartReason, sv.OutOfMemory)
			}
		})
	}
}
