// Tests for: integration — zerops_lease and zerops_scale at a pool's limits
// through the full MCP server: a full pool, a stopped pool, a lowering of a
// pool's maximum over a lease.

package integration_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// poolRoutes sends each orchestrator host in up to its fake, fails every host
// in down the way a stopped pool's orchestrator does (the address resolves,
// nothing answers), and leaves other traffic on the original transport.
type poolRoutes struct {
	up   map[string]*url.URL
	down map[string]bool
	next http.RoundTripper
}

func (p poolRoutes) RoundTrip(r *http.Request) (*http.Response, error) {
	if p.down[r.URL.Host] {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connect: no route to host")}
	}
	target, ok := p.up[r.URL.Host]
	if !ok {
		return p.next.RoundTrip(r)
	}
	c := r.Clone(r.Context())
	c.URL.Scheme, c.URL.Host = target.Scheme, target.Host
	return p.next.RoundTrip(c)
}

// routePools swaps http.DefaultTransport for poolRoutes for the test's
// length: the server builds its orchestrator clients over the default
// transport, so that is the seam the real wiring offers.
func routePools(t *testing.T, up map[string]http.Handler, down ...string) {
	t.Helper()
	routes := poolRoutes{up: map[string]*url.URL{}, down: map[string]bool{}, next: http.DefaultTransport}
	for host, h := range up {
		srv := httptest.NewServer(h)
		t.Cleanup(srv.Close)
		u, err := url.Parse(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		routes.up[host] = u
	}
	for _, host := range down {
		routes.down[host] = true
	}
	orig := http.DefaultTransport
	http.DefaultTransport = routes
	t.Cleanup(func() { http.DefaultTransport = orig })
}

// poolOrchestrator lists containers c-1..c-<count>, those in reserved as
// leased, answers GET /pool with maxContainers, and refuses every acquire as a full
// pool does (live body).
func poolOrchestrator(count, maxContainers int, reserved map[int]bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+leaseIntToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		const full = "all 10 container(s) of the pool that can serve the request are taken and another could not be created - retry once one is released"
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/pool":
			_, _ = fmt.Fprintf(w, `{"minimumContainerCount":0,"maximumContainerCount":%d}`, maxContainers)
		case r.Method == http.MethodGet && r.URL.Path == "/container":
			parts := make([]string, 0, count)
			for n := 1; n <= count; n++ {
				lock := ""
				if reserved[n] {
					lock = "reserved"
				}
				parts = append(parts, fmt.Sprintf(`{"id":"c-%d","number":%d,"hostname":"node-id-%d.runtime.devpool.zerops","status":"ACTIVE","lockState":%q,"dirty":false,"current":true}`, n, n, n, lock))
			}
			_, _ = w.Write([]byte("[" + strings.Join(parts, ",") + "]"))
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/container/c-") && strings.HasSuffix(r.URL.Path, "/renew"):
			// A reserved container the test's record names holds its lease.
			n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/container/c-"), "/renew"))
			if err != nil || !reserved[n] || r.Header.Get("X-Swarm-Lease") != leaseIntLease {
				writeIntUnknownLease(w)
				return
			}
			_, _ = fmt.Fprintf(w, `{"id":"c-%d","number":%d,"hostname":"node-id-%d.runtime.devpool.zerops","status":"ACTIVE","lockState":"reserved","dirty":false,"current":true}`, n, n, n)
		case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"` + full + `","meta":[{"error":"` + full + `","code":"containerAction",` +
				`"metadata":{"message":["[containerAction] Service is already at maximum number of containers"],"reason":["all-reserved"],"retryable":["true"]}}]}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

// recordIntLease writes zcp's record of a lease under the cwd's state dir.
func recordIntLease(t *testing.T, pool, poolServiceID string, number int, branch string) {
	t.Helper()
	dir := filepath.Join(".zcp", "state", "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"poolHostname": pool, "poolServiceId": poolServiceID, "containerId": fmt.Sprintf("c-%d", number),
		"containerNumber": number, "containerHostname": fmt.Sprintf("node-id-%d.runtime.%s.zerops", number, pool),
		"lease": leaseIntLease, "branch": branch, "acquiredAt": "2026-10-23T18:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%s-%d.json", pool, number)), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

func intPool(id, name, status string, maxContainers int32) platform.ServiceStack {
	svc := platform.ServiceStack{ID: id, Name: name, Status: status,
		ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}}
	if maxContainers > 0 {
		svc.CustomAutoscaling = &platform.CustomAutoscaling{HorizontalMinCount: 0, HorizontalMaxCount: maxContainers}
	}
	return svc
}

// TestLease_Integration_FullAndStoppedPools: an acquire on a full pool says
// how full it is and that nothing is queued; a stopped pool says to start
// it; a status of every pool still lists the running pool's lease and marks
// only the stopped pool's unverified.
func TestLease_Integration_FullAndStoppedPools(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir the server resolves from the cwd)
	// and a swap of http.DefaultTransport (the seam to the orchestrator hosts).
	t.Chdir(t.TempDir())
	routePools(t, map[string]http.Handler{"orch.devpool.zerops": poolOrchestrator(10, 10, map[int]bool{1: true, 2: true, 3: true, 4: true, 5: true, 6: true, 7: true, 8: true, 9: true, 10: true})},
		"orch.stoppool.zerops")
	recordIntLease(t, "devpool", "svc-pool", 3, "feat-a")
	recordIntLease(t, "stoppool", "svc-stop", 2, "feat-b")
	token := []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}}
	mock := defaultMock().
		WithServices([]platform.ServiceStack{intPool("svc-pool", "devpool", "ACTIVE", 10), intPool("svc-stop", "stoppool", "STOPPED", 4)}).
		WithServiceEnv("svc-pool", token).WithServiceEnv("svc-stop", token)
	session, cleanup := setupLeaseTestServer(t, mock, defaultLogFetcher(), nil)
	defer cleanup()

	res := callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app", "branch": "feat-c"})
	text := getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("an acquire on a full pool succeeded: %s", text)
	}
	for _, want := range []string{"is full", "10 of at most 10", "yours: devpool-3", "does not queue", "Raising maxContainers will not help"} {
		if !strings.Contains(text, want) {
			t.Errorf("full-pool refusal lacks %q:\n%s", want, text)
		}
	}

	res = callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "acquire", "pool": "stoppool", "repo": "https://github.com/acme/app"})
	text = getTextContent(t, res)
	if !res.IsError || !strings.Contains(text, "the pool is stopped") || !strings.Contains(text, "zerops_manage action=start") {
		t.Errorf("an acquire on a stopped pool should say to start it:\n%s", text)
	}

	res = callAndGetResult(t, session, "zerops_lease", map[string]any{"action": "status"})
	text = getTextContent(t, res)
	if res.IsError {
		t.Fatalf("status failed over one stopped pool: %s", text)
	}
	var got struct {
		Leases []struct {
			Alias      string `json:"alias"`
			State      string `json:"state"`
			Unverified bool   `json:"unverified"`
		} `json:"leases"`
		UnreadPools []struct {
			Pool string `json:"pool"`
		} `json:"unreadPools"`
	}
	if err := json.Unmarshal([]byte(text), &got); err != nil {
		t.Fatalf("status is not JSON: %v\n%s", err, text)
	}
	states := map[string]string{}
	for _, l := range got.Leases {
		states[l.Alias] = l.State
		if l.Unverified {
			states[l.Alias] += " (unverified)"
		}
	}
	// devpool's other nine reserved containers are other holders'.
	if len(states) != 11 || states["devpool-3"] != "held" || states["stoppool-2"] != "held (unverified)" || states["devpool-1"] != "unknown-holder" {
		t.Errorf("leases = %v, want devpool-3 held, stoppool-2 held (unverified), devpool's others unknown-holder", states)
	}
	if len(got.UnreadPools) != 1 || got.UnreadPools[0].Pool != "stoppool" {
		t.Errorf("unreadPools = %+v, want stoppool", got.UnreadPools)
	}
	for _, secret := range []string{leaseIntLease, leaseIntToken} {
		if strings.Contains(text, secret) {
			t.Fatal("status carries a secret")
		}
	}
}

// TestScale_Integration_PoolLoweringOverALease_AsksFirst: through the full
// server, zerops_scale reads the pool's orchestrator before it lowers the
// pool's maxContainers; a lowering that would delete a lease is refused with
// what would go until the call acknowledges it, and the acknowledged call
// names what it deletes.
func TestScale_Integration_PoolLoweringOverALease_AsksFirst(t *testing.T) {
	// Non-parallel: t.Chdir and a swap of http.DefaultTransport, as above.
	t.Chdir(t.TempDir())
	routePools(t, map[string]http.Handler{"orch.devpool.zerops": poolOrchestrator(5, 5, map[int]bool{5: true})})
	recordIntLease(t, "devpool", "svc-pool", 5, "feat-x")
	mock := defaultMock().
		WithServices([]platform.ServiceStack{intPool("svc-pool", "devpool", "ACTIVE", 5)}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	session, cleanup := setupLeaseTestServer(t, mock, defaultLogFetcher(), nil)
	defer cleanup()

	res := callAndGetResult(t, session, "zerops_scale", map[string]any{"serviceHostname": "devpool", "maxContainers": 4})
	text := getTextContent(t, res)
	if !res.IsError {
		t.Fatalf("a lowering over a lease went through unasked: %s", text)
	}
	var refusal struct {
		Code         string `json:"code"`
		WouldDestroy struct {
			Operation string   `json:"operation"`
			Targets   []string `json:"targets"`
		} `json:"wouldDestroy"`
	}
	if err := json.Unmarshal([]byte(text), &refusal); err != nil {
		t.Fatalf("refusal is not JSON: %v\n%s", err, text)
	}
	if refusal.Code != platform.ErrDiagnosisRequired || refusal.WouldDestroy.Operation != "pool-shrink" || strings.Join(refusal.WouldDestroy.Targets, ",") != "devpool-5" {
		t.Errorf("refusal = %+v, want %s pool-shrink naming devpool-5", refusal, platform.ErrDiagnosisRequired)
	}

	res = callAndGetResult(t, session, "zerops_scale", map[string]any{"serviceHostname": "devpool", "maxContainers": 4,
		"confirmDestructive": map[string]any{"operation": "pool-shrink", "acknowledgedTargets": []string{"devpool-5"}}})
	text = getTextContent(t, res)
	if res.IsError {
		t.Fatalf("the acknowledged lowering was refused: %s", text)
	}
	if !strings.Contains(text, "deletes devpool-5 (your lease, branch feat-x)") {
		t.Errorf("result does not name what the lowering deletes:\n%s", text)
	}
	if strings.Contains(text, leaseIntToken) || strings.Contains(text, leaseIntLease) {
		t.Fatal("scale result carries a secret")
	}
}
