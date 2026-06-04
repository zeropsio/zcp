package ops

import "testing"

// TestVerifyResult_PassedForLifecycle pins Friction-2: a verify degraded ONLY
// by a cosmetic http_root 4xx (REST API with no `/` route) counts as passed
// for auto-close, while a 5xx, a connection error, a non-http_root failure, or
// an unhealthy result still blocks. The verify RESPONSE keeps reporting
// degraded — this governs only the lifecycle gate.
func TestVerifyResult_PassedForLifecycle(t *testing.T) {
	t.Parallel()
	httpRootFail := func(code int) CheckResult {
		return CheckResult{Name: checkNameHTTPRoot, Status: CheckFail, HTTPStatus: code}
	}
	cases := []struct {
		name string
		r    *VerifyResult
		want bool
	}{
		{"healthy", &VerifyResult{Status: StatusHealthy}, true},
		{"degraded cosmetic 404", &VerifyResult{Status: StatusDegraded, Checks: []CheckResult{httpRootFail(404)}}, true},
		{"degraded auth-gated 401", &VerifyResult{Status: StatusDegraded, Checks: []CheckResult{httpRootFail(401)}}, true},
		{"degraded method 405", &VerifyResult{Status: StatusDegraded, Checks: []CheckResult{httpRootFail(405)}}, true},
		{"degraded http_root 5xx blocks", &VerifyResult{Status: StatusDegraded, Checks: []CheckResult{httpRootFail(503)}}, false},
		{"degraded conn error (status 0) blocks", &VerifyResult{Status: StatusDegraded, Checks: []CheckResult{httpRootFail(0)}}, false},
		{"degraded non-http_root failure blocks", &VerifyResult{Status: StatusDegraded, Checks: []CheckResult{{Name: "error_logs", Status: CheckFail}}}, false},
		{"degraded 404 + another failure blocks", &VerifyResult{Status: StatusDegraded, Checks: []CheckResult{httpRootFail(404), {Name: "error_logs", Status: CheckFail}}}, false},
		{"unhealthy blocks", &VerifyResult{Status: StatusUnhealthy, Checks: []CheckResult{{Name: "service_running", Status: CheckFail}}}, false},
		{"nil blocks", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := tc.r.PassedForLifecycle(); got != tc.want {
				t.Errorf("PassedForLifecycle() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAggregateAllStatus pins the multi-service rollup (Friction-2 follow-up,
// Codex catch): a LONE cosmetic-degraded service must report `degraded`, not
// `unhealthy` — else the project status contradicts the lifecycle accepting it.
// A hard-down service with nothing healthy is a total outage = unhealthy; a
// mix of healthy + unhealthy is a partial outage = degraded.
func TestAggregateAllStatus(t *testing.T) {
	t.Parallel()
	res := func(statuses ...string) []VerifyResult {
		out := make([]VerifyResult, len(statuses))
		for i, s := range statuses {
			out[i] = VerifyResult{Status: s}
		}
		return out
	}
	cases := []struct {
		name        string
		results     []VerifyResult
		wantStatus  string
		wantHealthy int
	}{
		{"all healthy", res(StatusHealthy, StatusHealthy), StatusHealthy, 2},
		{"lone cosmetic degraded → degraded NOT unhealthy", res(StatusDegraded), StatusDegraded, 0},
		{"healthy + degraded → degraded", res(StatusHealthy, StatusDegraded), StatusDegraded, 1},
		{"healthy + unhealthy (partial) → degraded", res(StatusHealthy, StatusUnhealthy), StatusDegraded, 1},
		{"lone unhealthy (total outage) → unhealthy", res(StatusUnhealthy), StatusUnhealthy, 0},
		{"unhealthy + degraded, no healthy → unhealthy", res(StatusUnhealthy, StatusDegraded), StatusUnhealthy, 0},
		{"empty → healthy (vacuous)", res(), StatusHealthy, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotStatus, gotHealthy := aggregateAllStatus(tc.results)
			if gotStatus != tc.wantStatus || gotHealthy != tc.wantHealthy {
				t.Errorf("aggregateAllStatus = (%q, %d), want (%q, %d)", gotStatus, gotHealthy, tc.wantStatus, tc.wantHealthy)
			}
		})
	}
}
