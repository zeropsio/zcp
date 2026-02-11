# Build Cache on Zerops

## Keywords
build cache, cache, prepare commands, build commands, cache invalidation, two-layer cache, build optimization, deploy cache

## TL;DR
Zerops uses a two-layer cache (base + build); changing `prepareCommands` invalidates BOTH layers, and `cache: false` doesn't disable caching of files outside `/build/source`.

## Two-Layer Architecture

### Layer 1: Base Layer
Contains: OS + dependencies + `prepareCommands` output.
Invalidated by changes to: `build.os`, `build.base`, `build.prepareCommands`, `build.cache`.

### Layer 2: Build Layer
Contains: State after `buildCommands` execution.
Invalidated by: Any base layer change (layer coupling).

## Cache Configuration

```yaml
build:
  cache:
    - node_modules
    - .next/cache
```

| Value | Behavior |
|-------|----------|
| `cache: true` | Preserve entire build container |
| `cache: false` | Only `/build/source` not cached — files elsewhere (Go modules, pip) remain cached |
| `cache: [paths]` | Cache specific paths only |

## Path Patterns
Uses Go's `filepath.Match` syntax:
- `node_modules` — exact directory
- `subdir/*.txt` — wildcard in subdirectory
- `package*` — prefix match

## Cache Restoration
- Files restored to `/build/source` in **no-clobber mode** (source code takes precedence)
- Cache file movement happens via container-level rename (fast, no compression)

## Build Lifecycle
1. Build container startup
2. Cache restored to `/build/source` (no-clobber)
3. `buildCommands` execute
4. Cache files moved outside `/build/source`

## Manual Invalidation
- API: `DELETE /service-stack/{id}/build-cache`
- Version activation via API also invalidates

## Gotchas
1. **`cache: false` is misleading**: Files outside `/build/source` (Go modules in `~/go`, pip in `/usr/lib/python`) stay cached
2. **`prepareCommands` change = full rebuild**: Both cache layers are invalidated — expect longer build
3. **No-clobber restoration**: Cached files don't overwrite source files — git-tracked files always win

## See Also
- zerops://config/zerops-yml
- zerops://platform/infrastructure
- zerops://gotchas/common
