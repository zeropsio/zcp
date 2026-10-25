// Tests for: integration — zerops_lease through the full MCP server once the
// container a lease named may no longer be its reservation
// (docs/spec-workflows.md §8 DP-2), against a mock platform and a fake pool
// orchestrator that keeps the live lock facts.

package integration_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

const ownershipIntOtherLease = "another-consumers-lease-0123456789"

// ownershipIntOrch is one pool container (#3) on the live wire (2026-10-24):
// an acquire, and a holder's stop or start, restart lockSince; a renew answers
// the holder's own lease unchanged and refuses any other, and any lease on a
// free container, unknown-lease; a release carrying another lease on a
// reserved container is refused held-by-other.
type ownershipIntOrch struct {
	mu                       sync.Mutex
	lockState, lockSince     string
	holder, status           string
	releases, renews, resets int
}

func (o *ownershipIntOrch) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+leaseIntToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	since := "null"
	if o.lockSince != "" {
		since = `"` + o.lockSince + `"`
	}
	status := o.status
	if status == "" {
		status = "ACTIVE"
	}
	container := `{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.devpool.zerops","status":"` + status + `",` +
		`"serviceId":"svc-pool","stackId":"stk","appVersionId":"av","current":true,"lockState":"` + o.lockState + `","lockSince":` + since + `,"dirty":false`
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/container":
		_, _ = w.Write([]byte(`[` + container + `}]`))
	case r.Method == http.MethodGet && r.URL.Path == "/container/c-abc":
		_, _ = w.Write([]byte(container + `,"snapshots":1}`))
	case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
		o.lockState, o.lockSince, o.holder, o.status = "reserved", "2026-10-24T11:01:00.88865963Z", leaseIntLease, "ACTIVE"
		_, _ = w.Write([]byte(`{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.devpool.zerops","status":"ACTIVE","serviceId":"svc-pool",` +
			`"current":true,"lockState":"reserved","lockSince":"` + o.lockSince + `","dirty":false,"lease":"` + leaseIntLease + `"}`))
	case r.Method == http.MethodPost && r.URL.Path == "/container/c-abc/renew":
		o.renews++
		if o.lockState == "" || r.Header.Get("X-Swarm-Lease") != o.holder {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"no reservation of this container is held by that lease - acquire it again","meta":[{"metadata":{"reason":["unknown-lease"],"retryable":["false"]}}]}}`))
			return
		}
		_, _ = w.Write([]byte(container + `}`))
	case r.Method == http.MethodPost && r.URL.Path == "/container/c-abc/release":
		o.releases++
		switch {
		case o.lockState == "":
			o.resets++
		case r.Header.Get("X-Swarm-Lease") != o.holder:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"container is reserved by another consumer - pass its lease, or ?force=true to take it over","meta":[{"metadata":{"reason":["held-by-other"],"retryable":["true"]}}]}}`))
			return
		}
		o.lockState, o.lockSince, o.holder = "", "", ""
		_, _ = w.Write([]byte(container + `}`))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (o *ownershipIntOrch) set(fn func(o *ownershipIntOrch)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fn(o)
}

type ownershipIntView struct {
	Alias string `json:"alias"`
	State string `json:"state"`
	Note  string `json:"note"`
}

// ownershipIntServer serves zerops_lease against orch for the test (which must
// not run in parallel: it changes the working directory and
// http.DefaultTransport) and returns a call that fails the test on an error
// result or a result carrying a secret.
func ownershipIntServer(t *testing.T, orch *ownershipIntOrch) func(args map[string]any) []ownershipIntView {
	t.Helper()
	t.Chdir(t.TempDir())
	hs := httptest.NewServer(orch)
	t.Cleanup(hs.Close)
	target, err := url.Parse(hs.URL)
	if err != nil {
		t.Fatal(err)
	}
	orig := http.DefaultTransport
	http.DefaultTransport = orchestratorRoute{host: "orch.devpool.zerops", target: target, next: orig}
	t.Cleanup(func() { http.DefaultTransport = orig })

	mock := defaultMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-pool", Name: "devpool", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "swarm@1", ServiceStackTypeCategoryName: "USER"}},
		}).
		WithServiceEnv("svc-pool", []platform.ServiceEnvVar{{ID: "env-t", Key: "API_TOKEN", Content: leaseIntToken, Sensitive: true}})
	session, cleanup := setupLeaseTestServer(t, mock, defaultLogFetcher(), nil)
	t.Cleanup(cleanup)
	return func(args map[string]any) []ownershipIntView {
		t.Helper()
		res := callAndGetResult(t, session, "zerops_lease", args)
		text := getTextContent(t, res)
		if res.IsError {
			t.Fatalf("zerops_lease %v failed: %s", args["action"], text)
		}
		for _, secret := range []string{leaseIntLease, leaseIntToken, ownershipIntOtherLease} {
			if strings.Contains(text, secret) {
				t.Fatalf("zerops_lease %v result carries a secret: %s", args["action"], text)
			}
		}
		var out struct {
			Leases []ownershipIntView `json:"leases"`
		}
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("result is not JSON: %v\n%s", err, text)
		}
		return out.Leases
	}
}

// TestLease_Integration_LeaseReleasedOutsideAndRetaken_DroppedAsLost: the
// live 4g sequence. zcp acquires devpool-3; the lease is released outside zcp
// and another consumer reserves the container. Status says lost, naming
// another holder; a plain release drops the record as lost without a release
// call to the orchestrator; the next status reports the container as reserved
// by a holder zcp has no record of, and nothing ever carries a secret.
func TestLease_Integration_LeaseReleasedOutsideAndRetaken_DroppedAsLost(t *testing.T) {
	// Non-parallel: ownershipIntServer changes the working directory and
	// http.DefaultTransport.
	orch := &ownershipIntOrch{}
	call := ownershipIntServer(t, orch)

	if got := call(map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app", "branch": "lv-bogus"}); len(got) != 1 || got[0].State != "held" {
		t.Fatalf("acquire = %+v, want devpool-3 held", got)
	}
	orch.set(func(o *ownershipIntOrch) {
		o.lockState, o.lockSince, o.holder = "reserved", "2026-10-24T11:02:04.783475573Z", ownershipIntOtherLease
	})

	got := call(map[string]any{"action": "status", "pool": "devpool"})
	if len(got) != 1 || got[0].Alias != "devpool-3" || got[0].State != "lost" || !strings.Contains(got[0].Note, "another holder") {
		t.Fatalf("status = %+v, want devpool-3 lost, naming another holder", got)
	}
	got = call(map[string]any{"action": "release", "lease": "devpool-3"})
	if len(got) != 1 || got[0].State != "lost" || !strings.Contains(got[0].Note, "record is dropped") {
		t.Fatalf("release = %+v, want devpool-3 lost, its record dropped", got)
	}
	orch.set(func(o *ownershipIntOrch) {
		if o.releases != 0 {
			t.Errorf("orchestrator releases = %d, want none: the container is another holder's", o.releases)
		}
	})
	if _, err := os.Stat(filepath.Join(".zcp", "state", "leases", "devpool-3.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("record survives the release: %v", err)
	}
	if got := call(map[string]any{"action": "status", "pool": "devpool"}); len(got) != 1 || got[0].State != "unknown-holder" {
		t.Errorf("status after = %+v, want devpool-3 an unknown holder's", got)
	}
}

// TestLease_Integration_LeaseStoppedOutsideZcp_StaysOurs: a stop of zcp's
// lease made outside zcp — an older zcp sharing the state directory, anyone
// holding the lease — restarts the reservation's lockSince, yet the
// orchestrator still renews the lease: status says parked, and a release goes
// to the orchestrator with the lease and resets the container — the record is
// never dropped while zcp's lease still reserves it.
func TestLease_Integration_LeaseStoppedOutsideZcp_StaysOurs(t *testing.T) {
	// Non-parallel: ownershipIntServer changes the working directory and
	// http.DefaultTransport.
	orch := &ownershipIntOrch{}
	call := ownershipIntServer(t, orch)

	if got := call(map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app", "branch": "feat-x"}); len(got) != 1 || got[0].State != "held" {
		t.Fatalf("acquire = %+v, want devpool-3 held", got)
	}
	orch.set(func(o *ownershipIntOrch) { o.status, o.lockSince = "STOPPED", "2026-10-24T11:04:00.5Z" })

	if got := call(map[string]any{"action": "status", "pool": "devpool"}); len(got) != 1 || got[0].State != "parked" {
		t.Fatalf("status = %+v, want devpool-3 parked: the orchestrator still gives it the lease", got)
	}
	got := call(map[string]any{"action": "release", "lease": "devpool-3", "force": true})
	if len(got) != 1 || got[0].State != "released" {
		t.Fatalf("release = %+v, want devpool-3 released", got)
	}
	orch.set(func(o *ownershipIntOrch) {
		if o.releases != 1 || o.lockState != "" {
			t.Errorf("orchestrator releases = %d, lockState %q: want the one release, the container free", o.releases, o.lockState)
		}
	})
}
