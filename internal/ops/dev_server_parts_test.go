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
