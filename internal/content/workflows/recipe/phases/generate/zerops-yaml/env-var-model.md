# Env-var model — cross-setup semantics

This atom covers the env-var model that applies across every setup in `zerops.yaml`. The goal is one mental model for where values come from and what writing them accomplishes — so self-shadow traps and name-invention classes never get written in the first place.

## envVariables contents — cross-service refs and mode flags only

Every setup's `envVariables:` map contains exactly two kinds of entries:

1. **Cross-service references** — `${hostname}_keyName` interpolations discovered via `zerops_discover`. These name the values the platform resolves at deploy time (database host, Redis URI, NATS server list, S3 endpoint, search host, …). The names in `plan.SymbolContract.EnvVarsByKind` — `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASS`, `DB_NAME`, `CACHE_HOST`, `QUEUE_HOST`, `STORAGE_HOST`, `STORAGE_API_URL`, and equivalents — are the exact keys the recipe's code reads from `process.env`. The map value is the `${hostname}_keyName` interpolation that resolves it.
2. **Mode flags** — `APP_ENV`, `NODE_ENV`, `DEBUG`, `LOG_LEVEL`, and the framework's own mode variables. These are literal strings whose value differs between `setup: dev` (verbose / `development` / `local`) and `setup: prod` (production values). The structural check that compares dev and prod maps requires the mode-flag entries to differ — identical maps between dev and prod fail the check.

`envSecrets` does not appear in `envVariables`. The platform injects secrets automatically; writing secret names back into `envVariables` creates a name-shadow loop.

## No same-name self-shadow lines

Every value in `envVariables` is a cross-service reference or a literal. A line where the key and the interpolation share a name — `DB_HOST: ${DB_HOST}` — creates a self-shadow: the platform interpolator resolves the right-hand side against the same-name service-level var first, which points back at the literal `${DB_HOST}` string, and the value the container receives is that string. The positive form: if you want a platform-provided variable available under its own name in the OS environment, write nothing — it is already injected. If you want to forward it under a different name (common for dual-runtime URL baking and framework-conventional aliases), write the line with the destination key on the left and the source interpolation on the right, where the two names differ.

A pre-attest grep over the file catches any remaining self-shadow line:

```
grep -nE '^[[:space:]]+([A-Z_]+):[[:space:]]+\$\{\1\}[[:space:]]*$' zerops.yaml
```

Non-empty output means a self-shadow is still present.

## Framework environment conventions

Use the framework's standard env-var names — not invented ones. If the framework has a base or app URL variable, set it to the appropriate Zerops-derived value (for public-subdomain URLs, `${zeropsSubdomain}` on the target service; for cross-service URL baking, the project-scope `STAGE_*` or `DEV_*` variable documented in the dual-runtime atom). The chain recipe's zerops.yaml template shows the correct names per framework family; the `SymbolContract` freezes those choices so every codebase in the plan reads the same names.

## Decision flow

1. Consult `zerops_discover` for the managed services in this plan — it returns the authoritative name map.
2. Cross-check against `plan.SymbolContract.EnvVarsByKind` — that is what every scaffold and feature already consumes.
3. Write each `envVariables` entry with the destination key on the left and the discovered interpolation on the right; destination names differ from source names whenever a forward-under-different-name pattern applies (dual-runtime URL baking, framework conventions).
4. Mode flags come last; they differ between dev and prod setups.

## Shared rules across all setups

- `envVariables` carries only the two kinds above (cross-service refs and mode flags).
- dev and prod `envVariables` maps must not be bit-identical — the mode flags carry the difference.
- Every reference name matches a `zerops_discover` key — no guessed names.
