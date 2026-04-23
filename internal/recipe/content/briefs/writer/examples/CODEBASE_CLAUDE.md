# CODEBASE_CLAUDE

## Pass — operational, repo-local

```
## Dev loop

SSH into the dev container, then:

    npm run start:dev       # hot-reload server
    npm run migration:run   # one migration by hand
    npm run dev:reset       # truncate + re-seed

## Health

- `GET /health` — 200 `{"status":"ok"}`
- `GET /debug/remote-ip` — echoes X-Forwarded-For (preship check)

## Container traps

- SSHFS reuses uid 1001 — chown host-created files to 1001.
```

## Fail — deploy steps on the wrong surface

```
## Deploying to Zerops

1. Push to GitHub
2. Create a Zerops project
3. Import the import.yaml
```

Reader has the repo checked out, not someone deploying.
