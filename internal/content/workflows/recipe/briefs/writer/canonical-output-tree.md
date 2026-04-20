# Canonical output tree

The complete list of files you may create or modify. Any path not on this list is out of scope for your role.

## Per-codebase files (one set per hostname in `{{.Hostnames}}`)

For each hostname `{h}` in the plan's codebase list:

- `{{.ProjectRoot}}/{h}/README.md` — reader-facing content with three extract fragments (intro, integration-guide, knowledge-base). Published to the recipe page at finalize.
- `{{.ProjectRoot}}/{h}/CLAUDE.md` — repo-local operational guide. Plain markdown. Not published.
- `{{.ProjectRoot}}/{h}/INTEGRATION-GUIDE.md` — stand-alone integration-guide document that the README integration-guide fragment references. Each H3 item reads as a single porter-actionable change.
- `{{.ProjectRoot}}/{h}/GOTCHAS.md` — stand-alone gotchas document that the README knowledge-base fragment references. Each bullet names a concrete observable symptom.

## Per-environment files (one per env tier in the plan)

For each env tier `{i}` declared in the plan:

- `{{.ProjectRoot}}/environments/{{index .EnvFolders i}}/README.md` — tier-focused teaching content: who the tier is for, what scale it handles, what changes relative to the adjacent tier.

The env folder names come from the plan's `EnvFolders` slice. The writer does not invent, paraphrase, or re-case them.

## Root-level files

- `{{.ProjectRoot}}/README.md` — one-paragraph recipe summary with a deploy-button row for every tier.
- `{{.ProjectRoot}}/ZCP_CONTENT_MANIFEST.json` — classification manifest for every recorded fact. Path is fixed at the recipe output root.

## Surfaces NOT authored by you

The comment set inside each codebase's `zerops.yaml` file is authored at generate time by the step above you. You do not author or rewrite it here.

The comment set inside each environment's `import.yaml` file is applied at finalize time by the step above you from a structured `env-comment-set` payload. You emit that payload in your completion return; you do not write the `import.yaml` files directly.

## Out-of-scope paths (positive allow-list means: anything else)

The seven bullets above enumerate every file path your role produces. Any other location on the SSHFS mount — for example `{{.ProjectRoot}}/recipe-{slug}/...`, `{{.ProjectRoot}}/{slug}-output/...`, or a paraphrased environment folder name — is outside your writer scope. The publish pipeline reads only the canonical tree above.
