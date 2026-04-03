# Ubuntu on Zerops

Full glibc base (~100MB). Use when Alpine's musl libc causes compatibility issues.

### When to Use
- CGO-enabled Go builds linking C libraries (musl mismatch causes 502)
- Python C extensions (numpy, scipy, pandas with compiled backends)
- Deno and Gleam runtimes (only available on Ubuntu)
- Legacy software requiring glibc
- PHP extensions that fail platform requirements on Alpine

### Package Installation
`sudo apt-get update && sudo apt-get install -y {package}` — sudo required (containers run as `zerops` user). NEVER use `apk` on Ubuntu.
