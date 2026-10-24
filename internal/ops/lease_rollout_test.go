// Tests for: ops/lease_rollout.go — rollout of a dev pool (docs/spec-workflows.md §8 DP-7).
package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// writeRawLeaseRecord writes zcp's record of the lease devpool-<n> as bytes,
// so the test shares no code with the record writer.
func writeRawLeaseRecord(t *testing.T, stateDir, containerID, number string) {
	t.Helper()
	dir := filepath.Join(stateDir, "leases")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := `{"poolHostname":"devpool","poolServiceId":"svc-pool","containerId":"` + containerID + `","containerNumber":` + number + `,` +
		`"containerHostname":"node-id-` + number + `.runtime.devpool.zerops","lease":"` + testLease + `","branch":"feat","acquiredAt":"2026-10-23T12:00:00Z"}`
	if err := os.WriteFile(filepath.Join(dir, "devpool-"+number+".json"), []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLeaseRollout_CountsAndSkippedLeases(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	writeRawLeaseRecord(t, fx.stateDir, "c-3", "3")
	fx.swarm.rollout = platform.SwarmRollout{
		ActiveAppVersionID: "av-new",
		Replaced:           []string{"c-1", "c-2"},
		Created:            []platform.SwarmContainer{{ID: "c-8", Number: 8, Current: true}, {ID: "c-9", Number: 9, Current: true}},
		Retiring:           []string{"c-4"},
		Skipped: []platform.SwarmRolloutSkip{
			{ID: "c-3", Reason: "held-by-other"},
			{ID: "c-7", Reason: "held-by-other"},
			{ID: "c-5", Reason: "removal-failed", Message: "the platform refused the removal"},
		},
		CreateErrors: []platform.SwarmRolloutCreateError{{ContainerName: "node-id-10", Message: "no capacity"}},
	}

	got, err := LeaseRollout(context.Background(), fx.deps, testPoolHost)
	if err != nil {
		t.Fatalf("LeaseRollout: %v", err)
	}
	if fx.swarm.rollouts != 1 {
		t.Errorf("rollout calls = %d, want 1", fx.swarm.rollouts)
	}
	if fx.gotURL != "http://orch.devpool.zerops" || fx.gotToken != testPoolToken {
		t.Errorf("orchestrator reached at %q with token %q, want http://orch.devpool.zerops with the pool's API_TOKEN", fx.gotURL, fx.gotToken)
	}
	if got.Pool != testPoolHost || got.ActiveAppVersionID != "av-new" {
		t.Errorf("pool/appVersion = %q/%q, want %s/av-new", got.Pool, got.ActiveAppVersionID, testPoolHost)
	}
	if got.Replaced != 2 || got.Created != 2 || got.Retiring != 1 || got.Skipped != 3 {
		t.Errorf("replaced/created/retiring/skipped = %d/%d/%d/%d, want 2/2/1/3", got.Replaced, got.Created, got.Retiring, got.Skipped)
	}
	want := []LeaseRolloutSkip{
		{ContainerID: "c-3", Lease: "devpool-3", Reason: "held-by-other"},
		{ContainerID: "c-7", Reason: "held-by-other"},
		{ContainerID: "c-5", Reason: "removal-failed", Message: "the platform refused the removal"},
	}
	if len(got.Skips) != len(want) {
		t.Fatalf("skips = %+v, want %+v", got.Skips, want)
	}
	for i, w := range want {
		if got.Skips[i] != w {
			t.Errorf("skips[%d] = %+v, want %+v (zcp's own lease named by its alias)", i, got.Skips[i], w)
		}
	}
	if len(got.CreateErrors) != 1 || !strings.Contains(got.CreateErrors[0], "node-id-10") || !strings.Contains(got.CreateErrors[0], "no capacity") {
		t.Errorf("createErrors = %v, want the failed node-id-10 with its message", got.CreateErrors)
	}
	for _, want := range []string{"held-by-other", "keep their version", "released", "rolled out again"} {
		if !strings.Contains(got.Note, want) {
			t.Errorf("note %q lacks %q", got.Note, want)
		}
	}
}

func TestLeaseRollout_NotAPool_Refused(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		pool     string
		wantCode string
		wantText []string
	}{
		{name: "a runtime, not a pool", pool: "appdev", wantCode: platform.ErrInvalidParameter, wantText: []string{"appdev", "nodejs@22", "not a Swarm pool"}},
		{name: "no such service", pool: "ghost", wantCode: platform.ErrServiceNotFound, wantText: []string{"ghost"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fx := newLeaseFixture(t)
			_, err := LeaseRollout(context.Background(), fx.deps, tt.pool)
			var pe *platform.PlatformError
			if !errors.As(err, &pe) || pe.Code != tt.wantCode {
				t.Fatalf("err = %v, want a *PlatformError with code %s", err, tt.wantCode)
			}
			for _, w := range tt.wantText {
				if !strings.Contains(pe.Message, w) {
					t.Errorf("message %q lacks %q", pe.Message, w)
				}
			}
			if fx.gotURL != "" || fx.swarm.rollouts != 0 {
				t.Errorf("an orchestrator was reached (%q, %d rollouts) for %s", fx.gotURL, fx.swarm.rollouts, tt.pool)
			}
		})
	}
}

func TestLeaseRollout_PoolFixedSize_Refused(t *testing.T) {
	t.Parallel()
	fx := newLeaseFixture(t)
	fx.swarm.rolloutErr = &platform.SwarmError{Status: 400, Code: "containerAction", Message: "nothing can be replaced in this pool", Reason: "pool-fixed-size"}

	_, err := LeaseRollout(context.Background(), fx.deps, testPoolHost)
	var pe *platform.PlatformError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %v, want a *PlatformError", err)
	}
	if pe.APICode != "pool-fixed-size" {
		t.Errorf("APICode = %q, want pool-fixed-size", pe.APICode)
	}
	for _, want := range []string{testPoolHost, "minimum", "maximum", "room"} {
		if !strings.Contains(pe.Message, want) {
			t.Errorf("message %q lacks %q", pe.Message, want)
		}
	}
	if !strings.Contains(pe.Suggestion, "zerops_scale") {
		t.Errorf("suggestion %q does not say how to make room (zerops_scale)", pe.Suggestion)
	}
}
