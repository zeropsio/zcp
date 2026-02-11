# Alpine on Zerops

## Keywords
alpine, linux, container, lightweight, base, musl, apk, minimal, custom runtime

## TL;DR
Alpine is the default base for all Zerops runtimes (~5MB), using musl libc and apk package manager — use it unless you specifically need Ubuntu's glibc or apt ecosystem.

## Zerops-Specific Behavior
- Versions: 3.20 (latest), 3.19, 3.18, 3.17
- Size: ~5MB (vs ~100MB for Ubuntu)
- Package manager: `apk add --no-cache`
- libc: musl (not glibc)
- Default base for all managed runtimes (Node.js, Python, Go, etc.)
- Full SSH access available
- Working directory: `/var/www` (same as all runtimes)

## Configuration
```yaml
# import.yaml
services:
  - hostname: myservice
    type: alpine@3.20
    minContainers: 1
```

```yaml
# zerops.yaml
zerops:
  - setup: myservice
    build:
      base: alpine@3.20
      prepareCommands:
        - apk add --no-cache curl jq
      buildCommands:
        - echo "build step"
      deployFiles: ./
    run:
      start: ./myapp
```

## Gotchas
1. **musl vs glibc**: Some C libraries won't compile on musl — use Ubuntu if you hit linking errors
2. **No apt-get**: Use `apk add --no-cache` — Alpine doesn't use Debian packages
3. **Minimal by default**: No common tools pre-installed — add them in `prepareCommands`

## See Also
- zerops://decisions/choose-runtime-base
- zerops://services/ubuntu
- zerops://services/_common-runtime
