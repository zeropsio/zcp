package eval

import (
	"context"
	"encoding/json"
	"errors"
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
	got := RunVerification(context.Background(), sc, "p1", nil, nil, "", time.Time{})
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
	got := RunVerification(context.Background(), sc, "p1", client, nil, "", time.Time{})
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
	got := RunVerification(context.Background(), sc, "p1", client, nil, "", time.Time{})
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
			got := RunVerification(context.Background(), sc, "p1", client, nil, "", time.Time{})
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
	findings := RunVerification(t.Context(), scenario, "project", client, nil, "", time.Now())
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
	got := RunVerification(context.Background(), sc, "p1", client, nil, "", runStart)
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
	got := RunVerification(context.Background(), sc, "p1", client, nil, "", time.Time{})
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
	got := RunVerification(context.Background(), sc, "p1", client, nil, "", time.Time{})
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
		"The flow went smoothly but I hand-scaffolded Laravel since the recipe didn't surface.", time.Time{})
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

	got := RunVerification(context.Background(), sc, "p1", client, nil, "", time.Time{})
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
	got := RunVerification(context.Background(), sc, "p1", client, nil, "", time.Time{})
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
