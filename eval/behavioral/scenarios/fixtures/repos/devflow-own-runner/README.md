# Notes board

A small notes board: one Node app, served in development by its own runner.

## Develop

Requires Node 24.

```sh
npm install
npm run dev
```

`npm run dev` runs `scripts/dev-runner.mjs`, which serves the app with
live reload on port 5173.

The runner listens on `127.0.0.1` only. For remote development (a VM, a
container, a cloud dev box) set `NOTES_DEV_HOST=0.0.0.0`. Requests that
arrive under another host name are refused until it is listed in
`NOTES_DEV_ALLOWED_HOSTS` (comma-separated host names, or `*`).

## Production

`zerops.yml` builds and serves the production app (`setup: prod`).
