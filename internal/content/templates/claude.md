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
