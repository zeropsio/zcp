# ZCP Env Handling Specification

## 1. Scope

Env state convergence across project / zerops.yaml / local
overlay sources to one or more rendered sinks. Primary user-visible
sink is local-mode `.env`.

---

## 2. Core mental model — three channels, one rendered sink

```
INPUT CHANNELS (sources of env state)            OUTPUT (sink)
────────────────────────────────────             ────────────
 ┌─ project vault                       ┐
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
    Writer: USER (ZCP only READS it)
```

**Channel ownership rules** (the decision tree every contributor and
every atom must know):

| Use case | Channel |
|---|---|
| Same value local + deployed (secrets, API keys, third-party tokens) | **project vault** |
| Derived from managed Zerops service (`${db_*}`, `${redis_*}`) | **zerops.yaml run.envVariables** with `${svc_var}` ref |
| Deployed-only (production feature flag, NODE_ENV=production) | **zerops.yaml run.envVariables** hardcoded |
| Local-only (APP_ENV=local, debug flags, mock URLs, ZCP-managed-key local override) | **`.env.local`** |

**The sink (`.env`) is fully derived.** Re-running render reproduces it
deterministically. Delete `.env` → next render restores it. Edit `.env`
directly → next render refuses with dry-run diff (see §6).

**The overlay (`.env.local`) is the user's no-touch zone.** ZCP
only READS `.env.local` (as the highest-precedence overlay during
`BuildEnvPlan`) and NEVER writes it — the user authors it. User edits
freely; overlay values always win at merge.

This split means **per-key override is solved without per-key
mechanism** — the user puts an override in `.env.local`, and that's the
override. There's no lock annotation, no manifest, no markers in `.env`.

---

## 3. EnvPlan — the state-convergence primitive

The architectural core is a typed plan that carries every rendered key
with metadata sufficient to drive any sink format (file write, dry-run
diff, shell export).

**Why this primitive is load-bearing**:

- `.env`, dry-run diff, shell export are all **thin formatters over the
  same plan**. Adding a new sink (e.g., container-mode env review)
  doesn't require revisiting source merging.
- Per-key metadata enables **provenance display** (`zerops_env action=
  preview` can say "DATABASE_URL came from zerops.yaml; APP_ENV is
  overridden by .env.local; APP_KEY came from project").
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
  1. the project vault
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
The slim
`service-stack/{id}/env` returns typed records (`SYSTEM` | `USER`) but is
INCOMPLETE — it omits project envs and platform-injected runtime vars, and
since 2026-08 its `USER` records mix user-set vars with a read-only mirror
of the yaml-baked `run.envVariables`; the user-set layer is derived by
subtracting the app-version userDataList keys (`ops.FetchServiceUserEnvs`).
The full effective service env IS reconstructable from the API — project
env + app-version userDataList + slim service env — and `zerops_discover`
/ `zerops_env get` assemble it for live runtimes. See
`docs/spec-zerops-env-lifecycle.md` §1, §6; bare-key precedence is the
4-layer order in §2.

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
`unowned` — there is NO `removed` field. A `modified` entry's `from`
never echoes the existing `.env` value: that literal carries no word of
where it came from, so a secret an earlier render put there (a ref the
key no longer uses, a sensitive var) reads like any other value — it
shows as `<not shown: the existing .env value>`, or as the credential /
sensitive mask when `to` is masked too; `to` is masked by the plan's own
classification (`TestGenerateDotenv_PreviewDiff_OldSecretNeverEchoed`,
`TestGenerateDotenv_PreviewDiff_SensitiveMasked`). A key the user removed from
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

- Authors a fresh `.env.local` by hand, or
- Copies values from a documented `.env.local.example` (committed,
  shows expected keys with placeholder values; team-shared
  documentation, not actual secrets).

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

### 9.1 Framework `.env.local` collision (Vite, Next, Symfony)

Multiple frameworks natively load `.env.local` with override semantics
over `.env`. ZCP also reads `.env.local` and merges into `.env`. After
ZCP merge, `.env.local` values are also in `.env`, so framework
double-load is harmless (same values either way).

**Why we accept this**: alternative names (`.env.zcp.local`,
`.env.user`) sacrifice developer familiarity for a non-problem. The
double-load is observable but produces no behavior change.

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

Stage runtime in READY_TO_DEPLOY means the
subdomain returns 502/503 until first local deploy. This is intentional
(forces pipeline verification as part of bootstrap). Atom guidance
states: "first deploy is mandatory bootstrap step; subdomain not live
until then."

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

### Render
- Dotenv format stability across runs: `TestEnvPlan_RenderDotenv_FormatStability`.
- Dry-run computes accurate diff: `TestGenerateDotenv_PreviewReturnsDiff`.
- Refuse on unowned edits without force: `TestGenerateDotenv_RefusesUnownedEdits`.
- Force overrides unowned: `TestGenerateDotenv_ForceOverridesUnowned`.

### Resilience
- VPN-down leaves prior .env intact: `TestGenerateDotenv_VPNDown_LeavesPriorEnvIntact`.
- Concurrent invocations serialize via lock: `TestGenerateDotenv_ConcurrentInvocations_Serialize`.

### `.env.local` contract
- Overlay is read-only to ZCP: `.env.local` is consumed only as
  the highest-precedence overlay (`readEnvLocal` → `BuildEnvPlan`),
  pinned by the overlay-precedence tests under Plan construction
  (`TestBuildEnvPlan_OverlayWinsOverYAML`,
  `TestBuildEnvPlan_OverlayWinsOverProject`). No code writes
  `.env.local`.

### Lifecycle status check
- Detects fresh / stale / unowned-edits / missing / vpn-down: `TestCheckLocalDotenvFresh_*` table.

