# dev_server `command` guidance is wrong for dev-mode dynamic compiled languages

**Surfaced:** 2026-05-04, eval suite `20260504-065807` `classic-go-simple` retro.
The agent burned two attempts before working out the real pattern.

**Why deferred:** the four-phase fix pass (probe skip / kind / fit / static
yaml) was scoped narrowly. This is a separate atom-level guidance bug, not
a regression of any of those.

## What

The dev_server tool description (and supporting atom) say the `command`
field should be "the exact `run.start` from `zerops.yaml`". For dev-mode
dynamic runtimes, `run.start: zsc noop --silent` is the platform-mandated
idle command — quoting it as the dev_server command is self-contradictory.

For compiled languages (Go, Rust, Java, …) the build container HAS
already produced a binary at `/var/www/<artifact>` from
`build.buildCommands`. The right pattern is to invoke that binary
directly (`./app` from `workDir=/var/www`) — the dev_server doesn't need
to compile from source on every restart.

For interpreted languages (Node, Python, …) the right pattern is to
invoke the actual server (`npx tsx src/index.ts`, `gunicorn ...`),
again not what's in `run.start`.

## Trigger to promote

Eval evidence is sufficient (one scenario this run, plus historical
`classic-go-simple` 211240). Promote when an atom-axes pass already
touches develop atoms — bundle the rewording then. Standalone fix is
also fine if someone is touching the dev_server description anyway.

## Sketch

Two atoms to update (both in `internal/content/atoms/`):

- `develop-dev-server-*` — replace "exact `run.start` from `zerops.yaml`"
  with split guidance:
  - **Compiled languages**: invoke the build artifact (`./app`,
    `./target/release/<bin>`, `java -jar app.jar`).
  - **Interpreted languages**: invoke the server entrypoint with the
    actual dev/runner command.
  - Mention explicitly that `zsc noop --silent` is NOT the command to
    pass — it's the container's idle marker.

- The dev_server tool description in `internal/tools/dev_server.go`
  jsonschema strings — same correction.

## Update 2026-05-20 — three new patterns across Rust + Python retros

Eval batch (suites `20260520-172631` python, `20260520-173405` rust)
surfaced three concrete dev-server-tool friction modes the current
guidance doesn't cover:

1. **Rust `waitSeconds` default 15 is too tight for cargo cold-compile.**
   `classic-rust-postgres-standard` retro: *"The first `cargo run` on a
   dev container compiles everything from scratch. In this minimal
   project it took ~9 seconds, which squeaks under the default. A
   project with more dependencies would blow past 15 seconds and the
   agent would get a health probe timeout, think the app is broken, and
   start debugging code that's actually fine — it just hasn't finished
   compiling yet."* Agent preemptively set `waitSeconds=45`. Atom or
   tool description should suggest bumping for any compiled-language
   `command` (Rust/Java/Go-when-not-prebuilt) — the default targets
   interpreted-language fast startup.

2. **Python vendored installs (`pip install --target=./vendor`) hide bin
   stubs from PATH.** `classic-python-postgres-dev-only` retro: *"The
   build step installs packages into `./vendor` via `pip install
   --target=./vendor`, but that means `uvicorn` isn't on the container's
   PATH — it's sitting inside `/var/www/vendor/`. My first
   `zerops_dev_server` call used `env PYTHONPATH=/var/www/vendor uvicorn
   app:app ...`, which failed with `can't execute 'uvicorn': No such
   file or directory`. The fix was `python -m uvicorn` instead."*
   Python-section of the compiled/interpreted split should call out
   `python -m <module>` as the durable pattern, because vendored installs
   don't create bin stubs.

3. **`zerops_dev_server.command` is exec, not shell — env-var prefix
   needs `env`.** `classic-python-postgres-dev-only` retro: *"The
   `zerops_dev_server` command field has a subtle constraint: `command
   runs via exec, NOT a shell`. The description says to use `env KEY=VAL
   cmd` instead of `KEY=VAL cmd`. I followed this, but it's easy to
   miss. If a future agent writes `PYTHONPATH=/var/www/vendor python -m
   uvicorn ...` without the `env` prefix, it'll fail with a confusing
   'command not found' because the shell assignment syntax isn't
   interpreted. The error won't tell you why — it'll just say it can't
   find the program name (which will be the entire `KEY=VAL cmd`
   string)."* The doc clause exists; the failure mode message is
   misleading enough that agents would need atom-level reinforcement.

These three patterns extend the same atom-rewrite scope. Total fresh
content ~6 lines of structured atom prose. Trigger condition (eval
evidence + dev_server atom touch) now well-satisfied — promote when
anyone is editing develop-active dev_server atoms next.

## Risks

- Atom-axes pass may end up gating dev-server atoms by `runtimes` axis,
  in which case the compiled-vs-interpreted split is naturally clean.
  Worth checking before pre-emptively splitting atoms.
