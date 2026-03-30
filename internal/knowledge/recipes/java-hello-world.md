---
description: "Java Hello World recipe running on Zerops, backed by a PostgreSQL database. Built with Spring Boot, the recipe demonstrates the full Zerops integration pattern: idempotent schema migration, database-backed health check, and ready-made environment configurations spanning the entire development lifecycle."
---

# Java Hello World on Zerops


# Java Hello World on Zerops


# Java Hello World on Zerops


# Java Hello World on Zerops


# Java Hello World on Zerops


# Java Hello World on Zerops





## Keywords
java, jdk, maven, gradle, spring, spring-boot, fat jar, zerops.yml, mvn

## TL;DR
JDK pre-installed but NO Maven/Gradle. Install Maven via `prepareCommands` on Ubuntu. Deploy a single fat JAR. Bind `server.address=0.0.0.0`.

### Base Image

Includes JDK, `git`, `wget` -- **NO Maven, NO Gradle pre-installed**.

NOTE: `java@latest` resolves to an older version, not the newest -- always specify the exact version explicitly.

### Build Procedure

1. Set `build.base: java@21`
2. **For new projects**: set `build.os: ubuntu`, `prepareCommands: [sudo apt-get update -qq && sudo apt-get install -y -qq maven]`, `buildCommands: [mvn -q clean package -DskipTests]`
3. **For existing projects with wrapper**: `buildCommands: [./mvnw clean package -DskipTests]`
4. `deployFiles: target/app.jar` (single fat JAR)
5. `run.start: java -jar target/app.jar`

### Fat JAR Required

Deploy a single fat/uber JAR. Use `maven-shade-plugin` or `spring-boot-maven-plugin`.

### Binding

`server.address=0.0.0.0` -- Spring Boot defaults to localhost!

### Key Settings

RAM: `-Xmx` = ~75% of container max RAM.
Cache: `.m2` or `.gradle`.

### Common Mistakes

- Bare `mvn` or `maven` in buildCommands -> "command not found" (not pre-installed)
- `apt-get` without `sudo` -> permission denied
- `apt-get` on default Alpine OS -> "command not found" (need `build.os: ubuntu`)
- Deploying thin JAR -> ClassNotFoundException at runtime
- Missing `server.address=0.0.0.0` for Spring Boot -> 502 Bad Gateway

### Resource Requirements

**Dev** (compilation on container): `minRam: 1.5` — Maven/Gradle + javac peak ~1.2 GB.
**Stage/Prod**: `minRam: 1` — JVM heap requires baseline allocation.

### JAR Naming

Without `<finalName>` in pom.xml, JAR name includes version: `target/{artifactId}-{version}.jar`. If version changes, deployFiles path breaks. Normalize: add `<build><finalName>app</finalName></build>` to pom.xml, then use `deployFiles: target/app.jar`.

### Deploy Patterns

**Dev deploy**: `deployFiles: [.]`, install maven in prepareCommands, `start: zsc noop --silent` (idle container -- agent starts `mvn -q compile exec:java` manually via SSH for iteration)
**Prod deploy**: `buildCommands: [mvn -q clean package -DskipTests]`, `deployFiles: target/app.jar`, `start: java -jar target/app.jar`

## zerops.yml

> Reference implementation — learn the patterns, adapt to your project.

```yaml
# The 'prod' setup compiles an optimized Spring Boot fat JAR
# for deployment. The 'dev' setup ships source code alongside
# the pre-built JAR so developers can SSH in, edit files, and
# rebuild freely using the pre-installed mvn and java tools.
zerops:
  - setup: prod
    build:
      base: java@21

      # Compile and package the Spring Boot fat JAR.
      # '-DskipTests' is intentional — integration tests
      # belong in CI pipelines, not in the Zerops build
      # container. Maven 3.9 is pre-installed on java@21.
      buildCommands:
        - mvn clean package -DskipTests

      # One file covers both the app and migration entry points.
      # ZIP layout in pom.xml enables PropertiesLauncher, which
      # reads -Dloader.main at runtime to switch between them.
      deployFiles:
        - target/app.jar

      # Maven resolves dependencies into ~/.m2 (outside the build
      # directory). 'cache: true' snapshots the build container
      # image — including ~/.m2 — so subsequent builds skip
      # downloading the dependency graph from Maven Central.
      cache: true

    # Zerops runs the readiness check after each new runtime
    # container starts and before it receives traffic from the
    # project balancer. Containers that fail are replaced,
    # not promoted.
    deploy:
      readinessCheck:
        httpGet:
          port: 8080
          path: /

    run:
      base: java@21

      # Run the migration exactly once per deployed version.
      # 'zsc execOnce ${appVersionId}' ensures a single container
      # executes even when minContainers > 1 — others wait.
      # In initCommands (not buildCommands) so migration and
      # new code are always deployed together atomically.
      #
      # PropertiesLauncher (ZIP layout) reads -Dloader.main
      # to invoke Migrate.main() directly — no Spring context
      # is created, just a plain JDBC connection.
      initCommands:
        - zsc execOnce ${appVersionId} -- java -Dloader.main=io.zerops.recipe.Migrate -jar target/app.jar

      ports:
        - port: 8080
          httpSupport: true

      # Env vars follow '{hostname}_{credential}' — for the 'db'
      # service: db_hostname, db_port, db_user, db_password.
      # DB_NAME matches the database name Zerops creates (same
      # as the service hostname).
      envVariables:
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

      start: java -jar target/app.jar

  - setup: dev
    build:
      base: java@21

      # Build the fat JAR during the build phase so that
      # target/app.jar is available for the initCommands
      # migration and for quick test runs after SSH.
      buildCommands:
        - mvn clean package -DskipTests

      # Deploy source for editing and the compiled JAR for
      # running migrations and the app. target/ is excluded
      # to avoid shipping hundreds of MB of build artifacts —
      # only app.jar is needed. Developers rebuild with
      # 'mvn package' or run directly with 'mvn spring-boot:run'.
      # zerops.yaml is included so 'zcli push' works from SSH.
      deployFiles:
        - ./src
        - ./pom.xml
        - ./zerops.yaml
        - target/app.jar

      cache: true

    run:
      base: java@21

      # Migration runs identically to prod — same JAR,
      # same zsc execOnce guard.
      initCommands:
        - zsc execOnce ${appVersionId} -- java -Dloader.main=io.zerops.recipe.Migrate -jar target/app.jar

      ports:
        - port: 8080
          httpSupport: true

      envVariables:
        DB_NAME: db
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASS: ${db_password}

      # Dev container stays idle. SSH in and use the pre-installed
      # tools: 'mvn spring-boot:run' for hot-reload development,
      # or 'java -jar target/app.jar' to run the compiled binary.
      # Database is already migrated and ready when you connect.
      start: zsc noop --silent
```
