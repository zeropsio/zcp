// Tests for: internal/ops/dev_server.go, dev_server_supervisor.go — what a
// dev service with several parts needs from its dev servers: each dev
// service's own servers, the declared health path, a slow first response told
// apart from a refused one, a log tail of the current run, and a coded refusal
// when a declared port is taken.
package ops

import (
	"testing"

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
