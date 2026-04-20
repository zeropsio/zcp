# symbol-naming-contract

The SymbolContract is a JSON object computed once per recipe before the first parallel dispatch. Every parallel sub-agent receives the identical contract. You consume it as the authoritative source for cross-codebase symbol names — env var keys, NATS subjects and queues, HTTP route paths, DTO interface names, hostname conventions — and for the fix-recurrence rules you must satisfy before returning.

## Schema

```
SymbolContract {
  envVarsByKind:      map[kind]map[role]envVarName
  httpRoutes:         map[routeKey]pathString
  natsSubjects:       map[subjectKey]subjectString
  natsQueues:         map[queueKey]queueString
  hostnames:          [ { role, dev, stage } ]
  dtos:               [ dtoInterfaceName ]
  fixRecurrenceRules: [ { id, positiveForm, preAttestCmd, appliesTo } ]
}
```

Fields explained:

- `envVarsByKind` is keyed by service kind (`db`, `cache`, `queue`, `storage`, `search`, `mail`). Each value is the map of platform-provided roles (`host`, `port`, `user`, `pass`, `name`, `apiUrl`, ...) to the exact env var key the running container sees. Derived from the plan's managed-service targets.
- `httpRoutes` declares cross-codebase route paths once; every codebase reads the same map. Key is a stable logical name (`status`, `items`), value is the URL path (`/api/status`, `/api/items`).
- `natsSubjects` and `natsQueues` pair producers (publish to subject) with consumers (subscribe with queue, for competing-consumer semantics). See principles/platform-principles/04-competing-consumer.md.
- `hostnames` lists each runtime target's `{role, dev, stage}` triple. Workers that share a codebase with a host runtime list only the host's triple.
- `dtos` names the cross-codebase DTO / interface symbols that must match across codebases.
- `fixRecurrenceRules` is the seeded list below — twelve positive-form invariants each with a pre-attest command you run via SSH before returning.

## Consumption conventions

1. **Read, do not re-derive.** Env var names, hostnames, NATS queues, and HTTP routes come from the contract exactly as the contract lists them. Independently inferring these names from framework convention or from reading managed-service docs is what produces cross-scaffold divergence (one codebase picks `DB_PASS`, another picks `DB_PASSWORD`, the platform provides one, the other crashes at runtime).
2. **Interpolate keys byte-for-byte.** When a codebase reads `process.env.DB_PASSWORD` (or equivalent in any language), `DB_PASSWORD` is the value from `envVarsByKind.db.pass` copied exactly. You do not lowercase, hyphenate, or abbreviate it.
3. **Cross-reference by logical key, not by value.** If your codebase calls a route, look it up by its logical key (`httpRoutes.status`) — that way the other codebase can change the path and your call follows automatically at stitch time.
4. **One contract per recipe.** Every parallel sub-agent reads byte-identical JSON. If your interpretation diverges from another codebase's, the divergence is in interpretation, not in the contract — re-read the field.

## The twelve seeded fix-recurrence rules

Each rule is a positive-form invariant plus a pre-attest command you run via SSH before returning. Non-zero exit on any rule that applies to your role means fix before you return.

### 1. nats-separate-creds (applies: api, worker)

Positive form: pass user and pass as separate ConnectionOptions fields; `servers` is `${queue_hostname}:${queue_port}` only.

Pre-attest: `ssh {host} "grep -rnE 'nats://[^ \t]*:[^ \t]*@' /var/www 2>/dev/null; test $? -eq 1"`

### 2. s3-uses-api-url (applies: api)

Positive form: S3 client `endpoint` is `process.env.storage_apiUrl` (https://), not `storage_apiHost` (http redirect).

Pre-attest: `ssh {host} "grep -rn 'storage_apiHost' /var/www/src 2>/dev/null; test $? -eq 1"`

### 3. s3-force-path-style (applies: api)

Positive form: S3 client `forcePathStyle: true`.

Pre-attest: `ssh {host} "grep -rn 'forcePathStyle' /var/www/src 2>/dev/null | grep -q true"`

### 4. routable-bind (applies: api, app)

Positive form: HTTP servers bind `0.0.0.0`, not `localhost` or `127.0.0.1`. See principles/platform-principles/02-routable-bind.md for framework forms.

Pre-attest: `ssh {host} "grep -rnE 'listen\\(.*(localhost|127\\.0\\.0\\.1)' /var/www/src 2>/dev/null; test $? -eq 1"`

### 5. trust-proxy (applies: api)

Positive form: Express / Fastify set trust proxy 1 (or framework equivalent) for L7 balancer IP forwarding. See principles/platform-principles/03-proxy-trust.md.

Pre-attest: `ssh {host} "grep -rnE 'trust[ _]proxy' /var/www/src 2>/dev/null | grep -q ."`

### 6. graceful-shutdown (applies: api, worker)

Positive form: worker and api register SIGTERM drain-then-exit; Nest apps call `app.enableShutdownHooks()`. See principles/platform-principles/01-graceful-shutdown.md.

Pre-attest: `ssh {host} "grep -rnE 'SIGTERM|enableShutdownHooks' /var/www/src 2>/dev/null | grep -q ."`

### 7. queue-group (applies: worker)

Positive form: NATS subscribers declare `queue: '<contract.NATSQueues[role]>'` for competing-consumer semantics.

Pre-attest: `ssh {host} "grep -rnE 'subscribe.*queue' /var/www/src 2>/dev/null | grep -q ."`

### 8. env-self-shadow (applies: any)

Positive form: `zerops.yaml` `run.envVariables` contains no `KEY: ${KEY}` self-shadow lines.

Pre-attest: `ssh {host} "grep -nE '^[[:space:]]+([A-Z_]+):[[:space:]]+\\$\\{\\1\\}[[:space:]]*$' /var/www/zerops.yaml 2>/dev/null; test $? -eq 1"`

### 9. gitignore-baseline (applies: any)

Positive form: `.gitignore` contains `node_modules`, `dist`, `.env`, `.DS_Store`, plus framework-specific cache dirs.

Pre-attest: `ssh {host} "grep -q node_modules /var/www/.gitignore && grep -q dist /var/www/.gitignore && grep -q '\\.env' /var/www/.gitignore"`

### 10. env-example-preserved (applies: any)

Positive form: framework-scaffolder's `.env.example` is kept if the scaffolder produced one.

Pre-attest: `ssh {host} "test ! -f /var/www/.env.example || test -s /var/www/.env.example"`

### 11. no-scaffold-test-artifacts (applies: any)

Positive form: no `preship.sh`, `.assert.sh`, or self-test shell scripts committed to the codebase.

Pre-attest: `ssh {host} "find /var/www -maxdepth 2 -type f \\( -name 'preship.sh' -o -name '*.assert.sh' \\) | head -n1 | grep -q . ; test $? -eq 1"`

### 12. skip-git (applies: any)

Positive form: framework scaffolders invoked with `--skip-git`, OR `.git` removed after scaffolder returns (`ssh {host} "rm -rf /var/www/.git"`).

Pre-attest: `ssh {host} "test ! -d /var/www/.git || ls /var/www/.git/HEAD 2>/dev/null | grep -q . ; test $? -eq 0 -o $? -eq 1"`

## Returning

Before you return, run each pre-attest command for every rule whose `appliesTo` matches your role (or `any`). Non-zero exit on any applicable rule means fix the underlying condition and re-run.
