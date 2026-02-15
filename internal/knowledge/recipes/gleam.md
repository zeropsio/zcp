# Gleam on Zerops

Gleam compiles to JavaScript and runs on Node.js.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: gleam@latest
      os: ubuntu
      buildCommands:
        - gleam export erlang-shipment
      deployFiles: build/erlang-shipment/~
    run:
      os: ubuntu
      ports:
        - port: 8000
          httpSupport: true
      start: ./entrypoint.sh run
```

## Gotchas
- **Ubuntu only** — Gleam has no Alpine support, must set `os: ubuntu`
- **Erlang shipment** — `gleam export erlang-shipment` creates a self-contained release
- **Bind 0.0.0.0** — configure your web framework (e.g. Wisp, Mist) to bind all interfaces
- **Deploy with tilde** — `build/erlang-shipment/~` extracts contents to `/var/www/`
