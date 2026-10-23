// Tests for: dev_server.go, dev_server_envelope.go, dev_server_memory.go — a
// dev server in a dev-pool lease (docs/spec-workflows.md §8 "Dev pools (DP)",
// docs/spec-mate.md §2.8a): the lease alias is the host the supervisor is
// driven over, and the pool is the service every platform fact comes from.
package ops

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/devsup"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
)

// previewDeclaration declares web (:3000) with a preview base of 9000.
const previewDeclaration = `{"version":1,"previewPortBase":9000,"servers":[{"name":"web","port":3000,"command":"pnpm dev"}]}`

// leaseState is a state directory holding the record of lease devpool-3.
func leaseState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := writeLeaseRecord(dir, leaseRecord{
		PoolHostname: "devpool", PoolServiceID: "svc-pool", ContainerID: "c-3", ContainerNumber: 3,
		ContainerHostname: "node-id-3.runtime.devpool.zerops", Lease: "lease-secret", Branch: "feat-x", AcquiredAt: "2026-10-23T12:00:00Z",
	}); err != nil {
		t.Fatalf("write lease record: %v", err)
	}
	return dir
}

// poolClient is a project whose only service is the pool devpool: it has no
// service named after the lease alias.
func poolClient(maxRAM float64, ports ...platform.Port) *platform.Mock {
	pool := platform.ServiceStack{
		ID: "svc-pool", Name: "devpool", ProjectID: "p1", Status: "ACTIVE", SubdomainAccess: true, Ports: ports,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER", ServiceStackTypeVersionName: "swarm@1"},
	}
	if maxRAM > 0 {
		pool.CustomAutoscaling = &platform.CustomAutoscaling{MaxRAM: maxRAM}
	}
	return platform.NewMock().
		WithProject(&platform.Project{ID: "p1", Name: "test", SubdomainHost: "abc1.prg1.zerops.app"}).
		WithServices([]platform.ServiceStack{pool})
}

// hostRecorder records the host every SSH command is sent to.
type hostRecorder struct {
	*devservertest.Container
	mu    sync.Mutex
	hosts []string
}

func (h *hostRecorder) ExecSSH(ctx context.Context, hostname, command string) ([]byte, error) {
	h.mu.Lock()
	h.hosts = append(h.hosts, hostname)
	h.mu.Unlock()
	return h.Container.ExecSSH(ctx, hostname, command)
}

func (h *hostRecorder) sentTo() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.hosts)
}

func TestDevServer_LeaseAlias_DrivesSupervisorOverAlias(t *testing.T) {
	t.Parallel()
	stateDir := leaseState(t)
	for _, action := range []string{"start", "status", "stop", "logs"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(previewDeclaration)
			c.Servers["web"].PreviewPort = 9003
			c.Logs = map[string][]string{"web": {"ready"}}
			rec := &hostRecorder{Container: c}

			result, err := ExecuteDevServer(context.Background(), rec, poolClient(0), "p1",
				DevServerParams{Action: action, Hostname: "devpool-3", StateDir: stateDir})
			if err != nil {
				t.Fatalf("%s on a lease alias: %v", action, err)
			}

			hosts := rec.sentTo()
			if len(hosts) == 0 {
				t.Fatal("nothing was sent over SSH")
			}
			for _, h := range hosts {
				if h != "devpool-3" {
					t.Errorf("SSH was sent to %q, want the alias devpool-3 only (all: %v)", h, hosts)
				}
			}
			if sentWith(c, "zcp-devsup ctl") == 0 {
				t.Errorf("the supervisor was never driven: %v", c.Sent())
			}
			if result.Hostname != "devpool-3" || result.Pool != "devpool" {
				t.Errorf("hostname/pool = %q/%q, want devpool-3/devpool", result.Hostname, result.Pool)
			}
			if result.PreviewPort != 9003 {
				t.Errorf("previewPort = %d, want the supervisor's 9003", result.PreviewPort)
			}
		})
	}
}

func TestDevServer_LeaseAlias_NeedsNoServiceOfThatName(t *testing.T) {
	t.Parallel()
	stateDir := leaseState(t)
	c := devservertest.NewSupervised(previewDeclaration)
	// devpool-4 has the shape of an alias but no record: it is looked up as the
	// service it would have to be, as before, and is not found.
	_, err := ExecuteDevServer(context.Background(), c, poolClient(0), "p1",
		DevServerParams{Action: "status", Hostname: "devpool-4", StateDir: stateDir})
	var pe *platform.PlatformError
	if !errors.As(err, &pe) || pe.Code != platform.ErrServiceNotFound {
		t.Fatalf("an unrecorded alias: err = %v, want service not found", err)
	}
	if len(c.Sent()) != 0 {
		t.Errorf("nothing may be sent to an unrecorded alias: %v", c.Sent())
	}
}

func TestDevServer_LeaseAlias_WithoutSupervisorIsNotMovedOntoOne(t *testing.T) {
	t.Parallel()
	stateDir := leaseState(t)
	c := &devservertest.Container{Declaration: previewDeclaration}

	_, err := ExecuteDevServer(context.Background(), c, poolClient(0), "p1",
		DevServerParams{Action: "start", Hostname: "devpool-3", StateDir: stateDir, Command: "pnpm dev", Port: 3000})

	// The move onto the supervisor redeploys a dev service; a lease is a pool
	// container, whose supervisor belongs to the pool's own zerops.yaml.
	var noSup *NoSupervisorError
	var pe *platform.PlatformError
	if errors.As(err, &noSup) || !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a refusal that is not NoSupervisorError", err)
	}
	if !strings.Contains(pe.Message, "devpool") || !strings.Contains(pe.Suggestion, "run.start") {
		t.Errorf("the refusal must name the pool and its run.start: %q / %q", pe.Message, pe.Suggestion)
	}
}

func TestLeasePreviewURL_BuiltFromThePoolsSubdomainAndThePreviewPort(t *testing.T) {
	t.Parallel()
	httpPorts := []platform.Port{{Port: 9001, HTTPSupport: true}, {Port: 9002, HTTPSupport: true}, {Port: 9003, HTTPSupport: true}}
	tests := []struct {
		name   string
		client *platform.Mock
		port   int
		want   string
	}{
		{name: "the slot's port is declared with httpSupport", client: poolClient(0, httpPorts...), port: 9003, want: "https://devpool-abc1-9003.prg1.zerops.app"},
		{name: "a port the pool does not declare", client: poolClient(0, httpPorts...), port: 9009},
		{name: "a port without httpSupport", client: poolClient(0, platform.Port{Port: 9003}), port: 9003},
		{name: "no preview port", client: poolClient(0, httpPorts...), port: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := LeasePreviewURL(context.Background(), tt.client, "p1", "devpool", tt.port)
			if got != tt.want {
				t.Fatalf("LeasePreviewURL = %q, want %q", got, tt.want)
			}
		})
	}

	t.Run("the pool's subdomain is off", func(t *testing.T) {
		t.Parallel()
		off := poolClient(0, httpPorts...)
		off.WithServices([]platform.ServiceStack{{ID: "svc-pool", Name: "devpool", ProjectID: "p1", Status: "ACTIVE", Ports: httpPorts}})
		if got := LeasePreviewURL(context.Background(), off, "p1", "devpool", 9003); got != "" {
			t.Fatalf("LeasePreviewURL = %q with the subdomain off, want none", got)
		}
	})
}

func TestDevServerStates_LeaseAlias_PreviewServerCarriesThePreviewURL(t *testing.T) {
	t.Parallel()
	stateDir := leaseState(t)
	c := devservertest.NewSupervised(previewDeclaration)
	c.Servers["web"].PreviewPort = 9003
	client := poolClient(0, platform.Port{Port: 9003, HTTPSupport: true})
	result, err := ExecuteDevServer(context.Background(), c, client, "p1",
		DevServerParams{Action: "status", Hostname: "devpool-3", StateDir: stateDir})
	if err != nil {
		t.Fatal(err)
	}

	states := DevServerStates(context.Background(), client, "p1", result)

	if len(states) != 1 || states[0].Name != "web" || states[0].Port != 3000 {
		t.Fatalf("states = %+v, want web on its declared port 3000", states)
	}
	if want := "https://devpool-abc1-9003.prg1.zerops.app"; states[0].URL != want {
		t.Errorf("url = %q, want the preview URL %q", states[0].URL, want)
	}
}

func TestDevServerMemory_LeaseAdviceNamesPool(t *testing.T) {
	t.Parallel()
	stateDir := leaseState(t)
	c := memoryContainer(6, 5, map[string]float64{"web": 2.5})
	// The pool's max RAM is the ceiling: it scales every container of the pool.
	client := poolClient(6)

	_, err := ExecuteDevServer(context.Background(), c, client, "p1",
		DevServerParams{Action: "start", Hostname: "devpool-3", Name: "api", StateDir: stateDir})

	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("want a memory refusal, got %v", err)
	}
	for _, want := range []string{"Not starting api on devpool-3", "6.0 GiB"} {
		if !strings.Contains(pe.Message, want) {
			t.Errorf("message %q lacks %q", pe.Message, want)
		}
	}
	if !strings.Contains(pe.Message, "devpool") || !strings.Contains(pe.Message, "every container of the pool") {
		t.Errorf("the advice must name the pool and say its max RAM scales every container: %q", pe.Message)
	}
	if strings.Contains(pe.Message, "the service's max RAM") {
		t.Errorf("a lease is not advised to raise a service's max RAM: %q", pe.Message)
	}
	if !strings.Contains(pe.Suggestion, `serviceHostname="devpool"`) || !strings.Contains(pe.Suggestion, "action=stop") {
		t.Errorf("suggestion %q must scale the pool and still offer stopping a server", pe.Suggestion)
	}
}

func TestDevServerMemory_LeaseCeilingIsThePoolsMaxRAM(t *testing.T) {
	t.Parallel()
	stateDir := leaseState(t)
	// 4 GiB granted, 3 used, web holds 2.5: against the grant it is refused; the
	// pool can grow to 8 GiB, which leaves room.
	c := memoryContainer(4, 3, map[string]float64{"web": 2.5})
	if _, err := ExecuteDevServer(context.Background(), c, poolClient(8), "p1",
		DevServerParams{Action: "start", Hostname: "devpool-3", Name: "api", StateDir: stateDir}); err != nil {
		t.Fatalf("an 8 GiB pool ceiling leaves room for api: %v", err)
	}
}

func TestDeclareDevServer_KeepsThePreviewPortBaseAndMarks(t *testing.T) {
	t.Parallel()
	d, err := devsup.ParseDeclaration([]byte(`{"version":1,"previewPortBase":9000,"servers":[{"name":"web","port":3000,"command":"a","preview":true}]}`))
	if err != nil {
		t.Fatal(err)
	}
	// A start that adds a server, and one that changes web's command, both
	// write the file back: the repo's preview base and mark must survive.
	for name, want := range map[string]devsup.Server{
		"added":   {Name: "api", Port: 8080, Command: "b"},
		"changed": {Name: "web", Port: 3000, Command: "c", Preview: true},
	} {
		got, changed := DeclareDevServer(d, want)
		if !changed {
			t.Fatalf("%s: not written", name)
		}
		if got.PreviewPortBase != 9000 {
			t.Errorf("%s: previewPortBase = %d, want 9000", name, got.PreviewPortBase)
		}
		if i := slices.IndexFunc(got.Servers, func(s devsup.Server) bool { return s.Name == "web" }); i < 0 || !got.Servers[i].Preview {
			t.Errorf("%s: web lost its preview mark: %+v", name, got.Servers)
		}
	}
}
