// Tests for: dev_server.go, dev_server_envelope.go, dev_server_memory.go — a
// dev server in a dev-pool lease (docs/spec-workflows.md §8 "Dev pools (DP)",
// docs/spec-mate.md §2.8a): the lease alias is the host the supervisor is
// driven over, and the pool is the service every platform fact comes from.
package ops

import (
	"context"
	"errors"
	"maps"
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
		// A pool reports its preview ports without httpSupport while its
		// subdomain is off — the flag follows the subdomain — and right after
		// it was switched on (2026-10-27, every pool of three sessions): the
		// lease's preview port, previewPortBase + its stable number, is
		// declared all the same, and its subdomain serves it.
		{name: "a declared preview port the pool reports without httpSupport", client: poolClient(0, platform.Port{Port: 9003, Protocol: "tcp"}), port: 9003, want: "https://devpool-abc1-9003.prg1.zerops.app"},
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

// wiringRepo is the repository of the wiring tests' branch feat-x.
const wiringRepo = "https://github.com/acme/app"

// wiringState is a state directory holding branch feat-x of wiringRepo on two
// pools — hqpool-7 and webpool-3, the group — and apipool-2 on another branch.
func wiringState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, r := range []leaseRecord{
		{PoolHostname: "hqpool", PoolServiceID: "svc-hq", ContainerID: "c-7", ContainerNumber: 7, ContainerHostname: "node-id-7.runtime.hqpool.zerops",
			Lease: "hq-lease", Branch: "feat-x", Repo: wiringRepo, AcquiredAt: "2026-10-23T11:00:00Z"},
		{PoolHostname: "webpool", PoolServiceID: "svc-web", ContainerID: "c-3", ContainerNumber: 3, ContainerHostname: "node-id-3.runtime.webpool.zerops",
			Lease: "web-lease", Branch: "feat-x", Repo: wiringRepo, AcquiredAt: "2026-10-23T12:00:00Z"},
		{PoolHostname: "apipool", PoolServiceID: "svc-api", ContainerID: "c-2", ContainerNumber: 2, ContainerHostname: "node-id-2.runtime.apipool.zerops",
			Lease: "api-lease", Branch: "other", Repo: wiringRepo, AcquiredAt: "2026-10-23T10:00:00Z"},
	} {
		if err := writeLeaseRecord(dir, r); err != nil {
			t.Fatalf("write lease record: %v", err)
		}
	}
	return dir
}

// siblingDeclaration declares web (:3000) and the variables web's servers read:
// hq's host (the branch leases hqpool), the api's (it does not) and its own.
const siblingDeclaration = `{"version":1,"siblingEnv":{"HQ_HOST":"hqpool","API_HOST":"apipool","SELF_HOST":"webpool"},` +
	`"servers":[{"name":"web","port":3000,"command":"pnpm dev"}]}`

// wantWebOverlay is the overlay webpool-3's servers get from siblingDeclaration.
var wantWebOverlay = map[string]string{"HQ_HOST": "node-id-7.runtime.hqpool.zerops", "SELF_HOST": "node-id-3.runtime.webpool.zerops"}

// firstSent is the index of the first command containing every part, -1 when none.
func firstSent(cmds []string, parts ...string) int {
	for i, cmd := range cmds {
		all := true
		for _, p := range parts {
			all = all && strings.Contains(cmd, p)
		}
		if all {
			return i
		}
	}
	return -1
}

func TestDevServer_LeaseSiblingEnv_SendsFullOverlayBeforeStart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		params DevServerParams
		// declares: the start writes the declaration and reloads first.
		declares bool
	}{
		{name: "a declared server", params: DevServerParams{Action: "start", Name: "web"}},
		{name: "a restart of a declared server", params: DevServerParams{Action: "restart", Name: "web"}},
		{name: "a new server, declared and reloaded", params: DevServerParams{Action: "start", Name: "admin", Command: "pnpm admin", Port: 4200}, declares: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(siblingDeclaration)
			p := tt.params
			p.Hostname, p.StateDir = "webpool-3", wiringState(t)

			result, err := ExecuteDevServer(context.Background(), c, poolClient(0), "p1", p)
			if err != nil {
				t.Fatalf("%s: %v", p.Action, err)
			}

			cmds := c.Sent()
			env := firstSent(cmds, "zcp-devsup ctl 'env'")
			if env < 0 {
				t.Fatalf("no ctl env was sent: %v", cmds)
			}
			want := "zcp-devsup ctl 'env' '--set' 'HQ_HOST=node-id-7.runtime.hqpool.zerops' '--set' 'SELF_HOST=node-id-3.runtime.webpool.zerops' --json"
			if !strings.Contains(cmds[env], want) {
				t.Errorf("ctl env = %q, want the full overlay, every value quoted:\n%s", cmds[env], want)
			}
			start := firstSent(cmds, "zcp-devsup ctl '"+p.Action+"' '"+p.Name+"'")
			if start < 0 {
				start = firstSent(cmds, "zcp-devsup ctl 'start' '"+p.Name+"'")
			}
			if start < 0 || env > start {
				t.Errorf("ctl env (%d) must come before the start (%d): %v", env, start, cmds)
			}
			if tt.declares {
				write, reload := firstSent(cmds, "dev-servers.json.tmp"), firstSent(cmds, "zcp-devsup ctl 'reload'")
				if write < 0 || reload < 0 || env > write || env > reload {
					t.Errorf("ctl env (%d) must come before the declaration's write (%d) and the reload (%d): %v", env, write, reload, cmds)
				}
			}
			if !maps.Equal(c.Env, wantWebOverlay) {
				t.Errorf("the supervisor's overlay = %v, want %v", c.Env, wantWebOverlay)
			}
			if result.SiblingEnv == nil || !maps.Equal(result.SiblingEnv.Set, wantWebOverlay) || !slices.Equal(result.SiblingEnv.LeftToContainerEnv, []string{"API_HOST"}) {
				t.Errorf("siblingEnv = %+v, want set %v and API_HOST left to the container's env", result.SiblingEnv, wantWebOverlay)
			}
			if !result.Running {
				t.Errorf("result = %+v, want the server running", result)
			}
		})
	}
}

func TestDevServer_LeaseSiblingEnv_PoolNotHeld_LeftToContainerEnv(t *testing.T) {
	t.Parallel()
	// apipool is leased, but on another branch: it is no member of feat-x's
	// group, so API_HOST keeps the container's own value (the part's stage).
	c := devservertest.NewSupervised(siblingDeclaration)
	result, err := ExecuteDevServer(context.Background(), c, poolClient(0), "p1",
		DevServerParams{Action: "start", Hostname: "webpool-3", Name: "web", StateDir: wiringState(t)})
	if err != nil {
		t.Fatal(err)
	}
	if _, set := c.Env["API_HOST"]; set {
		t.Errorf("API_HOST is in the overlay (%v): another branch's lease of apipool is no sibling", c.Env)
	}
	if strings.Contains(strings.Join(c.Sent(), "\n"), "apipool") {
		t.Errorf("a command names apipool: %v", c.Sent())
	}
	if result.SiblingEnv == nil || !slices.Equal(result.SiblingEnv.LeftToContainerEnv, []string{"API_HOST"}) {
		t.Fatalf("siblingEnv = %+v, want API_HOST left to the container's env", result.SiblingEnv)
	}
	for _, want := range []string{"API_HOST", "apipool", "container's own", "HQ_HOST=node-id-7.runtime.hqpool.zerops", "feat-x"} {
		if !strings.Contains(result.Message, want) {
			t.Errorf("message %q lacks %q", result.Message, want)
		}
	}
}

func TestDevServer_LeaseEmptyOverlay_StillSentToReplaceAPreviousHolders(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		decl string
	}{
		{name: "no siblingEnv declared", decl: previewDeclaration},
		{name: "siblingEnv names only pools the branch does not lease", decl: `{"version":1,"siblingEnv":{"API_HOST":"apipool"},"servers":[{"name":"web","port":3000,"command":"pnpm dev"}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(tt.decl)
			// What the container's previous holder left on its supervisor.
			c.Env = map[string]string{"HQ_HOST": "node-id-9.runtime.hqpool.zerops"}

			if _, err := ExecuteDevServer(context.Background(), c, poolClient(0), "p1",
				DevServerParams{Action: "start", Hostname: "webpool-3", Name: "web", StateDir: wiringState(t)}); err != nil {
				t.Fatal(err)
			}
			cmds := c.Sent()
			env, start := firstSent(cmds, "zcp-devsup ctl 'env' '--clear' --json"), firstSent(cmds, "zcp-devsup ctl 'start' 'web'")
			if env < 0 || start < 0 || env > start {
				t.Fatalf("want ctl env --clear before the start, got %v", cmds)
			}
			if len(c.Env) != 0 {
				t.Errorf("the overlay = %v, want the previous holder's cleared", c.Env)
			}
		})
	}
}

func TestDevServer_SupervisorWithoutEnvCapability_NotSentEnv(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		decl     string
		wantLeft []string
	}{
		{name: "no siblingEnv declared", decl: previewDeclaration},
		{name: "siblingEnv names only pools the branch does not lease", decl: `{"version":1,"siblingEnv":{"API_HOST":"apipool"},"servers":[{"name":"web","port":3000,"command":"pnpm dev"}]}`, wantLeft: []string{"API_HOST"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(tt.decl)
			c.Capabilities = nil // a pool image pinning a zcp-devsup older than `ctl env`

			result, err := ExecuteDevServer(context.Background(), c, poolClient(0), "p1",
				DevServerParams{Action: "start", Hostname: "webpool-3", Name: "web", StateDir: wiringState(t)})
			if err != nil {
				t.Fatalf("an empty overlay starts as before on an older supervisor: %v", err)
			}
			if n := sentWith(c, "zcp-devsup ctl 'env'"); n != 0 {
				t.Errorf("ctl env sent %d times to a supervisor that does not list it: %v", n, c.Sent())
			}
			if !result.Running {
				t.Errorf("result = %+v, want web running", result)
			}
			if len(tt.wantLeft) > 0 && (result.SiblingEnv == nil || !slices.Equal(result.SiblingEnv.LeftToContainerEnv, tt.wantLeft)) {
				t.Errorf("siblingEnv = %+v, want %v left to the container's env", result.SiblingEnv, tt.wantLeft)
			}
		})
	}
}

func TestDevServer_SupervisorWithoutEnvCapability_NonEmptyOverlay_RefusedWithRecovery(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"start", "restart"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(siblingDeclaration)
			c.Capabilities = nil

			_, err := ExecuteDevServer(context.Background(), c, poolClient(0), "p1",
				DevServerParams{Action: action, Hostname: "webpool-3", Name: "web", StateDir: wiringState(t)})
			var pe *platform.PlatformError
			if !errors.As(err, &pe) {
				t.Fatalf("err = %v, want a refusal", err)
			}
			for _, want := range []string{"webpool-3", "webpool", "v9.300.0", "HQ_HOST", "env"} {
				if !strings.Contains(pe.Message, want) {
					t.Errorf("message %q lacks %q", pe.Message, want)
				}
			}
			for _, want := range []string{"zcp-devsup", "run.prepareCommands", `zerops_lease action=release lease="webpool-3"`, `zerops_lease action=rollout pool="webpool"`, "acquire"} {
				if !strings.Contains(pe.Suggestion, want) {
					t.Errorf("suggestion %q lacks %q", pe.Suggestion, want)
				}
			}
			for _, banned := range []string{"zcp-devsup ctl 'env'", "dev-servers.json.tmp", "zcp-devsup ctl 'reload'", "zcp-devsup ctl 'start'", "zcp-devsup ctl 'restart'"} {
				if n := sentWith(c, banned); n != 0 {
					t.Errorf("a refused start sent %q: %v", banned, c.Sent())
				}
			}
		})
	}
}

func TestDevServer_LeaseSiblingEnv_ChangedOverlay_ReadsStatusBeforeStart(t *testing.T) {
	t.Parallel()
	// web runs under the overlay a previous start set; the new one restarts it
	// in the supervisor's own goroutine, after the env answer.
	c := devservertest.NewSupervised(siblingDeclaration)
	c.Env = map[string]string{"HQ_HOST": "node-id-9.runtime.hqpool.zerops"}

	result, err := ExecuteDevServer(context.Background(), c, poolClient(0), "p1",
		DevServerParams{Action: "start", Hostname: "webpool-3", Name: "web", StateDir: wiringState(t)})
	if err != nil {
		t.Fatal(err)
	}
	cmds := c.Sent()
	env, start := firstSent(cmds, "zcp-devsup ctl 'env'"), firstSent(cmds, "zcp-devsup ctl 'start' 'web'")
	status := -1
	for i := env + 1; env >= 0 && i < len(cmds); i++ {
		if strings.Contains(cmds[i], "zcp-devsup ctl 'status' --json") {
			status = i
			break
		}
	}
	if env < 0 || status < 0 || start < 0 || status > start {
		t.Fatalf("want ctl env, then a status read, then the start: %v", cmds)
	}
	if got := c.Servers["web"]; got.Restarts != 1 || got.LastRestartReason != "env changed" {
		t.Errorf("web restarts = %d (%q), want the supervisor's one restart under the new overlay, and no second", got.Restarts, got.LastRestartReason)
	}
	if !result.Running {
		t.Errorf("result = %+v, want web running", result)
	}
}

func TestDevServer_DevServiceNotALease_NoEnvCall(t *testing.T) {
	t.Parallel()
	// A dev service's declaration may carry siblingEnv (a repository shared
	// with pools): zcp sends an overlay only to a lease's supervisor.
	c := devservertest.NewSupervised(`{"version":1,"siblingEnv":{"HQ_HOST":"hqpool"},"servers":[{"name":"appdev","port":3000,"command":"pnpm dev"}]}`)
	result := runDevServer(t, c, DevServerParams{Action: "start", Hostname: "appdev", StateDir: wiringState(t)})
	if n := sentWith(c, "zcp-devsup ctl 'env'"); n != 0 {
		t.Errorf("ctl env sent to a dev service: %v", c.Sent())
	}
	if result.SiblingEnv != nil || len(c.Env) != 0 {
		t.Errorf("siblingEnv = %+v, overlay %v; want none on a dev service", result.SiblingEnv, c.Env)
	}
}

func TestDevServer_LeaseDeclaresItsPoolAsService(t *testing.T) {
	t.Parallel()
	// The repository's one declaration holds every part's servers; a lease's
	// supervisor runs only those naming its pool (or none).
	c := devservertest.NewSupervised(`{"version":1,"servers":[{"name":"hq","port":8080,"command":"go run .","cwd":"apps/hq","service":"hqpool"}]}`)
	stateDir := leaseState(t)

	if _, err := ExecuteDevServer(context.Background(), c, poolClient(0), "p1",
		DevServerParams{Action: "start", Hostname: "devpool-3", Name: "web", Command: "pnpm dev", Port: 3000, StateDir: stateDir}); err != nil {
		t.Fatal(err)
	}
	d, err := devsup.ParseDeclaration([]byte(c.Declaration))
	if err != nil {
		t.Fatal(err)
	}
	services := map[string]string{}
	for _, s := range d.Servers {
		services[s.Name] = s.Service
	}
	if want := map[string]string{"hq": "hqpool", "web": "devpool"}; !maps.Equal(services, want) {
		t.Errorf("declared services = %v, want %v (the new entry names the lease's pool)", services, want)
	}
}

func TestDevServer_LeaseLeavesADeclaredEntryWithoutServiceAsIs(t *testing.T) {
	t.Parallel()
	c := devservertest.NewSupervised(previewDeclaration)

	if _, err := ExecuteDevServer(context.Background(), c, poolClient(0), "p1",
		DevServerParams{Action: "start", Hostname: "devpool-3", Name: "web", Command: "pnpm dev --host", StateDir: leaseState(t)}); err != nil {
		t.Fatal(err)
	}
	d, err := devsup.ParseDeclaration([]byte(c.Declaration))
	if err != nil || len(d.Servers) != 1 {
		t.Fatalf("declaration = %s (%v)", c.Declaration, err)
	}
	if got := d.Servers[0]; got.Command != "pnpm dev --host" || got.Service != "" {
		t.Errorf("web = %+v, want its new command and still no service", got)
	}
}

func TestDevServer_LeaseReadsItsPoolsEntries(t *testing.T) {
	t.Parallel()
	// A lease's supervisor runs the entries naming its pool, so a status
	// without a name finds web — never the alias, which no entry names.
	c := devservertest.NewSupervised(`{"version":1,"servers":[` +
		`{"name":"web","port":3000,"command":"pnpm dev","service":"devpool"},` +
		`{"name":"hq","port":8080,"command":"go run .","service":"hqpool"}]}`)

	result, err := ExecuteDevServer(context.Background(), c, poolClient(0), "p1",
		DevServerParams{Action: "status", Hostname: "devpool-3", StateDir: leaseState(t)})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if result.Name != "web" || !result.Running {
		t.Errorf("result = %+v, want web running (devpool's one entry)", result)
	}
	states := DevServerStates(context.Background(), poolClient(0), "p1", result)
	if len(states) == 0 || states[0].Name != "web" {
		t.Errorf("devServers = %+v, want web first, as devpool's declared entry", states)
	}
}

func TestDeclareDevServer_KeepsTheSiblingEnv(t *testing.T) {
	t.Parallel()
	d, err := devsup.ParseDeclaration([]byte(siblingDeclaration))
	if err != nil {
		t.Fatal(err)
	}
	got, changed := DeclareDevServer(d, devsup.Server{Name: "admin", Port: 4200, Command: "pnpm admin"})
	if !changed || !maps.Equal(got.SiblingEnv, d.SiblingEnv) {
		t.Errorf("declared = %+v (changed %v), want the repository's siblingEnv %v kept", got, changed, d.SiblingEnv)
	}
	got.SiblingEnv["HQ_HOST"] = "elsewhere"
	if d.SiblingEnv["HQ_HOST"] != "hqpool" {
		t.Error("the rewritten declaration shares its siblingEnv map with the one read")
	}
}

// TestDevServerStatus_StaleWiring_Named: a status of a lease whose servers run
// with an overlay naming a container no lease of its group holds — one whose
// lease zcp released while this lease could not be reached, or another
// branch's — says wiring: stale, names the variable and the call that sends the
// overlay again, and sends nothing. Wiring naming the group's own leases is
// not stale, nor an overlay no server runs under.
func TestDevServerStatus_StaleWiring_Named(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		env       map[string]string
		stopped   bool
		wantStale bool
	}{
		{name: "a container no lease of the group holds", env: map[string]string{"HQ_HOST": "node-id-9.runtime.hqpool.zerops", "SELF_HOST": "node-id-3.runtime.webpool.zerops"}, wantStale: true},
		{name: "another branch's lease", env: map[string]string{"API_HOST": "node-id-2.runtime.apipool.zerops"}, wantStale: true},
		{name: "the group's own leases", env: wantWebOverlay},
		{name: "no server runs under it", env: map[string]string{"HQ_HOST": "node-id-9.runtime.hqpool.zerops"}, stopped: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(siblingDeclaration)
			c.Env = tt.env
			if tt.stopped {
				c.Servers["web"].Status, c.Servers["web"].Desired = devsup.StatusStopped, devsup.DesiredStopped
			}

			result, err := ExecuteDevServer(context.Background(), c, poolClient(0), "p1",
				DevServerParams{Action: "status", Hostname: "webpool-3", Name: "web", StateDir: wiringState(t)})
			if err != nil {
				t.Fatal(err)
			}
			if got := result.Wiring == LeaseWiringStale; got != tt.wantStale {
				t.Errorf("wiring = %q, want stale=%v (message %q)", result.Wiring, tt.wantStale, result.Message)
			}
			if tt.wantStale {
				for name := range tt.env {
					if tt.env[name] != wantWebOverlay[name] && !strings.Contains(result.Message, name) {
						t.Errorf("message %q does not name %s", result.Message, name)
					}
				}
				if !strings.Contains(result.Message, `zerops_dev_server action=restart hostname="webpool-3" name="web"`) {
					t.Errorf("message %q does not name the call that sends the overlay again", result.Message)
				}
			}
			for _, banned := range []string{"zcp-devsup ctl 'env'", "zcp-devsup ctl 'reload'", "zcp-devsup ctl 'restart'"} {
				if n := sentWith(c, banned); n != 0 {
					t.Errorf("status sent %q: %v", banned, c.Sent())
				}
			}
			if !maps.Equal(c.Env, tt.env) {
				t.Errorf("the overlay = %v after a status, want %v untouched", c.Env, tt.env)
			}
		})
	}
}

// TestDevServer_ParkedLease_RefusedWithUnpark: every dev-server action on a
// lease its record says is parked is refused before anything is sent — its
// container is stopped, so ssh would only time out — naming the unpark.
func TestDevServer_ParkedLease_RefusedWithUnpark(t *testing.T) {
	t.Parallel()
	stateDir := leaseState(t)
	rec, err := loadLeaseRecord(stateDir, "devpool-3")
	if err != nil {
		t.Fatal(err)
	}
	rec.Parked, rec.ParkedAt = true, "2026-10-24T09:00:00Z"
	if err := writeLeaseRecord(stateDir, rec); err != nil {
		t.Fatal(err)
	}
	for _, action := range []string{"start", "status", "stop", "logs"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			c := devservertest.NewSupervised(previewDeclaration)
			_, err := ExecuteDevServer(context.Background(), c, poolClient(0), "p1",
				DevServerParams{Action: action, Hostname: "devpool-3", StateDir: stateDir})
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != platform.ErrPrerequisiteMissing {
				t.Fatalf("%s on a parked lease: err = %v, want a PREREQUISITE_MISSING refusal", action, err)
			}
			if !strings.Contains(pe.Message, "devpool-3 is parked") || !strings.Contains(pe.Suggestion, "zerops_lease action=unpark lease=devpool-3") {
				t.Errorf("refusal = %q / %q, want it to say the lease is parked and name the unpark", pe.Message, pe.Suggestion)
			}
			if sent := c.Sent(); len(sent) != 0 {
				t.Errorf("sent %v to a parked lease", sent)
			}
		})
	}
}
