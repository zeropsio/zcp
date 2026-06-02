# Schema fetch host — derive from `ZCP_API_HOST`, not hardcoded

**Date:** 2026-06-02
**Status:** Codex-verified (NO-GO→edits applied); ready to implement
**Scope owner:** krls2020
**Relation:** extends `plans/schema-validation-final-2026-06-01.md` (the live-cache work touches the same fetch path — the recipe base-validation it added currently validates against the wrong instance for non-prg1 users).

---

## 1. Problem (confirmed in code)

Every host in ZCP is resolved from `internal/auth`: `ZCP_API_HOST` (env) → `apiHost`,
defaulting to `api.app-prg1.zerops.io` or the region address (`auth.go:121,145`). The
platform client uses it on both the server (`s.authInfo.APIHost`) and the CLI
(`auth.ResolveCredentials().APIHost` → `cmd/zcp/main.go:197`, `eval.go:618`).

The **schema fetch is the only path that ignores it** — `internal/schema/schema.go:15-16`
hardcodes two consts to `api.app-prg1.zerops.io`, consumed by `FetchSchemas`
(`cache.go:103,107`), `FetchRawSchemas` (`sync.go:34,38`), and `fetchURL`.

**Impact:** a user on a non-prg1 region / private Zerops instance (`ZCP_API_HOST` set)
deploys to *their* host but ZCP validates `zerops.yaml`/import against **prg1's** schema →
region-specific service types/bases are false-rejected (or stale ones wrongly accepted).
The just-landed recipe live-base validation (b) has the same latent defect — it checks
bases against prg1, not the user's instance.

---

## 2. Principle

The schema describing what a Zerops instance accepts MUST be fetched from the **same host
the user operates against**. So the schema URL is **derived** from the resolved `apiHost`,
exactly like the platform client — never hardcoded. Dependency-injected: the `schema`
package takes a host parameter; it does NOT import `auth` (stays a low-level, auth-free
package).

**One non-obvious split (the load-bearing design decision):**

| Path | Host | Why |
|------|------|-----|
| **Runtime cache** (recipe base validation, any live read) | the user's **`auth.APIHost`** (ZCP_API_HOST-respecting) | validate against the instance the user actually deploys to |
| **`schema sync` / `schema check`** (write/compare the COMMITTED embedded floor + `active_versions.json`) | **pinned canonical `api.app-prg1.zerops.io`** | the committed artifacts are a SHARED repo reference; if they varied by whoever's ZCP_API_HOST ran the sync, every region's dev would produce a different committed file |

The committed embedded schema is the canonical **floor**. **Scope, stated honestly**
(Codex #3): this fix regionalizes the runtime **enum** validation (the live cache fetches
the user's host → build/run-base + service-type existence is checked against the user's
instance). It does NOT regionalize **structure** — export/launch structure-only validation
and the recipe structure check compile the canonical EMBEDDED schema. **Assumption:** for
supported public Zerops regions the YAML *structure* (field shape, required, the stable
enums) is identical and only the volatile enum VALUES differ — which holds today (validated
by the structure-only strip targeting exactly the volatile nodes,
`validate_structure.go:22-28,92-125`). **Out of scope / documented limitation:** a PRIVATE
instance whose schema diverges *structurally* from canonical is NOT covered by this change;
handling that would require host-deriving the embedded/structure path too (a larger,
separate effort).

---

## 3. Design — concrete changes

### 3.1 `internal/schema` — URL builder + host parameter
- **Add** `const CanonicalAPIHost = "api.app-prg1.zerops.io"` (the shared-repo reference host; also the empty-host default so behavior is unchanged for default users).
- **Add** `func URLs(apiHost string) (zeropsURL, importURL string)`. **Normalization MUST mirror `platform.resolveEndpoint` (`internal/platform/zerops.go:48-64`), not strip-and-force-https** (Codex #1: forcing HTTPS while the platform client honors `http://` recreates exactly the host mismatch this plan removes):
  - empty → `CanonicalAPIHost`.
  - **preserve an explicit scheme** (`http://` / `https://`); add `https://` ONLY when no scheme is present.
  - keep host **port** intact; trim only a trailing `/`.
  - append `/api/rest/public/settings/zerops-yml-json-schema.json` and `…/import-project-yml-json-schema.json`.
  - Reuse `platform.resolveEndpoint` logic — but `schema` must not import `platform` (layering), so replicate its ~6 lines in-package with a shared test matrix.
  - **Byte-exact default:** `URLs("")` MUST return the two current const strings verbatim (pinned by test) so default users are unchanged.
- **Remove** the exported `ZeropsYmlURL` / `ImportYmlURL` consts (internal package; replaced by the builder). Verified: ONLY `internal/schema` (cache.go, sync.go, schema.go) consume them — no external consumers (Codex #4).
- **Thread the host:**
  - `FetchSchemas(ctx, apiHost string)` → builds URLs via `URLs(apiHost)`.
  - `FetchRawSchemas(ctx, apiHost string)` → same.
  - `Cache` gains an `apiHost string` field; `NewCache(ttl time.Duration, apiHost string)`; `Cache.Get` calls `FetchSchemas(ctx, c.apiHost)`.
  - `embeddedSchemas()` (the seed) does NOT fetch → unchanged, no host.

### 3.2 Callers
- `internal/server/server.go:140`: `schema.NewCache(schema.DefaultCacheTTL, s.authInfo.APIHost)`. **`s.authInfo` is non-nil by construction** — server startup already dereferences it at `server.go:79,138` (Codex #1), so no nil-guard; pass `s.authInfo.APIHost` directly (empty → canonical via the builder).
- **Recipe + workflow inherit the host for free** (Codex #5): `recipeStore.SetSchemaProvider` (`server.go:174`) and `RegisterWorkflow` (`server.go:187`) both close over / receive the SAME `schemaCache`. So threading the host into `NewCache` ALONE makes recipe base-validation + workflow recipe-plan validation use the user's host — no separate fetch to touch.
- `cmd/zcp/schema.go` (`runSchemaSync`, `runSchemaCheck`): use the **canonical** host — `FetchRawSchemas(ctx, schema.CanonicalAPIHost)`. (Dev-tooling pin per §2; NOT auth — must not read `ZCP_API_HOST`.) The CI drift workflow inherits this (canonical-vs-committed comparison stays apples-to-apples).
- **Testability seam** (Codex #6): the CLI handlers `runSchemaSync`/`runSchemaCheck` call `log.Fatalf`/`os.Exit` and write committed repo files, so they're not unit-testable as-is. Extract the core into testable functions — `schemaSync(host, outPaths) error` / `schemaCheck(host) (DriftReport, error)` — with the `os.Exit`/`log.Fatalf` wrapper staying thin. The canonical-pin test exercises the extracted function (asserting it targets `CanonicalAPIHost` even with `ZCP_API_HOST` set) via the `URLs` seam, with NO network and NO repo-file writes.

### 3.3 Docs / invariants
- Update `CLAUDE.md` "Live Zerops schemas" block + the schema-validation invariant bullet to state the URL is host-derived (runtime) / canonical-pinned (dev tooling).
- Update `docs/schema-integration.md` if it names the consts.
- Update the doc-comment at `cmd/zcp/check/yml_schema.go:19-21` which names the canonical schema URL (Codex #4 — comment-only, no fetch, but should reflect host-derived runtime schemas).

---

## 4. Migration — phases (TDD, each compiles + green)

1. **URL builder + canonical const** (`internal/schema`). RED: `TestURLs` — bare host, host with scheme, host with trailing slash, empty→canonical all produce the two correct URLs. No caller changes yet (keep a thin shim if needed to compile). GREEN.
2. **Thread host into fetch + cache.** Change `FetchSchemas`/`FetchRawSchemas` signatures (+ `NewCache`); update internal callers (`Cache.Get`, the sync CLI). RED: `NewCache(ttl, host)` stores host; a cache built with a custom host targets that host (assert via the builder, no network). GREEN. Update the existing cache/sync tests to the new signatures.
3. **Server caller** → `s.authInfo.APIHost` (nil-safe). Build + server tests green.
4. **CLI caller** → canonical pin in `schema sync`/`check`. Build + cmd tests green. Live-verify: `zcp schema check` still works against canonical.
5. **Remove the old consts + docs/invariant updates + a pinning test** asserting no hardcoded `app-prg1` literal remains in the fetch path (grep-style test or a lint). `make lint-local` + full suite + race green.

---

## 5. Risks / edge cases (each must be handled)

- **`auth.APIHost` empty / `authInfo` nil** (local mode, no creds) → builder defaults to canonical. Server passes `""` safely.
- **Host carries a scheme** (`https://api…`) → builder strips it (mirror `platform.resolveEndpoint`'s normalization, but in-package — schema must not import platform).
- **User's host doesn't serve `/public/settings/…`** → fetch fails → cache keeps the embedded floor (existing poison/error handling). Graceful; recipe base-check falls back to the canonical floor.
- **`schema sync` must NOT pick up `ZCP_API_HOST`** — explicit canonical const, not auth resolution. A test should pin that `runSchemaSync` targets canonical even with `ZCP_API_HOST` set.
- **Backward compat:** default users (no `ZCP_API_HOST`) see byte-identical behavior (canonical default). Exported-const removal is internal-only.

---

## 6. Test plan

- `TestURLs` — normalization matrix: `""`→canonical (byte-exact vs the current const strings), bare `host`, `https://host`, **`http://localhost:8080/`** (scheme preserved — the key Codex #1 case), `https://host:port/` (port kept), trailing-slash trimmed. Each → the two correct URLs.
- `TestNewCache_UsesAPIHost` — `NewCache(ttl, host)` stores `host`; the cache's fetch target derives from `URLs(host)` (assert via the builder, no network).
- `TestSchemaCLIPinsCanonical` — the extracted `schemaSync`/`schemaCheck` core targets `CanonicalAPIHost` even with `ZCP_API_HOST` set; no network, no repo-file writes (uses the seam from §3.2).
- **Pinning invariant** (Codex #6): NOT a raw "no `app-prg1` literal in the fetch path" grep (it would false-fail on `CanonicalAPIHost`, embedded `$id`s, docs). Instead pin **"every schema fetch routes through `URLs`"** — e.g. an AST/grep test asserting `fetchURL` is only ever called with a `URLs(...)` result, no inline URL literal.
- Update existing tests to the new signatures: `cache_seed_test.go:28` (`NewCache`), the `sync`/`schema` CLI tests (none exist yet — added by this plan), no `catalog.Sync` (already deleted).
- Full `go test ./... -short` + `-race` on touched packages + `make lint-local`.

---

## 7. Verifier (Codex) verdict — resolved

Codex: **NO-GO until edits → now applied.** Resolutions:
- **Split (runtime=auth-host / dev-tooling=canonical): CONFIRMED, do not reverse.** `app-prg1` is the public canonical API base per Zerops docs; ambient `ZCP_API_HOST` must not make different devs commit different `testdata`/`active_versions`.
- **No `--host` override needed** — the pinned canonical const is sufficient.
- **No missed fetch consumers** outside `internal/schema` (one doc-comment in `cmd/zcp/check/yml_schema.go` to update).
- Edits applied: URL builder mirrors `resolveEndpoint` (preserve scheme, keep port, trim trailing slash, byte-exact empty default); `authInfo` non-nil (no guard); CLI testability seam; structure-agnostic claim narrowed to a stated public-region assumption with private-instance structural divergence explicitly out of scope; pinning test = "all fetches route through `URLs`".
