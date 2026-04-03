# Knowledge System Redesign v3 — Implementation Plan

**Date**: 2026-04-02
**Approach**: H5 (recipes + checkers, minimum rules)
**Principle**: Platform validates import. Recipes show correct patterns. Checkers catch what platform can't. Text rules only where no other enforcement exists.

---

## E2E Import API Verification Results (COMPLETED)

Tested 13 scenarios against live Zerops API. Results:

### Platform CATCHES (confirmed — DON'T duplicate in ZCP):

| Input | API Response |
|-------|-------------|
| `type: valkey@8` | `Service stack Type not found.` |
| `minContainers: 2` on postgresql | `Invalid parameter provided.` |
| `maxContainers: 5` on postgresql | `Invalid parameter provided.` |
| `verticalAutoscaling` on shared-storage | `Mandatory parameter is missing.` |
| `verticalAutoscaling` on object-storage | `Invalid parameter provided.` |
| hostname with hyphens (`test-app`) | `Service stack name is invalid.` |
| object-storage without `objectStorageSize` | `Mandatory parameter is missing.` |
| hostname 41+ chars | `Service stack name is invalid.` |

### Platform SILENTLY DROPS (needs ZCP checker):

| Input | API Behavior | Evidence |
|-------|-------------|----------|
| `envVariables` at service level | Import succeeds, env var NOT created | discover shows 0 TESTVAR; envSecrets DOES work |

### Platform ACCEPTS (valid behavior, not bugs):

| Input | API Behavior | Evidence |
|-------|-------------|----------|
| `mode: HA` on runtime | Accepted, forced to HA regardless | All 3 test services (no mode, HA, NON_HA) show mode: "HA" in API |
| `mode: NON_HA` on runtime | Accepted, forced to HA | Same as above |
| No `priority` on managed | Accepted, parallel creation | Both services created, no ordering guarantee |

### DOCS CORRECTIONS needed:

| Fact | Docs say | Reality (E2E verified) |
|------|----------|----------------------|
| Hostname max length | 25 chars | **40 chars** (41 rejected). JSON schema description says 25 but has no maxLength constraint. |
| mode on runtime | "NEVER set mode for runtime — meaningless" | **Accepted but forced to HA**. Runtime mode field is stored but overridden to HA. |
| envVariables at service level | "does NOT exist" | **API accepts but silently drops**. envSecrets works correctly. |

---

## Content changes

### universals.md → DELETE entirely

All 18 items are covered elsewhere (see line-by-line audit in conversation).
`GetUniversals()` will return model.md "## Platform Constraints" H2 section instead.

### model.md → ADD 2 sections + fixes

**Add:**
- `## Container Lifecycle` — deploy=new container (files lost), restart=same container (files intact), persistent data must be external
- `## Immutable Decisions` — hostname (max 40 chars), mode (HA/NON_HA, immutable for managed), bucket, service type

**Fix:**
- Scaling section: add "Runtime services are always HA — mode field on runtimes is forced to HA regardless of input"
- Hostname: update max from 25 to 40 in all mentions

### core.md → DELETE Rules & Pitfalls + Causal Chains entirely

All 35 rules covered elsewhere (line-by-line audit completed):
- 7 import rules → platform validates (E2E confirmed)
- 3 event rules → move to tool descriptions
- 25 remaining → covered by model.md, recipes, services.md, bases/, schema comments, flow docs

**Stays in core.md** (~200L):
- STOP warning + TL;DR + Keywords
- import.yml Schema (update hostname max to 40)
- zerops.yml Schema
- Schema Rules (deploy semantics, tilde, cache, public access, zsc)
- Multi-Service Examples

### services.md — no changes

### operations.md — already cleaned (v1)

### recipes — already have ## Gotchas (v1)

### bases/ — already updated (v1)

---

## Code changes

### 1. hostname.go: fix max length 25→40

```
hostname.go line 8: `^[a-z][a-z0-9]{0,24}$` → `^[a-z][a-z0-9]{0,39}$`
hostname.go line 11: "max 25 chars" → "max 40 chars"
hostname.go line 16: "1-25 lowercase" → "1-40 lowercase"
hostname_test.go: update test cases for 26-40 valid, 41+ invalid
```

### 2. import.go: envVariables-at-service-level checker

```go
// In Import(), after ValidateServiceTypes:
for _, svc := range services {
    hostname, _ := svc["hostname"].(string)
    if _, has := svc["envVariables"]; has {
        warnings = append(warnings, fmt.Sprintf(
            "service %q: 'envVariables' at service level is silently dropped by the API. "+
            "Use 'envSecrets' for import-time secrets, or zerops.yml run.envVariables for runtime config.",
            hostname))
    }
}
```

### 3. engine.go: GetModel() on Provider interface

```go
GetModel() (string, error) // returns themes/model.md content
```

### 4. guidance.go: discover step model injection

```go
if params.Step == StepDiscover {
    if model, err := params.KP.GetModel(); err == nil && model != "" {
        parts = append(parts, model)
    }
}
```

### 5. tools/knowledge.go: scope=infrastructure includes model

```go
if model, mErr := store.GetModel(); mErr == nil {
    result = model + "\n\n---\n\n" + result
}
```

### 6. GetUniversals() refactor

Change to extract "## Platform Constraints" H2 section from model.md instead of reading universals.md.
OR: keep universals.md as tiny file with just the constraints, sourced from model.md section.

### 7. events.go: move 3 Event Monitoring rules to tool description

```
- Filter zerops_events by serviceHostname
- Stop polling after stack.build FINISHED
- Check stack.build process, not appVersion
```

### 8. core.md schema comments: update hostname max

```
hostname: string  # REQUIRED, max 40, a-z and 0-9 ONLY
```

---

## Checker summary (final)

### Existing (already work):
- Env var refs match discovered vars
- deployFiles present
- setup: matches hostname

### New (3 items, ~20 LOC):
1. envVariables at service level in import → warning (silently dropped)
2. /var/www in prepareCommands → error (runtime file not found)
3. php-nginx/php-apache as build.base → error (unknown base, explain why)

### Considered but rejected (platform handles):
- valkey version, minContainers on managed, verticalAutoscaling on storage, hostname format, port range — all caught by API with clear error messages

### Considered but low-value:
- initCommands package install → warning only, not critical, recipes show correct pattern

---

## Implementation sequence

```
Phase 1: Content restructure
  1. Add Container Lifecycle + Immutable Decisions to model.md (fix hostname 40, runtime HA note)
  2. Delete universals.md (or reduce to pointer)
  3. Delete Rules & Pitfalls + Causal Chains from core.md
  4. Update core.md import.yml schema (hostname max 40)
  5. Move Event Monitoring rules to events.go tool description

Phase 2: Code changes (TDD)
  6. Fix hostname.go regex {0,24} → {0,39}
  7. Add envVariables-at-service-level warning in import.go
  8. Add /var/www prepareCommands checker in checkGenerate
  9. Add php-nginx build.base checker in checkGenerate
  10. Add GetModel() to Provider + Store
  11. Add discover step model injection in guidance.go
  12. Update scope=infrastructure to include model
  13. Refactor GetUniversals() (extract from model.md or delete universals.md)

Phase 3: Tests
  14. hostname_test.go: update for 40 char limit
  15. import_test.go: envVariables warning test
  16. workflow_checks_generate_test.go: prepareCommands + build.base tests
  17. guidance_test.go: discover model injection test
  18. knowledge_test.go: scope includes model test
  19. store_access_test.go: mock updates

Phase 4: Validation
  20. go test ./... -count=1 -short (all packages)
  21. make lint-fast
  22. Manual: verify each knowledge mode output
```

---

## Decisions from E2E

| Decision | Evidence |
|----------|---------|
| Don't duplicate import validation in ZCP | E2E: platform catches 7/7 tested invalid configs with clear error messages |
| DO check envVariables at service level | E2E: platform silently drops — agent gets no error, service has no env var |
| Hostname max is 40 not 25 | E2E: 40 chars accepted, 41 rejected. JSON schema description wrong. |
| Runtime mode always HA | E2E: all runtimes forced to HA regardless of mode field value |
| envVariables at service level is silently dropped | E2E: import succeeds, discover shows no env var. envSecrets works. |
