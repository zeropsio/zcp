# Alpine on Zerops

## Keywords
alpine, musl, apk, base image, lightweight, zerops.yml

## TL;DR
Default base OS (~5MB). Uses musl libc. Package manager: `sudo apk add --no-cache`.

### Default Base
Alpine is the default for all runtimes. Use it unless you need glibc.

### When to Switch to Ubuntu
- CGO-enabled Go binaries linking C libraries
- Python packages with C extensions requiring glibc (numpy, pandas compiled backends)
- Deno and Gleam runtimes (not available on Alpine)
- Any software explicitly requiring glibc

### Package Installation
`sudo apk add --no-cache {package}` — sudo required (containers run as `zerops` user). NEVER use `apt-get` on Alpine.
