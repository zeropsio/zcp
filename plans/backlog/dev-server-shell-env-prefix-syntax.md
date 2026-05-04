# dev_server `command` rejects `KEY=val cmd` shell-prefix syntax

**Surfaced:** 2026-05-04, eval suite `20260504-065807`
`classic-python-postgres-dev-only` retro. Same finding in 211240 retro
(repeat across runs).

**Why deferred:** atom-level guidance fix; out of scope for the
four-phase response noise fixes.

## What

The dev_server tool description says "Env assignments and pipes are
supported." Agents read this and pass commands like
`PYTHONPATH=/var/www/vendor /var/www/vendor/bin/gunicorn ...`. It fails:

```
sh: exec: line 0: PYTHONPATH=/var/www/vendor: not found
```

The dev_server execs the command directly without going through
`/bin/sh -c`, so the leading `KEY=val` token is treated as the
executable name. The workaround agents discover after retry: prepend
`env`, e.g. `env PYTHONPATH=/var/www/vendor /var/www/vendor/bin/gunicorn ...`,
or use the structured env mechanism on the tool.

Two retros in two consecutive runs hitting the same friction.

## Trigger to promote

Promote in the same atom-axes pass that touches dev_server atoms
(see `dev-server-command-guidance-for-compiled-langs.md`). Cheap to
bundle.

## Sketch

`internal/tools/dev_server.go` jsonschema description on `command`:

- Replace "Env assignments and pipes are supported" with explicit
  "command is exec'd directly (not through a shell). For inline env
  vars use `env KEY=val cmd ...` or pass them via the structured `env`
  field. Shell features like `&&`, `||`, redirects, and globbing do
  NOT work — wrap in `sh -c '...'` if needed."

Plus the matching atom (`develop-dev-server-*`) gets the same
correction.

## Risks

- None significant. Pure description fix.
