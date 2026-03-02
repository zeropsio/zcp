# Recipe Validation Report -- Agent 6

Recipes: echo-go, java-spring, spring-boot, dotnet, ghost

---

# Validation: echo-go

## Repo: recipe-echo

## Findings

### F1: Cache service is redis/keydb in repo, valkey in recipe
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `hostname: cache`, `type: valkey@7.2` in import.yml; `REDIS_HOST: cache`, `REDIS_PORT: ${cache_port}` in zerops.yml
- Repo says: `hostname: redis`, `type: keydb@6` in import; `REDIS_HOST: redis`, `REDIS_PORT: $redis_port` in zerops.yml
- Recommendation: Update recipe to match repo: hostname `redis`, type `keydb@6`, env vars `REDIS_HOST: redis`, `REDIS_PORT: ${redis_port}`. Alternatively, if the recipe intends to modernize to valkey, note this is a deliberate divergence and update env var references to match (`cache` hostname and `${cache_port}`). Either way, recipe and import must be internally consistent.

### F2: Env var syntax -- recipe uses `${var}`, repo uses `$var` (no braces)
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `${db_port}`, `${db_user}`, `${db_password}`, `${storage_apiUrl}`, etc.
- Repo says: `$db_port`, `$db_user`, `$db_password`, `$storage_apiUrl`, etc.
- Recommendation: Both syntaxes are valid in Zerops. The recipe uses the more explicit `${var}` form which is preferred. This is cosmetic -- P2.

### F3: Build cache missing in repo zerops.yml
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `cache: [~/go/pkg/mod]` in build section
- Repo says: no `cache` key in build section
- Recommendation: The recipe adds a best-practice cache directive. This is a recipe enhancement over the repo, which is acceptable. No change needed.

### F4: initCommands seed service name differs
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `zsc execOnce ${appVersionId} -- /var/www/app -seed`
- Repo says: `zsc execOnce seed -- /var/www/app -seed`
- Recommendation: The repo uses `seed` as the service name for `zsc execOnce` (a fixed string identifying the one-time operation), while the recipe uses `${appVersionId}` (a Zerops runtime variable that changes per deploy). Using `${appVersionId}` means the seed command re-runs on every new deploy version, while `seed` means it runs only once ever. This is a semantic difference. The repo behavior (run once ever) is likely correct for DB seeding. Update recipe to use `zsc execOnce seed -- /var/www/app -seed`.

### F5: objectStorageSize differs
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `objectStorageSize: 2`
- Repo says: `objectStorageSize: 5`
- Recommendation: Minor sizing difference. Recipe uses a smaller default which is fine for guidance purposes. No change required.

### F6: Missing services in recipe import.yml (mailpit, adminer)
- Severity: P2
- Category: knowledge-gap
- Recipe says: import.yml has 4 services (app, db, cache, storage)
- Repo says: import.yml has 6 services (app, db, storage, redis, mailpit, adminer)
- Recommendation: The recipe correctly omits build-from-git services (mailpit, adminer) since they are auxiliary dev tools, not part of the core recipe pattern. The recipe does reference `SMTP_HOST: mailpit` in env vars, which is sufficient. No change needed.

### F7: S3 client uses Secure:true but recipe says use http://, never https://
- Severity: P1
- Category: env-var-pattern
- Recipe says (Gotchas): "`S3_ENDPOINT` from `${storage_apiUrl}` is an internal URL -- use `http://`, never `https://`"
- Repo says (main.go:262-269): `minio.New(strings.TrimPrefix(os.Getenv("S3_ENDPOINT"), "https://"), &minio.Options{Secure: true})`
- Recommendation: The repo code strips the `https://` prefix from the endpoint and sets `Secure: true`, meaning it connects via HTTPS. The recipe gotcha says "never https://" which contradicts the actual repo code. Investigate whether `${storage_apiUrl}` returns an http:// or https:// URL on Zerops and update the gotcha accordingly.

### F8: build.base version uses go@latest in repo, go@1 in recipe
- Severity: P2
- Category: version-drift
- Recipe says: `base: go@1` in zerops.yml build section
- Repo says: `base: go@latest` in zerops.yml build section
- Recommendation: Both are valid. `go@1` is more specific. The recipe is consistent with its import.yml (`type: go@1`). No change needed.

## Verdict: NEEDS_FIX (3 issues: F1, F4, F7)

---

# Validation: java-spring

## Repo: recipe-java

## Findings

### F1: Java version -- recipe says java@21, repo uses java@17
- Severity: P1
- Category: version-drift
- Recipe says: `base: java@21` (build and run), `type: java@21` (import)
- Repo says: `base: java@17` (build and run), `type: java@17` (import), pom.xml `<java.version>17</java.version>`
- Recommendation: The repo is pinned to Java 17 throughout (pom.xml, zerops.yml, import). Recipe upgraded to 21 which is a deliberate modernization. If this is intentional, document it. If the recipe should match the repo, downgrade to java@17. Either way, if recipe says java@21, the pom.xml guidance should also say `<java.version>21</java.version>`.

### F2: Build command -- recipe skips tests, repo does not
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `./mvnw clean package -DskipTests`
- Repo says: `./mvnw clean install`
- Recommendation: Recipe uses `package` (produces JAR without installing to local repo) and skips tests. Repo uses `install` (installs to local repo) and runs tests. Recipe's approach is more appropriate for CI/deploy. The difference between `package` and `install` is negligible for deploy. No change required, but note the divergence.

### F3: deployFiles artifact name -- recipe says app.jar, repo says recipe-1.0.0.jar
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `deployFiles: target/app.jar`, `start: java -jar target/app.jar` with guidance to set `<finalName>app</finalName>`
- Repo says: `deployFiles: [target/recipe-1.0.0.jar]`, `start: java -jar target/recipe-1.0.0.jar`, pom.xml has `<version>1.0.0</version>` and `<artifactId>recipe</artifactId>` with NO `<finalName>`
- Recommendation: Recipe provides better guidance (predictable artifact name via finalName). The repo does not follow this practice. This is a deliberate recipe improvement. The recipe Configuration section correctly documents the `<finalName>app</finalName>` pattern. No change to recipe needed -- this is a repo improvement suggestion.

### F4: DB_PORT not provided as env var, hardcoded in application.properties
- Severity: P2
- Category: env-var-pattern
- Recipe says: no DB_PORT env var in zerops.yml envVariables
- Repo says: `application.properties` has `spring.datasource.url=jdbc:postgresql://${DB_HOST}:5432/${DB_NAME}?sslmode=disable` (port 5432 hardcoded)
- Recommendation: Hardcoding well-known PostgreSQL port 5432 is acceptable per decision rules. The recipe correctly omits DB_PORT since it's not needed. No change required.

### F5: DB_USER hardcoded to "db" in recipe, repo uses ${DB_USER} from application.properties
- Severity: P1
- Category: env-var-pattern
- Recipe says: `DB_USER: db` (hardcoded string "db")
- Repo says: `DB_USER: db` (hardcoded string "db") in zerops.yml, but `spring.datasource.username=${DB_USER}` in application.properties
- Recommendation: Both recipe and repo use `DB_USER: db` as an env var set to the string "db". This is technically correct since the PostgreSQL service with hostname `db` has a default user named after the hostname. However, using `${db_user}` would be more robust. Per decision rules, hardcoded hostname-matching values are acceptable. No change needed.

### F6: Missing .m2 cache path in repo zerops.yml
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `cache: .m2`
- Repo says: no `cache` key in build section
- Recommendation: Recipe adds best-practice caching. This is a recipe enhancement. No change needed.

### F7: deployFiles format -- recipe uses bare string, repo uses list
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `deployFiles: target/app.jar` (string)
- Repo says: `deployFiles: [target/recipe-1.0.0.jar]` (list with one item, using dash syntax)
- Recommendation: Both formats are valid in Zerops. No change needed.

### F8: Recipe has no DB priority in import.yml
- Severity: P2
- Category: yaml-mismatch
- Recipe says: db service has `priority: 10`
- Repo says: db service has no `priority` field
- Recommendation: Recipe correctly adds priority to ensure DB starts before the API. This is a recipe improvement.

## Verdict: NEEDS_FIX (2 issues: F1 version clarity, F3 artifact name divergence documented but ok)

Actually revising -- the recipe improvements over the repo are intentional and documented. The only real concern is F1:

## Verdict: NEEDS_FIX (1 issue: F1 java version)

---

# Validation: spring-boot

## Repo: recipe-spring

## Findings

### F1: Missing server.address=0.0.0.0 in repo application.properties
- Severity: P0
- Category: yaml-mismatch
- Recipe says (Configuration + Gotchas): `server.address=0.0.0.0` is MANDATORY
- Repo says: `application.properties` does NOT contain `server.address` or `server.port`
- Recommendation: This is a critical omission in the repo. Without `server.address=0.0.0.0`, Spring Boot defaults to `localhost` which would cause 502 errors on Zerops. The repo likely works because Spring Boot may pick up the host from the environment or the default changed in newer versions, OR the repo is actually broken on Zerops. The recipe is CORRECT to flag this as mandatory. No recipe change needed -- repo should be fixed.

### F2: Env var syntax -- recipe uses `${var}`, repo uses `$var` (no braces)
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `${db_port}`, `${db_user}`, `${db_password}`, `${storage_apiUrl}`, etc.
- Repo says: `$db_port`, `$db_user`, `$db_password`, `$storage_apiUrl`, etc.
- Recommendation: Both syntaxes are valid in Zerops. P2 cosmetic.

### F3: Missing services in recipe import.yml (app/static frontend, adminer, mailpit)
- Severity: P2
- Category: knowledge-gap
- Recipe says: 3 services (api, db, storage)
- Repo says: 6 services (api, app/static, db, storage, adminer, mailpit)
- Recommendation: Recipe correctly focuses on the core services. The static frontend, adminer, and mailpit are auxiliary build-from-git services. No change needed.

### F4: Repo import has verticalAutoscaling minRam:1 on api, recipe also has it
- Severity: P2 (informational)
- Category: yaml-mismatch
- Recipe says: no `verticalAutoscaling` on api service
- Repo says: `verticalAutoscaling: {minRam: 1}` on api service
- Recommendation: Recipe omits `verticalAutoscaling` but the repo sets `minRam: 1` for the Java service (which needs more RAM). Consider adding `verticalAutoscaling: {minRam: 1}` to the recipe import.yml for the api service, since Java apps commonly need more than the default minimum RAM.

### F5: deployFiles path -- recipe uses `./target/api.jar`, repo uses `./target/api.jar` (with leading dot)
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `deployFiles: ./target/api.jar`
- Repo says: `deployFiles: [./target/api.jar]` (list format, also with leading dot)
- Recommendation: Both are consistent. No issue.

### F6: Recipe import has maxContainers:1, matching repo
- Severity: P2 (informational)
- Category: yaml-mismatch
- Recipe says: `maxContainers: 1`
- Repo says: `maxContainers: 1`
- Recommendation: Consistent. No change needed.

### F7: DB hardcoded name in application.properties
- Severity: P2
- Category: env-var-pattern
- Recipe says: no DB_NAME env var
- Repo says: `spring.datasource.url=jdbc:postgresql://${DB_HOST}:${DB_PORT}/db` (database name `db` is hardcoded in the JDBC URL)
- Recommendation: Hardcoded `db` matching the hostname is acceptable per decision rules.

## Verdict: NEEDS_FIX (1 issue: F1 is P0 in the repo, but the recipe correctly documents the requirement; F4 is a minor recipe improvement)

---

# Validation: dotnet

## Repo: recipe-dotnet

## Findings

### F1: .NET version -- recipe says dotnet@9, repo uses dotnet@6
- Severity: P1
- Category: version-drift
- Recipe says: `base: dotnet@9` (build), `type: dotnet@9` (import)
- Repo says: `base: dotnet@6` (build and run), `type: dotnet@6` (import), `dotnet.csproj` targets `net6.0`
- Recommendation: The recipe upgraded from .NET 6 to .NET 9. This is a major version jump (6 -> 9). The repo's .csproj targets `net6.0` and its packages (EF Core 6.0.0, Npgsql 6.0.0) are .NET 6 specific. If the recipe targets dotnet@9, it should note that the csproj TargetFramework needs to be updated to `net9.0` and packages upgraded accordingly. Currently the recipe guidance would fail if applied to the repo code as-is.

### F2: Build command -- recipe uses `dotnet publish`, repo uses `dotnet build`
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `dotnet publish -c Release -o app`
- Repo says: `dotnet build -o app`
- Recommendation: `dotnet publish` is the correct command for deployment (produces a self-contained output with all dependencies). `dotnet build` only compiles. The recipe is correct; the repo uses a less optimal command. No recipe change needed -- this is a recipe improvement.

### F3: run.base missing in repo zerops.yml but present in recipe
- Severity: P2
- Category: yaml-mismatch
- Recipe says: no `run.base` specified in zerops.yml (inherits from build base)
- Repo says: `run.base: dotnet@6` explicitly specified
- Recommendation: The recipe omits `run.base` which means the run environment inherits the build base. The repo explicitly sets it. Both approaches work. No change needed.

### F4: DB priority differs
- Severity: P2
- Category: yaml-mismatch
- Recipe says: db `priority: 10`
- Repo says: db `priority: 1`
- Recommendation: Both ensure DB starts before the app. The actual priority value difference (1 vs 10) doesn't matter functionally as long as it's higher than the app service (which has no priority, defaulting to 0). No change needed.

### F5: Recipe says `ASPNETCORE_URLS` env var, repo uses Kestrel config in appsettings.json
- Severity: P1
- Category: env-var-pattern
- Recipe says: set `ASPNETCORE_URLS: http://0.0.0.0:5000` as env var in zerops.yml AND call `UseUrls` in code
- Repo says: no `ASPNETCORE_URLS` in zerops.yml envVariables. Binding configured via `appsettings.json` Kestrel endpoints: `"Url": "http://0.0.0.0:5000"` and `"Url": "http://[::]:5000"`. No `UseUrls` call in Program.cs.
- Recommendation: The repo uses `appsettings.json` Kestrel configuration instead of `ASPNETCORE_URLS`. Both are valid approaches. The recipe recommends a belt-and-suspenders approach (env var + UseUrls). The recipe is more defensive. No change needed, but recipe could note appsettings.json as an alternative approach.

### F6: Recipe says ForwardedHeaders middleware is required -- repo does NOT have it
- Severity: P1
- Category: knowledge-gap
- Recipe says (Configuration): add `UseForwardedHeaders` middleware in Program.cs
- Repo says: Program.cs does NOT call `UseForwardedHeaders`
- Recommendation: The recipe correctly documents this as a requirement for proper HTTPS scheme detection behind Zerops proxy. The repo is missing it. This is a repo bug, not a recipe bug. No recipe change needed.

### F7: Health check response format differs
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `Results.Ok(new { status = "ok" })`
- Repo says: `Results.Ok(new { status = "UP" })`
- Recommendation: Minor difference in status string ("ok" vs "UP"). Both return 200 OK which is what the HTTP health check cares about. The actual body content doesn't matter for the health check. Consider making recipe match repo for consistency: `status = "UP"`.

### F8: NuGet cache path
- Severity: P2
- Category: yaml-mismatch
- Recipe says: `cache: ~/.nuget`
- Repo says: no `cache` key in build section
- Recommendation: Recipe adds best-practice caching. This is a recipe enhancement.

### F9: start command -- recipe says `dotnet app.dll`, repo says `dotnet dotnet.dll`
- Severity: P1
- Category: yaml-mismatch
- Recipe says: `start: dotnet app.dll`
- Repo says: `start: dotnet dotnet.dll`
- Recommendation: The DLL name depends on the .csproj filename. The repo's project is `dotnet.csproj`, so the output DLL is `dotnet.dll`. The recipe uses `app.dll` which would only work if the .csproj was named `app.csproj`. The recipe's Gotchas section correctly notes "DLL name matches .csproj filename" but the example zerops.yml uses a generic name. Consider updating the recipe to use `dotnet.dll` to match the repo, or add a comment noting the name must match the .csproj.

## Verdict: NEEDS_FIX (4 issues: F1 version gap, F5 binding approach, F7 status text, F9 DLL name)

---

# Validation: ghost

## Repo: recipe-ghost

## Findings

### F1: Repo zerops.yml has extra env var REGION not in recipe
- Severity: P2
- Category: knowledge-gap
- Recipe says: no `REGION` env var
- Repo says: `REGION: usc1`
- Recommendation: The `REGION` env var appears to be region-specific configuration. If it affects Ghost behavior, document it. If it's only used internally for some S3/infra routing, it may be fine to omit. Investigate and document if needed.

### F2: DB priority -- recipe says 10, repo says 1
- Severity: P2
- Category: yaml-mismatch
- Recipe says: db and storage `priority: 10`
- Repo says: db and storage `priority: 1`
- Recommendation: Both ensure DB/storage start before Ghost (which has no priority). The numeric value difference doesn't matter functionally. No change needed.

### F3: Repo import has mailpit service, recipe does not
- Severity: P2
- Category: knowledge-gap
- Recipe says: import has 3 services (db, storage, ghost)
- Repo says: import has 4 services (db, storage, ghost, mailpit)
- Recommendation: Recipe correctly omits build-from-git auxiliary services. Ghost's config.production.json references mailpit for mail transport, and the recipe's env vars don't include mail config (it's in config.production.json). No change needed.

### F4: objectStoragePolicy matches
- Severity: P2 (informational -- PASS)
- Category: yaml-mismatch
- Recipe says: `objectStoragePolicy: public-objects-read`
- Repo says: `objectStoragePolicy: public-objects-read`
- Recommendation: Consistent. No change needed.

### F5: Ghost import has verticalAutoscaling minRam:1 -- recipe matches
- Severity: P2 (informational -- PASS)
- Category: yaml-mismatch
- Recipe says: `verticalAutoscaling: {minRam: 1}`
- Repo says: `verticalAutoscaling: {minRam: 1}`
- Recommendation: Consistent. No change needed.

### F6: zerops.yml fully matches between recipe and repo
- Severity: P2 (informational -- PASS)
- Category: yaml-mismatch
- Recommendation: The zerops.yml in the recipe closely matches the repo. All env vars, ports, build commands, deploy files, and start command are consistent. The only addition in the repo is the `REGION` env var (F1).

### F7: config.production.json references mailpit for mail transport
- Severity: P2
- Category: knowledge-gap
- Recipe says: (not documented in recipe)
- Repo says: config.production.json has `"mail": {"transport": "SMTP", "options": {"host": "mailpit", "port": 1025}}`
- Recommendation: The recipe's Common Failures and Gotchas don't mention the config.production.json mail configuration. Consider adding a note that mail transport is configured in config.production.json and defaults to mailpit for development.

## Verdict: PASS (only P2 informational issues)

---

# Summary

| Recipe | Verdict | P0 | P1 | P2 |
|--------|---------|----|----|-----|
| echo-go | NEEDS_FIX | 0 | 3 | 5 |
| java-spring | NEEDS_FIX | 0 | 1 | 6 |
| spring-boot | NEEDS_FIX | 1 (repo) | 0 | 5 |
| dotnet | NEEDS_FIX | 0 | 4 | 4 |
| ghost | PASS | 0 | 0 | 7 |

## Top Priority Fixes

1. **echo-go F1**: Cache service hostname/type mismatch (redis/keydb@6 vs cache/valkey@7.2)
2. **echo-go F4**: initCommands `zsc execOnce` service name (`seed` vs `${appVersionId}`)
3. **echo-go F7**: S3 endpoint gotcha contradicts actual repo code (Secure:true)
4. **dotnet F1**: Version gap dotnet@9 vs repo dotnet@6 -- recipe guidance would fail on repo code
5. **dotnet F9**: DLL name `app.dll` vs repo's `dotnet.dll`
6. **dotnet F5**: Binding approach (ASPNETCORE_URLS vs Kestrel appsettings.json)
7. **java-spring F1**: Java version 21 vs repo's 17
8. **spring-boot F1** (P0 in repo): Missing `server.address=0.0.0.0` in repo -- recipe correctly flags this
