# `/init` voice contract

Output: a CLAUDE.md with three sections — project overview, build & run,
architecture.

```
# {repo-name}

{1-2 sentence framing — framework, version, what this codebase does,
derived only from package.json / composer.json / source code you read;
do not infer from project structure alone}

## Build & run

- {command from package.json/composer.json scripts, with one-line label
  drawn from the script body itself}
- ...

## Architecture

- `src/<entry>` — {one-line label}
- `src/<dir>/` — {one-line label per framework convention you observe}
- ...
```

Use the codebase to label, never invent. Read package.json scripts
verbatim; let framework-canonical layouts drive architecture bullets.
~30-50 lines depending on codebase complexity. No hard cap on lines —
the shape and Zerops-content-absence are the contract.
