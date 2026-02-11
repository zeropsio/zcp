# zerops.yaml Runtime Examples

## Keywords
examples, zerops.yaml, nodejs example, python example, php example, go example, java example, rust example, multi-service, monorepo

## TL;DR
Copy-paste ready zerops.yaml configurations for all supported runtimes and common framework patterns.

## Node.js (Express)
```yaml
zerops:
  - setup: api
    build:
      base: nodejs@22
      buildCommands:
        - pnpm i
        - pnpm build
      deployFiles:
        - dist
        - node_modules
        - package.json
      cache:
        - node_modules
        - .pnpm-store
    run:
      start: node dist/index.js
      ports:
        - port: 3000
          httpSupport: true
```

## Node.js (Next.js)
```yaml
zerops:
  - setup: web
    build:
      base: nodejs@22
      buildCommands:
        - pnpm i
        - pnpm build
      deployFiles:
        - .next
        - node_modules
        - package.json
        - next.config.js
        - public
      cache:
        - node_modules
        - .next/cache
    run:
      start: pnpm start
      ports:
        - port: 3000
          httpSupport: true
```

## Python (FastAPI)
```yaml
zerops:
  - setup: api
    build:
      base: python@3.12
      addToRunPrepare:
        - .
      buildCommands:
        - pip install --no-cache-dir -r requirements.txt
      deployFiles: ./
    run:
      start: python -m uvicorn main:app --host 0.0.0.0 --port 8000
      ports:
        - port: 8000
          httpSupport: true
```

## Python (Django)
```yaml
zerops:
  - setup: web
    build:
      base: python@3.12
      addToRunPrepare:
        - .
      buildCommands:
        - pip install --no-cache-dir -r requirements.txt
        - python manage.py collectstatic --noinput
      deployFiles: ./
    run:
      start: gunicorn myproject.wsgi:application --bind 0.0.0.0:8000
      ports:
        - port: 8000
          httpSupport: true
```

## PHP (Laravel)
```yaml
zerops:
  - setup: app
    build:
      base: php@8.4
      buildCommands:
        - composer install --ignore-platform-reqs
        - php artisan config:cache
        - php artisan route:cache
      deployFiles: ./
      cache:
        - vendor
        - composer.lock
    run:
      base: php-nginx@8.4
      documentRoot: public
      ports:
        - port: 80
          httpSupport: true
      envVariables:
        APP_ENV: production
        TRUSTED_PROXIES: "127.0.0.1,10.0.0.0/8"
```

## Go
```yaml
zerops:
  - setup: api
    build:
      base: go@1
      buildCommands:
        - go build -o app ./cmd/server
      deployFiles:
        - app
    run:
      start: ./app
      ports:
        - port: 8080
          httpSupport: true
```

## Rust
```yaml
zerops:
  - setup: api
    build:
      base: rust@stable
      buildCommands:
        - cargo build --release
      deployFiles:
        - ./target/release/~/myapp
      cache:
        - target
        - ~/.cargo/registry
    run:
      start: ./myapp
      ports:
        - port: 8080
          httpSupport: true
```

## Java (Spring Boot)
```yaml
zerops:
  - setup: api
    build:
      base: java@21
      buildCommands:
        - ./mvnw clean install --define maven.test.skip
      deployFiles:
        - ./target/api.jar
      cache:
        - .m2
    run:
      start: java -jar target/api.jar
      ports:
        - port: 8080
          httpSupport: true
```

## .NET
```yaml
zerops:
  - setup: api
    build:
      base: dotnet@8
      buildCommands:
        - dotnet build -o app
      deployFiles:
        - app/~
      cache:
        - ~/.nuget
    run:
      start: dotnet dotnet.dll
      ports:
        - port: 5000
          httpSupport: true
      envVariables:
        ASPNETCORE_URLS: http://0.0.0.0:5000
```

## Elixir (Phoenix)
```yaml
zerops:
  - setup: web
    build:
      base: elixir@1.16
      buildCommands:
        - mix local.hex --force && mix local.rebar --force
        - mix deps.get --only prod
        - MIX_ENV=prod mix compile
        - MIX_ENV=prod mix assets.deploy
        - MIX_ENV=prod mix phx.digest
        - MIX_ENV=prod mix release --overwrite
      deployFiles:
        - _build/prod/rel/app/~
      cache:
        - deps
        - _build
    run:
      base: alpine@latest
      start: bin/app start
      ports:
        - port: 4000
          httpSupport: true
      envVariables:
        PHX_SERVER: "true"
```

## Static (React/Vue/Angular SPA)
```yaml
zerops:
  - setup: web
    build:
      base: nodejs@22
      buildCommands:
        - pnpm i && pnpm build
      deployFiles:
        - dist/~
      cache:
        - node_modules
    run:
      base: static
```

## Multi-Service Monorepo
```yaml
zerops:
  - setup: api
    build:
      base: nodejs@22
      buildCommands:
        - pnpm i
        - pnpm build:api
      deployFiles:
        - packages/api/dist
        - node_modules
      cache:
        - node_modules
    run:
      start: node packages/api/dist/index.js
      ports:
        - port: 3000
          httpSupport: true

  - setup: web
    build:
      base: nodejs@22
      buildCommands:
        - pnpm i
        - pnpm build:web
      deployFiles:
        - packages/web/dist/~
      cache:
        - node_modules
    run:
      base: static
```

## Gleam (Wisp + Mist)
```yaml
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

## Bun
```yaml
zerops:
  - setup: api
    build:
      base: bun@1.1
      buildCommands:
        - bun install
        - bun run build
      deployFiles:
        - package.json
        - dist
      cache:
        - node_modules
    run:
      start: bun run start:prod
      ports:
        - port: 3000
          httpSupport: true
```

## Deno
```yaml
zerops:
  - setup: api
    build:
      base: deno@1
      buildCommands:
        - deno task build
      deployFiles:
        - dist
        - deno.jsonc
    run:
      start: deno task start
      ports:
        - port: 8000
          httpSupport: true
```

## Discord Bot (No HTTP — Background Process)
```yaml
zerops:
  - setup: bot
    build:
      base: nodejs@22
      buildCommands:
        - pnpm i
        - pnpm build
      deployFiles:
        - dist
        - node_modules
        - package.json
    run:
      start: node dist/bot.js
```
Note: No `ports` section — background process without HTTP server.

## Multi-Base: Node.js → Static (SSG)
```yaml
zerops:
  - setup: web
    build:
      base: nodejs@22
      buildCommands:
        - pnpm i && pnpm build
      deployFiles:
        - dist/~
      cache:
        - node_modules
    run:
      base: static
```

## See Also
- zerops://config/zerops-yml
- zerops://config/deploy-patterns
- zerops://examples/connection-strings
- zerops://services/_common-runtime
