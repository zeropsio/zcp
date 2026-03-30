---
description: ".NET 9 recipe for Zerops demonstrating a minimal web service with automatic PostgreSQL migrations and a health check endpoint that verifies live database connectivity. Includes ready-made configurations for every stage of the development lifecycle — from AI agent environments to highly-available production."
---

# .NET Hello World on Zerops


# .NET Hello World on Zerops


# .NET Hello World on Zerops


# .NET Hello World on Zerops


# .NET Hello World on Zerops


# .NET Hello World on Zerops





## Keywords
dotnet, .net, csharp, kestrel, aspnet, aspnetcore, zerops.yml, publish

## TL;DR
.NET SDK pre-installed. Use `dotnet publish -c Release -o app`. Bind `0.0.0.0` in code -- Kestrel defaults to localhost.

### Base Image

Includes .NET SDK, ASP.NET, `git`.

### Build Procedure

1. Set `build.base: dotnet@9` (or desired version)
2. `buildCommands: [dotnet publish -c Release -o app]` -- `publish` preferred over `build`
3. `deployFiles: [app/~]` -> files at `/var/www/`
4. `run.start: dotnet {ProjectName}.dll` -- DLL name = .csproj FILENAME (NOT RootNamespace)
   Example: `myapp.csproj` -> output is `myapp.dll` -> `start: dotnet myapp.dll`

### Binding (Critical)

Kestrel defaults to localhost -> 502. MUST bind in code:
```csharp
app.Urls.Add("http://0.0.0.0:5000");
```
Do NOT rely solely on `ASPNETCORE_URLS` env var.

### Key Settings

Cache: `~/.nuget`.

### Resource Requirements

**Dev** (build on container): `minRam: 1` — `dotnet build` peak ~0.8 GB.
**Stage/Prod**: `minRam: 0.5` — Kestrel + CLR needs baseline allocation.

### Common Mistakes

- Using RootNamespace as DLL name -> "file not found" (DLL name = .csproj filename, not namespace)
- Missing 0.0.0.0 binding in code -> 502 Bad Gateway
- Using `dotnet build` instead of `dotnet publish` -> missing runtime assets
- `ASPNETCORE_URLS` env var alone insufficient -> must set in code via UseUrls

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, `start: zsc noop --silent` (idle container -- agent starts `dotnet run` manually via SSH for iteration)
**Prod deploy**: `buildCommands: [dotnet publish -c Release -o app]`, `deployFiles: [app/~]`, `start: dotnet {name}.dll`

## zerops.yml

> Reference implementation — learn the patterns, adapt to your project.

```yaml
zerops:
  # Production setup — compile both app and migration, deploy
  # minimal framework-dependent artifacts to the runtime container.
  - setup: prod
    build:
      base: dotnet@9

      buildCommands:
        # Publish framework-dependent artifacts (runtime provided by
        # the dotnet@9 container image — no bundled runtime needed).
        # dotnet publish implicitly restores NuGet packages first.
        - dotnet publish App/App.csproj -c Release -o out/app
        - dotnet publish Migrate/Migrate.csproj -c Release -o out/migrate

      # Deploy only the published output — no source, no obj/ dirs.
      deployFiles:
        - ./out

      # cache: true snapshots the global NuGet package cache
      # (~/.nuget/packages) in the build container image, so
      # subsequent builds skip re-downloading packages.
      cache: true

    # Readiness check: verifies new containers respond before the
    # project balancer routes traffic to them (zero-downtime deploy).
    deploy:
      readinessCheck:
        httpGet:
          port: 8080
          path: /

    run:
      base: dotnet@9

      # Run the migration exactly once per deploy version across all
      # containers. initCommands runs before start on every container
      # — zsc execOnce ensures only one container executes, all others
      # wait. Placed in initCommands (not buildCommands) so schema
      # changes deploy atomically with the new application code.
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- dotnet ./out/migrate/Migrate.dll

      ports:
        - port: 8080
          httpSupport: true

      envVariables:
        ASPNETCORE_ENVIRONMENT: Production
        # Kestrel listens on all interfaces at port 8080.
        ASPNETCORE_URLS: http://0.0.0.0:8080
        # Referencing variables — Zerops injects credentials for the
        # 'db' service using the pattern {hostname}_{credential}.
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

      start: dotnet ./out/app/App.dll

  # Development setup — deploy full source code so the developer can
  # SSH in and run the app interactively. NuGet packages are restored
  # in the build container to populate the cache; the developer runs
  # 'dotnet restore' again after SSH (runtime container starts fresh).
  - setup: dev
    build:
      base: dotnet@9

      buildCommands:
        # Restore packages to populate the NuGet cache in the build
        # container (cached via cache: true for subsequent builds).
        # The developer will need to run 'dotnet restore' after SSH —
        # NuGet cache lives in the build container, not the runtime.
        - dotnet restore App/App.csproj
        - dotnet restore Migrate/Migrate.csproj

      # Deploy the entire working directory — source code, project
      # files, and zerops.yaml (so 'zcli push' works from SSH).
      deployFiles: ./

      cache: true

    run:
      base: dotnet@9

      # Migration still runs on deploy — database is ready when the
      # developer SSHs in. Compiles Migrate on the fly via 'dotnet
      # run'; zsc execOnce ensures single execution per version.
      initCommands:
        - zsc execOnce ${appVersionId} --retryUntilSuccessful -- dotnet run --project Migrate/Migrate.csproj

      ports:
        - port: 8080
          httpSupport: true

      envVariables:
        ASPNETCORE_ENVIRONMENT: Development
        ASPNETCORE_URLS: http://0.0.0.0:8080
        # HOME is required — .NET CLI and NuGet use it to locate
        # package caches and temp dirs in the runtime container.
        HOME: /home/zerops
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

      # Container stays idle — developer starts the app via SSH:
      #   dotnet restore App/App.csproj
      #   dotnet run --project App/App.csproj
      start: zsc noop --silent
```
