---
surface: ig-item
verdict: pass
reason: concrete-action-ok
title: "forcePathStyle: true on S3 client for Zerops object storage"
---

> ### Pass `forcePathStyle: true` when constructing the S3 client
>
> **Why**: Zerops object storage is MinIO-backed and only serves
> path-style addressing. Virtual-hosted addressing (the SDK default)
> returns `403`.
>
> **Change to make** (Node.js @aws-sdk/client-s3):
>
> ```typescript
> import { S3Client } from '@aws-sdk/client-s3';
>
> const s3 = new S3Client({
>   endpoint: `https://${process.env.storage_hostname}`,
>   region: 'us-east-1',             // unused by MinIO; any non-empty value
>   credentials: {
>     accessKeyId: process.env.storage_accessKeyId!,
>     secretAccessKey: process.env.storage_secretAccessKey!,
>   },
>   forcePathStyle: true,             // REQUIRED for MinIO
> });
> ```
>
> Corresponding flag exists in every S3 client library — look up
> `path-style` or `virtual-hosted` in your library's config reference.
> See `zerops_knowledge topic=object-storage`.

**Why this passes the IG-item test.**
- Named platform mechanism (MinIO path-style addressing).
- Concrete code diff with the flag the porter flips.
- Extension hint for other libraries.
- Environment-variable references use the `${hostname}_*` shape the
  platform injects.

Spec §4 classification: IG item, concrete-action with fenced code.
