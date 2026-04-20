# Surface walk task

You walk the deliverable in fixed order. The order matters: the root README frames the recipe, the environment surfaces frame the deployment shape, the per-codebase surfaces frame the porter's action items. A reader proceeds in that order; you review in that order.

## The seven surfaces, in order

1. **Root README** — the single `README.md` at the recipe output root. Names the recipe, names the managed services, names the environment tiers.
2. **Environment READMEs** — one `README.md` per environment tier directory. Teaches the tier's audience, scale, and the differences from adjacent tiers.
3. **Environment `import.yaml` comments** — the `# `-prefixed comment lines in the `import.yaml` at each environment tier directory. One service-block's worth of comments per service.
4. **Per-codebase README intro + body** — the `README.md` at each codebase directory. Opens with the one-paragraph intro a porter needs before diving into Integration Guide / Gotchas.
5. **Per-codebase `CLAUDE.md`** — the operational guide at each codebase directory for running the dev loop, exercising features by hand.
6. **Per-codebase `INTEGRATION-GUIDE.md`** — the imperative list of changes a porter bringing their own code must copy into their own app (a separate file, or an `INTEGRATION-GUIDE` section inside the per-codebase README — Glob for both).
7. **Per-codebase `GOTCHAS.md`** — the knowledge-base / gotchas content (a separate file, or a `GOTCHAS` section inside the per-codebase README — Glob for both).

Each surface has exactly one per-surface single-question test declared in the next atom. Apply it per item on that surface.

## Scope by tier

The recipe tier determines how many instances of each surface you walk.

### Minimal tier

- Root README: 1 file.
- Environment READMEs: 4 tier directories (typically `0`, `1`, `2`, `3` for dev / review-or-stage / stage / prod — read the actual directory names from the mount, do not assume).
- Environment `import.yaml` comments: 4 files (one per environment tier).
- Per-codebase README: 1 codebase (single-codebase minimal) OR 2 codebases (dual-runtime minimal with separate frontend + api). Glob for codebase directories under the output root — the count is whatever the mount shows.
- Per-codebase `CLAUDE.md`: same count as per-codebase README.
- Per-codebase `INTEGRATION-GUIDE.md`: same count.
- Per-codebase `GOTCHAS.md`: same count.

No worker codebase on minimal. Total surface instances: 1 root + 4 env × 2 (README + import-yaml) + 1 or 2 codebases × 4 per-codebase surfaces = **13 to 17 surface instances** on a minimal walk.

### Showcase tier

- Root README: 1 file.
- Environment READMEs: 6 tier directories.
- Environment `import.yaml` comments: 6 files.
- Per-codebase README: 3 codebases (typically `apidev`, `appdev`, `workerdev` — read the directory names from the mount).
- Per-codebase `CLAUDE.md`: 3 files.
- Per-codebase `INTEGRATION-GUIDE.md`: 3 files (or 3 sections inside the per-codebase READMEs).
- Per-codebase `GOTCHAS.md`: 3 files (or 3 sections).

Worker codebase is included in the showcase codebase count. Total surface instances: 1 root + 6 env × 2 + 3 codebases × 4 per-codebase surfaces = **25 surface instances** on a showcase walk.

## Walk discipline

Walk every instance in order. Do not skip a surface because "the previous instance looked fine" — each codebase's `GOTCHAS.md` is independent content and each environment's `import.yaml` comments make independent decisions. A skipped surface is a coverage gap in your findings.

If a file that should exist is absent on the mount, that absence is itself a finding: a missing surface at the completion step means the deliverable is incomplete. Record the missing path in the per-surface walk summary; flag the severity per the reporting-taxonomy atom.

If a file exists but is empty or reduced to template boilerplate, apply the surface's single-question test — most boilerplate fails its surface's test and is a finding.
