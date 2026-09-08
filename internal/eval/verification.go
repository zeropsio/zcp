package eval

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// processCreatedAfter reports whether a process's Created timestamp is strictly
// after runStart. Unparseable timestamps stay in scope: dropping them would turn
// unknown evidence into a false clean result. A zero runStart disables filtering.
func processCreatedAfter(created string, runStart time.Time) bool {
	if runStart.IsZero() || created == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339Nano, created)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, created)
		if err != nil {
			return true
		}
	}
	return parsed.After(runStart)
}

// RunVerification evaluates the scenario's Verification block against one
// direct, non-ES platform observation. The direct endpoints avoid classifying
// just-created services/processes from lagging search indexes.
func RunVerification(
	ctx context.Context,
	sc *Scenario,
	projectID string,
	client platform.Client,
	httpDoer ops.HTTPDoer,
	retrospectiveText string,
	runStart time.Time,
) []VerificationFinding {
	if sc == nil || sc.Verification == nil {
		return nil
	}
	observation := collectPlatformObservation(
		ctx,
		client,
		projectID,
		len(sc.Verification.ExpectedServices) > 0,
		sc.Verification.NoFailedProcesses,
	)
	return runVerificationWithObservation(ctx, sc, observation, httpDoer, retrospectiveText, runStart, projectID)
}

// runVerificationWithObservation derives the legacy advisory findings list
// from result rows (§10.1 point 3: rows are the single owner of the
// verdict), then appends the advisory-only retrospective phrase check, which
// is never a row because it isn't a deterministic platform assertion.
func runVerificationWithObservation(
	ctx context.Context,
	sc *Scenario,
	observation platformObservation,
	httpDoer ops.HTTPDoer,
	retrospectiveText string,
	runStart time.Time,
	projectID string,
) []VerificationFinding {
	if sc == nil || sc.Verification == nil {
		return nil
	}
	rows := generateRequiredChecks(ctx, sc, observation, httpDoer, runStart, projectID)
	findings := projectRowsToFindings(rows)
	findings = append(findings, retrospectivePhraseFindings(sc, retrospectiveText)...)
	return findings
}

// retrospectivePhraseFindings evaluates VerificationConfig.RetrospectiveMustNotMention.
// Advisory-only: grading open-ended retrospective prose is not a deterministic
// platform assertion, so it never produces a result row (§10.1).
func retrospectivePhraseFindings(sc *Scenario, retrospectiveText string) []VerificationFinding {
	var findings []VerificationFinding
	if sc.Verification == nil || len(sc.Verification.RetrospectiveMustNotMention) == 0 || retrospectiveText == "" {
		return findings
	}
	lower := strings.ToLower(retrospectiveText)
	for _, phrase := range sc.Verification.RetrospectiveMustNotMention {
		if phrase == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(phrase)) {
			findings = append(findings, VerificationFinding{
				Severity: "fail",
				Check:    "retrospective_phrase_forbidden",
				Message:  fmt.Sprintf("retrospective mentions forbidden phrase %q", phrase),
			})
		}
	}
	return findings
}

// projectRowsToFindings projects result rows to the legacy advisory finding
// shape: failed → fail, blocked → warn. Passed/not-run rows produce no
// finding, matching the pre-rows behavior where only problems surfaced.
func projectRowsToFindings(rows []RequiredCheck) []VerificationFinding {
	var findings []VerificationFinding
	for _, row := range rows {
		switch row.Result {
		case CheckFailed:
			findings = append(findings, VerificationFinding{Severity: "fail", Check: row.Check, Message: row.Message})
		case CheckBlocked:
			findings = append(findings, VerificationFinding{Severity: "warn", Check: row.Check, Message: row.Message})
		case CheckPassed, CheckNotRun:
		}
	}
	return findings
}

// generateRequiredChecks evaluates every executable check declared in
// sc.Verification against one platformObservation and returns the result
// rows (§10.1). Rows are the single owner of the verdict — RunVerification's
// legacy findings and the required-mode task result both derive from these.
func generateRequiredChecks(
	ctx context.Context,
	sc *Scenario,
	observation platformObservation,
	httpDoer ops.HTTPDoer,
	runStart time.Time,
	projectID string,
) []RequiredCheck {
	if sc == nil || sc.Verification == nil {
		return nil
	}
	var rows []RequiredCheck
	for _, exp := range sc.Verification.ExpectedServices {
		rows = append(rows, evaluateExpectedService(ctx, exp, observation, httpDoer)...)
	}
	if sc.Verification.NoFailedProcesses {
		rows = append(rows, evaluateNoFailedProcesses(observation, runStart, projectID))
	}
	return rows
}

// evaluateExpectedService evaluates one ExpectedService assertion against
// the observation, yielding one row per configured sub-check
// (service_status, service_type, subdomain_probe), or a single
// expected_service/<host>/exists row when the service can't be resolved or
// when no sub-check is configured.
func evaluateExpectedService(
	ctx context.Context,
	exp ExpectedService,
	observation platformObservation,
	httpDoer ops.HTTPDoer,
) []RequiredCheck {
	now := observation.observedAt
	if observation.servicesErr != nil {
		// Existence is a prerequisite for every sub-check: a query failure
		// blocks resolving the service at all, so it surfaces as a single
		// expected_service/<host>/exists row rather than one blocked row per
		// configured sub-check.
		return []RequiredCheck{{
			ID: expectedServiceRowID(exp.Hostname, "exists"), Check: "expected_service", Scope: exp.Hostname,
			Result: CheckBlocked, ObservedAt: now, Source: "ListServicesDirect",
			Message: fmt.Sprintf("ListServicesDirect failed: %v", observation.servicesErr),
		}}
	}

	found := findServiceByHostname(observation.services, exp.Hostname)
	if found == nil {
		return []RequiredCheck{{
			ID: expectedServiceRowID(exp.Hostname, "exists"), Check: "expected_service", Scope: exp.Hostname,
			Result: CheckFailed, Expected: "exists", Observed: "not found", ObservedAt: now, Source: "ListServicesDirect",
			Message: fmt.Sprintf("service %q not found in project", exp.Hostname),
		}}
	}

	var rows []RequiredCheck
	if len(exp.Status) > 0 {
		result := CheckFailed
		if sliceContainsString(exp.Status, found.Status) {
			result = CheckPassed
		}
		rows = append(rows, RequiredCheck{
			ID: expectedServiceRowID(exp.Hostname, "status"), Check: "service_status", Scope: exp.Hostname,
			Result: result, Expected: fmt.Sprintf("%v", exp.Status), Observed: found.Status, ObservedAt: now, Source: "ListServicesDirect",
			Message: fmt.Sprintf("service %q status %q (want %v)", exp.Hostname, found.Status, exp.Status),
		})
	}
	if exp.Type != "" {
		result := CheckFailed
		if matchTypeGlob(found.ServiceStackTypeInfo.ServiceStackTypeVersionName, exp.Type) {
			result = CheckPassed
		}
		rows = append(rows, RequiredCheck{
			ID: expectedServiceRowID(exp.Hostname, "type"), Check: "service_type", Scope: exp.Hostname,
			Result: result, Expected: exp.Type, Observed: found.ServiceStackTypeInfo.ServiceStackTypeVersionName, ObservedAt: now, Source: "ListServicesDirect",
			Message: fmt.Sprintf("service %q type %q (want %q)", exp.Hostname, found.ServiceStackTypeInfo.ServiceStackTypeVersionName, exp.Type),
		})
	}
	if exp.SubdomainProbe != nil {
		rows = append(rows, evaluateSubdomainProbeRow(ctx, exp, found, httpDoer, now))
	}
	if len(rows) == 0 {
		rows = append(rows, RequiredCheck{
			ID: expectedServiceRowID(exp.Hostname, "exists"), Check: "expected_service", Scope: exp.Hostname,
			Result: CheckPassed, Expected: "exists", Observed: "found", ObservedAt: now, Source: "ListServicesDirect",
			Message: fmt.Sprintf("service %q found", exp.Hostname),
		})
	}
	return rows
}

// evaluateSubdomainProbeRow evaluates the subdomain_probe row. Probe honesty
// (§10.2): a URL that cannot be resolved from the platform read is blocked,
// never a silent pass; when a URL resolves, the row is decided by an actual
// HTTP request.
func evaluateSubdomainProbeRow(
	ctx context.Context,
	exp ExpectedService,
	svc *platform.ServiceStack,
	httpDoer ops.HTTPDoer,
	now time.Time,
) RequiredCheck {
	id := expectedServiceRowID(exp.Hostname, "subdomain_probe")
	if !svc.SubdomainAccess {
		return RequiredCheck{
			ID: id, Check: "subdomain_probe", Scope: exp.Hostname,
			Result: CheckFailed, Expected: "subdomain enabled", Observed: "disabled", ObservedAt: now, Source: "ListServicesDirect",
			Message: fmt.Sprintf("service %q has no subdomain access enabled (cannot probe)", exp.Hostname),
		}
	}
	probe := exp.SubdomainProbe
	url := probeURLFromService(svc, probe.Path)
	if url == "" {
		return RequiredCheck{
			ID: id, Check: "subdomain_probe", Scope: exp.Hostname,
			Result: CheckBlocked, Expected: "resolvable subdomain URL", ObservedAt: now, Source: "ListServicesDirect",
			Message: fmt.Sprintf("service %q has subdomainAccess=true but URL not resolvable from ServiceStack fields", exp.Hostname),
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return RequiredCheck{
			ID: id, Check: "subdomain_probe", Scope: exp.Hostname,
			Result: CheckBlocked, ObservedAt: now, Source: "HTTP GET " + url,
			Message: fmt.Sprintf("build request for %s: %v", url, err),
		}
	}
	resp, err := httpDoer.Do(req)
	if err != nil {
		return RequiredCheck{
			ID: id, Check: "subdomain_probe", Scope: exp.Hostname,
			Result: CheckFailed, ObservedAt: now, Source: "HTTP GET " + url,
			Message: fmt.Sprintf("GET %s: %v", url, err),
		}
	}
	defer resp.Body.Close()
	result := CheckFailed
	if statusMatches(resp.StatusCode, probe.ExpectStatus) {
		result = CheckPassed
	}
	return RequiredCheck{
		ID: id, Check: "subdomain_probe", Scope: exp.Hostname,
		Result: result, Expected: probe.ExpectStatus, Observed: fmt.Sprintf("%d", resp.StatusCode), ObservedAt: now, Source: "HTTP GET " + url,
		Message: fmt.Sprintf("GET %s returned %d, expected %s", url, resp.StatusCode, probe.ExpectStatus),
	}
}

// evaluateNoFailedProcesses evaluates the no_failed_processes/<projectId>
// row: any FAILED process created after runStart fails the row; a query
// error blocks it; otherwise it passes.
func evaluateNoFailedProcesses(observation platformObservation, runStart time.Time, projectID string) RequiredCheck {
	id := noFailedProcessesRowID(projectID)
	now := observation.observedAt
	if observation.processesErr != nil {
		return RequiredCheck{
			ID: id, Check: "no_failed_processes", Scope: projectID,
			Result: CheckBlocked, ObservedAt: now, Source: "GetProjectProcessesDirect",
			Message: fmt.Sprintf("GetProjectProcessesDirect failed: %v", observation.processesErr),
		}
	}
	var failedIDs []string
	var messages []string
	for _, process := range observation.processes {
		if process.Status != platform.ProcessStatusFailed || !processCreatedAfter(process.Created, runStart) {
			continue
		}
		reason := process.ActionName
		if process.FailReason != nil && *process.FailReason != "" {
			reason = *process.FailReason
		}
		serviceLabel := ""
		if len(process.ServiceStacks) > 0 {
			serviceLabel = " on " + process.ServiceStacks[0].Name
		}
		failedIDs = append(failedIDs, process.ID)
		messages = append(messages, fmt.Sprintf("FAILED process %s: %s%s", process.ID, reason, serviceLabel))
	}
	expected := fmt.Sprintf("no FAILED process after %s", runStart)
	if len(failedIDs) == 0 {
		return RequiredCheck{
			ID: id, Check: "no_failed_processes", Scope: projectID,
			Result: CheckPassed, Expected: expected, Observed: "none", ObservedAt: now, Source: "GetProjectProcessesDirect",
			Message: "no FAILED process found",
		}
	}
	return RequiredCheck{
		ID: id, Check: "no_failed_processes", Scope: projectID,
		Result: CheckFailed, Expected: expected, Observed: strings.Join(failedIDs, ", "), ObservedAt: now, Source: "GetProjectProcessesDirect",
		Message: strings.Join(messages, "; "),
	}
}

// probeURLFromService derives the public subdomain URL from a ServiceStack
// when possible. ServiceStack itself doesn't carry the resolved subdomain
// — that lives in env vars (${zeropsSubdomain}). We can't form the URL
// from ServiceStack alone, so this returns "" — caller emits a blocked row
// (§10.2 probe honesty).
//
// A future iteration can pass a resolved URL through the VerificationConfig
// (e.g. `subdomainProbe.url: ${zeropsSubdomain}` parsed at scenario load
// after services are up).
func probeURLFromService(svc *platform.ServiceStack, path string) string {
	_ = svc
	_ = path
	return ""
}

// findServiceByHostname returns the first service whose Name matches host.
// Hostnames are unique per project so first-match is authoritative.
func findServiceByHostname(services []platform.ServiceStack, host string) *platform.ServiceStack {
	for i := range services {
		if services[i].Name == host {
			return &services[i]
		}
	}
	return nil
}

// sliceContainsString is a thin alias over slices.Contains, kept for
// callsite readability (`status not in [...]` reads naturally).
func sliceContainsString(s []string, x string) bool {
	return slices.Contains(s, x)
}

// matchTypeGlob returns true when actual matches the glob pattern. Only the
// trailing `*` wildcard is supported. A bare authored type accepts the live
// platform's equivalent composite form (OS prefix or managed-service mode),
// while an explicitly decorated pattern remains strict about that variant.
func matchTypeGlob(actual, pattern string) bool {
	candidate := actual
	if topology.CanonicalBareForm(pattern) == pattern {
		candidate = topology.CanonicalBareForm(actual)
	}
	if prefix, hadStar := strings.CutSuffix(pattern, "*"); hadStar {
		return strings.HasPrefix(candidate, prefix)
	}
	return candidate == pattern
}

// statusMatches reports whether code satisfies the expectStatus rule.
// Supported shapes: "" or "any" → any code; "2xx" / "3xx" / "4xx" / "5xx"
// → class match; "200" → exact; "200-299" → range.
func statusMatches(code int, expect string) bool {
	if expect == "" || expect == "any" {
		return true
	}
	if strings.HasSuffix(expect, "xx") && len(expect) == 3 {
		// 2xx, 3xx, etc.
		class := int(expect[0]-'0') * 100
		return code >= class && code < class+100
	}
	if strings.Contains(expect, "-") {
		parts := strings.SplitN(expect, "-", 2)
		var lo, hi int
		if _, err := fmt.Sscanf(parts[0], "%d", &lo); err != nil {
			return false
		}
		if _, err := fmt.Sscanf(parts[1], "%d", &hi); err != nil {
			return false
		}
		return code >= lo && code <= hi
	}
	var want int
	if _, err := fmt.Sscanf(expect, "%d", &want); err != nil {
		return false
	}
	return code == want
}
