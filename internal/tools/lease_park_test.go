// Tests for: tools/lease.go — zerops_lease action=park / action=unpark, and
// the tools that refuse a parked lease (docs/spec-workflows.md §8 DP-28).
package tools

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/ops/devservertest"
	"github.com/zeropsio/zcp/internal/platform"
)

// parkOrchestrator is a pool orchestrator holding container #3 that a holder
// can stop and start with its lease, read by id, and release.
type parkOrchestrator struct {
	mu       sync.Mutex
	reserved bool
	status   string
	powers   []string // "<path> <X-Swarm-Lease is the lease> <query>"
}

func (o *parkOrchestrator) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer "+leaseTestToken {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if o.status == "" {
		o.status = "ACTIVE"
	}
	lock := ""
	if o.reserved {
		lock = "reserved"
	}
	container := func(withLease bool) string {
		c := `{"id":"c-abc","name":"node-id-3","number":3,"hostname":"node-id-3.runtime.devpool.zerops","status":"` + o.status + `",` +
			`"serviceId":"svc-pool","stackId":"stk","appVersionId":"av","current":true,"lockState":"` + lock + `","dirty":false`
		if withLease {
			c += `,"lease":"` + leaseTestLease + `"`
		}
		return c + `}`
	}
	held := r.Header.Get("X-Swarm-Lease") == leaseTestLease
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/container":
		_, _ = w.Write([]byte(`[` + container(false) + `]`))
	case r.Method == http.MethodGet && r.URL.Path == "/container/c-abc":
		_, _ = w.Write([]byte(container(false)))
	case r.Method == http.MethodPost && r.URL.Path == "/container/acquire":
		o.reserved, lock = true, "reserved"
		_, _ = w.Write([]byte(container(true)))
	case r.Method == http.MethodPost && (r.URL.Path == "/container/c-abc/stop" || r.URL.Path == "/container/c-abc/start"):
		o.powers = append(o.powers, r.URL.Path+" "+boolText(held)+" "+r.URL.RawQuery)
		if !held {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":{"code":"containerAction","message":"nope","meta":[{"metadata":{"reason":["unknown-lease"],"retryable":["false"]}}]}}`))
			return
		}
		o.status = "ACTIVE"
		if strings.HasSuffix(r.URL.Path, "/stop") {
			o.status = "STOPPED"
		}
		_, _ = w.Write([]byte(container(false)))
	default:
		w.WriteHeader(http.StatusNotFound)
	}
}

func boolText(b bool) string {
	if b {
		return "lease"
	}
	return "no-lease"
}

func (o *parkOrchestrator) log() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.powers...)
}

// parkResult is the part of a zerops_lease result the park tests read.
type parkResult struct {
	Action string `json:"action"`
	Leases []struct {
		Alias    string `json:"alias"`
		State    string `json:"state"`
		ParkedAt string `json:"parkedAt"`
		Note     string `json:"note"`
	} `json:"leases"`
	NextActions string `json:"nextActions"`
	Envelope    *struct {
		Leases []struct {
			Alias string `json:"alias"`
			State string `json:"state"`
		} `json:"leases"`
	} `json:"envelope"`
}

// TestLeaseTool_ParkThenUnpark_StateRidesResultAndEnvelope: action=park stops
// the lease's container with its lease (in the header only) and answers it
// parked — in the result, with parkedAt and what starts it again, and in the
// envelope's leases[] — status lists it parked, and action=unpark starts it
// and answers it held. No result carries the lease.
func TestLeaseTool_ParkThenUnpark_StateRidesResultAndEnvelope(t *testing.T) {
	t.Parallel()
	orch := &parkOrchestrator{}
	srv, stateDir := leaseToolServer(t, orch)
	call := func(args map[string]any) parkResult {
		t.Helper()
		res := callTool(t, srv, "zerops_lease", args)
		text := getTextContent(t, res)
		if res.IsError {
			t.Fatalf("zerops_lease %v failed: %s", args["action"], text)
		}
		for _, secret := range []string{leaseTestLease, leaseTestToken} {
			if strings.Contains(text, secret) {
				t.Fatalf("zerops_lease %v result carries a secret: %s", args["action"], text)
			}
		}
		var out parkResult
		if err := json.Unmarshal([]byte(text), &out); err != nil {
			t.Fatalf("result is not JSON: %v\n%s", err, text)
		}
		return out
	}

	call(map[string]any{"action": "acquire", "pool": "devpool", "repo": leaseTestRepo, "branch": "feature-x"})

	parked := call(map[string]any{"action": "park", "lease": "devpool-3"})
	if len(parked.Leases) != 1 || parked.Leases[0].State != ops.LeaseStateParked || parked.Leases[0].ParkedAt == "" ||
		!strings.Contains(parked.Leases[0].Note, "zerops_lease action=unpark lease=devpool-3") {
		t.Fatalf("park leases = %+v, want devpool-3 parked with its parkedAt and the unpark", parked.Leases)
	}
	if !strings.Contains(parked.NextActions, "zerops_lease action=unpark lease=devpool-3") {
		t.Errorf("park nextActions %q should name the unpark", parked.NextActions)
	}
	if parked.Envelope == nil || len(parked.Envelope.Leases) != 1 || parked.Envelope.Leases[0].State != ops.LeaseStateParked {
		t.Errorf("park envelope = %+v, want leases[] with devpool-3 parked", parked.Envelope)
	}
	raw, err := os.ReadFile(filepath.Join(stateDir, "leases", "devpool-3.json"))
	if err != nil || !strings.Contains(string(raw), `"parked": true`) {
		t.Errorf("record = %s (%v), want it parked", raw, err)
	}

	status := call(map[string]any{"action": "status"})
	if len(status.Leases) != 1 || status.Leases[0].State != ops.LeaseStateParked || !strings.Contains(status.NextActions, "Parked") {
		t.Errorf("status = %+v / %q, want devpool-3 parked, named in nextActions", status.Leases, status.NextActions)
	}

	unparked := call(map[string]any{"action": "unpark", "lease": "devpool-3"})
	if len(unparked.Leases) != 1 || unparked.Leases[0].State != ops.LeaseStateHeld || unparked.Leases[0].ParkedAt != "" {
		t.Errorf("unpark leases = %+v, want devpool-3 held", unparked.Leases)
	}
	if unparked.Envelope == nil || len(unparked.Envelope.Leases) != 1 || unparked.Envelope.Leases[0].State != ops.LeaseStateHeld {
		t.Errorf("unpark envelope = %+v, want leases[] with devpool-3 held", unparked.Envelope)
	}
	want := []string{"/container/c-abc/stop lease ", "/container/c-abc/start lease "}
	if got := orch.log(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("orchestrator stops and starts = %q, want %q (each with the lease in its header, no query)", got, want)
	}
}

// TestLeaseTool_ParkUnpark_BadInput_TypedRefusal: park and unpark take one
// lease alias alone.
func TestLeaseTool_ParkUnpark_BadInput_TypedRefusal(t *testing.T) {
	t.Parallel()
	srv, _ := leaseToolServer(t, &parkOrchestrator{})
	for _, tt := range []struct {
		name     string
		args     map[string]any
		wantCode string
		wantText string
	}{
		{"park without lease", map[string]any{"action": "park"}, platform.ErrInvalidParameter, "park needs lease"},
		{"unpark without lease", map[string]any{"action": "unpark"}, platform.ErrInvalidParameter, "unpark needs lease"},
		{"park with a pool", map[string]any{"action": "park", "lease": "devpool-3", "pool": "devpool"}, platform.ErrInvalidParameter, "takes lease alone"},
		{"unpark with force", map[string]any{"action": "unpark", "lease": "devpool-3", "force": true}, platform.ErrInvalidParameter, "takes lease alone"},
		{"park an unrecorded alias", map[string]any{"action": "park", "lease": "devpool-9"}, platform.ErrInvalidParameter, "No lease record"},
		{"park with pools", map[string]any{"action": "park", "lease": "devpool-3", "pools": []string{"devpool"}}, platform.ErrInvalidParameter, "pools is for acquire only"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := callTool(t, srv, "zerops_lease", tt.args)
			text := getTextContent(t, res)
			if !res.IsError || !strings.Contains(text, tt.wantCode) || !strings.Contains(text, tt.wantText) {
				t.Errorf("result = (error %v) %s, want an error with code %s mentioning %q", res.IsError, text, tt.wantCode, tt.wantText)
			}
		})
	}
}

// parkLeaseFile marks the recorded lease alias parked, as zerops_lease
// action=park writes it.
func parkLeaseFile(t *testing.T, stateDir, alias string) {
	t.Helper()
	path := filepath.Join(stateDir, "leases", alias+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read lease record: %v", err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatalf("parse lease record: %v", err)
	}
	rec["parked"], rec["parkedAt"] = true, "2026-10-24T09:00:00Z"
	out, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write lease record: %v", err)
	}
}

// TestDeployTool_ParkedLeaseSource_RefusedBeforeAnyRead: zerops_deploy and
// zerops_deploy_batch from a parked lease are refused before pre-flight reads
// its (unmounted) checkout or anything reaches it over ssh, naming the unpark.
func TestDeployTool_ParkedLeaseSource_RefusedBeforeAnyRead(t *testing.T) {
	t.Parallel()

	t.Run("zerops_deploy", func(t *testing.T) {
		t.Parallel()
		rig := newLeaseDeployRig(t)
		parkLeaseFile(t, rig.stateDir, rig.alias)
		ssh := &leaseSSH{}
		result, text := rig.deploy(t, ssh, map[string]any{"sourceService": rig.alias, "targetService": "appstage"})
		if !result.IsError || !strings.Contains(text, rig.alias+" is parked") || !strings.Contains(text, "zerops_lease action=unpark lease="+rig.alias) {
			t.Fatalf("result = (error %v) %s, want a refusal naming the parked lease and its unpark", result.IsError, text)
		}
		if strings.Contains(text, "Pre-flight") || len(ssh.hosts()) != 0 {
			t.Errorf("pre-flight or ssh ran before the refusal (ssh hosts %v): %s", ssh.hosts(), text)
		}
	})

	t.Run("zerops_deploy_batch", func(t *testing.T) {
		t.Parallel()
		rig := newLeaseDeployRig(t)
		parkLeaseFile(t, rig.stateDir, rig.alias)
		ssh := &leaseSSH{}
		result, _, text := runPoolBatch(t, rig.mock, ssh, rig.stateDir, []map[string]any{{"sourceService": rig.alias, "targetService": "appstage"}})
		if !result.IsError || !strings.Contains(text, rig.alias+" is parked") || !strings.Contains(text, "zerops_lease action=unpark lease="+rig.alias) {
			t.Fatalf("result = (error %v) %s, want a refusal naming the parked lease and its unpark", result.IsError, text)
		}
		if strings.Contains(text, "Pre-flight") || len(ssh.hosts()) != 0 {
			t.Errorf("pre-flight or ssh ran before the refusal (ssh hosts %v): %s", ssh.hosts(), text)
		}
	})
}

// TestVerify_ParkedLease_NoProbeNamesUnpark: zerops_verify of a parked lease
// reports it unreachable with the unpark, without waiting out an ssh probe of
// a stopped container.
func TestVerify_ParkedLease_NoProbeNamesUnpark(t *testing.T) {
	t.Parallel()
	stateDir := filepath.Join(t.TempDir(), ".zcp", "state")
	writeLeaseFile(t, stateDir)
	parkLeaseFile(t, stateDir, "webpool-3")
	ssh := &hostSSH{inner: devservertest.NewSupervised("")}

	result, text := verifyLeaseCall(t, stateDir, ssh, "webpool-3")
	if result.IsError {
		t.Fatalf("verify failed: %s", text)
	}
	var vr ops.VerifyResult
	if err := json.Unmarshal([]byte(text), &vr); err != nil {
		t.Fatalf("result is not JSON: %v\n%s", err, text)
	}
	reach := checkByName(vr, "ssh_reachable")
	if vr.Status != ops.StatusUnhealthy || len(reach) != 1 || reach[0].Status != ops.CheckFail ||
		!strings.Contains(reach[0].Detail, "is parked") || !strings.Contains(reach[0].Detail, "zerops_lease action=unpark lease=webpool-3") {
		t.Errorf("verify = %+v, want unhealthy, ssh_reachable failing with the parked lease and its unpark", vr)
	}
	ssh.mu.Lock()
	defer ssh.mu.Unlock()
	if len(ssh.hosts) != 0 {
		t.Errorf("ssh reached %v for a parked lease", ssh.hosts)
	}
}
