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
with the exact fields the platform rejected.

Shape:

```json
{
  "code": "API_ERROR",
  "apiCode": "projectImportInvalidParameter",
  "error": "Invalid parameter provided.",
  "suggestion": "The platform flagged specific fields — see apiMeta for each field's failure reason.",
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

Each `apiMeta[].metadata` key is a **field path** (e.g. `{hostname}.mode`,
`build.base`, `parameter`); each value is the list of reasons. Fix those
specific fields in your YAML and retry — do not guess.

Common shapes you will see:

- `projectImportInvalidParameter` with `metadata: {"{host}.mode": ["..."]}` —
  the service-type/mode combination is not allowed.
- `projectImportMissingParameter` with `metadata: {"parameter": ["{host}.mode"]}` —
  a required field is missing.
- `serviceStackTypeNotFound` with `metadata: {"serviceStackTypeVersion": ["nodejs@99"]}` —
  the version string is not in the platform catalog.
- `zeropsYamlInvalidParameter` with `metadata: {"build.base": ["unknown ..."]}` —
  zerops.yaml validator caught the field before the build cycle.
- `yamlValidationInvalidYaml` with `metadata: {"reason": ["line N: ..."]}` —
  YAML syntax error with line number.

Per-service failures on import land in `serviceErrors[].meta` with the
same shape — one entry per failing service-stack.
