---
id: develop-api-error-meta
priority: 2
phases: [bootstrap-active, develop-active]
title: "API error responses — read apiMeta for field-level detail"
---

### Read `apiMeta` on every error response

Any `zerops_*` tool that surfaces a 4xx from the Zerops API returns
structured field-level detail on an optional `apiMeta` JSON key.
Missing key = server sent no detail; present key = an array of items
with the exact fields Zerops rejected.

Shape:

```json
{
  "code": "API_ERROR",
  "apiCode": "projectImportInvalidParameter",
  "error": "Invalid parameter provided.",
  "suggestion": "Zerops flagged specific fields — see apiMeta for each field's failure reason.",
  "apiMeta": [
    {
      "code": "projectImportInvalidParameter",
      "error": "Invalid parameter provided.",
      "metadata": {
        "storage.mode": ["mode not supported"]
      }
    }
  ]
}
```

Each `apiMeta[].metadata` key is a **field path** (e.g.
`{hostname}.mode`, `build.base`, `parameter`); each value lists the
reasons. Fix those fields in your YAML and retry — do not guess.

Common `apiCode` shapes:

| `apiCode` | `metadata` key | Meaning |
|---|---|---|
| `projectImportInvalidParameter` | `<host>.mode` | service-type/mode combination not allowed |
| `projectImportMissingParameter` | `parameter` (value `<host>.mode`) | required field missing |
| `serviceStackTypeNotFound` | `serviceStackTypeVersion` | version string not in platform catalog |
| `zeropsYamlInvalidParameter` | `build.base` etc. | zerops.yaml validator caught the field pre-build |
| `yamlValidationInvalidYaml` | `reason` (with `line N:`) | YAML syntax error |

Per-service import failures land in `serviceErrors[].meta` with the
same shape — one entry per failing service-stack.
