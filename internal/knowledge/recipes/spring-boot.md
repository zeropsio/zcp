# Spring Boot Multi-Service on Zerops
Spring Boot API with PostgreSQL, S3 Object Storage, static frontend, Adminer, and Mailpit.

## Keywords
spring boot, java, postgresql, s3, object-storage, maven, multi-service, api

## TL;DR
Spring Boot API with PostgreSQL and S3 -- service priorities ensure DB and storage start before the API, `server.address=0.0.0.0` is mandatory.

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
        DB_HOST: db
        DB_PORT: ${db_port}
        DB_USER: ${db_user}
        DB_PASSWORD: ${db_password}
        S3_ENDPOINT: ${storage_apiUrl}
        S3_BUCKET: ${storage_bucketName}
        S3_ACCESS_KEY: ${storage_accessKeyId}
        S3_SECRET_KEY: ${storage_secretAccessKey}
        MAIL_HOST: mailpit
        MAIL_PORT: "1025"
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

**pom.xml** -- set `<finalName>api</finalName>` so artifact is always `target/api.jar`:
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

- **deployFiles is for stage/production** — this recipe shows the optimized deploy pattern for cross-deploy targets or git-based builds. For self-deploying services (dev or simple mode), use `deployFiles: [.]` so source + zerops.yml survive the deploy. With `[.]`, build output stays in its original directory under `/var/www/` — adjust `start` path accordingly (see Deploy Semantics in platform reference).
- **`server.address=0.0.0.0`** is MANDATORY for Spring Boot -- defaults to localhost, which causes 502
- **Service priorities** -- DB/Storage priority 10 (start first), API priority 5 (starts after dependencies)
- **`S3_ENDPOINT`** from `${storage_apiUrl}` is an internal URL -- use `http://`, never `https://`
- **Maven tests skipped** (`--define maven.test.skip`) during build to save time
- **Mailpit** is for development only -- replace `MAIL_HOST` and `MAIL_PORT` with production SMTP settings before going live
- **Adminer** should have public access disabled or be removed entirely in production
- **File upload demo** uses S3-compatible Object Storage
- **`.m2` cache** -- Maven downloads are cached between builds to speed up subsequent deploys
- **healthCheck is for stage/production only** — the recipe shows the production `run:` config. When using dev+stage pairs, omit `healthCheck` (and `readinessCheck`) from the dev entry. Dev uses `start: zsc noop --silent` with manual server control.
