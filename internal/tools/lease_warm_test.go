// Tests for: tools/lease.go — zerops_lease keeps each pool's warm container
// (docs/spec-workflows.md §8 DP-26): a release keeps its container as the warm
// one or removes it with its own lease, an acquire that takes the warm one
// has another prepared in the background, and status reports each pool's
// warm containers.
package tools

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
)

// warmPoolOrchestrator is a pool orchestrator speaking the live wire shapes
// (I-P1): a list omits snapshots and only a read by id counts them; an
// acquire takes a free container first; a reset release frees it clean; a
// delete with the holder's lease answers 202 and leaves it deleting; a create
// adds free current containers, answered with snapshots null.
type warmPoolOrchestrator struct {
	mu         sync.Mutex
	max        int
	containers []*warmPoolContainer
	next       int
	// log
	releases, deletes []string // "<id> <lease> <query>"
	creates           []string // request bodies
	createAuth        []string // "<authorization>|<lease header>"
}

type warmPoolContainer struct {
	id, lease, status, lock string
	number, snapshots       int
	current                 bool
}

// add puts a free container with its primary snapshot in the pool.
func (o *warmPoolOrchestrator) add(number int) {
	c := &warmPoolContainer{id: fmt.Sprintf("c-%d", number), number: number, status: "ACTIVE", snapshots: 1, current: true}
	o.containers = append(o.containers, c)
	if number >= o.next {
		o.next = number + 1
	}
}

func (c *warmPoolContainer) json(withSnapshots, withLease bool) string {
	s := fmt.Sprintf(`{"id":%q,"number":%d,"hostname":"node-id-%d.runtime.devpool.zerops","status":%q,"serviceId":"svc-pool","current":%v,"lockState":%q,"dirty":false`,
		c.id, c.number, c.number, c.status, c.current, c.lock)
	if withSnapshots {
		s += fmt.Sprintf(`,"snapshots":%d`, c.snapshots)
	}
	if withLease {
		s += fmt.Sprintf(`,"lease":%q`, c.lease)
	}
	return s + "}"
}

func (o *warmPoolOrchestrator) find(id string) *warmPoolContainer {
	for _, c := range o.containers {
		if c.id == id {
			return c
		}
	}
	return nil
}

func (o *warmPoolOrchestrator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+leaseTestToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	refuse := func(reason, msg string) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprintf(w, `{"error":{"code":"containerAction","message":%q,"meta":[{"metadata":{"reason":[%q],"retryable":["false"]}}]}}`, msg, reason)
	}
	lease := r.Header.Get("X-Swarm-Lease")
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/pool":
		_, _ = fmt.Fprintf(w, `{"minimumContainerCount":0,"maximumContainerCount":%d}`, o.max)
	case r.Method == http.MethodGet && path == "/container":
		parts := make([]string, 0, len(o.containers))
		for _, c := range o.containers {
			parts = append(parts, c.json(false, false))
		}
		_, _ = w.Write([]byte("[" + strings.Join(parts, ",") + "]"))
	case r.Method == http.MethodPost && path == "/container":
		body, _ := io.ReadAll(r.Body)
		o.creates = append(o.creates, string(body))
		o.createAuth = append(o.createAuth, r.Header.Get("Authorization")+"|"+lease)
		if len(o.containers) >= o.max {
			refuse("pool-capacity-exceeded", "the requested containers do not fit under the pool maximum")
			return
		}
		o.add(o.next)
		_, _ = w.Write([]byte("[" + strings.Replace(o.containers[len(o.containers)-1].json(false, false), `"dirty":false`, `"dirty":false,"snapshots":null`, 1) + "]"))
	case r.Method == http.MethodPost && path == "/container/acquire":
		for _, c := range o.containers {
			if c.lock == "" && c.status == "ACTIVE" {
				c.lock, c.lease = "reserved", fmt.Sprintf("lease-%d-0123456789abcdef0123456789", c.number)
				_, _ = w.Write([]byte(c.json(false, true)))
				return
			}
		}
		refuse("all-reserved", "all taken")
	case strings.HasPrefix(path, "/container/") && strings.HasSuffix(path, "/release") && r.Method == http.MethodPost:
		c := o.find(strings.TrimSuffix(strings.TrimPrefix(path, "/container/"), "/release"))
		if c == nil || c.lease != lease {
			refuse("unknown-lease", "unknown lease")
			return
		}
		o.releases = append(o.releases, c.id+" "+lease+" "+r.URL.RawQuery)
		c.lock, c.lease = "", ""
		_, _ = w.Write([]byte(c.json(false, false)))
	case strings.HasPrefix(path, "/container/") && r.Method == http.MethodDelete:
		id := strings.TrimPrefix(path, "/container/")
		o.deletes = append(o.deletes, id+" "+lease+" "+r.URL.RawQuery)
		c := o.find(id)
		if c == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if c.lease != lease {
			refuse("held-by-other", "held by another holder")
			return
		}
		c.status, c.lock, c.lease = "DELETING", "deleting", ""
		w.WriteHeader(http.StatusAccepted)
	case strings.HasPrefix(path, "/container/") && r.Method == http.MethodGet:
		c := o.find(strings.TrimPrefix(path, "/container/"))
		if c == nil {
			refuse("container-not-found", "no such container")
			return
		}
		_, _ = w.Write([]byte(c.json(true, false)))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

type warmLeaseResult struct {
	Leases []ops.LeaseView     `json:"leases"`
	Warm   []ops.LeasePoolWarm `json:"warm"`
}

func warmLeaseResultOf(t *testing.T, text string) warmLeaseResult {
	t.Helper()
	var out warmLeaseResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	return out
}

// TestLeaseTool_Release_WarmPool_KeepsThenRemoves: of two leases in a pool
// whose every container is held, the first released is kept — a reset
// release — as the pool's warm container; the second is removed with its own
// lease (DELETE, the lease only in the header, no force); each result says
// which, and status reports the warm container.
func TestLeaseTool_Release_WarmPool_KeepsThenRemoves(t *testing.T) {
	t.Parallel()
	orch := &warmPoolOrchestrator{max: 2}
	orch.add(1)
	orch.add(2)
	srv, _ := leaseToolServer(t, orch)

	for _, branch := range []string{"feat-a", "feat-b"} {
		res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": branch})
		if text := getTextContent(t, res); res.IsError {
			t.Fatalf("acquire %s: %s", branch, text)
		}
	}
	first := callTool(t, srv, "zerops_lease", map[string]any{"action": "release", "lease": "devpool-1"})
	second := callTool(t, srv, "zerops_lease", map[string]any{"action": "release", "lease": "devpool-2"})
	firstText, secondText := getTextContent(t, first), getTextContent(t, second)
	if first.IsError || second.IsError {
		t.Fatalf("release failed:\n%s\n%s", firstText, secondText)
	}
	if got := warmLeaseResultOf(t, firstText); len(got.Leases) != 1 || !strings.Contains(got.Leases[0].Note, "kept as the pool's warm container") {
		t.Errorf("first release = %+v, want kept as the pool's warm container", got.Leases)
	}
	if got := warmLeaseResultOf(t, secondText); len(got.Leases) != 1 || got.Leases[0].State != ops.LeaseStateReleased || !strings.Contains(got.Leases[0].Note, "removed") {
		t.Errorf("second release = %+v, want released, its container removed", got.Leases)
	}

	orch.mu.Lock()
	releases, deletes, creates := orch.releases, orch.deletes, orch.creates
	orch.mu.Unlock()
	if len(releases) != 1 || releases[0] != "c-1 lease-1-0123456789abcdef0123456789 reset=true" {
		t.Errorf("releases = %q, want c-1 released with its lease and reset=true", releases)
	}
	if len(deletes) != 1 || deletes[0] != "c-2 lease-2-0123456789abcdef0123456789 " {
		t.Errorf("deletes = %q, want c-2 deleted with its own lease in the header and no query (never force)", deletes)
	}
	if len(creates) != 0 {
		t.Errorf("creates = %q, want none (the pool was at its maximum, then had its warm container)", creates)
	}
	for _, text := range []string{firstText, secondText} {
		if strings.Contains(text, "lease-1-0123") || strings.Contains(text, "lease-2-0123") || strings.Contains(text, leaseTestToken) {
			t.Errorf("result carries a secret:\n%s", text)
		}
	}

	status := callTool(t, srv, "zerops_lease", map[string]any{"action": "status"})
	got := warmLeaseResultOf(t, getTextContent(t, status))
	if len(got.Warm) != 1 || got.Warm[0].Pool != "devpool" || got.Warm[0].Target != 1 || got.Warm[0].Ready == nil || *got.Warm[0].Ready != 1 {
		t.Errorf("status warm = %+v, want devpool with its one warm container", got.Warm)
	}
}

// TestLeaseTool_Acquire_TakesTheWarmOne_PreparesAnother: an acquire that takes
// the pool's warm container says one is being prepared; the create runs in
// the background through POST /container {"count":1} with the bearer only,
// and status reports its outcome.
func TestLeaseTool_Acquire_TakesTheWarmOne_PreparesAnother(t *testing.T) {
	t.Parallel()
	orch := &warmPoolOrchestrator{max: 3}
	orch.add(1)
	srv, _ := leaseToolServer(t, orch)

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feat-a"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("acquire: %s", text)
	}
	if got := warmLeaseResultOf(t, text); len(got.Leases) != 1 || got.Leases[0].Alias != "devpool-1" || !strings.Contains(got.Leases[0].Note, "being prepared") {
		t.Errorf("acquire = %+v, want devpool-1 with a warm container being prepared", got.Leases)
	}

	var warm ops.LeasePoolWarm
	deadline := time.Now().Add(10 * time.Second)
	for {
		status := callTool(t, srv, "zerops_lease", map[string]any{"action": "status"})
		got := warmLeaseResultOf(t, getTextContent(t, status))
		if len(got.Warm) == 1 && got.Warm[0].Last != nil && got.Warm[0].Last.Outcome == ops.LeaseWarmCreated {
			warm = got.Warm[0]
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the background create never reported: warm = %+v", got.Warm)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if warm.Ready == nil || *warm.Ready != 1 || warm.Last.Count != 1 {
		t.Errorf("warm = %+v (last %+v), want 1 ready, last created 1", warm, warm.Last)
	}
	orch.mu.Lock()
	creates, auth := orch.creates, orch.createAuth
	orch.mu.Unlock()
	if len(creates) != 1 || creates[0] != `{"count":1}` {
		t.Errorf("creates = %q, want one POST /container {\"count\":1}", creates)
	}
	if len(auth) != 1 || auth[0] != "Bearer "+leaseTestToken+"|" {
		t.Errorf("create headers = %q, want the bearer and no lease", auth)
	}
}
