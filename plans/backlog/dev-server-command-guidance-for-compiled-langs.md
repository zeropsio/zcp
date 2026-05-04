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

## Risks

- Atom-axes pass may end up gating dev-server atoms by `runtimes` axis,
  in which case the compiled-vs-interpreted split is naturally clean.
  Worth checking before pre-emptively splitting atoms.
