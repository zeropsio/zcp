# Debug Mode on Zerops

## Keywords
debug, debug mode, build debug, pause, breakpoint, build troubleshooting, zsc debug

## TL;DR
Debug mode pauses the build pipeline at configurable points (before first command, on failure, after last command) with a 60-minute total time limit.

## Pause Points
- **Disable** — no pausing (default)
- **Before First Command** — pause before any build/prepare command runs
- **On Command Failure** — pause only when a command fails
- **After Last Command** — pause after all commands complete

## Debug Phases
1. Build phase
2. Runtime prepare phase (optional)

## Commands (inside debug session)
```bash
zsc debug continue    # Resume from current pause
zsc debug success     # End phase as success (must have valid deployFiles)
zsc debug fail        # Terminate with failure
```

## Time Limit
**60 minutes** total for entire build process. After that, build is terminated.

## Gotchas
1. **60-minute hard limit**: Applies to total build time including debug pauses — plan accordingly
2. **`success` requires valid deployFiles**: Cannot force-succeed if deploy artifacts are missing

## See Also
- zerops://platform/build-cache
- zerops://config/zerops-yml
- zerops://platform/infrastructure
