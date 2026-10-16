// Tests for: internal/ops/dev_server.go, dev_server_supervisor.go — what a
// dev service with several parts needs from its dev servers: each dev
// service's own servers, the declared health path, a slow first response told
// apart from a refused one, a log tail of the current run, and a coded refusal
// when a declared port is taken.
package ops

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
)

func TestDevServer_ARestartOrStatusWithoutHealthPath_ProbesThePathTheStartDeclared(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised("")
	runDevServer(t, c, DevServerParams{Action: "start", Hostname: "apidev", Command: "npm run dev", Port: 3000, HealthPath: "/api/health"})
	d, err := devsup.ParseDeclaration([]byte(c.Declaration))
	if err != nil || len(d.Servers) != 1 || d.Servers[0].HealthPath != "/api/health" {
		t.Fatalf("declaration = %s (%v), want the start's healthPath declared", c.Declaration, err)
	}

	for _, action := range []string{"restart", "status"} {
		result := runDevServer(t, c, DevServerParams{Action: action, Hostname: "apidev"})
		if result.HealthPath != "/api/health" {
			t.Errorf("%s probed %q, want the declared /api/health", action, result.HealthPath)
		}
	}
	if n := sentWith(c, "http://localhost:3000/api/health"); n != 3 {
		t.Errorf("probes of the declared path = %d, want 3 (start, restart, status)", n)
	}
}

func TestDevServer_AStartThatOnlyChangesTheHealthPath_DeclaresIt(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(oneServer)
	runDevServer(t, c, DevServerParams{Action: "start", Hostname: "apidev", HealthPath: "/health"})
	d, err := devsup.ParseDeclaration([]byte(c.Declaration))
	if err != nil || d.Servers[0].HealthPath != "/health" {
		t.Errorf("declaration = %s (%v), want healthPath /health declared", c.Declaration, err)
	}
}

// variantBDeclaration is a code volume's declaration shared by webdev and
// apidev: web is webdev's, api and admin are apidev's.
const variantBDeclaration = `{"version":1,"servers":[` +
	`{"name":"web","port":3000,"command":"pnpm dev","cwd":"apps/web","service":"webdev"},` +
	`{"name":"api","port":8080,"healthPath":"/health","command":"go run .","cwd":"apps/api","service":"apidev"},` +
	`{"name":"admin","port":8081,"healthPath":"/status","command":"go run ./admin","cwd":"apps/api","service":"apidev"}]}`

func TestDevServer_AStartOnAVariantBDevService_DeclaresTheServerForThatDevService(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised("")
	runDevServer(t, c, DevServerParams{Action: "start", Hostname: "apidev", Root: "/var/www/ws", Name: "api", Command: "go run .", Port: 8080, WorkDir: "/var/www/ws/apps/api"})
	d, err := devsup.ParseDeclaration([]byte(c.Declaration))
	if err != nil || len(d.Servers) != 1 || d.Servers[0].Service != "apidev" {
		t.Errorf("declaration = %s (%v), want api declared for apidev", c.Declaration, err)
	}

	c = devservertest.NewSupervised("")
	runDevServer(t, c, DevServerParams{Action: "start", Hostname: "apidev", Command: "go run .", Port: 8080})
	if d, err := devsup.ParseDeclaration([]byte(c.Declaration)); err != nil || d.Servers[0].Service != "" {
		t.Errorf("variant A declaration = %s (%v), want no service: the one supervisor reading it runs it", c.Declaration, err)
	}
}

func variantBRead(t *testing.T, hostname, decl string, running ...string) *DevServers {
	t.Helper()
	ds := &DevServers{Hostname: hostname, LoopbackOnly: map[int]bool{}}
	var err error
	if ds.Declared, err = devsup.ParseDeclaration([]byte(decl)); err != nil {
		t.Fatal(err)
	}
	ds.State.Servers = map[string]devsup.ServerState{}
	for _, n := range running {
		ds.State.Servers[n] = devsup.ServerState{Status: devsup.StatusRunning}
	}
	return ds
}

func TestDevServers_AnotherDevServicesServers_AreNotThisDevServicesServers(t *testing.T) {
	t.Parallel()
	ds := variantBRead(t, "apidev", variantBDeclaration, "api", "admin")
	ports := ds.DevPorts()
	if len(ports) != 2 || ports[0].Name != "api" || ports[0].HealthPath != "/health" || ports[1].Name != "admin" || ports[1].HealthPath != "/status" {
		t.Errorf("DevPorts = %+v, want apidev's api and admin, each with its declared health path", ports)
	}

	webdev := variantBRead(t, "webdev", variantBDeclaration, "web")
	name, err := resolveServerName(DevServerParams{Hostname: "webdev"}, webdev, false)
	if err != nil || name != "web" {
		t.Errorf("resolveServerName = %q, %v, want web — webdev's only server", name, err)
	}
	if webdev.DevPorts() != nil {
		t.Errorf("DevPorts = %+v, want none: webdev runs one server, verify keeps its one check", webdev.DevPorts())
	}
}

// localCurl is a dev container whose HTTP probes really run: every command
// that curls runs in a local sh against a local server, everything else is
// answered by the fake container. Its PATH puts a date first that answers
// GNU's +%s%N on every OS, as the container's coreutils do.
type localCurl struct {
	*devservertest.Container
	bin string
}

func newLocalCurl(t *testing.T, c *devservertest.Container) *localCurl {
	t.Helper()
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl is not installed")
	}
	bin := t.TempDir()
	date := "#!/bin/sh\nif [ \"$1\" = '+%s%N' ]; then echo \"$(/bin/date +%s)000000000\"; else exec /bin/date \"$@\"; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "date"), []byte(date), 0o755); err != nil {
		t.Fatal(err)
	}
	return &localCurl{Container: c, bin: bin}
}

func (l *localCurl) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	if !strings.Contains(command, "curl ") || strings.Contains(command, "zcp-devsup") {
		return l.Container.ExecSSH(ctx, hostname, command)
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Env = append(os.Environ(), "PATH="+l.bin+":"+os.Getenv("PATH"))
	out, err := cmd.Output()
	if err != nil {
		return out, fmt.Errorf("probe: %w", err)
	}
	return out, nil
}

// slowServer answers every request with 200 after delay; its port.
func slowServer(t *testing.T, delay time.Duration) int {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(delay):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "http://"))
	n, _ := strconv.Atoi(port)
	return n
}

func declaredOn(port int) string {
	return fmt.Sprintf(`{"version":1,"servers":[{"name":"apidev","port":%d,"command":"npm run dev","cwd":"."}]}`, port)
}

func TestDevServer_AFirstResponseSlowerThanTwoSeconds_PassesTheStartProbe(t *testing.T) {
	t.Parallel()
	port := slowServer(t, 3*time.Second)
	ssh := newLocalCurl(t, devservertest.NewSupervised(declaredOn(port)))

	result, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("apidev"), "p1",
		DevServerParams{Action: "restart", Hostname: "apidev", WaitSeconds: 6})
	if err != nil {
		t.Fatal(err)
	}
	if !result.HealthOK || result.HealthStatus != 200 || result.Reason != "" {
		t.Errorf("a server answering in 3 s within a 6 s wait: healthOK=%v status=%d reason=%q, want a passing probe", result.HealthOK, result.HealthStatus, result.Reason)
	}
}

func TestDevServer_AListenerThatAnswersSlowerThanTheWait_IsReportedSlowNotConnectionRefused(t *testing.T) {
	t.Parallel()
	port := slowServer(t, 10*time.Second)
	ssh := newLocalCurl(t, devservertest.NewSupervised(declaredOn(port)))

	result, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("apidev"), "p1",
		DevServerParams{Action: "restart", Hostname: "apidev", WaitSeconds: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != reasonHealthProbeSlow {
		t.Errorf("reason = %q, want %q for a listener that accepts and has not answered yet", result.Reason, reasonHealthProbeSlow)
	}
	if !strings.Contains(result.Message, "accepts connections") || strings.Contains(result.Message, "refused") {
		t.Errorf("message = %q, want it to say the port accepts connections", result.Message)
	}
}

func TestDevServer_StatusOfAServerSlowerThanItsProbe_SaysSlowNotConnectionRefused(t *testing.T) {
	t.Parallel()
	port := slowServer(t, 10*time.Second)
	ssh := newLocalCurl(t, devservertest.NewSupervised(declaredOn(port)))

	result, err := ExecuteDevServer(context.Background(), ssh, mockClientWithServices("apidev"), "p1",
		DevServerParams{Action: "status", Hostname: "apidev"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Reason != reasonSlow || !strings.Contains(result.Message, "accepts connections") {
		t.Errorf("reason/message = %q/%q, want %q and a port that accepts connections", result.Reason, result.Message, reasonSlow)
	}
}

func TestDevServer_AStartsLogTail_HoldsOnlyTheCurrentRun(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(oneServer)
	c.HTTP = map[int]int{3000: 0}
	c.Logs = map[string][]string{"apidev": {
		"--- zcp-devsup: start #1 at 2026-10-14T20:00:00Z: npm run start:dev",
		"Error: Cannot find module 'old'",
		"--- zcp-devsup: start #2 at 2026-10-14T21:00:00Z: npm run start:dev",
		"compiling...",
	}}
	result := runDevServer(t, c, DevServerParams{Action: "restart", Hostname: "apidev", WaitSeconds: 1})
	if strings.Contains(result.LogTail, "Cannot find module") || !strings.HasPrefix(result.LogTail, "--- zcp-devsup: start #2") {
		t.Errorf("logTail = %q, want only the run that start #2 began", result.LogTail)
	}
	logs := runDevServer(t, c, DevServerParams{Action: "logs", Hostname: "apidev"})
	if !strings.Contains(logs.LogTail, "Cannot find module") {
		t.Errorf("a log read = %q, want every line it asked for", logs.LogTail)
	}
}

func TestDevServer_APassingStartProbe_CarriesAShortLogTail(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(oneServer)
	c.Logs = map[string][]string{"apidev": {"ready"}}
	runDevServer(t, c, DevServerParams{Action: "restart", Hostname: "apidev"})
	if sentWith(c, "zcp-devsup ctl logs 'apidev' --lines 8 ") != 1 || sentWith(c, "--lines 40") != 0 {
		t.Errorf("log reads = %q, want one of 8 lines after a passing probe", c.Sent())
	}

	c = devservertest.NewSupervised(oneServer)
	c.HTTP = map[int]int{3000: 0}
	c.Logs = map[string][]string{"apidev": {"compiling"}}
	runDevServer(t, c, DevServerParams{Action: "restart", Hostname: "apidev", WaitSeconds: 1})
	if sentWith(c, "--lines 40") != 1 {
		t.Errorf("log reads = %q, want 40 lines after a failed probe", c.Sent())
	}
}

func TestDevServer_StatusWithoutANameOnSeveralServers_ReportsEveryServer(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(twoServers)
	c.Crash = map[string]bool{"api": true}
	runDevServer(t, c, DevServerParams{Action: "restart", Hostname: "appdev", Name: "api"})

	result := runDevServer(t, c, DevServerParams{Action: "status", Hostname: "appdev"})
	if !result.Unreported {
		t.Errorf("a status of several servers reports none of them alone: %+v", result)
	}
	for _, want := range []string{"web on appdev is running on :3000 (HTTP 200 on /)", "api on appdev crashed (exit 1)"} {
		if !strings.Contains(result.Message, want) {
			t.Errorf("message = %q, want %q", result.Message, want)
		}
	}
}

func TestDevServer_StatusSaysHowOftenTheSupervisorRestartedTheServer(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(oneServer)
	c.Servers["apidev"].Restarts = 136
	result := runDevServer(t, c, DevServerParams{Action: "status", Hostname: "apidev"})
	if result.Restarts != 136 || !strings.Contains(result.Message, "restarted it 136 times") {
		t.Errorf("restarts/message = %d/%q, want the supervisor's 136 restarts", result.Restarts, result.Message)
	}
}
