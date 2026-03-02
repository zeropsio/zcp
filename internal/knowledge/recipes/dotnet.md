# .NET on Zerops

ASP.NET Core app with PostgreSQL. Build with `dotnet publish`, deploy the published output.

## Keywords
dotnet, csharp, aspnet, asp.net core, kestrel, web api, postgresql

## TL;DR
ASP.NET Core with `dotnet publish` and PostgreSQL -- must configure `ASPNETCORE_URLS` to bind `0.0.0.0` and enable `ForwardedHeaders` middleware.

## zerops.yml

```yaml
zerops:
  - setup: api
    build:
      base: dotnet@9
      buildCommands:
        - dotnet publish -c Release -o app
      deployFiles: app/~
      cache: ~/.nuget
    run:
      ports:
        - port: 5000
          httpSupport: true
      envVariables:
        ASPNETCORE_URLS: http://0.0.0.0:5000
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}
      healthCheck:
        httpGet:
          port: 5000
          path: /status
      start: dotnet app.dll
```

## import.yml

```yaml
services:
  - hostname: api
    type: dotnet@9
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

## Configuration

Bind address -- set `ASPNETCORE_URLS` in zerops.yml envVariables and also configure in code for reliability:

```csharp
// Program.cs -- bind to 0.0.0.0 on the configured port
builder.WebHost.UseUrls("http://0.0.0.0:5000");
```

ForwardedHeaders middleware -- required because Zerops terminates TLS at the L7 balancer:

```csharp
// Program.cs -- add BEFORE other middleware
app.UseForwardedHeaders(new ForwardedHeadersOptions
{
    ForwardedHeaders = ForwardedHeaders.XForwardedFor | ForwardedHeaders.XForwardedProto
});
```

Database connection string:

```csharp
var host = Environment.GetEnvironmentVariable("DB_HOST");
var port = Environment.GetEnvironmentVariable("DB_PORT");
var user = Environment.GetEnvironmentVariable("DB_USER");
var pass = Environment.GetEnvironmentVariable("DB_PASS");
var name = Environment.GetEnvironmentVariable("DB_NAME");
var connStr = $"Host={host};Port={port};Username={user};Password={pass};Database={name}";
```

Health check endpoint:

```csharp
app.MapGet("/status", () => Results.Ok(new { status = "ok" }));
```

## Gotchas

- **Must bind `0.0.0.0` explicitly** -- set `ASPNETCORE_URLS=http://0.0.0.0:5000` in envVariables AND call `UseUrls` in code; the env var alone may not work reliably
- **ForwardedHeaders middleware required** -- without it, `HttpContext.Request.Scheme` reports `http` instead of `https` behind the Zerops proxy
- **Deploy with tilde** -- `app/~` extracts the contents of the `app/` folder directly into `/var/www/` (not `/var/www/app/`)
- **DLL name matches .csproj filename** -- if the project file is `MyApp.csproj`, the output is `MyApp.dll`, update the `start` command accordingly
- **Cache NuGet packages** -- `~/.nuget` caching avoids re-downloading packages on every build
- **Target framework** -- ensure `.csproj` `TargetFramework` matches the Zerops service version (e.g., `net9.0` for `dotnet@9`). Update NuGet packages (EF Core, Npgsql) to matching major versions
- **Port 5000 is the default** -- ASP.NET Core defaults to port 5000; match it in both `ports[]` and `ASPNETCORE_URLS`
- **healthCheck is for stage/production only** -- the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
