package eval

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/zeropsio/zcp/internal/platform"
)

// fakeDoer is a minimal http doer for probe tests.
type fakeDoer struct {
	status int
	body   string
	err    error
	gotURL string
}

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.gotURL = req.URL.String()
	if f.err != nil {
		return nil, f.err
	}
	return &http.Response{
		StatusCode: f.status,
		Body:       io.NopCloser(strings.NewReader(f.body)),
		Header:     http.Header{},
	}, nil
}

func TestProbeFinalURL_SubdomainReachable_ReturnsStatus(t *testing.T) {
	t.Parallel()

	svc := platform.ServiceStack{
		ID:              "svc-1",
		Name:            "app",
		SubdomainAccess: true,
		Ports:           []platform.Port{{Port: 80, HTTPSupport: true}},
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{svc}).
		WithProject(&platform.Project{SubdomainHost: "app.prg1.zerops.app"})

	doer := &fakeDoer{status: 200, body: "ok"}

	probe := ProbeFinalURL(context.Background(), mock, doer, "proj-1", "app")
	if probe.Got != 200 {
		t.Errorf("Got: got %d, want 200", probe.Got)
	}
	if probe.Err != "" {
		t.Errorf("Err: got %q, want empty", probe.Err)
	}
	if probe.URL == "" {
		t.Error("URL: empty (expected resolved subdomain URL)")
	}
}

func TestProbeFinalURL_SubdomainNotEnabled_ReturnsErr(t *testing.T) {
	t.Parallel()

	svc := platform.ServiceStack{
		ID:              "svc-1",
		Name:            "app",
		SubdomainAccess: false,
		Ports:           []platform.Port{{Port: 80, HTTPSupport: true}},
	}
	mock := platform.NewMock().WithServices([]platform.ServiceStack{svc})

	doer := &fakeDoer{status: 200}

	probe := ProbeFinalURL(context.Background(), mock, doer, "proj-1", "app")
	if probe.Err == "" {
		t.Fatal("expected error when subdomain not enabled")
	}
	if probe.Got != 0 {
		t.Errorf("Got should be 0 on error, got %d", probe.Got)
	}
	if doer.gotURL != "" {
		t.Errorf("httpDoer should not be called when subdomain unresolved, got URL %q", doer.gotURL)
	}
}

func TestProbeFinalURL_ServiceNotFound_ReturnsErr(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{})
	doer := &fakeDoer{status: 200}

	probe := ProbeFinalURL(context.Background(), mock, doer, "proj-1", "ghost")
	if probe.Err == "" {
		t.Fatal("expected error when service not found")
	}
	if !strings.Contains(probe.Err, "ghost") {
		t.Errorf("error should mention missing hostname, got %q", probe.Err)
	}
}

func TestResolveProbeHostname_SingleWebFacing_PicksIt(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{Name: "web", SubdomainAccess: true, Ports: []platform.Port{{Port: 80, HTTPSupport: true}}},
		{Name: "db", SubdomainAccess: false},
	})

	host, err := ResolveProbeHostname(context.Background(), mock, "proj-1")
	if err != nil {
		t.Fatalf("ResolveProbeHostname: %v", err)
	}
	if host != "web" {
		t.Errorf("hostname: got %q, want %q", host, "web")
	}
}

func TestResolveProbeHostname_Multiple_Errors(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{Name: "web", SubdomainAccess: true, Ports: []platform.Port{{Port: 80, HTTPSupport: true}}},
		{Name: "api", SubdomainAccess: true, Ports: []platform.Port{{Port: 3000, HTTPSupport: true}}},
	})

	_, err := ResolveProbeHostname(context.Background(), mock, "proj-1")
	if err == nil {
		t.Fatal("expected error when >1 web-facing service")
	}
	if !strings.Contains(err.Error(), "multiple") {
		t.Errorf("error should mention multiple candidates, got %q", err)
	}
}

func TestResolveProbeHostname_None_Errors(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{Name: "db", SubdomainAccess: false},
	})

	_, err := ResolveProbeHostname(context.Background(), mock, "proj-1")
	if err == nil {
		t.Fatal("expected error when no web-facing service")
	}
}

// Eval-zcp project shape: ZCP control-plane container exposes a subdomain
// (for VS Code remote) AND has ports — without explicit filtering it would
// be picked up as a probe candidate alongside the agent's deployed dev/stage
// pair, refusing as multiple-candidates. Resolver must skip it by type.
func TestResolveProbeHostname_FiltersZCPControlPlane(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			Name:            "zcp",
			SubdomainAccess: true,
			Ports:           []platform.Port{{Port: 8080, HTTPSupport: true}},
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName:  "zcp@1",
				ServiceStackTypeCategoryName: "USER",
			},
		},
		{
			Name:            "appdev",
			SubdomainAccess: true,
			Ports:           []platform.Port{{Port: 3000, HTTPSupport: true}},
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName:  "bun@1.3",
				ServiceStackTypeCategoryName: "USER",
			},
		},
		{
			Name:            "appstage",
			SubdomainAccess: true,
			Ports:           []platform.Port{{Port: 3000, HTTPSupport: true}},
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName:  "bun@1.3",
				ServiceStackTypeCategoryName: "USER",
			},
		},
	})

	// Without the zcp filter: 3 candidates → "multiple" error.
	// With the filter: 2 candidates (appdev + appstage) → still "multiple"
	// because the scenario's agent picked standard mode and didn't hint a
	// hostname; that's a real ambiguity the scenario author must resolve.
	// Test the filter works by removing one runtime — should now pick the other.
	mockSingle := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			Name:            "zcp",
			SubdomainAccess: true,
			Ports:           []platform.Port{{Port: 8080, HTTPSupport: true}},
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName: "zcp@1",
			},
		},
		{
			Name:            "app",
			SubdomainAccess: true,
			Ports:           []platform.Port{{Port: 3000, HTTPSupport: true}},
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName: "bun@1.3",
			},
		},
	})

	host, err := ResolveProbeHostname(context.Background(), mockSingle, "proj-1")
	if err != nil {
		t.Fatalf("ResolveProbeHostname with single user runtime + zcp: %v", err)
	}
	if host != "app" {
		t.Errorf("hostname: got %q, want %q (zcp must be filtered out)", host, "app")
	}

	// Multi-runtime + zcp case: zcp filtered out → 2 user runtimes (appdev +
	// appstage) → standard-pair heuristic picks appstage. The previous
	// behavior (always-error on N>1) papered over the standard-mode case;
	// see TestResolveProbeHostname_StandardPair_PicksStage for the rationale.
	host, err = ResolveProbeHostname(context.Background(), mock, "proj-1")
	if err != nil {
		t.Fatalf("ResolveProbeHostname with zcp + standard pair: %v", err)
	}
	if host != "appstage" {
		t.Errorf("hostname: got %q, want %q (zcp filtered, stage half wins)", host, "appstage")
	}
}

// Standard-mode greenfield deploy: agent picks dev/stage pair, both halves
// surface as web-facing candidates. Resolver should auto-pick the stage
// half (production endpoint) instead of refusing on multiple-candidates.
// Without this heuristic every greenfield scenario where the agent picks
// standard mode would need explicit finalUrlHostname pinning.
func TestResolveProbeHostname_StandardPair_PicksStage(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			Name:            "appdev",
			SubdomainAccess: true,
			Ports:           []platform.Port{{Port: 3000, HTTPSupport: true}},
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName: "nodejs@22",
			},
		},
		{
			Name:            "appstage",
			SubdomainAccess: true,
			Ports:           []platform.Port{{Port: 3000, HTTPSupport: true}},
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName: "nodejs@22",
			},
		},
	})

	host, err := ResolveProbeHostname(context.Background(), mock, "proj-1")
	if err != nil {
		t.Fatalf("ResolveProbeHostname: %v", err)
	}
	if host != "appstage" {
		t.Errorf("hostname: got %q, want %q (stage half is production endpoint)", host, "appstage")
	}
}

// Two stage candidates is genuine ambiguity — heuristic must NOT silently
// pick one. Forces the scenario to set finalUrlHostname.
func TestResolveProbeHostname_TwoStageCandidates_StillErrors(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			Name:            "frontstage",
			SubdomainAccess: true,
			Ports:           []platform.Port{{Port: 3000, HTTPSupport: true}},
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName: "nodejs@22",
			},
		},
		{
			Name:            "backstage",
			SubdomainAccess: true,
			Ports:           []platform.Port{{Port: 8080, HTTPSupport: true}},
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName: "go@1.23",
			},
		},
	})

	_, err := ResolveProbeHostname(context.Background(), mock, "proj-1")
	if err == nil {
		t.Fatal("expected error when 2 candidates both end with 'stage'")
	}
	if !strings.Contains(err.Error(), "multiple") {
		t.Errorf("error should mention multiple candidates, got %q", err)
	}
}

// IsSystem-categorised services (CORE/BUILD/INTERNAL/PREPARE_RUNTIME/HTTP_L7_BALANCER)
// must also drop out before the subdomain check, in case any system service
// surfaces with subdomain access enabled in some platform configuration.
func TestResolveProbeHostname_FiltersSystemServices(t *testing.T) {
	t.Parallel()

	mock := platform.NewMock().WithServices([]platform.ServiceStack{
		{
			Name:            "infra",
			SubdomainAccess: true,
			Ports:           []platform.Port{{Port: 80, HTTPSupport: true}},
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName:  "haproxy@2",
				ServiceStackTypeCategoryName: "HTTP_L7_BALANCER",
			},
		},
		{
			Name:            "app",
			SubdomainAccess: true,
			Ports:           []platform.Port{{Port: 3000, HTTPSupport: true}},
			ServiceStackTypeInfo: platform.ServiceTypeInfo{
				ServiceStackTypeVersionName:  "bun@1.3",
				ServiceStackTypeCategoryName: "USER",
			},
		},
	})

	host, err := ResolveProbeHostname(context.Background(), mock, "proj-1")
	if err != nil {
		t.Fatalf("ResolveProbeHostname: %v", err)
	}
	if host != "app" {
		t.Errorf("hostname: got %q, want %q (HTTP_L7_BALANCER must be filtered out)", host, "app")
	}
}

func TestProbeFinalURL_HTTPFailure_ReturnsErr(t *testing.T) {
	t.Parallel()

	svc := platform.ServiceStack{
		ID:              "svc-1",
		Name:            "app",
		SubdomainAccess: true,
		Ports:           []platform.Port{{Port: 80, HTTPSupport: true}},
	}
	mock := platform.NewMock().
		WithServices([]platform.ServiceStack{svc}).
		WithProject(&platform.Project{SubdomainHost: "app.prg1.zerops.app"})

	doer := &fakeDoer{err: errors.New("dial tcp: connection refused")}

	probe := ProbeFinalURL(context.Background(), mock, doer, "proj-1", "app")
	if probe.Err == "" {
		t.Fatal("expected error when httpDoer fails")
	}
	if !strings.Contains(probe.Err, "connection refused") {
		t.Errorf("error should wrap dial error, got %q", probe.Err)
	}
	if probe.URL == "" {
		t.Error("URL should still be populated even when request fails")
	}
}

func TestGrade_FinalURLStatus_Matches_Passes(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{FinalURLStatus: 200},
	}
	probe := &FinalURLProbe{URL: "https://app-x.zerops.app", Got: 200}

	g := GradeWithProbe(sc, "", nil, "", probe)
	if !g.Passed {
		t.Errorf("expected PASS when probe status matches, got: %+v", g.Failures)
	}
}

func TestGrade_FinalURLStatus_Mismatch_Fails(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{FinalURLStatus: 200},
	}
	probe := &FinalURLProbe{URL: "https://app-x.zerops.app", Got: 502}

	g := GradeWithProbe(sc, "", nil, "", probe)
	if g.Passed {
		t.Fatal("expected FAIL when probe status differs")
	}
	if !containsFailure(g.Failures, "502") {
		t.Errorf("failure should mention actual status: %+v", g.Failures)
	}
	if !containsFailure(g.Failures, "200") {
		t.Errorf("failure should mention expected status: %+v", g.Failures)
	}
}

func TestGrade_FinalURLStatus_ProbeError_Fails(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{FinalURLStatus: 200},
	}
	probe := &FinalURLProbe{Err: "dial tcp: connection refused"}

	g := GradeWithProbe(sc, "", nil, "", probe)
	if g.Passed {
		t.Fatal("expected FAIL when probe errored")
	}
	if !containsFailure(g.Failures, "connection refused") {
		t.Errorf("failure should mention probe error: %+v", g.Failures)
	}
}

func TestGrade_FinalURLStatus_Zero_SkipsProbe(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{FinalURLStatus: 0},
	}

	g := GradeWithProbe(sc, "", nil, "", nil)
	if !g.Passed {
		t.Errorf("expected PASS when FinalURLStatus=0 (disabled), got: %+v", g.Failures)
	}
}

// Ensure nil probe with expected status produces a clear failure (scenario
// author set FinalURLStatus but the runner didn't execute the probe).
func TestGrade_FinalURLStatus_MissingProbe_Fails(t *testing.T) {
	t.Parallel()

	sc := &Scenario{
		Expect: Expectation{FinalURLStatus: 200},
	}

	g := GradeWithProbe(sc, "", nil, "", nil)
	if g.Passed {
		t.Fatal("expected FAIL when expectation set but probe missing")
	}
	if !containsFailure(g.Failures, "finalUrlStatus") {
		t.Errorf("failure should mention finalUrlStatus: %+v", g.Failures)
	}
}

// Sanity: BuildSubdomainURL-style input is well-formed; used by other tests.
var _ = fmt.Sprintf
