// Tests for: integration — zerops_lease action=park / action=unpark through
// the full MCP server against a mock platform, a fake pool orchestrator and a
// fake leased container (docs/spec-workflows.md §8 DP-28): a parked lease is
// listed parked, refused by zerops_dev_server naming the unpark, comes back
// with its supervisor on unpark, and a release of it starts it for its guard.

package integration_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops/devservertest"
)

// parkIntSSH is the leased container: the fake dev container, whose checkout
// the release guard reads clean.
type parkIntSSH struct{ *devservertest.Container }

func (s parkIntSSH) ExecSSH(ctx context.Context, host, command string) ([]byte, error) {
	switch {
	case strings.Contains(command, "status --porcelain") && !strings.Contains(command, "rev-parse --verify HEAD"):
		return nil, nil
	case strings.Contains(command, "rev-list --count"):
		return []byte("0\n"), nil
	}
	return s.Container.ExecSSH(ctx, host, command)
}

func (s parkIntSSH) ExecSSHBackground(ctx context.Context, host, command string, _ time.Duration) ([]byte, error) {
	return s.ExecSSH(ctx, host, command)
}

func TestLease_Integration_ParkStatusUnparkRelease(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir the server resolves from the cwd)
	// and a swap of http.DefaultTransport (the seam to the orchestrator host).
	t.Chdir(t.TempDir())
	c := &devservertest.Container{Installed: true, SupervisorVersion: "v9.300.0", Formats: []int{1},
		Declaration: `{"version":1,"servers":[{"name":"web","port":3000,"command":"pnpm dev"}]}`}

	var mu sync.Mutex
	reserved, status := false, "ACTIVE"
	var calls []string
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+leaseIntToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		lock := ""
		if reserved {
			lock = "reserved"
		}
		container := `{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.devpool.zerops","status":"` + status + `",` +
			`"serviceId":"svc-pool","stackId":"stk","appVersionId":"av","current":true,"lockState":"` + lock + `","dirty":false`
		held := r.Header.Get("X-Swarm-Lease") == leaseIntLease && !r.URL.Query().Has("lease")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/container":
			_, _ = w.Write([]byte(`[` + container + `}]`))
		case r.Method == http.MethodGet && r.URL.Path == "/container/c-abc":
			_, _ = w.Write([]byte(container + `}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
			reserved = true
			_, _ = w.Write([]byte(strings.Replace(container, `"lockState":""`, `"lockState":"reserved"`, 1) + `,"lease":"` + leaseIntLease + `"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/c-abc/stop" && held:
			calls = append(calls, "stop")
			status = "STOPPED"
			c.Stop()
			_, _ = w.Write([]byte(strings.Replace(container, `"status":"ACTIVE"`, `"status":"STOPPED"`, 1) + `}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/c-abc/start" && held:
			calls = append(calls, "start")
			status = "ACTIVE"
			_, _ = w.Write([]byte(strings.Replace(container, `"status":"STOPPED"`, `"status":"ACTIVE"`, 1) + `}`))
		case r.Method == http.MethodPost && r.URL.Path == "/container/c-abc/release" && held:
			calls = append(calls, "release "+status+" reset="+r.URL.Query().Get("reset"))
			reserved = false
			_, _ = w.Write([]byte(container + `}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(orch.Close)
	target, err := url.Parse(orch.URL)
	if err != nil {
		t.Fatal(err)
	}
	orig := http.DefaultTransport
	http.DefaultTransport = orchestratorRoute{host: "orch.devpool.zerops", target: target, next: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })

	session, cleanup := setupTestServerWithDeploy(t, supervisorPoolMock(), defaultLogFetcher(), parkIntSSH{c})
	defer cleanup()

	type leaseView struct {
		Alias    string `json:"alias"`
		State    string `json:"state"`
		ParkedAt string `json:"parkedAt"`
		Note     string `json:"note"`
	}
	call := func(tool string, args map[string]any) (string, bool) {
		t.Helper()
		res := callAndGetResult(t, session, tool, args)
		text := getTextContent(t, res)
		for _, secret := range []string{leaseIntLease, leaseIntToken} {
			if strings.Contains(text, secret) {
				t.Fatalf("%s %v result carries a secret: %s", tool, args["action"], text)
			}
		}
		return text, res.IsError
	}
	lease := func(args map[string]any) leaseView {
		t.Helper()
		text, isErr := call("zerops_lease", args)
		if isErr {
			t.Fatalf("zerops_lease %v failed: %s", args["action"], text)
		}
		var out struct {
			Leases []leaseView `json:"leases"`
		}
		if err := json.Unmarshal([]byte(text), &out); err != nil || len(out.Leases) != 1 {
			t.Fatalf("zerops_lease %v = %s (%v), want one lease", args["action"], text, err)
		}
		return out.Leases[0]
	}

	lease(map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app", "branch": "feature-x"})
	startsBefore := c.SupervisorStarts

	if v := lease(map[string]any{"action": "park", "lease": "devpool-3"}); v.State != "parked" || v.ParkedAt == "" {
		t.Fatalf("park = %+v, want devpool-3 parked with its parkedAt", v)
	}
	if v := lease(map[string]any{"action": "status"}); v.State != "parked" || !strings.Contains(v.Note, "zerops_lease action=unpark lease=devpool-3") {
		t.Fatalf("status = %+v, want devpool-3 parked, naming its unpark", v)
	}
	text, isErr := call("zerops_dev_server", map[string]any{"action": "status", "hostname": "devpool-3"})
	if !isErr || !strings.Contains(text, "devpool-3 is parked") || !strings.Contains(text, "zerops_lease action=unpark lease=devpool-3") {
		t.Fatalf("zerops_dev_server on the parked lease = (error %v) %s, want a refusal naming the unpark", isErr, text)
	}

	if v := lease(map[string]any{"action": "unpark", "lease": "devpool-3"}); v.State != "held" || v.ParkedAt != "" {
		t.Fatalf("unpark = %+v, want devpool-3 held", v)
	}
	if c.SupervisorStarts != startsBefore+1 {
		t.Errorf("supervisors started by the unpark = %d, want one (the stop ended the one the acquire started)", c.SupervisorStarts-startsBefore)
	}
	if text, isErr := call("zerops_dev_server", map[string]any{"action": "status", "hostname": "devpool-3"}); isErr || !strings.Contains(text, `"web"`) {
		t.Fatalf("zerops_dev_server after the unpark = (error %v) %s, want the web server reported", isErr, text)
	}

	// Parked again, a release starts it for its guard, then releases it.
	lease(map[string]any{"action": "park", "lease": "devpool-3"})
	if v := lease(map[string]any{"action": "release", "lease": "devpool-3"}); v.State != "released" || !strings.Contains(v.Note, "started its parked container") {
		t.Fatalf("release = %+v, want released, saying zcp started it to check it", v)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := "stop,start,stop,start,release ACTIVE reset=true"; strings.Join(calls, ",") != want {
		t.Errorf("orchestrator calls = %s, want %s", strings.Join(calls, ","), want)
	}
}
