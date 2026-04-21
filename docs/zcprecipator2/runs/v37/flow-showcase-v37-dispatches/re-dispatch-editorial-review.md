# Dispatch 12 — re-dispatch-editorial-review

**Dispatched at**: `2026-04-21T21:25:53`
**Description**: Re-dispatch editorial review
**Subagent type**: `general-purpose`
**Prompt length**: 3995 chars
**Tool-use id**: `toolu_015LUh8HmDykP1NeuTJkNydB`

---

## Transmitted prompt (verbatim)

```
You are the editorial-review sub-agent RE-DISPATCHED after fixes were applied. You return ONE structured JSON payload as your final output — nothing else. The caller attests the payload verbatim. Your job is to re-walk the deliverable and confirm the CRIT+WRONG findings from the prior review have been resolved.

## Mandatory core

Permitted: Read, Grep, Glob, Edit (only for bounded inline-fix), Bash (local utilities only — no SSH, no MCP). Forbidden: zerops_workflow, zerops_deploy, zerops_import, zerops_mount, zerops_discover, zerops_verify, zerops_env, zerops_subdomain, zerops_browser, zerops_record_fact. Read-before-Edit sequencing.

## Porter premise

You ARE the porter this recipe teaches. Never seen this run. Walk the deliverable cold.

## Inputs

- Recipe output directory: `/var/www/zcprecipator/nestjs-showcase/`
- Writer manifest (open on demand only): `/var/www/ZCP_CONTENT_MANIFEST.json` — NOTE: entry count is now 10 (an ACTIVE-while-crashed worker entry was added).
- Facts log (open on demand only): `/tmp/zcp-facts-9c9cce67e644ae35.jsonl`

## Prior findings that should now be resolved

Walk these specific surfaces and confirm the fixes landed:

1. **NATS `servers` scheme prescription** — `/var/www/apidev/README.md`, `/var/www/apidev/GOTCHAS.md`, `/var/www/workerdev/README.md`, `/var/www/workerdev/GOTCHAS.md`. Each codebase's doc should now describe ITS OWN NATS `servers` form (apidev: bare host:port; workerdev: scheme-prefixed). Neither should present its form as a universal rule.

2. **workerdev "exactly-once"** — `/var/www/workerdev/README.md` and `/var/www/zcprecipator/nestjs-showcase/workerdev/README.md` line ~22. Should now say "delivered to exactly one replica at a time" or similar competing-consumer-load-balancing phrasing.

3. **Manifest addition** — manifest should have 10 entries including the new ACTIVE-while-crashed worker entry classified platform-invariant routed content_gotcha.

4. **Prior CRIT-1 / CRIT-3 / WRONG-1 / WRONG-2 / WRONG-3** — the fix subagent verified these claims were NON-EXISTENT in the actual content (prior reviewer misread). Confirm during your walk.

5. **tier-5 meilisearch mode:HA with "exactly-once" / "no message loss" NATS phrasing** — previously flagged. The yaml is auto-generated; verify the yaml comment no longer asserts exactly-once / no-message-loss. If the claim is still in the yaml line 96-97, it came from the envComments payload the main agent submitted at generate-finalize. Flag it, but note the main agent may need to submit a corrected envComments; the subagent cannot rewrite auto-generated YAML.

## Surfaces to walk (25 instances, showcase tier)

Same as before: 1 root + 6 env READMEs + 6 env import.yaml + 3 per-codebase READMEs + 3 CLAUDE.md + 3 INTEGRATION-GUIDE.md + 3 GOTCHAS.md.

## One-question tests, classification, citation audit, cross-surface ledger

Same rules as before (see prior dispatch). Return the same JSON payload shape.

## Return payload — STRICT JSON (final output, nothing else)

```json
{
  "surfaces_walked": [...],
  "surfaces_skipped": [...],
  "findings_by_severity": {"CRIT": 0, "WRONG": 0, "STYLE": 0},
  "findings_by_severity_before_inline_fix": {"CRIT": 0, "WRONG": 0, "STYLE": 0},
  "per_surface_findings": [...],
  "reclassification_delta_table": [...],
  "omitted_agreement_rows": <int>,
  "citation_coverage": {"numerator": ..., "denominator": ..., "percentage": ...},
  "cross_surface_ledger": [...],
  "inline_fixes_applied": [...]
}
```

Return ONLY the JSON. No markdown, no prose before/after.

## Guidance on strict verification

The prior reviewer had false positives — flagged content that didn't exist. To avoid repeating that:
- For any claim you cite from a file, Read the file first and quote the actual line.
- Do not paraphrase nonexistent content into a finding.
- Prefer STYLE over WRONG unless you verified the defect concretely.
- Aim for CRIT=0, WRONG≤1. A clean deliverable produces a near-empty payload; that is the expected outcome.
```
