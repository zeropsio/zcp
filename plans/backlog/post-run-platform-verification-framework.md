**Surfaced**: 2026-05-16 — Karel's real-life behavioral test request. Current `eval/behavioral/flow-eval.sh` framework runs agent → retro → cleanup. The retrospective is the only outcome signal; agent self-reports success. Karel kanban scenario #1 + #2 both showed agents reporting "Kanban je live, here's the URL", but the cleanup hook deleted services BEFORE Karel could manually verify the URL responds + the app actually works. Test "passed" (exit 0 from `flow-eval.sh`) but the FRAMEWORK didn't verify outcome — agent's self-report was taken at face value.

**Why deferred**: framework change (Go code in `internal/eval/behavioral_run.go` + scenario YAML schema). Karel asked for real-life behavioral coverage immediately; first iteration uses agent self-report + manual retro review. Programmatic post-run verification is the second iteration. Phase-2-push isn't blocked on this (the 4 Go live tests already verify code-shape correctness with assertions); this is for hardening the behavioral matrix Karel asked to build out.

**Trigger to promote**: a scenario "passes" but the underlying outcome is broken (agent reports done, service is FAILED on platform). Or: Karel manually runs matrix nightly and wants programmatic pass/fail without reading 12 retros.

---

## Current framework shape

`eval/behavioral/flow-eval.sh <scenario>`:

```
build + deploy zcp binary to container
scp scenario yaml
ssh "zcp eval behavioral run --id <scenario>"
  → seed (delete user services in eval-zcp)
  → spawn claude headless agent with task prompt
  → user-sim loop (per persona)
  → retrospective phase (separate claude call writes self-review.md)
  → CLEANUP (delete user services in eval-zcp again)
scp suite artifacts back
print self-review path
```

Pass/fail signal: shell exit code (0 if scp succeeded). No platform-side verification.

## What's missing

After retrospective, BEFORE cleanup, framework should:

1. **Query platform via real API** for each expected service.
2. **Assert per-service status** matches expected (ACTIVE, READY_TO_DEPLOY, FAILED).
3. **HTTP probe** subdomain URLs if `subdomainExpected: true`.
4. **Scan audit log** for token-leak sentinels.
5. **Build pass/fail verdict** combining retro + verification.
6. **THEN** cleanup runs.

Failure cases the verification catches that retro doesn't:
- Agent reports success but service is FAILED on platform (build pipeline broke)
- Agent reports subdomain URL but HTTP returns 502
- Agent classified a sensitive value as plain-config (token leak)
- Agent skipped a required hostname (missing service)

## Scenario YAML schema extension

Add `verification:` block to scenario frontmatter:

```yaml
verification:
  expectedServices:
    - hostname: appdev
      status: [ACTIVE]
      subdomainProbe:
        path: /
        expectStatus: 2xx
    - hostname: db
      status: [ACTIVE]
      type: postgresql@*  # version-loose match
  noFailedProcesses: true
  noTokenLeak:
    sentinels: [ghp_, ZCPCleanup-Sentinel]  # values that must NEVER appear in audit log
  retrospectiveMustMention: []  # optional: retro must contain certain phrases
  retrospectiveMustNotMention: [smuggled, hand-edited]  # red-flag phrases
```

## Implementation

```go
// internal/eval/behavioral_run.go (after retrospective, before cleanup)

func runVerification(ctx context.Context, sc *Scenario, projectID string, client platform.Client) []VerificationFinding {
    var findings []VerificationFinding
    if sc.Verification == nil {
        return nil
    }
    services, _ := client.ListServices(ctx, projectID)
    for _, exp := range sc.Verification.ExpectedServices {
        found := findByHostname(services, exp.Hostname)
        if found == nil {
            findings = append(findings, VerificationFinding{
                Severity: "fail",
                Message:  fmt.Sprintf("expected service %q not found", exp.Hostname),
            })
            continue
        }
        if !matchAny(found.Status, exp.Status) {
            findings = append(findings, VerificationFinding{
                Severity: "fail",
                Message:  fmt.Sprintf("service %q status=%q expected %v", exp.Hostname, found.Status, exp.Status),
            })
        }
        if exp.SubdomainProbe != nil {
            if !found.SubdomainAccess {
                findings = append(findings, VerificationFinding{Severity: "fail", Message: fmt.Sprintf("service %q has no subdomain", exp.Hostname)})
                continue
            }
            // HTTP probe via project's subdomain URL pattern
            url := fmt.Sprintf("https://%s-%s.prg1.zerops.app%s", exp.Hostname, projectSubdomainSuffix(projectID), exp.SubdomainProbe.Path)
            resp, err := httpClient.Get(url)
            // ... assert
        }
    }
    if sc.Verification.NoFailedProcesses {
        procs, _ := client.SearchProcesses(ctx, projectID, 50)
        for _, p := range procs {
            if p.Status == "FAILED" {
                findings = append(findings, VerificationFinding{Severity: "fail", Message: fmt.Sprintf("FAILED process: %s on %s", p.ActionName, p.ServiceStackName)})
            }
        }
    }
    if sc.Verification.NoTokenLeak != nil {
        auditPath := filepath.Join(stateDir, "launch-production", "launch-audit-log.json")
        if data, err := os.ReadFile(auditPath); err == nil {
            for _, sentinel := range sc.Verification.NoTokenLeak.Sentinels {
                if bytes.Contains(data, []byte(sentinel)) {
                    findings = append(findings, VerificationFinding{Severity: "fail", Message: fmt.Sprintf("token-leak sentinel %q found in audit log", sentinel)})
                }
            }
        }
    }
    return findings
}
```

Findings get written to `verification.json` alongside `self-review.md`. Suite verdict combines:

- retrospective fired without error
- AND no `severity: fail` findings

Exit code propagates accordingly.

## Out-of-scope here

- **Long-running probe** (e.g. "wait 5 min for build to complete then probe") — current scenarios are 8-15 min wall clock; build is usually done by retro time. If not, framework needs `waitForActive` flag with timeout.
- **Multi-project scenarios** — launch-production creates a SECOND project. Verification needs to query that project too. Currently scenarios target eval-zcp only.
- **Cleanup recovery** — if verification fails AND cleanup also fails, residue accumulates. Need explicit retry / dashboard-fallback prose.

## Migration path

1. Add scenario YAML schema field + parser (~30 LOC)
2. Add verification runner (~150 LOC)
3. Wire into RunBehavioralScenario between retrospective + cleanup (~20 LOC)
4. Update 1-2 existing scenarios with `verification:` blocks as proof
5. Document in `eval/behavioral/README.md`

Total ~250 LOC. Test by running migrated scenarios + asserting verification.json shape.
