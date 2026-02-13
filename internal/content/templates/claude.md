# Zerops

## MANDATORY: Knowledge-first workflow

For ANY task involving creating services, deploying, or debugging:

1. Call `zerops_workflow` to get the step-by-step guide
2. Follow the workflow steps IN ORDER — do not skip any
3. Call `zerops_knowledge` BEFORE generating any YAML (import.yml or zerops.yml)
4. Call `zerops_context` at the start of any new session or when you need platform fundamentals

### Rules

- **Never** generate import.yml or zerops.yml content without loading knowledge first via `zerops_knowledge`
- **Never** skip the workflow for multi-step operations — it prevents errors and ensures correct ordering
- **Always** validate import.yml with `zerops_import dryRun=true` before the real import
- **Always** use `zerops_knowledge` with runtime/services params to get contextual briefings for YAML generation
- **Always** bind to `0.0.0.0` — localhost/127.0.0.1 = 502 Bad Gateway (check runtime exceptions for framework-specific syntax)
- For bootstrap, default is **dev+stage service pairs** (`appdev` + `appstage`). Single service only if user explicitly requests it.
- For simple read-only queries (discover, events, logs), workflows are optional

## Quick reference

| Task | First tool to call |
|------|--------------------|
| Create/bootstrap services | `zerops_workflow workflow="bootstrap"` |
| Deploy code | `zerops_workflow workflow="deploy"` |
| Debug issues | `zerops_workflow workflow="debug"` |
| Scale services | `zerops_workflow workflow="scale"` |
| Configure env/ports | `zerops_workflow workflow="configure"` |
| Monitor/check status | `zerops_discover` |
| Platform fundamentals | `zerops_context` |
| Search docs | `zerops_knowledge query="..."` |
