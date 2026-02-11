# Gleam on Zerops

## Keywords
gleam, beam, erlang, functional, type-safe, gleam build, javascript target, erlang target

## TL;DR
Gleam on Zerops compiles to either Erlang (BEAM) or JavaScript targets; use Erlang target for server-side and deploy as an Erlang shipment with tilde syntax.

## Zerops-Specific Behavior
- Versions: 1.5, 1
- Base: Alpine (default), Erlang/OTP pre-installed
- Build tool: Gleam CLI (pre-installed)
- Working directory: `/var/www`
- No default port — must configure
- Targets: Erlang (server) or JavaScript (Node.js runtime needed)

## Configuration
```yaml
# Erlang target
zerops:
  - setup: api
    build:
      base: gleam@1.5
      buildCommands:
        - gleam export erlang-shipment
      deployFiles:
        - build/erlang-shipment/~
      cache:
        - build
    run:
      start: ./entrypoint.sh run
      ports:
        - port: 3000
          httpSupport: true
```

### JavaScript Target
```yaml
zerops:
  - setup: api
    build:
      base: gleam@1.5
      buildCommands:
        - gleam build --target javascript
      deployFiles:
        - build
        - node_modules
    run:
      start: node build/dev/javascript/myapp/main.mjs
      ports:
        - port: 3000
          httpSupport: true
```

## Gotchas
1. **Erlang target is default**: For server-side apps, Erlang target is recommended — JavaScript target needs Node.js runtime
2. **Tilde syntax for deploy**: Use `build/erlang-shipment/~` to deploy contents to root
3. **Start with `./entrypoint.sh run`**: After tilde extraction, entrypoint is at root level
4. **JavaScript target needs Node.js**: If using JS target, ensure Node.js is available in runtime

## See Also
- zerops://services/_common-runtime
- zerops://services/elixir
- zerops://examples/zerops-yml-runtimes
