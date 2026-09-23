# Mate UI built around the flow — what is still open

**Surfaced**: 2026-09-23 (the walk of `mate.zerops.io/zerops` and the owner's D29). The design is
in `spec-mate.md` D29, §5.4, §10.11 and MB-32, and in the fork's `design-system.md` (glossary
_preview_ / _stage_, the 2026-09-23 decision row). The UI landed on mate `main` (5017f47b7, 2026-09-24):
`groupFlow`, the projects page's Overview and Projects views with the polish round (no dashed
places, one verb per row at its cell's end, the _Next steps_ strip carrying each row's verb), the
left menu redrawn from `groupFlow`, and the conversation's next-step banner. This entry keeps only
what did not.

**Trigger to promote**: the next projects-page or thread slice, or the state-model programme
merging `main`.

## Not built
- **The surfaces still read the old derivation.** They draw from `client-runtime/src/zerops/groupFlow.ts`;
  the state model's `client-runtime/src/zerops/flow/groupFlow.ts` (per-key facts, a release offer
  known only when every input is) is on `main` but no surface reads its stores. The state-model
  programme moves them when it merges `main`: the page's derivation becomes a projection over that
  `GroupFlow`, `mateNextStep` moves beside it, the views stay.
- **`main` is never read for the flow.** `groupFlowInputOf`, which all three surfaces feed
  `groupFlow` through, leaves `mainHasCode`/`mainHead` unread; `planMainHeadReads` runs only for a
  group with a production and feeds the release offer. So "After the first merge" is never drawn,
  and the flow's own _Add production_ step misses code a recipe planted at birth or a merge that
  has scrolled off the recent list. The project's menu offers _Add production_ on
  `creatableRoles` + may-create as the stop-gap; once `main` is read, decide whether that menu item
  stays or goes.
- **Thread**: the header strip _Preview → PR → main → Production_; the **delivered** card when a
  stage-half deploy opens or updates the pull request ("Delivered · pull request #4 · merged"); the
  import/workspace card naming the pair's roles (`appdev · code`, `appstage · preview`).
- **The flow's facts wait on the delivery protocol** (the owner's Gitea delivery audit, 2026-09-23).
  `groupFlow` counts merged commits and reads production from the platform, so a squash with an
  identical tree (Juno, `fsadfdasfsa` #3/#4) reads as "1 change merged, not live" and offers a
  release of nothing. The audit moves the truth to Gitea commit statuses on the exact sha
  (`mate/preview`, `mate/release/{tag}`, `mate/deploy/{env}/{svc}`) and gives a release a lifecycle
  (Judging → Rolling out → Live / Failed with _Retry_ / _Ask Mate to fix_).
- **Layout majors from the polish round's QA**: at 390 px the sidebar's floating toggle sits over the
  page's content — `AppSidebarLayout`, global to every page, so it waits on the owner; the repair
  failure's line is cut and misplaced; at 1024 px `main`'s "Nothing waiting to release" is cut.
- `apps/web/src/design/sidebarHarness.tsx` passes neither `mayCreate` nor `health`, so the design
  harness's menu never offers _Add production_.

## Open
- The left menu keeps the D26 timeline and adds the next-step dot; the slice plan said "navigation
  only (name + a next-step dot)". Which one the owner wants is undecided.
- A broken production with a release on offer keeps _fix-deploy_ as the next step and draws
  _Release_ beside it. The other reading — release as the next step, since it may be what clears
  the failure — was not chosen; confirm with the owner.
- Whether _Add production_ should be offered before the recipe's production tier is on `main`. The
  flow's step requires it; the menu's stop-gap does not.
- A production that runs nothing reads "Nothing live yet" (`groupFlow`) in its cell, while a stop's
  line and the state model say "Nothing deployed yet" (`flow/deployment.ts`). One word is to go;
  the recommendation is the state model's.
