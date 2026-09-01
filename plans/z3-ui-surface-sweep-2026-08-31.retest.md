# Retest pack: z3-ui-surface-sweep

Zero-context bar: run these checks against Z3 commit `983927c92`. The current
hosted `z3.krls.cz` build does not contain this commit until it is released
separately.

## Run

From `/Users/macbook/Documents/Zerops-MCP/z3`, run each command independently.

| command | expected line |
|---|---|
| `cd apps/web && vp test run --project unit src/zerops/chatChrome.test.ts src/components/chat/ProviderStatusBanner.test.tsx src/components/zerops/ZeropsLifecycleStrip.test.tsx src/components/zerops/ZeropsPanel.test.tsx src/components/zerops/ZeropsServiceMap.test.tsx src/components/zerops/ZeropsQuickActions.test.tsx src/components/zerops/ZeropsAgentAuthCard.test.tsx src/components/zerops/ZeropsToolCard.test.tsx src/components/chat/MessagesTimeline.test.tsx src/components/zerops/ZeropsProjectPicker.test.tsx src/components/zerops/ZeropsProjectsPage.test.ts src/components/zerops/ZeropsProvisioningPanel.test.tsx src/components/zerops/showcaseScenes.render.test.tsx` | `Tests  186 passed (186)` |
| `cd apps/web && pnpm typecheck` | `$ tsgo --noEmit` with exit `0` |
| `git diff --name-only 538db7578a5fec50b7dc8770ab3fa6f3700663ff..983927c92 -- 'apps/web/**/*.ts' 'apps/web/**/*.tsx' \| xargs vp lint --report-unused-disable-directives` | no output |
| `vp test run scripts/z3-zone-architecture.test.ts` | `Tests  33 passed (33)` |
| `node scripts/check-css-motion.ts` | `no-infinite-motion: ledger reconciled (5 entries)` |
| `node scripts/check-guard-exceptions.ts --rule no-infinite-motion` | `no-infinite-motion: ledger reconciled (19 entries)` |
| `node scripts/check-guard-exceptions.ts --rule no-legacy-vocabulary` | `no-legacy-vocabulary: ledger reconciled (93 entries)` |
| `node scripts/check-css-tokens.ts` | `no-theme-escape-hatches: ledger reconciled (35 entries)` |
| `node scripts/check-guard-exceptions.ts --rule no-theme-escape-hatches` | `no-theme-escape-hatches: ledger reconciled (410 entries)` |
| `node scripts/generate-theme-tokens.ts --check` | `Theme token projections are current.` |
| `cd apps/web && pnpm build` | `✓ built` |

The generic `/flow` Go/Make battery does not exist in this hard-fork web
repository; the commands above are the scoped Z3 equivalents required by its
project policy.

## Drive

Use a staged build of `983927c92` with a Zerops-connected thread. Check desktop
first, then repeat steps 1, 4 and 5 at a 390×844 viewport.

1. AC1 — open a thread with a provider warning and a coding-agent sign-in requirement. Expect both notices to occupy their own layout rows; the first timeline message remains fully visible. Agent authorization appears exactly once inside the project-map panel, never over the conversation.
2. AC2 — click the compact lifecycle band below the thread header. Expect the project-map panel to open and the lifecycle phrase/status to remain visible in the band.
3. AC3 — inspect the project map. Expect liveness first, then `Runtimes`, `Data`, `Infrastructure`; every service has a dot plus status word, and `Zerops Control Plane` has mint emphasis. `Deploy`/`Show logs` actions only prefill the composer.
4. AC4 — inspect one `BUILD_TRIGGERED` deploy receipt and one terminal deploy receipt. Expect the first to say `Build triggered` with a busy/running step, never `Deployed`; expect the terminal success to be green and say `Deployed`. An unknown/invalid result still uses the generic tool block.
5. AC5 — open `/zerops`, connect a ready project, and force or reproduce one identity-exchange failure. Click `Try again` once. Expect one new identity exchange, a disabled action while it runs, and no reset to the earlier project-wait phase. Project cards remain one column at 390px and two columns where space permits.
6. AC6 — repeat the populated thread and `/zerops` review in light and dark mode with reduced motion enabled. Expect readable status contrast, no infinite animation and no horizontal page overflow.

## What changed

- S1: removed the timeline-owned coding-agent overlay and added a compact lifecycle band.
- S2: rebuilt the project map around shared liveness, status, card, chip and mint primitives with one authorization tray.
- S3: replaced ad-hoc Zerops tool results with process cards and safe generic fallback.
- S4: grouped and clarified responsive project cards and provisioning states.
- S5: moved provider warnings/errors into normal layout flow so they cannot cover messages.
- S6: made asynchronous `BUILD_TRIGGERED` deploy receipts visibly in progress.
- S7: made `Try again` repeat the failed ready-container identity exchange instead of restarting provisioning.

## Rollback

No deployment or push was performed in this run. To create one local inverse
commit, from the repository root run, in order:

```sh
git revert --no-commit -m 1 983927c92 e6e99f450 210c8f0da b8f07bf20 8f8f53092 2a1abc228
git revert --no-commit abd1fbd27 ed40de5a6
git commit -m "revert: Z3 UI surface sweep"
```

## Docs

Promoted contract: `/Users/macbook/Documents/Zerops-MCP/zcp/docs/spec-z3.md`
§5.4 — lifecycle band, single panel-owned agent authorization, service rows,
project/provisioning cards and Zerops process-card presentation.
