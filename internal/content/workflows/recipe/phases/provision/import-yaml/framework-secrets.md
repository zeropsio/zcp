# Framework secrets — where the secret lives

When `plan.Research.needsAppSecret` is true, decide where the secret lives based on whether multiple services must agree on the same value.

## Shared secrets — project level

Encryption keys, CSRF secrets, session signing keys, and anything multiple services must share live at the project level. Set them after provision completes, once services are RUNNING:

```
zerops_env project=true action=set variables=["{SECRET_KEY_NAME}=<@generateRandomString(<32>)>"]
```

Frameworks accept the raw 32-char output directly, so the preprocessor expression is passed plain. Any `base64:` or `hex:` prefix that appears in framework documentation is dropped at this layer. `zerops_env set` is upsert and auto-restarts affected services so the new value takes effect on the next container start.

## Per-service secrets — envSecrets in the workspace import

Unique API tokens, webhook secrets, and any value that belongs to exactly one service live under that service's `envSecrets` in the workspace import.yaml.

## Correlated or encoded secrets — preprocess directly

For key pairs, encoded variants, or correlated values, call `zerops_preprocess` directly and set the results via `zerops_env set`. The preprocessor runs once, the resulting values go onto the project or the service, and the containers restart with the new values present.
