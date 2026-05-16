package eval

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
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
		WithServices(nil).
		WithProcessEvents([]platform.ProcessEvent{
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
		WithServices(nil).
		WithProcessEvents([]platform.ProcessEvent{
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
		WithServices(nil).
		WithProcessEvents([]platform.ProcessEvent{
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
func TestRunVerification_ListServicesError(t *testing.T) {
	t.Parallel()
	sc := &Scenario{Verification: &VerificationConfig{
		ExpectedServices: []ExpectedService{{Hostname: "appdev", Status: []string{"ACTIVE"}}},
	}}
	client := platform.NewMock().WithError("ListServices", errors.New("network timeout"))
	got := RunVerification(context.Background(), sc, "p1", client, nil, "", time.Time{})
	if len(got) != 1 || got[0].Check != "platform_query" || got[0].Severity != "fail" {
		t.Errorf("expected single platform_query fail finding, got %+v", got)
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
// verification.json shape, including the "empty findings still writes
// empty array" convention so operator-side tooling can tell
// "verification ran with no failures" apart from "didn't run".
func TestWriteVerificationFindings_RoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	findings := []VerificationFinding{
		{Severity: "fail", Check: "service_status", Message: "service appdev status FAILED not in [ACTIVE]"},
	}
	if err := WriteVerificationFindings(dir, findings); err != nil {
		t.Fatalf("WriteVerificationFindings: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "verification.json"))
	if err != nil {
		t.Fatalf("read verification.json: %v", err)
	}
	var got []VerificationFinding
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Check != "service_status" || got[0].Severity != "fail" {
		t.Errorf("round-trip mismatch: %+v", got)
	}

	// Empty findings → still writes [].
	dir2 := t.TempDir()
	if err := WriteVerificationFindings(dir2, nil); err != nil {
		t.Fatalf("WriteVerificationFindings(nil): %v", err)
	}
	data2, err := os.ReadFile(filepath.Join(dir2, "verification.json"))
	if err != nil {
		t.Fatalf("read verification.json (nil case): %v", err)
	}
	if strings.TrimSpace(string(data2)) != "[]" {
		t.Errorf("nil findings: expected [], got %q", data2)
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

// TestProbeSubdomain_NoSubdomainAccess pins the immediate fail when
// subdomain isn't enabled on the service.
func TestProbeSubdomain_NoSubdomainAccess(t *testing.T) {
	t.Parallel()
	exp := ExpectedService{
		Hostname:       "appdev",
		SubdomainProbe: &SubdomainProbe{Path: "/", ExpectStatus: "2xx"},
	}
	svc := &platform.ServiceStack{Name: "appdev", SubdomainAccess: false}
	got := probeSubdomain(context.Background(), exp, svc, httpDoerFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTP probe should not fire when subdomain access disabled")
		return nil, errProbeShouldNotFire
	}))
	if len(got) != 1 || got[0].Check != "subdomain_access" || got[0].Severity != "fail" {
		t.Errorf("expected single subdomain_access fail finding, got %+v", got)
	}
}
