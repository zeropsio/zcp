package ops

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// orchestratorRefusal is the error a real orchestrator client returns for an
// answer of status and body: the refusal a lease call meets, parsed as the
// client parses it.
func orchestratorRefusal(t *testing.T, status int, body string) error {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	_, err := platform.NewSwarmClient(srv.URL, testPoolToken, srv.Client()).Release(context.Background(), "c-abc", testLease, true)
	if err == nil {
		t.Fatal("the orchestrator's refusal parsed as no error")
	}
	return err
}

// TestLeaseRelease_OrchestratorRefusal_SaysWhatItsMetadataSays: an
// orchestrator refusal whose message is the platform's "See metadata"
// reaches the agent with what its metadata says, scrubbed of the bearer and
// the lease (live, Kara's run 2026-10-25: "Pool 'webpool' orchestrator
// refused (release webpool-5): See metadata", and the cause never arrived).
func TestLeaseRelease_OrchestratorRefusal_SaysWhatItsMetadataSays(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	fx.swarm.acquired = acquiredAs(3)
	if _, err := LeaseAcquire(context.Background(), fx.deps, testPoolHost, testRepo, ""); err != nil {
		t.Fatal(err)
	}
	fx.swarm.releaseErr = orchestratorRefusal(t, http.StatusBadRequest,
		`{"error":{"code":"containerAction","message":"See metadata","meta":[{"code":"commandExec",`+
			`"error":"fork/exec /opt/zerops/bin/zsc: resource temporarily unavailable",`+
			`"metadata":{"echo":["`+testPoolToken+` `+testLease+`"]}}]}}`)

	_, err := LeaseRelease(context.Background(), fx.deps, "devpool-3", true)
	if err == nil {
		t.Fatal("want the release refused")
	}
	msg := err.Error()
	for _, want := range []string{"orchestrator refused (release devpool-3)", "commandExec", "fork/exec /opt/zerops/bin/zsc: resource temporarily unavailable"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not say %q", msg, want)
		}
	}
	for _, not := range []string{"See metadata", testPoolToken, testLease} {
		if strings.Contains(msg, not) {
			t.Errorf("error %q says %q", msg, not)
		}
	}
}
