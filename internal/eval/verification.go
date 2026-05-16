package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/zeropsio/zcp/internal/ops"
	"github.com/zeropsio/zcp/internal/platform"
)

// RunVerification evaluates the scenario's Verification block (if any)
// against the live project state BEFORE cleanup. Returns findings — empty
// slice when no Verification is configured or every check passed.
//
// Pure orchestration: every platform-side dependency is injected so the
// runner is fully unit-testable without spawning real client calls.
// Production wires client = the same platform.Client the agent used; the
// HTTP probe goes through ops.HTTPDoer (Runner.httpDoer).
//
// Severity policy: assertion failures emit "fail" severity; missing
// preconditions (no probe URL resolvable, e.g.) emit "warn". The runner
// captures all findings and writes them to verification.json; the suite
// verdict still propagates from the retrospective (warn-only at this
// stage — a later sprint may gate exit code on fail-severity findings).
func RunVerification(
	ctx context.Context,
	sc *Scenario,
	projectID string,
	client platform.Client,
	httpDoer ops.HTTPDoer,
	retrospectiveText string,
) []VerificationFinding {
	if sc == nil || sc.Verification == nil {
		return nil
	}
	v := sc.Verification

	var findings []VerificationFinding
	var services []platform.ServiceStack
	servicesNeeded := len(v.ExpectedServices) > 0 || v.NoFailedProcesses
	if servicesNeeded {
		var err error
		services, err = ops.ListProjectServices(ctx, client, projectID)
		if err != nil {
			return append(findings, VerificationFinding{
				Severity: "fail",
				Check:    "platform_query",
				Message:  fmt.Sprintf("ListProjectServices(%s) failed: %v", projectID, err),
			})
		}
	}

	for _, exp := range v.ExpectedServices {
		findings = append(findings, verifyExpectedService(ctx, exp, services, httpDoer)...)
	}

	if v.NoFailedProcesses {
		procs, err := client.SearchProcesses(ctx, projectID, 50)
		if err != nil {
			findings = append(findings, VerificationFinding{
				Severity: "warn",
				Check:    "no_failed_processes",
				Message:  fmt.Sprintf("SearchProcesses(%s) failed: %v", projectID, err),
			})
		} else {
			for _, p := range procs {
				if p.Status == "FAILED" {
					reason := p.ActionName
					if p.FailReason != nil && *p.FailReason != "" {
						reason = *p.FailReason
					}
					svcLabel := ""
					if len(p.ServiceStacks) > 0 {
						svcLabel = " on " + p.ServiceStacks[0].Name
					}
					findings = append(findings, VerificationFinding{
						Severity: "fail",
						Check:    "no_failed_processes",
						Message:  fmt.Sprintf("FAILED process %s: %s%s", p.ID, reason, svcLabel),
					})
				}
			}
		}
	}

	if len(v.RetrospectiveMustNotMention) > 0 && retrospectiveText != "" {
		lower := strings.ToLower(retrospectiveText)
		for _, phrase := range v.RetrospectiveMustNotMention {
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

// sliceContainsString is a generics-free Contains helper kept local to
// the verification package — the rest of the package targets Go 1.21+ but
// slice handling stays explicit to keep the diff small.
func sliceContainsString(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

// matchTypeGlob returns true when actual matches the glob pattern. Only
// the trailing `*` wildcard is supported — `postgresql@*` matches any
// version. Exact match required when no `*`.
func matchTypeGlob(actual, pattern string) bool {
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
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
