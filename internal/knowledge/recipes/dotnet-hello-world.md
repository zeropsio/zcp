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
