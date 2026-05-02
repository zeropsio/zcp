# Recipe-authoring generate checks have the same project-root fallback as preflight

**Surfaced**: 2026-05-02 — Codex adversarial review of the deploy-preflight
source-mount fix (commits b26a50d6 + b65afaf2 + 3d51f486 + c547b157 + e1ec8ac7).
The deploy-side regression was just removed; the recipe-authoring side has the
same anti-pattern still in place.

`internal/tools/workflow_checks_recipe.go::checkRecipeGenerateCodebase` (lines
176–186) probes `<projectRoot>/<hostname>dev/`, `<projectRoot>/<hostname>/`,
then falls back to `<projectRoot>` and parses whichever directory matched
first — the same "try mount, fallback to root" shape that masked the
deploy-preflight bug. Codex review:

> "checkRecipeGenerateCodebase has the same pattern: default project root,
> probe `<hostname>dev` / `<hostname>`, parse the selected dir, then raw-read
> YAML from that selected dir. These are not deploy preflight, but they are
> YAML gates and should probably share the same source-mount/local split."

**Why deferred**: this lives in the recipe-authoring (`internal/recipe/` and
`internal/tools/workflow_checks_recipe.go`) layer, which CLAUDE.md flags as
"v3 engine, separate scope." Production recipe authoring always mounts
codebases at `<projectRoot>/<hostname>dev/`, so the projectRoot fallback is
defense-in-depth that never triggers — the bug is theoretical, no observed
failure. The deploy-side fix did not depend on touching this layer.

**Trigger to promote**: any recipe-authoring run hits a "wrong yaml read"
class symptom (e.g. checks pass against an unrelated stray
`<projectRoot>/zerops.yaml` and the codebase yaml never gets validated). Or
a recipe-engine refactor that would exercise the path under conditions where
the fallback could fire.

## Sketch

Mirror the deploy-preflight split:
- Drop the projectRoot fallback from `checkRecipeGenerateCodebase`. Each
  codebase target has a known mount path; if the mount is empty, fail the
  check, never silently revalidate against a stray root yaml.
- Keep the dual-prefix probe (`<hostname>dev` / `<hostname>`) since that
  reflects the live recipe-authoring naming convention (`appdev`, `apidev`,
  `workerdev`).
- Sweep for similar patterns elsewhere in `internal/tools/workflow_checks_recipe.go`
  (raw-read at line 250-252, the visual-style ASCII check at line 254). They
  consume `ymlDir` derived from the same probe.

## Risks

- Recipe authoring tests almost certainly write yaml at projectRoot
  (matches the bug). Behavior change → test churn comparable to the
  deploy-preflight fix.
- The recipe-engine v3 (`internal/recipe/`) probably has its own assumptions
  about where yaml lives mid-authoring. Verify before touching.

## Refs

- Codex review (2026-05-02): `/tmp/codex-final-review.md`
- Pattern site: `internal/tools/workflow_checks_recipe.go:179-186`
- Companion code: `internal/tools/workflow_checks_recipe.go:202` (`checkZeropsYmlFields`),
  `:250-252` (raw read), `:254` (`checkScaffoldArtifactLeak`),
  `:256` (`checkVisualStyleASCIIOnly`)
- Architecturally adjacent: `docs/spec-architecture.md` recipe section
- Companion fixes: source-mount preflight fix chain b26a50d6→e1ec8ac7
