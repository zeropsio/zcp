# Self-review per surface

Before returning, walk every surface you authored and apply the positive pre-return checklist below. An item that does not satisfy every applicable check is removed, not rewritten — rewrite means the item was on the wrong surface. Move it to its correct surface (or drop it) and re-check.

Each check below is expressible as a shell predicate you can run against your in-mount draft. Exit 0 means the check passes; non-zero means the item needs removal or relocation. The aggregate exit at the end of this atom is what you report in the completion return.

---

## Surface 1 — Root README

Per-codebase readers: none (root is recipe-level). Checks:

- File exists at `{{.ProjectRoot}}/README.md`.
- Contains a deploy-button row for each env tier in the plan.
- Length between 20 and 30 lines: `wc -l {{.ProjectRoot}}/README.md` returns a value in `[20, 30]`.
- Single-question test passes: "a reader decides in 30 seconds whether this recipe deploys what they need and picks the right tier" — answer Yes in your return.

---

## Surface 2 — Per-env README

For each canonical env tier:

{{range .EnvFolders}}- File exists at `{{$.ProjectRoot}}/environments/{{.}}/README.md`.
{{end}}
Per-env review rules apply to each file above:

- Line count in `[40, 80]`: `wc -l <file>` returns a value in range.
- Four required teaching sections present: "Who this is for", "What changes vs the adjacent tier" (or "Entry-level tier"), "Promoting to the next tier" (or "Terminal tier"), "Tier-specific operational concerns". Grep-confirm each heading.
- Numeric claims match the adjacent env `import.yaml` — any `N GB` quota statement in the env README is consistent with `objectStorageSize: N` in the same env; any "N replicas" statement matches `minContainers: N`. Mismatch fails.

---

## Surface 3 — Env `import.yaml` comments (via env-comment-set payload)

For each env tier's payload entry:

- Every service block has a non-empty comment block.
- Each block explains a decision (why this service at this tier, why this scale, why this mode) rather than narrating what the YAML field does.
- No block is a word-for-word copy-paste of another block. Template openings repeated across service blocks fail.
- Numeric claims match the YAML in the same block.
- Every comment line is ASCII `#` prefixed; no Unicode box-drawing, no dividers.

---

## Surface 4 — Per-codebase README integration-guide + `INTEGRATION-GUIDE.md`

For each hostname `{h}` in `{{.Hostnames}}`:

- Fragment markers present: `grep -q '#ZEROPS_EXTRACT_START:integration-guide' {{.ProjectRoot}}/{h}/README.md` and the matching end marker.
- H3 count in `[3, 6]`: count `### ` headings inside the integration-guide markers.
- Every H3 item carries at least one fenced code block in its section (one action, one reason, one diff).
- Every H3 item is standalone: a porter reading the single item understands what to do without reading the neighbouring items.
- Self-referential items removed: no H3 references a scaffold helper file or class by name as the primary teaching.
- Matching-topic items cite their platform topic from the Citation Map in prose.
- The companion `INTEGRATION-GUIDE.md` in the codebase directory mirrors the fragment content.

---

## Surface 5 — Per-codebase README knowledge-base + `GOTCHAS.md`

For each hostname `{h}` in `{{.Hostnames}}`:

- Fragment markers present: start and end for `knowledge-base`.
- Gotcha bullet count in `[3, 6]`: count `- **` bullets inside the knowledge-base markers.
- Authenticity: every gotcha either names a platform mechanism by name OR describes a concrete failure mode (HTTP status, quoted error string, measurable wrong-state). Aim for at least 80% of bullets passing one of these two tests.
- Zero self-inflicted bullets: every bullet's manifest entry has classification in {framework-invariant, intersection, scaffold-decision-reframed, framework-quirk-reframed}. Any self-inflicted classification routed here without `override_reason` fails.
- Zero folk-doctrine bullets: every bullet on a matching-topic Citation Map row references the cited platform topic in the body.
- No recipe-run version-anchor strings in the published bullet text — describe the behavior class rather than which run surfaced it.
- Cross-codebase uniqueness: stems do not overlap between codebases; repeated facts cross-reference by prose.
- IG/gotcha distinctness: no gotcha stem is a paraphrase of an IG heading in the same README.
- The companion `GOTCHAS.md` mirrors the fragment content.

---

## Surface 6 — Per-codebase CLAUDE.md

For each hostname `{h}` in `{{.Hostnames}}`:

- File exists at `{{.ProjectRoot}}/{h}/CLAUDE.md`.
- Byte count floor: `test $(wc -c < {{.ProjectRoot}}/{h}/CLAUDE.md) -ge 1200`.
- Four template sections present: "Dev Loop", "Migrations" (or "Migrations & Seed"), "Container Traps", "Testing".
- At least two custom sections present beyond the template four. Count headings at level 2.
- Zero deploy instructions inside CLAUDE.md — deploy content lives in integration-guide items or `zerops.yaml` comments.

---

## Aggregate pre-attest commands

Run these locally against the mount before returning. Exit 0 in aggregate is the green-light condition:

```bash
# Manifest exists and parses.
test -f {{.ProjectRoot}}/ZCP_CONTENT_MANIFEST.json
jq empty {{.ProjectRoot}}/ZCP_CONTENT_MANIFEST.json

# Every fact has a non-empty routed_to.
jq '[.facts[] | select(.routed_to == null or .routed_to == "")] | length' \
   {{.ProjectRoot}}/ZCP_CONTENT_MANIFEST.json | grep -qE '^0$'

# Default-discard classifications without override_reason fail.
jq '[.facts[] | select(.classification == "framework-quirk" or .classification == "self-inflicted") | select(.routed_to != "discarded") | select((.override_reason // "") == "")] | length' \
   {{.ProjectRoot}}/ZCP_CONTENT_MANIFEST.json | grep -qE '^0$'

# Canonical output tree only — no invented sibling directories.
! find {{.ProjectRoot}} -maxdepth 2 -type d -name 'recipe-*'
! find {{.ProjectRoot}} -maxdepth 2 -type d -name '*-output'

# Per-codebase fragments present.
for h in {{range .Hostnames}}{{.}} {{end}}; do
  grep -q '#ZEROPS_EXTRACT_START:intro'             {{.ProjectRoot}}/$h/README.md &&
  grep -q '#ZEROPS_EXTRACT_START:integration-guide' {{.ProjectRoot}}/$h/README.md &&
  grep -q '#ZEROPS_EXTRACT_START:knowledge-base'    {{.ProjectRoot}}/$h/README.md || exit 1
done

# CLAUDE.md byte floor.
for h in {{range .Hostnames}}{{.}} {{end}}; do
  test $(wc -c < {{.ProjectRoot}}/$h/CLAUDE.md) -ge 1200 || exit 1
done
```

A non-zero exit anywhere above is a pre-return failure. Fix the item (remove or relocate) and re-run. Report the final exit code in the completion return.
