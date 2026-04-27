// Tests for: deploy_subdomain.go — post-deploy subdomain auto-enable.
//
// Verifies the deploy handler hook that activates the L7 route on first
// deploy for dev/stage/simple/standard/local-stage modes (when meta is
// present), falls back to the platform-state predicate for recipe-
// authoring deploys (which never write meta), and survives platform
// failures as best-effort warnings on the DeployResult. Cluster A.2 of
// run-14-readiness extended the meta-nil branch — recipe-authoring
// services with an HTTP-supporting port now auto-enable the same as
// bootstrap-managed services do.

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
	"github.com/zeropsio/zcp/internal/topology"
	"github.com/zeropsio/zcp/internal/workflow"
)

// The single-service fixtures in this file share one hostname ("app") and
// one serviceID ("svc-1"). The stage-pair fixture builds its own mock
// inline. Keeping these fixture constants inline rather than parameterized
// keeps unparam happy and makes the test intent explicit: the helpers are
// for "the default single-service case".
const (
	autoEnableTestHostname  = "app"
	autoEnableTestServiceID = "svc-1"
)

// autoEnableTestMock builds a mock with the default single-service
// fixture. subdomainOn controls platform-side SubdomainAccess.
func autoEnableTestMock(t *testing.T, subdomainOn bool) *platform.Mock {
	t.Helper()
	svc := platform.ServiceStack{
		ID:              autoEnableTestServiceID,
		Name:            autoEnableTestHostname,
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
			ID:     "proc-subdomain-enable-" + autoEnableTestServiceID,
			Status: statusFinished,
		})
}

func writeMeta(t *testing.T, dir string, mode topology.Mode) {
	t.Helper()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         autoEnableTestHostname,
		Mode:             mode,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-22",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

func TestMaybeAutoEnableSubdomain_FirstDeploy_DevMode_Enables(t *testing.T) {
	// t.Parallel omitted — OverrideHTTPReadyConfigForTest mutates a
	// package-level config; parallel tests would clobber each other's
	// interval/timeout values even though the mutex keeps the race
	// detector green.
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	dir := t.TempDir()
	writeMeta(t, dir, topology.PlanModeDev)

	mock := autoEnableTestMock(t, false /* subdomain off */)
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
	writeMeta(t, dir, topology.PlanModeStandard)

	mock := autoEnableTestMock(t, true /* subdomain on */)
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

// TestMaybeAutoEnable_NoMeta_PlanDeclaredIntent_Dispatches pins
// Cluster A.2 (R-13-12) + run-15 R-14-1. Recipe-authoring deploys land
// via zerops_import content=<yaml> which never writes the per-PID
// ServiceMeta. The auto-enable path derives eligibility from plan-
// declared intent persisted by the platform: yaml `enableSubdomainAccess:
// true` becomes detail.SubdomainAccess at import time. With the intent
// in place, ops.Subdomain returns already_enabled (no fresh API call)
// and surfaces the URLs.
//
// Why HTTPSupport is NOT used here: ports[].HTTPSupport races L7 port-
// registration on the FIRST cross-deploy of every stage slot (run-14
// scaffold-app burned three manual zerops_subdomain action=enable calls
// on this race). detail.SubdomainAccess is set at yaml-import and does
// not race deploy-time port propagation — that's the run-15 fix.
//
// I/O boundary: ops.LookupService → REST API; client.GetService → REST
// API (REST-authoritative). Reads pre-deploy intent flag, not deploy-
// time port propagation; no race-prone surface in the read path.
func TestMaybeAutoEnable_NoMeta_PlanDeclaredIntent_Dispatches(t *testing.T) {
	// Override pulled in to keep the readiness probe under test latency.
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	dir := t.TempDir() // no meta written — recipe-authoring scenario.

	// Plan-declared intent: SubdomainAccess=true (yaml had
	// enableSubdomainAccess: true at import). Ports DO NOT carry
	// HTTPSupport=true — simulates the FIRST cross-deploy propagation
	// race that R-14-1 names. The fix: don't gate on HTTPSupport.
	svc := platform.ServiceStack{
		ID:              autoEnableTestServiceID,
		Name:            autoEnableTestHostname,
		ProjectID:       "proj-1",
		SubdomainAccess: true,
		Ports:           []platform.Port{{Port: 3000, Protocol: "tcp"}},
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeCategoryName: "USER",
		},
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{svc}).
		WithService(&svc).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "test", Status: statusActive,
			SubdomainHost: "abc1.prg1.zerops.app",
		})

	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}
	maybeAutoEnableSubdomain(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if !result.SubdomainAccessEnabled {
		t.Error("plan-declared intent (SubdomainAccess=true) should auto-enable; stayed false")
	}
	// SubdomainAccess already true → ops.Subdomain short-circuits as
	// already_enabled; no EnableSubdomainAccess API call.
	if mock.CallCounts["EnableSubdomainAccess"] != 0 {
		t.Errorf("EnableSubdomainAccess calls: want 0 (already_enabled short-circuit), got %d", mock.CallCounts["EnableSubdomainAccess"])
	}
}

// TestMaybeAutoEnable_NoMeta_PlanNotDeclared_Skips pins the safety side
// of run-15 R-14-1: a recipe-authoring service whose yaml omitted
// enableSubdomainAccess (worker, or any role the plan declared without
// subdomain) MUST NOT trigger auto-enable. The plan's intent is the
// gate.
func TestMaybeAutoEnable_NoMeta_PlanNotDeclared_Skips(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // no meta written

	// Default fixture has SubdomainAccess=false.
	mock := autoEnableTestMock(t, false)
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	maybeAutoEnableSubdomain(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if result.SubdomainAccessEnabled {
		t.Error("plan-undeclared (SubdomainAccess=false) should skip auto-enable; stayed true")
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 0 {
		t.Errorf("EnableSubdomainAccess calls: want 0 (plan-undeclared), got %d", mock.CallCounts["EnableSubdomainAccess"])
	}
}

// TestMaybeAutoEnable_NoMeta_SystemService_Skips pins that system-
// category services (BUILD/CORE/PREPARE_RUNTIME/HTTP_L7_BALANCER) skip
// auto-enable even when the meta-nil fallback runs. They never serve
// porter HTTP traffic; the auto-enable would be wrong for them.
func TestMaybeAutoEnable_NoMeta_SystemService_Skips(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // no meta

	svc := platform.ServiceStack{
		ID:              autoEnableTestServiceID,
		Name:            autoEnableTestHostname,
		ProjectID:       "proj-1",
		SubdomainAccess: false,
		Ports:           []platform.Port{{Port: 3000, Protocol: "tcp", HTTPSupport: true}},
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeCategoryName: "BUILD",
		},
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{svc}).
		WithService(&svc).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "test", Status: statusActive,
			SubdomainHost: "abc1.prg1.zerops.app",
		})

	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}
	maybeAutoEnableSubdomain(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if result.SubdomainAccessEnabled {
		t.Error("system-category service should skip auto-enable")
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 0 {
		t.Errorf("EnableSubdomainAccess calls: want 0 (system service), got %d", mock.CallCounts["EnableSubdomainAccess"])
	}
}

func TestMaybeAutoEnableSubdomain_LocalOnlyMode_Skipped(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMeta(t, dir, topology.PlanModeLocalOnly)

	mock := autoEnableTestMock(t, false)
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
	writeMeta(t, dir, topology.PlanModeDev)

	mock := autoEnableTestMock(t, false)
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
	// t.Parallel omitted at the top level so the Override helper's config
	// mutation doesn't interleave with sibling tests in the package.
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	cases := []struct {
		name string
		mode topology.Mode
		want bool
	}{
		{"Dev", topology.PlanModeDev, true},
		{"Standard", topology.PlanModeStandard, true},
		{"Stage", topology.ModeStage, true},
		{"Simple", topology.PlanModeSimple, true},
		{"LocalStage", topology.PlanModeLocalStage, true},
		{"LocalOnly", topology.PlanModeLocalOnly, false},
		{"Unknown", topology.Mode("production-future"), false}, // future-proof: new modes default to skip
		{"Empty", topology.Mode(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Subtests share the parent's Override restore; no per-subtest
			// override needed, and no t.Parallel (see parent comment).

			dir := t.TempDir()
			writeMeta(t, dir, tc.mode)

			mock := autoEnableTestMock(t, false)
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
	// t.Parallel omitted — see TestMaybeAutoEnableSubdomain_FirstDeploy_DevMode_Enables.
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	dir := t.TempDir()
	// Pair meta keyed by dev, StageHostname set. Simulates an already-
	// deployed dev (FirstDeployedAt stamped) and a stage that hasn't been
	// deployed yet.
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",
		StageHostname:    "appstage",
		Mode:             topology.PlanModeStandard,
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
	writeMeta(t, dir, topology.PlanModeDev)

	mock := autoEnableTestMock(t, true /* already on */)

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
