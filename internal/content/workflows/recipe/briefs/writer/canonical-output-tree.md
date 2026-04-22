# Canonical output tree

The complete list of files you may create or modify. Any path not on this list is out of scope for your role.

## Per-codebase files (one set per hostname in `{{.Hostnames}}`)

For each hostname `{h}` in the plan's codebase list:

- `{{.ProjectRoot}}/{h}/README.md` — reader-facing content with three extract fragments (intro, integration-guide, knowledge-base). Published to the recipe page at finalize. The file is pre-scaffolded on the mount with the three marker pairs — you Edit the placeholder line between each marker pair; you do NOT touch or retype the markers themselves.
- `{{.ProjectRoot}}/{h}/CLAUDE.md` — repo-local operational guide. Plain markdown. Not published.

Per codebase, exactly two authored files. Content that would have lived in standalone porter-facing documents belongs inside the README integration-guide and knowledge-base fragments — those fragments ARE the porter surface.

## Recipe-output-root files

Exactly one file at the recipe output root:

- `/var/www/zcprecipator/{{.Slug}}/ZCP_CONTENT_MANIFEST.json` — classification manifest for every recorded fact. Path is fixed at the recipe output root.

## Surfaces NOT authored by you

The per-environment README files under `environments/` are authored at finalize time by the step above you. They are NOT a writer surface — do not create or modify them.

The top-level recipe README (the one a porter lands on from the zerops.io recipe page) is authored at finalize time by the step above you from the plan snapshot. It is NOT a writer surface — do not create or modify it.

The comment set inside each codebase's `zerops.yaml` file is authored at generate time by the step above you. You do not author or rewrite it here.

The comment set inside each environment's `import.yaml` file is applied at finalize time by the step above you from a structured `env-comment-set` payload. You emit that payload in your completion return; you do not write the `import.yaml` files directly.

## Out-of-scope paths (positive allow-list means: anything else)

The bullets above enumerate every file path your role produces. Any other location on the SSHFS mount — including every path under `environments/` (any directory name, any sibling of `environments`, any invented env-slug folder), the mount root `/var/www/` itself except for the explicit manifest path above, or any paraphrased sibling directory — is outside your writer scope. The publish pipeline reads only the canonical tree above.

**Env folder names are NOT your vocabulary.** Even referring to tiers by a slug in prose risks the step above you paraphrasing slug names into file paths. Use the tier's prettyName from the plan (e.g. "AI Agent", "Small Production") whenever you need to reference a tier in writing.
