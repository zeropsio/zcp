# Node.js (Express) on Zerops

Express.js API with TypeScript, PostgreSQL via pg driver. Minimal Node.js backend template.

## Keywords
nodejs, express, typescript, postgresql, api, node, pg

## TL;DR
Express.js TypeScript API on port 3000 with PostgreSQL -- build compiles TS, deploys `dist/` + `node_modules/`.

## zerops.yml
```yaml
zerops:
  - setup: api
    build:
      base: nodejs@20
      buildCommands:
        - npm i
        - npm run build
      deployFiles:
        - ./dist
        - ./node_modules
        - ./package.json
      cache: node_modules
    run:
      base: nodejs@20
      ports:
        - port: 3000
          httpSupport: true
      envVariables:
        NODE_ENV: production
        DB_NAME: db
        DB_HOST: db
        DB_PORT: "5432"
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
      start: node ./dist/index.js
      healthCheck:
        httpGet:
          port: 3000
          path: /status
```

## import.yml
```yaml
services:
  - hostname: api
    type: nodejs@20
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

## Configuration

Database connection reads env vars directly:

```typescript
// config.ts
export const config = {
  db: {
    host: process.env.DB_HOST,
    port: parseInt(process.env.DB_PORT || '5432'),
    username: process.env.DB_USER,
    password: process.env.DB_PASS,
    database: process.env.DB_NAME,
  }
};
```

Health check endpoint:

```typescript
// app.ts
app.get('/status', (_, res) => {
  res.status(200).send({ status: 'UP' });
});
```

## Gotchas

- **DB_HOST uses hardcoded hostname** `db` matching the import.yml service hostname (Zerops internal VXLAN network)
- **TypeScript must be compiled** during build -- deploy `dist/` directory, not source `.ts` files
- **node_modules deployed alongside dist** because runtime dependencies are needed (pg, express, etc.)
- **Bind to 0.0.0.0** -- Express defaults to all interfaces, but if configuring manually ensure you do not bind to `127.0.0.1`
- **trust proxy** -- if reading client IPs behind the Zerops L7 balancer, set `app.set('trust proxy', true)`
- **${db_password}** is auto-injected by Zerops from the `db` service secret -- do not hardcode credentials
