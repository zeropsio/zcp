//go:build linux || darwin

package devsup

import (
	"maps"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// printedNames are the variables an env server prints at each start: HQ_HOST
// is set by the service and by the overlay, KEEP by the service only.
const printedNames = "HQ_HOST,KEEP"

// envServer is a listener that first prints printedNames as it sees them.
func (r *rig) envServer(name string, port int) Server {
	return Server{Name: name, Port: port, Command: helperCmd(r.t, "env", "PRINT_ENV="+printedNames, "PORT="+strconv.Itoa(port))}
}

// lastStart is what server name printed since its last start began.
func lastStart(dir, name string) string {
	b, _ := os.ReadFile(LogPath(dir, name))
	starts := strings.Split(string(b), "--- zcp-devsup: start #")
	return starts[len(starts)-1]
}

// startedWith waits until server name's last start printed every line.
func startedWith(t *testing.T, dir, name string, lines ...string) {
	t.Helper()
	eventually(t, name+" to start with "+strings.Join(lines, ", "), func() bool {
		got := lastStart(dir, name)
		for _, l := range lines {
			if !strings.Contains(got, l+"\n") {
				return false
			}
		}
		return true
	})
}

// A lease's dev servers reach their sibling parts through the overlay zcp
// sets: every start the supervisor makes carries it, an overlay entry winning
// over the service's own value, the service's other variables kept.
func TestSupervisor_EnvOverlay_AppliedAtEveryStart(t *testing.T) {
	t.Parallel()
	overlay := map[string]string{"HQ_HOST": "node-id-7.runtime.hqpool.zerops"}
	want := []string{"env HQ_HOST=node-id-7.runtime.hqpool.zerops", "env KEEP=from-service"}
	tests := []struct {
		name string
		// declare runs before the supervisor boots; start after the overlay is set.
		declare func(r *rig, port int)
		start   func(t *testing.T, r *rig, s *Supervisor, port int)
	}{
		{
			name: "ctl start of a stopped server",
			declare: func(r *rig, port int) {
				web := r.envServer("web", port)
				web.Autostart = autostart(false)
				r.declare(web)
			},
			start: func(t *testing.T, _ *rig, s *Supervisor, _ int) {
				t.Helper()
				if err := s.Start("web"); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:    "a reload that adds the server",
			declare: func(r *rig, _ int) { r.declare() },
			start: func(t *testing.T, r *rig, s *Supervisor, port int) {
				t.Helper()
				r.declare(r.envServer("web", port))
				s.Reload()
			},
		},
		{
			name:    "ctl run of a runtime entry",
			declare: func(r *rig, _ int) { r.declare() },
			start: func(t *testing.T, r *rig, s *Supervisor, port int) {
				t.Helper()
				if err := s.RunEntry(r.envServer("web", port)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "a watched change that restarts the server",
			declare: func(r *rig, port int) {
				web := r.envServer("web", port)
				web.Autostart = autostart(false)
				web.Watch = &Watch{Paths: []string{"apps/web"}}
				r.write("apps/web/index.ts", "v1")
				r.declare(web)
			},
			start: func(t *testing.T, r *rig, s *Supervisor, port int) {
				t.Helper()
				if err := s.Start("web"); err != nil {
					t.Fatal(err)
				}
				eventually(t, "web to listen", func() bool { return listening(port) })
				r.write("apps/web/index.ts", "v2")
				eventually(t, "the change to restart web", func() bool {
					return mustServer(t, s.State(), "web").LastRestartReason == "apps/web/index.ts changed"
				})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			r.env = []string{"HQ_HOST=from-service", "KEEP=from-service"}
			port := freePort(t)
			tt.declare(r, port)
			s := r.supervisor()
			s.Boot()

			if err := s.SetEnv(overlay); err != nil {
				t.Fatal(err)
			}
			tt.start(t, r, s, port)

			eventually(t, "web to listen", func() bool { return listening(port) })
			startedWith(t, r.state, "web", want...)
		})
	}
}

// zcp re-sends the overlay before every start; only a change restarts what
// runs, each running server once, and a server that is not running is not
// started by it.
func TestSupervisor_EnvOverlay_ChangeRestartsRunningServersOnce(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web, api, admin := freePort(t), freePort(t), freePort(t)
	adm := r.envServer("admin", admin)
	adm.Autostart = autostart(false)
	r.declare(r.envServer("web", web), r.envServer("api", api), adm, Server{Name: "job", Command: helperCmd(t, "exit", "CODE=1")})
	s := r.supervisor()
	s.Boot()
	eventually(t, "web and api to listen and job to crash", func() bool {
		return listening(web) && listening(api) && mustServer(t, s.State(), "job").Status == StatusCrashed
	})
	startedWith(t, r.state, "web", "env HQ_HOST unset")
	before := s.State()

	for i, host := range []string{"node-id-7.runtime.hqpool.zerops", "node-id-9.runtime.hqpool.zerops"} {
		if err := s.SetEnv(map[string]string{"HQ_HOST": host}); err != nil {
			t.Fatal(err)
		}
		for _, name := range []string{"web", "api"} {
			eventually(t, name+" to restart under "+host, func() bool {
				sv := mustServer(t, s.State(), name)
				return sv.Restarts == i+1 && sv.Status == StatusRunning && sv.LastRestartReason == "env changed"
			})
			startedWith(t, r.state, name, "env HQ_HOST="+host)
		}
	}
	time.Sleep(500 * time.Millisecond) // a restart that should not happen has no event to wait on

	after := s.State()
	for _, name := range []string{"web", "api"} {
		if sv := mustServer(t, after, name); sv.Restarts != 2 || sv.Pid == mustServer(t, before, name).Pid {
			t.Errorf("%s restarted %d times for two changes: %+v", name, sv.Restarts, sv)
		}
	}
	if sv := mustServer(t, after, "admin"); sv.Status != StatusStopped || sv.StartedAt != nil || listening(admin) {
		t.Errorf("the overlay started a stopped server: %+v", sv)
	}
	if sv := mustServer(t, after, "job"); sv.Status != StatusCrashed || sv.Restarts != 0 {
		t.Errorf("the overlay started a crashed server: %+v", sv)
	}
	kept, err := ReadState(r.state)
	if err != nil {
		t.Fatal(err)
	}
	if !maps.Equal(kept.Env, map[string]string{"HQ_HOST": "node-id-9.runtime.hqpool.zerops"}) {
		t.Errorf("state.json env = %v", kept.Env)
	}
}

func TestSupervisor_EnvOverlay_SameOverlay_NoRestart(t *testing.T) {
	t.Parallel()
	hq := map[string]string{"HQ_HOST": "node-id-7.runtime.hqpool.zerops", "API_HOST": "node-id-2.runtime.apipool.zerops"}
	tests := []struct {
		name  string
		first map[string]string // set before the servers settle; nil sets none
		again map[string]string
	}{
		{name: "the same overlay again", first: hq, again: maps.Clone(hq)},
		{name: "an empty overlay where none is set", again: map[string]string{}},
		{name: "no overlay where none is set", again: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			s := r.supervisor()
			s.env = maps.Clone(tt.first)
			restarts := 0
			if tt.first != nil {
				restarts = 1
			}
			s.servers["web"] = &server{spec: Server{Name: "web"}, desired: DesiredRunning, starts: restarts + 1}
			pid := mustServer(t, s.State(), "web").Pid

			if err := s.SetEnv(tt.again); err != nil {
				t.Fatal(err)
			}
			// Equality returns synchronously, without scheduling the restart worker.
			if s.envRestarting {
				t.Fatal("an unchanged overlay scheduled a restart")
			}

			if sv := mustServer(t, s.State(), "web"); sv.Restarts != restarts || sv.Pid != pid || sv.Status != StatusRunning {
				t.Fatalf("an unchanged overlay restarted web: %+v", sv)
			}
		})
	}
}

// The overlay lives in $HOME with the rest of the state: a container restart
// starts every server under it again, while a reset or a redeploy (a fresh
// $HOME) starts without it until zcp sends it again.
func TestSupervisor_EnvOverlay_SurvivesRestart(t *testing.T) {
	t.Parallel()
	overlay := map[string]string{"HQ_HOST": "node-id-7.runtime.hqpool.zerops"}
	tests := []struct {
		name      string
		freshHome bool
		wantEnv   map[string]string
		wantLine  string
	}{
		{name: "a container restart keeps it", wantEnv: overlay, wantLine: "env HQ_HOST=node-id-7.runtime.hqpool.zerops"},
		{name: "a reset or a redeploy empties it", freshHome: true, wantEnv: map[string]string{}, wantLine: "env HQ_HOST unset"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRig(t)
			web := freePort(t)
			r.declare(r.envServer("web", web))
			s1 := r.supervisor()
			s1.Boot()
			eventually(t, "web to listen", func() bool { return listening(web) })
			if err := s1.SetEnv(overlay); err != nil {
				t.Fatal(err)
			}
			startedWith(t, r.state, "web", "env HQ_HOST=node-id-7.runtime.hqpool.zerops")
			s1.Shutdown()
			released(t, web)

			if tt.freshHome {
				r.state = shortDir(t)
			}
			s2 := r.supervisor()
			s2.Boot()

			eventually(t, "web to listen again", func() bool { return listening(web) })
			startedWith(t, r.state, "web", tt.wantLine)
			if got := s2.State().Env; !maps.Equal(got, tt.wantEnv) || got == nil {
				t.Fatalf("env = %#v, want %#v", got, tt.wantEnv)
			}
		})
	}
}

// siblingEnv is zcp's to read: the supervisor never puts it into a server's
// environment — only an overlay set through ctl env does.
func TestSupervisor_EnvOverlay_DeclaredSiblingEnvNeverReachesAServer(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	web := freePort(t)
	r.writeDeclaration(`{"version":1,"siblingEnv":{"HQ_HOST":"hqpool"},"servers":[{"name":"web","port":` +
		strconv.Itoa(web) + `,"command":` + strconv.Quote(r.envServer("web", web).Command) + `}]}`)
	s := r.supervisor()
	s.Boot()

	eventually(t, "web to listen", func() bool { return listening(web) })
	startedWith(t, r.state, "web", "env HQ_HOST unset")
	if st := s.State(); st.Declaration.Error != "" || len(st.Env) != 0 {
		t.Fatalf("declaration %+v env %v", st.Declaration, st.Env)
	}
}

func TestSupervisor_EnvOverlay_RejectsReservedAndMalformedKeys(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	r.declare()
	s := r.supervisor()
	s.Boot()
	kept := map[string]string{"HQ_HOST": "node-id-7.runtime.hqpool.zerops"}
	if err := s.SetEnv(kept); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"ZEROPS_StableNumber": "9"}, `"ZEROPS_StableNumber"`},
		{map[string]string{"hostname": "x"}, `"hostname"`},
		{map[string]string{"serviceId": "x"}, `"serviceId"`},
		{map[string]string{"projectId": "x"}, `"projectId"`},
		{map[string]string{"PATH": "/tmp"}, `"PATH"`},
		{map[string]string{"HOME": "/tmp"}, `"HOME"`},
		{map[string]string{"1HQ": "x"}, `"1HQ"`},
		{map[string]string{"HQ-HOST": "x"}, `"HQ-HOST"`},
		{map[string]string{"": "x"}, `""`},
		{map[string]string{"HQ_HOST": "a\x00b"}, `"HQ_HOST"`},
		{map[string]string{"API_HOST": "fine", "PATH": "/tmp"}, `"PATH"`},
	} {
		err := s.SetEnv(tc.env)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("SetEnv(%q) = %v, want an error naming %s", tc.env, err, tc.want)
		}
	}
	if got := s.State().Env; !maps.Equal(got, kept) {
		t.Fatalf("a refused overlay changed it: %v", got)
	}
}
