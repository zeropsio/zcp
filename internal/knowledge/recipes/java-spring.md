# Java on Zerops

## Spring Boot (with Maven Wrapper)

```yaml
zerops:
  - setup: app
    build:
      base: java@21
      buildCommands:
        - ./mvnw clean package -DskipTests
      deployFiles: target/app.jar
      cache: .m2
    run:
      start: java -jar target/app.jar
      ports:
        - port: 8080
          httpSupport: true
```

Spring Boot `spring-boot-maven-plugin` produces a fat JAR automatically. Set `<finalName>app</finalName>` in pom.xml so artifact is always `target/app.jar`.

**application.properties** (REQUIRED):
```properties
server.address=0.0.0.0
```

## Vanilla Java (no framework, new project)

When no Maven Wrapper exists, install Maven via prepareCommands:

```yaml
zerops:
  - setup: app
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
      start: java -jar target/app.jar
      ports:
        - port: 8080
          httpSupport: true
```

**Fat JAR plugin REQUIRED** in pom.xml (one of):
- `maven-shade-plugin` — creates uber-JAR with all dependencies
- `maven-assembly-plugin` — alternative for jar-with-dependencies

Set `<finalName>app</finalName>` in `<build>` so artifact is always `target/app.jar`.

**Binding** for `com.sun.net.httpserver.HttpServer`:
```java
HttpServer.create(new InetSocketAddress("0.0.0.0", port), 0);
```

## Path behavior

`deployFiles: target/app.jar` → file at `/var/www/target/app.jar`
`start: java -jar target/app.jar` → correct (relative to `/var/www`)

## Gotchas
- **server.address=0.0.0.0** MANDATORY for Spring Boot (defaults to localhost → 502)
- **Fat JAR required**: thin JARs cause `ClassNotFoundException` at runtime
- **Maven/Gradle NOT pre-installed**: use Maven Wrapper (`./mvnw`) or install via prepareCommands with `sudo`
- **`build.os: ubuntu`** required when using `apt-get` (Alpine default has no apt-get)
- **`<finalName>app</finalName>`**: normalize artifact name so `deployFiles` and `start` paths are predictable
