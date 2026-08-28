# Ubuntu on Zerops

Full glibc base (~100MB). Use when Alpine's musl libc causes compatibility issues.

### Selecting Ubuntu
The `ubuntu/` prefix selects it: `ubuntu/nodejs@22` in the import `type` and
in both `build.base` and `run.base` (a bare import `type` also materializes
as Ubuntu, but write the prefix). Changing the OS prefix invalidates the
build cache.

### When to Use
- CGO-enabled Go builds linking C libraries (musl mismatch causes 502)
- Python C extensions (numpy, scipy, pandas with compiled backends)
- Deno runtime (only available on Ubuntu — Gleam runs on both Alpine and Ubuntu)
- Legacy software requiring glibc
- PHP extensions that fail platform requirements on Alpine

### Package Installation
`sudo apt-get update && sudo apt-get install -y {package}` — sudo required (containers run as `zerops` user). NEVER use `apk` on Ubuntu.
