# Content authoring

## Voice — the reader is a porter, never another recipe author

Everything you write — fragment bodies, `zerops.yaml` inline comments,
committed source-code comments, README prose — is read by someone
deploying this recipe into their own project.

**Never write:** "the scaffold", "feature phase", "pre-ship contract
item N", "showcase default", "showcase tier", "showcase tradeoff",
"the recipe", "we chose", "we added", "grew from".

**Always write:** the finished product. The product IS wired. HAS the
health probe. HANDLES the upload. No authoring "before" for a porter.

GOOD `# Bucket policy private — signed URLs give time-bounded access.`
BAD  `# Private (showcase default) — tradeoff.`
GOOD `// /health returns 200 once runtime is ready.`
BAD  `// /health added per pre-ship contract item 1.`

Produce your codebase's `zerops.yaml` (with inline comments) + record
5 fragments via `zerops_recipe action=record-fragment`:

- `codebase/<h>/intro` — one paragraph
- `codebase/<h>/integration-guide` — porter items starting at `### 2.`
  (item #1 is engine-generated — see below)
- `codebase/<h>/knowledge-base` — `**Topic**` bullets with guide ids
- `codebase/<h>/claude-md/service-facts` — port/hostname facts
- `codebase/<h>/claude-md/notes` — operator notes (dev loop, SSH)

### Integration Guide — item #1 is engine-owned

The engine generates IG item #1 during stitch: a `### 1. Adding
\`zerops.yaml\`` heading, an intro derived from your yaml (setups
declared, initCommands presence, readiness + health check presence),
and a fenced yaml block carrying `<cb.SourceRoot>/zerops.yaml`
verbatim. Reference: `laravel-showcase-app/README.md`.

Your `codebase/<h>/integration-guide` fragment contains items #2+ —
porter-facing app-side changes. Start at `### 2. <title>`. Do NOT
author item #1. Do NOT describe the yaml in English as a numbered
item — the yaml block IS the description; clarifications go in yaml
inline comments.

### Knowledge Base — `**Topic** — prose` only

Every KB bullet: `**<topic>**` + em-dash + 2–5 sentences.

Good:

```
- **Expose X-Cache via CORS** — a cross-origin fetch only sees headers
  listed under Access-Control-Expose-Headers. app.enableCors() must
  pass exposedHeaders: ['X-Cache'] for the L7 balancer's cache header
  to reach the browser.
```

Bad (debugging-runbook triple — belongs in claude-md/notes):

```
- **symptom**: 502. **mechanism**: bind default. **fix**: 0.0.0.0.
```

Bad (citation boilerplate — see Citation map below):

```
- **Expose X-Cache via CORS** — same body. **Cited guide: `http-support`.**
```

Do NOT use `**symptom**:` triples in KB; runbooks live in
`claude-md/notes`. Do NOT append `Cited guide: <name>` to bullets —
citations live in prose where natural, not as boilerplate.

### Citation map — author-time signals, not render output

Citations are signals to **YOU** at author-time. Before writing a KB
bullet that touches `env-var-model` / `http-support` / `init-commands`
/ `rolling-deploys` / `object-storage` / `deploy-files` /
`readiness-health-checks`, call `zerops_knowledge` on that guide and
read it. The bullet's prose IS the citation: if you couldn't write
the bullet without consulting the guide, the bullet correctly reflects
the guide's framing. Spec rule 3: don't duplicate guide content as
paraphrase — add new intersection content beyond it (V-2 enforces
> 50% containment).

Don't write `**Cited guide: <name>.**` at the end of bullets. Don't
write `(cite \`x\`)` in env import.yaml comments. Don't tell the
porter which guide you read — tell them the rule. If a guide name
genuinely belongs in prose ("Per the http-support guide…"), it can
stay; mechanical boilerplate is the target.

### CLAUDE.md — codebase-scoped, 30–50 lines (cap 60)

Target 30–50 lines; hard cap 60. Reference:
`laravel-showcase-app/CLAUDE.md` (33 lines). One fact per line;
multi-line only with code examples. Do NOT add cross-codebase
sections — `Quick curls`, `Smoke test(s)`, `Local curl`,
`In-container curls`, `Redeploy vs edit`, `Boot-time connectivity` —
those live in the recipe root README.

## Placement

- Stanza IS in yaml → yaml inline comment
- Absence / alternative / consequence → KB (`**Topic** — prose`)
- Topology walkthrough → IG (items #2+)
- Debugging runbook (symptom/mechanism/fix) → claude-md/notes
- Dev loop / SSH / curl → claude-md/notes

Why-not-what. Use `because`, `so that`, `otherwise`, `trade-off`.

## Classify before routing

Self-inflicted + pure framework quirks → DISCARD. Platform × framework
intersections → KB + `zerops_knowledge` citation.

### Self-inflicted litmus

Spec rule 4: could this observation be summarized as "our code did X,
we fixed it to do Y"? If yes, **discard** — the fix belongs in the
code, there is no teaching for a porter cloning the finished recipe.

Run-10 anti-patterns to pattern-match against:

- *"we chose `npx ts-node`, it failed, switched to `node dist/migrate.js`"* — **DISCARD**, code-fix not platform teaching.
- *"wrote `dist` into `.deployignore`, deploy bricked, removed `dist`"* — **DISCARD**, recipe-author error not porter trap.
- *"Trust proxy is per-framework, not per-platform"* — **DISCARD**, framework-quirk per spec, belongs in NestJS docs.

Operational rule: before recording a KB-eligible fact, ask: would a porter cloning this finished recipe (with the FIXED yaml, FIXED .deployignore) ever encounter this? If no, discard.

Dev/prod process model + `zerops_dev_server` → `principles/dev-loop.md`.
Implicit-webserver runtimes (php-nginx, static) skip `zsc noop` for
their backend but may still need a dev-server for a compiled frontend.

Mount vs container execution-split → `principles/mount-vs-container.md`.
Never `npm install` / `tsc` / `nest build` against the SSHFS mount.

## Validator tripwires

Finalize gates reject on these; fix at author-time:

- IG item #1 is engine-owned; your items start at `### 2.`
- IG 2+: no scaffold-only filenames (`main.ts`, `seed.ts`, `migrate.ts`)
- Env READMEs use porter voice (never "agent"/"sub-agent"/"zerops_knowledge")
- Env READMEs target 45+ lines (threshold 40; leave margin)
- yaml comment blocks: one causal word per block (not per line)
- KB: `**Topic** — prose` only; triples live in `claude-md/notes`
- CLAUDE.md: 30–50 lines (cap 60); no cross-codebase runbooks
- Fragment IDs use `cb.Hostname` (the codebase name, e.g. `app`) — NEVER the slot hostname (`appdev` / `appstage`). The slot is the SSHFS mount; the codebase is the logical name. Engine rejects `codebase/appdev/intro` with the Plan codebase list.
- Do NOT author `.deployignore` reflexively. Most recipes do not need it (the builder excludes `.git/`; editor metadata belongs in `.gitignore`). Author one only if the recipe has a specific reason — and NEVER list `dist`, `node_modules`, or anything in `deployFiles`. Worker run-10 burned 20 minutes on `dist`-in-`.deployignore`.
