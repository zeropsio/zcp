// Tests for: tools/lease.go — zerops_lease on a lease whose container is no
// longer its reservation (docs/spec-workflows.md §8 DP-2): status and the
// envelope say lost and why, and a release drops the record without reading
// the container over ssh or calling the orchestrator's release.
package tools

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
)

// ownershipOrchestrator is one pool container (#3) on the live wire, keeping
// the live lock facts (2026-10-24): each reservation change — an acquire, a
// holder's stop or start — restarts lockSince; a release, stop or start
// carrying another lease on a reserved container is refused held-by-other; a
// release of a free container resets it whatever lease it carries (a stop or
// start of one, never live-verified, goes through as well and is counted); a
// renew answers the holder's own lease unchanged and refuses any other, and
// any lease on a free container, unknown-lease.
type ownershipOrchestrator struct {
	mu         sync.Mutex
	lockState  string
	lockSince  string
	lease      string
	status     string // ACTIVE unless stopped
	releases   int    // release calls that reached it
	freeResets int    // free containers a release reset
	freePowers int    // stops and starts of a free container
	renews     int
	// renewFails answers every renew 500; takeOverOnStop lets another
	// consumer take the container over right before a stop reaches it.
	renewFails     bool
	takeOverOnStop bool
}

const (
	ownershipSince      = "2026-10-24T11:01:00.88865963Z"
	ownershipOtherSince = "2026-10-24T11:02:04.783475573Z"
	ownershipOtherLease = "another-consumers-lease-0123456789"
)

func (o *ownershipOrchestrator) container() string {
	since := "null"
	if o.lockSince != "" {
		since = `"` + o.lockSince + `"`
	}
	status := o.status
	if status == "" {
		status = "ACTIVE"
	}
	return `{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.devpool.zerops","status":"` + status + `",` +
		`"serviceId":"svc-pool","stackId":"stk","appVersionId":"av","current":true,"lockState":"` + o.lockState + `","lockSince":` + since + `,"dirty":false,"snapshots":1}`
}

func (o *ownershipOrchestrator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+leaseTestToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	heldByOther := func() {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"container is reserved by another consumer - pass its lease, or ?force=true to take it over","meta":[{"metadata":{"reason":["held-by-other"],"retryable":["true"]}}]}}`))
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/container":
		_, _ = w.Write([]byte(`[` + o.container() + `]`))
	case r.Method == http.MethodGet && r.URL.Path == "/container/c-abc":
		_, _ = w.Write([]byte(o.container()))
	case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
		o.lockState, o.lockSince, o.lease = "reserved", ownershipSince, leaseTestLease
		c := o.container()
		_, _ = w.Write([]byte(c[:len(c)-1] + `,"lease":"` + leaseTestLease + `"}`))
	case r.Method == http.MethodPost && r.URL.Path == "/container/c-abc/renew":
		o.renews++
		switch {
		case o.renewFails:
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"code":"internalServerError","message":"the orchestrator could not read its reservations"}}`))
		case o.lockState == "" || r.Header.Get("X-Swarm-Lease") != o.lease:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"no reservation of this container is held by that lease - acquire it again","meta":[{"metadata":{"reason":["unknown-lease"],"retryable":["false"]}}]}}`))
		default:
			_, _ = w.Write([]byte(o.container()))
		}
	case r.Method == http.MethodPost && (r.URL.Path == "/container/c-abc/stop" || r.URL.Path == "/container/c-abc/start"):
		if o.takeOverOnStop && strings.HasSuffix(r.URL.Path, "/stop") {
			o.lockState, o.lockSince, o.lease = "reserved", ownershipOtherSince, ownershipOtherLease
		}
		if o.lockState == "reserved" && r.Header.Get("X-Swarm-Lease") != o.lease {
			heldByOther()
			return
		}
		if o.lockState == "" {
			o.freePowers++
		}
		o.status = "ACTIVE"
		if strings.HasSuffix(r.URL.Path, "/stop") {
			o.status = "STOPPED"
		}
		if o.lockState == "reserved" {
			o.lockSince = "2026-10-24T11:05:00.123456789Z"
		}
		_, _ = w.Write([]byte(o.container()))
	case r.Method == http.MethodPost && r.URL.Path == "/container/c-abc/release":
		o.releases++
		switch {
		case o.lockState == "":
			o.freeResets++
		case r.Header.Get("X-Swarm-Lease") != o.lease:
			heldByOther()
			return
		default:
			o.lockState, o.lockSince, o.lease = "", "", ""
		}
		_, _ = w.Write([]byte(o.container()))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

// takeOver frees the container outside zcp, and another consumer reserves it.
func (o *ownershipOrchestrator) takeOver() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.lockState, o.lockSince, o.lease = "reserved", ownershipOtherSince, ownershipOtherLease
}

// stopOutside stops the container with the holder's own lease outside zcp:
// still the holder's, its lockSince restarted.
func (o *ownershipOrchestrator) stopOutside() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.status, o.lockSince = "STOPPED", "2026-10-24T11:04:00.5Z"
}

// set runs fn on the orchestrator's state under its lock.
func (o *ownershipOrchestrator) set(fn func(o *ownershipOrchestrator)) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fn(o)
}

// releaseOutside frees the container outside zcp.
func (o *ownershipOrchestrator) releaseOutside() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.lockState, o.lockSince, o.lease = "", "", ""
}

func (o *ownershipOrchestrator) counts() (releases, freeResets int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.releases, o.freeResets
}

// ownershipResult is the part of a zerops_lease result these tests read.
type ownershipResult struct {
	Leases   []ops.LeaseView `json:"leases"`
	Envelope *struct {
		Leases []struct {
			Alias string `json:"alias"`
			State string `json:"state"`
		} `json:"leases"`
	} `json:"envelope"`
}

func decodeOwnership(t *testing.T, text string) ownershipResult {
	t.Helper()
	var out ownershipResult
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	return out
}

// TestLeaseTool_Ownership_LeaseAnotherHolderTook_LostThenDropped: once
// another consumer reserves the container a lease named (live 4g), status and
// the envelope's leases[] say lost and why — read from no container — and a
// plain release drops the record as lost without the release guard's ssh
// reads or an orchestrator release.
func TestLeaseTool_Ownership_LeaseAnotherHolderTook_LostThenDropped(t *testing.T) {
	t.Parallel()
	orch := &ownershipOrchestrator{}
	ssh := &leaseStubSSH{}
	srv, stateDir := leaseToolServerSSH(t, orch, ssh)
	if res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feat-x"}); res.IsError {
		t.Fatalf("acquire failed: %s", getTextContent(t, res))
	}
	orch.takeOver()
	ssh.mu.Lock()
	ssh.calls = nil
	ssh.mu.Unlock()

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "status", "pool": "devpool"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("status failed: %s", text)
	}
	got := decodeOwnership(t, text)
	if len(got.Leases) != 1 || got.Leases[0].State != ops.LeaseStateLost || !strings.Contains(got.Leases[0].Note, "another holder") {
		t.Errorf("status leases = %+v, want devpool-3 lost, naming another holder", got.Leases)
	}
	if got.Envelope == nil || len(got.Envelope.Leases) == 0 || got.Envelope.Leases[0].State != ops.LeaseStateLost {
		t.Errorf("envelope leases = %+v, want devpool-3 lost", got.Envelope)
	}
	if len(ssh.calls) != 0 {
		t.Errorf("status reached the other holder's container over ssh: %v", ssh.calls)
	}

	res = callTool(t, srv, "zerops_lease", map[string]any{"action": "release", "lease": "devpool-3"})
	text = getTextContent(t, res)
	if res.IsError {
		t.Fatalf("release of a lost lease refused: %s", text)
	}
	got = decodeOwnership(t, text)
	if len(got.Leases) != 1 || got.Leases[0].State != ops.LeaseStateLost || !strings.Contains(got.Leases[0].Note, "record is dropped") {
		t.Errorf("release leases = %+v, want devpool-3 lost with its record dropped", got.Leases)
	}
	if ssh.sawCommand("status --porcelain") {
		t.Error("the release guard read the other holder's checkout")
	}
	if releases, _ := orch.counts(); releases != 0 {
		t.Errorf("orchestrator releases = %d, want none", releases)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "leases", "devpool-3.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("record survives: %v", err)
	}
}

// TestLeaseTool_Ownership_ForcedReleaseOfAFreeContainer_NeverResetsIt: a
// forced release of a lease released outside zcp (live 4f) answers lost — the
// orchestrator would reset the pool's free container whatever lease the
// release carried, under another acquire racing for it.
func TestLeaseTool_Ownership_ForcedReleaseOfAFreeContainer_NeverResetsIt(t *testing.T) {
	t.Parallel()
	orch := &ownershipOrchestrator{}
	srv, _ := leaseToolServerSSH(t, orch, &leaseStubSSH{})
	if res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feat-x"}); res.IsError {
		t.Fatalf("acquire failed: %s", getTextContent(t, res))
	}
	orch.releaseOutside()

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "release", "lease": "devpool-3", "force": true})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("forced release failed: %s", text)
	}
	if got := decodeOwnership(t, text); len(got.Leases) != 1 || got.Leases[0].State != ops.LeaseStateLost {
		t.Errorf("release leases = %+v, want devpool-3 lost", got.Leases)
	}
	if releases, resets := orch.counts(); releases != 0 || resets != 0 {
		t.Errorf("orchestrator releases %d, free containers reset %d: want neither", releases, resets)
	}
}

// TestLeaseTool_Ownership_ParkHeldByOther_ResultSaysLost: a park the
// orchestrator answers held-by-other — another consumer took the container
// over right before the stop reached it — says the lease is lost and its
// record dropped: never "parked, still yours", never "retry in a moment".
func TestLeaseTool_Ownership_ParkHeldByOther_ResultSaysLost(t *testing.T) {
	t.Parallel()
	orch := &ownershipOrchestrator{}
	srv, stateDir := leaseToolServerSSH(t, orch, &leaseStubSSH{})
	if res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feat-x"}); res.IsError {
		t.Fatalf("acquire failed: %s", getTextContent(t, res))
	}
	orch.set(func(o *ownershipOrchestrator) { o.takeOverOnStop = true })

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "park", "lease": "devpool-3"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("park failed: %s", text)
	}
	got := decodeOwnership(t, text)
	if len(got.Leases) != 1 || got.Leases[0].State != ops.LeaseStateLost || !strings.Contains(got.Leases[0].Note, "another holder") {
		t.Errorf("park leases = %+v, want devpool-3 lost, naming another holder", got.Leases)
	}
	var next struct {
		NextActions string `json:"nextActions"`
	}
	if err := json.Unmarshal([]byte(text), &next); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(next.NextActions, "still yours") || !strings.Contains(next.NextActions, "record") {
		t.Errorf("nextActions = %q, want the dropped record, never a parked lease", next.NextActions)
	}
	if strings.Contains(text, "in a moment") {
		t.Errorf("result tells the agent to retry: %s", text)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "leases", "devpool-3.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("record survives: %v", err)
	}
}

// TestLeaseTool_Ownership_StoppedOutsideZcp_StaysParked: a stop of zcp's lease
// made outside zcp restarts the reservation's lockSince but leaves it zcp's:
// status and the envelope say parked, and the orchestrator's renew of the
// lease is what told.
func TestLeaseTool_Ownership_StoppedOutsideZcp_StaysParked(t *testing.T) {
	t.Parallel()
	orch := &ownershipOrchestrator{}
	srv, _ := leaseToolServerSSH(t, orch, &leaseStubSSH{})
	if res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feat-x"}); res.IsError {
		t.Fatalf("acquire failed: %s", getTextContent(t, res))
	}
	orch.stopOutside()

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "status", "pool": "devpool"})
	text := getTextContent(t, res)
	if res.IsError {
		t.Fatalf("status failed: %s", text)
	}
	got := decodeOwnership(t, text)
	if len(got.Leases) != 1 || got.Leases[0].State != ops.LeaseStateParked || got.Leases[0].Unverified {
		t.Errorf("status leases = %+v, want devpool-3 parked, verified", got.Leases)
	}
	if got.Envelope == nil || len(got.Envelope.Leases) == 0 || got.Envelope.Leases[0].State != ops.LeaseStateParked {
		t.Errorf("envelope leases = %+v, want devpool-3 parked", got.Envelope)
	}
	renews := 0
	orch.set(func(o *ownershipOrchestrator) { renews = o.renews })
	if renews == 0 {
		t.Error("status never renewed the lease")
	}
}

// TestLeaseTool_Ownership_RenewFails_UnverifiedNeverRead: a renew the
// orchestrator fails leaves zcp unable to tell whether the container is still
// the lease's: status keeps it held, unverified, reads nothing from it over
// ssh, and a release — forced too — is refused, nothing released.
func TestLeaseTool_Ownership_RenewFails_UnverifiedNeverRead(t *testing.T) {
	t.Parallel()
	orch := &ownershipOrchestrator{}
	ssh := &leaseStubSSH{}
	srv, stateDir := leaseToolServerSSH(t, orch, ssh)
	if res := callTool(t, srv, "zerops_lease", map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feat-x"}); res.IsError {
		t.Fatalf("acquire failed: %s", getTextContent(t, res))
	}
	orch.set(func(o *ownershipOrchestrator) { o.renewFails = true })
	ssh.mu.Lock()
	ssh.calls = nil
	ssh.mu.Unlock()

	res := callTool(t, srv, "zerops_lease", map[string]any{"action": "status", "pool": "devpool"})
	got := decodeOwnership(t, getTextContent(t, res))
	if len(got.Leases) != 1 || got.Leases[0].State != ops.LeaseStateHeld || !got.Leases[0].Unverified {
		t.Errorf("status leases = %+v, want devpool-3 held, unverified", got.Leases)
	}
	if len(ssh.calls) != 0 {
		t.Errorf("status read a lease it cannot verify over ssh: %v", ssh.calls)
	}
	res = callTool(t, srv, "zerops_lease", map[string]any{"action": "release", "lease": "devpool-3", "force": true})
	if !res.IsError {
		t.Errorf("forced release of an unverified lease = %s, want a refusal", getTextContent(t, res))
	}
	if releases, _ := orch.counts(); releases != 0 {
		t.Errorf("orchestrator releases = %d, want none", releases)
	}
	if _, err := os.Stat(filepath.Join(stateDir, "leases", "devpool-3.json")); err != nil {
		t.Errorf("record gone after a refused release: %v", err)
	}
}
