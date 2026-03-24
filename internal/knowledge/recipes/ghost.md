# Ghost CMS on Zerops

Ghost blogging platform with MariaDB and S3 storage.

## Keywords
ghost, ghost-cli, ghost-storage-adapter-s3, blogging, publishing

## TL;DR
Ghost CMS on Node.js 18 with MariaDB and S3 content storage — `maxContainers: 1` is mandatory because Ghost uses local file locks and cannot run in multiple containers.

## zerops.yml
```yaml
zerops:
  - setup: ghost
    build:
      base: nodejs@18
      buildCommands:
        - sudo apk add git jq
        - |
          cd ./current
          yarn
          yarn add ghost-storage-adapter-s3
          mkdir -p ./content/adapters/storage
          cp -r ./node_modules/ghost-storage-adapter-s3 ./content/adapters/storage/s3
      deployFiles:
        - content
        - current
        - versions
        - .ghost-cli
        - config.production.json
    deploy:
      readinessCheck:
        httpGet:
          port: 2368
          path: /
    run:
      base: nodejs@18
      envVariables:
        AWS_DEFAULT_REGION: us-east-1
        AWS_ACCESS_KEY_ID: ${storage_accessKeyId}
        AWS_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        GHOST_STORAGE_ADAPTER_S3_PATH_BUCKET: ${storage_bucketName}
        GHOST_STORAGE_ADAPTER_S3_ASSET_HOST: ${storage_apiUrl}/${storage_bucketName}
        GHOST_STORAGE_ADAPTER_S3_ENDPOINT: ${storage_apiUrl}
        GHOST_STORAGE_ADAPTER_S3_FORCE_PATH_STYLE: "true"
        database__connection__database: ${db_hostname}
        database__connection__host: ${db_hostname}
        database__connection__password: ${db_password}
        database__connection__user: ${db_user}
        url: ${zeropsSubdomain}
      ports:
        - port: 2368
          httpSupport: true
      prepareCommands:
        - npm install -g ghost-cli
      start: ghost run
      healthCheck:
        httpGet:
          port: 2368
          path: /
```

## import.yml
```yaml
services:
  - hostname: db
    type: mariadb@10.6
    mode: NON_HA
    priority: 10

  - hostname: storage
    type: object-storage
    objectStorageSize: 2
    objectStoragePolicy: public-read
    priority: 10

  - hostname: ghost
    type: nodejs@18
    maxContainers: 1
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 1
```

## Gotchas

- **`maxContainers: 1` is MANDATORY** — Ghost uses local file locks and cannot run across multiple containers
- **Port 2368** — Ghost listens on 2368, not 3000 or 8080; declare in `ports` and all health checks
- **`url` env var** — controls all generated URLs (links, images, admin redirects); must match the actual domain; use `${zeropsSubdomain}` for dev or your custom domain for production
- **MariaDB `database__connection__database`** — set to `${db_hostname}`, not a custom name; MariaDB database name equals the service hostname on Zerops
- **S3 adapter installed during build** — `ghost-storage-adapter-s3` must be added under `./current/` (the Ghost installation directory) and copied to `content/adapters/storage/s3`
- **`GHOST_STORAGE_ADAPTER_S3_FORCE_PATH_STYLE: "true"`** — required for Zerops S3 (MinIO backend)
- **Ghost needs ~30s to boot** — `readinessCheck` on deploy path handles this; do not reduce the check timeout
- **MariaDB HA in production** — switch to `mode: HA` and add `SET GLOBAL wsrep_sync_wait=1;` in initCommands for Galera cluster sync
