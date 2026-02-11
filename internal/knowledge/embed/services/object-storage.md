# Object Storage on Zerops

## Keywords
object storage, s3, minio, bucket, blob, file storage, cdn, s3 compatible, upload, assets

## TL;DR
Object Storage on Zerops is S3-compatible (MinIO-based), provides one bucket per service with 1-100GB quota, and has no Zerops-managed backup — use S3 lifecycle policies.

## Zerops-Specific Behavior
- Backend: MinIO (S3-compatible API)
- One bucket per service (created automatically)
- Quota: 1-100 GB (configurable)
- Separate infrastructure from compute (not colocated)
- Env vars: `apiUrl`, `accessKeyId`, `secretAccessKey`, `bucketName`
- CDN integration: `${storageCdnUrl}` env var

## Connection Pattern
```
Endpoint: ${apiUrl}
Bucket: ${bucketName}
Access Key: ${accessKeyId}
Secret Key: ${secretAccessKey}
```

### AWS SDK Example (Node.js)
```javascript
const { S3Client } = require("@aws-sdk/client-s3");
const client = new S3Client({
  endpoint: process.env.apiUrl,
  region: "us-east-1",  // required but ignored
  credentials: {
    accessKeyId: process.env.accessKeyId,
    secretAccessKey: process.env.secretAccessKey,
  },
  forcePathStyle: true,  // required for MinIO
});
```

## Configuration
```yaml
# import.yaml
services:
  - hostname: storage
    type: object-storage
    objectStorageSize: 10           # GB (1-100)
    objectStoragePolicy: public-read  # optional: public-read or private (default)
    priority: 10                    # start before app services
```

**Import parameters:**
- `objectStorageSize` (required): Quota in GB (1-100)
- `objectStoragePolicy` (optional): `public-read` for public access, omit or `private` for private
- `priority`: Use same priority as databases (10) so storage is ready before app starts

## Gotchas
1. **No Zerops backup**: Object Storage is not backed up by Zerops — implement your own backup/lifecycle policies
2. **`forcePathStyle: true` required**: MinIO uses path-style URLs, not virtual-hosted-style
3. **`region` required but ignored**: AWS SDKs require a region — use any value (e.g., `us-east-1`)
4. **One bucket per service**: Cannot create additional buckets — create another Object Storage service if needed
5. **Separate infrastructure**: Network latency slightly higher than same-node services

## See Also
- zerops://platform/cdn
- zerops://services/shared-storage
- zerops://config/import-yml
