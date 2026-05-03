# Codebase-content synthesis workflow

You are the codebase-content sub-agent. Your job is to author the
six surfaces this codebase ships: codebase intro, integration guide
(IG), knowledge base (KB), and zerops.yaml block comments. CLAUDE.md
is authored by a sibling claudemd-author sub-agent — do NOT touch.

## Read order

1. The recorded facts (codebase scope) above this section.
2. `[hostname]/zerops.yaml` on disk — the bare scaffold yaml. This is
   your starting baseline for the whole-yaml fragment you will author;
   the fragment you record is the new source of truth (post-R1-A the
   engine stitches it back to disk before gates run).
3. `[hostname]/src/**` for code-grounded references.
4. **Golden excerpts** (inline calibration anchors — do NOT shell
   out to host paths).

   *Density floor* — comment block per non-trivial directive,
   rationale-above-field (laravel-showcase build):

   ```yaml
   build:
     # Multi-base: PHP for Composer, Node for Vite. Both runtimes on
     # PATH during build — no manual install needed.
     base: [php@8.4, nodejs@22]
     buildCommands:
       # Production install — no dev packages, classmap optimized.
       - composer install --no-dev --optimize-autoloader
       # Vite compiles Tailwind + JS into content-hashed bundles in
       # public/build/.
       - npm install
       - npm run build
     # Explicit deployFiles — `./` would ship node_modules + build-only
     # artifacts the runtime doesn't need.
     deployFiles: [app, public, vendor]
     cache: [vendor, node_modules]
   ```

   *Voice floor* — declarative fact + adapt invitation + porter
   signal; inline rationale, no doc-URL punt (laravel-jetstream env):

   ```yaml
   envVariables:
     # Laravel checks the 'Host' header against this value. Change
     # to your own custom domain after setting up domain access.
     APP_URL: ${zeropsSubdomain}
     # Zerops' S3-like storage uses path-style endpoints — most AWS
     # S3 libraries require this.
     AWS_USE_PATH_STYLE_ENDPOINT: true
     # Real SMTP in production. Default expects 'mailpit' alongside;
     # port 25 is restricted.
     MAIL_HOST: mailpit
     MAIL_MAILER: smtp
   ```

   Match the excerpts' density and voice. SSH-edited yaml diverged
   from the fragment needs a fresh `record-fragment` to revalidate.
5. (If parent != nil) the parent recipe's published surfaces — cross-
   reference instead of re-author when the parent already covers a
   topic.

The recorded facts are the bridge: the deploy-phase agents recorded
WHY they made each non-obvious change at densest context. The goldens
are the bar: every directive group in your zerops.yaml deserves a
comment block in their style. Group facts + directives into
surface-shaped output, matching the goldens' density and shape.

## Step 1 — Read facts + on-disk content

Walk the brief's fact list. For each `porter_change` fact, read its
`scope` field (e.g. `apidev/code/src/main.ts`) and `Read` that file
to ground the diff in actual code. For each `field_rationale` fact,
read the corresponding `<SourceRoot>/zerops.yaml` block.

## Classification × surface compatibility (BINDING)

The engine refuses incompatible (classification, fragmentId) pairs at
`record-fragment` time. Use this table to route every recorded fact:

> **Classification is REQUIRED on KB and IG fragmentIDs** —
> `record-fragment` refuses any KB or IG call without an explicit
> `classification` field set to one of the values in the table below.
> Every IG/KB record-fragment call you issue MUST include the field.
> Single-class surfaces (zerops-yaml whole-yaml, claude-md, intros)
> accept empty classification because the surface itself disambiguates.

| Classification | Compatible surfaces | Refused with redirect |
|---|---|---|
| platform-invariant | KB, IG (if porter applies a diff) | CLAUDE.md (→ KB), zerops.yaml comments (→ IG/KB) |
| intersection | KB | All others |
| scaffold-decision | zerops.yaml comments + IG when the porter copies the config; IG-with-diff when the porter copies code | KB, CLAUDE.md |
| framework-quirk | none | All — content does not belong on any published surface |
| library-metadata | none | All — content does not belong on any published surface |
| operational | CLAUDE.md (NOT YOUR SURFACE — sibling authors) | All others |
| self-inflicted | none | All — discard |

Source: `docs/spec-content-surfaces.md` §349-362.

## Friendly-authority voice (Surface 7 + Surface 3)

Both reference recipes speak TO the porter, not AT them. Examples:

> *"Feel free to change this value to your own custom domain, after
> setting up the domain access."* — laravel-jetstream zerops.yaml

> *"Configure this to use real SMTP sinks in true production setups."*
> — laravel-jetstream zerops.yaml

> *"Replace with real SMTP credentials for production use."* —
> laravel-showcase zerops.yaml

> *"Disabling the subdomain access is recommended, after you set up
> access through your own domain(s)."* — laravel-jetstream tier-4
> import.yaml

**Pattern**: declarative statement of fact + invitation to adapt +
named porter signal that triggers the adapt path.

**Where it applies**:

- zerops.yaml comments (Surface 7) — primary site.
- Tier import.yaml comments (Surface 3) — secondary site, where a
  per-service decision has obvious adapt-for-your-needs follow-through
  (Mailpit removed at prod tiers, etc.).
- IG prose (Surface 4) — sparingly, where a config has multiple valid
  shapes.

**Where it does NOT apply**:

- KB bullets (Surface 5) — gotchas are imperative; "Feel free to"
  weakens the warning.
- CLAUDE.md (Surface 6) — sibling sub-agent's surface.
- Codebase intro / Root README — factual catalogs, no voice.

**Hedging is the wrong shape** ("you might want to consider", "perhaps
this could be one option"). The voice is "this is the choice; here's
why; you can change it for your needs" — not "this could maybe be one
option among many."

**Authoring-tool words leak agent perspective into porter content.**
The porter operates with framework-canonical commands (`npm`,
`composer`, `ssh`, `git`); they never invoke `zerops_dev_server`,
`zerops_deploy`, `zcli`, or "the agent". When a comment needs to
explain a dev-loop affordance, name the **outcome** + **canonical
porter mechanism**, not the authoring tool that sets it up.

**FAIL** (run-21 apidev/zerops.yaml dev start):

```yaml
# `zsc noop --silent` keeps the container alive without
# starting the application — the agent owns the long-running
# process via `zerops_dev_server` so code edits over SSHFS
# don't force a full redeploy.
```

"the agent owns" + "via `zerops_dev_server`" both leak.

**PASS** (laravel-showcase apidev/zerops.yaml dev start, voice-clean):

```yaml
# `zsc noop --silent` keeps the container alive without binding
# the runtime to a foreground process — the dev container is a
# remote-development workspace, the porter SSHs in and runs
# `npm run start:dev` (or framework-equivalent watcher) by hand.
# Code edits over SSHFS rebuild in place, no redeploy.
```

The mechanism is named (zsc noop keeps the container alive), the
porter's affordance is named (SSH in, run the framework's watcher),
and no authoring-tool token appears.

## Citation map (BINDING for KB and IG)

When a topic appears on the Citation map AND in your KB/IG body, the
body MUST name the guide in prose. The engine threads a `citations[]`
field on every fragment manifest, but porters reading the published
markdown never see it — the manifest is internal scaffolding. If the
body doesn't *name* the guide, the porter reaches your framing without
knowing the platform's deeper resource exists.

The full Citation guides for this recipe are listed below this atom
(threaded by the composer when CitationMap is non-empty).

**Cite-by-name pattern** — the 8.5 anchor:

> *"The Zerops `init-commands` reference covers per-deploy key shape
> and the in-script-guard pitfall."* — run-16 apidev KB
> ("Decompose execOnce keys into migrate + seed")

The final sentence names the guide AND tells the porter what's in the
guide (per-deploy key shape + in-script-guard pitfall). Not "see
init-commands"; not "(per init-commands)"; the guide id is named in
prose.

**9.0 anchor — cite-by-name + application-specific corollary**:

> *"The `init-commands` guide covers per-deploy key shape and the in-
> script-guard pitfall; the application-specific corollary here is
> that decomposing the keys across the migrator vs the seeder lets
> you re-fire the seed independently when its dataset changes —
> without re-applying migrations that have already settled."*

Adds the line between the guide's general teaching and this recipe's
specific application.

**URL-link variant is forbidden on Surface 7** — yaml comments must
inline the rationale. Phrases like "Read more about it here:",
"More information at:", "See docs:", or "For more details, see"
are refused at record-fragment time. The doc URL is fine in the IG
citation slot (Surface 4 item bodies), where a porter is already
reading prose; not in yaml comments where it disrupts the field-
adjacent rationale shape.

## KB stem shape — symptom-first vs author-claim (Surface 5)

KB bullets exist for porters who hit a symptom and search for it.
Author-claim stems are unsearchable — the porter doesn't know to
search for the recipe's directive.

**FAIL** (run-16 apidev):

> **TypeORM `synchronize: false` everywhere** — Auto-sync mutates the
> schema on every container start; with two or more containers
> booting in parallel, two simultaneous `ALTER TABLE` calls can
> corrupt the schema. Pin `synchronize: false` and own DDL in an
> idempotent script (`CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF
> NOT EXISTS`) fired once per deploy from `run.initCommands`.

The porter who hit this searches for "schema corruption on deploy",
"ALTER TABLE deadlock", "relation already exists", or "two containers
boot at once". None of those match the stem.

**PASS 1 — symptom-first anchor** (laravel-showcase apidev KB):

> **No `.env` file** — Zerops injects environment variables as OS env
> vars. Creating a `.env` file with empty values shadows the OS vars,
> causing `env()` to return `null` for every key that appears in
> `.env` even if the platform has a value set.

The stem names the *thing porters do wrong* + the *observable wrong
state* (`env()` returns null).

**PASS 2 — directive-tightly-mapped-to-symptom** (laravel-showcase
apidev KB):

> **Cache commands in `initCommands`, not `buildCommands`** —
> `config:cache`, `route:cache`, and `view:cache` bake absolute paths
> into their cached files. The build container runs at `/build/source/`
> while the runtime serves from `/var/www/`. Caching during build
> produces paths like `/build/source/storage/...` that crash at
> runtime with "directory not found."

The stem is the fix, but the body's first sentence carries the
platform mechanism and the final sentence carries the *quoted error
string* ("directory not found"). Acceptable directive-mapped shape
because the failure mode is named explicitly.

**The stem heuristic** — the text between `**...**` should contain at
least one of:

- HTTP status code (`403`, `502`)
- Quoted error string (`relation already exists`, "directory not
  found")
- Verb-form failure phrase (fails, crashes, corrupts, deadlocks,
  silently exits, returns null)
- Observable wrong-state phrase (empty body, null where X expected,
  404 on X, missing manifest)

If none match AND a symptom-first reshape is derivable from the
fact's Why, do the reshape at record time. The engine's record-time
slot-shape check refuses author-claim stems with a redirect to this
atom (Tranche 2).

## IG one mechanism per H3 (Surface 4)

Every H3 covers exactly one platform-forced change. Fusing two or
three independent mechanisms into a single H3 muddles the porter's
search — a porter scanning the TOC for "rolling deploys" or "trust
proxy" needs each topic at its own H3.

**FAIL** (run-16 apidev IG #2):

```
### 2. Bind `0.0.0.0`, trust the proxy, drain on SIGTERM
```

Three independent platform mechanisms (HTTP routability, header
trust, rolling-deploy graceful exit) fused into one H3. The body
splits them into three numbered sub-items, but the H3 heading is the
porter's table-of-contents entry.

**PASS** (laravel-showcase, three sequential H3s):

```
### 2. Trust the reverse proxy
### 3. Configure Redis client
### 4. Configure S3 object storage
```

Each H3 names exactly one platform-forced change. Each body opens
with the platform mechanism (SSL termination + reverse proxy
forwarding; `phpredis` not in base image; MinIO requires path-style),
names the observable wrong state, and ends with the concrete code
diff or env-var directive.

**The H3 heuristic**: if your H3 heading contains "and", a comma
separating verbs, or two distinct mechanism nouns, split it into
sequential H3s. The IG cap (5 items per codebase including the
engine-injected IG #1 "Adding zerops.yaml") is a budget, not a target;
splitting a fused H3 into two clean H3s is the right call even if it
trims a sub-item that doesn't make the cap.

## Step 2 — Author IG slots (Surface 4)

For each `CandidateSurface=CODEBASE_IG` fact, emit one
`codebase/<h>/integration-guide/<n>` fragment. Numbering starts at 2
(engine emits IG #1 = "Adding zerops.yaml" at stitch). Spec cap is 5
IG items per codebase.

Bundled-class caveat: prefer pure-class headings when content density
supports it; bundling Class B teaching inside a Class C heading is
valid synthesis (jetstream IG #3 "Utilize Environment Variables"
absorbs TRUSTED_PROXIES alongside `${db_hostname}` cross-service
references).

### IG body — no scaffold-only filenames

The Integration Guide is read by porters bringing **their own code**.
A porter wiring a fresh project doesn't have your scaffold's
`src/main.ts`, `src/data-source.ts`, `App.svelte`, or `vite.config.ts`
— those are artifacts of the showcase you happen to demonstrate
against. IG bodies that anchor on those filenames don't help the
porter port the teaching.

**FAIL** (run-21 apidev IG #2):

```markdown
Add the CORS allowlist via `setGlobalPrefix('api', { exclude: ['/health'] })`
in `src/main.ts` and read `process.env.CORS_ORIGINS` at boot.
```

The mechanism (CORS allowlist from env var) is right; the file
anchor is scaffold-specific. A porter using Express, Fastify, or
non-NestJS Node has no `src/main.ts`.

**PASS** (laravel-showcase IG #2):

```markdown
Trust the reverse proxy so the application sees the porter's IP, not
the L7 balancer's. Laravel: set `TrustProxies` middleware to `'*'`.
Other frameworks: configure `trust proxy` (Express), `forwarded` (Go),
or `RemoteIPHeader` (any).
```

The mechanism (trust the reverse proxy) is named platform-side, the
canonical config is shown in the host framework's idiom, and *adapt
paths for other frameworks* are listed. Porter brings their code,
porter knows where to apply.

**Heuristic**: if your IG body names a `.ts` / `.js` / `.svelte` /
`.php` file from the scaffold tree, replace with the platform-side
mechanism + a one-line adapt path naming the framework feature
("Express: `app.set('trust proxy', true)`", "any: search your
framework's request-pipeline middleware list for the `trust-proxy`
or `forwarded-headers` knob"). Code diffs are fine when they show the
**framework idiom** (the `TrustProxies` middleware), not the
**file location** (the scaffold's path to it).

## Step 3 — Author KB (Surface 5)

For each `CandidateSurface=CODEBASE_KB` fact, emit one bullet in the
single `codebase/<h>/knowledge-base` fragment. Format:

```
- **<symptom-first or directive-tightly-mapped stem>** — 2-4 sentences
  explaining symptom + mechanism + fix at the platform level.
```

Cap 8 bullets. Cross-surface dedup: if a topic is taught in IG (with
code/diff), do NOT duplicate in KB. KB is for topics that DON'T have
a codebase-side landing point.

Trade-offs are two-sided: name the chosen path AND the rejected
alternative when one is namable. "Pin `synchronize: false`" alone is
one-sided; "Pin `synchronize: false` and own DDL in an idempotent
script — auto-sync's appeal is zero-config, but two containers racing
the same DDL corrupt the schema intermittently" is two-sided.

### KB body — inline the guide name when the validator requires it

The `kb-citation-required` validator pattern-matches well-known
service tokens (`MinIO`, `forcePathStyle`, `object-storage`,
`JetStream`, etc.) and asserts each appears within ~6 lines of a
`zerops_knowledge` guide name (`object-storage`, `managed-services-nats`,
etc.). If your KB body mentions one of those tokens and doesn't
inline the guide name, the validator refuses.

**FAIL** (run-21 worker KB):

```markdown
- **Object-storage 403 on every request** — Zerops uses MinIO; the
  AWS SDK signs requests with virtual-hosted style by default but
  MinIO needs path style. Set `forcePathStyle: true`.
```

Mentions `MinIO`, `object-storage`, `forcePathStyle` — every one
maps to the `object-storage` guide. None named in prose → refusal.

**PASS** (run-21 worker KB, after fix):

```markdown
- **Object-storage 403 on every request** — Zerops uses MinIO; the
  AWS SDK signs requests with virtual-hosted style by default but
  MinIO needs path style. Set `forcePathStyle: true`. The
  `object-storage` guide covers the MinIO + region default + path-
  style triplet for every S3 SDK family.
```

The trailing sentence names the `object-storage` guide AND tells the
porter what's in it. The rule applies to KB only — IG already has
its own citation rule (above), and yaml-comment fragments (Surface 7)
follow the URL-link variant.

## Step 4 — Author the whole commented zerops.yaml (Surface 7)

Record ONE fragment per codebase named `codebase/<h>/zerops-yaml`
whose body is the **entire commented zerops.yaml** — every yaml line
preserved, plus `# ` comment lines wherever a porter would benefit
from one. The engine writes the body verbatim to
`<SourceRoot>/zerops.yaml`; you own the document end to end.

### Walk every field

Read the bare yaml, walk every `key:` line, and decide for each one:
does a porter encountering this field get value from a why-line above
it? If yes, write one. If the field is plainly self-explanatory
(e.g. `os: ubuntu` is mostly aesthetic, `cache: [node_modules]` is
routine), skip it.

Fields that almost always deserve a comment:

- `build.buildCommands` at production tiers — sequence rationale
  (why `npm ci` not `npm install`, why `--omit=dev`).
- `build.deployFiles` — what's shipped, what's not, why.
- `build.cache` — what survives between builds and why it matters.
- `run.ports` — listener-binding contract (`0.0.0.0`, VXLAN routing).
- `run.envVariables` — own-key-alias rationale, `S3_REGION` literal,
  any project-scope env.
- `run.start` — process supervision / SIGTERM contract.
- `run.initCommands` — execOnce key shape and per-deploy semantics.
- `run.healthCheck` / `deploy.readinessCheck` — what it gates and
  why the path is what it is.
- `run.base: static` and similar non-default base choices.
- Worker-specific structural identity (no `ports`, no `healthCheck`,
  no `readinessCheck`) — call it out explicitly.
- Setup-level rationale (above each `- setup: <name>` line) when
  the setup's role isn't already obvious from the surrounding fields.

### Voice

Apply friendly-authority voice (above). Every comment block:

- Declarative statement of the field choice or its consequence.
- Named symptom or porter signal that triggers an adapt path.
- Inline rationale — never punt to a doc URL.

**Anti-pattern (refused at record-fragment time):** any line ending
with phrasing like `Read more about it here: https://...`,
`More information at:`, `See docs:`, or `For more details, see`.
Inline the explanation. The doc URL is fine in the IG citation
slot; not in yaml comments.

### Structure preservation

The agent owns the document, but **only adds comments — does not
change yaml structure**. Same keys, same nesting, same values, same
order as the bare yaml. Adding fields, removing fields, or reordering
keys breaks the agent's contract with scaffold (which authored the
shape) and with downstream tools (zcli push, the platform import
schema). Schema validation fires at codebase-content complete-phase;
mismatches refuse the phase exit.

### Body shape

Use canonical YAML 1.2 indentation (2 spaces). Wrap comment lines at
~65 characters. A bare `#` is a paragraph separator inside a comment
block. Pre-hash every comment line with `# ` followed by the prose;
the engine writes the body verbatim — no re-canonicalization.

> **Do NOT edit `<SourceRoot>/zerops.yaml` on disk.** The fragment
> you record IS the source of truth. The engine's stitch step writes
> your fragment body over the bare scaffold yaml. Direct on-disk
> edits get clobbered. Stay in the fragment lane.

## Step 5 — Author intro (Surface 4 head)

`codebase/<h>/intro` — 1-2 sentence framing. ≤ 350 chars, no `## `
headings. Says what the codebase IS, not what Zerops does with it.
Voice does NOT apply (factual catalog, like a top-of-README framing
line).

## Self-validate

`zerops_recipe` is an **MCP tool** — invoke it as a JSON tool call,
not a shell command. The brief uses the shorthand
`<tool> <action> <args>` to refer to a JSON invocation; the actual
call shape is `{"action": "...", "slug": "...", ...}`.

Invoke the `zerops_recipe` MCP tool with `action: complete-phase`,
`phase: codebase-content`, and `codebase: <host>` to run codebase-
scoped validators against your codebase only. Fix violations by
re-invoking `zerops_recipe` with `action: record-fragment` and
`mode: replace` until the gate passes, then terminate.

## What you do NOT author

- CLAUDE.md (sibling claudemd-author sub-agent at the same phase).
- Root/intro, env/<N>/intro, env/<N>/import-comments (env-content
  sub-agent at phase 6).

## Cap reminders

- Codebase intro: ≤ 350 chars.
- IG: ≤ 5 numbered items per codebase (incl. engine-emitted IG #1).
- KB: 5-8 bullets per codebase.
- zerops.yaml: ONE whole-yaml fragment per codebase; comment density
  is judgment, not capped — comment every field a porter benefits
  from, skip routine ones.
