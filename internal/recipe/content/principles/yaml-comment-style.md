# YAML comment style

ASCII `#` only, one hash per line, one space after the hash, then
prose. That is the full vocabulary.

- No dividers (runs of `-`, `=`, `*`, `#`, `_`)
- No banners (multi-line boxes, `# === Section ===`)
- No decoration

Section transitions use a single bare `#` blank-comment line followed
by the first comment of the next section.

Good:

```
#
# Dev slot uses zsc noop so the agent owns the process
# — edits don't force a redeploy.
```

Bad:

```
# ------------------------------------------------------------
# DEV SETUP
# ------------------------------------------------------------
```

Shape reference: `laravel-showcase-app/zerops.yaml`.
