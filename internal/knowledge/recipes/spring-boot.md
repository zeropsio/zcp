# Spring Boot Multi-Service on Zerops

Spring Boot API with PostgreSQL, S3, static frontend, Adminer, Mailpit. Service priorities control startup order.

## Keywords
spring boot, java, postgresql, s3, object-storage, maven, multi-service

## TL;DR
Spring Boot API with PostgreSQL and S3 — service priorities ensure DB starts before the API.

## zerops.yml
```yaml
zerops:
  - setup: api
    build:
      base: java@21
      buildCommands:
        - ./mvnw clean install --define maven.test.skip
      deployFiles: ./target/api.jar
    run:
      envVariables:
        DB_HOST: db
        S3_ENDPOINT: ${storage_apiUrl}
        S3_BUCKET: ${storage_bucketName}
        S3_ACCESS_KEY: ${storage_accessKeyId}
        S3_SECRET_KEY: ${storage_secretAccessKey}
      start: java -jar target/api.jar
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
    priority: 10
```

## Gotchas
- **Service priorities**: DB/Storage priority 10 (start first), API priority 5
- Maven tests skipped (--define maven.test.skip)
- 6 services: api + pg + s3 + static frontend + adminer + mailpit
- File upload demo uses Object Storage
