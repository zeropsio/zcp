# Retest pack: onboarding-menu-v3

Zero-context bar: every step below runs with no other file open, in minutes.
Branch: `feat/onboarding-menu-v3` @ e8f97c91. Everything is already deployed
on the localflow `zcp` container (binary v9.139.1-20-ge8f97c91, hash-verified).

## Run

| command | expected line |
|---|---|
| `go test ./internal/tools -run Playbook -short -count=1` | `ok` |
| `go test ./internal/eval/... ./internal/content/... -short -count=1` | `ok` ×2 |
| `go test ./internal/sync/... ./internal/knowledge/... -short -count=1` | `ok` ×2 (needs `zcp sync pull` first on a fresh clone) |
| `go test ./internal/workflow/... ./integration/... -short -count=1` | `ok` ×2 |
| `make lint-local` | `0 issues.` |
| `make e2e-zcp-fast` | `--- PASS` lines, no `FAIL` (VPN up) |

## Drive

All on localflow (VPN up), in the code-server container:

1. **AC1/AC8 — menu**: open a terminal in the container, `cd /var/www`, run
   `claude "Onboard me to Zerops."` — expect the v3 menu VERBATIM: greeting
   "Welcome to Zerops! Zerops builds and runs apps…", options **Build
   something** / **Try a ready-made recipe** / **What are Zerops & ZCP?**,
   escape line `Or just tell me what you want, in plain words…`. No
   "Bring an app", no "Take a quick tour".
2. **AC4 — orientation**: reply `What are Zerops & ZCP?` — expect a short
   plain-language explanation (project/services/containers, hostname
   networking, build→deploy→run, subdomain, what ZCP is, consent boundary),
   NO service versions, NO pricing, and a closing re-offer of the two
   active options. Nothing gets created.
3. **AC2 — Build something happy path**: reply `udelej dashboard pocasi v
   bun` (or any one-liner + technology) — expect, in order: footprint
   consent BEFORE any commit (dev + stage + database named, dev-only
   offered only if you ask); after your yes, the recipe route commits with
   `bun-hello-world`; because localflow already has `db`, expect the
   EXISTS disclosure ("recipe's migration may write into the existing
   database") and a renewed consent; the three concepts explained BEFORE
   the blocking import; after ACTIVE, the STAGE URL handed over (dev never
   presented — it answers 502 by design) and verified before presenting.
   Then the agent continues toward the weather dashboard in the develop
   loop.
4. **AC3 — ownership**: after a recipe lands, ask `how do I make this mine?`
   — expect: offer to run `git-push-setup` to your own repo (asks YOU for
   the token, never invents one) / export, plus the link
   `https://app.zerops.io/recipes/bun-hello-world`.
5. **AC7 — structured URLs**: in the same session ask for workflow status
   while close is active — the payload carries runtime URLs with the stage
   entry marked as handoff.
6. **Cleanup after the drive** (the demo creates real services): ask the
   agent to delete the services it created (it must name them and ask you
   per-service), and optionally drop the `greetings` table it may have
   re-created in `db` (`psql -h db -U zps -d db -c 'DROP TABLE greetings;'`
   — check it only has the one hello row first).

Deferred (needs one thing from you): **e2e
`TestBootstrapRecipeRoute_AuthoredSlug_LiveURL`** — create an EMPTY
disposable project (e.g. `zcp-e2e-recipes`) in KRLS (my token lacks
project-create permission), then:
`ZCP_E2E_RECIPE_PROJECT_ID=<id> go test -tags e2e -run TestBootstrapRecipeRoute_AuthoredSlug_LiveURL ./e2e/ -count=1 -v -timeout 20m`
— expect PASS with stage 200 / dev 502 and a clean teardown (the RED/GREEN
owner procedure is documented in the test file header).

## What changed

- S1: onboarding playbook rewritten to menu v3 (verbatim menu, language→slug mapping, staged consent, stage-URL handoff, failure ending) + NEW orientation playbook `zerops://playbooks/orientation`; full-line content pins.
- S3: five onboarding behavioral scenarios rewritten to v3; retired v2 labels guarded by a content drift test.
- S4: `zcp sync pull recipes` persists `categories:` frontmatter + conditional `## Take ownership` section; push-parser fragments stop at the new heading (fence-aware); byte-idempotent; docs/recipes README updated.
- S5a: recipe provision guide renders services-only YAML + executable `zerops_env` pre-steps; `bootstrap-recipe-import` atom states the true importer contract.
- S5b: `BootstrapResponse.RuntimeURLs` (hostname/role/url/handoff) populated at L4 via `ops.ResolveSubdomainURL`, present on post-provision/status/close; guidance derived from it.
- S6: env-gated e2e for the whole chain (blocked on the disposable project).
- Corpus refresh: 7 new upstream import ymls committed (elixir-minimal + wordpress no longer orphans).

## Rollback

`git revert 9deddab2..e8f97c91` on `feat/onboarding-menu-v3` (or simply do
not merge the branch). Container: re-run `./eval/scripts/build-deploy.sh`
from main to restore the previous binary. No data migration involved.

## Docs

Promoted at GATE 1: `docs/spec-onboarding.md` (rewritten: preamble three
artifacts, §2 ordering, §3 verbatim menu + staged consent, §4 branches,
§5 family, §7 O-table) · `docs/spec-workflows.md` §8 RCO-6 (services-only
provision YAML + env pre-steps) + RCO-7 (structured runtime URLs).
