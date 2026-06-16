# Deep research: atoms / templates findings A, B, E

Investigated 2026-05-18 against `main @ 9186712a`. Each finding verified
against source + live transcript; root cause confirmed; blast radius mapped.

---

## Finding A — `ToolSearch select:` bare-name form fails

### Root cause (verified)

Three atoms document the bare-name form `select:zerops_workflow,...`:
`internal/content/atoms/idle-tool-preload.md:17`,
`internal/content/atoms/bootstrap-tool-preload.md:16`,
`internal/content/atoms/develop-tool-preload.md:16`. The Claude Code
deferred-tool registry requires the **MCP-fully-qualified name**
(`mcp__<server>__<tool>`). Container init writes the MCP server name as
`zerops` (`internal/content/templates/mcp-config.json:3`), so the only
form the harness recognises is `select:mcp__zerops__zerops_workflow,...`.

Verified live: in `eval/behavioral/runs/20260518-132736/develop-loop-after-bootstrap/transcript.jsonl`:

- Line tool-use `toolu_01R8EXqF6SPTSdMJb55kgKJN`:
  `query="select:zerops_import,zerops_discover,zerops_process,..."`
  → response `"No matching deferred tools found"`, `matches: []`.
- Immediate retry with prefix
  `toolu_01F4CmSqtZkRPEPGP5xg2EJi`:
  `query="select:mcp__zerops__zerops_import,..."`
  → succeeds, returns all 8 schemas in one round-trip.

Bug introduced 2026-05-06 in commit `3cfa4c94` (bootstrap+develop atoms);
duplicated 2026-05-16 in `02f374f5` (idle atom); never worked. The
synthesis text says `mcp__zcp__zerops_*` — that's the *local-dev* prefix
(`.mcp.json:3` names the server `zcp`); the *container* prefix is
`mcp__zerops__` because container init uses the `zerops` mcp-config
template. Atoms are gated `environments: [container]`, so the
canonical prefix is `mcp__zerops__`.

### Parallel paths checked

- **Local-dev preload** — does NOT exist. The three atoms gate on
  `environments: [container]` only (verified in each atom's frontmatter),
  so no parallel local-dev variant to bring to parity.
- **Recipe sub-agent prose** —
  `internal/recipe/briefs_subagent_prompt.go:411` already uses
  `mcp__zerops__*` (the correct form), so the bug is isolated to the
  three preload atoms + their pinned goldens.
- **Plan-doc drafts** —
  `plans/archive/path-to-everything-tested-2026-05-16.md:110` already documents
  the correct `select:mcp__zerops__zerops_workflow,...` form. Author had
  the right syntax in plan but the atoms shipped the wrong one.
- **Other guidance surfaces** — grep across `internal/content/` for
  `select:zerops_` returns only the three atoms (+ goldens). No
  `internal/content/workflows/` or response prose uses the bare form.

### Blast radius

| Layer | Files | Notes |
|---|---|---|
| Atoms | `idle-tool-preload.md:17`, `bootstrap-tool-preload.md:16`, `develop-tool-preload.md:16` | Add `mcp__zerops__` prefix to each tool in `select:` |
| Goldens | ~22 files under `internal/workflow/testdata/atom-goldens/{idle,bootstrap,develop}/` | All re-rendered automatically by the golden-update test |
| Tests | `TestScenario_S1_*` / `TestScenario_S4_*` in `internal/workflow/scenarios_test.go` | Only assert atom-ID presence, not literal text — pass unchanged |

### Fix shape

Rewrite the three atom select-blocks to use `mcp__zerops__` prefix:

```
ToolSearch query="select:mcp__zerops__zerops_workflow,mcp__zerops__zerops_discover,mcp__zerops__zerops_import,..."
```

Apply to all three atoms (idle / bootstrap / develop). Regenerate
goldens. Pin with a unit test that all three atoms' `select:` blocks
contain the `mcp__zerops__` prefix (lint, not golden).

### Risks / non-obvious

- `mcp__zerops__` is correct in container (the only env these atoms fire
  in), but if zcp later supports running on a host with a different mcp
  server name (e.g. local `mcp__zcp__`), the same line breaks. **Don't**
  add a local-env parallel atom with `mcp__zcp__` — the atoms are
  `environments: [container]` and there's no preload need locally
  (CLAUDE Code on the dev mac loads schemas eagerly per `.mcp.json`).
- Atoms include the full literal string. Avoid template substitution
  of the server name — adds a render-time dependency without buying
  flexibility, and the prefix is stable for containers.

### Verification test

New unit test in `internal/content/atoms_lint_test.go`:
`TestToolPreloadAtoms_UseFullyQualifiedMCPName` — load each of the three
preload atoms, parse the `select:` query, assert every comma-separated
entry has prefix `mcp__zerops__`. Fail-fast on regression.

---

## Finding B — `/var/www/{hostname}/` mount-claim lie

### Root cause (verified)

`internal/content/templates/claude_container.md:3` asserts
unconditionally: **"Service code SSHFS-mounted at `/var/www/{hostname}/`;
mount IS the service's runtime filesystem."** This text is frozen into
the orchestrator's `CLAUDE.md` at `zcp init` time
(`internal/content/build_claude.go:32-36`), so every fresh agent reads
it on turn 1.

The actual mechanism: mounts are auto-triggered ONLY after a successful
`zerops_workflow action=complete step=provision` call
(`internal/tools/workflow_bootstrap.go:78-81` →
`autoMountTargets` at line 79). Before that — empty project, mid-bootstrap,
post-compaction without re-mount, or after a destructive override — the
path `/var/www/{hostname}/` does not exist. Agents reading the boot shim
treat the path as ambient infrastructure and try `ls /var/www/appdev/`
expecting source code; they see nothing or `CLAUDE.md` only.

Verified live: `eval/behavioral/runs/20260518-144307/cross-deploy-stage-promote-from-dev/transcript.jsonl`
runs 4 distinct `ls /var/www[...]` probes before realising the source
isn't there. Same pattern across 4 of 17 sessions per synthesis (`cross-deploy`,
`launch-laravel`, `launch-from-standard-pair`, `resume-after-compaction`).

Last touched 2026-05-04 in commit `9c9d0547`; **no commit in the
2026-05-16/-18 fix window addresses it.**

### Parallel paths checked

- **`claude_local.md` template** — verified does NOT make this claim;
  `internal/content/build_claude_test.go:78` actively forbids
  `/var/www/` substring in the local CLAUDE.md. So the lie is
  container-only.
- **Recipe workflow prose** (`internal/content/workflows/recipe.md`)
  — references `/var/www/{hostname}/` in 14+ places but **always**
  scoped to recipe-author guidance ("when scaffolding", "if you SSH
  into the dev service"), not as a passive precondition the way the
  shim does. Different surface; not in scope for this fix.
- **`develop-platform-rules-local.md:48`** — accurately says "No SSHFS,
  no `/var/www/{hostname}` mount — that shape is container-only". The
  local-env contrast is correct.
- **`develop-first-deploy-write-app.md:18`** — has a **conditional**:
  "If `ls` errors (stale SSHFS), run `zerops_mount action=mount` to
  recover". This is the right shape; the shim should match this
  pattern.
- **Mount-availability state** — synthesis RC#2 sketch proposes
  `sourceMountAvailable` field. Grep confirms it does NOT yet exist
  in `internal/workflow/`, `internal/ops/`, or `internal/tools/`.
  `workflow.AutoMountInfo` exists (`bootstrap.go:100-106`) and could
  feed a `sourceMountAvailable` derivation, but no consumer reads it
  outside the immediate post-provision response.

### Blast radius

| Layer | Files | Notes |
|---|---|---|
| Truth source | `internal/content/templates/claude_container.md:3` | Replace unconditional with conditional shape |
| Boot-shim test | `internal/content/build_claude_test.go:29` | Currently pins literal `/var/www/{hostname}/` substring. Survives. Add new assertion that the conditional framing is present (e.g. "after bootstrap" or "if `ls` empty"). |
| Discover response | `internal/ops/discover.go` + per-service field — could carry `sourceMountAvailable` derived from `os.Stat(mountPath)` or from `meta.IsComplete()` + mount fact | Optional, **structural** fix |
| Dependent atoms (consume the shim implicitly) | `export-classify-envs.md` lines 69-78 (`rg`-over-source tables), `bootstrap-env-var-discovery.md`, `launch-classify-platform-envs.md` | Add fallback branch: "if mount unavailable, classify by name + surface rationale" |
| Mount recovery atom | `develop-first-deploy-write-app.md:18` already has the pattern | Extract into a primitive atom; have other atoms reference it |

### Fix shape

Minimum (symptom-only): rewrite shim line 3 to
"Service code SSHFS-mounted at `/var/www/{hostname}/` **after the
service has been bootstrapped or adopted**; mount IS the service's
runtime filesystem when present. Before that, `/var/www/{hostname}/`
does not exist — run `workflow=bootstrap route=adopt` (existing service)
or wait until provision-step closes (in-flight bootstrap)."

Structural (per prior RC#2 sketch): add `sourceMountAvailable: bool`
to discover-response per-service block + classify-prompt response
envelope. Rewrite dependent atoms to lead with the no-mount path
("classify by name; grep-fallback if mount available"). One source of
truth (the bool); all downstream atoms read it.

Recommended: structural — the boot-shim rewording alone leaves classify
atoms reading the lie indirectly. The `sourceMountAvailable` field is
~20 lines of code and unbreaks 4-of-17 sessions.

Invariant restored: **One signal for mount availability; agents never
probe with `Glob`/`ls` to discover environmental preconditions.**

### Risks / non-obvious

- The shim is rendered at `zcp init` time and frozen into CLAUDE.md
  (`refresh_claude.go` re-renders on `zcp serve` but only between marked
  block). A long-lived orchestrator install that pre-dates the fix
  needs `zcp init` re-run, or the refresh path must touch the marker.
  Confirm `RefreshClaudeMD` re-renders this section.
- Atoms that embed `rg`-tables become longer if both branches are
  inline. Consider an indirection atom `bootstrap-mount-availability.md`
  that classify atoms reference, instead of duplicating the conditional
  in three places.

### Verification test

- `TestClaudeContainerShim_MountClaimIsConditional` —
  `BuildClaudeMD(rt{InContainer: true})` output MUST contain both
  the path `/var/www/{hostname}/` AND a conditional marker like
  "after bootstrap" / "if `ls` empty" / "when present". Existing
  `TestBuildClaudeMD_Container_HasContainerFacts` keeps the
  path-substring assertion.
- `TestDiscoverResponse_CarriesSourceMountAvailable` (if structural
  fix taken) — discover with a bootstrapped service returns
  `sourceMountAvailable: true`; with a never-mounted service returns
  `false`.

---

## Finding E — Static `build.base: nginx@*` rejection — root is RuntimeClassFor bug, not atom timing

### Root cause (verified)

**Synthesis attribution is incomplete.** Synthesis attributes the
failure to "atom gated on `phases:[develop-active]+runtimes:[static]`;
agent wrote yaml before static-runtime classifier triggered." The
actual root cause is one layer deeper: **`topology.RuntimeClassFor`
does not strip the OS prefix from service type-versions.**

`internal/topology/runtime_class.go:31` checks
`strings.HasPrefix(lower, "static") || strings.HasPrefix(lower, "nginx")`.
When the import-yaml uses the canonical OS-prefixed form
`alpine/nginx@1.22` (which the live Zerops schema accepts and emits
back via `ServiceStackTypeInfo.ServiceStackTypeVersionName`), the
prefix check fails and the classifier returns `RuntimeDynamic`.

Direct empirical verification (ran in-package test against current
classifier):

```
alpine/nginx@1.22              -> dynamic    (expected: static)
ubuntu/nginx@1.22              -> dynamic    (expected: static)
alpine/php-apache@8.4          -> dynamic    (expected: implicit-webserver)
alpine/php-nginx@8.4           -> dynamic    (expected: implicit-webserver)
ubuntu/php-nginx@8.4           -> dynamic    (expected: implicit-webserver)
alpine/static@latest           -> dynamic    (expected: static)
nginx@1.22                     -> static     (correct — no OS prefix)
static                         -> static     (correct)
php-nginx@8.4                  -> implicit-webserver  (correct)
```

Existing classifier test `internal/topology/runtime_class_test.go:5-35`
only exercises bare forms (no OS prefix); the live shape is never
tested. So the bug has been latent since at least the schema's OS-prefix
era — any `alpine/<X>` or `ubuntu/<X>` service has been mis-classified
since.

Consequence chain:
1. Agent submits import-yaml with `alpine/nginx@1.22`. Service is
   provisioned.
2. `workflow.ComputeEnvelope` calls `client.ListServices`
   (`internal/workflow/compute_envelope.go:55`), gets back
   `ServiceStackTypeVersionName="alpine/nginx@1.22"`.
3. `buildOneSnapshot` (line 223) passes that string to
   `topology.RuntimeClassFor` → returns `RuntimeDynamic`.
4. Develop-active envelope has `svc.RuntimeClass = dynamic` for the
   nginx service. Atom `develop-static-workflow.md` (gated
   `runtimes:[static]`, enforced by
   `synthesize.go:383`) does NOT fire — its `build.base` warning
   never reaches the agent.
5. Agent writes yaml with `build.base: nginx@1.22` (the obvious
   choice given run.base is `nginx@1.22`), `zerops_deploy` returns
   `INVALID_ZEROPS_YML: unknown base nginx@1.22`. Confirmed in
   `eval/behavioral/runs/20260518-145140/classic-static-nginx-simple/transcript.jsonl:51`.

Karel's commit `2b896edc` (atom rewrite leading with the
counter-intuition banner) was the correct *content* but on the wrong
*delivery axis* — the atom itself never fans out, regardless of how
prominently the banner is positioned inside it.

### Parallel paths checked

- **`bootstrap-classic-plan-static.md`** — previously had `runtimes:[static]`
  (verified via `git log`); removed in commit `bff507f2` per review-ledger
  A2-L7 ("atoms NEVER FIRE in any of the 30 scenarios — service-scoped
  axis can't fire pre-import"). The atom now fires correctly at
  classic+discover, but its body does NOT include the `build.base`
  warning — only describes hostname/mode choice. So this atom isn't a
  fallback delivery channel.
- **`develop-first-deploy-scaffold-yaml.md`** — universal scaffold-yaml
  atom; fires for every never-deployed service regardless of runtime.
  Body mentions `base: <runtime-only key, e.g. nodejs@22 — NOT the
  composite run key>` but nothing about static-specific rejection.
- **Implicit-webserver atom** (`runtimes:[implicit-webserver]`-gated)
  — exists for php-apache/php-nginx, suffers the **same classifier
  bug**: any `alpine/php-nginx@8.4` service mis-classifies as
  `dynamic`, so any `implicit-webserver` atom never fires for OS-prefixed
  php services. PARITY ISSUE.
- **Deploy classifier site** (`internal/tools/deploy_poll.go:227`,
  `internal/tools/deploy_subdomain.go:118`, `internal/tools/subdomain.go:146`)
  — all four consumers of `RuntimeClassFor` swallow the same mis-
  classification. Most-impacted: `deploy_subdomain.go::serviceEligible`
  (subdomain auto-enable) uses `IsDeferredStart(mode, class)`, which
  returns false for `RuntimeDynamic` only when mode != dev/standard.
  Effect: nginx static service in dev mode might incorrectly enter
  the deferred-start branch (treated as dynamic) and behave
  differently than expected on auto-enable / probe.

### Blast radius

| Layer | Files | Notes |
|---|---|---|
| Classifier | `internal/topology/runtime_class.go:20-35` | Strip OS prefix before checking prefixes |
| Classifier test | `internal/topology/runtime_class_test.go:5-35` | Extend with OS-prefixed cases — `alpine/nginx@1.22`, `ubuntu/php-apache@8.4`, `alpine/static@1.0`, `alpine/nodejs@22` (controls dynamic stays dynamic) |
| Consumers (no code change, but behavior changes) | `compute_envelope.go:223`, `deploy_poll.go:227`, `deploy_subdomain.go:118`, `subdomain.go:146` | All automatically correct once classifier fixed |
| Synthesis golden tests | None — none of the 30 scenario goldens use OS-prefixed types | Likely a gap; consider adding |
| Atoms | None — atoms keep their `runtimes:[static]` / `runtimes:[implicit-webserver]` gating | Atom delivery now actually works |

### Fix shape

Single-site classifier patch in `internal/topology/runtime_class.go`:

```go
func RuntimeClassFor(typeVersion string) RuntimeClass {
    if typeVersion == "" {
        return RuntimeUnknown
    }
    if IsManagedService(typeVersion) {
        return RuntimeManaged
    }
    lower := strings.ToLower(typeVersion)
    // Strip OS prefix (alpine/, ubuntu/) — Zerops returns these on the
    // ServiceStackTypeVersionName for runtime services. Managed services
    // never carry an OS prefix in their type-version (postgresql@18,
    // valkey@7.2, …) — see import_yml_schema.json — so the strip is
    // unambiguous after the IsManagedService check.
    if idx := strings.Index(lower, "/"); idx >= 0 {
        lower = lower[idx+1:]
    }
    if strings.HasPrefix(lower, "php-apache") || strings.HasPrefix(lower, "php-nginx") {
        return RuntimeImplicitWeb
    }
    if strings.HasPrefix(lower, "static") || strings.HasPrefix(lower, "nginx") {
        return RuntimeStatic
    }
    return RuntimeDynamic
}
```

Invariant restored: **Classifier is OS-prefix-agnostic. `alpine/<X>` /
`ubuntu/<X>` / bare `<X>` all map to the same RuntimeClass.**

After this fix, `develop-static-workflow.md` fans out as designed in
every static-runtime session, regardless of which OS variant the user
picked. The `2b896edc` content fix (banner-first) then actually reaches
the agent before yaml authoring.

### Risks / non-obvious

- The strip-once-on-first-slash assumes no managed service ever has a
  slash in its type-version. Verified via `import_yml_schema.json` —
  managed enums (`postgresql@*`, `valkey@*`, …) are slash-free. The
  `IsManagedService` check runs BEFORE the strip, so even if a managed
  type one day gained a prefix, the managed-classification path still
  fires.
- `nginx@latest` and `static@latest` remain valid. `nginx` and `static`
  bare forms (no version) — accepted by both schema and classifier.
- The classifier is `topology/` (Layer 2). Per architecture rule, a
  Layer-2 change ripples to every Layer-3 consumer; no Layer-3 file
  changes shape, but their *behaviour* shifts. Pin the consumer behaviour
  with at least one envelope test that uses an OS-prefixed nginx service
  and asserts the static-workflow atom fires.

### Verification test

- Extend `TestRuntimeClassFor` in
  `internal/topology/runtime_class_test.go` with table rows:
  ```go
  {"alpine/nginx@1.22", RuntimeStatic},
  {"ubuntu/nginx@1.22", RuntimeStatic},
  {"alpine/static@1.0", RuntimeStatic},
  {"alpine/php-nginx@8.4", RuntimeImplicitWeb},
  {"ubuntu/php-apache@8.4", RuntimeImplicitWeb},
  {"alpine/nodejs@22", RuntimeDynamic}, // control — stays dynamic
  ```
- New scenario-test fixture in
  `internal/workflow/scenarios_test.go`: `S?_StaticNginxFirstDeploy`
  with `TypeVersion: "alpine/nginx@1.22"` + envelope-deploy-state
  `never-deployed`; assert `develop-static-workflow` is in the
  synthesized atom list.

---

## Summary

- **E (priority 1).** Classifier bug is one-site, ~5-line fix that
  unblocks the static + implicit-webserver atom-delivery axis platform-
  wide. Two of 17 sessions in the subset hit the static failure; an
  unknown larger share hit silent php mis-classification (subdomain
  auto-enable, dev-server probe). Highest leverage per LoC; structural
  win because every `RuntimeClassFor` consumer benefits automatically.
- **A (priority 2).** Cosmetic but high-frequency — every fresh
  bootstrap session burns one tool-call round-trip on the bare-name
  rejection. Self-reviews don't flag it because agents recover in 1
  turn, but transcripts show the dead-end-then-recover dance every
  time. Three atom rewrites + one lint test.
- **B (priority 3, two-tier).** Highest-frequency *visible* friction
  (4-of-17 sessions). Tier-1 minimum (shim rewording, ~5 lines) ships
  same day. Tier-2 structural (`sourceMountAvailable` boolean +
  classify-atom rewrite) is the prior RC#2 sketch — ~20 LoC code + 3
  atom rewrites — and unbreaks the dependent classify atoms that
  silently assume the lie.
