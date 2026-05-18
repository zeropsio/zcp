# Group 1: classic-* sessions

Run dir: `/Users/macbook/Documents/Zerops-MCP/zcp/eval/behavioral/runs/20260515-154213/`

## Session: classic-bun-simple
**Task:** Set up a small Bun HTTP service for me on Zerops. Just one container, nothing fancy — no database, no staging.

**Findings:**

1. **[SCOPE-CONFUSION] Two-phase bootstrap `start` discriminator hidden behind skim-friendly response shape**
   - What: First `action="start"` returns `kind: "route-menu"` and "no open session"; second call with `route=` actually commits. Agent flags that the `kind` field is the only reliable discriminator — `sessionId` presence is unreliable as signal. Quote: "if you're skimming, you might think you have a session and try `complete` next, which would fail. The discriminator is the `kind` field, not the presence of `sessionId`."
   - Layer: `internal/workflow/engine.go` route-menu/session-active envelope, atom `bootstrap-route-options.md`
   - Cross-session signal: yes — same finding recurs in classic-go, classic-static, classic-rust

2. **[GUESS-SHAPE] Runtime-version selection has no documented preference rule; "historical record" in CLAUDE.md misled choice**
   - What: Stacks listed `bun@{canary,nightly,1.1.34,1.2,1.3}`. Agent reverted to `bun@1.2` to match an old historical-record entry rather than picking highest stable (1.3). "Nothing in the guidance tells you which to prefer."
   - Layer: atom (bootstrap-discover guidance), guidance text on classify-prompt about version selection
   - Cross-session signal: low — specific to historical-record influence, but the "no preference rule documented" part is general

3. **[GUIDANCE-OVERLOAD] Bootstrap discover step dumps multi-section guide; ~10% applies to trivial dev cases**
   - What: "massive multi-section guide covering recipe vs classic, dynamic vs static vs implicit-webserver vs managed runtimes, mode confirmation dichotomies, env channel rules, and pre-loading advice. For a trivial 'one Bun container, dev mode, no deps' case maybe 10% of that applies."
   - Layer: discover-step guidance in `internal/workflow/` (bootstrap_*.go), atom triage
   - Cross-session signal: yes — same complaint in classic-static, classic-rust

4. **[ATOM-CONTENT] "Confirm mode with user" guidance fires even when user intent is already explicit**
   - What: User said "one container, no staging" — that already pins `dev`. Asking the user to confirm again is friction. Quote: "if the user's intent already says 'one container, no staging,' just pick `dev` and move on — confirming again is friction the user won't thank you for."
   - Layer: atom `bootstrap-mode-prompt.md`
   - Cross-session signal: yes — classic-static also picked dev silently against guidance

5. **[ATOM-CONTENT] Dev-mode `start:` rule (zsc noop) is documented but easy to miss when scaffolding minimal yaml**
   - What: Dev-mode dynamic runtimes MUST use `start: zsc noop --silent` — real start command goes to `zerops_dev_server`. Agent "almost missed that." A real `start:` fights the dev-server process.
   - Layer: atom `develop-first-deploy-scaffold-yaml.md`, `develop-dynamic-runtime-start-container.md`
   - Cross-session signal: yes — classic-go and classic-python flagged the same trap; classic-go separately noted dev_server `command` arg semantics inferred from this rule

6. **[ATOM-CONTENT] subdomainUrl is in deploy response but agents might reconstruct from `${zeropsSubdomainHost}`**
   - What: "The deploy response gives you `subdomainUrl` directly (with the `-{port}` suffix already baked in for dynamic runtimes). Don't reconstruct it from `${zeropsSubdomainHost}` yourself." Confirmed: `internal/ops/deploy_common.go:42` carries the field.
   - Layer: atom (response-shape guidance for deploy result) / deploy result documentation
   - Cross-session signal: medium — explicit instinct-trap

## Session: classic-go-simple
**Task:** Set up a small Go HTTP service for me on Zerops. Just one container, nothing fancy.

**Findings:**

1. **[RESPONSE-CONFUSING] Route-menu fit-annotation "over-provisions" easy to miss when recipe is first in list**
   - What: `go-hello-world` recipe was first in route menu and had longer description, but its label said "over-provisions: adds postgresql." Agent's instinct was to grab the recipe; only the annotation saved them. "The fit annotation is the signal; trust it over the ordering."
   - Layer: workflow route-menu envelope ordering + annotation prominence (`internal/workflow/`)
   - Cross-session signal: yes — classic-python also flagged this (recipe matched runtime but locked in unwanted stage pair)

2. **[ATOM-CONTENT] Plan-schema nesting: `bootstrapMode`/`stageHostname` must nest inside `runtime`**
   - What: Hard-rejected with actionable diagnostic, but skim-readers will flatten the JSON. Agent got it right "only because the guidance explicitly warned about it twice."
   - Layer: atom `bootstrap-classic-plan-dynamic.md` (warning already exists) + plan-schema JSON example clarity
   - Cross-session signal: yes — every single classic session flagged this

3. **[ATOM-CONTENT] Dev-server `command` arg semantic: it's "what you'd run if `start:` were real", not literal `start:`**
   - What: Guidance says `command` should be "the exact `run.start` from `zerops.yaml`," but `run.start` is `zsc noop --silent`, which is obviously not what you actually want to start. Real rule: "what you'd run if `start:` were real." Agent had to infer.
   - Layer: atom `develop-dynamic-runtime-start-container.md` / `dev_server.go` tool description
   - Cross-session signal: yes — classic-python and classic-rust hit this from different angles (PATH issue in python, waitSeconds issue in rust)

4. **[RESPONSE-CONFUSING] `subdomainAccessEnabled: true` + URL in deploy response implies running app**
   - What: Saw subdomain ready in deploy response, had momentary "is the server already running?" confusion. `nextActions` field correctly spells out the dev-mode idle state — but agent had to learn to read `nextActions` first.
   - Layer: deploy result `nextActions` prominence, or atom on dev-mode deploy meaning
   - Cross-session signal: yes — classic-python also confused about "verify before dev_server start"

5. **[ATOM-CONTENT] Recipe `importYaml` top-level `project:` block — copy-paste trap**
   - What: `zerops_import` rejects top-level `project:`. Recipe `importYaml` field includes it. If the agent copies the recipe's importYaml verbatim, they hit a rejection that guidance warned about but the source-data hands the trap.
   - Layer: recipe response `importYaml` field shape, or import-tool error suggestion / atom about stripping `project:`
   - Cross-session signal: medium — recipe-route specific but applies broadly

## Session: classic-php-mariadb-standard
**Task:** I want to deploy a PHP web app backed by MariaDB. I need both a development environment and a staging slot for testing builds.

**Findings:**

1. **[ATOM-CONTENT] PREFLIGHT_FAILED on dev/prod env-var parity: knowledge template ships identical envVariables**
   - What: "the knowledge guide template I pulled from `zerops_knowledge` had bit-identical env vars in its `dev` and `prod` blocks, so I copied the pattern verbatim and got rejected." Fix is one `APP_ENV` differentiator.
   - Layer: atom / `internal/knowledge/recipes/<slug>.md` for php-apache, or env-channel preflight atom
   - Cross-session signal: high (template-vs-preflight mismatch likely recurs across runtimes with standard mode)

2. **[ATOM-CONTENT + RESPONSE-CONFUSING] PHP `/status` readiness check shipped without `.htaccess` rewrite guidance**
   - What: php-apache knowledge guide shows readiness check on `/status` as "healthy pattern" — but without `.htaccess` rewrites, Apache 404s on `/status`. Failure mode is: `failedPhase: init` with `category: start`, even though init commands all succeeded. Then `zerops_logs` returns zero entries (readiness failures leave no logs), so diagnosis path looks dead-ended. Quote: "the readiness check failure manifested as `failedPhase: init` with a `category: start` classification, even though the actual init commands all ran fine."
   - Layer: recipe knowledge `php-apache.md` + classifier `internal/ops/deploy_failure*.go` (readiness-fail mislabeled as start/init)
   - Cross-session signal: yes — failure-classification mislabel is a general issue beyond PHP

3. **[RETRY-TOOL + RESPONSE-CONFUSING] `zerops_import override=true` requires two calls — `confirmDestructive` shape only readable from refusal suggestion**
   - What: First call → DIAGNOSIS_REQUIRED + `wouldDestroy` payload telling agent to call `zerops_logs`. Second call needs `confirmDestructive: {operation, acknowledgedTargets}`. "The shape isn't obvious from the schema description alone — you have to read the suggestion field of the first refusal verbatim to get the exact JSON shape."
   - Layer: `internal/tools/import.go` `confirmDestructive` schema description (already long), or atom on diagnose-before-destruct
   - Cross-session signal: medium — only stage-replay scenarios will hit this

4. **[STATE-CONFUSION] READY_TO_DEPLOY after failed deploy = stuck; redeploy doesn't work, must `import override`**
   - What: Agent didn't expect this failure mode. "I assumed I could just retry the deploy with the fix." DIAGNOSIS_REQUIRED error spells the recovery path, but the surprise cost time.
   - Layer: atom on READY_TO_DEPLOY-stuck-after-failure, or recovery hint on deploy refusal
   - Cross-session signal: yes — any standard-mode scenario with failed-first-deploy could hit this

5. **[RESPONSE-CONFUSING] verify returns `error_logs` status=info "/etc/fstab does not exist" on healthy php-apache**
   - What: "It looks alarming at first glance because it's listed under 'error_logs,' but it's harmless platform noise." Agent learned to ignore.
   - Layer: `internal/ops/verify_checks.go` (filter known-benign), or atom about php-apache verify noise
   - Cross-session signal: low — runtime-specific noise

6. **[GUESS-SHAPE] `ToolSearch select:` shortcut requires fully-qualified MCP names, not short names**
   - What: `select:zerops_import` returned "No matching deferred tools found." Had to use `mcp__zerops__zerops_import`. The deferred-tools list shows short names so the syntax is ambiguous.
   - Layer: ToolSearch usage docs / bootstrap-tool-preload atom
   - Cross-session signal: yes — classic-static-nginx also questioned this exact syntax

## Session: classic-python-postgres-dev-only
**Task:** Set up a Python web service with a Postgres database for me. Just a development environment, no production stage needed.

**Findings:**

1. **[ATOM-CONTENT] gunicorn PATH gotcha with `pip install --target`**
   - What: `--target=./vendor` puts binary at `./vendor/bin/gunicorn`, not on `$PATH`. `PYTHONPATH` only helps Python find modules, not shell find binaries. Knowledge doc shows absolute path in a comment but main `start:` example doesn't apply to dev-mode. Quote: "The fix is using the absolute path `/var/www/vendor/bin/gunicorn`. The knowledge doc actually shows this in a comment but it's easy to miss."
   - Layer: recipe knowledge `python.md` / atom on dev-mode dev-server command for python
   - Cross-session signal: low — Python-specific, but the broader "real start command in dev-mode" pattern is universal

2. **[SCOPE-CONFUSION] Recipe vs classic route choice when user constrains topology recipe doesn't match**
   - What: `python-hello-world` recipe was exact runtime match but ships dev+stage pair. User explicitly asked dev-only. "The route-menu doesn't tell you 'recipes lock you into their topology' — you have to infer from the import YAML preview that recipes include a stage service you can't easily drop."
   - Layer: route-menu annotations (recipe topology fit warning), atom `bootstrap-route-options.md`
   - Cross-session signal: yes — classic-go hit the analog (recipe over-provisions postgres)

3. **[ATOM-CONTENT] Plan-schema nesting: `bootstrapMode`/`stageHostname` inside `runtime` — buried in prose**
   - What: "The detailed guide does say this — but if you skim, you'll flatten them at the top level and get rejected. Read the JSON example, not just the prose."
   - Layer: atom `bootstrap-classic-plan-dynamic.md` JSON-example prominence
   - Cross-session signal: every classic session

4. **[ATOM-CONTENT + RESPONSE-CONFUSING] Dev-mode verify sequencing not obvious: deploy → dev_server → verify**
   - What: After deploy, before starting gunicorn, agent unsure if verify would 502 or platform shim. Develop guide spells it out but sequence "is non-obvious." Quote: "If you verify before starting the dev server, the 502 is expected, not a deploy failure."
   - Layer: atom about develop-loop ordering (probably `develop-dev-server-triage.md` or `develop-checklist-dev-mode.md`)
   - Cross-session signal: yes — classic-go had the same momentary confusion about subdomain-ready vs app-ready

5. **[ATOM-CONTENT] Env-var wiring ambiguity: `DB_HOST: db` literal vs `${db_hostname}`**
   - What: "The knowledge doc says `DB_HOST: db` (literal hostname, not `${db_hostname}`) but lists `hostname` in the env catalog, so a fresh agent might use `${db_hostname}` thinking it's the canonical reference." Both work, but doc prefers literal hostname for the `*_HOST` slot.
   - Layer: recipe knowledge / `develop-env-var-channels.md` clarity on hostname vs ref-token
   - Cross-session signal: medium — applies to any managed-dep wiring scenario

## Session: classic-rust-postgres-standard
**Task:** Build me a small Rust HTTP API on Zerops, with Postgres. I want a dev environment plus a stage slot — and yes, I want to be able to iterate on dev fast.

**Findings:**

1. **[GUIDANCE-MISLEADING] Develop workflow's "Next" hint omits `setup=` for self-deploy when recipe ships multi-setup zerops.yaml**
   - What: Workflow printed `zerops_deploy targetService="appdev"` with no `setup` arg. Recipe ships zerops.yaml with `dev` and `prod` setups. Deploy tool itself rejects multi-setup without `setup=`. Cross-deploy hint right below it correctly included `setup="prod"` — so the workflow knew about setups for cross-deploy but dropped it for self-deploy. Agent rescued by reading zerops.yaml first. Quote: "Don't trust the 'Next' pointer blindly; cross-check against the zerops.yaml on the mount."
   - Layer: `internal/workflow/` develop synthesize logic — the primary-action plan for self-deploy in recipe routes (search around `aggregate_render_probe_test.go:88` "appdev" hint construction)
   - Cross-session signal: high — recipe-route develop is exercised in many scenarios; this is a systemic missing-arg bug in the hint

2. **[ATOM-CONTENT + RESPONSE-CONFUSING] `waitSeconds` default 15 too small for Rust cold compile**
   - What: Default 15s would have failed cold cargo build (actix-web + tokio-postgres). Agent bumped to 45 on gut feel; tool gives "no signal that it timed out vs. genuinely failed beyond a `reason` code." Confirmed: `internal/tools/dev_server.go:64-67` documents Default 15, max 45.
   - Layer: `internal/tools/dev_server.go` waitSeconds default + reason-code disambiguation in failure response, atom `develop-dev-server-reason-codes.md`
   - Cross-session signal: medium — Rust/Java/native-compile runtimes will hit this; small managed runtimes won't

3. **[SCOPE-CONFUSION] Two-phase bootstrap start `kind=route-menu` vs `kind=session-active`**
   - What: Same finding as classic-bun. "if you're skimming you might think the menu response is the session itself and try to advance from it. Read the `kind` field."
   - Layer: same as classic-bun #1
   - Cross-session signal: yes — repeats

4. **[GUIDANCE-OVERLOAD] Recipe-route develop response dumps many KB of guidance largely irrelevant when buildFromGit already populated the mount**
   - What: "develop-start response dumped many KB of guidance about scaffolding, writing application code, framework defaults, dev-server lifecycle, env channel rules, self-deploy destruction risk, per-managed-type wiring for Kafka and S3 and Qdrant — none of which applied." For exact-fit recipe matches, real loop is: read mount, deploy, dev_server, verify, cross-deploy, verify.
   - Layer: `internal/workflow/` develop guidance assembly — branch-by-route-source (recipe-with-buildFromGit vs classic-scaffold) to trim sections
   - Cross-session signal: yes — classic-static and classic-bun flagged the same "too much guidance" issue, but this is sharper because the recipe content is already shipped

## Session: classic-static-nginx-simple
**Task:** Put up a small static landing page for me using nginx. Just HTML, no backend.

**Findings:**

1. **[GUIDANCE-OVERLOAD] Develop-active response ~200+ lines, ~95% irrelevant for trivial static page**
   - What: "200+ lines of guidance covering git-push strategies, dev-server lifecycles, env-var shadow loops, cross-deploy artifact selection, managed-service catalogs, and HTTP diagnostics — almost none of which applied to 'drop one HTML file in nginx.'" Load-bearing block was a small "Static runtime — develop workflow" section buried in the middle.
   - Layer: develop envelope assembly — needs runtime-class + topology filtering before printing full guidance set
   - Cross-session signal: high — every minimal-scope classic session flagged some version of this

2. **[SCOPE-CONFUSION] Two-phase bootstrap start (`kind=route-menu` vs `kind=session-active`) — repeated**
   - What: "If you assume one call is enough and try to `complete` the discover step next, nothing is committed yet. The `kind` field is the signal."
   - Layer: same as classic-bun #1
   - Cross-session signal: yes — repeats

3. **[ATOM-CONTENT] Plan-schema `bootstrapMode`/`stageHostname` nesting trap**
   - What: "Flat top-level placement is hard-rejected with a specific error code. I got it right on the first try only because the guidance shouted about it twice."
   - Layer: same as classic-go #2, classic-python #3
   - Cross-session signal: yes — every classic session

4. **[ATOM-CONTENT] "Confirm dev vs standard mode" overkill for obviously-throwaway scenarios**
   - What: Agent silently picked `dev` for "small static landing page" instead of confirming with user. Worked fine, but agent flagged the guidance felt heavy. Same observation as classic-bun #4.
   - Layer: atom `bootstrap-mode-prompt.md` — confirmation skip rule when user intent is unambiguously trivial
   - Cross-session signal: yes — classic-bun

5. **[GUESS-SHAPE] `setup` parameter on `zerops_deploy` description hedge-y enough to require re-read**
   - What: "the deploy tool's schema says it's only required when there are multiple setups, but the description is long and hedge-y, so I had to read it twice to be sure omitting was OK." Confirmed: `internal/tools/deploy_local.go:43` description is dense.
   - Layer: `internal/tools/deploy_local.go` / `deploy_ssh.go` setup-arg description tightening
   - Cross-session signal: medium — combines with classic-rust #1 (workflow hint dropped setup) to suggest the setup-arg story is generally muddy

6. **[GUESS-SHAPE] ToolSearch `select:` short-name vs `mcp__zerops__` prefix**
   - What: Agent used fully-qualified one-at-a-time, never confirmed bare-name batch syntax that guidance suggested. "Both apparently work, but I never confirmed the bare-name batch syntax."
   - Layer: ToolSearch usage docs / `bootstrap-tool-preload.md` clarity
   - Cross-session signal: yes — classic-php confirmed the short-name form is broken; this session corroborates the ambiguity

## Group-level patterns

- **Two-phase bootstrap `start` (route-menu → session-active) is the most repeated finding** across classic-bun, classic-go, classic-static, classic-rust. The `kind` field discriminator is correct but skim-hostile — agents repeatedly note they had to read the response twice. Likely fix lives in the envelope shape, the route-menu prose, and/or the `bootstrap-route-options.md` atom.
- **Plan-schema nesting (`bootstrapMode`/`stageHostname` inside `runtime`) was flagged by every classic session.** All five got it right, but only because the guidance "shouted twice." The hard-reject + diagnostic catches it fast; the upstream fix is in the JSON example prominence in `bootstrap-classic-plan-dynamic.md` (and equivalents) so agents don't rely on the rejection feedback.
- **Guidance overload on develop / discover step is a chronic friction across trivial scenarios** (classic-bun, classic-static, classic-rust, classic-php). Recipe-fit cases dump scaffolding guidance the buildFromGit already obviated; minimal-scope scenarios get cross-deploy / git-push / multi-runtime content irrelevant to them. The systemic fix is route-source-aware + topology-aware envelope trimming in `internal/workflow/` develop synthesis, not atom-level edits.
- **Dev-mode dynamic-runtime semantics (`zsc noop` start, dev_server command vs literal `start:`, deploy → dev_server → verify sequencing) recurred** as a pitfall cluster across classic-bun, classic-go, classic-python, and classic-rust — each session hit a different facet (waitSeconds, gunicorn PATH, command-arg semantic, verify-before-dev_server). The atoms exist but the wiring is non-obvious from a top-down read; rust-style cold-compile and python-style PATH gotchas point at a missing "first dev-server start: what to expect by runtime class" reference.
- **Recipe-route trapdoors are consistent across runtimes** — top-level `project:` block in `importYaml`, recipes locking in topology, develop-workflow hint dropping `setup=`. These point at recipe response shape + workflow hint assembly being the locus, not atom content.
- **ToolSearch `select:` bare-name vs `mcp__zerops__`-prefixed-name ambiguity** surfaced in classic-php and classic-static. Two independent observations, one negative confirmation (`select:zerops_import` "No matching deferred tools found"). This is a small but real guidance bug.
