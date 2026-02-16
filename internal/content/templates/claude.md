# Zerops

## Tool routing

| Task | Tool |
|------|----|
| Create/bootstrap services | `zerops_workflow workflow="bootstrap"` |
| Deploy code | `zerops_workflow workflow="deploy"` |
| Debug issues | `zerops_workflow workflow="debug"` |
| Scale services | `zerops_workflow workflow="scale"` |
| Configure env/ports | `zerops_workflow workflow="configure"` |
| Monitor/check status | `zerops_discover` |
| Search Zerops docs | `zerops_knowledge query="..."` |

For read-only queries (discover, events, logs), use tools directly.
For multi-step operations, start with the workflow.

## When to consult knowledge

Call `zerops_knowledge` with `runtime` and `services` parameters BEFORE:
- Creating or importing services — loads import.yml rules, dev/stage naming conventions, required fields
- Writing or modifying zerops.yml — loads runtime-specific build/run configuration
- Deploying code to stage — loads deployment workflow, verification steps
- Debugging issues — loads troubleshooting patterns, log access methods, common error causes
- Setting up environment variables or wiring between services — loads connection templates

Skip `zerops_knowledge` for direct commands where you already have all parameters:
- `zerops_manage action="restart" serviceHostname="appdev"` — explicit restart
- `zerops_delete serviceHostname="old-service"` — explicit delete
- `zerops_logs serviceHostname="appdev"` — read-only check
- `zerops_discover` — read-only state check

**Rule of thumb**: if the operation involves *generating config*, *making architectural decisions*, or *following a multi-step procedure*, call zerops_knowledge first. If it's a *single direct command* with clear parameters from the user, just execute it.
