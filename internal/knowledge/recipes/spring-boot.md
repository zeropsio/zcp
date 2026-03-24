# Spring Boot Multi-Service on Zerops

Spring Boot API with PostgreSQL and S3 Object Storage.

## Keywords
spring-boot, spring, maven, jar, jvm, gradle, quarkus, micronaut

## TL;DR
Spring Boot API with PostgreSQL and S3 — service priorities ensure DB and storage start before the API, `server.address=0.0.0.0` is mandatory. Wire managed services via `${hostname_varName}`.

## zerops.yml
```yaml
zerops:
  - setup: api
    build:
      base: java@21
      buildCommands:
        - ./mvnw clean install --define maven.test.skip
      deployFiles: ./target/api.jar
      cache: .m2
    deploy:
      readinessCheck:
        httpGet:
          port: 8080
          path: /
    run:
      base: java@21
      ports:
        - port: 8080
          httpSupport: true
      envVariables:
        DB_HOST: ${db_hostname}
        DB_PORT: ${db_port}
        DB_NAME: ${db_dbName}
        DB_USER: ${db_user}
        DB_PASSWORD: ${db_password}
        S3_ENDPOINT: ${storage_apiUrl}
        S3_BUCKET: ${storage_bucketName}
        S3_ACCESS_KEY: ${storage_accessKeyId}
        S3_SECRET_KEY: ${storage_secretAccessKey}
      start: java -jar target/api.jar
      healthCheck:
        httpGet:
          port: 8080
          path: /
```

## import.yml
```yaml
services:
  - hostname: api
    type: java@21
    enableSubdomainAccess: true
    priority: 5
    maxContainers: 1

  - hostname: db
    type: postgresql@16
    mode: NON_HA
    priority: 10

  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    objectStoragePolicy: public-read
    priority: 10
```

## Configuration

**application.properties** (REQUIRED):
```properties
server.address=0.0.0.0
```

**pom.xml** — set `<finalName>api</finalName>` so artifact is always `target/api.jar`:
```xml
<build>
  <finalName>api</finalName>
  <plugins>
    <plugin>
      <groupId>org.springframework.boot</groupId>
      <artifactId>spring-boot-maven-plugin</artifactId>
    </plugin>
  </plugins>
</build>
```

## Gotchas

- **`server.address=0.0.0.0`** is MANDATORY for Spring Boot — defaults to localhost, which causes 502
- **Service priorities** — DB/Storage priority 10 (start first), API priority 5 (starts after dependencies)
- **`S3_ENDPOINT` is an internal URL** — use `http://`, never `https://` for internal S3 traffic
- **Maven tests skipped** (`--define maven.test.skip`) during build to save time
- **`.m2` cache** — Maven downloads are cached between builds to speed up subsequent deploys
- **Fat JAR required** — the spring-boot-maven-plugin packages all dependencies; thin JARs fail at runtime (run container has no Maven repo)
- **`${db_dbName}` not `${db_database}`** — PostgreSQL exposes `dbName`. Wrong name resolves to literal string silently.
