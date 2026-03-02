# Ghost CMS on Zerops

Ghost blogging platform with MariaDB and S3 storage. Ghost does NOT support horizontal scaling.

## Keywords
ghost, nodejs, mariadb, cms, blog, content management, s3, object storage

## TL;DR
Ghost CMS on Node.js 18 with MariaDB and S3 content storage — `maxContainers: 1` is mandatory because Ghost cannot run in multiple containers.

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
    run:
      base: nodejs@18
      envVariables:
        AWS_DEFAULT_REGION: us-east-1
        AWS_ACCESS_KEY_ID: ${storage_accessKeyId}
        AWS_SECRET_ACCESS_KEY: ${storage_secretAccessKey}
        STORAGE_HOSTNAME: storage
        GHOST_STORAGE_ADAPTER_S3_PATH_BUCKET: ${storage_bucketName}
        GHOST_STORAGE_ADAPTER_S3_ASSET_HOST: ${storage_apiUrl}/${GHOST_STORAGE_ADAPTER_S3_PATH_BUCKET}
        GHOST_STORAGE_ADAPTER_S3_ENDPOINT: ${storage_apiUrl}
        GHOST_STORAGE_ADAPTER_S3_FORCE_PATH_STYLE: true
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
    objectStoragePolicy: public-objects-read
    priority: 10

  - hostname: ghost
    type: nodejs@18
    maxContainers: 1
    enableSubdomainAccess: true
    verticalAutoscaling:
      minRam: 1
```

## Common Failures
- **502 after deploy**: Ghost needs ~30s to boot; the `url` env var must match the actual subdomain URL (use `${zeropsSubdomain}`)
- **Images broken**: S3 adapter not installed or env vars incorrect; verify `GHOST_STORAGE_ADAPTER_S3_ENDPOINT` points to `${storage_apiUrl}`
- **DB connection refused**: MariaDB must be created with `priority: 10` so it starts before Ghost

## Gotchas
- **maxContainers: 1** is MANDATORY — Ghost uses local file locks and cannot run in multiple containers
- Ghost listens on port **2368** (not 3000 or 8080)
- The `url` env var controls all generated links; set it to `${zeropsSubdomain}` for development or your domain for production
- S3 storage adapter requires `ghost-storage-adapter-s3` installed during build and the adapter copied to `content/adapters/storage/s3`
- For MariaDB HA in production: switch to `mode: HA` and add `SET GLOBAL wsrep_sync_wait=1;` in initCommands for Galera sync
- Admin interface is at `/ghost` on the subdomain URL
