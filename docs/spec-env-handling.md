# ZCP Env Handling Specification

> **Status**: Authoritative — all env-rendering code, atoms, and tooling
> MUST conform to this document.
> **Scope**: Env state convergence across project / zerops.yaml / local
> overlay sources to one or more rendered sinks. Primary user-visible
> sink is local-mode `.env`; same primitive serves container-mode review,
> CI export, env-promotion diffs.
> **Date**: 2026-05-07 — initial revision.
> **Predecessor**: prior local-mode env handling lived implicitly in
> `internal/ops/env_generate.go` as flat resolve + write; design pass
> documented in `plans/local-flow-recipes-env-sync-2026-05-07.md`.

---

## 1. Why this spec exists

ZCP env state has multiple writers (Zerops project, zerops.yaml in repo,
user's local overrides) and multiple readers (running app, deployed
runtime, CI/CD pipelines, future container-mode review). Without an
explicit convergence model, two failure modes recurred:

1. **Channel confusion** — agents added env vars to the wrong channel
   (project env when it should have been deployed-only; zerops.yaml when
   the value belonged in user-overlay). Symptoms appear at deploy time
   or local-app startup, far from the original change.
2. **Lost user state** — every regen of `.env` clobbered local-only
   overrides. Per-key override design via markers/manifests was
   evaluated and rejected (see §10).

This spec captures the **mental model and design rationale** that live
above any single implementation file. Code can change; the model is
load-bearing for the entire local-dev experience and for future
extensions (container-mode env review, CI export, env promotion
between environments).

---

## 2. Core mental model — three channels, one rendered sink

```
INPUT CHANNELS (sources of env state)            OUTPUT (sink)
────────────────────────────────────             ────────────
 ┌─ project.envVariables                ┐
 │  Zerops project state, cross-service │
 │  Persistent across deploys           │
 │  Writers: import.yml, zerops_env     │
 │           action=set scope=project   │
 │                                      │
 ├─ zerops.yaml run.envVariables        ├──→  .env  (CWD, ZCP-rendered)
 │  Per-service deployed runtime        │
 │  In git repo, per-build snapshot     │
 │  Writer: text editor on the file     │
 │                                      │
 └─ .env.local                          ┘
    User-authored local overlay
    In CWD, gitignored
    Writer: USER (ZCP only READS it today;
             ZCP-seeding is design-intent, §7.1)
```

**Channel ownership rules** (the decision tree every contributor and
every atom must know):

| Use case | Channel |
|---|---|
| Same value local + deployed (secrets, API keys, third-party tokens) | **project.envVariables** |
| Derived from managed Zerops service (`${db_*}`, `${redis_*}`) | **zerops.yaml run.envVariables** with `${svc_var}` ref |
| Deployed-only (production feature flag, NODE_ENV=production) | **zerops.yaml run.envVariables** hardcoded |
| Local-only (APP_ENV=local, debug flags, mock URLs, ZCP-managed-key local override) | **`.env.local`** |

**The sink (`.env`) is fully derived.** Re-running render reproduces it
deterministically. Delete `.env` → next render restores it. Edit `.env`
directly → next render refuses with dry-run diff (see §6).

**The overlay (`.env.local`) is the user's no-touch zone.** Today ZCP
only READS `.env.local` (as the highest-precedence overlay during
`BuildEnvPlan`) and NEVER writes it — the user authors it. ZCP-seeding
it once at bootstrap/adoption (with detected/declared local-mode flags)
is design-intent, NOT yet shipped (§7.1). Either way the rule that
matters holds: user edits freely; overlay values always win at merge.

This split means **per-key override is solved without per-key
mechanism** — the user puts an override in `.env.local`, and that's the
override. There's no lock annotation, no manifest, no markers in `.env`.

---

## 3. EnvPlan — the state-convergence primitive

The architectural core is a typed plan that carries every rendered key
with metadata sufficient to drive any sink format (file write, dry-run
diff, shell export, future container-review surface).

```go
// internal/ops/env_plan.go (canonical location)

type EnvSource int
const (
    SourceProject            EnvSource = iota // project.envVariables
    SourceYAMLSetup                            // zerops.yaml run.envVariables, resolved
    SourceLocalOverlay                         // .env.local
    SourceBrownfieldImport                     // brownfield-adopt classification (Theme 3)
)

type EnvScope int
const (
    ScopeShared            EnvScope = iota // same value local + deployed
    ScopeDeployedRuntime                    // deployed-only
    ScopeLocalOverride                      // local-only
    ScopeManagedRef                         // resolved from ${svc_var}
)

type ConflictStatus int
const (
    StatusClean      ConflictStatus = iota // single source contributes this key
    StatusOverridden                        // local-overlay overrides base source
    StatusShadowed                          // multiple base sources contribute (zerops.yaml > project)
)

type EnvKey struct {
    Key      string
    Value    string
    Source   EnvSource
    Scope    EnvScope
    Conflict ConflictStatus
}

type EnvPlan struct {
    Setup                   string    // selected setup block from zerops.yaml
    CWD                     string    // absolute path
    Keys                    []EnvKey  // alphabetical ordering within source-precedence merge
    OmittedPlatformKeys     []string  // platform-internal keys filtered from the sink (provenance)
    TouchedServiceHostnames []string  // services whose ${svc_var} refs were resolved (provenance)
    Generated               time.Time // wall clock of plan construction
}

// BuildEnvPlan gathers sources, applies precedence, produces typed plan.
// Returns *SetupRequiredError (Available []string) when zerops.yaml has
// multiple setup blocks and `setup` is empty.
// Returns *RefResolveTransientError (Service, Cause) when a ${svc_var}
// ref cannot resolve (typically Zerops API unreachable; caller's prior
// .env left intact). Both are detected via errors.As.
func BuildEnvPlan(ctx context.Context, client platform.Client, projectID, setup, cwd string) (*EnvPlan, error)

// Render formats the plan for a specific sink. Sinks are pluggable
// formatters; new sinks (CI export, env-promotion diff) add format
// functions without changing the plan itself.
type EnvSink int
const (
    SinkDotenv      EnvSink = iota // .env file content
    SinkShellExport                 // `export KEY=VALUE` lines
    SinkDryRunDiff                  // diff vs existing .env (needs side input)
)

func (p *EnvPlan) Render(sink EnvSink) ([]byte, error)
```

`Render` directly produces `SinkDotenv` and `SinkShellExport`.
`SinkDryRunDiff` is NOT renderable from the plan alone — the plan
carries no existing-file side input — so `Render(SinkDryRunDiff)`
returns an error. The diff is a separate path: `(*EnvPlan)
DiffAgainstExisting(envPath)` reads the current `.env` and returns a
typed `*EnvDiff` (the dry-run/preview surface, §6.1).

**Why this primitive is load-bearing**:

- `.env`, dry-run diff, shell export are all **thin formatters over the
  same plan**. Adding a new sink (e.g., container-mode env review)
  doesn't require revisiting source merging.
- Per-key metadata enables **provenance display** (`zerops_env action=
  preview` can say "DATABASE_URL came from zerops.yaml; APP_ENV is
  overridden by .env.local; APP_KEY came from project").
- `SourceBrownfieldImport` is **reserved for future** (Theme 3); ensures
  brownfield-adopt slots in without revisiting Theme 0 design.
- Stable key ordering (alphabetical within each source, sources merged
  in precedence order) makes `.env` diffs in git readable and
  reproducible.

**What this primitive intentionally does NOT do**:

- Track which source last wrote a key (no per-key history).
- Persist state between invocations (no manifest file).
- Distinguish user-edited keys from ZCP-written keys in `.env` (drift
  detection happens via dry-run diff against current sources, not via
  hashes).

---

## 4. Source precedence

Within `BuildEnvPlan`, sources merge with explicit precedence:

```
base layers (lowest to highest):
  1. project.envVariables
  2. zerops.yaml run.envVariables (selected setup, ${svc_var} resolved)
overlay (highest, always wins):
  3. .env.local (when present)
```

**Rationale for `zerops.yaml > project`**: zerops.yaml is more specific
to deployment shape. If `APP_NAME` is in project as `myapp` and in
zerops.yaml run.envVariables as `myapp-stage`, the deploy-shape value
wins (matching what would happen at runtime on Zerops). The reverse —
project always wins — would surprise users who put per-environment
variants in zerops.yaml.

**Rationale for `.env.local > all base`**: it IS the user override
channel by definition. If the user wants something different from
deployed runtime, they put it here and it always reflects in `.env`.

**Rationale for excluding the deployed service-level layer from local
`.env` render**: this map models LOCAL-machine vars (project +
zerops.yaml `run.envVariables` + `.env.local`), not a container snapshot
— the deployed service userData/secret layer is a different surface.
(Earlier this was attributed to the API being unable to distinguish
user- from system-defined service envs; that was too narrow. The slim
`service-stack/{id}/env` returns typed records but is INCOMPLETE — it
omits the yaml-baked vars. The full effective service env IS
reconstructable from the API — project env + app-version userDataList +
slim service env — and `zerops_discover` / `zerops_env get` now assemble
it for live runtimes. See `docs/spec-zerops-env-lifecycle.md` §1, §6;
bare-key precedence is the 4-layer order in §2.)

---

## 5. Setup parameter (first-class)

`zerops.yaml` may declare multiple setup blocks (e.g., `app` + `worker`
in a monorepo, or recipe-style `dev` + `prod`). Each block has its own
`run.envVariables`. The plan must select one.

```
zerops_env action=generate-dotenv setup=<name>
```

**Resolution rules**:

- `setup` non-empty: pick that block; error if missing from zerops.yaml.
- `setup` empty + zerops.yaml has exactly one block: auto-pick.
- `setup` empty + zerops.yaml has zero blocks: legacy fallback to
  `serviceHostname` matching (deprecated path; warning emitted).
- `setup` empty + zerops.yaml has >1 block: refuse with
  `*SetupRequiredError` (carries `Available []string`), list available
  block names.

**Why dedicated parameter** rather than overloading `serviceHostname`:
recipe setup names (`dev`, `prod`, `worker`) are not always service
hostnames. A monorepo may have setup `worker` that builds artifacts for
service hostname `appworker`. Keeping these distinct reflects the
zerops.yaml schema.

---

## 6. Render policy — dry-run + refuse-unowned + atomic write

Three guarantees every render must hold:

### 6.1 Dry-run mode

```
zerops_env action=generate-dotenv preview=true
```

Returns an `EnvDotenvResult` (no write performed; `preview: true`):

```json
{
  "path":     "/abs/cwd/.env",
  "setup":    "dev",
  "services": 2,
  "variables": 14,
  "diff": {
    "added":    ["KEY1", "KEY2"],
    "modified": [{"key": "DATABASE_URL", "from": "...", "to": "..."}],
    "unowned":  ["MANUAL_EDIT"]
  },
  "preview": true
}
```

The `diff` is an `EnvDiff` with exactly `added` / `modified` /
`unowned` — there is NO `removed` field. A key the user removed from
all sources but that lingers in the existing `.env` surfaces under
`unowned` (it is no longer produced by any source), not a dedicated
removed list.

**`unowned`** = keys present in the existing `.env` that:
- are NOT in the plan (no source produces them), AND
- are NOT in `.env.local` (overlay didn't introduce them).

Such keys are user-direct edits to `.env` (breaking the "ZCP renders,
user does not edit `.env`" contract).

### 6.2 Refuse-on-unowned-edits

Default write (without `force=true`) refuses when `diff.unowned` is
non-empty. Error message: "Existing `.env` has keys not produced by any
source: \[KEY...\]. Move them to `.env.local` to preserve, or pass
`force=true` to discard."

This **replaces the manifest-based ownership tracking** considered in
the design pass — per-key drift is detected stateless by comparing
current sources to current `.env`, no hidden state to desync.

### 6.3 VPN-down / API-fail policy

If `BuildEnvPlan` fails with `*RefResolveTransientError` (detected via
errors.As; typically when Zerops API is unreachable for `${svc_var}`
resolution):

- Generate-dotenv returns the error with a VPN/retry hint.
- **No write occurs.** Prior `.env` remains the operative file.
- Dry-run mode also returns the error; no partial diff.

**Never write a placeholder** value into `.env`. A wrong-but-syntactic
DATABASE_URL is worse than no change — the app starts and fails at
first DB query, user debugs upstream of the actual cause.

### 6.4 Atomic write + advisory lock

Writes use temp-file + rename (`.env.tmp.<pid>` → `.env`). Concurrent
regens (rare but possible — two CLI invocations) serialize via advisory
lock at `.zcp/state/locks/dotenv-<setup>.lock` (flock-style; auto-released
on process exit).

---

## 7. `.env.local` lifecycle

### 7.1 Creation/seeding — DESIGN-INTENT, not yet implemented

> **Status: design-only.** Today ZCP NEVER creates or seeds
> `.env.local` — it only READS it (`readEnvLocal` in
> `internal/ops/env_plan.go`, consumed by `BuildEnvPlan`). The user
> authors `.env.local`. The seeding flow below is reserved design
> intent (like the §11 brownfield sketch), so Theme 0/1/2 don't
> foreclose it; no production code path performs it.

When implemented, ZCP would create `.env.local` exactly once, in two
contexts:

- **Recipe local bootstrap** (Theme 1): seed with extracted overrides
  from the recipe's `dev` setup block (typically `APP_ENV=local`,
  `LOG_LEVEL=debug`, etc.).
- **Brownfield adoption** (Theme 3): seed with classified local-only
  entries from the user's existing `.env`.

The created file would carry a fixed header:

```
# Created by ZCP. Edit freely — ZCP merges these values into .env at
# every generate-dotenv but will not overwrite this file.
# Add ".env.local" to .gitignore if not already there.
```

The seeding design is single-write-then-read-only: a created file is
never rewritten, and every other path treats `.env.local` as the
user's no-touch zone (read-only overlay). Until the flow ships, that
read-only invariant already holds trivially — ZCP has no `.env.local`
writer at all.

### 7.2 User edits

User adds, removes, or modifies entries in `.env.local` freely. Next
`generate-dotenv` reads the current `.env.local` content as overlay and
merges into `.env`.

### 7.3 Override semantics

A key in `.env.local` always wins in `.env`, regardless of base source.
To "release" a previously-overridden key (let ZCP's base value resume),
remove the key from `.env.local` and re-render.

### 7.4 Multi-machine clone

`.env.local` is gitignored — does not transfer via clone. New developer
either:

- Authors a fresh `.env.local` by hand (once ZCP bootstrap seeding ships
  per §7.1, that seeds the default flags), or
- Copies values from a documented `.env.local.example` (committed,
  shows expected keys with placeholder values; team-shared
  documentation, not actual secrets).

### 7.5 Git-tracking detection — DESIGN-INTENT, not yet implemented

> **Status: design-only.** No production code runs `git ls-files
> .env.local` or otherwise detects whether `.env.local` is git-tracked,
> and nothing emits the warning below. The atom
> `develop-local-env-troubleshoot.md` documents the warning text for the
> user, but no code surface produces it yet.

When implemented, ZCP would detect if `.env.local` is git-tracked (via
`git ls-files .env.local`) and surface a high-severity warning. Tracked
`.env.local` promotes per-developer state to team state and can poison
production builds (a developer's `APP_ENV=local` flowing into a
deployed image).

---

## 8. Lifecycle events

The decision tree for "where do I put this new env var" maps to atom
guidance for each common case. Each row below corresponds to an atom in
`internal/content/atoms/develop-local-env-*.md`.

| Event | Action | Channel | Result after regen |
|---|---|---|---|
| Add shared secret | `zerops_env action=set scope=project key=X value=Y` | project | Appears in `.env` |
| Add deployed-only var | edit `zerops.yaml run.envVariables` | zerops.yaml | Appears in `.env` (and deployed on push) |
| Add local-only override | edit `.env.local` | overlay | Persists across regens |
| Override ZCP-managed key locally | put key in `.env.local` | overlay | User value wins in `.env` |
| Add managed service (e.g. redis) | `zerops_import` extension + edit zerops.yaml `${redis_*}` ref | zerops.yaml | REDIS_URL resolves into `.env` |
| Rotate secret | `zerops_env action=set scope=project` | project | New value in `.env`; warn if `.env.local` masks |
| Retake ZCP-managed key | delete from `.env.local` | (release) | Base value resumes |

---

## 9. Edge case decisions — explicit policies

These are choices made during design pass; future work should know the
*why* before changing them.

### 9.1 Framework `.env.local` collision (Vite, Next, Symfony)

Multiple frameworks natively load `.env.local` with override semantics
over `.env`. ZCP also reads `.env.local` and merges into `.env`. After
ZCP merge, `.env.local` values are also in `.env`, so framework
double-load is harmless (same values either way).

**Why we accept this**: alternative names (`.env.zcp.local`,
`.env.user`) sacrifice developer familiarity for a non-problem. The
double-load is observable but produces no behavior change.

**Hard warning case**: `.env.local` git-tracked → §7.5 (the warning is
design-intent; not yet emitted by any code path).

### 9.2 User edits `.env` directly

User intuitively might edit `.env`. Next regen detects `unowned` keys
via §6.1 and refuses with §6.2. Error message guides to move into
`.env.local`.

**Why refuse rather than auto-migrate**: migration changes user code
without their explicit action. Error-with-instruction respects user
intent.

### 9.3 Variable interpolation in dotenv files

Dotenv loaders sometimes support `KEY=${OTHER_KEY}` interpolation
(generic, not Zerops-specific). ZCP preserves these raw — does NOT
expand them at render time. Only Zerops `${svc_var}` refs from
zerops.yaml are resolved.

**Why preserve generic interpolation**: it's framework-loader semantics,
not Zerops semantics. Eager expansion would prevent the framework from
applying its own resolution rules.

### 9.4 Multi-setup mandatory selection

`zerops.yaml` with >1 setup block + bare `generate-dotenv` (no `setup=`)
→ refuse with available-setups list. See §5.

**Why refuse rather than guess**: guessing leads to wrong-setup
rendering (e.g., `worker` env values in app's `.env`), which silently
fails at app startup with confusing errors.

### 9.5 Concurrent regen

Two CLI invocations regenerating same setup race on read-modify-write.
Atomic rename prevents torn files but doesn't serialize. Advisory lock
serializes. See §6.4.

### 9.6 User deletes `.env`

Next regen creates fresh `.env` from sources + `.env.local` overlay.
This is one of the strongest validations of "`.env` is fully derived" —
recovery is automatic and complete.

### 9.7 First-deploy latency for recipe local mode

Stage runtime in READY_TO_DEPLOY (post Theme 1 transform) means the
subdomain returns 502/503 until first local deploy. This is intentional
(forces pipeline verification as part of bootstrap). Atom guidance
states: "first deploy is mandatory bootstrap step; subdomain not live
until then."

---

## 10. Alternatives considered and rejected

### 10.1 Two-region `.env` with `=== ZCP-MANAGED BEGIN/END ===` markers

Single `.env` with comment markers delimiting ZCP-owned vs user-owned
regions. Rejected because:

- Marker parsing is fragile (collision with user-authored marker-like
  lines).
- Leaks ZCP internals into user-facing primary file.
- User can break parsing by editing markers.
- Doesn't solve per-key override cleanly (user override of an
  ZCP-managed key requires either editing managed region or duplicating
  in user region with first-wins semantics).

The `.env.local` overlay solves the same problem with better separation.

### 10.2 Manifest-based ownership tracking

Hidden state file `.zcp/state/dotenv-manifest.json` with per-key hashes,
detecting drift between regens. Rejected because:

- Hidden state can desync (user moves `.env`, deletes manifest, edits
  between regens).
- Recovery semantics are subtle (what if manifest is gone but `.env`
  exists?).
- Adds another moving part.

Stateless dry-run diff (§6.1) provides the same drift detection without
persistent state.

### 10.3 Framework-side `.env.local` overlay only

Rely on frameworks to load `.env.local` for overrides; ZCP only writes
`.env`. Rejected because:

- Doesn't work for Laravel, Django, plain Go, plain Python, Spring
  Boot, ASP.NET Core (no native `.env.local` overlay).
- Framework precedence varies (some load always, some only in
  development mode).

ZCP merging at render time (§4) is framework-agnostic.

### 10.4 Per-key annotation in zerops.yaml (`# zcp:local-override`)

Allow zerops.yaml to declare per-key whether an entry is overridable
locally. Rejected because:

- Adds custom syntax to a yaml file validated by Zerops platform schema
  (would have to be inside comments — fragile parsing).
- Coupling: yaml-file behavior depends on ZCP-side parser version.
- The same effect is achievable by putting the key in `.env.local`.

### 10.5 Layered `.env.zerops` + `.env.local` files

Three-file scheme: `.env.zerops` (ZCP-managed), `.env.local` (user),
`.env` (final, framework-loaded). Rejected because:

- More files = more user confusion.
- Framework dependency on which file to load first.
- Same problem as 10.3.

---

## 11. Brownfield-adopt sketch (Theme 3, design-only)

Scenario: user has working app + existing `.env`, says "deploy to
Zerops in local mode." ZCP must classify the existing `.env` and
distribute entries across channels.

This section captures the design intent so Theme 0 / 1 / 2 don't
foreclose it.

### 11.1 Trigger

`zerops_workflow workflow=bootstrap` → discover phase detects:

- non-empty `.env` in CWD, AND
- framework signals (`package.json`, `composer.json`, `go.mod`, etc.), AND
- no Zerops integration (no `zerops.yaml`, no `.zcp/state`).

→ enters **brownfield-adopt subroute** (sibling to recipe and
classic-greenfield routes under local-mode bootstrap).

### 11.2 Adoption transaction

```
1. zerops_env action="classify-dotenv" cwd=<dir>
   → ClassificationProposal (per-key suggestions with reasoning)
2. Atom guides agent to surface proposal to user; user confirms / edits.
3. zerops_env action="adopt-dotenv" cwd=<dir> proposal=<confirmed>
   → backs up original .env to .zcp/state/backups/dotenv/<ts>.env (0600);
   → writes import.yml + zerops.yaml + .env.local from proposal;
   → returns next-step: zerops_import.
4. zerops_import → Zerops creates project + system vars resolve.
5. generate-dotenv setup=<auto-picked> → fresh .env.
6. Backup path surfaced so user can recover if classification was off.
```

### 11.3 Classification heuristics (library)

| Pattern | Class | Channel |
|---|---|---|
| URL-scheme: `postgres://`, `redis://`, `mongodb://`, `mysql://`, `amqp://`, `nats://`, `s3://`; hostnames `localhost`, `db`, `redis`, `cache`, common Docker Compose service names; standard ports | managed-service candidate | zerops.yaml `${svc_*}` ref + suggest service in import.yml |
| `APP_KEY`, `APP_SECRET`, `JWT_*`, `*_KEY`, `*_TOKEN`, `SECRET_*`, `SESSION_*`, `COOKIE_*`, `ENCRYPTION_*` | shared app secret | project.envVariables; preserve existing value (rotation breaks sessions) |
| `STRIPE_*`, `OPENAI_*`, `ANTHROPIC_*`, `MAILGUN_*`, `SENDGRID_*`, `GITHUB_TOKEN`, `AWS_SECRET_ACCESS_KEY` | external secret | project.envVariables |
| `NODE_ENV`, `APP_ENV`, `RAILS_ENV`, `ASPNETCORE_ENVIRONMENT`, `GO_ENV`, `DEBUG`, `APP_DEBUG` | mode flag | split: zerops.yaml=production, `.env.local`=local |
| `LOG_LEVEL=debug`, `MOCK_*`, `LOCAL_*`, `XDEBUG_*` | local-only | `.env.local` |
| `PORT`, `*_TIMEOUT`, `*_RETRIES`, public URLs | plain config | zerops.yaml + optionally `.env.local` |

### 11.4 Auto-vs-confirm rules

**Auto-distribute** (low risk):
- Mode flags with explicit `local`/`development` value → `.env.local`.
- `LOG_LEVEL=debug` → `.env.local`.
- `DATABASE_URL` to `localhost:5432` when user requested Zerops Postgres
  and no external DB evidence.

**Require explicit user confirmation**:
- External-looking hostname (could be local Docker or external managed DB).
- Provider/payment token (high sensitivity).
- App encryption keys (rotation risks).
- Any value with production-looking secret prefixes.
- Ambiguous URLs.

### 11.5 Why brownfield reuses Theme 0 primitive

`SourceBrownfieldImport` is reserved as an `EnvSource` enum value in
Theme 0 (Phase 0G). When Theme 3 implements adoption, classified
entries flow as `EnvSource: SourceBrownfieldImport` at appropriate
precedence (between project and yaml-setup, since brownfield values
were "user's previous truth" — more specific than project, less
specific than current zerops.yaml deployment shape).

This means brownfield-adopt is **not a parallel mechanism** — it's the
same EnvPlan with one additional source layer. Render policy, dry-run,
refuse-unowned, atomic write all apply unchanged.

---

## 12. Future extensions (architectural reservations)

The EnvPlan primitive (§3) is designed to serve more than local-mode
`.env` rendering. Future surfaces:

### 12.1 Container-mode env review

`zerops_env action=preview-runtime serviceHostname=<name>` would build
an EnvPlan over the deployed runtime's effective env (project +
yaml-baked `run.envVariables` + slim service-level), render via
`SinkDryRunDiff` for human review. All three layers are API-readable
today — yaml-baked via the app-version userDataList, no SSH
(`docs/spec-zerops-env-lifecycle.md` §6) — and `ops.EffectiveServiceEnv`
already assembles exactly this. No `.env.local` overlay (container runs
deployed env, no local override). Pure additive — no Theme 0 changes
needed.

### 12.2 CI / shell export

`zerops_env action=export setup=<name> format=sh|json` renders via
`SinkShellExport` or new `SinkJSON`. Useful for CI environments that
inject env into runners without writing files. Future sink, no plan
changes.

### 12.3 Env promotion diff

`zerops_env action=promote-diff from=<setup> to=<setup>` builds two
EnvPlans (one per setup), renders side-by-side via new `SinkPromoteDiff`.
Surfaces "what changes when promoting stage to prod" — answers a
recurring question in multi-environment deployments.

### 12.4 Multi-target local

A future `output=` parameter (`generate-dotenv setup=<name>
output=.env.<name>`) would allow simultaneous multi-target rendering
(single CWD, multiple `.env.<setup>` files). NOT yet shipped:
`generate-dotenv` has no `output` parameter and always writes
`<workingDir>/.env` — this is a §12 reservation, not current behavior.

`.env.local` per-setup (`.env.<setup>.local`) is opt-in advanced case;
default single `.env.local` covers monorepo where all setups share
local-mode flags.

---

## 13. Invariants pinned by tests

This section enumerates the invariants that MUST be pinned by tests so
future refactors don't silently regress them. Each invariant lists its
canonical test name(s).

### Plan construction
- Source precedence (project < yaml-setup): `TestBuildEnvPlan_PrecedenceYAMLOverProject`.
- Overlay wins on conflict: `TestBuildEnvPlan_OverlayWinsOverYAML`, `TestBuildEnvPlan_OverlayWinsOverProject`.
- Stable key ordering: `TestBuildEnvPlan_StableKeyOrdering`.
- Setup parameter required when multiple blocks: `TestBuildEnvPlan_MultipleSetups_RequiresSelection`.
- Brownfield import slot (Theme 3 reservation): `TestBuildEnvPlan_BrownfieldImport_MergesAtCorrectPrecedence`.

### Render
- Dotenv format stability across runs: `TestEnvPlan_RenderDotenv_FormatStability`.
- Dry-run computes accurate diff: `TestGenerateDotenv_PreviewReturnsDiff`.
- Refuse on unowned edits without force: `TestGenerateDotenv_RefusesUnownedEdits`.
- Force overrides unowned: `TestGenerateDotenv_ForceOverridesUnowned`.

### Resilience
- VPN-down leaves prior .env intact: `TestGenerateDotenv_VPNDown_LeavesPriorEnvIntact`.
- Concurrent invocations serialize via lock: `TestGenerateDotenv_ConcurrentInvocations_Serialize`.

### `.env.local` contract
- Overlay is read-only to ZCP today: `.env.local` is consumed only as
  the highest-precedence overlay (`readEnvLocal` → `BuildEnvPlan`),
  pinned by the overlay-precedence tests under Plan construction
  (`TestBuildEnvPlan_OverlayWinsOverYAML`,
  `TestBuildEnvPlan_OverlayWinsOverProject`). No code writes
  `.env.local`.
- Creation/seeding + git-tracking detection are design-intent (§7.1,
  §7.5), NOT shipped — no `EnsureEnvLocal`, no single-writer lint, no
  detection code exists, so there is nothing to pin yet. Add these
  invariants here only when the seeding flow is implemented.

### Lifecycle status check
- Detects fresh / stale / unowned-edits / missing / vpn-down: `TestCheckLocalDotenvFresh_*` table.

---

## 14. Open evolution paths

When future work surfaces friction or new requirements:

- **Per-key history / audit log**: would require persistent state. Defer
  until concrete need (e.g., compliance trail). Stateless dry-run diff
  covers most "what changed" questions.
- **Cross-developer overlay sharing**: `.env.local` is per-developer;
  `.env.local.example` is the documented sharing path. If teams need
  more, evaluate after seeing concrete pain.
- **Per-key annotation** (e.g., zerops.yaml `# zcp:sensitive`): would
  enable smarter rendering (mask secrets in dry-run output). Currently
  inferable from key-name heuristics (§11.3); annotation makes it
  explicit at cost of yaml-file complexity. Defer.
- **Multi-developer conflict resolution** (`.env.local` collisions in
  shared dev environments): not a current scenario; per-developer
  overlay is the simplifying assumption.
