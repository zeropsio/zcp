# Bun + PostgreSQL on Zerops

Basic Bun.serve() API with PostgreSQL database.

## Keywords
bun, postgresql, postgres, api, typescript

## TL;DR
Bun runtime with PostgreSQL — source deploy pattern with 0.0.0.0 binding.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: bun@1.2
      buildCommands:
        - bun install
      deployFiles:
        - src
        - package.json
        - bun.lockb
        - node_modules
      cache:
        - node_modules
    run:
      ports:
        - port: 3000
          httpSupport: true
      start: bun run src/index.ts
      envVariables:
        PORT: "3000"
```

## import.yml
```yaml
#yamlPreprocessor=on
services:
  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: app
    type: bun@1.2
    priority: 5
    enableSubdomainAccess: true
    envVariables:
      DATABASE_HOST: db
      DATABASE_PORT: "5432"
      DATABASE_NAME: ${db_dbName}
      DATABASE_USER: ${db_user}
    envSecrets:
      DATABASE_PASSWORD: ${db_password}
```

## App code requirement

**CRITICAL**: Bun.serve() must bind to `0.0.0.0`. Default `localhost` = 502 Bad Gateway.

```typescript
Bun.serve({
  hostname: "0.0.0.0",
  port: Number(process.env.PORT) || 3000,
  fetch(req) {
    return new Response("OK");
  },
});
```

## Gotchas
- **`hostname: "0.0.0.0"` is mandatory** in Bun.serve() — without it, Zerops L7 balancer cannot reach the app
- `bun.lockb` is binary — always include in deployFiles
- Don't deploy `node_modules` if using bundled output (`bun build --outdir dist`)
- For source deploy (this recipe): include `node_modules` in deployFiles
- PostgreSQL connection: use `${db_user}:${db_password}@db:5432/${db_dbName}` pattern
- Use `envSecrets` for passwords, `envVariables` for non-sensitive config
