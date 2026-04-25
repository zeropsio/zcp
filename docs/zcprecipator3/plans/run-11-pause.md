# run-11 pause — architectural reframe before dogfood

**Date**: 2026-04-25
**Status**: paused; no engine code changes since run-11-readiness
shipped

## What happened

Run-11-readiness shipped 22 commits + a CHANGELOG entry on 2026-04-25
(see [run-11-readiness.md](run-11-readiness.md) for the plan and the
preceding [CHANGELOG entry](../CHANGELOG.md) for what landed). Run 11
dogfood was queued next.

In the same session, an architectural audit walked the validator
inventory across runs 8 → 9 → 10 → 11 and recognized a pattern: each
run added a tranche of hardcoded knowledge catalogs (vocabulary lists,
phrase regexes, banned-heading lists, banned filenames) wired as
finalize-blocking gates. ~16 artifacts now live in the engine on the
wrong side of the TEACH/DISCOVER line drawn in
[../system.md](../system.md) §4. This is the same failure mode that
killed v2 across versions 35 → 39, one layer up.

## Why we paused before dogfooding

Two reasons, in order:

1. **Dogfooding now would compound the problem.** Run 11 against the
   current catalog-shaped engine would produce a run that learns to
   evade the existing trigger strings and a run-12-readiness plan with
   another tranche of catalogs. We'd burn a real recipe authoring run
   to teach the agent to game regexes.

2. **We needed an unambiguous test, not another argument.** Without
   the TEACH/DISCOVER line written down, every future "should we add
   this validator" question becomes a case-by-case argument that
   defaults to "yes, ship it as a gate." The line in system.md §4
   gives a falsifiable test: a piece of knowledge is TEACH-side iff
   it's the same for every recipe AND can be expressed as a positive
   shape (not a negative pattern); otherwise it's DISCOVER-side.

## What landed during the pause

- [../system.md](../system.md) — north-star doc. What v3 is, output
  shape (corrected against `recipes/_template/` and
  `laravel-showcase-app/` references), runtime sequence, the
  TEACH/DISCOVER line, verdict table for every current artifact,
  knowledge-flow channels, reading order for fresh instances.
- [CHANGELOG entry "2026-04-25 — architectural reframe"](../CHANGELOG.md)
  — durable decision record. Names the catalog drift, names the
  decision, lists the wrong-side artifacts, makes clear no code
  changed yet.
- This document.

## What did NOT land

- **No engine code changes.** Validators, classifiers, and brief
  composers are byte-identical to run-11-readiness's final commit.
- **No atom changes.** V-5's three concrete run-10 anti-patterns still
  sit in `content/briefs/scaffold/content_authoring.md` and
  `content/briefs/feature/content_extension.md`.
- **No run 11 dogfood.** Queued; not run.

## What comes after the pause

Two paths the user can choose between:

### Path A — cleanup first, then dogfood

1. Cleanup pass guided by [system.md §4 verdict table](../system.md):
   - Wrong-side artifacts that have a recoverable TEACH-shape:
     rewrite as engine-emitted positive rules. (Few candidates in the
     current set; IG item #1 is the model.)
   - Wrong-side artifacts that belong on DISCOVER: demote from
     finalize-blocking `Violation` to record-time `Notice`. V-1
     (`ClassifyWithNotice`) already does this; the pattern extends to
     V-3 / V-4 / O-2 / P-3 / `metaVoiceWords` /
     `claudeMDForbiddenSubsections` / `sourceForbiddenPhrases`.
   - Pure-style artifacts with no semantic load: delete.
     (`yamlDividerREs`, debatable cases.)
2. Phrase-pinning tests — delete. Tests that pin exact strings in
   atom or brief content (`TestKnowledge_CoreThemes_DeployignoreParagraph_NoMirrorGitignore`,
   `TestBrief_Scaffold_DeployignoreTripwire`,
   `TestBuildFinalizeBrief_NoCiteGuideInstruction`) replicate the
   catalog problem at the test layer.
3. Vocabulary-list merge — `platformMechanismVocab` (V-1) and
   `platformMentionVocabBase` (V-3) overlap and will drift; if the
   demoted V-3 still uses one, merge them.
4. V-5 brief anti-patterns — reconsider whether the three concrete
   run-10 anti-patterns belong in scaffold/feature briefs at all.
   Run-specific knowledge in author-time briefs is a TEACH-side
   reification of DISCOVER-side material.
5. THEN dogfood run 11 against the cleaned engine.

### Path B — dogfood first, then cleanup

1. Dogfood run 11 against the current engine.
2. Use the run's output to test the TEACH/DISCOVER line empirically:
   does the engine catch wrong-side reifications on its own? Do the
   gates actually improve the deliverable, or just add iteration
   cost? What does the agent game?
3. Cleanup pass informed by what run 11 surfaces.

### Trade-off

Path A produces a cleaner run 11 with less iteration noise and
less risk of re-encoding cleaned-up rules into a fresh
run-12-readiness plan. Path B produces empirical evidence for which
specific gates are loud-no-ops vs which are actually useful, but
risks reinforcing the wrong mental model in the run output and the
analysis that follows.

Recommendation: **Path A**. Cleanup is bounded (~16 artifacts, all
identified, all with named corrective actions in system.md §4),
mechanical (the table tells you what to do per row), and reversible
(any demoted gate can be re-promoted if a future run proves the
detection is genuinely TEACH-side). The pause is cheap; the
reinforcement is expensive.

## Resuming

When the user is ready, the resume signal is one of:
- "Path A — start cleanup with [specific category]"
- "Path B — dogfood run 11"
- "Modify the line in system.md §4 first, then choose"

Until then, no engine work.
