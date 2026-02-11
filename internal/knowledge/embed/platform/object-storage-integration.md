# Object Storage Integration on Zerops

## Keywords
object storage, s3, minio, aws, upload, files, media, storage integration, flysystem, boto3, aws-sdk, path style, bucket, persistent files

## TL;DR
Zerops Object Storage is S3-compatible (MinIO). Always set `AWS_USE_PATH_STYLE_ENDPOINT: true`. Use env var references `${storage_*}` for credentials. Containers are volatile — use Object Storage for any persistent files.

## Environment Variables

When you create an Object Storage service, Zerops auto-generates these env vars:

| Variable | Description |
|----------|-------------|
| `${storage_apiUrl}` | S3 endpoint URL |
| `${storage_accessKeyId}` | Access key |
| `${storage_secretAccessKey}` | Secret key |
| `${storage_bucketName}` | Default bucket name |

Reference them in your app's env:
```yaml
envSecrets:
  S3_ENDPOINT: ${storage_apiUrl}
  S3_ACCESS_KEY: ${storage_accessKeyId}
  S3_SECRET_KEY: ${storage_secretAccessKey}
  S3_BUCKET: ${storage_bucketName}
envVariables:
  S3_REGION: us-east-1
  AWS_USE_PATH_STYLE_ENDPOINT: "true"
```

## Path Style Endpoint (Required)

Zerops uses MinIO which requires **path-style** URLs (not virtual-hosted):

```
# Path-style (correct for Zerops):
https://endpoint.com/bucket-name/object-key

# Virtual-hosted (WRONG for Zerops):
https://bucket-name.endpoint.com/object-key
```

**Every S3 client must be configured for path-style access.**

## Framework Integration

### PHP (Laravel — Flysystem)
```php
// config/filesystems.php
's3' => [
    'driver' => 's3',
    'endpoint' => env('S3_ENDPOINT'),
    'use_path_style_endpoint' => true,  // REQUIRED
    'key' => env('S3_ACCESS_KEY'),
    'secret' => env('S3_SECRET_KEY'),
    'region' => env('S3_REGION', 'us-east-1'),
    'bucket' => env('S3_BUCKET'),
],
```
Package: `league/flysystem-aws-s3-v3`

### Node.js (AWS SDK v3)
```javascript
import { S3Client } from '@aws-sdk/client-s3';
const s3 = new S3Client({
  endpoint: process.env.S3_ENDPOINT,
  forcePathStyle: true,  // REQUIRED
  credentials: {
    accessKeyId: process.env.S3_ACCESS_KEY,
    secretAccessKey: process.env.S3_SECRET_KEY,
  },
  region: process.env.S3_REGION || 'us-east-1',
});
```
Package: `@aws-sdk/client-s3`

### Python (boto3)
```python
import boto3
s3 = boto3.client('s3',
    endpoint_url=os.environ['S3_ENDPOINT'],
    aws_access_key_id=os.environ['S3_ACCESS_KEY'],
    aws_secret_access_key=os.environ['S3_SECRET_KEY'],
    region_name='us-east-1',
    config=boto3.session.Config(s3={'addressing_style': 'path'}),  # REQUIRED
)
```
Package: `boto3`

### Java (AWS SDK)
```java
S3Client s3 = S3Client.builder()
    .endpointOverride(URI.create(System.getenv("S3_ENDPOINT")))
    .serviceConfiguration(S3Configuration.builder()
        .pathStyleAccessEnabled(true)  // REQUIRED
        .build())
    .credentialsProvider(StaticCredentialsProvider.create(
        AwsBasicCredentials.create(
            System.getenv("S3_ACCESS_KEY"),
            System.getenv("S3_SECRET_KEY"))))
    .region(Region.US_EAST_1)
    .build();
```

## import.yaml Definition

```yaml
services:
  - hostname: storage
    type: object-storage
    objectStorageSize: 2              # GB
    objectStoragePolicy: public-read  # or private
    priority: 10
```

**Policies:**
- `public-read` — files accessible via URL (media, avatars, static assets)
- `private` — files only accessible via S3 API (documents, backups)

## When to Use Object Storage

| Scenario | Use Object Storage? |
|----------|-------------------|
| User uploads (avatars, documents) | Yes — containers are volatile |
| Media files (images, videos) | Yes — serve via public URL |
| Build artifacts | No — deploy via zerops.yaml |
| Temporary files | No — container disk is fine |
| Logs | No — use Zerops logging |
| Database dumps | Yes — for backup storage |

## Gotchas
1. **`AWS_USE_PATH_STYLE_ENDPOINT: true` is required**: Zerops uses MinIO which doesn't support virtual-hosted style
2. **Containers are volatile**: Files on disk are lost on restart — always use Object Storage for persistent data
3. **Region is required but ignored**: Set `us-east-1` — MinIO ignores it but SDKs require it
4. **Public URL format**: `${storage_apiUrl}/${storage_bucketName}/path/to/file`
5. **No CDN integration**: Object Storage is direct access — use Zerops CDN separately if needed

## See Also
- zerops://services/object-storage
- zerops://examples/connection-strings
- zerops://operations/production-checklist
- zerops://config/import-yml-patterns
