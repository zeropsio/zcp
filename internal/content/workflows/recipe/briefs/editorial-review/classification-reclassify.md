# Classification reclassify

You independently classify every published gotcha and every Integration-Guide item the reviewer encounters on the surface walk. You compare your classification to the writer's classification recorded in the manifest. You report deltas.

## Why "independent"

The writer's self-classification is the hypothesis under test. Reviewing a classification by reading the writer's own reasoning rehearses the writer's reasoning; you learn nothing the writer did not already see. Independent classification means you form your own classification from the item's published content and its observable mechanism, and only then open the manifest to compare.

Concretely: when you land on a gotcha or an IG item during the surface walk, first classify it using the rules below. Write the classification into your working notes. Then — and only then — open `{{.ManifestPath}}` and retrieve the writer's classification for that same item. If they agree, move on. If they disagree, record a reclassification delta.

## The seven classes

Every published fact classifies as exactly one of these. The test is the routing criterion, not the prose tag.

### Platform-invariant

The fact is true of Zerops regardless of this recipe's scaffold choices. A different framework entirely, a different codebase shape, would hit the same trap. Route: `GOTCHAS.md` with a citation to the matching platform topic (see the citation-audit atom).

### Platform-×-framework intersection

The fact is specific to this framework AND caused by a platform behaviour. Neither side alone would produce it. Route: `GOTCHAS.md`, naming both sides explicitly.

### Framework-quirk

The fact is about the framework's own behaviour, unrelated to Zerops. Any user of that framework hits it regardless of where they deploy. Route: **discard**. It belongs in framework docs. If it ships as a Zerops gotcha, the finding is wrong-surface.

### Library-metadata

The fact is about dependency resolution, version pinning, peer-dependency conflicts. Route: **discard**. It belongs in the dependency manifest comments, not in recipe content.

### Scaffold-decision

The fact is "we chose X over Y for this recipe; a reader should understand why." A non-obvious design choice in the recipe's own code. Route: per-codebase `zerops.yaml` comments (if the choice is a config decision), or Integration-Guide prose (if the choice is a code principle a porter would need to apply to their own code), or `CLAUDE.md` (if the choice is operational). NOT `GOTCHAS.md` — a scaffold decision is not a platform trap.

### Operational

The fact is how to iterate on / test / reset this specific repo locally. Route: `CLAUDE.md`.

### Self-inflicted

Our code had a bug; we fixed it; a reasonable porter with different code would not hit it because their code does not have that specific bug. Route: **discard entirely**. There is no teaching for a porter. The fix belongs in the code, not in published content.

## How to classify — concrete rules

- **Separate mechanism from symptom.** The mechanism (what the platform does) is what determines platform-invariant versus framework-quirk; the symptom (what the recipe's code did wrong) is what determines self-inflicted. Classify on the mechanism.
- **Ask "would a porter with different scaffold code hit this?"** If no: scaffold-decision or self-inflicted. If yes: platform-invariant or intersection.
- **Check the citation map.** If a platform topic already documents the mechanism, the fact is probably platform-invariant — route to `GOTCHAS.md` with citation, do not duplicate the guide's content.
- **Self-inflicted litmus test**: can the finding be summarised as "our code did X, we fixed it to do Y"? If yes, discard.
- **When in doubt between intersection and framework-quirk**: does the platform side contribute materially to the failure mode, or is the platform merely incidentally hosting the code when the framework's own behaviour bites? If the platform is incidental, it is a framework-quirk.

## Reporting the delta

For every published item, record a reclassification row in your return payload:

- `item_path` — the file + section where the item lives on the mount.
- `writer_said` — the class the writer's manifest records.
- `reviewer_said` — the class you independently assigned.
- `final` — either `writer_said` (if you agree), or `reviewer_said` (if you disagree and the reclassification is confident), or the string `ambiguous` (if the classification is genuinely debatable and you want the caller receiving your payload to adjudicate).

If your reclassification routes the item to a different surface than the writer's classification did, that is a wrong-surface finding — report at the severity the reporting-taxonomy atom names. If your reclassification keeps the item on the same surface but disagrees on class, that is still a finding — severity depends on whether the disagreement affects routing.
