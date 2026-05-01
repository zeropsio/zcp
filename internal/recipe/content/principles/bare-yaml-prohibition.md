# Bare zerops.yaml during scaffold

Produce your codebase's `zerops.yaml` **without inline causal comments**.

The bare yaml is the scaffold contract. Causal comments are authored
later at codebase-content phase via per-block fragments
(`codebase/<h>/zerops-yaml-comments/<block-name>`) and stamped back
into the on-disk file by the engine's stitch step. Inlining comments
during scaffold forces a strip-and-re-inject round-trip and risks
double-comments if the codebase-content sub-agent records overlapping
fragments.

The only `#` lines you may keep in scaffold-authored yaml are:

1. The `#zeropsPreprocessor=on` shebang at line 0 (when present) — it
   is a directive, not a causal comment.
2. Trailing comments on data lines (e.g. `port: 3000  # see <link>`)
   — they ride on a data line; the strip step preserves them.

Any other `^\s*# ` line — single-hash, leading whitespace, the
"description above the directive" shape — is REFUSED at scaffold
complete-phase. The validator names the exact violating line numbers
so you can clean them in one pass.

GOOD (bare yaml; comments authored later):

```yaml
zerops:
  - setup: api
    run:
      base: nodejs@22
      ports:
        - port: 3000
          httpSupport: true
      start: node dist/main.js
```

BAD (inline causal comments — refused):

```yaml
zerops:
  - setup: api
    run:
      # API runs on Node 22 because the build container is also Node 22.
      base: nodejs@22
      ports:
        # Port 3000 is the framework default; httpSupport publishes it.
        - port: 3000
          httpSupport: true
      start: node dist/main.js
```
