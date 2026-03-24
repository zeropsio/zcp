# Java on Zerops

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
