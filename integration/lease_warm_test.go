// Tests for: integration — zerops_lease keeps each pool's warm container
// through the full MCP server (docs/spec-workflows.md §8 DP-26).

package integration_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zeropsio/zcp/internal/platform"
)

// warmIntBox is one container of warmIntOrchestrator's pool.
type warmIntBox struct {
	id, lease, status, lock string
	number                  int
}

// warmIntOrchestrator is a pool of two containers, both free and each with
// its primary snapshot, at its maximum of two, speaking the live wire shapes:
// a list omits snapshots, a read by id counts them, a delete with the
// holder's lease answers 202 and leaves the container deleting.
type warmIntOrchestrator struct {
	mu                         sync.Mutex
	boxes                      []*warmIntBox
	releases, deletes, creates []string
}

func newWarmIntOrchestrator() *warmIntOrchestrator {
	return &warmIntOrchestrator{boxes: []*warmIntBox{{id: "c-1", number: 1, status: "ACTIVE"}, {id: "c-2", number: 2, status: "ACTIVE"}}}
}

func (o *warmIntOrchestrator) render(b *warmIntBox, byID, withLease bool) string {
	s := fmt.Sprintf(`{"id":%q,"number":%d,"hostname":"node-id-%d.runtime.devpool.zerops","status":%q,"serviceId":"svc-pool","current":true,"lockState":%q,"dirty":false`,
		b.id, b.number, b.number, b.status, b.lock)
	if byID {
		s += `,"snapshots":1`
	}
	if withLease {
		s += fmt.Sprintf(`,"lease":%q`, b.lease)
	}
	return s + "}"
}

func (o *warmIntOrchestrator) find(id string) *warmIntBox {
	for _, b := range o.boxes {
		if b.id == id {
			return b
		}
	}
	return nil
}

func (o *warmIntOrchestrator) refuse(w http.ResponseWriter, reason, msg string) {
	w.WriteHeader(http.StatusBadRequest)
	_, _ = fmt.Fprintf(w, `{"error":{"code":"containerAction","message":%q,"meta":[{"metadata":{"reason":[%q],"retryable":["false"]}}]}}`, msg, reason)
}

func (o *warmIntOrchestrator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+leaseIntToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	lease, path := r.Header.Get("X-Swarm-Lease"), r.URL.Path
	id := strings.TrimSuffix(strings.TrimPrefix(path, "/container/"), "/release")
	switch {
	case r.Method == http.MethodGet && path == "/pool":
		_, _ = w.Write([]byte(`{"minimumContainerCount":0,"maximumContainerCount":2}`))
	case r.Method == http.MethodGet && path == "/container":
		parts := make([]string, 0, len(o.boxes))
		for _, b := range o.boxes {
			parts = append(parts, o.render(b, false, false))
		}
		_, _ = w.Write([]byte("[" + strings.Join(parts, ",") + "]"))
	case r.Method == http.MethodPost && path == "/container":
		o.creates = append(o.creates, path)
		o.refuse(w, "pool-capacity-exceeded", "the requested containers do not fit under the pool maximum")
	case r.Method == http.MethodPost && path == "/container/acquire":
		o.acquire(w)
	case r.Method == http.MethodPost && strings.HasSuffix(path, "/release"):
		b := o.find(id)
		if b == nil || b.lease != lease {
			o.refuse(w, "unknown-lease", "unknown lease")
			return
		}
		o.releases = append(o.releases, b.id+" "+r.URL.RawQuery)
		b.lock, b.lease = "", ""
		_, _ = w.Write([]byte(o.render(b, false, false)))
	case r.Method == http.MethodDelete:
		o.deletes = append(o.deletes, id+" "+r.URL.RawQuery)
		b := o.find(id)
		if b == nil || b.lease != lease {
			o.refuse(w, "held-by-other", "held by another holder")
			return
		}
		b.status, b.lock, b.lease = "DELETING", "deleting", ""
		w.WriteHeader(http.StatusAccepted)
	case r.Method == http.MethodGet && o.find(id) != nil:
		_, _ = w.Write([]byte(o.render(o.find(id), true, false)))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func (o *warmIntOrchestrator) acquire(w http.ResponseWriter) {
	for _, b := range o.boxes {
		if b.lock == "" && b.status == "ACTIVE" {
			b.lock, b.lease = "reserved", fmt.Sprintf("lease-%d-%s", b.number, leaseIntLease)
			_, _ = w.Write([]byte(o.render(b, false, true)))
			return
		}
	}
	o.refuse(w, "all-reserved", "all taken")
}

func (o *warmIntOrchestrator) calls() (releases, deletes, creates []string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.releases...), append([]string(nil), o.deletes...), append([]string(nil), o.creates...)
}

// warmIntResult is what the warm checks read of a zerops_lease result.
type warmIntResult struct {
	Leases []struct {
		Alias string `json:"alias"`
		State string `json:"state"`
		Note  string `json:"note"`
	} `json:"leases"`
	Warm []struct {
		Pool   string `json:"pool"`
		Target int    `json:"target"`
		Ready  *int   `json:"ready"`
	} `json:"warm"`
}

func warmIntCall(t *testing.T, session *mcp.ClientSession, args map[string]any) warmIntResult {
	t.Helper()
	res := callAndGetResult(t, session, "zerops_lease", args)
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("zerops_lease %v failed: %s", args, text)
	}
	if strings.Contains(text, leaseIntLease) || strings.Contains(text, leaseIntToken) {
		t.Fatalf("zerops_lease %v result carries a secret: %s", args, text)
	}
	var out warmIntResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	return out
}

// TestLease_Integration_WarmPool_KeepsOneRemovesTheOther: in a pool of two
// containers, both leased, the first lease released is kept as the pool's
// warm container (a reset release) and the second is removed with its own
// lease (DELETE, the lease only in the header, never force); each result says
// which, nothing is created at the maximum, and status reports the warm
// container.
func TestLease_Integration_WarmPool_KeepsOneRemovesTheOther(t *testing.T) {
	// Non-parallel: t.Chdir (the state dir the server resolves from the cwd)
	// and a swap of http.DefaultTransport (the seam to the orchestrator host).
	t.Chdir(t.TempDir())

	orch := newWarmIntOrchestrator()
	hs := httptest.NewServer(orch)
	defer hs.Close()
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
	session, cleanup := setupTestServer(t, mock, defaultLogFetcher())
	defer cleanup()

	warmIntCall(t, session, map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app", "branch": "feat-a"})
	warmIntCall(t, session, map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app", "branch": "feat-b"})
	first := warmIntCall(t, session, map[string]any{"action": "release", "lease": "devpool-1"})
	second := warmIntCall(t, session, map[string]any{"action": "release", "lease": "devpool-2"})
	if len(first.Leases) != 1 || first.Leases[0].State != "released" || !strings.Contains(first.Leases[0].Note, "kept as the pool's warm container") {
		t.Errorf("first release = %+v, want released and kept as the pool's warm container", first.Leases)
	}
	if len(second.Leases) != 1 || second.Leases[0].State != "released" || !strings.Contains(second.Leases[0].Note, "removed") {
		t.Errorf("second release = %+v, want released, its container removed", second.Leases)
	}
	releases, deletes, creates := orch.calls()
	if len(releases) != 1 || releases[0] != "c-1 reset=true" {
		t.Errorf("releases = %q, want c-1 with reset=true", releases)
	}
	if len(deletes) != 1 || deletes[0] != "c-2 " {
		t.Errorf("deletes = %q, want c-2 alone, its lease in the header, no query", deletes)
	}
	if len(creates) != 0 {
		t.Errorf("creates = %q, want none", creates)
	}

	status := warmIntCall(t, session, map[string]any{"action": "status"})
	if len(status.Warm) != 1 || status.Warm[0].Pool != "devpool" || status.Warm[0].Target != 1 || status.Warm[0].Ready == nil || *status.Warm[0].Ready != 1 {
		t.Errorf("status warm = %+v, want devpool with its one warm container", status.Warm)
	}
}
