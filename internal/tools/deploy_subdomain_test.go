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
	return autoEnableTestMockWithType(t, subdomainOn, "")
}

// autoEnableTestMockWithType is autoEnableTestMock with an explicit service
// type version (e.g. "nodejs@22", "nginx@1.22", "php-nginx@8.4"). Used by
// deferred-start probe-skip tests where the runtime class derived from
// the type version drives whether the HTTP probe fires.
func autoEnableTestMockWithType(t *testing.T, subdomainOn bool, typeVersion string) *platform.Mock {
	t.Helper()
	svc := platform.ServiceStack{
		ID:              autoEnableTestServiceID,
		Name:            autoEnableTestHostname,
		ProjectID:       "proj-1",
		SubdomainAccess: subdomainOn,
		Ports:           []platform.Port{{Port: 3000, Protocol: "tcp"}},
		ServiceStackTypeInfo: platform.ServiceTypeInfo{
			ServiceStackTypeCategoryName: "USER",
			ServiceStackTypeVersionName:  typeVersion,
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
	svc, ok := serviceEligibleForSubdomain(context.Background(), mock, nil, "proj-1", "app")
	if !ok {
		t.Error("no meta + USER-category service should be eligible (recipe-authoring / manual import path)")
	}
	if svc == nil {
		t.Error("eligible path must return the looked-up service for downstream classification")
	}
}

func TestServiceEligible_NoMeta_SystemStack_False(t *testing.T) {
	t.Parallel()
	for _, category := range []string{"BUILD", "CORE", "INTERNAL", "PREPARE_RUNTIME", "HTTP_L7_BALANCER"} {
		t.Run(category, func(t *testing.T) {
			t.Parallel()
			mock := systemStackMock(t, category)
			svc, ok := serviceEligibleForSubdomain(context.Background(), mock, nil, "proj-1", "app")
			if ok {
				t.Errorf("system-category %q should be ineligible (defensive guard)", category)
			}
			if svc != nil {
				t.Errorf("ineligible path must return nil service; got %+v", svc)
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
			if _, ok := serviceEligibleForSubdomain(context.Background(), mock, meta, "proj-1", "app"); !ok {
				t.Errorf("mode %q should be eligible", mode)
			}
		})
	}
}

func TestServiceEligible_LocalOnly_False(t *testing.T) {
	t.Parallel()
	meta := &workflow.ServiceMeta{Hostname: autoEnableTestHostname, Mode: topology.ModeLocalOnly}
	mock := autoEnableTestMock(t, false)
	if _, ok := serviceEligibleForSubdomain(context.Background(), mock, meta, "proj-1", "app"); ok {
		t.Error("local-only mode must be ineligible (no Zerops runtime to route to)")
	}
}

func TestServiceEligible_UnknownMode_False(t *testing.T) {
	t.Parallel()
	meta := &workflow.ServiceMeta{Hostname: autoEnableTestHostname, Mode: topology.Mode("production-future")}
	mock := autoEnableTestMock(t, false)
	if _, ok := serviceEligibleForSubdomain(context.Background(), mock, meta, "proj-1", "app"); ok {
		t.Error("unknown mode must default to ineligible (future-proof: explicit opt-in via modeAllowsSubdomain switch)")
	}
}

func TestServiceEligible_EmptyMode_NonSystem_True(t *testing.T) {
	t.Parallel()
	meta := &workflow.ServiceMeta{Hostname: autoEnableTestHostname} // Mode left empty
	mock := autoEnableTestMock(t, false)
	if _, ok := serviceEligibleForSubdomain(context.Background(), mock, meta, "proj-1", "app"); !ok {
		t.Error("empty Mode should be permissive (recipe-authoring scaffold path)")
	}
}

func TestServiceEligible_LookupFails_False(t *testing.T) {
	t.Parallel()
	mock := autoEnableTestMock(t, false).WithError("ListServices", &platform.PlatformError{
		Code:    platform.ErrAPIError,
		Message: "transient lookup failure",
	})
	if _, ok := serviceEligibleForSubdomain(context.Background(), mock, nil, "proj-1", "app"); ok {
		t.Error("LookupService failure should soft-fail to ineligible")
	}
}

// --- ensurePublicAccess tests (S7 — O3/E9) ---------------------------------

func TestEnsurePublicAccess_AutoFirstDeploy_EnablesAndStamps(t *testing.T) {
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	dir := t.TempDir()
	writeMeta(t, dir, topology.PlanModeDev)

	mock := autoEnableTestMock(t, false /* subdomain off — fresh enable */)
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if !result.SubdomainAccessEnabled {
		t.Error("SubdomainAccessEnabled: want true, got false")
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 1 {
		t.Errorf("EnableSubdomainAccess calls: want 1, got %d", mock.CallCounts["EnableSubdomainAccess"])
	}

	meta, err := workflow.FindServiceMeta(dir, "app")
	if err != nil {
		t.Fatalf("FindServiceMeta: %v", err)
	}
	rec := meta.PublicAccessFor("app")
	if rec.SubdomainEnabledByZcpAt == "" {
		t.Error("PublicAccessFor(app).SubdomainEnabledByZcpAt: want non-empty stamp after auto-enable")
	}
}

// writeMetaWithPublicAccess is writeMeta plus a pre-recorded public-access
// record for autoEnableTestHostname — used by PA-2/PA-3 tests that need a
// prior stamp or intent already on disk.
func writeMetaWithPublicAccess(t *testing.T, dir string, mode topology.Mode, rec topology.PublicAccessRecord) {
	t.Helper()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         autoEnableTestHostname,
		Mode:             mode,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-22",
		PublicAccess:     map[string]topology.PublicAccessRecord{autoEnableTestHostname: rec},
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}
}

// TestEnsurePublicAccess_Stamped_UserDisabled_NeverReenables pins PA-2: once
// zcp has auto-enabled a subdomain (stamp set), observing it off on a later
// call means the USER switched it off — zcp must never re-enable, and the
// persisted intent flips from auto to none so a future call stays silent.
func TestEnsurePublicAccess_Stamped_UserDisabled_NeverReenables(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMetaWithPublicAccess(t, dir, topology.PlanModeDev, topology.PublicAccessRecord{
		Intent:                  topology.PublicAccessAuto,
		SubdomainEnabledByZcpAt: "2026-04-22T00:00:00Z",
	})

	mock := autoEnableTestMock(t, false /* subdomain currently off — user switched it off */)
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if mock.CallCounts["EnableSubdomainAccess"] != 0 {
		t.Errorf("EnableSubdomainAccess calls: want 0 (PA-2 — never re-enable), got %d", mock.CallCounts["EnableSubdomainAccess"])
	}

	meta, err := workflow.FindServiceMeta(dir, "app")
	if err != nil {
		t.Fatalf("FindServiceMeta: %v", err)
	}
	if got := meta.PublicAccessFor("app").Intent; got != topology.PublicAccessNone {
		t.Errorf("Intent = %q, want %q (PA-2 reconcile)", got, topology.PublicAccessNone)
	}
}

// TestEnsurePublicAccess_CustomDomain_NoEnable_IntentDomain pins PA-3:
// domains routed to this service always win over auto-enable, and the
// persisted intent records "domain" so future calls skip the auto-enable
// predicate cheaply (no need to re-derive from the routing list each time
// — though O3 still reads it live everywhere else).
func TestEnsurePublicAccess_CustomDomain_NoEnable_IntentDomain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMeta(t, dir, topology.PlanModeDev)

	mock := autoEnableTestMock(t, false).WithPublicHTTPRoutings(platform.PublicHTTPRouting{
		ID:      "routing-1",
		Domains: []platform.PublicHTTPDomain{{Name: "example.com"}},
		Locations: []platform.PublicHTTPLocation{
			{Path: "/", Port: 3000, ServiceID: autoEnableTestServiceID},
		},
	})
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if mock.CallCounts["EnableSubdomainAccess"] != 0 {
		t.Errorf("EnableSubdomainAccess calls: want 0 (PA-3 — domains win), got %d", mock.CallCounts["EnableSubdomainAccess"])
	}

	meta, err := workflow.FindServiceMeta(dir, "app")
	if err != nil {
		t.Fatalf("FindServiceMeta: %v", err)
	}
	if got := meta.PublicAccessFor("app").Intent; got != topology.PublicAccessDomain {
		t.Errorf("Intent = %q, want %q (PA-3 reconcile)", got, topology.PublicAccessDomain)
	}
}

// TestEnsurePublicAccess_DeferredStartNoListener_SkipsWithoutStamp pins the
// deploy-hook half of the two-hook design: a dev-mode dynamic runtime has no
// listener until `zerops_dev_server action=start` runs, so the DEPLOYED-
// result hook must NOT auto-enable (no listener ⇒ ShouldAutoEnableSubdomain
// is false) — the second hook (dev-server start, S7 dev_server.go) is the
// one that fires once a listener actually exists.
func TestEnsurePublicAccess_DeferredStartNoListener_SkipsWithoutStamp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMeta(t, dir, topology.PlanModeDev)

	mock := autoEnableTestMockWithType(t, false, "nodejs@22")
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if mock.CallCounts["EnableSubdomainAccess"] != 0 {
		t.Errorf("EnableSubdomainAccess calls: want 0 (deferred-start — no listener yet), got %d", mock.CallCounts["EnableSubdomainAccess"])
	}
	if result.SubdomainAccessEnabled {
		t.Error("SubdomainAccessEnabled: want false, got true")
	}

	meta, err := workflow.FindServiceMeta(dir, "app")
	if err != nil {
		t.Fatalf("FindServiceMeta: %v", err)
	}
	if got := meta.PublicAccessFor("app").SubdomainEnabledByZcpAt; got != "" {
		t.Errorf("SubdomainEnabledByZcpAt = %q, want empty (no stamp without a listener)", got)
	}
}

// TestEnsurePublicAccess_NotHTTP_BenignSkip_NoStamp pins the caller-side
// serviceStackIsNotHttp classification for the new entry point: worker /
// non-HTTP stacks eat one wasted enable attempt (the platform is the source
// of truth on "is this HTTP-shaped?"), get no warning, and — new for
// E9 — no stamp, since the attempt didn't actually enable anything.
func TestEnsurePublicAccess_NotHTTP_BenignSkip_NoStamp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMeta(t, dir, topology.PlanModeDev)

	mock := notHTTPErrorMock(t)
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if result.SubdomainAccessEnabled {
		t.Error("serviceStackIsNotHttp must NOT set SubdomainAccessEnabled")
	}
	for _, w := range result.Warnings {
		if strings.Contains(w, "subdomain") {
			t.Errorf("serviceStackIsNotHttp must NOT surface a subdomain warning (benign signal); got %q", w)
		}
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 1 {
		t.Errorf("EnableSubdomainAccess calls: want 1 (caller attempts then classifies response), got %d",
			mock.CallCounts["EnableSubdomainAccess"])
	}

	meta, err := workflow.FindServiceMeta(dir, "app")
	if err != nil {
		t.Fatalf("FindServiceMeta: %v", err)
	}
	if got := meta.PublicAccessFor("app").SubdomainEnabledByZcpAt; got != "" {
		t.Errorf("SubdomainEnabledByZcpAt = %q, want empty (benign skip must not stamp)", got)
	}
}

// --- more ensurePublicAccess tests (carried over from maybeAutoEnableSubdomain,
// still valid under the O3/E9 design) ---------------------------------------

func TestEnsurePublicAccess_AlreadyEnabled_SetsURL_NoAPICall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMeta(t, dir, topology.PlanModeStandard)

	mock := autoEnableTestMock(t, true /* subdomain already on */)
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if !result.SubdomainAccessEnabled {
		t.Error("SubdomainAccessEnabled: want true on already-on, got false")
	}
	if result.SubdomainURL == "" {
		t.Error("SubdomainURL: want non-empty (URL resolved from observed state), got empty")
	}
	// Core invariant: ops.Subdomain.Enable check-before-mutate skips the
	// API call when subdomain is already active — and ensurePublicAccess
	// doesn't even attempt an enable once obs.Observed.Subdomain is on.
	if mock.CallCounts["EnableSubdomainAccess"] != 0 {
		t.Errorf("EnableSubdomainAccess calls: want 0 (already-on), got %d", mock.CallCounts["EnableSubdomainAccess"])
	}
}

func TestEnsurePublicAccess_AlreadyEnabled_SkipsHTTPProbe(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMeta(t, dir, topology.PlanModeDev)

	mock := autoEnableTestMock(t, true /* already on */)

	// HTTPDoer that counts calls so we can verify it was NOT invoked.
	doer := &countingDoer{status: http.StatusOK}
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}
	ensurePublicAccess(context.Background(), mock, doer, "proj-1", dir, "app", result)

	if doer.calls != 0 {
		t.Errorf("HTTP probe must be skipped on already_enabled; got %d calls", doer.calls)
	}
}

func TestEnsurePublicAccess_OtherError_AddsWarning(t *testing.T) {
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

	ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

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

func TestEnsurePublicAccess_LocalOnlyMode_NoEnableCall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeMeta(t, dir, topology.PlanModeLocalOnly)

	mock := autoEnableTestMock(t, false)
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if result.SubdomainAccessEnabled {
		t.Error("local-only mode must skip auto-enable (no Zerops runtime to route to)")
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 0 {
		t.Errorf("EnableSubdomainAccess calls: want 0 (local-only mode rejected at predicate), got %d",
			mock.CallCounts["EnableSubdomainAccess"])
	}
}

func TestEnsurePublicAccess_NoMeta_SystemService_NoEnableCall(t *testing.T) {
	t.Parallel()
	dir := t.TempDir() // no meta

	mock := systemStackMock(t, "BUILD")
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}
	ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

	if result.SubdomainAccessEnabled {
		t.Error("system-category service should skip auto-enable")
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 0 {
		t.Errorf("EnableSubdomainAccess calls: want 0 (IsSystem defensive guard), got %d",
			mock.CallCounts["EnableSubdomainAccess"])
	}
}

func TestEnsurePublicAccess_AllEligibleModes_TriggerEnable(t *testing.T) {
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
			ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "app", result)

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
func TestEnsurePublicAccess_StageCrossDeploy_EnablesForStage(t *testing.T) {
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
	ensurePublicAccess(context.Background(), mock, okHTTP, "proj-1", dir, "appstage", result)

	if !result.SubdomainAccessEnabled {
		t.Error("stage cross-deploy must trigger enable — pair-keyed meta lookup + platform classifies")
	}
	if mock.CallCounts["EnableSubdomainAccess"] != 1 {
		t.Errorf("EnableSubdomainAccess calls: want 1 for stage cross-deploy, got %d", mock.CallCounts["EnableSubdomainAccess"])
	}
}

// --- Deferred-start probe-skip tests ---------------------------------------
//
// Subtractive root fix for the post-deploy 502 friction (eval suite
// 20260503-211240, 4-of-9 scenarios flagged the noise): when the runtime
// is dev-mode dynamic with `run.start` omitted (the container boots idle,
// no app process), no app is expected to be HTTP-reachable until
// `zerops_dev_server action=start` fires. Probing in this state always
// returns 502 and emits a misleading warning. Skip the probe entirely;
// the agent's next move (dev_server start) tests HTTP readiness more
// reliably anyway.
//
// The predicate (topology.IsDeferredStart) gates only on (mode in
// {Dev, Standard}) AND (class == Dynamic). Stage / simple modes auto-start
// via run.start; static + implicit-webserver runtimes auto-serve regardless
// of mode — those keep the probe + warning path.
//
// The dev/standard-mode-dynamic cases themselves are covered by
// TestEnsurePublicAccess_DeferredStartNoListener_SkipsWithoutStamp above —
// under the O3/E9 design the deploy hook no longer even attempts an enable
// there (no listener yet), so there's nothing left to probe.

func TestEnsurePublicAccess_DevModeStatic_StillProbes(t *testing.T) {
	// t.Parallel omitted — OverrideHTTPReadyConfigForTest mutates package
	// state shared with WaitHTTPReady; sibling parallel tests would race.
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	dir := t.TempDir()
	writeMeta(t, dir, topology.PlanModeDev)

	mock := autoEnableTestMockWithType(t, false, "nginx@1.22")
	doer := &countingDoer{status: 200}
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	ensurePublicAccess(context.Background(), mock, doer, "proj-1", dir, "app", result)

	if doer.calls == 0 {
		t.Error("static runtime must still probe — nginx auto-serves; 502 in dev mode is a real problem there")
	}
}

func TestEnsurePublicAccess_DevModePHPNginx_StillProbes(t *testing.T) {
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	dir := t.TempDir()
	writeMeta(t, dir, topology.PlanModeDev)

	mock := autoEnableTestMockWithType(t, false, "php-nginx@8.4")
	doer := &countingDoer{status: 200}
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	ensurePublicAccess(context.Background(), mock, doer, "proj-1", dir, "app", result)

	if doer.calls == 0 {
		t.Error("implicit-webserver must still probe — php-nginx auto-starts; 502 means the deploy is broken")
	}
}

// TestEnsurePublicAccess_StageDynamic_StillProbes pins the stage-half rule:
// stage runtime runs run.start, so a 502 IS a real problem and the probe
// must run. The fixture mirrors production shape — pair-keyed standard
// meta (Hostname=dev half, StageHostname=stage half), targetService is
// the stage hostname (= autoEnableTestHostname here, to reuse the
// shared mock fixture). ServiceMeta.ModeFor projects target=StageHostname
// as ModeStage even though m.Mode is ModeStandard; IsDeferredStart
// returns false for stage; probe runs.
func TestEnsurePublicAccess_StageDynamic_StillProbes(t *testing.T) {
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	dir := t.TempDir()
	if err := workflow.WriteServiceMeta(dir, &workflow.ServiceMeta{
		Hostname:         "appdev",               // dev half (pair primary key)
		StageHostname:    autoEnableTestHostname, // stage half — the deploy target below
		Mode:             topology.PlanModeStandard,
		BootstrapSession: "sess1",
		BootstrappedAt:   "2026-04-22",
	}); err != nil {
		t.Fatalf("WriteServiceMeta: %v", err)
	}

	mock := autoEnableTestMockWithType(t, false, "nodejs@22")
	doer := &countingDoer{status: 200}
	result := &ops.DeployResult{TargetService: autoEnableTestHostname, TargetServiceID: "svc-1"}

	ensurePublicAccess(context.Background(), mock, doer, "proj-1", dir, autoEnableTestHostname, result)

	if doer.calls == 0 {
		t.Error("stage half of a standard pair must still probe — run.start runs the real app; 502 means the deploy is broken")
	}
}

func TestEnsurePublicAccess_NoMeta_DynamicType_StillProbes(t *testing.T) {
	// No meta = no mode info, so we can't classify deferred-start. Fail
	// closed: probe runs (recipe-authoring / manual-import path).
	restore := ops.OverrideHTTPReadyConfigForTest(1*time.Millisecond, 50*time.Millisecond)
	defer restore()

	dir := t.TempDir() // no meta written

	mock := autoEnableTestMockWithType(t, false, "nodejs@22")
	doer := &countingDoer{status: 200}
	result := &ops.DeployResult{TargetService: "app", TargetServiceID: "svc-1"}

	ensurePublicAccess(context.Background(), mock, doer, "proj-1", dir, "app", result)

	if doer.calls == 0 {
		t.Error("no-meta path must still probe (fail closed) — recipe-authoring / manual import has no mode info")
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
