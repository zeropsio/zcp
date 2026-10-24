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

	"github.com/zeropsio/zcp/internal/platform"
)

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

	type box struct {
		id, lease, status, lock string
		number                  int
	}
	var mu sync.Mutex
	boxes := []*box{{id: "c-1", number: 1, status: "ACTIVE"}, {id: "c-2", number: 2, status: "ACTIVE"}}
	var releases, deletes, creates []string
	render := func(b *box, byID, withLease bool) string {
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
	find := func(id string) *box {
		for _, b := range boxes {
			if b.id == id {
				return b
			}
		}
		return nil
	}
	orch := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer "+leaseIntToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		lease, path := r.Header.Get("X-Swarm-Lease"), r.URL.Path
		switch {
		case r.Method == http.MethodGet && path == "/pool":
			_, _ = w.Write([]byte(`{"minimumContainerCount":0,"maximumContainerCount":2}`))
		case r.Method == http.MethodGet && path == "/container":
			parts := make([]string, 0, len(boxes))
			for _, b := range boxes {
				parts = append(parts, render(b, false, false))
			}
			_, _ = w.Write([]byte("[" + strings.Join(parts, ",") + "]"))
		case r.Method == http.MethodPost && path == "/container":
			creates = append(creates, path)
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"the requested containers do not fit under the pool maximum","meta":[{"metadata":{"reason":["pool-capacity-exceeded"],"retryable":["false"]}}]}}`))
		case r.Method == http.MethodPost && path == "/container/acquire":
			for _, b := range boxes {
				if b.lock == "" && b.status == "ACTIVE" {
					b.lock, b.lease = "reserved", fmt.Sprintf("lease-%d-%s", b.number, leaseIntLease)
					_, _ = w.Write([]byte(render(b, false, true)))
					return
				}
			}
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"all taken","meta":[{"metadata":{"reason":["all-reserved"],"retryable":["true"]}}]}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/release"):
			b := find(strings.TrimSuffix(strings.TrimPrefix(path, "/container/"), "/release"))
			if b == nil || b.lease != lease {
				w.WriteHeader(http.StatusConflict)
				return
			}
			releases = append(releases, b.id+" "+r.URL.RawQuery)
			b.lock, b.lease = "", ""
			_, _ = w.Write([]byte(render(b, false, false)))
		case r.Method == http.MethodDelete:
			b := find(strings.TrimPrefix(path, "/container/"))
			deletes = append(deletes, strings.TrimPrefix(path, "/container/")+" "+r.URL.RawQuery)
			if b == nil || b.lease != lease {
				w.WriteHeader(http.StatusConflict)
				return
			}
			b.status, b.lock, b.lease = "DELETING", "deleting", ""
			w.WriteHeader(http.StatusAccepted)
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/container/"):
			b := find(strings.TrimPrefix(path, "/container/"))
			if b == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_, _ = w.Write([]byte(render(b, true, false)))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer orch.Close()
	target, err := url.Parse(orch.URL)
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

	type result struct {
		Leases []struct {
			Alias string `json:"alias"`
			State string `json:"state"`
			Note  string `json:"note"`
		} `json:"leases"`
		Warm []struct {
			Pool   string `json:"pool"`
			Target int    `json:"target"`
			Ready  *int   `json:"ready"`
			Last   *struct {
				Outcome string `json:"outcome"`
			} `json:"last"`
		} `json:"warm"`
	}
	call := func(args map[string]any) result {
		t.Helper()
		res := callAndGetResult(t, session, "zerops_lease", args)
		text := getTextContent(t, res)
		if res.IsError {
			t.Fatalf("zerops_lease %v failed: %s", args, text)
		}
		if strings.Contains(text, leaseIntLease) || strings.Contains(text, leaseIntToken) {
			t.Fatalf("zerops_lease %v result carries a secret: %s", args, text)
		}
		var out result
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("result is not JSON: %v\n%s", err, text)
		}
		return out
	}

	call(map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app", "branch": "feat-a"})
	call(map[string]any{"action": "acquire", "pool": "devpool", "repo": "https://github.com/acme/app", "branch": "feat-b"})
	first := call(map[string]any{"action": "release", "lease": "devpool-1"})
	second := call(map[string]any{"action": "release", "lease": "devpool-2"})
	if len(first.Leases) != 1 || first.Leases[0].State != "released" || !strings.Contains(first.Leases[0].Note, "kept as the pool's warm container") {
		t.Errorf("first release = %+v, want released and kept as the pool's warm container", first.Leases)
	}
	if len(second.Leases) != 1 || second.Leases[0].State != "released" || !strings.Contains(second.Leases[0].Note, "removed") {
		t.Errorf("second release = %+v, want released, its container removed", second.Leases)
	}
	mu.Lock()
	gotReleases, gotDeletes, gotCreates := releases, deletes, creates
	mu.Unlock()
	if len(gotReleases) != 1 || gotReleases[0] != "c-1 reset=true" {
		t.Errorf("releases = %q, want c-1 with reset=true", gotReleases)
	}
	if len(gotDeletes) != 1 || gotDeletes[0] != "c-2 " {
		t.Errorf("deletes = %q, want c-2 alone, its lease in the header, no query", gotDeletes)
	}
	if len(gotCreates) != 0 {
		t.Errorf("creates = %q, want none", gotCreates)
	}

	status := call(map[string]any{"action": "status"})
	if len(status.Warm) != 1 || status.Warm[0].Pool != "devpool" || status.Warm[0].Target != 1 || status.Warm[0].Ready == nil || *status.Warm[0].Ready != 1 {
		t.Errorf("status warm = %+v, want devpool with its one warm container", status.Warm)
	}
}
