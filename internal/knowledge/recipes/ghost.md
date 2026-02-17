# Ghost CMS on Zerops

Ghost blogging platform with MariaDB. **CRITICAL**: Ghost does not support horizontal scaling.

## zerops.yml (key sections)
```yaml
zerops:
  - setup: ghost
    build:
      base: nodejs@18
      buildCommands:
        - |
          cd ./current
          yarn add ghost-storage-adapter-s3
          mkdir -p ./content/adapters/storage
          cp -r ./node_modules/ghost-storage-adapter-s3 ./content/adapters/storage/s3
    run:
      envVariables:
        GHOST_STORAGE_ADAPTER_S3_ENDPOINT: ${storage_apiUrl}
        GHOST_STORAGE_ADAPTER_S3_FORCE_PATH_STYLE: true
        database__connection__host: ${db_hostname}
      start: ghost run
```

## import.yml (critical constraint)
```yaml
services:
  - hostname: ghost
    maxContainers: 1  # REQUIRED: Ghost cannot scale horizontally
    verticalAutoscaling:
      minRam: 1
```

## Gotchas
- **maxContainers: 1** is MANDATORY (Ghost does not support multiple containers)
- For MariaDB HA: run `SET GLOBAL wsrep_sync_wait=1;` in initCommands (Galera sync)
- Caching (Valkey/CDN) often better than DB HA due to static content nature
