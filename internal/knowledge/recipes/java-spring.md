# Java Spring Boot on Zerops
Java Spring Boot API with PostgreSQL, Maven Wrapper build, fat JAR deploy.

## Keywords
java, spring boot, maven, postgresql, jar, jvm, api

## TL;DR
Java Spring Boot with Maven Wrapper and fat JAR deploy -- `server.address=0.0.0.0` is mandatory, bind to all interfaces.

## zerops.yml
```yaml
zerops:
  - setup: api
    build:
      base: java@21
      buildCommands:
        - ./mvnw clean package -DskipTests
      deployFiles: target/app.jar
      cache: .m2
    run:
      base: java@21
      ports:
        - port: 8080
          httpSupport: true
      envVariables:
        DB_NAME: db
        DB_HOST: db
        DB_USER: db
        DB_PASS: ${db_password}
      start: java -jar target/app.jar
      healthCheck:
        httpGet:
          port: 8080
          path: /status
```

## import.yml
```yaml
services:
  - hostname: api
    type: java@21
    enableSubdomainAccess: true

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10
```

## Configuration

**application.properties** (REQUIRED):
```properties
server.address=0.0.0.0
```

**pom.xml** -- set `<finalName>app</finalName>` in `<build>` so artifact is always `target/app.jar`:
```xml
<build>
  <finalName>app</finalName>
  <plugins>
    <plugin>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-maven-plugin</artifactId>
    </plugin>
  </plugins>
</build>
```

## Vanilla Java (no Spring Boot)

When no Maven Wrapper exists, install Maven via prepareCommands:

```yaml
zerops:
  - setup: api
    build:
      base: java@21
      os: ubuntu
      prepareCommands:
        - sudo apt-get update -qq && sudo apt-get install -y -qq maven
      buildCommands:
        - mvn -q clean package -DskipTests
      deployFiles: target/app.jar
      cache: .m2
    run:
      base: java@21
      ports:
        - port: 8080
          httpSupport: true
      start: java -jar target/app.jar
```

**Fat JAR plugin REQUIRED** in pom.xml (one of):
- `maven-shade-plugin` -- creates uber-JAR with all dependencies
- `maven-assembly-plugin` -- alternative for jar-with-dependencies

**Binding** for `com.sun.net.httpserver.HttpServer`:
```java
HttpServer.create(new InetSocketAddress("0.0.0.0", port), 0);
```

## Path behavior

`deployFiles: target/app.jar` places file at `/var/www/target/app.jar`.
`start: java -jar target/app.jar` resolves correctly (relative to `/var/www`).

## Gotchas
- **`server.address=0.0.0.0`** is MANDATORY for Spring Boot -- defaults to localhost, which causes 502
- **Fat JAR required** -- thin JARs cause `ClassNotFoundException` at runtime
- **Maven/Gradle NOT pre-installed** -- use Maven Wrapper (`./mvnw`) or install via prepareCommands with `sudo`
- **`build.os: ubuntu`** required when using `apt-get` -- Alpine default has no apt-get
- **`<finalName>app</finalName>`** -- normalize artifact name so `deployFiles` and `start` paths are predictable
- **`.m2` cache** -- Maven downloads are cached between builds to speed up subsequent deploys
- **healthCheck is for stage/production only** -- the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
