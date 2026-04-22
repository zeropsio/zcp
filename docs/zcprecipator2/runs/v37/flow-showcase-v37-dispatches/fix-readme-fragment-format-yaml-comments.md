# Dispatch 6 — fix-readme-fragment-format-yaml-comments

**Dispatched at**: `2026-04-21T20:50:58`
**Description**: Fix README fragment format + YAML comments
**Subagent type**: `general-purpose`
**Prompt length**: 6084 chars
**Tool-use id**: `toolu_01VxvC9xUNVkvkDXKXUr1QTE`

---

## Transmitted prompt (verbatim)

```
You are fixing three Zerops recipe README files to pass deploy-step checks. Fixes are mechanical but must be applied carefully.

Files (on SSHFS mounts — Read/Edit/Write work directly):
- `/var/www/apidev/README.md`
- `/var/www/workerdev/README.md`
- `/var/www/appdev/README.md`

## Required fixes (apply ALL to each file)

### Fix 1 — Intro fragment content

Current intro fragments are multi-paragraph with a `## Intro` heading inside. The checker requires:
- 1-3 lines of plain prose inside the markers.
- NO markdown titles (no `##` or `###`) inside the intro fragment.
- Blank line AFTER the start marker (extractor requirement).
- Blank line BEFORE the end marker.

Rewrite each intro fragment to a terse 1-3 line standalone description. Example target shape:

```
<!-- #ZEROPS_EXTRACT_START:intro# -->

A NestJS 11 HTTP API on `nodejs@22` that exposes five feature endpoints backed by PostgreSQL, Valkey, NATS, MinIO-compatible Object Storage, and Meilisearch. Part of the nestjs-showcase recipe.

<!-- #ZEROPS_EXTRACT_END:intro# -->
```

Content already at top of file (outside markers) can retain H1 title + a one-sentence subtitle; move richer context (feature list, component details) into the H2 sections ABOVE the integration-guide fragment or into CLAUDE.md. Do NOT delete that content from the file — just lift it out of the intro fragment.

### Fix 2 — Integration-guide fragment heading level

The `## Integration Guide` line INSIDE the fragment is H2; it must be removed (or demoted). The H2 section title goes OUTSIDE the markers. Inside the markers, keep the existing `### 1.`, `### 2.`, etc. items — those are already H3 and are correct.

Structural shape:
```
## Integration Guide

<!-- #ZEROPS_EXTRACT_START:integration-guide# -->

Four/five/six changes turn a generic [framework] app into one that runs on Zerops.

### 1. Adding `zerops.yaml`
... (existing H3 items as-is)

<!-- #ZEROPS_EXTRACT_END:integration-guide# -->
```

Move the `## Integration Guide` + intro paragraph OUTSIDE the markers (above the START marker). Inside the markers, start directly with the lead paragraph + `### 1.` items.

### Fix 3 — Knowledge-base fragment heading level + Gotchas section

The `## Knowledge Base` line INSIDE must be removed. INSIDE, the section heading must be `### Gotchas` (H3). Same pattern: H2 title outside, `### Gotchas` inside.

Structural shape:
```
## Knowledge Base

<!-- #ZEROPS_EXTRACT_START:knowledge-base# -->

Failures you hit on the way to a green deploy, with the Zerops mechanism that explains each one.

### Gotchas

- **<symptom>** -- <mechanism>. <evidence>.
- ... (existing bullets unchanged)

<!-- #ZEROPS_EXTRACT_END:knowledge-base# -->
```

### Fix 4 — Blank line after each START marker

All six fragments (3 READMEs × 3 fragments × START side) need a blank line after the `<!-- #ZEROPS_EXTRACT_START:{name}# -->` line. Also blank line before the END marker is good practice.

### Fix 5 — Comment ratio in integration-guide YAML block ≥30%

The first `### 1. Adding zerops.yaml` H3 item in each README contains a fenced ```yaml ``` block. Current comment-to-code ratios: app 8%, api 12%, worker 5%. All need to reach ≥30%.

Add Zerops-specific `#` comment lines inside the YAML block (NOT the backing file zerops.yaml — just the README's rendered copy of it). Mirror the richer existing `/var/www/{hostname}/zerops.yaml` commentary that's already on the mount: Read each zerops.yaml first via `Read /var/www/{hostname}/zerops.yaml`, then re-copy its commented form into the README's YAML block byte-identically (the check gates on keeping both surfaces in sync).

Simpler approach: replace the terse YAML in each README's `### 1. Adding zerops.yaml` with the FULL commented `zerops.yaml` content verbatim from `/var/www/{hostname}/zerops.yaml`. Those files already have ≥30% comments.

### Fix 6 — appdev comment specificity

The app's comments are too generic. The checker flagged "npm ci for reproducible builds" etc as generic. Each comment must either explain WHY (because/so that/prevents/required/fails/breaks) or name a Zerops-specific term (execOnce, L7 balancer, ${env_var}, httpSupport, 0.0.0.0, subdomain, advisory lock, trust proxy, cold start, build time, horizontal container).

Since Fix 5 copies the full zerops.yaml verbatim (which has richer comments), Fix 6 should auto-resolve. Still verify appdev's comments mention Zerops terms.

### Fix 7 — DO NOT touch non-README files

Do NOT edit `zerops.yaml`, `INTEGRATION-GUIDE.md`, `GOTCHAS.md`, `CLAUDE.md`, manifest, or any env README. The fixes scope is ONLY `/var/www/{apidev,workerdev,appdev}/README.md`.

## Execution

1. Read each of the 3 README.md files.
2. Read each of the 3 `/var/www/{hostname}/zerops.yaml` files (to copy into fix 5).
3. For each README, rewrite using Write (complete rewrite is cleaner than Edit for this scope). Preserve all existing prose content — just restructure per the fixes above.
4. Run check commands (exit 0 each):
   ```bash
   # Intro 1-3 lines
   for h in apidev workerdev appdev; do
     awk '/ZEROPS_EXTRACT_START:intro#/{f=1;next} /ZEROPS_EXTRACT_END:intro#/{f=0} f' /var/www/$h/README.md | grep -v '^$' | wc -l
   done
   # Fragments have H3 (### Gotchas, ### N.), no H2 inside
   for h in apidev workerdev appdev; do
     for frag in integration-guide knowledge-base; do
       awk "/ZEROPS_EXTRACT_START:$frag#/{f=1;next} /ZEROPS_EXTRACT_END:$frag#/{f=0} f" /var/www/$h/README.md | grep -c '^## ' | grep -qE '^0$' || echo "H2 found in $h/$frag"
     done
   done
   # Blank line after each START marker
   for h in apidev workerdev appdev; do
     for frag in intro integration-guide knowledge-base; do
       grep -A1 "ZEROPS_EXTRACT_START:$frag#" /var/www/$h/README.md | sed -n '2p' | grep -qE '^$' || echo "no blank after $h/$frag"
     done
   done
   ```

All should print nothing. Return when clean.

## Reporting

Return:
- Files written (3 READMEs with byte counts).
- Final intro line count per README (should be 1-3).
- Comment ratio per README's IG YAML block (≥30%).
- Pre-return check output (empty = green).
```
