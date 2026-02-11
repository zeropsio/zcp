# Logging on Zerops

## Keywords
logging, logs, syslog, build logs, runtime logs, service log, log access, log severity

## TL;DR
Zerops captures stdout/stderr as logs; use syslog output format for severity filtering. Access via GUI or `zcli service log`.

## Log Types
1. **Build logs** — output from build pipeline
2. **Prepare runtime logs** — output from custom runtime image creation
3. **Runtime/Database logs** — operational output (stdout/stderr)

## Access Methods

### GUI
- Project detail → service → Logs section
- Filter by severity, time range, container

### CLI
```bash
zcli service log <service-name>                  # Runtime logs
zcli service log <service-name> --showBuildLogs  # Build logs
```

## Severity Filtering
Logs must output to **syslog format** for severity filtering to work. Plain stdout/stderr logs appear as "info" level.

## Gotchas
1. **Syslog format required**: Without syslog formatting, all logs appear as same severity — no filtering possible
2. **Build logs separate**: Use `--showBuildLogs` flag in CLI — not shown by default

## See Also
- zerops://operations/log-forwarding
- zerops://operations/metrics
- zerops://config/zcli
