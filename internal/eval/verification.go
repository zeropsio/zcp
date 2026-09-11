package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/capture"
	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
	"github.com/zeropsio/zcp/internal/topology"
)

// RuntimeInputs carries the run-scoped evidence generateRequiredChecks and
// generateAskWhenAdvisoryFindings need for the O6/O7/O8 oracles and the
// askWhen decision row (docs/spec-eval-farm.md §4.4, §4.1 FM-31) that
// RunVerification itself cannot derive from a platform observation alone:
// LaunchTokenSHA256 is the hex sha256 of the run's ZCP_E2E_LAUNCH_KEY (hashed
// once at the read site, never stored/logged/passed anywhere else, empty
// when the env is absent); MCPStreamPaths are the run's captured
// capture/mcp/zcp-<pid>.jsonl files (chronological order not required — every
// consumer treats the set as one merged stream); TranscriptPath is the
// scenario's transcript.jsonl; UserSimTurns is the run's recorded user-sim
// turns (BehavioralResult.UserSim.Turns), empty meaning none were recorded;
// MutatingTools names the tools whose annotations mark them non-read-only —
// eval/ may not import internal/tools directly (docs/spec-architecture.md's
// package table: eval is a peer to tools), so the caller supplies this
// precomputed set; empty means every call is treated as non-mutating. A
// zero-value RuntimeInputs is valid: every consumer treats an empty field as
// "no evidence available" and blocks rather than silently passing or
// omitting the row.
type RuntimeInputs struct {
	LaunchTokenSHA256 string
	MCPStreamPaths    []string
	TranscriptPath    string
	UserSimTurns      []UserSimTurn
	MutatingTools     map[string]bool
}

// readRuntimeMCPCalls reads every path in mcpStreamPaths via capture.ReadMCPStream
// — the same reader mcpToolCallTexts and evaluateNoFabricatedSecretRow use —
// concatenating the resulting tool calls in path order. present is true once
// at least one path is read without error; a path that fails to read is
// skipped (best-effort, matching RuntimeInputs's "empty evidence blocks
// rather than fails" contract). present stays false only when every path
// failed or mcpStreamPaths is empty — the "no captured MCP stream" case
// EvaluateNeverRows and EvaluateAskWhenRows block on rather than silently
// pass.
func readRuntimeMCPCalls(mcpStreamPaths []string) (calls []capture.MCPToolCall, present bool) {
	for _, path := range mcpStreamPaths {
		fileCalls, err := capture.ReadMCPStream(path)
		if err != nil {
			continue
		}
		present = true
		calls = append(calls, fileCalls...)
	}
	return calls, present
}

// readTranscriptText reads path's full contents for the O6
// token_not_in_transcript scan. Empty on any read error or empty path — the
// scan then degrades to scanning only the MCP tool-call texts.
func readTranscriptText(path string) string {
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

// mcpToolCallTexts reads every path in mcpStreamPaths and renders each tool
// call's arguments as one whitespace-scannable text blob (JSON-marshaled),
// for the O6 token_not_in_transcript scan and the O8 no_fabricated_secret
// scan's underlying stream reads (evaluateNoFabricatedSecretRow reads its
// own path directly; this helper only serves O6's toolCallTexts parameter).
// A path that fails to read is skipped — best-effort, matching the "empty
// evidence blocks rather than fails" contract RuntimeInputs documents.
func mcpToolCallTexts(mcpStreamPaths []string) []string {
	var texts []string
	for _, path := range mcpStreamPaths {
		calls, err := capture.ReadMCPStream(path)
		if err != nil {
			continue
		}
		for _, call := range calls {
			if data, marshalErr := json.Marshal(call.Arguments); marshalErr == nil {
				texts = append(texts, string(data))
			}
		}
	}
	return texts
}

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
	runtime RuntimeInputs,
) []VerificationFinding {
	if sc == nil || sc.Verification == nil {
		return nil
	}
	observation := collectPlatformObservation(
		ctx,
		client,
		projectID,
		len(sc.Verification.ExpectedServices) > 0 || sc.Verification.Liveness != nil || len(sc.Verification.Unchanged) > 0,
		sc.Verification.NoFailedProcesses,
	)
	return runVerificationWithObservation(ctx, sc, observation, httpDoer, retrospectiveText, runStart, projectID, client, true, nil, runtime)
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
	client platform.Client,
	settled bool,
	baseline *ScenarioBaseline,
	runtime RuntimeInputs,
) []VerificationFinding {
	if sc == nil || sc.Verification == nil {
		return nil
	}
	rows := generateRequiredChecks(ctx, sc, observation, httpDoer, runStart, projectID, client, settled, baseline, runtime)
	findings := projectRowsToFindings(rows)
	findings = append(findings, generateAskWhenAdvisoryFindings(sc, runtime, observation.observedAt)...)
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
	client platform.Client,
	settled bool,
	baseline *ScenarioBaseline,
	runtime RuntimeInputs,
) []RequiredCheck {
	if sc == nil || sc.Verification == nil {
		return nil
	}
	var rows []RequiredCheck
	for _, exp := range sc.Verification.ExpectedServices {
		rows = append(rows, evaluateExpectedService(ctx, exp, observation, httpDoer, client, projectID)...)
	}
	if sc.Verification.NoFailedProcesses {
		rows = append(rows, evaluateNoFailedProcesses(observation, runStart, projectID, sc.Verification.AllowFailed))
	}
	if sc.Verification.NodePostgresRecord != nil {
		rows = append(rows, evaluateNodePostgresRecordCheck(ctx, sc.Verification.NodePostgresRecord, client, httpDoer, projectID, settled, baseline)...)
	}
	if sc.Verification.Liveness != nil {
		rows = append(rows, evaluateLivenessRow(ctx, sc.Verification.Liveness, observation, httpDoer, client, projectID))
	}
	for _, hostname := range sc.Verification.Unchanged {
		rows = append(rows, evaluateUnchangedFieldRow(hostname, observation, baseline))
	}
	if sc.Verification.LaunchShape != nil {
		rows = append(rows, evaluateLaunchShapeRows(
			ctx, sc.Verification.LaunchShape, client,
			readTranscriptText(runtime.TranscriptPath),
			mcpToolCallTexts(runtime.MCPStreamPaths),
			runtime.LaunchTokenSHA256,
		)...)
	}
	if sc.Verification.NoFabricatedSecret {
		var mcpStreamPath string
		if len(runtime.MCPStreamPaths) > 0 {
			mcpStreamPath = runtime.MCPStreamPaths[0]
		}
		rows = append(rows, evaluateNoFabricatedSecretRow(mcpStreamPath, sc.ID, nil))
	}
	for _, entry := range sc.Verification.ArtifactPromotion {
		rows = append(rows, evaluateArtifactPromotionRows(ctx, entry, client, projectID, runStart, baseline)...)
	}
	if len(sc.Verification.Never) > 0 {
		calls, present := readRuntimeMCPCalls(runtime.MCPStreamPaths)
		rows = append(rows, EvaluateNeverRows(sc.Verification.Never, calls, present, observation.observedAt)...)
	}
	return rows
}

// generateAskWhenAdvisoryFindings evaluates every verification.askWhen entry
// (FM-31) and projects the resulting rows into the legacy advisory finding
// shape (projectRowsToFindings) — advisory-only, kept deliberately OUT of
// generateRequiredChecks's return value: that slice is what callers pass
// straight into aggregateTaskResult (behavioral_run.go), and an askWhen row
// mixed into it would gate the task result the moment any Result is
// CheckFailed, contradicting FM-31 ("never contributes to the scenario's
// aggregated result"). Returns nil when the scenario declares no askWhen
// entries.
func generateAskWhenAdvisoryFindings(sc *Scenario, runtime RuntimeInputs, now time.Time) []VerificationFinding {
	if sc == nil || sc.Verification == nil || len(sc.Verification.AskWhen) == 0 {
		return nil
	}
	calls, present := readRuntimeMCPCalls(runtime.MCPStreamPaths)
	observations := BuildAskWhenObservations(sc.Verification.AskWhen, calls, runtime.UserSimTurns, runtime.MutatingTools)
	return projectRowsToFindings(EvaluateAskWhenRows(observations, present, now))
}

// evaluateUnchangedFieldRow grades one entry of the standalone
// verification.unchanged list (docs/spec-eval-farm.md §4.1 FM-29): the same
// grading nodePostgresRecord's unrelated row uses (gradeUnchangedRow,
// internal/eval/node_postgres_verifier.go). Independent of whether the
// scenario also declares nodePostgresRecord (FM-29) — the per-hostname
// baseline (behavioral_run.go's scenarioBaselineHostnames/
// recordScenarioBaseline) covers the union of both, so every declared
// hostname is looked up by its own entry in baseline.AppVersions; a
// hostname absent from that map (never recorded, or recorded with no
// active app-version) blocks with an explicit "no baseline for <host>"
// message, never a silent pass.
func evaluateUnchangedFieldRow(hostname string, observation platformObservation, baseline *ScenarioBaseline) RequiredCheck {
	id := unrelatedArtifactRowID(hostname)
	now := observation.observedAt
	if observation.servicesErr != nil {
		return RequiredCheck{ID: id, Check: checkUnrelatedArtifact, Scope: hostname, Result: CheckBlocked, ObservedAt: now, Source: "ListServicesDirect", Message: fmt.Sprintf("ListServicesDirect failed: %v", observation.servicesErr)}
	}
	baselineAppVersion, ok := "", false
	if baseline != nil {
		baselineAppVersion, ok = baseline.AppVersions[hostname]
	}
	if !ok || baselineAppVersion == "" {
		return RequiredCheck{ID: id, Check: checkUnrelatedArtifact, Scope: hostname, Result: CheckBlocked, ObservedAt: now, Source: "ListServicesDirect", Message: fmt.Sprintf("no baseline for %s", hostname)}
	}
	svc := findServiceByHostname(observation.services, hostname)
	return gradeUnchangedRow(id, hostname, baselineAppVersion, svc, now)
}

// livenessRowID builds the stable row id for the O2 liveness check
// (docs/spec-eval-farm.md §4.1 FM-27 table).
func livenessRowID(service string) string {
	return fmt.Sprintf("liveness/%s/marker", service)
}

// evaluateLivenessRow evaluates the O2 liveness probe: resolve the named
// service's subdomain URL through ops.ResolveSubdomainURL (never from the
// agent), expect a 2xx response whose body contains Marker. Reuses the
// subdomain-probe row's HTTP path rather than a second HTTP client.
func evaluateLivenessRow(ctx context.Context, probe *LivenessProbe, observation platformObservation, httpDoer ops.HTTPDoer, client platform.Client, projectID string) RequiredCheck {
	id := livenessRowID(probe.Service)
	now := observation.observedAt
	if observation.servicesErr != nil {
		return RequiredCheck{ID: id, Check: "liveness", Scope: probe.Service, Result: CheckBlocked, ObservedAt: now, Source: "ListServicesDirect", Message: fmt.Sprintf("ListServicesDirect failed: %v", observation.servicesErr)}
	}
	svc := findServiceByHostname(observation.services, probe.Service)
	if svc == nil {
		return RequiredCheck{ID: id, Check: "liveness", Scope: probe.Service, Result: CheckFailed, Expected: "exists", Observed: "not found", ObservedAt: now, Source: "ListServicesDirect", Message: fmt.Sprintf("service %q not found in project", probe.Service)}
	}
	url := ops.ResolveSubdomainURL(ctx, client, projectID, svc)
	if url == "" {
		return RequiredCheck{ID: id, Check: "liveness", Scope: probe.Service, Result: CheckBlocked, Expected: "resolvable subdomain URL", ObservedAt: now, Source: "ListServicesDirect", Message: fmt.Sprintf("service %q has no resolvable subdomain URL", probe.Service)}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return RequiredCheck{ID: id, Check: "liveness", Scope: probe.Service, Result: CheckBlocked, ObservedAt: now, Source: "HTTP GET " + url, Message: fmt.Sprintf("build request for %s: %v", url, err)}
	}
	resp, err := httpDoer.Do(req)
	if err != nil {
		return RequiredCheck{ID: id, Check: "liveness", Scope: probe.Service, Result: CheckFailed, ObservedAt: now, Source: "HTTP GET " + url, Message: fmt.Sprintf("GET %s: %v", url, err)}
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxLivenessBodyBytes))
	if readErr != nil {
		return RequiredCheck{ID: id, Check: "liveness", Scope: probe.Service, Result: CheckFailed, ObservedAt: now, Source: "HTTP GET " + url, Message: fmt.Sprintf("read body from %s: %v", url, readErr)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return RequiredCheck{ID: id, Check: "liveness", Scope: probe.Service, Result: CheckFailed, Expected: "2xx with marker " + probe.Marker, Observed: fmt.Sprintf("%d", resp.StatusCode), ObservedAt: now, Source: "HTTP GET " + url, Message: fmt.Sprintf("GET %s returned %d, expected 2xx", url, resp.StatusCode)}
	}
	if !strings.Contains(string(body), probe.Marker) {
		return RequiredCheck{ID: id, Check: "liveness", Scope: probe.Service, Result: CheckFailed, Expected: "body contains " + probe.Marker, Observed: "marker not found", ObservedAt: now, Source: "HTTP GET " + url, Message: fmt.Sprintf("GET %s returned 2xx but body does not contain marker %q", url, probe.Marker)}
	}
	return RequiredCheck{ID: id, Check: "liveness", Scope: probe.Service, Result: CheckPassed, Expected: "2xx with marker " + probe.Marker, Observed: fmt.Sprintf("%d, marker present", resp.StatusCode), ObservedAt: now, Source: "HTTP GET " + url, Message: fmt.Sprintf("GET %s returned %d with marker %q present", url, resp.StatusCode, probe.Marker)}
}

// maxLivenessBodyBytes caps the liveness probe's body read — a marker
// substring check never needs an unbounded read.
const maxLivenessBodyBytes = 1 << 20

// evaluateNodePostgresRecordCheck adapts the exported NodePostgresVerifier
// into the generateRequiredChecks pipeline. §10.2 ordering: the oracle runs
// only when the task-end observation settled; an unsettled freeze emits its
// four rows blocked with zero HTTP/SQL calls, so the verifier itself is
// never constructed in that case.
func evaluateNodePostgresRecordCheck(
	ctx context.Context,
	cfg *NodePostgresRecordConfig,
	client platform.Client,
	httpDoer ops.HTTPDoer,
	projectID string,
	settled bool,
	baseline *ScenarioBaseline,
) []RequiredCheck {
	in := NodePostgresInput{ProjectID: projectID, Stage: cfg.Stage, Database: cfg.Database, Unrelated: cfg.Unrelated, Environment: cfg.Environment}
	if baseline != nil {
		in.BaselineUnrelatedAppVersion = baseline.AppVersions[cfg.Unrelated]
	}
	if !settled {
		return blockedNodePostgresRows(in, time.Now().UTC(), "task-end observation unsettled")
	}
	verifier := NodePostgresVerifier{Client: client, HTTP: httpDoer, DB: PgxNodePostgresDB{}, Nonce: randomNodePostgresNonce}
	return verifier.Verify(ctx, in)
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
	client platform.Client,
	projectID string,
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
		rows = append(rows, evaluateSubdomainProbeRow(ctx, exp, found, httpDoer, client, projectID, now))
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
	client platform.Client,
	projectID string,
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
	url := ops.ResolveSubdomainURL(ctx, client, projectID, svc)
	if url != "" && probe.Path != "" {
		url = strings.TrimRight(url, "/") + "/" + strings.TrimLeft(probe.Path, "/")
	}
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
// error blocks it; otherwise it passes. allowFailed lists services whose
// FAILED process state is the scenario's seeded starting point, not a
// violation (docs/spec-eval-farm.md §4.1 FM-28) — a process naming any of
// those services is ignored regardless of when it was created.
func evaluateNoFailedProcesses(observation platformObservation, runStart time.Time, projectID string, allowFailed []string) RequiredCheck {
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
		if processOnAllowedService(process, allowFailed) {
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

// processOnAllowedService reports whether process names at least one
// service in allowFailed among its ServiceStacks.
func processOnAllowedService(process platform.Process, allowFailed []string) bool {
	if len(allowFailed) == 0 {
		return false
	}
	for _, stack := range process.ServiceStacks {
		if slices.Contains(allowFailed, stack.Name) {
			return true
		}
	}
	return false
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
