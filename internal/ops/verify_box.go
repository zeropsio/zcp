package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

// boxAPIPort is the port a Zerops Box serves its workspace API and web console
// on; 8081 is its sandbox ingress.
const boxAPIPort = 8080

// boxPingTimeout bounds the Box's ping: an orchestrator that does not answer
// is a failed check, not a verify that hangs.
const boxPingTimeout = 10 * time.Second

// boxPingMaxBody caps what is read of the ping's answer, a small JSON object.
const boxPingMaxBody = 64 << 10

// BoxOrchestratorCheck is a Zerops Box's "orchestrator" verify check. A Box is
// never probed as an app (PS-3); what it serves is its workspace API, whose
// GET /api/rest/ping is the one route that answers without the workspace API
// key. Other services of the project reach the API at http://<box>:8080, so
// the check asks only when zcp runs inside the project (inProject), and only
// once service_running passed — a stopped Box's API does not answer. No key
// is read or sent.
func BoxOrchestratorCheck(ctx context.Context, httpClient HTTPDoer, r *VerifyResult, inProject bool) CheckResult {
	check := CheckResult{Name: "orchestrator"}
	base := "http://" + net.JoinHostPort(r.Hostname, strconv.Itoa(boxAPIPort))
	pingURL := base + "/api/rest/ping"
	switch {
	case !inProject:
		check.Status = CheckSkip
		check.Detail = fmt.Sprintf("not asked: the workspace API at %s answers only inside the project — run zerops_verify from the project's zcp container to ask its keyless GET /api/rest/ping", base)
		return check
	case !serviceRunningPassed(r):
		check.Status = CheckSkip
		check.Detail = "not asked: the Box is not running, and its workspace API answers only while it runs"
		return check
	}

	pingCtx, cancel := context.WithTimeout(ctx, boxPingTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(pingCtx, http.MethodGet, pingURL, nil)
	if err != nil {
		check.Status = CheckFail
		check.Detail = fmt.Sprintf("build GET %s: %v", pingURL, err)
		return check
	}
	logsHint := fmt.Sprintf("the orchestrator starts with the Box's VM; read its log with zerops_logs serviceHostname=%q", r.Hostname)
	resp, err := httpClient.Do(req)
	if err != nil {
		check.Status = CheckFail
		check.Detail = fmt.Sprintf("GET %s did not answer: %v — %s", pingURL, err, logsHint)
		return check
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, boxPingMaxBody))
	if resp.StatusCode != http.StatusOK {
		check.Status = CheckFail
		check.Detail = fmt.Sprintf("GET %s answered %d — %s", pingURL, resp.StatusCode, logsHint)
		return check
	}
	var ping struct {
		Version string `json:"version"`
	}
	_ = json.Unmarshal(body, &ping) // the version is a detail; a 200 is the answer
	check.Status = CheckPass
	check.Detail = fmt.Sprintf("the workspace API at %s answers GET /api/rest/ping", base)
	if ping.Version != "" {
		check.Detail += fmt.Sprintf(" (orchestrator %s)", ping.Version)
	}
	return check
}

// serviceRunningPassed reports whether r's service_running check passed.
func serviceRunningPassed(r *VerifyResult) bool {
	for _, c := range r.Checks {
		if c.Name == checkNameServiceRunning {
			return c.Status == CheckPass
		}
	}
	return false
}
