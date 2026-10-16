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
	s, err := New(Config{Root: r.root, StateDir: r.state, Service: service, Env: os.Environ(), Grace: time.Second, PortWait: portWait, Version: "v-test"})
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
	r.declare(Server{Name: "api", Command: helperCmd(t, "exit", "CODE=3")})
	s := r.supervisor()
	s.Boot()

	eventually(t, "api to be recorded as crashed", func() bool {
		return mustServer(t, s.State(), "api").Status == StatusCrashed
	})
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
