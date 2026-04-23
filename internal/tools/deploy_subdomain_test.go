// Tests for: deploy_subdomain.go — post-deploy subdomain auto-enable.
//
// Verifies the deploy handler hook that activates the L7 route on first
// deploy for dev/stage/simple/standard/local-stage modes, skips for other
// modes and for services without ZCP meta, and survives platform failures
// as best-effort warnings on the DeployResult.

package tools

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/workflow"
)

// autoEnableTestMock builds a mock with a single service fixture ready for
// the auto-enable path. subdomainOn controls platform-side SubdomainAccess.
func autoEnableTestMock(t *testing.T, serviceID, hostname string, subdomainOn bool) *platform.Mock {
	t.Helper()
	svc := platform.ServiceStack{
		ID:              serviceID,
		Name:            hostname,
		ProjectID:       "proj-1",
		SubdomainAccess: subdomainOn,
		Ports:           []platform.Port{{Port: 3000, Protocol: "tcp"}},
	}
	return platform.NewMock().
		WithServices([]platform.ServiceStack{svc}).
		WithService(&svc).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "test", Status: statusActive,
			SubdomainHost: "abc1.prg1.zerops.app",
		}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-enable-" + serviceID,
			Status: statusFinished,
		})
}

func writeMeta(t *testing.T, dir, hostname string, mode workflow.Mode) {
	t.Helper()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         hostname,
		Mode:             mode,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-22",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

func TestMaybeAutoEnableSubdomain_FirstDeploy_DevMode_Enables(t *testing.T) {
	t.Parallel()
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	dir := t.TempDir()
	writeMeta(t, dir, "app", workflow.PlanModeDev)

	mock := autoEnableTestMock(t, "svc-1", "app", false /* subdomain off */)
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	maybeAutoEnableSubdomain(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if !result.SubdomainAccessEnabled {
		t.Error("SubdomainAccessEnabled: want true, got false")
	}
	if result.SubdomainURL == "" {
		t.Error("SubdomainURL: want non-empty, got empty")
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 1 {
		t.Errorf("EnableSubdomainAccess calls: want 1, got %d", mock.CallCounts["EnableSubdomainAccess"])
	}
}

func TestMaybeAutoEnableSubdomain_SubdomainAlreadyOn_SetsFlag_NoAPICall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMeta(t, dir, "app", workflow.PlanModeStandard)

	mock := autoEnableTestMock(t, "svc-1", "app", true /* subdomain on */)
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	maybeAutoEnableSubdomain(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if !result.SubdomainAccessEnabled {
		t.Error("SubdomainAccessEnabled: want true on already-on, got false")
	}
	if result.SubdomainURL == "" {
		t.Error("SubdomainURL: want non-empty (URLs built from cached meta), got empty")
	}
	// Core Plan 1 invariant: no redundant enable API call when already active.
	if mock.CallCounts["EnableSubdomainAccess"] != 0 {
		t.Errorf("EnableSubdomainAccess calls: want 0 (already-on), got %d", mock.CallCounts["EnableSubdomainAccess"])
	}
}

func TestMaybeAutoEnableSubdomain_NoMeta_Skipped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // no meta written

	mock := autoEnableTestMock(t, "svc-1", "app", false)
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	maybeAutoEnableSubdomain(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if result.SubdomainAccessEnabled {
		t.Error("no meta must skip auto-enable; SubdomainAccessEnabled should stay false")
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 0 {
		t.Errorf("EnableSubdomainAccess calls: want 0 (no meta), got %d", mock.CallCounts["EnableSubdomainAccess"])
	}
}

func TestMaybeAutoEnableSubdomain_LocalOnlyMode_Skipped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMeta(t, dir, "app", workflow.PlanModeLocalOnly)

	mock := autoEnableTestMock(t, "svc-1", "app", false)
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	maybeAutoEnableSubdomain(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if result.SubdomainAccessEnabled {
		t.Error("local-only mode must skip auto-enable (no Zerops runtime to route to)")
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 0 {
		t.Errorf("EnableSubdomainAccess calls: want 0 (local-only), got %d", mock.CallCounts["EnableSubdomainAccess"])
	}
}

func TestMaybeAutoEnableSubdomain_EnableFails_WarningNotFatal(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMeta(t, dir, "app", workflow.PlanModeDev)

	mock := autoEnableTestMock(t, "svc-1", "app", false)
	mock.WithError("EnableSubdomainAccess", &platform.PlatformError{
		Code:    platform.ErrAPIError,
		Message: "transient platform failure",
	})
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	maybeAutoEnableSubdomain(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if result.SubdomainAccessEnabled {
		t.Error("must NOT set SubdomainAccessEnabled when enable failed")
	}
	if len(result.Warnings) == 0 {
		t.Fatal("want warnings populated on enable failure")
	}
	found := false
	for _, w := range result.Warnings {
		if strings.Contains(w, "auto-enable subdomain failed") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Warnings must include auto-enable failure; got %v", result.Warnings)
	}
}

func TestMaybeAutoEnableSubdomain_AllEligibleModes_TriggerEnable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		mode workflow.Mode
		want bool
	}{
		{"Dev", workflow.PlanModeDev, true},
		{"Standard", workflow.PlanModeStandard, true},
		{"Stage", workflow.ModeStage, true},
		{"Simple", workflow.PlanModeSimple, true},
		{"LocalStage", workflow.PlanModeLocalStage, true},
		{"LocalOnly", workflow.PlanModeLocalOnly, false},
		{"Unknown", workflow.Mode("production-future"), false}, // future-proof: new modes default to skip
		{"Empty", workflow.Mode(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
			defer restore()

			dir := t.TempDir()
			writeMeta(t, dir, "app", tc.mode)

			mock := autoEnableTestMock(t, "svc-1", "app", false)
			result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}
			maybeAutoEnableSubdomain(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

			if result.SubdomainAccessEnabled != tc.want {
				t.Errorf("mode %q: SubdomainAccessEnabled = %v, want %v", tc.mode, result.SubdomainAccessEnabled, tc.want)
			}
		})
	}
}

// Regression pin for the E8 pair-keyed invariant: stage cross-deploy must
// resolve the meta via FindServiceMeta (dev-pair-keyed) and — crucially —
// still auto-enable when SubdomainAccess==false on the stage service.
// meta.FirstDeployedAt is stamped on the dev half and useless as a
// first-deploy signal for stage; only platform-side SubdomainAccess is
// authoritative.
func TestMaybeAutoEnableSubdomain_StageCrossDeploy_EnablesForStage(t *testing.T) {
	t.Parallel()
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	dir := t.TempDir()
	// Pair meta keyed by dev, StageHostname set. Simulates an already-
	// deployed dev (FirstDeployedAt stamped) and a stage that hasn't been
	// deployed yet.
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		StageHostname:    "appstage",
		Mode:             workflow.PlanModeStandard,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-22",
		FirstDeployedAt:  "2026-04-22", // dev already deployed; stage hasn't
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	// Stage service (different serviceID), SubdomainAccess=false (platform
	// truth: stage L7 hasn't been activated).
	stageSvc := platform.ServiceStack{
		ID:              "svc-stage",
		Name:            "appstage",
		ProjectID:       "proj-1",
		SubdomainAccess: false,
		Ports:           []platform.Port{{Port: 3000, Protocol: "tcp"}},
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev", SubdomainAccess: true,
				Ports: []platform.Port{{Port: 3000, Protocol: "tcp"}}},
			stageSvc,
		}).
		WithService(&stageSvc).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "test", Status: statusActive,
			SubdomainHost: "abc1.prg1.zerops.app",
		}).
		WithProcess(&platform.Process{
			ID:     "proc-subdomain-enable-svc-stage",
			Status: statusFinished,
		})

	result := &ops.DeployResult{TargetService: "appstage", TargetServiceID: "svc-stage"}
	maybeAutoEnableSubdomain(context.Background(), mock, okHTTP, "proj-1", dir, "appstage", result)

	if !result.SubdomainAccessEnabled {
		t.Error("stage cross-deploy must trigger enable even though dev-side FirstDeployedAt is stamped — gate is platform-side (E8)")
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 1 {
		t.Errorf("EnableSubdomainAccess calls: want 1 for stage cross-deploy, got %d", mock.CallCounts["EnableSubdomainAccess"])
	}
}

// Verify the HTTP readiness wait skips on already-enabled subdomain — the
// L7 route is live since the earlier enable, so probing adds latency for
// no signal. Fresh enables do probe.
func TestMaybeAutoEnableSubdomain_AlreadyEnabled_SkipsHTTPProbe(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMeta(t, dir, "app", workflow.PlanModeDev)

	mock := autoEnableTestMock(t, "svc-1", "app", true /* already on */)

	// HTTPDoer that counts calls so we can verify it was NOT invoked.
	doer := &countingDoer{status: http.StatusOK}
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}
	maybeAutoEnableSubdomain(context.Background(), mock, doer, "proj-1", dir, "app", result)

	if doer.calls != 0 {
		t.Errorf("HTTP probe must be skipped on already_enabled; got %d calls", doer.calls)
	}
}

type countingDoer struct {
	calls  int
	status int
}

func (d *countingDoer) Do(*http.Request) (*http.Response, error) {
	d.calls++
	return &http.Response{StatusCode: d.status, Body: io.NopCloser(strings.NewReader(""))}, nil
}
