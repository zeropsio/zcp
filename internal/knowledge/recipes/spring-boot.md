# Spring Boot Multi-Service on Zerops

Spring Boot API with PostgreSQL, S3, static frontend, Adminer, Mailpit. Service priorities control startup order.

## zerops.yml
```yaml
zerops:
  - setup: api
    build:
      base: java@21
      buildCommands:
        - ./mvnw clean install --define maven.test.skip
      deployFiles: ./target/api.jar  # Single JAR
    run:
      envVariables:
        DB_HOST: db
        S3_ENDPOINT: ${storage_apiUrl}
        S3_BUCKET: ${storage_bucketName}
        S3_ACCESS_KEY: ${storage_accessKeyId}
        S3_SECRET_KEY: ${storage_secretAccessKey}
      start: java -jar target/api.jar
```

## import.yml (service priorities)
```yaml
services:
  - hostname: api
    priority: 5
    minRam: 1GB
    maxContainers: 1
  - hostname: db
    priority: 10  # Higher = starts first
  - hostname: storage
    priority: 10
    objectStoragePolicy: public-read
    objectStorageSize: 2GB
```

## Gotchas
- **Service priorities**: DB/Storage priority 10 (start first), API priority 5
- Maven tests skipped (--define maven.test.skip)
- 6 services: api + pg + s3 + static frontend + adminer + mailpit
- File upload demo uses Object Storage
