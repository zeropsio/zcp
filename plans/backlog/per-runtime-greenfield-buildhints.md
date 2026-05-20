# Per-runtime greenfield buildhint atoms — pay-as-discover

- **Surfaced**: 2026-05-20, alongside shipped `develop-nodejs-greenfield-buildhint` (plan: `plans/runtime-bases-axis-and-nodejs-buildhint-2026-05-20.md`)
- **Why deferred**: Phase 1 shipped the `runtimeBases:` axis infrastructure + one Node atom. Other runtimes haven't surfaced the same **install-command failure class** in eval retros. Pre-emptive per-runtime atoms would add corpus noise without empirical justification.
- **Trigger to promote** (refined per codex review 2026-05-20): same greenfield **install-command failure class** repeats ≥2 times for the same runtime in eval retros. Not any runtime-specific friction — only install-command failures where LLM chose the wrong default (strict lockfile-mode vs lenient install-mode).

## Pattern (when atom is added for a runtime)

Each future per-runtime atom follows the Node atom shape exactly:

```yaml
---
id: develop-<runtime>-greenfield-buildhint
priority: 2
phases: [develop-active]
runtimeBases: [<runtime-base>]
deployStates: [never-deployed]
multiService: aggregate
title: "<Runtime> greenfield — use <install>, not <strict-install>"
references-fields: [workflow.ServiceSnapshot.TypeVersion]
---

### <Runtime> — `<install>`, not `<strict-install>`

Fresh <runtime> scaffold with no committed `<lockfile>`: `<install>` in
`build.buildCommands`. `<strict-install>` fails until a lockfile is committed.
```

Body is TRIGGER + ACTION + FAILURE MODE in 1-2 lines per CLAUDE.local.md. No YAML snippets, no general scaffold guidance — covered by `develop-first-deploy-scaffold-yaml` and runtime-class atoms.

## Candidate runtimes (NOT promoted yet — informational)

These runtimes have known strict-install commands but **no eval evidence** of repeated install-command failures:

| Runtime base | Greenfield install | Strict (after lockfile) | Lockfile |
|---|---|---|---|
| `python` | `pip install -r requirements.txt` | same (use venv for reproducibility) | n/a (no lockfile concept) |
| `go` | `go mod download` | same (mod download is idempotent) | n/a |
| `bun` | `bun install` | `bun install --frozen-lockfile` | `bun.lock` |
| `deno` | `deno cache <entry>.ts` | n/a | `deno.lock` |
| `rust` | `cargo fetch` | `cargo build --locked` | `Cargo.lock` |
| `ruby` | `bundle install` | `bundle install --frozen` | `Gemfile.lock` |
| `dotnet` | `dotnet restore` | `dotnet restore --locked-mode` | `packages.lock.json` |
| `java` | `./mvnw clean package -DskipTests` | same | n/a (pom.xml deps strict by default) |
| `php-nginx` | `composer install` | `composer install --no-dev --optimize-autoloader` | `composer.lock` |
| `php-apache` | same as php-nginx | same | same |

PHP family: each php-* run base needs its own `runtimeBases:` entry (no family expansion in axis match).

## Non-install-command friction in other runtimes (NOT in scope)

Eval retros show non-install friction for some runtimes but those are **different classes** — separate fix paths:

- **Python**: gunicorn `start` command shape friction (start command issue, not install)
- **Go**: dev-mode iteration shape (build vs run, not install)
- **Bun**: dev-mode `start: zsc noop --silent` vs healthCheck confusion (lifecycle issue, not install)

These would be addressed by their own dedicated atoms with different axes (`runtimeClass` × `mode` filters), not by extending the greenfield-buildhint pattern.

## Sketch — promotion path

When a runtime promotes:

1. Add `develop-<runtime>-greenfield-buildhint.md` atom (~10 lines, fitting pattern above)
2. Add atom ID to `internal/workflow/scenarios_test.go::TestScenario_PinCoverage_AllAtomsReachable` inventory
3. `ZCP_UPDATE_ATOM_GOLDENS=1 go test ./internal/workflow -run TestScenarios_GoldenComparison` to refresh affected goldens
4. Verify atom fires only in fixtures where the runtime is `never-deployed` (golden diffs review)
5. Verify atom does NOT fire in other-runtime fixtures (negative test)
6. Eval re-run scenario for that runtime to confirm first-try success rate improvement

~30 min total per runtime, axis infrastructure already in place.

## Risks

- **Atom corpus sprawl**: limit by enforcing `≥2 install-command failures per runtime` trigger. Don't add for "I think this might help" speculation.
- **MCP cap pressure**: `develop_first_deploy_two_runtime_pairs_standard` fixture is already over 28 KB soft cap (allowlisted, see `knownOverflowFixtures` in `corpus_coverage_test.go`). Adding more per-runtime atoms to this fixture would compound. Each future atom needs cap-pressure check during refresh.

## Refs

- Plan: `plans/runtime-bases-axis-and-nodejs-buildhint-2026-05-20.md` (axis infrastructure + Node atom)
- Axis: `internal/workflow/atom.go::AxisVector.RuntimeBases`
- Match logic: `internal/workflow/synthesize.go::matchesRuntimeBase`
- Failure classifier (defense-in-depth): `internal/ops/deploy_failure_signals.go::build:npm-ci-missing-lockfile`
