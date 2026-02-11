# .NET on Zerops

## Keywords
dotnet, csharp, aspnet, kestrel, nuget, blazor, entity framework, dotnet build, dotnet publish

## TL;DR
.NET on Zerops uses Kestrel as the default web server. Use `dotnet build -o app` to compile and deploy the `app/~` directory with tilde syntax.

## Zerops-Specific Behavior
- Versions: 9, 8, 7, 6
- Base: Alpine (default)
- Web server: Kestrel (built-in)
- Working directory: `/var/www`
- No default port — Kestrel defaults to 5000 but configure explicitly
- Supports ASP.NET Core, Blazor, minimal APIs

## Configuration
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

### Self-Contained Build
```yaml
build:
  buildCommands:
    - dotnet publish -c Release -r linux-musl-x64 --self-contained -o app
  deployFiles:
    - app/~
```

## Gotchas
1. **Set `ASPNETCORE_URLS`**: Must bind to `0.0.0.0` — Kestrel defaults to localhost which won't receive traffic
2. **Alpine = linux-musl-x64**: Use `linux-musl-x64` runtime identifier for self-contained builds on Alpine
3. **Cache NuGet packages**: `~/.nuget` cache avoids re-downloading packages every build
4. **Tilde syntax for deploy**: Use `app/~` to deploy contents of app directory to root
5. **Use `dotnet build`**: Recipes use `dotnet build -o app`, not `dotnet publish` for framework-dependent deploys
6. **Start with `dotnet dotnet.dll`**: The runtime starts the compiled DLL directly

## See Also
- zerops://services/_common-runtime
- zerops://examples/zerops-yml-runtimes
- zerops://platform/scaling
