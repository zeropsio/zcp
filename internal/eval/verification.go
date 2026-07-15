package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
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
	return runVerificationWithObservation(ctx, sc, observation, httpDoer, retrospectiveText, runStart)
}

func runVerificationWithObservation(
	ctx context.Context,
	sc *Scenario,
	observation platformObservation,
	httpDoer ops.HTTPDoer,
	retrospectiveText string,
	runStart time.Time,
) []VerificationFinding {
	if sc == nil || sc.Verification == nil {
		return nil
	}
	verification := sc.Verification
	var findings []VerificationFinding

	if len(verification.ExpectedServices) > 0 {
		if observation.servicesErr != nil {
			findings = append(findings, VerificationFinding{
				Severity: "fail",
				Check:    "platform_query",
				Message:  fmt.Sprintf("ListServicesDirect failed: %v", observation.servicesErr),
			})
		} else {
			for _, expected := range verification.ExpectedServices {
				findings = append(findings, verifyExpectedService(ctx, expected, observation.services, httpDoer)...)
			}
		}
	}

	if verification.NoFailedProcesses {
		if observation.processesErr != nil {
			findings = append(findings, VerificationFinding{
				Severity: "warn",
				Check:    "no_failed_processes",
				Message:  fmt.Sprintf("GetProjectProcessesDirect failed: %v", observation.processesErr),
			})
		} else {
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
				findings = append(findings, VerificationFinding{
					Severity: "fail",
					Check:    "no_failed_processes",
					Message:  fmt.Sprintf("FAILED process %s: %s%s", process.ID, reason, serviceLabel),
				})
			}
		}
	}

	if len(verification.RetrospectiveMustNotMention) > 0 && retrospectiveText != "" {
		lower := strings.ToLower(retrospectiveText)
		for _, phrase := range verification.RetrospectiveMustNotMention {
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
	}
	return findings
}

// verifyExpectedService evaluates one ExpectedService assertion: hostname
// exists, status matches one of the allowed values, optional type-glob,
// optional subdomain HTTP probe.
func verifyExpectedService(
	ctx context.Context,
	exp ExpectedService,
	services []platform.ServiceStack,
	httpDoer ops.HTTPDoer,
) []VerificationFinding {
	var findings []VerificationFinding
	found := findServiceByHostname(services, exp.Hostname)
	if found == nil {
		return append(findings, VerificationFinding{
			Severity: "fail",
			Check:    "expected_service",
			Message:  fmt.Sprintf("service %q not found in project", exp.Hostname),
		})
	}
	if len(exp.Status) > 0 && !sliceContainsString(exp.Status, found.Status) {
		findings = append(findings, VerificationFinding{
			Severity: "fail",
			Check:    "service_status",
			Message:  fmt.Sprintf("service %q status %q not in %v", exp.Hostname, found.Status, exp.Status),
		})
	}
	if exp.Type != "" {
		if !matchTypeGlob(found.ServiceStackTypeInfo.ServiceStackTypeVersionName, exp.Type) {
			findings = append(findings, VerificationFinding{
				Severity: "fail",
				Check:    "service_type",
				Message:  fmt.Sprintf("service %q type %q does not match %q", exp.Hostname, found.ServiceStackTypeInfo.ServiceStackTypeVersionName, exp.Type),
			})
		}
	}
	if exp.SubdomainProbe != nil {
		findings = append(findings, probeSubdomain(ctx, exp, found, httpDoer)...)
	}
	return findings
}

// probeSubdomain runs an HTTP GET against the service's subdomain URL +
// optional path. Emits findings only when the probe fails the
// expectStatus rule or when the service has no subdomain enabled.
func probeSubdomain(
	ctx context.Context,
	exp ExpectedService,
	svc *platform.ServiceStack,
	httpDoer ops.HTTPDoer,
) []VerificationFinding {
	if !svc.SubdomainAccess {
		return []VerificationFinding{{
			Severity: "fail",
			Check:    "subdomain_access",
			Message:  fmt.Sprintf("service %q has no subdomain access enabled (cannot probe)", exp.Hostname),
		}}
	}
	// Subdomain URL convention: <hostname>-<projectSuffix>.<region>.zerops.app.
	// Operator-runtime resolves this via ${zeropsSubdomain} env var; from
	// the verifier we don't have that resolution (would require another
	// platform query). For now, treat probe failures as warn-only when we
	// can't form the URL and require the scenario to set the full URL
	// implicitly via env-vars or accept warn-on-missing.
	probe := exp.SubdomainProbe
	url := probeURLFromService(svc, probe.Path)
	if url == "" {
		return []VerificationFinding{{
			Severity: "warn",
			Check:    "subdomain_probe_url",
			Message:  fmt.Sprintf("service %q has subdomainAccess=true but URL not resolvable from ServiceStack fields", exp.Hostname),
		}}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return []VerificationFinding{{
			Severity: "warn",
			Check:    "subdomain_probe_build",
			Message:  fmt.Sprintf("build request for %s: %v", url, err),
		}}
	}
	resp, err := httpDoer.Do(req)
	if err != nil {
		return []VerificationFinding{{
			Severity: "fail",
			Check:    "subdomain_probe_http",
			Message:  fmt.Sprintf("GET %s: %v", url, err),
		}}
	}
	defer resp.Body.Close()
	if !statusMatches(resp.StatusCode, probe.ExpectStatus) {
		return []VerificationFinding{{
			Severity: "fail",
			Check:    "subdomain_probe_status",
			Message:  fmt.Sprintf("GET %s returned %d, expected %s", url, resp.StatusCode, probe.ExpectStatus),
		}}
	}
	return nil
}

// probeURLFromService derives the public subdomain URL from a ServiceStack
// when possible. ServiceStack itself doesn't carry the resolved subdomain
// — that lives in env vars (${zeropsSubdomain}). We can't form the URL
// from ServiceStack alone, so this returns "" — caller emits a warn finding.
//
// A future iteration can pass a resolved URL through the VerificationConfig
// (e.g. `subdomainProbe.url: ${zeropsSubdomain}` parsed at scenario load
// after services are up). For Sprint 3 the probe is informational: when
// the URL is unknowable the verifier flags warn, not fail.
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

// matchTypeGlob returns true when actual matches the glob pattern. Only
// the trailing `*` wildcard is supported — `postgresql@*` matches any
// version. Exact match required when no `*`.
func matchTypeGlob(actual, pattern string) bool {
	if prefix, hadStar := strings.CutSuffix(pattern, "*"); hadStar {
		return strings.HasPrefix(actual, prefix)
	}
	return actual == pattern
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

// WriteVerificationFindings serializes the findings list to verification.json
// in outDir. Empty findings still writes an empty array — operator-side
// tooling can distinguish "verification ran with no failures" from
// "verification didn't run" by the file's presence.
func WriteVerificationFindings(outDir string, findings []VerificationFinding) error {
	path := filepath.Join(outDir, "verification.json")
	if findings == nil {
		findings = []VerificationFinding{}
	}
	data, err := json.MarshalIndent(findings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal verification findings: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write verification.json: %w", err)
	}
	return nil
}
