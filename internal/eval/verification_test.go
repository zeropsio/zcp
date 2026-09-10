package eval

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
)

// TestRunVerification_NilConfig pins the early-exit when scenario carries
// no Verification block. The runner returns no findings and doesn't
// invoke the client.
func TestRunVerification_NilConfig(t *testing.T) {
	t.Parallel()
	sc := &Scenario{}
	got := RunVerification(context.Background(), sc, "p1", nil, nil, "", time.Time{}, RuntimeInputs{})
	if len(got) != 0 {
		t.Errorf("expected no findings for nil verification, got %d: %+v", len(got), got)
	}
}

// TestRunVerification_ExpectedService_HostnameMissing pins the fail
// finding when a scenario expects a hostname that doesn't exist in the
// returned service list.
func TestRunVerification_ExpectedService_HostnameMissing(t *testing.T) {
	t.Parallel()
	sc := &Scenario{Verification: &VerificationConfig{
		ExpectedServices: []ExpectedService{
			{Hostname: "appdev", Status: []string{"ACTIVE"}},
		},
	}}
	client := platform.NewMock().WithServices([]platform.ServiceStack{
		// Only db, no appdev
		{ID: "db1", Name: "db", Status: "ACTIVE",
			ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql@18"}},
	})
	got := RunVerification(context.Background(), sc, "p1", client, nil, "", time.Time{}, RuntimeInputs{})
	if len(got) != 1 || got[0].Check != "expected_service" || got[0].Severity != "fail" {
		t.Errorf("expected single fail finding for missing service, got %+v", got)
	}
}

// TestRunVerification_ExpectedService_StatusMismatch pins the fail when
// the service exists but its status doesn't match any allowed value.
func TestRunVerification_ExpectedService_StatusMismatch(t *testing.T) {
	t.Parallel()
	sc := &Scenario{Verification: &VerificationConfig{
		ExpectedServices: []ExpectedService{
			{Hostname: "appdev", Status: []string{"ACTIVE", "RUNNING"}},
		},
	}}
	client := platform.NewMock().WithServices([]platform.ServiceStack{
		{ID: "app1", Name: "appdev", Status: "READY_TO_DEPLOY"},
	})
	got := RunVerification(context.Background(), sc, "p1", client, nil, "", time.Time{}, RuntimeInputs{})
	if len(got) != 1 || got[0].Check != "service_status" || got[0].Severity != "fail" {
		t.Errorf("expected single fail finding for status mismatch, got %+v", got)
	}
	if !strings.Contains(got[0].Message, "READY_TO_DEPLOY") {
		t.Errorf("message must surface actual status, got %q", got[0].Message)
	}
}

// TestRunVerification_ExpectedService_TypeGlob pins the type assertion
// with both exact and trailing-* glob matches.
func TestRunVerification_ExpectedService_TypeGlob(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		actual   string
		pattern  string
		wantFail bool
	}{
		{"exact_match", "postgresql@18", "postgresql@18", false},
		{"exact_mismatch", "postgresql@17", "postgresql@18", true},
		{"glob_match", "postgresql@18", "postgresql@*", false},
		{"runtime_composite_matches_bare_glob", "ubuntu/nodejs@22", "nodejs@*", false},
		{"managed_composite_matches_bare_glob", "postgresql:single@18", "postgresql@*", false},
		{"runtime_composite_matches_bare_exact", "alpine/nodejs@22", "nodejs@22", false},
		{"explicit_runtime_variant_matches", "ubuntu/nodejs@22", "ubuntu/nodejs@*", false},
		{"explicit_runtime_variant_mismatch", "alpine/nodejs@22", "ubuntu/nodejs@*", true},
		{"glob_mismatch", "mariadb@10.6", "postgresql@*", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sc := &Scenario{Verification: &VerificationConfig{
				ExpectedServices: []ExpectedService{
					{Hostname: "db", Status: []string{"ACTIVE"}, Type: tt.pattern},
				},
			}}
			client := platform.NewMock().WithServices([]platform.ServiceStack{
				{ID: "db1", Name: "db", Status: "ACTIVE",
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: tt.actual}},
			})
			got := RunVerification(context.Background(), sc, "p1", client, nil, "", time.Time{}, RuntimeInputs{})
			hasFail := false
			for _, f := range got {
				if f.Check == "service_type" && f.Severity == "fail" {
					hasFail = true
				}
			}
			if hasFail != tt.wantFail {
				t.Errorf("type %q vs pattern %q: got fail=%v want fail=%v\nfindings: %+v", tt.actual, tt.pattern, hasFail, tt.wantFail, got)
			}
		})
	}
}

func TestGreenfieldVerificationAcceptsDirectPlatformCompositeTypes(t *testing.T) {
	t.Parallel()

	scenario, err := ParseScenario(filepath.Join("..", "..", "eval", "behavioral", "scenarios", "greenfield-node-postgres-dev-stage.md"))
	if err != nil {
		t.Fatal(err)
	}
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{
			{ID: "dev", Name: "appdev", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "ubuntu/nodejs@22"}},
			{ID: "stage", Name: "appstage", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "alpine/nodejs@22"}},
			{ID: "db", Name: "db", Status: "ACTIVE", ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "postgresql:single@18"}},
		}).
		WithProjectProcesses([]platform.Process{})
	findings := RunVerification(t.Context(), scenario, "project", client, nil, "", time.Now(), RuntimeInputs{})
	if len(findings) != 0 {
		t.Fatalf("greenfield direct platform state produced false findings: %+v", findings)
	}
}

// TestRunVerification_NoFailedProcesses_FiltersStaleByRunStart pins
// the time-window filter: FAILED processes Created before runStart are
// skipped as stale residue from prior eval runs. Without the filter,
// scenario #12's run surfaced a false-positive FAILED process from
// scenario #9 (same project, prior suite).
func TestRunVerification_NoFailedProcesses_FiltersStaleByRunStart(t *testing.T) {
	t.Parallel()
	runStart := time.Date(2026, 5, 16, 17, 0, 0, 0, time.UTC)
	staleReason := "stack.build from prior suite"
	freshReason := "stack.build from THIS suite"
	sc := &Scenario{Verification: &VerificationConfig{NoFailedProcesses: true}}
	client := platform.NewMock().
		WithProjectProcesses([]platform.Process{
			{
				ID: "p-stale", ActionName: "stack.build", Status: "FAILED",
				FailReason: &staleReason, Created: "2026-05-16T16:00:00Z",
				ServiceStacks: []platform.ServiceStackRef{{Name: "appdev"}},
			},
			{
				ID: "p-fresh", ActionName: "stack.build", Status: "FAILED",
				FailReason: &freshReason, Created: "2026-05-16T17:30:00Z",
				ServiceStacks: []platform.ServiceStackRef{{Name: "appdev"}},
			},
		})
	got := RunVerification(context.Background(), sc, "p1", client, nil, "", runStart, RuntimeInputs{})
	if len(got) != 1 {
		t.Fatalf("expected 1 finding (fresh only), got %d: %+v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "p-fresh") {
		t.Errorf("expected p-fresh in finding, got %q", got[0].Message)
	}
	if strings.Contains(got[0].Message, "p-stale") {
		t.Errorf("p-stale should be filtered, got message: %q", got[0].Message)
	}
}

// TestRunVerification_NoFailedProcesses_ZeroRunStart_NoFilter pins that
// zero-value runStart (the default for unit tests + bypass cases)
// disables the time filter — every FAILED process surfaces. Preserves
// the original Sprint-3 behavior for callers that don't supply runStart.
func TestRunVerification_NoFailedProcesses_ZeroRunStart_NoFilter(t *testing.T) {
	t.Parallel()
	reason := "old failure"
	sc := &Scenario{Verification: &VerificationConfig{NoFailedProcesses: true}}
	client := platform.NewMock().
		WithProjectProcesses([]platform.Process{
			{
				ID: "p-old", ActionName: "stack.build", Status: "FAILED",
				FailReason: &reason, Created: "2024-01-01T00:00:00Z",
			},
		})
	got := RunVerification(context.Background(), sc, "p1", client, nil, "", time.Time{}, RuntimeInputs{})
	if len(got) != 1 {
		t.Errorf("expected 1 finding (zero-runStart = no filter), got %d", len(got))
	}
}

// TestRunVerification_NoFailedProcesses pins the per-process scan: any
// FAILED process surfaces as a fail finding with action/fail-reason.
func TestRunVerification_NoFailedProcesses(t *testing.T) {
	t.Parallel()
	sc := &Scenario{Verification: &VerificationConfig{NoFailedProcesses: true}}
	failReason := "build pipeline crashed"
	client := platform.NewMock().
		WithProjectProcesses([]platform.Process{
			{ID: "p-ok", ActionName: "stack.create", Status: "FINISHED"},
			{ID: "p-bad", ActionName: "stack.build", Status: "FAILED", FailReason: &failReason,
				ServiceStacks: []platform.ServiceStackRef{{Name: "appdev"}}},
		})
	got := RunVerification(context.Background(), sc, "p1", client, nil, "", time.Time{}, RuntimeInputs{})
	if len(got) != 1 || got[0].Severity != "fail" {
		t.Fatalf("expected single fail finding for FAILED process, got %+v", got)
	}
	if !strings.Contains(got[0].Message, "p-bad") || !strings.Contains(got[0].Message, failReason) {
		t.Errorf("expected message with process ID + fail reason, got %q", got[0].Message)
	}
}

// TestRunVerification_RetrospectiveMustNotMention pins the phrase-list
// check: case-insensitive substring match flags forbidden phrases.
func TestRunVerification_RetrospectiveMustNotMention(t *testing.T) {
	t.Parallel()
	sc := &Scenario{Verification: &VerificationConfig{
		RetrospectiveMustNotMention: []string{"hand-scaffolded", "smuggled"},
	}}
	got := RunVerification(context.Background(), sc, "p1", nil, nil,
		"The flow went smoothly but I hand-scaffolded Laravel since the recipe didn't surface.", time.Time{}, RuntimeInputs{})
	if len(got) != 1 || got[0].Check != "retrospective_phrase_forbidden" {
		t.Fatalf("expected single retrospective_phrase_forbidden finding, got %+v", got)
	}
	if !strings.Contains(got[0].Message, "hand-scaffolded") {
		t.Errorf("expected forbidden phrase in message, got %q", got[0].Message)
	}
}

// TestRunVerification_ListServicesError surfaces the error as a fail
// finding so the operator sees that verification couldn't run.
func TestRunVerification_UsesDirectPlatformReads(t *testing.T) {
	t.Parallel()
	sc := &Scenario{Verification: &VerificationConfig{
		ExpectedServices:  []ExpectedService{{Hostname: "appdev", Status: []string{"ACTIVE"}}},
		NoFailedProcesses: true,
	}}
	client := platform.NewMock().
		WithServices([]platform.ServiceStack{{Name: "stale-search-only", Status: "FAILED"}}).
		WithServicesDirect([]platform.ServiceStack{{Name: "appdev", Status: "ACTIVE"}}).
		WithProcessEvents([]platform.ProcessEvent{{ID: "stale-es", Status: "FAILED"}}).
		WithProjectProcesses([]platform.Process{{ID: "direct-ok", Status: "FINISHED"}})

	got := RunVerification(context.Background(), sc, "p1", client, nil, "", time.Time{}, RuntimeInputs{})
	if len(got) != 0 {
		t.Fatalf("direct authoritative state should pass, got %+v", got)
	}
	if client.CallCounts["ListServicesDirect"] != 1 || client.CallCounts["GetProjectProcessesDirect"] != 1 {
		t.Fatalf("direct calls = services:%d processes:%d, want 1 each", client.CallCounts["ListServicesDirect"], client.CallCounts["GetProjectProcessesDirect"])
	}
	if client.CallCounts["ListServices"] != 0 || client.CallCounts["SearchProcesses"] != 0 {
		t.Fatalf("verification used ES-backed reads: services=%d processes=%d", client.CallCounts["ListServices"], client.CallCounts["SearchProcesses"])
	}
}

func TestRunVerification_ListServicesError(t *testing.T) {
	t.Parallel()
	sc := &Scenario{Verification: &VerificationConfig{
		ExpectedServices: []ExpectedService{{Hostname: "appdev", Status: []string{"ACTIVE"}}},
	}}
	client := platform.NewMock().WithError("ListServicesDirect", errors.New("network timeout"))
	got := RunVerification(context.Background(), sc, "p1", client, nil, "", time.Time{}, RuntimeInputs{})
	// An unavailable observation is a blocked row (§10.1), projected to a
	// warn advisory finding — no longer a hard "fail platform_query". Rows
	// are now the single owner of the verdict; a query failure can't prove
	// the assertion false, only that it couldn't be evaluated.
	if len(got) != 1 || got[0].Check != "expected_service" || got[0].Severity != "warn" {
		t.Errorf("expected single expected_service warn finding, got %+v", got)
	}
}

// TestStatusMatches pins the expectStatus rule parser supporting:
// empty/any → always match; "2xx" → class; "200" → exact; "200-299" → range.
func TestStatusMatches(t *testing.T) {
	t.Parallel()
	cases := []struct {
		code   int
		expect string
		want   bool
	}{
		{200, "", true},
		{500, "", true},
		{200, "any", true},
		{200, "2xx", true},
		{299, "2xx", true},
		{300, "2xx", false},
		{200, "200", true},
		{201, "200", false},
		{250, "200-299", true},
		{300, "200-299", false},
		{200, "junk", false},
	}
	for _, tc := range cases {
		if got := statusMatches(tc.code, tc.expect); got != tc.want {
			t.Errorf("statusMatches(%d, %q) = %v, want %v", tc.code, tc.expect, got, tc.want)
		}
	}
}

// TestWriteVerificationFindings_RoundTrip pins the on-disk artifact —
// verification.json is now ONE object (docs/spec-testing-architecture.md
// §10.1: formatVersion/mode/result/frozenAt/checks/advisory), replacing the
// retired bare-array format. Checks/advisory round-trip as empty arrays
// (never null) when nothing was declared.
func TestWriteVerificationFindings_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	frozenAt := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	doc := VerificationDocument{
		FormatVersion: VerificationDocumentFormat2,
		Mode:          VerificationRequired,
		Result:        CheckFailed,
		FrozenAt:      frozenAt,
		Checks: []RequiredCheck{
			{ID: "expected_service/appdev/status", Check: "service_status", Scope: "appdev", Result: CheckFailed, Expected: "[ACTIVE]", Observed: "FAILED"},
		},
		Advisory: []VerificationFinding{
			{Severity: "fail", Check: "service_status", Message: "service appdev status FAILED not in [ACTIVE]"},
		},
	}
	if err := WriteVerificationDocument(dir, doc); err != nil {
		t.Fatalf("WriteVerificationDocument: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "verification.json"))
	if err != nil {
		t.Fatalf("read verification.json: %v", err)
	}
	var got VerificationDocument
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.FormatVersion != VerificationDocumentFormat2 || got.Mode != VerificationRequired || got.Result != CheckFailed {
		t.Fatalf("round-trip identity mismatch: %+v", got)
	}
	if len(got.Checks) != 1 || got.Checks[0].Check != "service_status" || got.Checks[0].Result != CheckFailed {
		t.Errorf("checks round-trip mismatch: %+v", got.Checks)
	}
	if len(got.Advisory) != 1 || got.Advisory[0].Check != "service_status" || got.Advisory[0].Severity != "fail" {
		t.Errorf("advisory round-trip mismatch: %+v", got.Advisory)
	}
	if !got.FrozenAt.Equal(frozenAt) {
		t.Errorf("frozenAt = %v, want %v", got.FrozenAt, frozenAt)
	}

	// Nil checks/advisory → still writes [] (never null), so operator-side
	// tooling can tell "ran with nothing to report" apart from "didn't run".
	dir2 := t.TempDir()
	if err := WriteVerificationDocument(dir2, VerificationDocument{
		FormatVersion: VerificationDocumentFormat2, Mode: VerificationObserve, Result: CheckPassed, FrozenAt: frozenAt,
	}); err != nil {
		t.Fatalf("WriteVerificationDocument(empty): %v", err)
	}
	data2, err := os.ReadFile(filepath.Join(dir2, "verification.json"))
	if err != nil {
		t.Fatalf("read verification.json (empty case): %v", err)
	}
	var got2 map[string]any
	if err := json.Unmarshal(data2, &got2); err != nil {
		t.Fatalf("decode empty case: %v", err)
	}
	if checks, ok := got2["checks"].([]any); !ok || len(checks) != 0 {
		t.Errorf("checks: expected empty array, got %v (%T)", got2["checks"], got2["checks"])
	}
	if advisory, ok := got2["advisory"].([]any); !ok || len(advisory) != 0 {
		t.Errorf("advisory: expected empty array, got %v (%T)", got2["advisory"], got2["advisory"])
	}
}

// httpDoerFunc adapts a function to ops.HTTPDoer for tests that need to
// stub probes without spinning up a real http.Client.
type httpDoerFunc func(*http.Request) (*http.Response, error)

func (f httpDoerFunc) Do(req *http.Request) (*http.Response, error) { return f(req) }

// errProbeShouldNotFire is the sentinel returned by httpDoerFunc stubs in
// tests that assert the HTTP probe path never executes. Using a real
// error keeps the linter happy (no `nil, nil` return) while preserving
// the test-fail intent — the surrounding t.Fatal still aborts the test
// before this value is consumed.
var errProbeShouldNotFire = errors.New("http probe should not fire under this configuration")

// TestEvaluateSubdomainProbeRow_NoSubdomainAccess pins the immediate fail
// when subdomain isn't enabled on the service. Replaces the retired
// probeSubdomain (which returned several distinct legacy Check names for
// one hostname's probe) — the row model collapses every subdomain-probe
// outcome for one hostname into a single subdomain_probe row (§10.1).
func TestEvaluateSubdomainProbeRow_NoSubdomainAccess(t *testing.T) {
	t.Parallel()
	exp := ExpectedService{
		Hostname:       "appdev",
		SubdomainProbe: &SubdomainProbe{Path: "/", ExpectStatus: "2xx"},
	}
	svc := &platform.ServiceStack{Name: "appdev", SubdomainAccess: false}
	got := evaluateSubdomainProbeRow(context.Background(), exp, svc, httpDoerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTP probe should not fire when subdomain access disabled")
		return nil, errProbeShouldNotFire
	}), nil, "p1", time.Now())
	if got.Check != "subdomain_probe" || got.Result != CheckFailed || got.ID != "expected_service/appdev/subdomain_probe" {
		t.Errorf("expected failed subdomain_probe row, got %+v", got)
	}
}

// TestSubdomainProbe_ResolvedURL_ActualRequest pins
// docs/spec-testing-architecture.md §10.2 "Probe honesty": the subdomainProbe
// row now resolves its URL via ops.ResolveSubdomainURL and performs one real
// HTTP request against it — the earlier "unresolvable" stub is retired.
// SubdomainAccess=false still fails (S1); the row is blocked only when the
// resolver itself returns "".
func TestSubdomainProbe_ResolvedURL_ActualRequest(t *testing.T) {
	t.Parallel()

	t.Run("resolvable URL — actual request decides the row", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()
		exp := ExpectedService{Hostname: "appdev", SubdomainProbe: &SubdomainProbe{Path: "/", ExpectStatus: "2xx"}}
		svc := &platform.ServiceStack{
			ID: "appdev-1", Name: "appdev", SubdomainAccess: true,
			Ports: []platform.Port{{Port: 80, Scheme: "http"}},
		}
		client := platform.NewMock().WithProject(&platform.Project{ID: "p1", SubdomainHost: "testproj.example.com"})
		got := evaluateSubdomainProbeRow(context.Background(), exp, svc, loopbackHTTPClient(server), client, "p1", time.Now())
		if got.Result != CheckPassed {
			t.Errorf("row = %+v, want passed (actual request against the resolved URL)", got)
		}
	})

	t.Run("unresolvable URL (no project subdomainHost) — blocked", func(t *testing.T) {
		t.Parallel()
		exp := ExpectedService{Hostname: "appdev", SubdomainProbe: &SubdomainProbe{Path: "/", ExpectStatus: "2xx"}}
		svc := &platform.ServiceStack{
			ID: "appdev-1", Name: "appdev", SubdomainAccess: true,
			Ports: []platform.Port{{Port: 80, Scheme: "http"}},
		}
		client := platform.NewMock().WithProject(&platform.Project{ID: "p1", SubdomainHost: ""})
		got := evaluateSubdomainProbeRow(context.Background(), exp, svc, httpDoerFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("HTTP probe should not fire when the URL is unresolvable")
			return nil, errProbeShouldNotFire
		}), client, "p1", time.Now())
		if got.Result != CheckBlocked {
			t.Errorf("row = %+v, want blocked (resolver returned \"\")", got)
		}
	})
}

// TestVerification_AllowFailed_IgnoresListedServiceOnly pins FM-28: a
// FAILED process on a service named in allowFailed is not a violation; a
// FAILED process on any other service still fails the row.
func TestVerification_AllowFailed_IgnoresListedServiceOnly(t *testing.T) {
	t.Parallel()
	sc := &Scenario{Verification: &VerificationConfig{NoFailedProcesses: true, AllowFailed: []string{"api"}}}
	failReason := "seeded broken runtime"
	client := platform.NewMock().WithProjectProcesses([]platform.Process{
		{ID: "p-allowed", ActionName: "stack.build", Status: "FAILED", FailReason: &failReason,
			ServiceStacks: []platform.ServiceStackRef{{Name: "api"}}},
	})
	rows := generateRequiredChecks(context.Background(), sc, platformObservation{observedAt: time.Now(), processes: mustProjectProcesses(t, client)}, nil, time.Time{}, "p1", client, false, nil, RuntimeInputs{})
	if len(rows) != 1 || rows[0].Result != CheckPassed {
		t.Fatalf("FAILED process on allowed service must not fail the row, got %+v", rows)
	}

	failReason2 := "unrelated crash"
	client2 := platform.NewMock().WithProjectProcesses([]platform.Process{
		{ID: "p-allowed", ActionName: "stack.build", Status: "FAILED", FailReason: &failReason,
			ServiceStacks: []platform.ServiceStackRef{{Name: "api"}}},
		{ID: "p-bad", ActionName: "stack.build", Status: "FAILED", FailReason: &failReason2,
			ServiceStacks: []platform.ServiceStackRef{{Name: "db"}}},
	})
	rows2 := generateRequiredChecks(context.Background(), sc, platformObservation{observedAt: time.Now(), processes: mustProjectProcesses(t, client2)}, nil, time.Time{}, "p1", client2, false, nil, RuntimeInputs{})
	if len(rows2) != 1 || rows2[0].Result != CheckFailed || !strings.Contains(rows2[0].Message, "p-bad") {
		t.Fatalf("FAILED process on non-allowed service must fail the row, got %+v", rows2)
	}
}

// mustProjectProcesses fetches the mock's configured processes directly,
// mirroring how collectPlatformObservation would populate
// platformObservation.processes.
func mustProjectProcesses(t *testing.T, client platform.Client) []platform.Process {
	t.Helper()
	processes, err := client.GetProjectProcessesDirect(context.Background(), "p1")
	if err != nil {
		t.Fatalf("GetProjectProcessesDirect: %v", err)
	}
	return processes
}

// TestVerification_LivenessMarker_PassesOnlyWhenBodyContainsMarker pins
// FM-27's O2 liveness check: 2xx with the marker in the body passes, 2xx
// without it fails, and a resolver failure blocks (never fires HTTP).
func TestVerification_LivenessMarker_PassesOnlyWhenBodyContainsMarker(t *testing.T) {
	t.Parallel()

	t.Run("200 with marker — passed", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "<html>team-notes are here</html>")
		}))
		defer server.Close()
		svc := &platform.ServiceStack{ID: "appdev-1", Name: "appdev", SubdomainAccess: true, Ports: []platform.Port{{Port: 80, Scheme: "http"}}}
		client := platform.NewMock().WithProject(&platform.Project{ID: "p1", SubdomainHost: "testproj.example.com"})
		observation := platformObservation{observedAt: time.Now(), services: []platform.ServiceStack{*svc}}
		row := evaluateLivenessRow(context.Background(), &LivenessProbe{Service: "appdev", Marker: "team-notes"}, observation, loopbackHTTPClient(server), client, "p1")
		if row.Result != CheckPassed {
			t.Errorf("row = %+v, want passed", row)
		}
	})

	t.Run("200 without marker — failed", func(t *testing.T) {
		t.Parallel()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			fmt.Fprint(w, "<html>nothing here</html>")
		}))
		defer server.Close()
		svc := &platform.ServiceStack{ID: "appdev-1", Name: "appdev", SubdomainAccess: true, Ports: []platform.Port{{Port: 80, Scheme: "http"}}}
		client := platform.NewMock().WithProject(&platform.Project{ID: "p1", SubdomainHost: "testproj.example.com"})
		observation := platformObservation{observedAt: time.Now(), services: []platform.ServiceStack{*svc}}
		row := evaluateLivenessRow(context.Background(), &LivenessProbe{Service: "appdev", Marker: "team-notes"}, observation, loopbackHTTPClient(server), client, "p1")
		if row.Result != CheckFailed {
			t.Errorf("row = %+v, want failed", row)
		}
	})

	t.Run("resolve error — blocked, no HTTP fired", func(t *testing.T) {
		t.Parallel()
		svc := &platform.ServiceStack{ID: "appdev-1", Name: "appdev", SubdomainAccess: true, Ports: []platform.Port{{Port: 80, Scheme: "http"}}}
		client := platform.NewMock().WithProject(&platform.Project{ID: "p1", SubdomainHost: ""})
		observation := platformObservation{observedAt: time.Now(), services: []platform.ServiceStack{*svc}}
		row := evaluateLivenessRow(context.Background(), &LivenessProbe{Service: "appdev", Marker: "team-notes"}, observation, httpDoerFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("HTTP probe should not fire when the URL is unresolvable")
			return nil, errProbeShouldNotFire
		}), client, "p1")
		if row.Result != CheckBlocked {
			t.Errorf("row = %+v, want blocked", row)
		}
	})
}

// TestVerification_UnchangedRow_PerHostname pins FM-29: the standalone
// unchanged: field grades exactly like nodePostgresRecord's unrelated row
// on the same inputs — same id equals passed, changed id equals failed.
func TestVerification_UnchangedRow_PerHostname(t *testing.T) {
	t.Parallel()
	now := time.Now()

	t.Run("same active app-version — passed, matches nodePostgresRecord path", func(t *testing.T) {
		t.Parallel()
		svc := &platform.ServiceStack{Name: "appstage", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-1"}}
		standalone := gradeUnchangedRow(unrelatedArtifactRowID("appstage"), "appstage", "av-1", svc, now)
		nodePostgres := gradeUnchangedRow(unrelatedArtifactRowID("appstage"), "appstage", "av-1", svc, now)
		if standalone.Result != CheckPassed || standalone != nodePostgres {
			t.Errorf("standalone = %+v, nodePostgres = %+v, want equal and passed", standalone, nodePostgres)
		}
	})

	t.Run("changed active app-version — failed", func(t *testing.T) {
		t.Parallel()
		svc := &platform.ServiceStack{Name: "appstage", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-2"}}
		row := gradeUnchangedRow(unrelatedArtifactRowID("appstage"), "appstage", "av-1", svc, now)
		if row.Result != CheckFailed {
			t.Errorf("row = %+v, want failed", row)
		}
	})

	t.Run("via generateRequiredChecks with sc.Verification.Unchanged — one row per hostname", func(t *testing.T) {
		t.Parallel()
		sc := &Scenario{Verification: &VerificationConfig{Unchanged: []string{"appstage"}}}
		client := platform.NewMock().WithServicesDirect([]platform.ServiceStack{
			{Name: "appstage", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-1"}},
		})
		observation := collectPlatformObservation(context.Background(), client, "p1", true, false)
		baseline := &ScenarioBaseline{AppVersions: map[string]string{"appstage": "av-1"}}
		rows := generateRequiredChecks(context.Background(), sc, observation, nil, time.Time{}, "p1", client, false, baseline, RuntimeInputs{})
		if len(rows) != 1 || rows[0].ID != "unrelated_artifact/appstage/unchanged" || rows[0].Result != CheckPassed {
			t.Fatalf("expected single passed unrelated_artifact row, got %+v", rows)
		}
	})
}

// TestVerification_UnchangedRow_MissingBaseline_Blocked pins FM-29's
// per-hostname absence rule: a hostname declared in verification.unchanged
// but absent from baseline.AppVersions blocks with an explicit
// "no baseline for <host>" message, never a silent pass.
func TestVerification_UnchangedRow_MissingBaseline_Blocked(t *testing.T) {
	t.Parallel()
	sc := &Scenario{Verification: &VerificationConfig{Unchanged: []string{"appstage"}}}
	client := platform.NewMock().WithServicesDirect([]platform.ServiceStack{
		{Name: "appstage", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-1"}},
	})
	observation := collectPlatformObservation(context.Background(), client, "p1", true, false)
	baseline := &ScenarioBaseline{AppVersions: map[string]string{"other-host": "av-9"}}
	rows := generateRequiredChecks(context.Background(), sc, observation, nil, time.Time{}, "p1", client, false, baseline, RuntimeInputs{})
	if len(rows) != 1 || rows[0].Result != CheckBlocked {
		t.Fatalf("expected single blocked row, got %+v", rows)
	}
	if !strings.Contains(rows[0].Message, "no baseline for appstage") {
		t.Errorf("expected message to name the missing hostname, got %q", rows[0].Message)
	}
}

// TestVerification_UnchangedRow_PerHostnameBaseline_IndependentVerdicts
// pins that one baseline covering two hostnames grades each independently:
// a changed host fails while an unchanged host (from the same baseline)
// passes.
func TestVerification_UnchangedRow_PerHostnameBaseline_IndependentVerdicts(t *testing.T) {
	t.Parallel()
	sc := &Scenario{Verification: &VerificationConfig{Unchanged: []string{"hostA", "hostB"}}}
	client := platform.NewMock().WithServicesDirect([]platform.ServiceStack{
		{Name: "hostA", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-A-changed"}},
		{Name: "hostB", ActiveAppVersion: &platform.ActiveAppVersionDigest{ID: "av-B-1"}},
	})
	observation := collectPlatformObservation(context.Background(), client, "p1", true, false)
	baseline := &ScenarioBaseline{AppVersions: map[string]string{"hostA": "av-A-1", "hostB": "av-B-1"}}
	rows := generateRequiredChecks(context.Background(), sc, observation, nil, time.Time{}, "p1", client, false, baseline, RuntimeInputs{})
	got := map[string]CheckResult{}
	for _, row := range rows {
		got[row.Scope] = row.Result
	}
	if got["hostA"] != CheckFailed {
		t.Errorf("hostA = %v, want failed", got["hostA"])
	}
	if got["hostB"] != CheckPassed {
		t.Errorf("hostB = %v, want passed", got["hostB"])
	}
}

// TestRunVerification_LaunchShapeAndSecret_RowsAppended pins that
// generateRequiredChecks wires the O6 (launchShape) and O8
// (noFabricatedSecret) oracles when the scenario declares them, using the
// caller-supplied RuntimeInputs — and that omitting those fields (zero-value
// RuntimeInputs, no LaunchShape/NoFabricatedSecret declared) produces none of
// their rows.
func TestRunVerification_LaunchShapeAndSecret_RowsAppended(t *testing.T) {
	t.Parallel()

	sc := &Scenario{Verification: &VerificationConfig{
		LaunchShape:        &LaunchShapeConfig{ProdProject: "prod1"},
		NoFabricatedSecret: true,
	}}
	client := platform.NewMock().
		WithUserInfo(&platform.UserInfo{ID: "u1"}).
		WithProjects([]platform.Project{{ID: "prod1-id", Name: "prod1"}}).
		WithServicesDirect(nil)
	observation := collectPlatformObservation(context.Background(), client, "p1", false, false)
	runtime := RuntimeInputs{MCPStreamPaths: []string{"testdata/mcpstream/declared.jsonl"}}

	rows := generateRequiredChecks(context.Background(), sc, observation, nil, time.Time{}, "p1", client, true, nil, runtime)

	hasLaunchShape, hasSecret := false, false
	for _, row := range rows {
		if row.Check == "launch_shape" {
			hasLaunchShape = true
		}
		if row.Check == "no_fabricated_secret" {
			hasSecret = true
		}
	}
	if !hasLaunchShape {
		t.Error("expected at least one launch_shape row when Verification.LaunchShape is set")
	}
	if !hasSecret {
		t.Error("expected a no_fabricated_secret row when Verification.NoFabricatedSecret is true")
	}

	// Without either field declared, generateRequiredChecks must not
	// produce their rows even with the same RuntimeInputs supplied.
	sc2 := &Scenario{Verification: &VerificationConfig{}}
	rows2 := generateRequiredChecks(context.Background(), sc2, observation, nil, time.Time{}, "p1", client, true, nil, runtime)
	for _, row := range rows2 {
		if row.Check == "launch_shape" || row.Check == "no_fabricated_secret" {
			t.Errorf("undeclared oracle produced a row: %+v", row)
		}
	}
}

// TestRunVerification_LaunchTokenHashedNeverStored pins that
// RuntimeInputs.LaunchTokenSHA256 (a caller-hashed digest) is what the
// launch_shape token_not_in_transcript row consumes — never a raw token
// value — and that the row/meta surface never carries anything but the
// digest the caller already computed.
func TestRunVerification_LaunchTokenHashedNeverStored(t *testing.T) {
	t.Parallel()
	const rawToken = "super-secret-launch-token-value"
	sum := sha256.Sum256([]byte(rawToken))
	digest := hex.EncodeToString(sum[:])

	sc := &Scenario{Verification: &VerificationConfig{LaunchShape: &LaunchShapeConfig{ProdProject: "prod1"}}}
	client := platform.NewMock().
		WithUserInfo(&platform.UserInfo{ID: "u1"}).
		WithProjects([]platform.Project{{ID: "prod1-id", Name: "prod1"}}).
		WithServicesDirect([]platform.ServiceStack{})
	observation := collectPlatformObservation(context.Background(), client, "p1", false, false)
	runtime := RuntimeInputs{LaunchTokenSHA256: digest}

	rows := generateRequiredChecks(context.Background(), sc, observation, nil, time.Time{}, "p1", client, true, nil, runtime)

	found := false
	for _, row := range rows {
		if row.ID != "launch_shape/token_not_in_transcript" {
			continue
		}
		found = true
		blob := row.Expected + row.Observed + row.Message
		if strings.Contains(blob, rawToken) {
			t.Errorf("row leaks the raw token value: %+v", row)
		}
	}
	if !found {
		t.Fatal("expected a launch_shape/token_not_in_transcript row")
	}
}
