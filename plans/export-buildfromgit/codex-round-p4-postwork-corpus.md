# 1. Verdict

NEEDS-REVISION. The atom/reference checks and pin-density test pass, but the corpus is over the 28KB soft cap, `make lint-local` did not complete because schema fetch DNS failed, and a few wording issues can become stale or violate the requested filename/separation hygiene.

# 2. Holistic axis K/L/M/N hygiene

Axis L: checked every frontmatter title and markdown heading in all six atoms. No standalone heading/title qualifier equals `container`, `local`, `container env`, or `local env`.

Axis K: markers are correctly adjacent to the flagged guidance they intend to keep:

- `export-intro.md:25` marks `local-only` on `export-intro.md:26`.
- `export-validate.md:59` marks `local-only` on `export-validate.md:60`.
- `export-publish-needs-setup.md:26` marks `local-only` on `export-publish-needs-setup.md:27`.

Other uppercase "do NOT" phrases do not match the configured Axis K verb list in `atoms_lint_axes.go:146-156`.

Axis M: grep found one instance, `export-publish-needs-setup.md:27`, but it says "Zerops container", "container walkthrough", and "local walkthrough". The canonical prefix is present for "Zerops container"; no bare `the container`, `the platform`, `the tool`, `the agent`, or `the LLM` problem remains.

Axis N: all six atoms are `environments: [container]`, so universal-atom env leakage is not in scope.

# 3. Cross-atom consistency

Filename: canonical `zerops-project-import.yaml` is used in intro, classify table, validate field description, publish write/commit/import steps, and coverage fixture. Two schema-validation caveats still say `import.yaml`, which is shorthand and should be corrected.

Invocation: canonical `zerops_workflow workflow="export"` appears in intro, classify examples, publish-needs-setup, and scaffold; no contradictory standalone export tool is present.

Variant mapping: intro defines it once: `dev` re-imports as `mode=dev`, `stage` re-imports as `mode=simple`. Validate only asks agents to confirm `services[].mode` matches the intended mapping; no contradiction found.

Bucket emit shapes: classify-envs defines the four emit shapes once: drop infrastructure from `project.envVariables`, generate random auto-secret, emit external-secret placeholder, emit plain-config verbatim. Validate/publish reference consequences only; no competing emit table found.

# 4. Compaction safety

`wc -c` output:

```text
    2333 internal/content/atoms/export-intro.md
   10370 internal/content/atoms/export-classify-envs.md
    5201 internal/content/atoms/export-validate.md
    3632 internal/content/atoms/export-publish.md
    4683 internal/content/atoms/export-publish-needs-setup.md
    3350 internal/content/atoms/scaffold-zerops-yaml.md
   29569 total
```

Total is 29,569 bytes, which is 897 bytes over the 28KB soft cap of 28,672 bytes.

# 5. references-fields integrity

`export-validate.md:7` declares `ops.ExportBundle.ImportYAML`, `ops.ExportBundle.ZeropsYAML`, and `ops.ExportBundle.Warnings`; these match exported fields at `internal/ops/export_bundle.go:35-36`, `internal/ops/export_bundle.go:37-41`, and `internal/ops/export_bundle.go:58-61`.

`export-publish.md:7` declares `ops.ExportBundle.ImportYAML`, `ops.ExportBundle.ZeropsYAML`, `ops.ExportBundle.RepoURL`, and `ops.ExportBundle.Warnings`; these match exported fields at `internal/ops/export_bundle.go:35-36`, `internal/ops/export_bundle.go:37-41`, `internal/ops/export_bundle.go:44-46`, and `internal/ops/export_bundle.go:58-61`.

# 6. Pin coverage

Test output:

```text
ok  	github.com/zeropsio/zcp/internal/workflow	0.200s
```

All six atom IDs appear in the scenario pin closure at `internal/workflow/scenarios_test.go:985-990`. S12 also requires the exact six rendered atoms at `internal/workflow/scenarios_test.go:624-630`. The `export_active` coverage fixture exists at `internal/workflow/corpus_coverage_test.go:766-786`.

# 7. Phase 5/6 forward-compat

`export-classify-envs.md:118`: "does NOT yet schema-validate the generated `import.yaml`" and "Phase 5 ... adds blocking errors". Scope is appropriate, but the `import.yaml` shorthand will become stale/confusing next to canonical `zerops-project-import.yaml`.

`export-validate.md:57`: "`importYaml` and `zeropsYaml` are not yet schema-validated client-side ... Phase 5 ... adds JSON-Schema validation". Scope is appropriate for current behavior, but should avoid sounding permanent once Phase 5 lands.

`export-publish-needs-setup.md:44`: "Phase 6 ... adds an automatic refresh on every export pass". This is scoped well because it explicitly says what changes after Phase 6; not stale unless Phase 6 lands without the refresh.

`export-publish-needs-setup.md:48`: "does not yet support a `compose-only / no-publish` mode". This can become stale if compose-only ships; it should be written as a status-gated caveat or removed when that feature lands.

# 8. Recipe-style separation

Grep hits:

```text
internal/content/atoms/export-classify-envs.md:66:## Source-tree grep recipes
internal/content/atoms/export-classify-envs.md:68:... The recipes below use `rg -n` everywhere ...
```

No `registry` or `scaffold-templates` hits. These are command-recipe wording, not recipe-engine content, but changing "recipes" to "commands" would remove the ambiguity. `scaffold-zerops-yaml` is about one file, not a multi-file template; the word "scaffold" itself is acceptable.

# 9. Rendering-order coherence

Priority ASC order:

1. `export-intro` (`priority: 1`)
2. `export-classify-envs` (`priority: 2`)
3. `export-validate` (`priority: 3`)
4. `export-publish` (`priority: 4`)
5. `export-publish-needs-setup` (`priority: 5`)
6. `scaffold-zerops-yaml` (`priority: 6`)

The sequence reads coherently as intro -> classify -> validate -> publish -> chain -> scaffold. The last two are conditional recovery atoms; because all six render for `export-active`, placing them after the happy path keeps the agent-facing flow readable.

# 10. Recommended amendments

- Reduce the six-atom total by at least 897 bytes to get under the 28KB soft cap. Largest target: `internal/content/atoms/export-classify-envs.md:66-82`.
- Replace `import.yaml` shorthand with `zerops-project-import.yaml` in `internal/content/atoms/export-classify-envs.md:118` and `internal/content/atoms/export-validate.md:57`.
- Rename "Source-tree grep recipes" / "The recipes below" to "Source-tree grep commands" / "The commands below" at `internal/content/atoms/export-classify-envs.md:66` and `internal/content/atoms/export-classify-envs.md:68`.
- Re-run `make lint-local` when DNS/network access can resolve `api.app-prg1.zerops.io`; current run failed during `catalog sync`, before confirming the full gate green.
