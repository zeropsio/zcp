# Ubuntu on Zerops

## Keywords
ubuntu, linux, container, debian, glibc, apt, full os, system packages, cgo

## TL;DR
Ubuntu provides a full Debian-based environment (~100MB) with glibc — use it when Alpine's musl libc causes compatibility issues or you need apt packages.

## Zerops-Specific Behavior
- Base image: Ubuntu 24.04 LTS
- Size: ~100MB (vs ~5MB for Alpine)
- Package manager: `apt-get install -y`
- libc: glibc (full compatibility)
- Full SSH access available
- Working directory: `/var/www`

## Configuration
```yaml
# import.yaml
services:
  - hostname: myservice
    type: ubuntu@24.04
    minContainers: 1
```

```yaml
# zerops.yaml
zerops:
  - setup: myservice
    build:
      base: ubuntu@24.04
      prepareCommands:
        - apt-get update && apt-get install -y build-essential
      buildCommands:
        - make build
      deployFiles: ./app
    run:
      start: ./app
```

## When to Use Ubuntu Over Alpine
- Go apps with CGO enabled (C bindings need glibc)
- Python packages with C extensions that fail on musl
- Software that explicitly requires Debian/Ubuntu packages
- Legacy applications depending on glibc behavior

## Gotchas
1. **Larger base image**: ~100MB vs ~5MB — slower initial deploy, more disk usage
2. **apt-get update required**: Always run `apt-get update` before `apt-get install` in prepareCommands
3. **More attack surface**: Larger OS = more packages = more potential vulnerabilities

## See Also
- zerops://decisions/choose-runtime-base
- zerops://services/alpine
- zerops://services/_common-runtime
