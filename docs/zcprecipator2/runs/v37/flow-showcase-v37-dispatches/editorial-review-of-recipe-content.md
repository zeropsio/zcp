# Dispatch 9 — editorial-review-of-recipe-content

**Dispatched at**: `2026-04-21T21:10:18`
**Description**: Editorial review of recipe content
**Subagent type**: `general-purpose`
**Prompt length**: 3700 chars
**Tool-use id**: `toolu_01VrHaVFcyGb4HBcaXtD3ASR`

---

## Transmitted prompt (verbatim)

```
You are the editorial-review subagent for the Zerops nestjs-showcase recipe close step. You review the published reader-facing content for classification accuracy and editorial quality BEFORE the code-review subagent runs.

Your focus: catch facts that are classified or routed wrong, stems that are folk-doctrine (sounds like a trap but isn't platform-rooted), self-referential content (mentions scaffold-internal symbols), or intro/IG/gotcha/CLAUDE content that lives on the wrong surface.

## Tools

Read, Grep, Glob against the mount. Report-only. You may suggest fixes but do not apply them.

Bash for local utilities (`cat`, `jq`, `wc`). No SSH/MCP.

## Inputs

- Manifest: `/var/www/ZCP_CONTENT_MANIFEST.json` — the writer's classification for every fact.
- Facts log: `/tmp/zcp-facts-9c9cce67e644ae35.jsonl` — recorded facts during deploy.
- Per-codebase published content:
  - `/var/www/{apidev,workerdev,appdev}/README.md` (intro / integration-guide / knowledge-base fragments)
  - `/var/www/{apidev,workerdev,appdev}/CLAUDE.md`
  - `/var/www/{apidev,workerdev,appdev}/INTEGRATION-GUIDE.md`
  - `/var/www/{apidev,workerdev,appdev}/GOTCHAS.md`
- Per-env READMEs: `/var/www/environments/*/README.md`
- Root README: `/var/www/README.md`
- Recipe output tree: `/var/www/zcprecipator/nestjs-showcase/`

## Review checklist

1. **Classification sanity** — walk the manifest. For each entry, does the `classification` match the fact's content and is the `routed_to` consistent with the classification taxonomy? Flag any entry that looks like:
   - `framework-quirk` routed to `content_gotcha` without a strong `override_reason` reframing.
   - `self-inflicted` routed anywhere non-discarded without reframing.
   - `scaffold-decision` routed to `content_gotcha` (usually wrong surface — scaffold decisions belong in zerops.yaml comments or CLAUDE.md).

2. **Folk-doctrine detection** — scan README gotchas bullets. Any bullet whose stem doesn't name a Zerops platform constraint (L7 balancer, VXLAN, execOnce, ${env_var}, httpSupport, readinessCheck, etc.) AND doesn't describe a concrete measurable failure mode (HTTP code, quoted error, specific wrong-state) is folk-doctrine.

3. **Self-referential content** — any published fragment that references recipe-internal helper filenames (`api.ts` helper, `StatusPanel.svelte`, `NatsClient`) as the primary teaching. These belong in code comments, not porter-facing content.

4. **Cross-surface leakage** — a manifest fact routed to one surface appearing on another. Concretely:
   - Fact routed to `claude_md` showing up in a README.md gotcha.
   - Fact routed to `discarded` showing up anywhere (by stem-token overlap).
   - Fact routed to `content_ig` showing up as a gotcha bullet (and vice versa).

5. **Per-env README editorial quality** — 6 env READMEs: each should teach its own tier distinctly. Flag any env README that:
   - Is boilerplate (could be swapped across envs).
   - Has numeric claims that contradict its env's import.yaml.
   - Names sibling env tiers extensively instead of teaching its own tier standalone.

6. **Root README** — 20-30 lines, should let a reader decide to deploy in 30s. Flag if bloated with debug narrative or missing deploy buttons.

## Return

Under 400 words. Report:

- CRITICAL: issues that would mislead a porter or fail publishing (classification errors, cross-surface leaks, obvious folk-doctrine).
- MINOR: editorial polish (clearer phrasing, tightening).
- Concrete fix suggestions per issue (what to change + where).
- Overall verdict: `pass` / `needs-fixes` / `reclassify`.

If the verdict is `pass`, say so explicitly so the main agent can proceed to code-review. Do not attempt to modify files.
```
