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

### IG scope — "what changes for Zerops" only

IG items 2+ describe what changes about a NestJS / Laravel / SvelteKit
app to deploy on Zerops:

- Bind 0.0.0.0 (instead of 127.0.0.1)
- Trust the L7 proxy
- Read cross-service env vars from own-key aliases (not platform-side names)
- Cache control / SIGTERM drain — only when there's a Zerops-specific shape

What does NOT go here:
- Framework configuration that doesn't change for Zerops (route declarations,
  middleware ordering, controller decoration patterns).
- Recipe-internal contracts (NATS subject naming, cache key shape,
  image storage layout, queue topic conventions). Those are
  customization points for someone extending THIS recipe; they go in
  KB or claude-md/notes.
- Application architecture (module structure, class hierarchy).

Aim for 4-7 IG items. More usually means recipe-internal content
crept in. Reference (laravel-showcase-app): 5 items.

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

### CLAUDE.md — porter-facing, codebase-scoped, 30–50 lines (cap 60)

Target 30–50 lines; hard cap 60. Reference:
`laravel-showcase-app/CLAUDE.md` (33 lines). One fact per line;
multi-line only with code examples.

The reader is an AI agent or human developer working in this codebase
in their own editor with their own Zerops project. They do NOT have
zcp's control plane. Write **framework-canonical commands**, never
MCP tool invocations.

GOOD `Dev loop: \`npm run start:dev\` (Nest CLI watches src/**, reloads on change).`
BAD  `Dev loop: \`zerops_dev_server action=start hostname=apidev command="npm run start:dev"\`.`

GOOD `Deploy: edit, then commit + push to your Zerops-connected branch.`
BAD  `Deploy: \`zerops_deploy targetService=apidev\`.`

The platform's `start: zsc noop --silent` is background context — one
line, factual, not the dev loop the porter follows. The porter starts
the watcher themselves.

What goes here:
- **Zerops service facts** — hostnames, port, runtime, subdomain, etc.
  Concise list. Reference: `laravel-showcase-app/CLAUDE.md` (33 lines).
- **Dev loop** — framework-canonical command (`npm run start:dev`,
  `npm run dev`, `php artisan serve`, etc.).
- **Notes** — codebase-scoped operational facts that don't fit
  service-facts (cross-codebase rules, things-NOT-to-add).

What does NOT go here:
- MCP tool invocations (`zerops_*`, `zcp *`).
- zcli commands (`zcli push`, `zcli vpn`).
- Cross-codebase runbooks (those live in the recipe-root README) —
  `Quick curls`, `Smoke test(s)`, `Local curl`, `In-container curls`,
  `Redeploy vs edit`, `Boot-time connectivity`.
- Quick curls / Smoke tests / Boot-time connectivity narration.

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

Spec rule 4: if your fix is a recipe-source change AND the failure-mode
description lacks platform-mechanism vocabulary (Zerops, L7, balancer,
subdomain, zsc, execOnce, ${...}, ...), it's self-inflicted —
**discard**, don't author as KB. The fix belongs in the code; there is
no teaching for a porter cloning the finished recipe.

Operational rule: before recording a KB-eligible fact, ask: would a porter cloning this finished recipe (with the fix already applied) ever encounter this? If no, discard.

Dev/prod process model + `zerops_dev_server` → `principles/dev-loop.md`.
Implicit-webserver runtimes (php-nginx, static) skip `zsc noop` for
their backend but may still need a dev-server for a compiled frontend.

Mount vs container execution-split → `principles/mount-vs-container.md`.
Never `npm install` / `tsc` / `nest build` against the SSHFS mount.

## Self-validate before terminating

Before you terminate, call:

    zerops_recipe action=complete-phase phase=scaffold codebase=<your-host>

This runs the codebase-scoped validators (IG / KB / CLAUDE / yaml-
comment / source-comment-voice) against your codebase's surfaces only
— peer codebases are NOT validated. You only see your own work, in
your own session, where you can correct it.

If `ok:true`: safe to terminate.

If `ok:false` with violations:

- Violations on `codebase/<host>/{integration-guide,knowledge-base,
  claude-md/*}` ids → fix in-session via `record-fragment
  mode=replace`:

  ```
  zerops_recipe action=record-fragment slug=<slug>
    fragmentId=codebase/<host>/integration-guide
    mode=replace
    fragment=<corrected body>
  ```

  Default mode is append for codebase IG/KB/claude-md ids (so feature
  phase can extend scaffold's content). `mode=replace` overwrites —
  use when correcting your own previously-recorded fragment within
  the same phase.

- Violations on `<SourceRoot>/zerops.yaml` (yaml-comment-missing-
  causal-word, IG-scaffold-filename, etc.) → ssh-edit the yaml file
  directly; it's not a fragment, it's the committed source. After
  ssh-edit, the engine's IG item-1 generator will re-read the yaml
  body on next stitch.

- Re-call `complete-phase phase=scaffold codebase=<your-host>` to
  verify the fix.

- Repeat until `ok:true`, then terminate.

The phase-level `complete-phase` (no codebase parameter) is the main
agent's responsibility after every sub-agent returns — it advances
the phase state. Your job is just to ensure your own codebase's gate
passes before you exit. Feature sub-agent can also use `mode=replace`
to correct scaffold's content if scaffold wrote something feature
needs to rewrite (rare; prefer extending).

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

## At scaffold close — initialize git

Run `git init && git add -A && git commit -m 'scaffold: initial structure + zerops.yaml'` from `<cb.SourceRoot>` (= `/var/www/<hostname>dev/`). The apps-repo publish path needs a clean git history; doing this post-hoc loses the per-feature commit shape a porter sees when scrolling the repo.
