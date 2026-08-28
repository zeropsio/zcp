# Alpine on Zerops

Default base OS (~5MB). Uses musl libc. Package manager: `sudo apk add --no-cache`.

### Selecting Alpine
The `alpine/` prefix on the type/base selects it: `alpine/nodejs@22` in the
import `type` and in both `build.base` and `run.base`. Note the asymmetric
legacy defaults: a bare `<tech>@<ver>` resolves to Alpine in zerops.yaml but
to Ubuntu at import — always write the prefix.

### When to Switch to Ubuntu
- CGO-enabled Go binaries linking C libraries
- Python packages with C extensions requiring glibc (numpy, pandas compiled backends)
- Deno runtime (no Alpine build — Gleam, by contrast, IS available on Alpine)
- Any software explicitly requiring glibc

### Package Installation
`sudo apk add --no-cache {package}` — sudo required (containers run as `zerops` user). NEVER use `apt-get` on Alpine.
