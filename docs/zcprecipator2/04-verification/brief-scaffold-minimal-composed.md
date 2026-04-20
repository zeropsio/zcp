# brief-scaffold-minimal-composed.md

**Role**: scaffold (single inline — main agent reads the atoms at `phases/generate/scaffold/*` substep; no Agent dispatch)
**Tier**: minimal
**Source atoms** (per data-flow-minimal.md §4a — minimal single-codebase path):

```
phases/generate/entry.md
phases/generate/scaffold/entry.md
phases/generate/scaffold/where-to-write-single.md          ← tier-branch, minimal
phases/generate/scaffold/dev-server-host-check.md          (if hasBundlerDevServer)

+ briefs/scaffold/framework-task.md                          (main consumes as guidance, not as dispatch)
+ briefs/scaffold/pre-ship-assertions.md
+ briefs/scaffold/api-codebase-addendum.md OR frontend-codebase-addendum.md  (per framework)

+ pointer-include principles/where-commands-run.md
+ pointer-include principles/file-op-sequencing.md
+ pointer-include principles/platform-principles/* (filtered by applicable role: principle 1/2/3 for api-style minimal; principle 2 for frontend-static minimal)
+ pointer-include principles/symbol-naming-contract.md
+ pointer-include principles/comment-style.md
+ pointer-include principles/visual-style.md
```

**Minimal tier boundary**: single codebase → NO Agent dispatch. Main agent reads these atoms in-band and writes scaffold on the one mount (`/var/www/appdev/`). The composed output is **substep-entry guidance delivered to the main agent**, not a transmitted sub-agent prompt.

Interpolations: `{{.Hostname}} = appdev`, `{{.SymbolContract | toJSON}}` (minimal contract, single hostname), `{{.Framework}} = NestJS / Svelte / Laravel / etc.` per plan.

---

## Composed brief (substep guidance, main agent consumes)

```
## phases/generate/entry.md — step-entry framing

This phase completes when every substep's predicate holds on the mount; `zerops_workflow action=status` lists the substeps. The substep order for minimal tier is: scaffold → app-code → smoke-test → zerops-yaml.

You are on the zcp orchestrator. `/var/www/appdev/` is an SSHFS mount — the one codebase for this minimal recipe. Executables run via `ssh appdev "cd /var/www && <command>"`; file writes go through the mount via Write/Edit.

--- [phases/generate/scaffold/entry.md] ---

Substep completes when the single codebase is scaffolded, pre-ship assertions pass, and no scaffold test artifacts remain. This is the minimal tier; no sub-agent dispatch fires — you write the scaffold inline.

--- [phases/generate/scaffold/where-to-write-single.md, tier-branch=minimal] ---

Single-codebase minimal: write the scaffold on `/var/www/appdev/`. No parallel dispatch; you author the scaffold yourself using the atoms below as guidance. You apply the same framework-task + pre-ship-assertions that a scaffold sub-agent would follow in a multi-codebase recipe — except you own the whole surface.

--- [phases/generate/scaffold/dev-server-host-check.md, if hasBundlerDevServer] ---

For SPA-frontend minimals (Vite, Next, etc.): the dev-server must bind `0.0.0.0` not `localhost`. For API-style minimals (NestJS, Laravel): the HTTP server binds `0.0.0.0`. See `principles/platform-principles/02-routable-bind.md`.

--- [briefs/scaffold/framework-task.md, main-inline] ---

You scaffold a minimal `{{.Framework}}` application on the mount. Execution follows three steps:

1. Run the framework scaffolder via SSH with --skip-git:

       ssh appdev "cd /var/www && <framework-scaffolder-command> --skip-git --skip-install"

   Example: `npx -y @nestjs/cli new . --skip-git --skip-install` for NestJS; `npm create vite@latest . -- --template svelte-ts` for Svelte.

2. Install dependencies via SSH.

3. Read every file the scaffolder emitted before the first Edit. Then apply the addendum for this framework role (below).

--- [briefs/scaffold/{api|frontend}-codebase-addendum.md] ---

(Select addendum per framework:
- NestJS/Laravel/Go-API minimal → api-codebase-addendum.md
- Svelte/Next/Astro static-frontend minimal → frontend-codebase-addendum.md
- Dual-runtime minimal with both → both addenda, tier-gated to each codebase.

Same atom content as showcase scaffold — describes the files to write, signature requirements (bind 0.0.0.0, enable shutdown hooks if applicable, env-var reading from contract).)

--- [principles/symbol-naming-contract.md consumption conventions only] ---

`SymbolContract` for minimal is smaller (single codebase, ≤1 managed service): `EnvVarsByKind.db` populated if db service exists; empty NATS / storage / search sections; `Hostnames[] = [{role:"primary",dev:"appdev",stage:"appstage"}]`; `FixRecurrenceRules` filtered to rules with `appliesTo` containing `any` plus role-specific rules.

For single-codebase minimal, the contract's primary role is to supply `EnvVarsByKind.db.*` runtime names and `FixRecurrenceRules` list. No cross-codebase coordination (the v34 class is trivially closed because there's only one codebase).

Applicable rules (for a single-codebase api-style minimal with db):
- gitignore-baseline, env-example-preserved, no-scaffold-test-artifacts, skip-git (always)
- env-self-shadow (main-agent zerops.yaml substep; filter at consumption)
- routable-bind, trust-proxy, graceful-shutdown (if api-style minimal)

--- [principles/where-commands-run.md] ---

(Positive form.)

--- [principles/file-op-sequencing.md] ---

Every Edit preceded by a Read of the same file in this session. For scaffolder-emitted files, batch-Read before the first Edit.

--- [principles/platform-principles/01..06.md, filtered] ---

(Only principles applicable to the minimal's framework role are pointer-included. For an api minimal: 01 + 02 + 03 + 05 + 06. For a static-frontend minimal: 02 + 06.)

--- [briefs/scaffold/pre-ship-assertions.md, main-inline] ---

Source of truth: `SymbolContract.FixRecurrenceRules` where `appliesTo` matches this minimal's role set. Aggregate exit 0 before advancing to the next substep.

Reminder snapshot (api-style minimal with single db managed service):

    HOST=appdev
    MOUNT=/var/www/$HOST
    # --- if api ---
    grep -q '0.0.0.0' $MOUNT/src/main.ts 2>/dev/null                             || true  # or framework equivalent
    grep -q 'trust proxy' $MOUNT/src/main.ts 2>/dev/null                          || true
    grep -q 'enableShutdownHooks' $MOUNT/src/main.ts 2>/dev/null                  || true
    # --- if frontend ---
    grep -q '0.0.0.0' $MOUNT/vite.config.* 2>/dev/null                            || true
    grep -q 'zerops.app' $MOUNT/vite.config.* 2>/dev/null                         || true
    # --- shared ---
    test ! -f $MOUNT/README.md                                                    || { echo FAIL: README.md present; exit 1; }
    test ! -f $MOUNT/zerops.yaml                                                  || { echo FAIL: zerops.yaml present; exit 1; }
    ! ssh $HOST 'test -d /var/www/.git'                                           || { echo FAIL: /var/www/.git present; exit 1; }
    test ! -f $MOUNT/.env                                                         || { echo FAIL: .env present; exit 1; }
    test -f $MOUNT/.env.example                                                   || { echo FAIL: .env.example missing; exit 1; }
    grep -qE '^(node_modules|/node_modules)' $MOUNT/.gitignore                    || { echo FAIL: .gitignore missing node_modules; exit 1; }
    ! find $MOUNT -maxdepth 3 \( -name 'preship.sh' -o -name '*.assert.sh' \)     || { echo FAIL: scaffold test artifacts present; exit 1; }
    exit 0

After assertions: `ssh appdev "cd /var/www && npm run build 2>&1 | tail -40"` (or framework build equivalent). Fix errors before advancing.

--- [principles/comment-style.md + principles/visual-style.md] ---

ASCII only; positive style.

--- [phases/generate/scaffold/completion.md] ---

Attest via `zerops_workflow action=complete step=generate substep=scaffold`. Attestation reports: files written, pre-ship exit code (must be 0), build tail, and (if any facts recorded) fact summary.
```

**Composed byte-budget**: ~6 KB (smaller than showcase scaffold dispatch because no sub-agent framing, no duplicated MANDATORY, no cross-codebase coordination).

**Key tier-boundary note**: this composition is consumed IN-BAND by the main agent. It is not transmitted to a sub-agent. All P2 rules about "leaf brief" still apply — the composition contains no dispatcher-facing text, but there is also no sub-agent that receives it. The difference is architectural: the same atoms are used in both paths; only the delivery mechanism differs (stitched into main's substep-entry vs transmitted via Agent-tool).
