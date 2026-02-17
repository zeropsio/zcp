# .NET on Zerops

ASP.NET Core app. Build with `dotnet publish`, deploy the published output.

## Keywords
dotnet, csharp, aspnet, asp.net core, kestrel, web api

## TL;DR
ASP.NET Core with `dotnet publish` — must bind `0.0.0.0` in code via `UseUrls`, not just env var.

## zerops.yml
```yaml
zerops:
  - setup: app
    build:
      base: dotnet@9
      buildCommands:
        - dotnet publish -c Release -o app
      deployFiles: app/~
    run:
      ports:
        - port: 5000
          httpSupport: true
      start: ./MyApp
```

## import.yml
```yaml
services:
  - hostname: app
    type: dotnet@9
    enableSubdomainAccess: true
```

## Code requirement (binding)
```csharp
// Program.cs — MUST bind 0.0.0.0 explicitly in code
builder.WebHost.UseUrls("http://0.0.0.0:5000");
```

Setting `ASPNETCORE_URLS` env var alone is **insufficient** — the binding must be set in code via `UseUrls`.

## Gotchas
- **Must bind 0.0.0.0 in code** — `builder.WebHost.UseUrls("http://0.0.0.0:5000")` is required
- **Env var binding insufficient** — `ASPNETCORE_URLS` alone does not work reliably on Zerops
- **Deploy with tilde** — `app/~` extracts contents to `/var/www/` (not `/var/www/app/`)
- **Port 5000** is the default ASP.NET Core port — match it in `ports[]`
- **Self-contained publish** — add `-r linux-x64 --self-contained` for deployments without .NET runtime
