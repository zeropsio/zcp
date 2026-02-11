# Java on Zerops

## Keywords
java, jdk, maven, gradle, spring boot, jar, war, jvm, heap, memory, spring

## TL;DR
Java on Zerops supports JDK 17/21 with Maven/Gradle wrappers; tune JVM heap with `-Xmx` relative to container RAM and deploy the fat JAR.

## Zerops-Specific Behavior
- Versions: 17, 21
- Base: Alpine (default)
- Build tools: Maven (`mvnw`), Gradle (`gradlew`) — use wrappers
- Working directory: `/var/www`
- No default port — must configure
- Deploy: Fat JAR or exploded directory

## Configuration
```yaml
zerops:
  - setup: api
    build:
      base: java@21
      buildCommands:
        - ./mvnw clean install --define maven.test.skip
      deployFiles:
        - ./target/api.jar
      cache:
        - .m2
    run:
      start: java -jar target/api.jar
      ports:
        - port: 8080
          httpSupport: true
```

### Gradle
```yaml
zerops:
  - setup: api
    build:
      base: java@21
      buildCommands:
        - ./gradlew build -x test
      deployFiles:
        - build/libs/*.jar
      cache:
        - .gradle
    run:
      start: java -jar build/libs/app.jar
      ports:
        - port: 8080
          httpSupport: true
```

## JVM Memory Tuning
- Set `-Xmx` to ~75% of container max RAM
- Example: Container max 1GB → `-Xmx768m`
- Without `-Xmx`, JVM may consume all container RAM and trigger OOM

## Gotchas
1. **Set `-Xmx` explicitly**: Without it, JVM defaults can exceed container RAM → OOM kill
2. **Use wrapper scripts**: `./mvnw` and `./gradlew` ensure correct build tool version
3. **Cache `.m2` or `.gradle`**: Maven/Gradle dependency downloads are slow — always cache
4. **Deploy only the JAR**: Don't deploy entire `target/` directory — just the fat JAR
5. **Spring Boot bind address**: Set `server.address=0.0.0.0` — default binds to localhost only

## See Also
- zerops://services/_common-runtime
- zerops://platform/scaling
- zerops://examples/zerops-yml-runtimes
