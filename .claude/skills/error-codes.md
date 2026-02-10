# ZCP Error Codes

| Code | Exit | Description | Resolution |
|------|------|-------------|------------|
| AUTH_REQUIRED | 2 | Not authenticated | Run `zaia login --token <value>` |
| AUTH_INVALID_TOKEN | 2 | Invalid token | Check token format |
| AUTH_TOKEN_EXPIRED | 2 | Expired token | Re-authenticate |
| TOKEN_NO_PROJECT | 2 | Token has no project access | Token needs project scope |
| TOKEN_MULTI_PROJECT | 2 | Token has 2+ projects | Use project-scoped token |
| INVALID_ZEROPS_YML | 3 | Invalid zerops.yml | Run `zcp validate --file zerops.yml` |
| INVALID_IMPORT_YML | 3 | Invalid import.yml | Check YAML syntax + required fields |
| IMPORT_HAS_PROJECT | 3 | import.yml contains project: section | Remove `project:` — only `services:` allowed |
| INVALID_SCALING | 3 | Invalid scaling parameters | Check min/max CPU/RAM ranges |
| INVALID_PARAMETER | 3 | Invalid parameter | Check parameter types and ranges |
| INVALID_ENV_FORMAT | 3 | Bad KEY=VALUE format | Use `KEY=value` syntax |
| FILE_NOT_FOUND | 3 | File doesn't exist | Verify file path |
| SERVICE_NOT_FOUND | 4 | Service doesn't exist | Run `zcp discover` to list services |
| PROCESS_NOT_FOUND | 4 | Process doesn't exist | Check process ID |
| PROCESS_ALREADY_TERMINAL | 4 | Process already finished | No action needed |
| PERMISSION_DENIED | 5 | Insufficient permissions | Check token permissions |
| NETWORK_ERROR | 6 | Network error | Check connectivity |
| API_ERROR | 1 | Generic API error | Check API response details |
| API_TIMEOUT | 6 | Timeout | Retry or increase timeout |
| API_RATE_LIMITED | 6 | Rate limit | Wait and retry |
