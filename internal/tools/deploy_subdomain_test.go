// Tests for: deploy_subdomain.go — post-deploy subdomain auto-enable.
//
// Verifies the deploy handler hook that activates the L7 route on first
// deploy for dev/stage/simple/standard/local-stage modes. Phase 1 of the
// foundation fix (plans/subdomain-auto-enable-foundation-fix-2026-05-03.md)
// rewrote the predicate to drop broken DTO checks; the test suite here
// covers the new mode-allowlist + IsSystem() defensive guard, and the
// caller-side serviceStackIsNotHttp benign-skip classification.

package tools

import (
	"context"
	"errors"
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

const (
	autoEnableTestHostname  = "app"
	autoEnableTestServiceID = "svc-1"
)

// autoEnableTestMock builds a mock with the default single-service
// fixture matching real platform pre-enable state. subdomainOn controls
// platform-side SubdomainAccess (which the platform sets only after a
// successful EnableSubdomainAccess call — see live evidence in plans/
// subdomain-auto-enable-foundation-fix-2026-05-03.md §2.2).
//
// Default port HTTPSupport=false: the platform's HttpRouting field (mapped
// to ZCP's HTTPSupport) only flips true alongside subdomain enable, NOT from
// the deployed zerops.yaml's ports[].httpSupport. The new predicate doesn't
// consult Ports[].HTTPSupport at all, so this fixture default matches reality
// and predicate truth values stay correct.
func autoEnableTestMock(t *testing.T, subdomainOn bool) *platform.Mock {
	t.Helper()
	svc := platform.ServiceStack{
		ID:              autoEnableTestServiceID,
		Name:            autoEnableTestHostname,
		ProjectID:       "proj-1",
		SubdomainAccess: subdomainOn,
		Ports:           []platform.Port{{Port: 3000, Protocol: "tcp"}},
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeCategoryName: "USER",
		},
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

// systemStackMock builds a mock with a system-category service (BUILD,
// CORE, etc.) so the IsSystem() defensive guard can be pinned.
func systemStackMock(t *testing.T, category string) *platform.Mock {
	t.Helper()
	svc := platform.ServiceStack{
		ID:        autoEnableTestServiceID,
		Name:      autoEnableTestHostname,
		ProjectID: "proj-1",
		Ports:     []platform.Port{{Port: 3000, Protocol: "tcp"}},
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeCategoryName: category,
		},
	}
	return platform.NewMock().
		WithServices([]platform.ServiceStack{svc}).
		WithService(&svc).
		WithProject(&platform.Project{
			ID: "proj-1", Name: "test", Status: statusActive,
			SubdomainHost: "abc1.prg1.zerops.app",
		})
}

// notHTTPErrorMock builds a mock that fails EnableSubdomainAccess with the
// platform's serviceStackIsNotHttp apiCode — pins the F8 / worker case
// where the predicate fires enable but the platform rejects "not HTTP shape".
func notHTTPErrorMock(t *testing.T) *platform.Mock {
	t.Helper()
	mock := autoEnableTestMock(t, false)
	mock.WithError("EnableSubdomainAccess", &platform.PlatformError{
		Code:    platform.ErrAPIError,
		Message: "Service stack is not http or https",
		APICode: apiCodeServiceStackIsNotHTTP,
	})
	return mock
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

// --- Predicate tests (serviceEligibleForSubdomain) -------------------------

func TestServiceEligible_NoMeta_NonSystem_True(t *testing.T) {
	t.Parallel()
	mock := autoEnableTestMock(t, false)
	if !serviceEligibleForSubdomain(context.Background(), mock, nil, "proj-1", "app") {
		t.Error("no meta + USER-category service should be eligible (recipe-authoring / manual import path)")
	}
}

func TestServiceEligible_NoMeta_SystemStack_False(t *testing.T) {
	t.Parallel()
	for _, category := range []string{"BUILD", "CORE", "INTERNAL", "PREPARE_RUNTIME", "HTTP_L7_BALANCER"} {
		t.Run(category, func(t *testing.T) {
			t.Parallel()
			mock := systemStackMock(t, category)
			if serviceEligibleForSubdomain(context.Background(), mock, nil, "proj-1", "app") {
				t.Errorf("system-category %q should be ineligible (defensive guard)", category)
			}
		})
	}
}

func TestServiceEligible_MetaMode_AllowList_NonSystem_True(t *testing.T) {
	t.Parallel()
	for _, mode := range []topology.Mode{
		topology.PlanModeDev,
		topology.PlanModeStandard,
		topology.ModeStage,
		topology.PlanModeSimple,
		topology.PlanModeLocalStage,
	} {
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			meta := &workflow.ServiceMeta{Hostname: autoEnableTestHostname, Mode: mode}
			mock := autoEnableTestMock(t, false)
			if !serviceEligibleForSubdomain(context.Background(), mock, meta, "proj-1", "app") {
				t.Errorf("mode %q should be eligible", mode)
			}
		})
	}
}

func TestServiceEligible_LocalOnly_False(t *testing.T) {
	t.Parallel()
	meta := &workflow.ServiceMeta{Hostname: autoEnableTestHostname, Mode: topology.ModeLocalOnly}
	mock := autoEnableTestMock(t, false)
	if serviceEligibleForSubdomain(context.Background(), mock, meta, "proj-1", "app") {
		t.Error("local-only mode must be ineligible (no Zerops runtime to route to)")
	}
}

func TestServiceEligible_UnknownMode_False(t *testing.T) {
	t.Parallel()
	meta := &workflow.ServiceMeta{Hostname: autoEnableTestHostname, Mode: topology.Mode("production-future")}
	mock := autoEnableTestMock(t, false)
	if serviceEligibleForSubdomain(context.Background(), mock, meta, "proj-1", "app") {
		t.Error("unknown mode must default to ineligible (future-proof: explicit opt-in via modeAllowsSubdomain switch)")
	}
}

func TestServiceEligible_EmptyMode_NonSystem_True(t *testing.T) {
	t.Parallel()
	meta := &workflow.ServiceMeta{Hostname: autoEnableTestHostname} // Mode left empty
	mock := autoEnableTestMock(t, false)
	if !serviceEligibleForSubdomain(context.Background(), mock, meta, "proj-1", "app") {
		t.Error("empty Mode should be permissive (recipe-authoring scaffold path)")
	}
}

func TestServiceEligible_LookupFails_False(t *testing.T) {
	t.Parallel()
	mock := autoEnableTestMock(t, false).WithError("ListServices", &platform.PlatformError{
		Code:    platform.ErrAPIError,
		Message: "transient lookup failure",
	})
	if serviceEligibleForSubdomain(context.Background(), mock, nil, "proj-1", "app") {
		t.Error("LookupService failure should soft-fail to ineligible")
	}
}

// --- maybeAutoEnableSubdomain tests ----------------------------------------

func TestMaybeAutoEnableSubdomain_FirstDeploy_DevMode_Enables(t *testing.T) {
	// t.Parallel omitted — OverrideHTTPReadyConfigForTest mutates a
	// package-level config; parallel tests would clobber each other's
	// interval/timeout values even though the mutex keeps the race
	// detector green.
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	dir := t.TempDir()
	writeMeta(t, dir, topology.PlanModeDev)

	mock := autoEnableTestMock(t, false /* subdomain off — fresh enable */)
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

func TestMaybeAutoEnableSubdomain_AlreadyEnabled_SetsURL_NoAPICall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMeta(t, dir, topology.PlanModeStandard)

	mock := autoEnableTestMock(t, true /* subdomain already on */)
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	maybeAutoEnableSubdomain(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if !result.SubdomainAccessEnabled {
		t.Error("SubdomainAccessEnabled: want true on already-on, got false")
	}
	if result.SubdomainURL == "" {
		t.Error("SubdomainURL: want non-empty (URLs built from cached meta), got empty")
	}
	// Core invariant: ops.Subdomain.Enable check-before-mutate skips the
	// API call when subdomain is already active.
	if mock.CallCounts["EnableSubdomainAccess"] != 0 {
		t.Errorf("EnableSubdomainAccess calls: want 0 (already-on), got %d", mock.CallCounts["EnableSubdomainAccess"])
	}
}

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

// TestMaybeAutoEnable_ServiceStackIsNotHttp_BenignSkip pins the new
// caller-side classification: when ops.Subdomain.Enable returns the
// platform's serviceStackIsNotHttp apiCode, maybeAutoEnableSubdomain
// silently swallows it (no warning, SubdomainAccessEnabled stays false).
//
// Covers the F8 dev+dynamic+zsc-noop case AND the worker case AND any
// other non-HTTP-shaped stack the platform refuses to route. The previous
// predicate over-protected by skipping enable entirely; now the platform
// is the source of truth on "is this HTTP-shaped?" and the caller
// classifies the response.
//
// IMPORTANT: this benign swallow lives ONLY in maybeAutoEnableSubdomain.
// Explicit zerops_subdomain enable calls (TestSubdomain_* in subdomain_test.go)
// still receive serviceStackIsNotHttp as a real error so the user sees the
// "missing httpSupport: true on port" diagnostic.
func TestMaybeAutoEnable_ServiceStackIsNotHttp_BenignSkip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMeta(t, dir, topology.PlanModeDev)

	mock := notHTTPErrorMock(t)
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	maybeAutoEnableSubdomain(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if result.SubdomainAccessEnabled {
		t.Error("serviceStackIsNotHttp must NOT set SubdomainAccessEnabled")
	}
	if result.SubdomainURL != "" {
		t.Errorf("serviceStackIsNotHttp must NOT set SubdomainURL; got %q", result.SubdomainURL)
	}
	for _, w := range result.Warnings {
		if strings.Contains(w, "subdomain") {
			t.Errorf("serviceStackIsNotHttp must NOT surface a subdomain warning (benign signal); got %q", w)
		}
	}
	// Confirm the API call WAS issued — the §6.1 design choice is that
	// workers / non-HTTP stacks eat one wasted RT in exchange for "no
	// fallbacks" purity.
	if mock.CallCounts["EnableSubdomainAccess"] != 1 {
		t.Errorf("EnableSubdomainAccess calls: want 1 (caller attempts then classifies response), got %d",
			mock.CallCounts["EnableSubdomainAccess"])
	}
}

func TestMaybeAutoEnableSubdomain_OtherError_AddsWarning(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMeta(t, dir, topology.PlanModeDev)

	mock := autoEnableTestMock(t, false)
	mock.WithError("EnableSubdomainAccess", &platform.PlatformError{
		Code:    platform.ErrAPIError,
		Message: "transient platform failure",
		APICode: "someOtherApiCode",
	})
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	maybeAutoEnableSubdomain(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if result.SubdomainAccessEnabled {
		t.Error("must NOT set SubdomainAccessEnabled when enable failed")
	}
	if len(result.Warnings) == 0 {
		t.Fatal("want warnings populated on non-benign enable failure")
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

func TestMaybeAutoEnableSubdomain_LocalOnlyMode_NoEnableCall(t *testing.T) {
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
		t.Errorf("EnableSubdomainAccess calls: want 0 (local-only mode rejected at predicate), got %d",
			mock.CallCounts["EnableSubdomainAccess"])
	}
}

func TestMaybeAutoEnable_NoMeta_SystemService_NoEnableCall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // no meta

	mock := systemStackMock(t, "BUILD")
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}
	maybeAutoEnableSubdomain(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if result.SubdomainAccessEnabled {
		t.Error("system-category service should skip auto-enable")
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 0 {
		t.Errorf("EnableSubdomainAccess calls: want 0 (IsSystem defensive guard), got %d",
			mock.CallCounts["EnableSubdomainAccess"])
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
		{"Unknown", topology.Mode("production-future"), false}, // future-proof
		{"Empty", topology.Mode(""), true},                     // permissive: recipe-authoring path
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
// resolve the meta via FindServiceMeta (dev-pair-keyed) and auto-enable
// when meta.Mode is in the allow-list. meta.FirstDeployedAt is stamped on
// the dev half and useless as a first-deploy signal for stage; the new
// design doesn't read it — predicate fires Enable, platform classifies.
func TestMaybeAutoEnableSubdomain_StageCrossDeploy_EnablesForStage(t *testing.T) {
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	dir := t.TempDir()
	// Pair meta keyed by dev, StageHostname set.
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

	stageSvc := platform.ServiceStack{
		ID:        "svc-stage",
		Name:      "appstage",
		ProjectID: "proj-1",
		Ports:     []platform.Port{{Port: 3000, Protocol: "tcp"}},
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeCategoryName: "USER",
		},
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "svc-dev", Name: "appdev",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeCategoryName: "USER"},
				Ports:                []platform.Port{{Port: 3000, Protocol: "tcp"}}},
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
		t.Error("stage cross-deploy must trigger enable — pair-keyed meta lookup + platform classifies")
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 1 {
		t.Errorf("EnableSubdomainAccess calls: want 1 for stage cross-deploy, got %d", mock.CallCounts["EnableSubdomainAccess"])
	}
}

// --- isServiceStackIsNotHTTPErr tests --------------------------------------

func TestIsServiceStackIsNotHTTPErr_Match(t *testing.T) {
	t.Parallel()
	err := &platform.PlatformError{
		Code:    platform.ErrAPIError,
		Message: "Service stack is not http or https",
		APICode: apiCodeServiceStackIsNotHTTP,
	}
	if !isServiceStackIsNotHTTPErr(err) {
		t.Error("error with apiCode=serviceStackIsNotHttp should match")
	}
}

func TestIsServiceStackIsNotHTTPErr_Mismatch(t *testing.T) {
	t.Parallel()
	err := &platform.PlatformError{
		Code:    platform.ErrAPIError,
		Message: "Some other error",
		APICode: "someOtherCode",
	}
	if isServiceStackIsNotHTTPErr(err) {
		t.Error("error with different apiCode should not match")
	}
}

func TestIsServiceStackIsNotHTTPErr_NonPlatformError(t *testing.T) {
	t.Parallel()
	if isServiceStackIsNotHTTPErr(errors.New("plain error")) {
		t.Error("non-platform error should not match")
	}
	if isServiceStackIsNotHTTPErr(nil) {
		t.Error("nil error should not match")
	}
}

// --- helpers ---------------------------------------------------------------

type countingDoer struct {
	calls  int
	status int
}

func (d *countingDoer) Do(*http.Request) (*http.Response, error) {
	d.calls++
	return &http.Response{StatusCode: d.status, Body: io.NopCloser(strings.NewReader(""))}, nil
}
