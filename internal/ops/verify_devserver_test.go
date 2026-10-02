// Tests for: ops/verify.go + verify_checks.go — how verify reads a service a
// dev server serves: its first page compiles on the first request, and its
// host check may refuse the internal hostname the public address does not.
package ops

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// hostAnswers answers each probed host with its own status: the internal
// address (app:3000) and the public subdomain differ only by the Host the
// server sees.
type hostAnswers map[string]int

func (h hostAnswers) Do(req *http.Request) (*http.Response, error) {
	code, ok := h[req.URL.Host]
	if !ok {
		code = http.StatusOK
	}
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(http.StatusText(code))), Header: http.Header{}}, nil
}

// TestVerify_AnInternalAnswerTheHostCheckRefuses: an internal address that
// answers 4xx while the public address serves the same path is reached and
// serving — only the Host differs, which a dev server's host check (Vite's
// allowedHosts: 403) refuses. It is advisory, never a failed check; a 5xx, or
// a 4xx with no public answer to set it against, still fails.
func TestVerify_AnInternalAnswerTheHostCheckRefuses(t *testing.T) {
	t.Parallel()
	const publicHost = "app-1df2-3000.prg1.zerops.app"
	tests := []struct {
		name         string
		internal     int
		public       int
		intent       topology.PublicAccessIntent
		subdomain    bool
		wantInternal string
		wantStatus   string
	}{
		{name: "403 inside, the public address serves", internal: 403, public: 200, intent: topology.PublicAccessAuto, subdomain: true, wantInternal: CheckInfo, wantStatus: StatusHealthy},
		{name: "421 inside, the public address redirects", internal: 421, public: 302, intent: topology.PublicAccessAuto, subdomain: true, wantInternal: CheckInfo, wantStatus: StatusHealthy},
		{name: "403 inside and outside: the path is refused", internal: 403, public: 403, intent: topology.PublicAccessAuto, subdomain: true, wantInternal: CheckFail, wantStatus: StatusDegraded},
		{name: "500 inside: broken, whatever the public address says", internal: 500, public: 200, intent: topology.PublicAccessAuto, subdomain: true, wantInternal: CheckFail, wantStatus: StatusDegraded},
		{name: "403 inside, no public address: nothing to set it against", internal: 403, intent: topology.PublicAccessNone, wantInternal: CheckFail, wantStatus: StatusDegraded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().
				WithServices([]platform.ServiceStack{{
					ID: "svc-1", Name: "app", Status: "RUNNING", SubdomainAccess: tt.subdomain,
					ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
					Ports:                []platform.Port{{Port: 3000}},
				}}).
				WithProject(&platform.Project{ID: "proj-1", SubdomainHost: "1df2.prg1.zerops.app"})
			doer := hostAnswers{"app:3000": tt.internal, publicHost: tt.public}
			result, err := VerifyWithMeta(context.Background(), mock, platform.NewMockLogFetcher(), doer, "proj-1", "app",
				RuntimeMeta{}, PublicAccessInput{Record: topology.PublicAccessRecord{Intent: tt.intent}})
			if err != nil {
				t.Fatalf("verify: %v", err)
			}
			internal := findCheck(t, result, checkNameHTTPInternal, tt.wantInternal)
			if internal.HTTPStatus != tt.internal {
				t.Errorf("http_internal httpStatus = %d, want %d: the answer is reported as it came", internal.HTTPStatus, tt.internal)
			}
			if result.Status != tt.wantStatus {
				t.Errorf("status = %q, want %q (checks %+v)", result.Status, tt.wantStatus, result.Checks)
			}
		})
	}
}

// compilingServer is a dev server that compiles a page on its first request:
// nothing answers until compile has passed since that request; after it,
// every request is answered at once. A request the probe gives up on leaves
// the compile running, as a dev server does.
type compilingServer struct {
	mu      sync.Mutex
	compile time.Duration
	started time.Time
	tries   int
}

func (s *compilingServer) Do(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.tries++
	if s.started.IsZero() {
		s.started = time.Now()
	}
	ready := s.started.Add(s.compile)
	s.mu.Unlock()
	select {
	case <-time.After(time.Until(ready)):
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("shop")), Header: http.Header{}}, nil
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
}

// TestProbe_ADevServerGetsItsFirstCompile: a probe of a page a dev server
// serves waits through the compile its first request starts — retrying a
// request that got no answer, each wait longer, up to the ceiling — where any
// other probe gives up at its first wait.
func TestProbe_ADevServerGetsItsFirstCompile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		patient    bool
		compile    time.Duration
		wantStatus string
		wantTries  int
	}{
		{name: "a dev server answers after its compile", patient: true, compile: 45 * time.Millisecond, wantStatus: CheckPass, wantTries: 2},
		{name: "a dev server already compiled answers at once", patient: true, compile: 0, wantStatus: CheckPass, wantTries: 1},
		{name: "a dev server that never answers fails at the ceiling", patient: true, compile: time.Hour, wantStatus: CheckFail},
		{name: "any other server gets one wait", patient: false, compile: 60 * time.Millisecond, wantStatus: CheckFail, wantTries: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := &compilingServer{compile: tt.compile}
			p := httpProbe{doer: server, patient: tt.patient, first: 20 * time.Millisecond, ceiling: 300 * time.Millisecond}
			start := time.Now()
			check := p.probe(context.Background(), "http://appdev:3000/", checkNameHTTPInternal)
			took := time.Since(start)
			if check.Status != tt.wantStatus {
				t.Errorf("status = %q (%s), want %q", check.Status, check.Detail, tt.wantStatus)
			}
			if tt.wantTries > 0 && server.tries != tt.wantTries {
				t.Errorf("requests = %d, want %d", server.tries, tt.wantTries)
			}
			if tt.wantStatus == CheckFail && tt.patient && (took < 300*time.Millisecond || took > 600*time.Millisecond) {
				t.Errorf("gave up after %s, want at the 300ms ceiling", took)
			}
			if tt.wantStatus == CheckPass && tt.compile > 0 && !strings.Contains(check.Detail, "first request") {
				t.Errorf("detail = %q, want it to say the page compiled on its first request", check.Detail)
			}
		})
	}
}

// timeoutThenOK answers its first request with a timeout, then 200.
type timeoutThenOK struct {
	mu    sync.Mutex
	tries int
}

func (d *timeoutThenOK) Do(*http.Request) (*http.Response, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.tries++
	if d.tries == 1 {
		return nil, errTestTimeout
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("OK")), Header: http.Header{}}, nil
}

var errTestTimeout = timeoutError{}

type timeoutError struct{}

func (timeoutError) Error() string   { return "Client.Timeout exceeded while awaiting headers" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

// TestVerify_ADevModeRuntimeIsAllowedItsFirstCompile: verify of a runtime a
// dev server serves gives its HTTP probes the first compile; any other
// runtime keeps the one wait.
func TestVerify_ADevModeRuntimeIsAllowedItsFirstCompile(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		devServer    bool
		wantInternal string
	}{
		{name: "a dev server: the request a compile outlasted is asked again", devServer: true, wantInternal: CheckPass},
		{name: "not a dev server: no answer is a failed check", devServer: false, wantInternal: CheckFail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock := platform.NewMock().WithServices([]platform.ServiceStack{{
				ID: "svc-1", Name: "appdev", Status: "RUNNING",
				ServiceStackTypeInfo: platform.ServiceTypeInfo{ServiceStackTypeVersionName: "nodejs@22", ServiceStackTypeCategoryName: "USER"},
				Ports:                []platform.Port{{Port: 3000}},
			}})
			result, err := VerifyWithMeta(context.Background(), mock, platform.NewMockLogFetcher(), &timeoutThenOK{}, "proj-1", "appdev",
				RuntimeMeta{}, PublicAccessInput{Record: topology.PublicAccessRecord{Intent: topology.PublicAccessNone}, DevServer: tt.devServer})
			if err != nil {
				t.Fatalf("verify: %v", err)
			}
			findCheck(t, result, checkNameHTTPInternal, tt.wantInternal)
		})
	}
}

// TestProbeTimedOut tells a request a compile outlasted from one refused.
func TestProbeTimedOut(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "the attempt's own deadline", err: context.DeadlineExceeded, want: true},
		{name: "the client's timeout", err: errTestTimeout, want: true},
		{name: "refused", err: errors.New("dial tcp: connection refused"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := probeTimedOut(tt.err); got != tt.want {
				t.Errorf("probeTimedOut(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
