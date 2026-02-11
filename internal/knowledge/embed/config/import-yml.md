# import.yaml Specification

## Keywords
import.yaml, import yaml, project import, service creation, preprocessor, random, jwt, hash, bcrypt, argon2id, service definition

## TL;DR
`import.yaml` defines project infrastructure (services, databases, storage) with a YAML preprocessor for generating secrets, passwords, and JWT tokens at import time.

## Structure
```yaml
project:
  name: my-project
  corePackage: SERIOUS          # LIGHT or SERIOUS

services:
  # Runtime services
  - hostname: api
    type: nodejs@22
    minContainers: 1
    maxContainers: 3
    envSecrets:
      SECRET_KEY: ${random(32)}

  # Databases
  - hostname: db
    type: postgresql@16
    mode: HA                     # HA or NON_HA

  # Cache
  - hostname: cache
    type: valkey@7.2
    mode: HA

  # Storage
  - hostname: storage
    type: object-storage
    objectStorageSize: 10        # GB (1-100)

  # Shared Storage
  - hostname: files
    type: shared-storage
    mode: HA
```

## Preprocessor Functions

### Random Generation
```yaml
${random(length)}              # Random alphanumeric string
${randomInt(min, max)}         # Random integer in range
```

### Hashing
```yaml
${sha256(value)}               # SHA-256 hash
${bcrypt(value, rounds)}       # bcrypt hash (default 10 rounds)
${argon2id(value)}             # Argon2id hash
```

### JWT
```yaml
${jwt(algorithm, secret, payload)}
# algorithm: HS256, HS384, HS512, RS256, RS384, RS512
```

### Key Generation
```yaml
${generateRSAKeyPair(bits)}    # RSA key pair
${generateEd25519KeyPair()}    # Ed25519 key pair
```

## Scaling Configuration
```yaml
services:
  - hostname: api
    type: nodejs@22
    minContainers: 1
    maxContainers: 5
    verticalAutoscaling:
      cpuMode: SHARED            # SHARED or DEDICATED
      minCpu: 1
      maxCpu: 5
      minRam: 0.5
      maxRam: 4
      minFreeRamGB: 0.5          # absolute free RAM threshold (GB)
      minFreeRamPercent: 50      # % of granted RAM that must stay free
      minDisk: 1
      maxDisk: 20
```

## Service Types
| Type | Available Versions |
|------|-------------------|
| Runtimes | `nodejs@22` / `@20` / `@18`, `python@3.12`, `go@1.22` / `@1`, `php-nginx@8.4` / `@8.3` / `@8.1`, `php-apache@8.4` / `@8.3` / `@8.1`, `java@21` / `@17`, `dotnet@9` / `@8` / `@7` / `@6`, `rust@1.80` / `@1.78` / `@nightly` / `@stable`, `bun@1.2` / `@1.1` / `@nightly` / `@canary`, `deno@2` / `@1`, `elixir@1.16` / `@1`, `gleam@1.5` / `@1` |
| Containers | `alpine@3.20` / `@3.19` / `@3.18` / `@3.17`, `ubuntu@24.04` / `@22.04` |
| Databases | `postgresql@17` / `@16` / `@14`, `mariadb@10.6`, `clickhouse@25.3` |
| Cache | `valkey@7.2` (only version available), `keydb@6` (deprecated) |
| Search | `elasticsearch@8.16`, `meilisearch@1.10`, `typesense@27.1`, `qdrant@1.12` / `@1.10` |
| Queues | `kafka@3.8`, `nats@2.10` |
| Web | `nginx@1.22`, `static` |
| Storage | `object-storage`, `shared-storage` |

## Using zerops_import Tool

The `zerops_import` tool accepts YAML in two ways:
- **`content`** (inline YAML string) — preferred for programmatic use, no file I/O needed
- **`filePath`** (path to existing file) — for pre-existing files only

Always use `dryRun: true` first to validate before actual import.

### Validate Before Import
Use `zerops_validate` with `type: "import.yml"` (not `"import"`) to check syntax:
```
zerops_validate(content: "services:\n  ...", type: "import.yml")
```

## Gotchas
1. **`project:` section is optional**: When using ZAIA, import adds services to existing project — no `project:` section needed
2. **Preprocessor runs at import time**: `${random(32)}` generates once — value is fixed after import
3. **`mode` is immutable**: HA/NON_HA cannot be changed after creation
4. **`corePackage` matters**: LIGHT vs SERIOUS affects build hours, backup storage, and egress limits
5. **Object storage size range**: 1-100 GB — cannot exceed 100GB per service
6. **Validate type is `import.yml`**: Using `type: "import"` returns "Unknown file type" — always use `type: "import.yml"`
7. **Dry-run ≠ real import**: Some service types (e.g., `valkey@8`) pass dry-run validation but fail at actual import — use exact documented versions
8. **`mode` is mandatory for HA-capable services**: PostgreSQL, MariaDB, Valkey, KeyDB, shared-storage, elasticsearch, and other HA-capable services require `mode: NON_HA` or `mode: HA` explicitly — dry-run passes without it but real import fails with "Mandatory parameter is missing"

## See Also
- zerops://config/zerops-yml
- zerops://platform/infrastructure
- zerops://platform/scaling
- zerops://services/_common-database
