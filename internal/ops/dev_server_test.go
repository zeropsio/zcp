// Tests for: internal/ops/dev_server.go, dev_server_supervisor.go — the
// dev-server lifecycle on top of a dev service's supervisor (zcp-devsup),
// driven over SSH against devservertest.Container, a fake dev container that
// answers like one running the supervisor (docs/spec-mate.md §2.8a).
package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// disableNoProbeSettleForTest zeroes the no-probe settle interval so
// unit tests don't wait 3s per case for init-time crashes. Uses
// atomic.Int64 under the hood (storeNoProbeSettle) so parallel tests don't
// data-race the settle-interval reader. Restored via t.Cleanup.
//
// non-parallel: callers override the package's settle interval, so they run
// before the parallel tests are released.
func disableNoProbeSettleForTest(t *testing.T) {
	t.Helper()
	orig := storeNoProbeSettle(0)
	t.Cleanup(func() { storeNoProbeSettle(orig) })
}

// mockClientWithServices returns a platform mock that reports the given
// hostnames as existing services — enough for verifyDevServerTarget to
// pass.
func mockClientWithServices(hostnames ...string) platform.Client {
	svcs := make([]platform.ServiceStack, 0, len(hostnames))
	for _, h := range hostnames {
		svcs = append(svcs, platform.ServiceStack{
			ID:        "svc-" + h,
			Name:      h, // Name IS the hostname on ServiceStack.
			ProjectID: "p1",
			Status:    "ACTIVE",
		})
	}
	return platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test"}).
		WithServices(svcs)
}

// twoServers declares web (:3000) and api (:8080) on appdev.
const twoServers = `{"version":1,"servers":[` +
	`{"name":"web","port":3000,"command":"pnpm dev","cwd":"apps/web"},` +
	`{"name":"api","port":8080,"command":"pnpm dev","cwd":"apps/api"}]}`

// oneServer declares apidev's one server, named after the dev service.
const oneServer = `{"version":1,"servers":[{"name":"apidev","port":3000,"command":"npm run start:dev","cwd":"."}]}`

func runDevServer(t *testing.T, c *devservertest.Container, p DevServerParams) *DevServerResult {
	t.Helper()
	result, err := ExecuteDevServer(context.Background(), c, mockClientWithServices(p.Hostname), "p1", p)
	if err != nil {
		t.Fatalf("ExecuteDevServer(%s): %v", p.Action, err)
	}
	return result
}

// sentWith counts the commands that contain every part.
func sentWith(c *devservertest.Container, parts ...string) int {
	n := 0
	for _, cmd := range c.Sent() {
		all := true
		for _, p := range parts {
			all = all && strings.Contains(cmd, p)
		}
		if all {
			n++
		}
	}
	return n
}

func TestDevServer_Start_Success(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised("")
	c.Logs = map[string][]string{"apidev": {"starting...", "ok"}}

	result := runDevServer(t, c, DevServerParams{
		Action:     "start",
		Hostname:   "apidev",
		Command:    "npm run start:dev",
		Port:       3000,
		HealthPath: "/api/health",
	})
	if !result.Running {
		t.Errorf("expected Running=true, got %+v", result)
	}
	if result.HealthStatus != 200 {
		t.Errorf("expected HealthStatus=200, got %d", result.HealthStatus)
	}
	if result.StartMillis != 123 {
		t.Errorf("expected StartMillis=123, got %d", result.StartMillis)
	}
	if result.LogTail == "" {
		t.Errorf("expected non-empty LogTail")
	}
	// B9: URL is the consumer-vantage (hostname) address, and the message must
	// NOT claim the server "responded at localhost" (the probe ran localhost
	// INSIDE the container — a loopback-only bind passes it but refuses
	// hostname traffic, so claiming the hostname responded would be a tell≠check).
	if result.URL != "http://apidev:3000/api/health" {
		t.Errorf("URL = %q, want http://apidev:3000/api/health", result.URL)
	}
	if strings.Contains(result.Message, "http://localhost") {
		t.Errorf("success message must not present a localhost URL as the agent's reachable address: %q", result.Message)
	}
	if !strings.Contains(result.Message, "http://apidev:3000/api/health") {
		t.Errorf("success message must hand the hostname-vantage URL: %q", result.Message)
	}
	// The server is named after its dev service, so the result does not name it.
	if result.Name != "" {
		t.Errorf("Name = %q, want empty for the server named after the dev service", result.Name)
	}
	if result.LogFile != "/home/zerops/.zcp/devsup/logs/apidev.log" || result.PID == 0 {
		t.Errorf("LogFile/PID = %q/%d, want the supervisor's log path and the group's pid", result.LogFile, result.PID)
	}
	d, err := devsup.ParseDeclaration([]byte(c.Declaration))
	if err != nil {
		t.Fatalf("declaration written is invalid: %v\n%s", err, c.Declaration)
	}
	want := devsup.Server{Name: "apidev", Port: 3000, Command: "npm run start:dev", Cwd: "."}
	if len(d.Servers) != 1 || !d.Servers[0].SameRun(want) || d.Servers[0].Port != 3000 {
		t.Errorf("declaration = %+v, want one server %+v", d.Servers, want)
	}
	if c.Status("apidev") != devsup.StatusRunning {
		t.Errorf("supervisor status = %q, want running", c.Status("apidev"))
	}
}

// TestDevServer_Start_ProbeOKButSpawnedPidDead_PortInUse pins F3: when the HTTP
// probe passes but the server's own process group is gone (a stale/foreign
// listener owns the port), the tool must report running:false reason=port_in_use
// instead of trusting the foreign 2xx. Reproduces the p.txt #2 EADDRINUSE
// false-positive (running:true + healthStatus:200 next to an EADDRINUSE log).
func TestDevServer_Start_ProbeOKButSpawnedPidDead_PortInUse(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised("")
	c.Foreign = map[int]int{3000: 200}
	c.Logs = map[string][]string{"apidev": {"Error: listen EADDRINUSE: address already in use :::3000"}}

	result := runDevServer(t, c, DevServerParams{Action: "start", Hostname: "apidev", Command: "npm run start:dev", Port: 3000})
	if result.Running {
		t.Errorf("Running = true while another listener answers the port: %+v", result)
	}
	if result.Reason != reasonPortInUse {
		t.Errorf("Reason = %q, want %q", result.Reason, reasonPortInUse)
	}
	if !strings.Contains(result.LogTail, "EADDRINUSE") {
		t.Errorf("LogTail = %q, want the server's EADDRINUSE", result.LogTail)
	}
}

func TestDevServer_Start_HealthProbeFails(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised("")
	c.HTTP = map[int]int{3000: 0}
	c.Logs = map[string][]string{"apidev": {"Error: listen EADDRINUSE ::: 3000"}}

	result := runDevServer(t, c, DevServerParams{
		Action:      "start",
		Hostname:    "apidev",
		Command:     "npm run start:dev",
		Port:        3000,
		HealthPath:  "/api/health",
		WaitSeconds: 5,
	})
	if result.Running {
		t.Errorf("expected Running=false, got %+v", result)
	}
	if !strings.Contains(result.Reason, "connection_refused") {
		t.Errorf("expected reason to classify as connection_refused, got %q", result.Reason)
	}
	if !strings.Contains(result.LogTail, "EADDRINUSE") {
		t.Errorf("expected LogTail to carry EADDRINUSE, got %q", result.LogTail)
	}
}

// TestDevServer_Start_MissingCommand: the first start of a server must name
// its command; nothing is declared or started without one.
func TestDevServer_Start_MissingCommand(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised("")
	_, err := ExecuteDevServer(context.Background(), c, mockClientWithServices("apidev"), "p1",
		DevServerParams{
			Action:   "start",
			Hostname: "apidev",
			Port:     3000,
		})
	if err == nil {
		t.Fatal("expected error for missing command")
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T", err)
	}
	if pe.Code != platform.ErrInvalidParameter {
		t.Errorf("expected ErrInvalidParameter, got %s", pe.Code)
	}
	if c.Declaration != "" || sentWith(c, "zcp-devsup ctl 'start'") != 0 {
		t.Errorf("a start without a command declared %q or started a server", c.Declaration)
	}
}

// TestDevServer_AcceptsEnvPrefixForm pins the positive case: `env KEY=VAL cmd`
// starts like any other command.
func TestDevServer_AcceptsEnvPrefixForm(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised("")
	result := runDevServer(t, c, DevServerParams{
		Action:   "start",
		Hostname: "apidev",
		Command:  "env PYTHONPATH=/var/www/vendor gunicorn app:app",
		Port:     3000,
	})
	if !result.Running {
		t.Errorf("env KEY=VAL form did not start: %+v", result)
	}
}

func TestDevServer_Start_InvalidHostname(t *testing.T) {
	t.Parallel()

	cases := []string{
		"APIDEV",                       // uppercase
		"api dev",                      // space
		"api;rm -rf /",                 // shell injection
		"",                             // empty
		"a" + strings.Repeat("b", 128), // too long
	}
	for _, hostname := range cases {
		t.Run(hostname, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised("")
			_, err := ExecuteDevServer(context.Background(), c, nil, "p1",
				DevServerParams{
					Action:   "start",
					Hostname: hostname,
					Command:  "npm run dev",
					Port:     3000,
				})
			if err == nil {
				t.Errorf("expected error for hostname %q", hostname)
			}
			if n := len(c.Sent()); n != 0 {
				t.Errorf("hostname %q: expected 0 ssh calls, got %d", hostname, n)
			}
		})
	}
}

func TestDevServer_Start_ServiceNotFound(t *testing.T) {
	t.Parallel()

	_, err := ExecuteDevServer(context.Background(), devservertest.NewSupervised(""), mockClientWithServices("other"), "p1",
		DevServerParams{
			Action:   "start",
			Hostname: "apidev",
			Command:  "npm run dev",
			Port:     3000,
		})
	if err == nil {
		t.Fatal("expected error for unknown hostname")
	}
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("expected PlatformError, got %T", err)
	}
	if pe.Code != platform.ErrServiceNotFound {
		t.Errorf("expected ErrServiceNotFound, got %s", pe.Code)
	}
}

// TestDevServer_StopOneNamed_SiblingsKeepRunning (TEST-PLAN E1 #2): stopping
// one named server leaves its siblings running, and the answer says which.
func TestDevServer_StopOneNamed_SiblingsKeepRunning(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(twoServers)

	result := runDevServer(t, c, DevServerParams{Action: "stop", Hostname: "appdev", Name: "web"})
	if c.Status("web") != devsup.StatusStopped || c.Status("api") != devsup.StatusRunning {
		t.Errorf("after stopping web: web=%s api=%s, want stopped/running", c.Status("web"), c.Status("api"))
	}
	if result.Running || result.Name != "web" {
		t.Errorf("result = %+v, want web not running", result)
	}
	if !strings.Contains(result.Message, "Still running: api.") {
		t.Errorf("message = %q, want it to name the sibling still running", result.Message)
	}
	if sentWith(c, "zcp-devsup ctl 'stop' 'web'") != 1 {
		t.Errorf("want exactly one supervisor stop of web, sent %q", c.Sent())
	}
}

// TestDevServer_SeveralDeclared_NameRequired: a dev service with several
// servers, none named after it, needs the name for stop/status/logs.
func TestDevServer_SeveralDeclared_NameRequired(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(twoServers)
	_, err := ExecuteDevServer(context.Background(), c, mockClientWithServices("appdev"), "p1",
		DevServerParams{Action: "stop", Hostname: "appdev"})
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || !strings.Contains(pe.Suggestion, "web (running), api (running)") {
		t.Fatalf("err = %v, want a request to name one of the declared servers", err)
	}
	if c.Status("web") != devsup.StatusRunning || c.Status("api") != devsup.StatusRunning {
		t.Error("an unnamed stop stopped a server")
	}
}

// TestDevServer_UndeclaredStart_DeclaredOnce (TEST-PLAN E1 #5): starting an
// undeclared server adds it to .zcp/dev-servers.json once; start and stop of
// a declared one never rewrite the declaration.
func TestDevServer_UndeclaredStart_DeclaredOnce(t *testing.T) {
	disableNoProbeSettleForTest(t)
	c := devservertest.NewSupervised(twoServers)
	c.Logs = map[string][]string{"worker": {"tick"}}

	runDevServer(t, c, DevServerParams{Action: "start", Hostname: "appdev", Name: "worker", Command: "bun run worker.ts", WorkDir: "/var/www/apps/worker", NoHTTPProbe: true})
	d, err := devsup.ParseDeclaration([]byte(c.Declaration))
	if err != nil || len(d.Servers) != 3 || d.Servers[2].Name != "worker" || d.Servers[2].Cwd != "apps/worker" || d.Servers[2].Port != 0 {
		t.Fatalf("declaration after the first start = %s (%v), want web, api and worker in apps/worker", c.Declaration, err)
	}
	if !strings.HasPrefix(c.Declaration, `{"version":1,`) && !strings.Contains(c.Declaration, `"version": 1`) {
		t.Errorf("declaration lost its format version: %s", c.Declaration)
	}
	written := c.Declaration
	runDevServer(t, c, DevServerParams{Action: "stop", Hostname: "appdev", Name: "worker"})
	runDevServer(t, c, DevServerParams{Action: "start", Hostname: "appdev", Name: "worker", NoHTTPProbe: true})
	runDevServer(t, c, DevServerParams{Action: "restart", Hostname: "appdev", Name: "web"})
	if n := sentWith(c, "dev-servers.json.tmp"); n != 1 {
		t.Errorf("declaration written %d times, want once", n)
	}
	if c.Declaration != written {
		t.Errorf("start/stop rewrote the declaration:\n%s\nwant\n%s", c.Declaration, written)
	}
	if c.Status("worker") != devsup.StatusRunning {
		t.Errorf("worker = %s after its second start, want running", c.Status("worker"))
	}
}

// TestDevServer_WorkDirOfACrewCopy_NeverRewritesTheDeclaredCwd (orchestrator
// decision D, review hole 2): a start or restart of a declared server with
// workDir naming another directory (a crew copy) leaves the repo's
// declaration as written and runs nothing — a copy runs as a supervisor
// runtime entry (docs/spec-mate.md §2.8a.2). The declared directory itself
// is accepted.
func TestDevServer_WorkDirOfACrewCopy_NeverRewritesTheDeclaredCwd(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"start", "restart"} {
		c := devservertest.NewSupervised(twoServers)
		_, err := ExecuteDevServer(context.Background(), c, mockClientWithServices("appdev"), "p1",
			DevServerParams{Action: action, Hostname: "appdev", Name: "web", WorkDir: "/var/www/.crew/ada"})
		var pe *platform.PlatformError
		if !errors.As(err, &pe) || !strings.Contains(pe.Message, "apps/web") || !strings.Contains(pe.Suggestion, "zcp-devsup ctl run") {
			t.Fatalf("%s with a crew copy's workDir: err = %v, want a refusal naming the declared cwd and the runtime entry", action, err)
		}
		if c.Declaration != twoServers || sentWith(c, "dev-servers.json.tmp") != 0 {
			t.Errorf("%s with a crew copy's workDir rewrote the declaration: %s", action, c.Declaration)
		}
		if sentWith(c, "zcp-devsup ctl '"+action+"'") != 0 {
			t.Errorf("%s with a crew copy's workDir still asked the supervisor to %s: %q", action, action, c.Sent())
		}
	}
	c := devservertest.NewSupervised(twoServers)
	runDevServer(t, c, DevServerParams{Action: "restart", Hostname: "appdev", Name: "web", WorkDir: "/var/www/apps/web"})
	if c.Declaration != twoServers {
		t.Errorf("a restart naming the declared directory rewrote the declaration: %s", c.Declaration)
	}
}

// TestDevServer_InvalidDeclaration_NeverOverwritten: zcp never overwrites a
// declaration it cannot read (docs/spec-mate.md §2.8a.1).
func TestDevServer_InvalidDeclaration_NeverOverwritten(t *testing.T) {
	t.Parallel()
	broken := `{"version":1,"servers":[{"name":"web"`
	c := devservertest.NewSupervised(broken)
	_, err := ExecuteDevServer(context.Background(), c, mockClientWithServices("appdev"), "p1",
		DevServerParams{Action: "start", Hostname: "appdev", Name: "api", Command: "pnpm dev", Port: 8080})
	if err == nil || !strings.Contains(err.Error(), "is invalid") {
		t.Fatalf("err = %v, want the declaration reported invalid", err)
	}
	if c.Declaration != broken || sentWith(c, "dev-servers.json.tmp") != 0 {
		t.Errorf("an invalid declaration was overwritten: %s", c.Declaration)
	}
}

// TestDevServer_SupervisorOlderThanDeclaration (TEST-PLAN E1 #6): the
// declaration carries a format version; zcp reports a supervisor older than it.
func TestDevServer_SupervisorOlderThanDeclaration(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised("")
	c.Declaration = `{"version":2,"servers":[]}`
	c.SupervisorVersion = "v9.210.0"
	_, err := ExecuteDevServer(context.Background(), c, mockClientWithServices("appdev"), "p1",
		DevServerParams{Action: "start", Hostname: "appdev", Command: "pnpm dev", Port: 3000})
	if err == nil {
		t.Fatal("a start against a declaration newer than the supervisor succeeded")
	}
	for _, want := range []string{"v9.210.0", "format 2"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %q, want it to name %q", err, want)
		}
	}
}

func TestDevServer_Status_NotRunning(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(oneServer)
	c.HTTP = map[int]int{3000: 0}
	result := runDevServer(t, c, DevServerParams{
		Action:     "status",
		Hostname:   "apidev",
		Port:       3000,
		HealthPath: "/api/health",
	})
	if result.Running {
		t.Errorf("expected Running=false for 000 curl response")
	}
	if result.Reason != "connection_refused" {
		t.Errorf("expected reason=connection_refused, got %q", result.Reason)
	}
}

func TestDevServer_Status_Running(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(oneServer)
	result := runDevServer(t, c, DevServerParams{
		Action:     "status",
		Hostname:   "apidev",
		Port:       3000,
		HealthPath: "/health",
	})
	if !result.Running {
		t.Errorf("expected Running=true for 200 response, got %+v", result)
	}
	if result.HealthStatus != 200 {
		t.Errorf("expected HealthStatus=200, got %d", result.HealthStatus)
	}
}

// TestDevServer_Status_Crashed: a server the supervisor holds as crashed is
// reported with its exit, and the supervisor leaves it down.
func TestDevServer_Status_Crashed(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised("")
	c.Declaration = twoServers
	c.Crash = map[string]bool{"api": true}
	c.Logs = map[string][]string{"api": {"panic: boom"}}
	runDevServer(t, c, DevServerParams{Action: "start", Hostname: "appdev", Name: "web"})
	runDevServer(t, c, DevServerParams{Action: "start", Hostname: "appdev", Name: "api"})

	result := runDevServer(t, c, DevServerParams{Action: "status", Hostname: "appdev", Name: "api"})
	if result.Running || result.Reason != reasonCrashed {
		t.Errorf("result = %+v, want crashed", result)
	}
	if !strings.Contains(result.Message, "api on appdev crashed (exit 1)") || strings.TrimSpace(result.LogTail) != "panic: boom" {
		t.Errorf("message/logTail = %q/%q, want the exit and the server's log", result.Message, result.LogTail)
	}
}

// A restart reply that sees its server crashed while starting, with the
// supervisor's one more start pending, says the server comes back and when to
// look again — not that the supervisor leaves it down to be fixed.
func TestDevServer_ARestartThatCrashedWithItsOneMoreStartPendingSaysItComesBack(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(twoServers)
	c.Crash = map[string]bool{"api": true}
	c.RetryPending = map[string]bool{"api": true}

	result := runDevServer(t, c, DevServerParams{Action: "restart", Hostname: "appdev", Name: "api"})

	want := `Dev server api on appdev crashed while starting (exit 1); the supervisor starts it once more after its siblings settle. Check api again in about 30 s with zerops_dev_server action=status hostname="appdev" name="api".`
	if result.Reason != reasonCrashed || !strings.Contains(result.Message, want) {
		t.Errorf("result = %+v, want reason crashed and message containing %q", result, want)
	}
	if strings.Contains(result.Message, "leaves it down") {
		t.Errorf("message = %q, says the supervisor leaves down a server it starts once more", result.Message)
	}
}

// A server that crashed because the service's task limit was reached says
// so, with the limit and the tasks running now — coming back while the
// supervisor starts it again after a backoff, and what frees tasks once it
// stopped trying — instead of an exit to fix in the server's code.
func TestDevServer_AServerThatCrashedOnTheServicesTaskLimitNamesTheLimitAndTheTasksRunningNow(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		pending bool
		want    string
	}{
		{"coming back", true, `Dev server api on appdev crashed (exit 1): the service's task limit (300) was reached, 297 tasks run now; the supervisor starts it again after a backoff, one server at a time. Check api again in about 30 s with zerops_dev_server action=status hostname="appdev" name="api".`},
		{"stopped trying", false, `Dev server api on appdev crashed (exit 1): the service's task limit (300) was reached, 297 tasks run now; the supervisor stopped starting it. Free tasks — stop a server you do not need, or cap the threads its tools start (RAYON_NUM_THREADS, UV_CONCURRENT_BUILDS, UV_CONCURRENT_INSTALLS) — then start it again.`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(twoServers)
			c.Crash = map[string]bool{"api": true}
			c.RetryPending = map[string]bool{"api": tt.pending}
			c.TaskLimit = map[string]*devsup.TaskLimit{"api": {Max: 300, Current: 297}}

			result := runDevServer(t, c, DevServerParams{Action: "restart", Hostname: "appdev", Name: "api"})

			if result.Reason != reasonCrashed || !strings.Contains(result.Message, tt.want) {
				t.Errorf("result = %+v, want reason crashed and message containing %q", result, tt.want)
			}
		})
	}
}

// vinext bound to 127.0.0.1 was reported "slow" with a public URL that
// answered 502: the probe inside the container reaches a loopback listener,
// nothing outside it does. A start or status of a server that listens on
// loopback only says so and to bind 0.0.0.0, and gives no address.
func TestDevServer_AServerThatListensOnLoopbackOnlyIsNamedSoWithoutAnAddress(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"start", "restart", "status"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(twoServers)
			c.Loopback = map[int]bool{8080: true}

			result := runDevServer(t, c, DevServerParams{Action: action, Hostname: "appdev", Name: "api"})

			want := "Dev server api on appdev listens on loopback only (127.0.0.1:8080): neither http://appdev:8080 nor its subdomain reaches it. Make it bind 0.0.0.0 (for example --host 0.0.0.0) and restart it, unless a web server in front of it proxies to it."
			if result.Reason != ReasonLoopbackOnly || !strings.Contains(result.Message, want) {
				t.Errorf("reason/message = %q/%q, want %q and %q", result.Reason, result.Message, ReasonLoopbackOnly, want)
			}
			if result.URL != "" {
				t.Errorf("url = %q, want none for a server nothing outside the container reaches", result.URL)
			}
		})
	}
}

// A restart whose health probe ends while the supervisor still reports the
// server starting names it as still starting and says when to look again.
func TestDevServer_ARestartStillStartingWhenItsProbeEndsSaysWhenToCheckItAgain(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(twoServers)
	c.HTTP = map[int]int{8080: 0}

	result := runDevServer(t, c, DevServerParams{Action: "restart", Hostname: "appdev", Name: "api", WaitSeconds: 1})

	want := `Check api again in about 30 s with zerops_dev_server action=status hostname="appdev" name="api".`
	if !strings.Contains(result.Message, "the supervisor reports it starting") || !strings.Contains(result.Message, want) {
		t.Errorf("message = %q, want it still starting and %q", result.Message, want)
	}
}

func TestDevServer_Logs_ReturnsTail(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(oneServer)
	c.Logs = map[string][]string{"apidev": {"line 1", "line 2", "line 3"}}
	result := runDevServer(t, c, DevServerParams{
		Action:   "logs",
		Hostname: "apidev",
		LogLines: 10,
	})
	if !strings.Contains(result.LogTail, "line 1") {
		t.Errorf("expected log tail to include 'line 1', got %q", result.LogTail)
	}
	if result.LogFile != "/home/zerops/.zcp/devsup/logs/apidev.log" {
		t.Errorf("expected LogFile to be the server's own log, got %q", result.LogFile)
	}
	if sentWith(c, "zcp-devsup ctl logs 'apidev' --lines 10") != 1 {
		t.Errorf("want one supervisor log read of 10 lines, sent %q", c.Sent())
	}
}

// TestDevServer_Restart_IsStopThenStart: a restart is the supervisor's own
// stop-then-start of the server's group.
func TestDevServer_Restart_IsStopThenStart(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(`{"version":1,"servers":[{"name":"workerdev","port":3001,"command":"npm run start:dev","cwd":"."}]}`)
	c.HTTP = map[int]int{3001: 204}
	before := c.Servers["workerdev"].Pid

	result := runDevServer(t, c, DevServerParams{
		Action:     "restart",
		Hostname:   "workerdev",
		Command:    "npm run start:dev",
		Port:       3001,
		HealthPath: "/health",
	})
	if result.Action != "restart" {
		t.Errorf("expected Action=restart, got %q", result.Action)
	}
	if !result.Running {
		t.Errorf("expected Running=true, got %+v", result)
	}
	if result.HealthStatus != 204 {
		t.Errorf("expected HealthStatus=204, got %d", result.HealthStatus)
	}
	if sentWith(c, "zcp-devsup ctl 'restart' 'workerdev'") != 1 || result.PID == before {
		t.Errorf("want one supervisor restart and a new group (pid %d → %d)", before, result.PID)
	}
}

func TestDevServer_UnknownAction(t *testing.T) {
	t.Parallel()

	_, err := ExecuteDevServer(context.Background(), devservertest.NewSupervised(""), mockClientWithServices("apidev"), "p1",
		DevServerParams{
			Action:   "kill-all",
			Hostname: "apidev",
		})
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

// TestDevServer_Start_NoHTTPProbe_Success — the no-probe mode serves
// services with no HTTP surface: a long-running consumer, a cron runner, a
// sidecar writer. The supervisor runs it like any server; the tool skips the
// HTTP probe and decides from the supervisor's status after a settle.
func TestDevServer_Start_NoHTTPProbe_Success(t *testing.T) {
	disableNoProbeSettleForTest(t)
	c := devservertest.NewSupervised("")
	c.Logs = map[string][]string{"workerdev": {"process started", "waiting..."}}

	result := runDevServer(t, c, DevServerParams{
		Action:      "start",
		Hostname:    "workerdev",
		Command:     "./bin/worker",
		NoHTTPProbe: true, // no Port, no HealthPath — service has no HTTP surface
	})
	if !result.Running {
		t.Errorf("expected Running=true in no-probe mode when the supervisor runs it, got %+v", result)
	}
	if result.HealthStatus != 0 {
		t.Errorf("expected HealthStatus=0 in no-probe mode (probe skipped), got %d", result.HealthStatus)
	}
	if result.Port != 0 {
		t.Errorf("expected Port=0 in no-probe mode, got %d", result.Port)
	}
	if result.LogTail == "" {
		t.Errorf("expected non-empty LogTail")
	}
	if result.Reason != "" {
		t.Errorf("expected empty Reason on successful no-probe start, got %q", result.Reason)
	}
	// Message should explicitly tell the agent this was no-probe mode
	// and point at zerops_logs for verification — agents should not
	// conclude "Running=true" means "worker is processing work".
	if !strings.Contains(strings.ToLower(result.Message), "no-probe") {
		t.Errorf("expected Message to reference no-probe mode: %q", result.Message)
	}
	if !strings.Contains(strings.ToLower(result.Message), "zerops_logs") {
		t.Errorf("expected Message to reference zerops_logs for verification: %q", result.Message)
	}
	for _, cmd := range c.Sent() {
		if strings.Contains(cmd, "curl") {
			t.Errorf("no-probe start curled: %q", cmd)
		}
	}
}

// TestDevServer_Start_NoHTTPProbe_AllowsZeroPort — validation must accept
// Port=0 when NoHTTPProbe=true.
func TestDevServer_Start_NoHTTPProbe_AllowsZeroPort(t *testing.T) {
	disableNoProbeSettleForTest(t)

	_, err := ExecuteDevServer(context.Background(), devservertest.NewSupervised(""), mockClientWithServices("workerdev"), "p1",
		DevServerParams{
			Action:      "start",
			Hostname:    "workerdev",
			Command:     "./bin/worker",
			Port:        0, // explicitly zero — service has no port
			NoHTTPProbe: true,
		})
	if err != nil {
		t.Fatalf("expected no validation error when NoHTTPProbe=true allows Port=0, got: %v", err)
	}
}

// TestDevServer_Start_PortRequiredWithoutNoHTTPProbe — regression guard:
// the default start path MUST still reject Port=0. Relaxing port validation
// unconditionally would hide bugs in the common HTTP-service case.
func TestDevServer_Start_PortRequiredWithoutNoHTTPProbe(t *testing.T) {
	t.Parallel()

	_, err := ExecuteDevServer(context.Background(), devservertest.NewSupervised(""), mockClientWithServices("apidev"), "p1",
		DevServerParams{
			Action:      "start",
			Hostname:    "apidev",
			Command:     "./bin/server",
			Port:        0,
			NoHTTPProbe: false, // default — probe required, port required
		})
	if err == nil {
		t.Fatal("expected validation error for Port=0 when NoHTTPProbe=false")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "port") {
		t.Errorf("expected error mentioning port, got: %v", err)
	}
}

// TestDevServer_Start_NoHTTPProbe_CrashedAfterSettle — a worker that dies
// during init (bad config, missing env var, syntax error) is crashed in the
// supervisor's state, and that is what the check observes. No
// runtime-specific log-string matching.
func TestDevServer_Start_NoHTTPProbe_CrashedAfterSettle(t *testing.T) {
	disableNoProbeSettleForTest(t)
	c := devservertest.NewSupervised("")
	c.Crash = map[string]bool{"workerdev": true}
	c.Logs = map[string][]string{"workerdev": {"starting...", "fatal: something went wrong"}}

	result := runDevServer(t, c, DevServerParams{Action: "start", Hostname: "workerdev", Command: "./bin/worker", NoHTTPProbe: true})
	if result.Running {
		t.Errorf("expected Running=false when the supervisor reports crashed, got %+v", result)
	}
	if result.Reason != reasonCrashed {
		t.Errorf("expected Reason=%s, got %q", reasonCrashed, result.Reason)
	}
	if result.LogTail == "" {
		t.Errorf("expected LogTail populated so agent can diagnose the exit")
	}
}

// TestDevServer_NoSupervisor: stop, status and logs on a dev service whose dev
// setup does not run the supervisor change nothing and say how to set it up;
// a start returns *NoSupervisorError for the caller to migrate.
func TestDevServer_NoSupervisor(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"stop", "status", "logs"} {
		c := &devservertest.Container{}
		result := runDevServer(t, c, DevServerParams{Action: action, Hostname: "appdev"})
		if result.Reason != reasonNoSupervisor || !strings.Contains(result.Message, "action=start sets it up") {
			t.Errorf("%s: result = %+v, want no_supervisor with the way to set it up", action, result)
		}
		if n := len(c.Sent()); n != 1 {
			t.Errorf("%s: sent %d commands, want only the read", action, n)
		}
	}
	for _, installed := range []bool{false, true} {
		c := &devservertest.Container{Installed: installed, SupervisorVersion: "v9.300.0", Formats: []int{1}}
		_, err := ExecuteDevServer(context.Background(), c, mockClientWithServices("appdev"), "p1",
			DevServerParams{Action: "start", Hostname: "appdev", Command: "bun index.ts", Port: 3000})
		var noSup *NoSupervisorError
		if !errors.As(err, &noSup) || noSup.Installed != installed {
			t.Errorf("installed=%v: err = %v, want *NoSupervisorError{Installed: %v}", installed, err, installed)
		}
		if c.Declaration != "" {
			t.Errorf("installed=%v: a refused start declared %q", installed, c.Declaration)
		}
	}
}

// A supervisor that is running but does not answer in time (a thrashing
// container) is reported unresponsive, not missing: every action changes
// nothing, says to check the container's load and memory and retry, and a
// start never takes the move onto the supervisor (no redeploy).
func TestDevServer_ASupervisorThatDoesNotAnswerInTimeIsReportedUnresponsiveNotMissing(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"stop", "status", "logs", "start", "restart"} {
		c := devservertest.NewSupervised(`{"version":1,"servers":[{"name":"web","port":3000,"command":"next dev","cwd":"."}]}`)
		c.Unresponsive = true
		decl := c.Declaration
		result, err := ExecuteDevServer(context.Background(), c, mockClientWithServices("difydev"), "p1",
			DevServerParams{Action: action, Hostname: "difydev", Name: "web"})
		if err != nil {
			t.Fatalf("%s: err = %v, want an unresponsive result", action, err)
		}
		if result.Reason != reasonSupervisorUnresponsive || result.Running ||
			!strings.Contains(result.Message, "running but did not answer") || !strings.Contains(result.Message, "zerops_scale") ||
			strings.Contains(result.Message, "redeploy") || strings.Contains(result.Message, "sets it up") {
			t.Errorf("%s: result = %+v, want supervisor_unresponsive naming load/memory and no redeploy", action, result)
		}
		if n := len(c.Sent()); n != 1 || c.Declaration != decl || c.Deploys != 0 {
			t.Errorf("%s: sent %d commands, declaration %q, deploys %d — want only the read, nothing changed", action, n, c.Declaration, c.Deploys)
		}
	}
}

// A dev container that does not answer over SSH in time (out of memory or
// CPU) is reported not answering, never an unknown ssh error: every action
// changes nothing and says to check its load and memory, scale it or restart it.
func TestDevServer_AContainerThatDoesNotAnswerSSHInTimeIsReportedNotAnsweringWithItsRecovery(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"stop", "status", "logs", "start", "restart"} {
		c := devservertest.NewSupervised(`{"version":1,"servers":[{"name":"web","port":3000,"command":"next dev","cwd":"."}]}`)
		c.Silent = true
		decl := c.Declaration
		result, err := ExecuteDevServer(context.Background(), c, mockClientWithServices("difydev"), "p1",
			DevServerParams{Action: action, Hostname: "difydev", Name: "web"})
		if err != nil {
			t.Fatalf("%s: err = %v, want a not-answering result", action, err)
		}
		want := "difydev did not answer over SSH in time — the container may be out of memory or CPU. Check its load and memory (zerops_logs, zerops_scale) or restart it (zerops_manage action=restart), then retry."
		if result.Reason != reasonSupervisorUnresponsive || result.Running || result.Message != want {
			t.Errorf("%s: result = %+v, want supervisor_unresponsive saying %q", action, result, want)
		}
		if n := len(c.Sent()); n != 1 || c.Declaration != decl || c.Deploys != 0 {
			t.Errorf("%s: sent %d commands, declaration %q, deploys %d — want only the read, nothing changed", action, n, c.Declaration, c.Deploys)
		}
	}
}

// An entry removed from the declaration by hand leaves its server running
// until the supervisor re-reads the file: the next zerops_dev_server call has
// it re-read, which stops and drops that server, and the reply names it.
func TestDevServer_AServerRemovedFromTheDeclarationIsStoppedAndTheReplySaysSo(t *testing.T) {
	t.Parallel()
	const both = `{"version":1,"servers":[{"name":"web","port":3000,"command":"next dev","cwd":"."},{"name":"lo","port":5999,"command":"python3 -m http.server 5999","cwd":"."}]}`
	const removed = "Dev server lo on difydev is no longer declared in .zcp/dev-servers.json, so the supervisor stopped it."
	for _, tc := range []struct{ action, name string }{{"status", ""}, {"logs", "web"}, {"stop", "lo"}, {"status", "lo"}} {
		c := devservertest.NewSupervised(both)
		c.Declaration = `{"version":1,"servers":[{"name":"web","port":3000,"command":"next dev","cwd":"."}]}`
		result, err := ExecuteDevServer(context.Background(), c, mockClientWithServices("difydev"), "p1",
			DevServerParams{Action: tc.action, Hostname: "difydev", Name: tc.name})
		if err != nil {
			t.Fatalf("%s %s: err = %v, want a reply naming lo as stopped", tc.action, tc.name, err)
		}
		if _, held := c.Servers["lo"]; held {
			t.Errorf("%s %s: the supervisor still holds lo, want it stopped and dropped", tc.action, tc.name)
		}
		if c.Status("web") != devsup.StatusRunning {
			t.Errorf("%s %s: web is %q, want it left running", tc.action, tc.name, c.Status("web"))
		}
		if !strings.Contains(result.Message, removed) {
			t.Errorf("%s %s: message = %q, want it to contain %q", tc.action, tc.name, result.Message, removed)
		}
	}
	c := devservertest.NewSupervised(both)
	if _, err := ExecuteDevServer(context.Background(), c, mockClientWithServices("difydev"), "p1",
		DevServerParams{Action: "status", Hostname: "difydev"}); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range c.Sent() {
		if strings.Contains(cmd, "ctl 'reload'") {
			t.Errorf("sent %q with both servers still declared, want no reload", cmd)
		}
	}
}

// TestDevServer_NoProcessPatternMatching (TEST-PLAN E1 #3, #14): no
// dev-server path finds processes by name or command pattern, and the
// keeper, the zcp-side dev-servers.json and the SSH start are gone. One
// mechanism runs dev servers: the dev service's supervisor.
func TestDevServer_NoProcessPatternMatching(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..")
	forbidden := regexp.MustCompile(`\bpkill\b|\bpgrep\b|processMatch|ProcessMatch|setsid|nohup|zcp-dev-server-spawned|devservers-%|KeepDevServer|KeptDevServer|DevServerRunning|RunDevServerKeeper|"dev-server", "keep"`)
	paths := []string{
		"internal/ops/dev_server.go", "internal/ops/dev_server_supervisor.go", "internal/ops/dev_server_migrate.go",
		"internal/tools/dev_server.go", "internal/tools/dev_server_migrate.go", "internal/tools/verify.go",
		"internal/tools/deploy_ssh.go", "internal/tools/deploy_batch.go", "internal/tools/delete.go",
		"internal/platform/units.go", "cmd/zcp/main.go",
	}
	for _, p := range paths {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if trimmed := strings.TrimSpace(line); strings.HasPrefix(trimmed, "//") {
				continue
			}
			if m := forbidden.FindString(line); m != "" {
				t.Errorf("%s:%d uses %q — dev servers run only under the supervisor", p, i+1, m)
			}
		}
	}
	for _, gone := range []string{
		"internal/ops/dev_server_start.go", "internal/ops/dev_server_lifecycle.go",
		"internal/tools/dev_server_keep.go", "internal/workflow/dev_servers.go", "cmd/zcp/dev_server.go",
	} {
		if _, err := os.Stat(filepath.Join(root, gone)); err == nil {
			t.Errorf("%s is back — the keeper and the SSH start are deleted, not kept beside the supervisor", gone)
		}
	}
}

// TestDevServersAfterDeploy: after a deploy replaced the container, a dev
// service with one declared server reports it from the supervisor — probed
// when it runs, its exit when it crashed, stopped when it is not run — and
// one with several reports none on its own.
func TestDevServersAfterDeploy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		container   func() *devservertest.Container
		wantResult  bool
		wantRunning bool
		wantReason  string
	}{
		{name: "the declared server runs", container: func() *devservertest.Container { return devservertest.NewSupervised(oneServer) },
			wantResult: true, wantRunning: true},
		{name: "it crashed", container: func() *devservertest.Container {
			c := devservertest.NewSupervised("")
			c.Declaration, c.Crash = oneServer, map[string]bool{"apidev": true}
			c.Redeploy()
			return c
		}, wantResult: true, wantReason: reasonCrashed},
		{name: "it is stopped", container: func() *devservertest.Container {
			c := devservertest.NewSupervised(oneServer)
			c.Servers["apidev"].Status = devsup.StatusStopped
			return c
		}, wantResult: true, wantReason: reasonStopped},
		{name: "it is declared with autostart false", container: func() *devservertest.Container {
			return devservertest.NewSupervised(`{"version":1,"servers":[{"name":"apidev","port":3000,"command":"npm run start:dev","cwd":".","autostart":false}]}`)
		}, wantResult: true, wantReason: reasonStopped},
		{name: "several servers", container: func() *devservertest.Container { return devservertest.NewSupervised(twoServers) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := tt.container()
			_, result, err := DevServersAfterDeploy(context.Background(), c, "apidev", "")
			if err != nil {
				t.Fatalf("DevServersAfterDeploy: %v", err)
			}
			if (result != nil) != tt.wantResult {
				t.Fatalf("result = %+v, want one: %v", result, tt.wantResult)
			}
			if result != nil && (result.Running != tt.wantRunning || result.Reason != tt.wantReason) {
				t.Errorf("result = %+v, want running=%v reason=%q", result, tt.wantRunning, tt.wantReason)
			}
			if !tt.wantRunning && sentWith(c, "for i in $(seq") != 0 {
				t.Error("probed a server the supervisor does not run")
			}
		})
	}
}

// TestDevServer_Running_IsTheSupervisorsStatus (docs/spec-mate.md §2.8a.3,
// "One source"): a result's running is the supervisor's status of the server
// it reports; the HTTP probe is a health fact beside it (healthStatus, reason,
// the envelope entry's verify) and never decides running. An api with no root
// route runs and answers 404.
func TestDevServer_Running_IsTheSupervisorsStatus(t *testing.T) {
	t.Parallel()
	probeOK, probeFailed := true, false
	tests := []struct {
		name       string
		http       map[int]int
		crash      bool
		p          DevServerParams
		running    bool
		healthCode int
		reason     string
		verify     *bool
	}{
		{name: "status, answers 200", p: DevServerParams{Action: "status", Name: "api"},
			running: true, healthCode: 200, verify: &probeOK},
		{name: "status, no root route answers 404", http: map[int]int{8080: 404}, p: DevServerParams{Action: "status", Name: "api"},
			running: true, healthCode: 404, reason: "http_404", verify: &probeFailed},
		{name: "status, port not open yet", http: map[int]int{8080: 0}, p: DevServerParams{Action: "status", Name: "api"},
			running: false, reason: "connection_refused", verify: &probeFailed},
		{name: "status, crashed", crash: true, p: DevServerParams{Action: "status", Name: "api"},
			running: false, reason: reasonCrashed},
		{name: "start, no root route answers 404", http: map[int]int{8080: 404}, p: DevServerParams{Action: "start", Name: "api", WaitSeconds: 1},
			running: true, healthCode: 404, reason: "health_probe_http_404", verify: &probeFailed},
		{name: "start, answers 200", p: DevServerParams{Action: "start", Name: "api"},
			running: true, healthCode: 200, verify: &probeOK},
		{name: "logs of a running server", p: DevServerParams{Action: "logs", Name: "api"},
			running: true},
		{name: "stop", p: DevServerParams{Action: "stop", Name: "api"},
			running: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(twoServers)
			c.HTTP = tt.http
			c.Logs = map[string][]string{"api": {"listening on :8080"}}
			if tt.crash {
				c.Crash = map[string]bool{"api": true}
				runDevServer(t, c, DevServerParams{Action: "restart", Hostname: "appdev", Name: "api"})
			}
			tt.p.Hostname = "appdev"
			result := runDevServer(t, c, tt.p)
			if result.Running != tt.running || result.HealthStatus != tt.healthCode || result.Reason != tt.reason {
				t.Errorf("running/healthStatus/reason = %v/%d/%q, want %v/%d/%q (%s)",
					result.Running, result.HealthStatus, result.Reason, tt.running, tt.healthCode, tt.reason, result.Message)
			}
			var api *topology.DevServerState
			states := DevServerStates(context.Background(), mockClientWithServices("appdev"), "p1", result)
			for i := range states {
				if states[i].Name == "api" {
					api = &states[i]
				}
			}
			if api == nil {
				t.Fatalf("no api entry in %+v", states)
			}
			if running := api.Status == string(devsup.StatusRunning); running != result.Running {
				t.Errorf("running = %v, but the entry's status is %q", result.Running, api.Status)
			}
			switch {
			case tt.verify == nil && api.Verify != nil:
				t.Errorf("verify = %+v, want none (no probe in this call)", api.Verify)
			case tt.verify != nil && (api.Verify == nil || api.Verify.OK != *tt.verify):
				t.Errorf("verify = %+v, want ok=%v", api.Verify, *tt.verify)
			}
		})
	}
}

const gib64 = int64(1) << 30

func memoryContainer(maxGiB, usedGiB int64, rssGiB map[string]float64) *devservertest.Container {
	c := devservertest.NewSupervised(twoServers)
	c.MemMaxB, c.MemCurrentB = maxGiB*gib64, usedGiB*gib64
	c.Servers["api"].Status, c.Servers["api"].Desired = devsup.StatusStopped, devsup.DesiredStopped
	for n, g := range rssGiB {
		c.Servers[n].RssKB = int64(g * 1024 * 1024)
	}
	return c
}

func TestDevServer_StartThatWouldLeaveLessHeadroomThanTheLargestRunningServer_IsRefusedWithNumbersAndTwoWaysOut(t *testing.T) {
	t.Parallel()
	c := memoryContainer(6, 5, map[string]float64{"web": 2.5})
	_, err := ExecuteDevServer(context.Background(), c, mockClientWithServices("appdev"), "p1",
		DevServerParams{Action: "start", Hostname: "appdev", Name: "api"})
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("want a refusal, got %v", err)
	}
	for _, want := range []string{"Not starting api on appdev", "5.0 GiB", "6.0 GiB", "1.0 GiB", "2.5 GiB", "web 2.5 GiB"} {
		if !strings.Contains(pe.Message, want) {
			t.Errorf("message %q lacks %q", pe.Message, want)
		}
	}
	if !strings.Contains(pe.Suggestion, "action=stop") || !strings.Contains(pe.Suggestion, "zerops_scale") {
		t.Errorf("suggestion %q must name stopping another server and scaling max RAM", pe.Suggestion)
	}
	if sentWith(c, "ctl 'start' 'api'") != 0 {
		t.Errorf("a refused start must not reach the supervisor")
	}
	if c.Status("web") != devsup.StatusRunning {
		t.Errorf("the running server must stay running, got %s", c.Status("web"))
	}
}

func TestDevServer_StartThatFitsTheHeadroom_Starts(t *testing.T) {
	t.Parallel()
	c := memoryContainer(6, 3, map[string]float64{"web": 2.5})
	result := runDevServer(t, c, DevServerParams{Action: "start", Hostname: "appdev", Name: "api"})
	if result.Name != "api" || sentWith(c, "ctl 'start' 'api'") != 1 {
		t.Errorf("api must start, got %+v", result)
	}
}

func TestDevServer_StartWhenTheServiceMaxRamIsAboveTheGrantedRam_JudgesAgainstTheMaxRam(t *testing.T) {
	t.Parallel()
	c := memoryContainer(4, 3, map[string]float64{"web": 2.5})
	client := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test"}).
		WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev", ProjectID: "p1", Status: "ACTIVE",
			CustomAutoscaling: &platform.CustomAutoscaling{MaxRAM: 8}}})
	if _, err := ExecuteDevServer(context.Background(), c, client, "p1",
		DevServerParams{Action: "start", Hostname: "appdev", Name: "api"}); err != nil {
		t.Fatalf("8 GiB ceiling with 3 GiB used leaves 5 GiB, enough for another 2.5 GiB server: %v", err)
	}
}

func TestDevServer_StartOfASecondNamedServerOnAOneGiBService_IsNotRefusedWhileNothingHeavyRuns(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(twoServers)
	c.MemMaxB, c.MemCurrentB = gib64, gib64/10
	c.Servers["api"].Status, c.Servers["api"].Desired = devsup.StatusStopped, devsup.DesiredStopped
	result := runDevServer(t, c, DevServerParams{Action: "start", Hostname: "appdev", Name: "api"})
	if result.Name != "api" || sentWith(c, "ctl 'start' 'api'") != 1 {
		t.Errorf("api must start on a 1 GiB service with 0.9 GiB free, got %+v", result)
	}
}

func TestDevServer_AFirstDevServerStartWithNoServerRunning_IsNeverRefusedOnMemory(t *testing.T) {
	t.Parallel()
	// A single-part app on a 0.5 GiB grant with 400 MiB in use: nothing runs
	// to measure one more server by, and the autoscaler grows the grant.
	c := memoryContainer(0, 0, nil)
	c.MemMaxB, c.MemCurrentB = gib64/2, 400<<20
	c.Servers["web"].Status, c.Servers["web"].Desired = devsup.StatusStopped, devsup.DesiredStopped
	result := runDevServer(t, c, DevServerParams{Action: "start", Hostname: "appdev", Name: "api"})
	if result.Name != "api" || sentWith(c, "ctl 'start' 'api'") != 1 {
		t.Errorf("api must start, got %+v", result)
	}
}

func TestDevServer_ADefaultAutoscaledService_IsJudgedAgainstItsAutoscalingMaxNotItsCurrentGrant(t *testing.T) {
	t.Parallel()
	// 2 GiB granted, 1.6 GiB used, web holds 0.5 GiB: against the grant 0.4
	// GiB is left, against the 6 GiB the service can grow to 4.4 GiB.
	c := memoryContainer(2, 0, map[string]float64{"web": 0.5})
	c.MemCurrentB = gib64 * 16 / 10
	// The project's service list carries no currentAutoscaling; only the
	// service's own detail does, and a default-autoscaled service has no
	// customAutoscaling.
	client := platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test"}).
		WithServices([]platform.ServiceStack{{ID: "svc-appdev", Name: "appdev", ProjectID: "p1", Status: "ACTIVE"}}).
		WithService(&platform.ServiceStack{ID: "svc-appdev", Name: "appdev", ProjectID: "p1", Status: "ACTIVE",
			CurrentAutoscaling: &platform.CustomAutoscaling{MinRAM: 0.5, MaxRAM: 6}})
	if _, err := ExecuteDevServer(context.Background(), c, client, "p1",
		DevServerParams{Action: "start", Hostname: "appdev", Name: "api"}); err != nil {
		t.Fatalf("6 GiB autoscaling max with 1.6 GiB used leaves 4.4 GiB, enough for another 0.5 GiB server: %v", err)
	}
	if sentWith(c, "ctl 'start' 'api'") != 1 {
		t.Errorf("api must reach the supervisor")
	}
}

func TestDevServer_PageCache_DoesNotCountAsUsedMemory(t *testing.T) {
	t.Parallel()
	// 5.5 GiB charged of 6, of it 2.5 GiB file cache the kernel reclaims:
	// 3 GiB is in use, and another 2.5 GiB server fits.
	c := memoryContainer(6, 0, map[string]float64{"web": 2.5})
	c.MemCurrentB, c.MemActiveB = gib64*55/10, gib64*25/10
	result := runDevServer(t, c, DevServerParams{Action: "start", Hostname: "appdev", Name: "api"})
	if result.Name != "api" || sentWith(c, "ctl 'start' 'api'") != 1 {
		t.Errorf("api must start, got %+v", result)
	}
}

// TestDevServer_StartingAServerBesideAnotherRunningOne_TellsTheAgentToCapEachHeapToFitTheSharedCeiling:
// two dev servers compiling at once ran a 6 GiB service out of memory twice
// although the start fitted, so a start beside another running declared
// server adds one line naming the ceiling they share and asking for a heap
// cap on each. A start with no other server running, and a restart of a
// running one, add nothing.
func TestDevServer_StartingAServerBesideAnotherRunningOne_TellsTheAgentToCapEachHeapToFitTheSharedCeiling(t *testing.T) {
	t.Parallel()
	const heapLine = "NODE_OPTIONS=--max-old-space-size"

	beside := runDevServer(t, memoryContainer(6, 3, map[string]float64{"web": 2.5}),
		DevServerParams{Action: "start", Hostname: "appdev", Name: "api"})
	var lines []string
	for line := range strings.SplitSeq(beside.Message, "\n") {
		if strings.Contains(line, heapLine) {
			lines = append(lines, line)
		}
	}
	if len(lines) != 1 {
		t.Fatalf("want one heap-cap line, got %d in %q", len(lines), beside.Message)
	}
	for _, want := range []string{"api and web", "appdev's 6.0 GiB RAM ceiling", "side by side"} {
		if !strings.Contains(lines[0], want) {
			t.Errorf("heap-cap line %q lacks %q", lines[0], want)
		}
	}

	alone := memoryContainer(6, 0, nil)
	alone.Servers["web"].Status, alone.Servers["web"].Desired = devsup.StatusStopped, devsup.DesiredStopped
	if msg := runDevServer(t, alone, DevServerParams{Action: "start", Hostname: "appdev", Name: "api"}).Message; strings.Contains(msg, heapLine) {
		t.Errorf("a start with no other server running must add no heap-cap line: %q", msg)
	}
	if msg := runDevServer(t, memoryContainer(6, 3, map[string]float64{"web": 2.5}),
		DevServerParams{Action: "restart", Hostname: "appdev", Name: "web"}).Message; strings.Contains(msg, heapLine) {
		t.Errorf("a restart of a running server must add no heap-cap line: %q", msg)
	}
}

func TestDevServer_RestartOfARunningServer_IsNeverRefusedOnMemory(t *testing.T) {
	t.Parallel()
	c := memoryContainer(6, 5, map[string]float64{"web": 2.5})
	runDevServer(t, c, DevServerParams{Action: "restart", Hostname: "appdev", Name: "web"})
}

func TestDevServer_StartWhenTheCgroupIsUnreadable_StartsAsBefore(t *testing.T) {
	t.Parallel()
	c := memoryContainer(0, 0, nil)
	runDevServer(t, c, DevServerParams{Action: "start", Hostname: "appdev", Name: "api"})
}

// TestDevServer_StartBesideARunningServerWhenTheCgroupIsUnreadable_AddsNoHeapCapLine:
// with the service's RAM ceiling unknown, a start beside another running
// server names no shared ceiling and asks for no heap cap.
func TestDevServer_StartBesideARunningServerWhenTheCgroupIsUnreadable_AddsNoHeapCapLine(t *testing.T) {
	t.Parallel()
	c := memoryContainer(0, 0, map[string]float64{"web": 2.5})
	if c.Status("web") != devsup.StatusRunning {
		t.Fatalf("web must be running beside the start, got %s", c.Status("web"))
	}
	if msg := runDevServer(t, c, DevServerParams{Action: "start", Hostname: "appdev", Name: "api"}).Message; strings.Contains(msg, "NODE_OPTIONS=--max-old-space-size") || strings.Contains(msg, "RAM ceiling") {
		t.Errorf("an unreadable cgroup must add no heap-cap line: %q", msg)
	}
}

// The settled read waits for starting servers only within its budget: a
// server still starting when the budget ends is read as starting.
func TestReadDevServersSettled_AServerStillStartingWhenTheBudgetEndsIsReadStarting(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(`{"version":1,"servers":[{"name":"vets","port":8083,"command":"b","cwd":"."}]}`)
	c.Settle = map[string]int{"vets": 1000}
	c.Redeploy()

	start := time.Now()
	ds, err := readDevServersSettled(t.Context(), c, "petclinicdev", "/var/www", 100*time.Millisecond, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the read took %s past a 100ms budget", took)
	}
	if got := ds.State.Servers["vets"].Status; got != devsup.StatusStarting {
		t.Fatalf("vets = %q, want starting", got)
	}
}
